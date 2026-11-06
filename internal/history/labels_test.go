package history

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotLabels(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktree := filepath.Join(base, "worktree")
	store := New(filepath.Join(base, "history"))

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "init", "-b", "main")
	mustGit(t, project, "config", "user.name", "Test")
	mustGit(t, project, "config", "user.email", "test@example.com")
	mustGit(t, project, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(project, "a.txt"), "base\n")
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	recorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(worktree, "a.txt"), "one\n")
	snapshot, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Short hashes resolve to the full commit, whitespace is folded.
	label, err := recorder.SetLabel(ctx, snapshot.Hash[:8], "  before \n refactor ")
	if err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	if label != "before refactor" {
		t.Errorf("label = %q, want %q", label, "before refactor")
	}

	page, err := recorder.Timeline(ctx, 50, 0, "")
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if page.Entries[0].Label != "before refactor" {
		t.Errorf("timeline label = %q, want %q", page.Entries[0].Label, "before refactor")
	}
	detail, err := recorder.SnapshotDetail(ctx, snapshot.Hash, "")
	if err != nil {
		t.Fatalf("SnapshotDetail: %v", err)
	}
	if detail.Label != "before refactor" {
		t.Errorf("snapshot label = %q, want %q", detail.Label, "before refactor")
	}

	// The label is persisted in the project store and survives a rebuild.
	known, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(known) != 1 {
		t.Fatalf("known worktrees = %d, want 1", len(known))
	}
	rebuilt := store.RecorderFromKnown(known[0])
	page, err = rebuilt.Timeline(ctx, 50, 0, "")
	if err != nil {
		t.Fatalf("rebuilt Timeline: %v", err)
	}
	if page.Entries[0].Label != "before refactor" {
		t.Errorf("persisted label = %q, want %q", page.Entries[0].Label, "before refactor")
	}

	if _, err := recorder.SetLabel(ctx, snapshot.Hash, ""); err != nil {
		t.Fatalf("clear label: %v", err)
	}
	detail, err = recorder.SnapshotDetail(ctx, snapshot.Hash, "")
	if err != nil {
		t.Fatalf("SnapshotDetail after clear: %v", err)
	}
	if detail.Label != "" {
		t.Errorf("label after clear = %q, want empty", detail.Label)
	}

	if _, err := recorder.SetLabel(ctx, snapshot.Hash, strings.Repeat("x", labelMaxLength+1)); err == nil {
		t.Error("SetLabel accepted an overlong label")
	}
	if _, err := recorder.SetLabel(ctx, strings.Repeat("0", 40), "missing"); !errors.Is(err, ErrSnapshotNotFound) {
		t.Errorf("SetLabel(missing) error = %v, want ErrSnapshotNotFound", err)
	}
	if _, err := recorder.SetLabel(ctx, "../escape", "x"); err == nil {
		t.Error("SetLabel accepted an invalid hash")
	}
}
