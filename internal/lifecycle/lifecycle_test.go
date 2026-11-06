package lifecycle

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheFranconianCoder/tachograph/internal/history"
)

func TestApplyCopiesEnvAndKeepsItOutOfHistory(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktree := filepath.Join(base, "worktree")

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "init", "-b", "main")
	mustGit(t, project, "config", "user.name", "Test")
	mustGit(t, project, "config", "user.email", "test@example.com")
	mustGit(t, project, "config", "commit.gpgsign", "false")

	writeFile(t, filepath.Join(project, "a.txt"), "base\n")
	writeFile(t, filepath.Join(project, ".env.example"), "EXAMPLE=1\n")
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	// .env is deliberately untracked, like a real local environment file.
	writeFile(t, filepath.Join(project, ".env"), "SECRET=1\n")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	recorder, err := history.New(filepath.Join(base, "history")).Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	result, err := Apply(ctx, recorder, Options{
		Actions: []Action{{Copy: &CopyAction{Path: ".env", Optional: true}}},
		Hash:    "test-hash",
		Log:     discardLogger(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Copied) != 1 || result.Copied[0] != ".env" {
		t.Fatalf("copied = %v, want [.env]", result.Copied)
	}

	copied, err := os.ReadFile(filepath.Join(worktree, ".env"))
	if err != nil {
		t.Fatalf("read copied .env: %v", err)
	}
	if string(copied) != "SECRET=1\n" {
		t.Errorf("copied .env = %q", copied)
	}

	marker, err := recorder.Config(ctx, "local-history.setupHash")
	if err != nil {
		t.Fatal(err)
	}
	if marker != "test-hash" {
		t.Errorf("setup marker = %q, want test-hash", marker)
	}

	second, err := Apply(ctx, recorder, Options{
		Actions: []Action{{Copy: &CopyAction{Path: ".env", Optional: true}}},
		Hash:    "other-hash",
		Log:     discardLogger(),
	})
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if !second.Skipped {
		t.Fatal("second Apply should be skipped because the marker exists")
	}

	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	files := mustGit(t, recorder.Placeholder, "ls-files")
	if strings.Contains(files, "\n.env\n") || strings.HasPrefix(files, ".env\n") {
		t.Errorf("history contains .env:\n%s", files)
	}
	if !strings.Contains(files, ".env.example") {
		t.Errorf("history is missing .env.example:\n%s", files)
	}
}

func TestCleanRelativeRejectsEscapes(t *testing.T) {
	for _, path := range []string{"", ".", "..", "../secret", "/etc/passwd"} {
		if _, err := cleanRelative(path); err == nil {
			t.Errorf("cleanRelative(%q) accepted a path it should reject", path)
		}
	}
	if got, err := cleanRelative("config/local.env"); err != nil || got != "config/local.env" {
		t.Errorf("cleanRelative valid path = %q, %v", got, err)
	}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCopyGlobsOverwriteAndDirectorySkip(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktree := filepath.Join(base, "worktree")

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

	writeFile(t, filepath.Join(project, ".env"), "ENV=1\n")
	writeFile(t, filepath.Join(project, ".env.local"), "LOCAL=1\n")
	writeFile(t, filepath.Join(project, ".env.production.local"), "PROD=1\n")
	writeFile(t, filepath.Join(project, "config", "local.yaml"), "local: true\n")
	writeFile(t, filepath.Join(project, "config", "keep.txt"), "keep\n")
	writeFile(t, filepath.Join(project, "config", "nested", "deep.local"), "deep\n")

	store := history.New(filepath.Join(base, "history"))
	recorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	actions := []Action{{Copy: &CopyAction{Path: ".env*", Optional: true}}}

	result, err := CopyOnly(recorder, actions, discardLogger())
	if err != nil {
		t.Fatalf("CopyOnly: %v", err)
	}
	want := []string{".env", ".env.local", ".env.production.local"}
	if strings.Join(result.Copied, ",") != strings.Join(want, ",") {
		t.Errorf("copied = %v, want %v", result.Copied, want)
	}

	dirResult, err := CopyOnly(recorder, []Action{{Copy: &CopyAction{Path: "config/*", Optional: true}}}, discardLogger())
	if err != nil {
		t.Fatalf("CopyOnly config: %v", err)
	}
	if strings.Join(dirResult.Copied, ",") != "config/keep.txt,config/local.yaml" {
		t.Errorf("config copies = %v, want the files and no directory", dirResult.Copied)
	}
	if _, err := os.Stat(filepath.Join(worktree, "config", "nested", "deep.local")); !os.IsNotExist(err) {
		t.Error("directory match was copied recursively")
	}

	// Existing files are preserved unless overwrite is set.
	writeFile(t, filepath.Join(worktree, ".env.local"), "edited\n")
	if _, err := CopyOnly(recorder, actions, discardLogger()); err != nil {
		t.Fatalf("CopyOnly overwrite=false: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(worktree, ".env.local")); err != nil || string(got) != "edited\n" {
		t.Errorf(".env.local = %q, %v; want the local edit preserved", got, err)
	}
	overwrite := []Action{{Copy: &CopyAction{Path: ".env*", Optional: true, Overwrite: true}}}
	if _, err := CopyOnly(recorder, overwrite, discardLogger()); err != nil {
		t.Fatalf("CopyOnly overwrite=true: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(worktree, ".env.local")); err != nil || string(got) != "LOCAL=1\n" {
		t.Errorf(".env.local = %q, %v; want the main worktree content", got, err)
	}

	// Every copied path is excluded from the history.
	exclude, err := os.ReadFile(filepath.Join(recorder.HistoryRepo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range want {
		if !strings.Contains(string(exclude), path) {
			t.Errorf("exclude does not contain %s:\n%s", path, exclude)
		}
	}

	if _, err := CopyOnly(recorder, []Action{{Copy: &CopyAction{Path: "missing/*.none"}}}, discardLogger()); err == nil {
		t.Error("required missing glob was accepted")
	}
	if _, err := CopyOnly(recorder, []Action{{Copy: &CopyAction{Path: "missing/*.none", Optional: true}}}, discardLogger()); err != nil {
		t.Errorf("optional missing glob failed: %v", err)
	}

	for _, path := range []string{"", ".", "..", "../secret", "/etc/passwd", ".git/config", "config/[bad"} {
		if _, err := cleanRelativePattern(path); err == nil {
			t.Errorf("cleanRelativePattern(%q) accepted a path it should reject", path)
		}
	}
}
