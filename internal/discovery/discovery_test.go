package discovery

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func TestScanFindsProjectAndWorktrees(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	worktree := filepath.Join(root, "project--feature")

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "init", "-b", "main")
	mustGit(t, project, "config", "user.name", "Test")
	mustGit(t, project, "config", "user.email", "test@example.com")
	mustGit(t, project, "config", "commit.gpgsign", "false")

	if err := os.WriteFile(filepath.Join(project, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	result := Scan(context.Background(), []string{root}, nil, discardLogger())
	if len(result.Errors) != 0 {
		t.Fatalf("Scan errors: %v", result.Errors)
	}
	if len(result.Projects) != 1 {
		t.Fatalf("projects = %d, want 1: %+v", len(result.Projects), result.Projects)
	}

	discovered := result.Projects[0]
	if discovered.Name != "project" {
		t.Errorf("project name = %q, want project", discovered.Name)
	}
	if discovered.MainWorktree != project {
		t.Errorf("main worktree = %q, want %q", discovered.MainWorktree, project)
	}
	if len(discovered.Worktrees) != 2 {
		t.Fatalf("worktrees = %d, want 2: %+v", len(discovered.Worktrees), discovered.Worktrees)
	}

	var branches []string
	for _, wt := range discovered.Worktrees {
		branches = append(branches, wt.Branch)
	}
	sort.Strings(branches)
	if got, want := branches, []string{"feature", "main"}; !equal(got, want) {
		t.Errorf("branches = %v, want %v", got, want)
	}
}

func TestScanSkipsDeletedWorktrees(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	worktree := filepath.Join(root, "project--feature")

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "init", "-b", "main")
	mustGit(t, project, "config", "user.name", "Test")
	mustGit(t, project, "config", "user.email", "test@example.com")
	mustGit(t, project, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(project, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}

	result := Scan(context.Background(), []string{root}, nil, discardLogger())
	if len(result.Errors) != 0 {
		t.Fatalf("Scan errors: %v", result.Errors)
	}
	if len(result.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(result.Projects))
	}
	discovered := result.Projects[0]
	if len(discovered.Worktrees) != 1 {
		t.Fatalf("worktrees = %d, want only the main worktree: %+v",
			len(discovered.Worktrees), discovered.Worktrees)
	}
	if discovered.Worktrees[0].Path != project {
		t.Errorf("remaining worktree = %q, want %q", discovered.Worktrees[0].Path, project)
	}
}

func TestScanReportsMissingRoot(t *testing.T) {
	result := Scan(context.Background(), []string{filepath.Join(t.TempDir(), "missing")}, nil, discardLogger())
	if len(result.Errors) == 0 {
		t.Fatal("expected an error for a missing root")
	}
	if len(result.Projects) != 0 {
		t.Fatalf("projects = %d, want 0", len(result.Projects))
	}
}

func TestScanIgnoresNestedNoise(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "package"), 0o755); err != nil {
		t.Fatal(err)
	}
	result := Scan(context.Background(), []string{root}, nil, discardLogger())
	if len(result.Projects) != 0 {
		t.Fatalf("projects = %d, want 0", len(result.Projects))
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

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func equal(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestScanFindsWorktreesThroughPatterns(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "projects", "repo")
	worktree := filepath.Join(base, "trees", "repo--feature")
	emptyRoot := filepath.Join(base, "empty")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(emptyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
		t.Fatal(err)
	}

	mustGit(t, project, "init", "-b", "main")
	mustGit(t, project, "config", "user.name", "Test")
	mustGit(t, project, "config", "user.email", "test@example.com")
	mustGit(t, project, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(project, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	// The project and its worktree both live outside the root; only the pattern
	// points at the worktree directory.
	result := Scan(context.Background(), []string{emptyRoot}, []string{filepath.Join(base, "trees", "*")}, discardLogger())
	if len(result.Errors) != 0 {
		t.Fatalf("scan errors = %v", result.Errors)
	}
	if len(result.Projects) != 1 {
		t.Fatalf("projects = %+v, want the repository behind the worktree", result.Projects)
	}
	projectResult := result.Projects[0]
	if projectResult.CommonDir == "" || len(projectResult.Worktrees) != 2 {
		t.Errorf("project = %+v, want both the main and linked worktree", projectResult)
	}

	relative := Scan(context.Background(), nil, []string{"../trees/*"}, discardLogger())
	if len(relative.Errors) == 0 {
		t.Error("relative pattern was accepted")
	}
}
