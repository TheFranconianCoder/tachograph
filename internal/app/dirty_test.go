package app

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWorktreeSourceState(t *testing.T) {
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

	state, err := worktreeSourceState(ctx, dir)
	if err != nil {
		t.Fatalf("worktreeSourceState(clean): %v", err)
	}
	if state.Dirty {
		t.Error("clean worktree reported dirty")
	}

	testWrite(t, filepath.Join(dir, "tracked.txt"), "two\n")
	state, err = worktreeSourceState(ctx, dir)
	if err != nil {
		t.Fatalf("worktreeSourceState(tracked): %v", err)
	}
	if !state.Dirty {
		t.Error("modified tracked file did not mark the worktree dirty")
	}

	testGit(t, dir, "checkout", "--", "tracked.txt")
	testWrite(t, filepath.Join(dir, "new.txt"), "new\n")
	state, err = worktreeSourceState(ctx, dir)
	if err != nil {
		t.Fatalf("worktreeSourceState(untracked): %v", err)
	}
	if !state.Dirty {
		t.Error("untracked file did not mark the worktree dirty")
	}

	testGit(t, dir, "add", "-A")
	testGit(t, dir, "commit", "-m", "add untracked")
	testWrite(t, filepath.Join(dir, "ignored", "secret.txt"), "ignored\n")
	state, err = worktreeSourceState(ctx, dir)
	if err != nil {
		t.Fatalf("worktreeSourceState(ignored): %v", err)
	}
	if state.Dirty {
		t.Error("ignored file marked the worktree dirty")
	}
}

func TestWorktreeSourceStateTracksUpstream(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	remote := filepath.Join(root, "remote.git")
	testGit(t, root, "init", "-q", "--bare", remote)

	seed := filepath.Join(root, "seed")
	testGit(t, root, "init", "-q", "-b", "main", seed)
	configureTestGit(t, seed)
	testWrite(t, filepath.Join(seed, "tracked.txt"), "one\n")
	testGit(t, seed, "add", "-A")
	testGit(t, seed, "commit", "-q", "-m", "one")
	testGit(t, seed, "remote", "add", "origin", remote)
	testGit(t, seed, "push", "-q", "-u", "origin", "main")

	work := filepath.Join(root, "work")
	testGit(t, root, "clone", "-q", "-b", "main", remote, work)
	configureTestGit(t, work)

	state, err := worktreeSourceState(ctx, work)
	if err != nil {
		t.Fatalf("worktreeSourceState(in sync): %v", err)
	}
	if state.Upstream != "origin/main" || state.Ahead != 0 || state.Behind != 0 {
		t.Errorf("in-sync state = %+v, want origin/main ahead 0 behind 0", state)
	}

	testWrite(t, filepath.Join(work, "tracked.txt"), "two\n")
	testGit(t, work, "commit", "-qam", "local")
	state, err = worktreeSourceState(ctx, work)
	if err != nil {
		t.Fatalf("worktreeSourceState(ahead): %v", err)
	}
	if state.Ahead != 1 || state.Behind != 0 {
		t.Errorf("ahead state = %+v, want ahead 1 behind 0", state)
	}

	other := filepath.Join(root, "other")
	testGit(t, root, "clone", "-q", "-b", "main", remote, other)
	configureTestGit(t, other)
	testWrite(t, filepath.Join(other, "tracked.txt"), "remote\n")
	testGit(t, other, "commit", "-qam", "remote")
	testGit(t, other, "push", "-q")
	testGit(t, work, "fetch", "-q")

	state, err = worktreeSourceState(ctx, work)
	if err != nil {
		t.Fatalf("worktreeSourceState(diverged): %v", err)
	}
	if state.Ahead != 1 || state.Behind != 1 {
		t.Errorf("diverged state = %+v, want ahead 1 behind 1", state)
	}
}

func TestParseSourceState(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   sourceState
	}{
		{
			name:   "in sync",
			output: "# branch.oid abc\n# branch.head main\n# branch.upstream origin/main\n# branch.ab +0 -0\n",
			want:   sourceState{Upstream: "origin/main"},
		},
		{
			name:   "ahead and behind",
			output: "# branch.head main\n# branch.upstream origin/main\n# branch.ab +2 -3\n",
			want:   sourceState{Upstream: "origin/main", Ahead: 2, Behind: 3},
		},
		{
			name:   "dirty tracked and untracked",
			output: "# branch.head main\n1 .M N... 100644 100644 100644 abc abc tracked.txt\n? new.txt\n",
			want:   sourceState{Dirty: true},
		},
		{
			name:   "detached head",
			output: "# branch.oid abc\n# branch.head (detached)\n",
			want:   sourceState{},
		},
		{
			name:   "upstream ref gone",
			output: "# branch.head main\n# branch.upstream origin/main\n",
			want:   sourceState{Upstream: "origin/main"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseSourceState(test.output); got != test.want {
				t.Errorf("parseSourceState() = %+v, want %+v", got, test.want)
			}
		})
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

func configureTestGit(t *testing.T, dir string) {
	t.Helper()
	testGit(t, dir, "config", "user.name", "Test")
	testGit(t, dir, "config", "user.email", "test@example.com")
	testGit(t, dir, "config", "commit.gpgsign", "false")
}
