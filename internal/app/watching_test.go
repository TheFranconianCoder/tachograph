package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/history"
	"github.com/TheFranconianCoder/tachograph/internal/recorder"
)

func TestActivityKeepsNewestEventPerWorktree(t *testing.T) {
	a := &App{activity: make(map[string]Activity)}
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

	a.recordRecorderEvent(recorder.Event{Time: base, Path: "/a", Kind: "snapshot", Hash: "1"})
	a.recordRecorderEvent(recorder.Event{Time: base.Add(time.Minute), Path: "/b", Kind: "snapshot", Hash: "2"})
	a.recordRecorderEvent(recorder.Event{Time: base.Add(2 * time.Minute), Path: "/a", Kind: "error", Message: "boom"})

	got := a.activitySnapshot()
	if len(got) != 2 {
		t.Fatalf("activity entries = %d, want 2 (one per worktree)", len(got))
	}
	if got[0].Path != "/a" || got[0].Kind != "error" || got[0].Message != "boom" {
		t.Errorf("newest entry = %+v, want the error for /a", got[0])
	}
	if got[1].Path != "/b" {
		t.Errorf("second entry = %+v, want /b", got[1])
	}
}

func TestActivitySnapshotDropsMainWorktreeStats(t *testing.T) {
	base := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	stats := &history.ChangeStats{Files: 1, Added: 1, Deleted: 1}
	a := &App{
		activity: map[string]Activity{
			"/main": {
				Time:  base.Format(time.RFC3339),
				Kind:  "snapshot",
				Path:  "/main",
				Stats: stats,
			},
			"/worktree": {
				Time:  base.Add(time.Minute).Format(time.RFC3339),
				Kind:  "snapshot",
				Path:  "/worktree",
				Stats: stats,
			},
		},
		projects: []ProjectStatus{{MainWorktree: "/main"}},
	}

	got := a.activitySnapshot()
	for _, entry := range got {
		switch entry.Path {
		case "/main":
			if entry.Stats != nil {
				t.Errorf("main worktree stats = %+v, want nil", entry.Stats)
			}
		case "/worktree":
			if entry.Stats == nil {
				t.Error("linked worktree stats missing")
			}
		default:
			t.Errorf("unexpected activity path %q", entry.Path)
		}
	}
}

func TestActivityStateRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	statePath := activityStatePath()
	if statePath == "" {
		t.Fatal("activityStatePath returned an empty path")
	}

	now := time.Now().UTC().Truncate(time.Second)
	a := &App{
		log:        discardAppLogger(),
		statePath:  statePath,
		lastActive: map[string]time.Time{"/worktree": now},
		activity: map[string]Activity{
			"/worktree": {
				Time: now.Format(time.RFC3339),
				Kind: "snapshot",
				Path: "/worktree",
				Hash: "abc123",
			},
		},
		stateDirty: true,
	}
	a.saveActivityState()

	loadedLastActive, loadedActivity := loadActivityState(a.log)
	got, ok := loadedLastActive["/worktree"]
	if !ok {
		t.Fatalf("loaded activity = %v, missing /worktree", loadedLastActive)
	}
	if !got.Equal(now) {
		t.Errorf("loaded time = %s, want %s", got, now)
	}
	if len(loadedActivity) != 1 || loadedActivity[0].Hash != "abc123" {
		t.Errorf("loaded entries = %+v, want the snapshot activity", loadedActivity)
	}
}

func TestLoadActivityStateDropsOldStats(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := activityStatePath()
	if path == "" {
		t.Fatal("activityStatePath returned an empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	stored := activityState{
		Version: 2,
		Activity: []Activity{{
			Time:  time.Now().UTC().Format(time.RFC3339),
			Kind:  "snapshot",
			Path:  "/worktree",
			Hash:  "abc123",
			Stats: &history.ChangeStats{Files: 1, Added: 1},
		}},
	}
	data, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, activity := loadActivityState(discardAppLogger())
	if len(activity) != 1 {
		t.Fatalf("loaded activity = %+v, want one entry", activity)
	}
	if activity[0].Stats != nil {
		t.Errorf("loaded stats = %+v, want them cleared", activity[0].Stats)
	}
}

func TestGroupActiveUsesActivityAndGrace(t *testing.T) {
	now := time.Now()
	a := &App{
		hibernateAfter: 5 * time.Minute,
		lastActive:     map[string]time.Time{"/a": now.Add(-10 * time.Minute)},
	}
	group := &watchGroup{paths: []string{"/a"}}

	if a.groupActiveLocked(group, now) {
		t.Fatal("a group idle for ten minutes must not be active")
	}

	group.graceUntil = now.Add(time.Minute)
	if !a.groupActiveLocked(group, now) {
		t.Fatal("the startup grace must keep the group active")
	}

	a.lastActive["/a"] = now.Add(-time.Minute)
	if !a.groupActiveLocked(group, now) {
		t.Fatal("a recent snapshot must keep the group active")
	}
}

func discardAppLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
