package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
		dirty: newDirtyState(),
	}
	a.dirty.set("/repo/main", sourceState{Dirty: true})
	a.dirty.set("/repo/feature", sourceState{Dirty: true})

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
			if !worktree.Dirty {
				t.Error("main worktree dirty = false, want true")
			}
		case "/repo/feature":
			if worktree.Stats == nil || worktree.Stats.Added != 12 || worktree.Stats.Deleted != 3 {
				t.Errorf("linked worktree stats = %+v, want +12 -3", worktree.Stats)
			}
			if !worktree.Dirty {
				t.Error("linked worktree dirty = false, want true")
			}
		case "/repo/no-stats":
			if worktree.Stats != nil {
				t.Errorf("worktree without activity stats = %+v, want nil", worktree.Stats)
			}
			if worktree.Dirty {
				t.Error("clean worktree dirty = true, want false")
			}
		}
	}
}

func TestActiveProject(t *testing.T) {
	tests := []struct {
		name    string
		project ProjectStatus
		want    bool
	}{
		{
			name:    "clean main worktree only",
			project: ProjectStatus{MainWorktree: "/repo", Worktrees: []WorktreeStatus{{Path: "/repo"}}},
		},
		{
			name: "dirty worktree",
			project: ProjectStatus{
				MainWorktree: "/repo",
				Worktrees:    []WorktreeStatus{{Path: "/repo", Dirty: true}},
			},
			want: true,
		},
		{
			name: "ahead of upstream",
			project: ProjectStatus{
				MainWorktree: "/repo",
				Worktrees:    []WorktreeStatus{{Path: "/repo", Ahead: 1}},
			},
			want: true,
		},
		{
			name: "behind upstream",
			project: ProjectStatus{
				MainWorktree: "/repo",
				Worktrees:    []WorktreeStatus{{Path: "/repo", Behind: 2}},
			},
			want: true,
		},
		{
			name: "linked worktree",
			project: ProjectStatus{
				MainWorktree: "/repo",
				Worktrees:    []WorktreeStatus{{Path: "/repo"}, {Path: "/repo-feature"}},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := activeProject(test.project); got != test.want {
				t.Errorf("activeProject() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestUIProjectsSnapshotFiltersInactive(t *testing.T) {
	a := &App{
		cfg: Config{UI: UIConfig{ActiveOnly: true}},
		projects: []ProjectStatus{
			{Name: "quiet", MainWorktree: "/quiet", Worktrees: []WorktreeStatus{{Path: "/quiet"}}},
			{Name: "dirty", MainWorktree: "/dirty", Worktrees: []WorktreeStatus{{Path: "/dirty"}}},
			{
				Name:         "linked",
				MainWorktree: "/linked",
				Worktrees:    []WorktreeStatus{{Path: "/linked"}, {Path: "/linked-feature"}},
			},
		},
		dirty: newDirtyState(),
	}
	a.dirty.set("/dirty", sourceState{Dirty: true})

	visible := a.uiProjectsSnapshot()
	if len(visible) != 2 {
		t.Fatalf("uiProjectsSnapshot() = %d projects, want 2", len(visible))
	}
	if visible[0].Name != "dirty" || visible[1].Name != "linked" {
		t.Errorf("uiProjectsSnapshot() names = %q, %q; want dirty, linked", visible[0].Name, visible[1].Name)
	}
	if all := a.projectsSnapshot(); len(all) != 3 {
		t.Errorf("projectsSnapshot() = %d projects, want all 3", len(all))
	}
}

func TestHandleActiveOnlyPersistsConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	a := &App{cfg: DefaultConfig(), cfgPath: path, dirty: newDirtyState()}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/projects/active-only",
		strings.NewReader(`{"enabled":true}`))
	response := httptest.NewRecorder()
	a.handleActiveOnly(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !a.activeOnlyFilter() {
		t.Error("in-memory filter = false, want true")
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !loaded.UI.ActiveOnly {
		t.Error("persisted filter = false, want true")
	}
}

func TestVisibleActivityDropsHiddenWorktrees(t *testing.T) {
	projects := []ProjectStatus{{
		MainWorktree: "/visible",
		Worktrees:    []WorktreeStatus{{Path: "/visible"}},
	}}
	activity := []Activity{
		{Kind: "snapshot", Path: "/visible"},
		{Kind: "snapshot", Path: "/hidden"},
		{Kind: "error", Message: "global"},
	}

	filtered := visibleActivity(activity, projects)
	if len(filtered) != 2 {
		t.Fatalf("visibleActivity() = %d entries, want 2", len(filtered))
	}
	if filtered[0].Path != "/visible" {
		t.Errorf("first entry path = %q, want /visible", filtered[0].Path)
	}
	if filtered[1].Path != "" || filtered[1].Message != "global" {
		t.Errorf("global entry = %+v, want the pathless message", filtered[1])
	}
}

func TestStatusAppliesActiveOnlyToProjectsAndActivity(t *testing.T) {
	a := &App{
		cfg: Config{UI: UIConfig{ActiveOnly: true}},
		projects: []ProjectStatus{
			{Name: "quiet", MainWorktree: "/quiet", Worktrees: []WorktreeStatus{{Path: "/quiet"}}},
			{
				Name:         "linked",
				MainWorktree: "/linked",
				Worktrees:    []WorktreeStatus{{Path: "/linked"}, {Path: "/linked-feature"}},
			},
		},
		activity: map[string]Activity{
			"/quiet":  {Kind: "snapshot", Path: "/quiet"},
			"/linked": {Kind: "snapshot", Path: "/linked"},
		},
		dirty: newDirtyState(),
	}

	status := a.status()
	if !status.ActiveOnly {
		t.Error("status.ActiveOnly = false, want true")
	}
	if len(status.Projects) != 1 || status.Projects[0].Name != "linked" {
		t.Errorf("status.Projects = %+v, want only linked", status.Projects)
	}
	if len(status.Activity) != 1 || status.Activity[0].Path != "/linked" {
		t.Errorf("status.Activity = %+v, want only /linked", status.Activity)
	}
}
