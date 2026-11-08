package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEnsureAndSnapshot(t *testing.T) {
	ctx := context.Background()
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

	writeFile(t, filepath.Join(project, ".gitignore"), "node_modules/\n")
	writeFile(t, filepath.Join(project, "a.txt"), "one\n")
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	writeFile(t, filepath.Join(worktree, "a.txt"), "two\n")
	writeFile(t, filepath.Join(worktree, "node_modules", "ignored.txt"), "ignored\n")

	recorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if recorder.Project.Name != "project" {
		t.Errorf("project name = %q, want %q", recorder.Project.Name, "project")
	}
	if got, want := canonical(t, recorder.Worktree.Path), canonical(t, worktree); got != want {
		t.Errorf("worktree path = %q, want %q", got, want)
	}
	if recorder.Worktree.Branch != "feature" {
		t.Errorf("worktree branch = %q, want feature", recorder.Worktree.Branch)
	}

	if got, want := mustGit(t, recorder.Placeholder, "rev-parse", "--show-toplevel"), canonical(t, worktree); got != want {
		t.Errorf("placeholder toplevel = %q, want %q", got, want)
	}
	if got, want := mustGit(t, recorder.Placeholder, "config", "--worktree", "core.worktree"), canonical(t, worktree); got != want {
		t.Errorf("core.worktree = %q, want %q", got, want)
	}
	if got, want := mustGit(t, recorder.Placeholder, "rev-parse", "--abbrev-ref", "HEAD"), recorder.Worktree.HistoryBranch; got != want {
		t.Errorf("history branch = %q, want %q", got, want)
	}
	if !strings.HasPrefix(recorder.Project.Dir, "project-") {
		t.Errorf("project dir = %q, want project-<hash>", recorder.Project.Dir)
	}
	if !strings.HasSuffix(recorder.Project.Dir, recorder.Project.ID[:8]) {
		t.Errorf("project dir %q does not end in project hash %q", recorder.Project.Dir, recorder.Project.ID[:8])
	}
	if !strings.HasPrefix(recorder.Worktree.Dir, "worktree-") {
		t.Errorf("worktree dir = %q, want worktree-<hash>", recorder.Worktree.Dir)
	}
	if !strings.HasSuffix(recorder.Worktree.Dir, recorder.Worktree.ID[:8]) {
		t.Errorf("worktree dir %q does not end in worktree hash %q", recorder.Worktree.Dir, recorder.Worktree.ID[:8])
	}
	if got, want := recorder.Worktree.HistoryBranch, "wt/"+recorder.Worktree.Dir; got != want {
		t.Errorf("history branch = %q, want %q", got, want)
	}
	if got, want := filepath.Base(filepath.Dir(recorder.HistoryRepo)), recorder.Project.Dir; got != want {
		t.Errorf("history project dir = %q, want %q", got, want)
	}
	if got, want := filepath.Base(recorder.Placeholder), recorder.Worktree.Dir; got != want {
		t.Errorf("placeholder dir = %q, want %q", got, want)
	}

	projectHead := mustGit(t, project, "rev-parse", "HEAD")
	projectBranch := mustGit(t, project, "branch", "--show-current")

	first, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}
	if first.Hash == "" {
		t.Fatal("first snapshot has no hash")
	}
	if !strings.HasPrefix(first.Message, "snap: ") {
		t.Errorf("snapshot message = %q, want snap: prefix", first.Message)
	}
	if !strings.Contains(first.Message, "[feature@") {
		t.Errorf("snapshot message = %q, want project context", first.Message)
	}
	if first.Stats != nil {
		t.Errorf("initial snapshot stats = %+v, want nil", first.Stats)
	}

	files := mustGit(t, recorder.Placeholder, "ls-files")
	if !strings.Contains(files, "a.txt") || !strings.Contains(files, ".gitignore") {
		t.Errorf("snapshot files missing expected paths:\n%s", files)
	}
	if strings.Contains(files, "node_modules") {
		t.Errorf("snapshot contains ignored files:\n%s", files)
	}
	if status := mustGit(t, recorder.Placeholder, "status", "--porcelain"); status != "" {
		t.Errorf("history worktree is dirty after snapshot:\n%s", status)
	}

	if _, err := recorder.Snapshot(ctx); !errors.Is(err, ErrNoChanges) {
		t.Fatalf("second Snapshot error = %v, want ErrNoChanges", err)
	}

	writeFile(t, filepath.Join(worktree, "a.txt"), "three\n")
	second, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("second changed Snapshot: %v", err)
	}
	if second.Hash == first.Hash {
		t.Errorf("snapshot hash did not change: %s", second.Hash)
	}
	if second.Stats == nil {
		t.Fatal("second snapshot stats missing")
	}
	if second.Stats.Files != 1 || second.Stats.Added != 1 || second.Stats.Deleted != 1 {
		t.Errorf("second snapshot stats = %+v, want 1 file changed, +1 -1", second.Stats)
	}

	if got := mustGit(t, project, "rev-parse", "HEAD"); got != projectHead {
		t.Errorf("project HEAD changed: %q -> %q", projectHead, got)
	}
	if got := mustGit(t, project, "branch", "--show-current"); got != projectBranch {
		t.Errorf("project branch changed: %q -> %q", projectBranch, got)
	}

	summary, err := recorder.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Snapshots != 2 {
		t.Errorf("summary snapshots = %d, want 2", summary.Snapshots)
	}
	if summary.LastHash != second.Hash {
		t.Errorf("summary last hash = %q, want %q", summary.LastHash, second.Hash)
	}

	if _, err := store.Ensure(ctx, worktree); err != nil {
		t.Fatalf("Ensure is not idempotent: %v", err)
	}

	known, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(known) != 1 {
		t.Fatalf("known worktrees = %d, want 1: %+v", len(known), known)
	}
	if got, want := known[0].Path, canonical(t, worktree); got != want {
		t.Errorf("known path = %q, want %q", got, want)
	}
	if known[0].ProjectDir != recorder.Project.Dir {
		t.Errorf("known project dir = %q, want %q", known[0].ProjectDir, recorder.Project.Dir)
	}
	if known[0].CommonDir != recorder.Project.CommonDir {
		t.Errorf("known common dir = %q, want %q", known[0].CommonDir, recorder.Project.CommonDir)
	}
	if known[0].WorktreeDir != recorder.Worktree.Dir {
		t.Errorf("known worktree dir = %q, want %q", known[0].WorktreeDir, recorder.Worktree.Dir)
	}
	if known[0].HistoryBranch != recorder.Worktree.HistoryBranch {
		t.Errorf("known history branch = %q, want %q", known[0].HistoryBranch, recorder.Worktree.HistoryBranch)
	}
}

func TestSnapshotExcludesNestedRepositoryWithoutCommit(t *testing.T) {
	ctx := context.Background()
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

	nested := filepath.Join(worktree, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, nested, "init", "-b", "main")

	recorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	snapshot, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.Hash == "" {
		t.Fatal("snapshot has no hash")
	}

	files := mustGit(t, recorder.Placeholder, "ls-files")
	if strings.Contains(files, "nested") {
		t.Errorf("history contains the nested repository:\n%s", files)
	}
	if !strings.Contains(files, "a.txt") {
		t.Errorf("history is missing a.txt:\n%s", files)
	}

	exclude, err := os.ReadFile(filepath.Join(recorder.HistoryRepo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exclude), "nested/") {
		t.Errorf("nested repository was not excluded:\n%s", exclude)
	}
}

func TestListSkipsLegacyProjectDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "3c0903a692ef", "worktrees", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	known, err := New(root).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(known) != 0 {
		t.Fatalf("known worktrees = %d, want 0: %+v", len(known), known)
	}
}

func TestReadWorktreeConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.worktree")
	content := `[core]
	worktree = /tmp/project
[local-history]
	worktree = /tmp/project
	branch = main
	historyBranch = wt/project-12345678
	setupHash = abc123
	setupAt = 2026-11-06T08:00:00Z
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	values := readWorktreeConfig(path)
	if got, want := values["local-history.worktree"], "/tmp/project"; got != want {
		t.Errorf("worktree = %q, want %q", got, want)
	}
	if got, want := values["local-history.branch"], "main"; got != want {
		t.Errorf("branch = %q, want %q", got, want)
	}
	if got, want := values["local-history.historyBranch"], "wt/project-12345678"; got != want {
		t.Errorf("history branch = %q, want %q", got, want)
	}
	if got, want := values["local-history.setupHash"], "abc123"; got != want {
		t.Errorf("setup hash = %q, want %q", got, want)
	}
	if got, want := values["core.worktree"], "/tmp/project"; got != want {
		t.Errorf("core worktree = %q, want %q", got, want)
	}
}

func TestRemoveWorktreeDropsPlaceholderAndBranch(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktrees := []string{filepath.Join(base, "worktree-a"), filepath.Join(base, "worktree-b")}
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
	for i, worktree := range worktrees {
		mustGit(t, project, "worktree", "add", "-b", "branch-"+strconv.Itoa(i), worktree)
		if _, err := store.Ensure(ctx, worktree); err != nil {
			t.Fatalf("Ensure %s: %v", worktree, err)
		}
	}

	known, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(known) != 2 {
		t.Fatalf("known entries = %d, want 2", len(known))
	}
	var removed, kept KnownWorktree
	for _, entry := range known {
		if entry.Path == worktrees[0] {
			removed = entry
		} else if entry.Path == worktrees[1] {
			kept = entry
		}
	}
	if removed.WorktreeDir == "" || kept.WorktreeDir == "" {
		t.Fatalf("could not identify worktrees: %+v", known)
	}

	if err := os.RemoveAll(worktrees[0]); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveWorktree(ctx, removed); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	repo := filepath.Join(store.Root, removed.ProjectDir, "repo")
	if branches := mustGit(t, repo, "branch", "--list", removed.HistoryBranch); strings.TrimSpace(branches) != "" {
		t.Errorf("removed worktree's branch %q still exists: %q", removed.HistoryBranch, branches)
	}
	if branches := mustGit(t, repo, "branch", "--list", kept.HistoryBranch); !strings.Contains(branches, kept.HistoryBranch) {
		t.Errorf("kept worktree's branch %q was removed", kept.HistoryBranch)
	}
	placeholder := filepath.Join(store.Root, removed.ProjectDir, "worktrees", removed.WorktreeDir)
	if _, err := os.Stat(placeholder); !os.IsNotExist(err) {
		t.Errorf("placeholder still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, removed.ProjectDir)); err != nil {
		t.Errorf("project store disappeared while a worktree remains: %v", err)
	}

	remaining, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List after removal: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Path != worktrees[1] {
		t.Errorf("remaining entries = %+v, want only %s", remaining, worktrees[1])
	}
}

func TestRemoveLastWorktreeDropsProjectStore(t *testing.T) {
	ctx := context.Background()
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
	writeFile(t, filepath.Join(worktree, "a.txt"), "changed\n")
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	known, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(known) != 1 {
		t.Fatalf("known entries = %d, want 1", len(known))
	}

	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveWorktree(ctx, known[0]); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(store.Root, recorder.Project.Dir)); !os.IsNotExist(err) {
		t.Errorf("project store still exists: %v", err)
	}
	remaining, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List after removal: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("known entries after removal = %d, want 0", len(remaining))
	}
}

func TestListInheritsCommonDirForDeletedWorktree(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktrees := []string{filepath.Join(base, "worktree-a"), filepath.Join(base, "worktree-b")}
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
	for i, worktree := range worktrees {
		mustGit(t, project, "worktree", "add", "-b", "branch-"+strconv.Itoa(i), worktree)
		if _, err := store.Ensure(ctx, worktree); err != nil {
			t.Fatalf("Ensure %s: %v", worktree, err)
		}
	}

	if err := os.RemoveAll(worktrees[1]); err != nil {
		t.Fatal(err)
	}

	known, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(known) != 2 {
		t.Fatalf("known entries = %d, want 2", len(known))
	}
	want := known[0].CommonDir
	if want == "" {
		t.Fatal("live worktree has no CommonDir")
	}
	for _, entry := range known {
		if entry.CommonDir != want {
			t.Errorf("CommonDir for %s = %q, want %q", entry.Path, entry.CommonDir, want)
		}
	}
}

func TestSlugOr(t *testing.T) {
	cases := map[string]string{
		"thought-keeper":            "thought-keeper",
		"Thought Keeper":            "thought-keeper",
		"thought-keeper--feature/x": "thought-keeper-feature-x",
		"  multiple   separators  ": "multiple-separators",
		"---":                       "fallback",
		"":                          "fallback",
	}
	for input, want := range cases {
		if got := slugOr(input, "fallback"); got != want {
			t.Errorf("slugOr(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSnapshotStatsAreCumulative(t *testing.T) {
	recorder, worktree := newRevertRecorder(t)
	ctx := t.Context()

	writeFile(t, filepath.Join(worktree, "a.txt"), "one\n")
	writeFile(t, filepath.Join(worktree, "b.txt"), "one\n")
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}

	writeFile(t, filepath.Join(worktree, "a.txt"), "two\n")
	if _, err := recorder.Snapshot(ctx); err != nil {
		t.Fatalf("first snapshot: %v", err)
	}

	writeFile(t, filepath.Join(worktree, "b.txt"), "one\ntwo\nthree\n")
	snapshot, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if snapshot.Stats == nil {
		t.Fatal("cumulative stats missing")
	}
	if snapshot.Stats.Files != 2 || snapshot.Stats.Added != 3 || snapshot.Stats.Deleted != 1 {
		t.Errorf("cumulative stats = %+v, want 2 files, +3 -1", snapshot.Stats)
	}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
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

func canonical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestInitializationStateWaitsForFirstFiles(t *testing.T) {
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
	mustGit(t, project, "commit", "--allow-empty", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	recorder, err := store.Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !recorder.Initializing() {
		t.Fatal("a fresh worktree did not start in initialization")
	}

	known, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, entry := range known {
		if filepath.Clean(entry.Path) != filepath.Clean(worktree) {
			continue
		}
		found = true
		if entry.InitAt == "" || entry.InitDone {
			t.Errorf("known init state = %q/%v, want an open initialization", entry.InitAt, entry.InitDone)
		}
		if !store.RecorderFromKnown(entry).Initializing() {
			t.Error("RecorderFromKnown lost the open initialization")
		}
	}
	if !found {
		t.Fatal("List did not return the worktree")
	}

	// A live Git index lock means the checkout is still running. The recorder
	// must defer instead of staging a half-finished tree.
	lock := filepath.Join(gitDirForWorktree(worktree), "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Snapshot(ctx); !errors.Is(err, ErrWorktreeBusy) {
		t.Fatalf("locked snapshot error = %v, want ErrWorktreeBusy", err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}

	// The worktree is still empty because the checkout has not produced files
	// yet. That must not be mistaken for a completed initialization.
	if _, err := recorder.Snapshot(ctx); !errors.Is(err, ErrWorktreeBusy) {
		t.Fatalf("empty snapshot error = %v, want ErrWorktreeBusy", err)
	}
	if !recorder.Initializing() {
		t.Fatal("empty snapshot closed the initialization window")
	}

	writeFile(t, filepath.Join(worktree, "a.txt"), "one\n")
	first, err := recorder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	if !first.Initial {
		t.Fatal("first snapshot is not part of the initialization")
	}
	if _, err := recorder.Snapshot(ctx); !errors.Is(err, ErrNoChanges) {
		t.Fatalf("quiet snapshot error = %v, want ErrNoChanges", err)
	}
	if recorder.Initializing() {
		t.Fatal("quiet check did not close the initialization window")
	}
}
