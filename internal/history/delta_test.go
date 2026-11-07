package history

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/TheFranconianCoder/tachograph/internal/difft"
)

func TestDeltaRendering(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}

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
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}
	writeFile(t, filepath.Join(worktree, "a.txt"), "two\n")
	snapshot, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	fake := filepath.Join(base, "delta")
	script := "#!/bin/sh\nprintf 'ARGS=%s\\n' \"$*\"\ncat\n" +
		"printf '\\033[38;5;196mDELTA COLOR\\033[0m\\n'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	recorder.Diff = difft.Tool{Name: difft.ModeDelta, Path: fake}

	ctx = WithDiffTheme(ctx, "light")
	detail, err := recorder.SnapshotDetail(ctx, snapshot.Hash, "")
	if err != nil {
		t.Fatalf("SnapshotDetail(delta): %v", err)
	}
	if detail.DiffTool != "delta" {
		t.Errorf("DiffTool = %q, want delta", detail.DiffTool)
	}
	for _, want := range []string{
		"--paging=never",
		"--line-numbers",
		"--no-gitconfig",
		"--width=160",
		"--true-color=always",
		"--light",
		`--plus-style syntax "#e8f3ec"`,
		"DELTA COLOR",
		"+two",
	} {
		if !strings.Contains(detail.Diff, want) {
			t.Errorf("delta output = %q, want %q", detail.Diff, want)
		}
	}

	dark, err := recorder.SnapshotDetail(WithDiffTheme(ctx, "dark"), snapshot.Hash, "")
	if err != nil {
		t.Fatalf("SnapshotDetail(delta, dark): %v", err)
	}
	for _, want := range []string{
		"--dark",
		`--plus-style syntax "#132820"`,
	} {
		if !strings.Contains(dark.Diff, want) {
			t.Errorf("dark delta output = %q, want %q", dark.Diff, want)
		}
	}

	recorder.Diff = difft.Tool{Name: difft.ModeDelta, Path: filepath.Join(base, "missing")}
	fallback, err := recorder.SnapshotDetail(ctx, snapshot.Hash, "")
	if err != nil {
		t.Fatalf("SnapshotDetail(fallback): %v", err)
	}
	if fallback.DiffTool != "git" || !strings.Contains(fallback.Diff, "+two") {
		t.Errorf("fallback = %q (%s), want the git diff", fallback.Diff, fallback.DiffTool)
	}
}
