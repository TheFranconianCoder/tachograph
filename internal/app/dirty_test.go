package app

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWorktreeDirty(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	testGit(t, dir, "init", "-b", "main")
	testGit(t, dir, "config", "user.name", "Test")
	testGit(t, dir, "config", "user.email", "test@example.com")
	testGit(t, dir, "config", "commit.gpgsign", "false")
	testWrite(t, filepath.Join(dir, ".gitignore"), "ignored/\n")
	testWrite(t, filepath.Join(dir, "tracked.txt"), "one\n")
	testGit(t, dir, "add", "-A")
	testGit(t, dir, "commit", "-m", "base")

	dirty, err := worktreeDirty(ctx, dir)
	if err != nil {
		t.Fatalf("worktreeDirty(clean): %v", err)
	}
	if dirty {
		t.Error("clean worktree reported dirty")
	}

	testWrite(t, filepath.Join(dir, "tracked.txt"), "two\n")
	dirty, err = worktreeDirty(ctx, dir)
	if err != nil {
		t.Fatalf("worktreeDirty(tracked): %v", err)
	}
	if !dirty {
		t.Error("modified tracked file did not mark the worktree dirty")
	}

	testGit(t, dir, "checkout", "--", "tracked.txt")
	testWrite(t, filepath.Join(dir, "new.txt"), "new\n")
	dirty, err = worktreeDirty(ctx, dir)
	if err != nil {
		t.Fatalf("worktreeDirty(untracked): %v", err)
	}
	if !dirty {
		t.Error("untracked file did not mark the worktree dirty")
	}

	testGit(t, dir, "add", "-A")
	testGit(t, dir, "commit", "-m", "add untracked")
	testWrite(t, filepath.Join(dir, "ignored", "secret.txt"), "ignored\n")
	dirty, err = worktreeDirty(ctx, dir)
	if err != nil {
		t.Fatalf("worktreeDirty(ignored): %v", err)
	}
	if dirty {
		t.Error("ignored file marked the worktree dirty")
	}
}

func TestRefreshDirtyCoversTheSourceWorktree(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	testGit(t, dir, "init", "-b", "main")
	testGit(t, dir, "config", "user.name", "Test")
	testGit(t, dir, "config", "user.email", "test@example.com")
	testGit(t, dir, "config", "commit.gpgsign", "false")
	testWrite(t, filepath.Join(dir, "tracked.txt"), "one\n")
	testGit(t, dir, "add", "-A")
	testGit(t, dir, "commit", "-m", "base")

	app := &App{
		projects: []ProjectStatus{{
			Name:         "project",
			MainWorktree: dir,
			Worktrees:    []WorktreeStatus{{Path: dir}},
		}},
		dirty: newDirtyState(),
	}

	app.refreshDirty(ctx)
	if app.projectsSnapshot()[0].Worktrees[0].Dirty {
		t.Error("clean source worktree reported dirty")
	}

	testWrite(t, filepath.Join(dir, "tracked.txt"), "two\n")
	app.refreshDirty(ctx)
	if !app.projectsSnapshot()[0].Worktrees[0].Dirty {
		t.Error("changed source worktree reported clean")
	}

	testGit(t, dir, "add", "-A")
	testGit(t, dir, "commit", "-m", "change")
	app.refreshDirty(ctx)
	if app.projectsSnapshot()[0].Worktrees[0].Dirty {
		t.Error("committed source worktree reported dirty")
	}
}
