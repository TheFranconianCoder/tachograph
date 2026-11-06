package history

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRestoreFileDirectorySymlinkAndModes(t *testing.T) {
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
	first, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}

	writeFile(t, filepath.Join(worktree, "a.txt"), "two\n")
	if err := os.Remove(filepath.Join(worktree, "sub", "one.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("second snapshot: %v", err)
	}

	result, err := recorder.Restore(ctx, first.Hash, "a.txt")
	if err != nil {
		t.Fatalf("restore a.txt: %v", err)
	}
	if len(result.Restored) != 1 || result.Restored[0] != "a.txt" {
		t.Errorf("restored = %v, want a.txt", result.Restored)
	}
	if content := readFile(t, filepath.Join(worktree, "a.txt")); content != "one\n" {
		t.Errorf("a.txt = %q, want the first snapshot", content)
	}

	result, err = recorder.Restore(ctx, first.Hash, "sub")
	if err != nil {
		t.Fatalf("restore sub: %v", err)
	}
	if len(result.Restored) != 1 || result.Restored[0] != "sub/one.txt" {
		t.Errorf("restored = %v, want sub/one.txt", result.Restored)
	}
	if content := readFile(t, filepath.Join(worktree, "sub", "one.txt")); content != "nested\n" {
		t.Errorf("sub/one.txt = %q", content)
	}

	result, err = recorder.Restore(ctx, first.Hash, "script.sh")
	if err != nil {
		t.Fatalf("restore script.sh: %v", err)
	}
	if len(result.Restored) != 1 {
		t.Errorf("restored = %v, want script.sh", result.Restored)
	}
	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("script.sh mode = %o, want executable", info.Mode().Perm())
	}

	if runtime.GOOS != "windows" {
		result, err = recorder.Restore(ctx, first.Hash, "link")
		if err != nil {
			t.Fatalf("restore link: %v", err)
		}
		if len(result.Restored) != 1 {
			t.Errorf("restored = %v, want link", result.Restored)
		}
		target, err := os.Readlink(filepath.Join(worktree, "link"))
		if err != nil || target != "a.txt" {
			t.Errorf("link target = %q, %v; want a.txt", target, err)
		}
	}

	if _, err := recorder.Restore(ctx, first.Hash, "../outside.txt"); err == nil {
		t.Error("restore accepted a path outside the worktree")
	}
	if _, err := recorder.Restore(ctx, first.Hash, ".git/config"); err == nil {
		t.Error("restore accepted a path in the Git directory")
	}
	if _, err := recorder.Restore(ctx, first.Hash, "missing.txt"); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Errorf("restore missing file error = %v, want a not-found error", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
