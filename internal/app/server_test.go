package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/history"
	"github.com/TheFranconianCoder/tachograph/internal/recorder"
)

// A revert writes into the worktree without a watcher event. It must record the
// resulting snapshot immediately instead of waiting for the next cold poll.
func TestRevertTriggersSnapshotImmediately(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktree := filepath.Join(base, "worktree")

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	testGit(t, project, "init", "-b", "main")
	testGit(t, project, "config", "user.name", "Test")
	testGit(t, project, "config", "user.email", "test@example.com")
	testGit(t, project, "config", "commit.gpgsign", "false")
	testWrite(t, filepath.Join(project, "keep.txt"), "keep\n")
	testGit(t, project, "add", "-A")
	testGit(t, project, "commit", "-m", "base")
	testGit(t, project, "worktree", "add", "-b", "feature", worktree)

	store := history.New(filepath.Join(base, "history"))
	historyRecorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	testWrite(t, filepath.Join(worktree, "a.txt"), "one\n")
	if _, err := historyRecorder.Snapshot(ctx); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}
	testWrite(t, filepath.Join(worktree, "a.txt"), "two\n")
	snapshot, err := historyRecorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	supervisor := recorder.New(recorder.Options{
		Interval:    time.Hour,
		MaxInterval: time.Hour,
		Workers:     1,
		Log:         discardAppLogger(),
	})
	supervisor.Set(historyRecorder.Worktree.Path, historyRecorder)
	supervisor.SetCold(historyRecorder.Worktree.Path, true)
	go supervisor.Run(ctx)

	app := &App{
		historyStore: store,
		supervisor:   supervisor,
		log:          discardAppLogger(),
		lastActive:   make(map[string]time.Time),
		watchGroups:  make(map[string]*watchGroup),
	}

	body, err := json.Marshal(map[string]any{
		"path": historyRecorder.Worktree.Path,
		"from": snapshot.Hash,
		"to":   snapshot.Hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/revert", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	app.handleRevert(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	waitForApp(t, 5*time.Second, func() bool {
		status, ok := supervisor.Status(historyRecorder.Worktree.Path)
		return ok && status.Snapshots >= 1
	})
	if content, err := os.ReadFile(filepath.Join(worktree, "a.txt")); err != nil || string(content) != "one\n" {
		t.Fatalf("a.txt = %q, %v; want the reverted content", content, err)
	}
	if _, ok := app.lastActive[historyRecorder.Worktree.Path]; !ok {
		t.Error("revert did not mark the worktree active")
	}
}

func waitForApp(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
