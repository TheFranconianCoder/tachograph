package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorktreeMetadataSignature(t *testing.T) {
	base := t.TempDir()
	common := filepath.Join(base, "project", ".git")
	admin := filepath.Join(common, "worktrees")
	if err := os.MkdirAll(filepath.Join(admin, "feature-a"), 0o755); err != nil {
		t.Fatal(err)
	}

	app := &App{projects: []ProjectStatus{
		{CommonDir: common},
		{CommonDir: common},
	}}
	first := app.worktreeMetadataSignature()
	if first == "" {
		t.Fatal("signature is empty for a project with a worktree")
	}
	if second := app.worktreeMetadataSignature(); second != first {
		t.Errorf("signature changed without a worktree change: %q -> %q", first, second)
	}

	if err := os.MkdirAll(filepath.Join(admin, "feature-b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if changed := app.worktreeMetadataSignature(); changed == first {
		t.Errorf("signature did not change after adding a worktree: %q", changed)
	}

	app.projects = nil
	if empty := app.worktreeMetadataSignature(); empty != "" {
		t.Errorf("signature without projects = %q, want empty", empty)
	}
}
