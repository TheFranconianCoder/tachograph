package app

import "testing"

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
