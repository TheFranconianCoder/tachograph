package history

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRevertSingleSnapshot(t *testing.T) {
	recorder, worktree := newRevertRecorder(t)
	ctx := t.Context()

	writeFile(t, filepath.Join(worktree, "a.txt"), "one\n")
	writeFile(t, filepath.Join(worktree, "sub", "one.txt"), "nested\n")
	script := filepath.Join(worktree, "script.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("a.txt", filepath.Join(worktree, "link")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}

	writeFile(t, filepath.Join(worktree, "a.txt"), "two\n")
	if err := os.Remove(filepath.Join(worktree, "sub", "one.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(worktree, "new.txt"), "new\n")
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	snapshot, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	result, err := recorder.Revert(ctx, snapshot.Hash, snapshot.Hash)
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if !hasPath(result.Restored, "a.txt") ||
		!hasPath(result.Restored, "sub/one.txt") ||
		!hasPath(result.Restored, "script.sh") {
		t.Errorf("restored = %v, want a.txt, sub/one.txt and script.sh", result.Restored)
	}
	if !hasPath(result.Deleted, "new.txt") {
		t.Errorf("deleted = %v, want new.txt", result.Deleted)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("skipped = %v, want none", result.Skipped)
	}

	if content := readFile(t, filepath.Join(worktree, "a.txt")); content != "one\n" {
		t.Errorf("a.txt = %q, want the previous version", content)
	}
	if content := readFile(t, filepath.Join(worktree, "sub", "one.txt")); content != "nested\n" {
		t.Errorf("sub/one.txt = %q", content)
	}
	if _, err := os.Stat(filepath.Join(worktree, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("new.txt still exists: %v", err)
	}
	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("script.sh mode = %o, want executable", info.Mode().Perm())
	}
	if content := readFile(t, filepath.Join(worktree, "keep.txt")); content != "keep\n" {
		t.Errorf("keep.txt = %q, want it untouched", content)
	}
	if runtime.GOOS != "windows" {
		target, err := os.Readlink(filepath.Join(worktree, "link"))
		if err != nil || target != "a.txt" {
			t.Errorf("link target = %q, %v; want a.txt", target, err)
		}
	}
}

func TestRevertRange(t *testing.T) {
	recorder, worktree := newRevertRecorder(t)
	ctx := t.Context()

	writeFile(t, filepath.Join(worktree, "a.txt"), "one\n")
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}

	writeFile(t, filepath.Join(worktree, "a.txt"), "two\n")
	first, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	writeFile(t, filepath.Join(worktree, "b.txt"), "new\n")
	second, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}

	result, err := recorder.Revert(ctx, first.Hash, second.Hash)
	if err != nil {
		t.Fatalf("Revert range: %v", err)
	}
	if !hasPath(result.Restored, "a.txt") {
		t.Errorf("restored = %v, want a.txt", result.Restored)
	}
	if !hasPath(result.Deleted, "b.txt") {
		t.Errorf("deleted = %v, want b.txt", result.Deleted)
	}
	if content := readFile(t, filepath.Join(worktree, "a.txt")); content != "one\n" {
		t.Errorf("a.txt = %q, want the pre-range version", content)
	}
	if _, err := os.Stat(filepath.Join(worktree, "b.txt")); !os.IsNotExist(err) {
		t.Errorf("b.txt still exists: %v", err)
	}
	if content := readFile(t, filepath.Join(worktree, "keep.txt")); content != "keep\n" {
		t.Errorf("keep.txt = %q, want it untouched", content)
	}

	if _, err := recorder.Revert(ctx, second.Hash, first.Hash); err == nil {
		t.Error("Revert accepted reversed endpoints")
	}
}

func TestRevertSelectedFiles(t *testing.T) {
	recorder, worktree := newRevertRecorder(t)
	ctx := t.Context()

	writeFile(t, filepath.Join(worktree, "a.txt"), "one\n")
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}
	writeFile(t, filepath.Join(worktree, "a.txt"), "two\n")
	writeFile(t, filepath.Join(worktree, "b.txt"), "new\n")
	snapshot, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	result, err := recorder.Revert(ctx, snapshot.Hash, snapshot.Hash, "a.txt")
	if err != nil {
		t.Fatalf("Revert selected: %v", err)
	}
	if !hasPath(result.Restored, "a.txt") {
		t.Errorf("restored = %v, want a.txt", result.Restored)
	}
	if hasPath(result.Deleted, "b.txt") {
		t.Errorf("deleted = %v, want b.txt untouched", result.Deleted)
	}
	if content := readFile(t, filepath.Join(worktree, "a.txt")); content != "one\n" {
		t.Errorf("a.txt = %q, want the previous version", content)
	}
	if content := readFile(t, filepath.Join(worktree, "b.txt")); content != "new\n" {
		t.Errorf("b.txt = %q, want it untouched", content)
	}

	stale, err := recorder.Revert(ctx, snapshot.Hash, snapshot.Hash, "missing.txt")
	if err != nil {
		t.Fatalf("Revert stale selection: %v", err)
	}
	if !hasPath(stale.Skipped, "missing.txt") {
		t.Errorf("skipped = %v, want missing.txt", stale.Skipped)
	}
}

func TestRevertRename(t *testing.T) {
	recorder, worktree := newRevertRecorder(t)
	ctx := t.Context()

	writeFile(t, filepath.Join(worktree, "old.txt"), "content\n")
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}
	if err := os.Rename(filepath.Join(worktree, "old.txt"), filepath.Join(worktree, "new.txt")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	result, err := recorder.Revert(ctx, snapshot.Hash, snapshot.Hash)
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if !hasPath(result.Restored, "old.txt") {
		t.Errorf("restored = %v, want old.txt", result.Restored)
	}
	if !hasPath(result.Deleted, "new.txt") {
		t.Errorf("deleted = %v, want new.txt", result.Deleted)
	}
	if content := readFile(t, filepath.Join(worktree, "old.txt")); content != "content\n" {
		t.Errorf("old.txt = %q", content)
	}
	if _, err := os.Stat(filepath.Join(worktree, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("new.txt still exists: %v", err)
	}
}

func TestDeleteRestoredPathRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	if _, err := deleteRestoredPath(root, "../outside.txt"); err == nil {
		t.Error("delete accepted a path outside the worktree")
	}
	if _, err := deleteRestoredPath(root, ".git/config"); err == nil {
		t.Error("delete accepted a path in the Git directory")
	}
}

func newRevertRecorder(t *testing.T) (*Recorder, string) {
	t.Helper()
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
	writeFile(t, filepath.Join(project, "keep.txt"), "keep\n")
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	recorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	return recorder, worktree
}

func hasPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
