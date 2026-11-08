package app

import (
	"testing"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/history"
)

func TestSortWorktreesOrdersByActivity(t *testing.T) {
	worktrees := []WorktreeStatus{
		{Path: "/repo/old", LastSnapshot: "2024-01-01T10:00:00Z"},
		{Path: "/repo/main", LastSnapshot: "2023-01-01T10:00:00Z"},
		{Path: "/repo/new", LastSnapshot: "2024-02-01T10:00:00Z"},
		{Path: "/repo/none"},
		{Path: "/repo/alpha", LastSnapshot: "2024-01-01T10:00:00Z"},
	}

	sortWorktrees(worktrees, "/repo/main")

	want := []string{
		"/repo/main",
		"/repo/new",
		"/repo/alpha",
		"/repo/old",
		"/repo/none",
	}
	for i, path := range want {
		if worktrees[i].Path != path {
			t.Fatalf("worktrees[%d] = %q, want %q", i, worktrees[i].Path, path)
		}
	}
}

func TestProjectsSnapshotAddsStatsToLinkedWorktrees(t *testing.T) {
	stats := &history.ChangeStats{Files: 2, Added: 12, Deleted: 3}
	at := time.Now().UTC().Format(time.RFC3339)
	a := &App{
		projects: []ProjectStatus{{
			Name:         "project",
			MainWorktree: "/repo/main",
			Worktrees: []WorktreeStatus{
				{Path: "/repo/main", LastSnapshot: at},
				{Path: "/repo/feature", LastSnapshot: at},
				{Path: "/repo/no-stats", LastSnapshot: at},
			},
		}},
		activity: map[string]Activity{
			"/repo/main":    {Time: at, Kind: "snapshot", Path: "/repo/main", Stats: stats},
			"/repo/feature": {Time: at, Kind: "snapshot", Path: "/repo/feature", Stats: stats},
		},
	}

	projects := a.projectsSnapshot()
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
	for _, worktree := range projects[0].Worktrees {
		switch worktree.Path {
		case "/repo/main":
			if worktree.Stats != nil {
				t.Errorf("main worktree stats = %+v, want nil", worktree.Stats)
			}
		case "/repo/feature":
			if worktree.Stats == nil || worktree.Stats.Added != 12 || worktree.Stats.Deleted != 3 {
				t.Errorf("linked worktree stats = %+v, want +12 -3", worktree.Stats)
			}
		case "/repo/no-stats":
			if worktree.Stats != nil {
				t.Errorf("worktree without activity stats = %+v, want nil", worktree.Stats)
			}
		}
	}
}
