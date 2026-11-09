package app

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/history"
)

// ProjectStatus is the UI/API view of a discovered project.
type ProjectStatus struct {
	Name         string           `json:"name"`
	CommonDir    string           `json:"commonDir"`
	MainWorktree string           `json:"mainWorktree"`
	HistoryRepo  string           `json:"historyRepo,omitempty"`
	LastChange   string           `json:"lastChange,omitempty"`
	Worktrees    []WorktreeStatus `json:"worktrees"`
}

// WorktreeStatus is the UI/API view of one worktree. State is one of "ready",
// "error" or "setup-error"; watcher state arrives with M4. Setup is the human
// label of the last lifecycle action, SetupState its UI signal ("ok", "error",
// "none").
type WorktreeStatus struct {
	ID           string `json:"id,omitempty"`
	Path         string `json:"path"`
	Branch       string `json:"branch"`
	Head         string `json:"head"`
	HistoryRepo  string `json:"historyRepo,omitempty"`
	Placeholder  string `json:"placeholder,omitempty"`
	State        string `json:"state"`
	Setup        string `json:"setup,omitempty"`
	SetupState   string `json:"setupState,omitempty"`
	Error        string `json:"error,omitempty"`
	Watcher      string `json:"watcher,omitempty"`
	LastSnapshot string `json:"lastSnapshot,omitempty"`
	Snapshots    int    `json:"snapshots,omitempty"`
	IntervalMs   int    `json:"intervalMs,omitempty"`
	WatcherError string `json:"watcherError,omitempty"`
	// Dirty describes the source checkout, not the snapshot store: git status
	// reports uncommitted or untracked changes against HEAD.
	Dirty bool `json:"dirty,omitempty"`
	// Upstream, Ahead and Behind describe the branch position against the
	// configured upstream. The counts compare local commits and remote-tracking
	// refs, so they reflect the last fetch; Tachograph never fetches.
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
	// Stats describe the cumulative change since initialization. Linked
	// worktrees show them in the project list; the main worktree stays without
	// numbers because it is the source tree.
	Stats *history.ChangeStats `json:"stats,omitempty"`
}

func (a *App) projectsSnapshot() []ProjectStatus {
	a.mu.RLock()
	projects := make([]ProjectStatus, len(a.projects))
	for i, project := range a.projects {
		worktrees := make([]WorktreeStatus, len(project.Worktrees))
		copy(worktrees, project.Worktrees)
		project.Worktrees = worktrees
		projects[i] = project
	}
	a.mu.RUnlock()

	if a.supervisor != nil {
		for i := range projects {
			for j := range projects[i].Worktrees {
				status, ok := a.supervisor.Status(projects[i].Worktrees[j].Path)
				if !ok {
					continue
				}
				projects[i].Worktrees[j].Watcher = status.State
				projects[i].Worktrees[j].LastSnapshot = status.LastSnapshot
				projects[i].Worktrees[j].Snapshots = status.Snapshots
				projects[i].Worktrees[j].IntervalMs = status.IntervalMs
				projects[i].Worktrees[j].WatcherError = status.Error
			}
		}
	}

	// Linked worktrees show the same +/- numbers as the activity table. The
	// main checkout is the source tree and deliberately stays without stats.
	stats := a.worktreeStats()
	source := a.dirty.snapshot()
	for i := range projects {
		for j := range projects[i].Worktrees {
			worktree := &projects[i].Worktrees[j]
			state := source[filepath.Clean(worktree.Path)]
			worktree.Dirty = state.Dirty
			worktree.Upstream = state.Upstream
			worktree.Ahead = state.Ahead
			worktree.Behind = state.Behind
			if samePath(worktree.Path, projects[i].MainWorktree) {
				continue
			}
			value, ok := stats[filepath.Clean(worktree.Path)]
			if !ok || value == nil {
				continue
			}
			copied := *value
			worktree.Stats = &copied
		}
	}

	// Projects are ordered by activity: the newest snapshot first. Inside a
	// project the main worktree is pinned to the top, the others follow by
	// activity: the newest snapshot first.
	for i := range projects {
		projects[i].LastChange = newestSnapshot(projects[i].Worktrees)
		sortWorktrees(projects[i].Worktrees, projects[i].MainWorktree)
	}
	sort.SliceStable(projects, func(left, right int) bool {
		a := snapshotTime(projects[left].LastChange)
		b := snapshotTime(projects[right].LastChange)
		if !a.Equal(b) {
			return a.After(b)
		}
		if projects[left].Name == projects[right].Name {
			return projects[left].CommonDir < projects[right].CommonDir
		}
		return projects[left].Name < projects[right].Name
	})

	return projects
}

// uiProjectsSnapshot is the project list the UI receives. When the Active only
// filter is enabled, projects without relevant worktree state are left out of
// the payload entirely instead of being hidden client-side.
func (a *App) uiProjectsSnapshot() []ProjectStatus {
	projects := a.projectsSnapshot()
	if !a.activeOnlyFilter() {
		return projects
	}
	filtered := make([]ProjectStatus, 0, len(projects))
	for _, project := range projects {
		if activeProject(project) {
			filtered = append(filtered, project)
		}
	}
	return filtered
}

func (a *App) activeOnlyFilter() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.UI.ActiveOnly
}

// activeProject reports whether a project has uncommitted changes, upstream
// differences, or at least one linked worktree besides the main checkout.
func activeProject(project ProjectStatus) bool {
	for _, worktree := range project.Worktrees {
		if worktree.Dirty || worktree.Ahead > 0 || worktree.Behind > 0 {
			return true
		}
		if !samePath(worktree.Path, project.MainWorktree) {
			return true
		}
	}
	return false
}

// visibleActivity drops recorder events for worktrees that the Active only
// filter keeps out of the payload. Entries without a path are global messages
// and stay.
func visibleActivity(activity []Activity, projects []ProjectStatus) []Activity {
	visible := make(map[string]bool)
	for _, project := range projects {
		for _, worktree := range project.Worktrees {
			visible[filepath.Clean(worktree.Path)] = true
		}
	}
	filtered := make([]Activity, 0, len(activity))
	for _, entry := range activity {
		if entry.Path == "" || visible[filepath.Clean(entry.Path)] {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// worktreeStats returns the newest cumulative diffstats per worktree. The
// activity map already holds exactly the values shown in the activity table.
func (a *App) worktreeStats() map[string]*history.ChangeStats {
	a.activityMu.Lock()
	defer a.activityMu.Unlock()
	stats := make(map[string]*history.ChangeStats, len(a.activity))
	for path, entry := range a.activity {
		if entry.Stats != nil {
			stats[filepath.Clean(path)] = entry.Stats
		}
	}
	return stats
}

// sortWorktrees pins the main worktree to the first position and orders the
// remaining worktrees by activity: the newest snapshot first. Worktrees
// without a snapshot follow the active ones, alphabetically by directory name.
func sortWorktrees(worktrees []WorktreeStatus, mainWorktree string) {
	sort.SliceStable(worktrees, func(left, right int) bool {
		a := worktrees[left]
		b := worktrees[right]
		aMain := samePath(a.Path, mainWorktree)
		bMain := samePath(b.Path, mainWorktree)
		if aMain != bMain {
			return aMain
		}
		aTime := snapshotTime(a.LastSnapshot)
		bTime := snapshotTime(b.LastSnapshot)
		if !aTime.Equal(bTime) {
			return aTime.After(bTime)
		}
		aName := strings.ToLower(filepath.Base(a.Path))
		bName := strings.ToLower(filepath.Base(b.Path))
		if aName != bName {
			return aName < bName
		}
		return a.Path < b.Path
	})
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

// newestSnapshot returns the most recent snapshot timestamp of a worktree set.
func newestSnapshot(worktrees []WorktreeStatus) string {
	newest := ""
	newestTime := time.Time{}
	for _, worktree := range worktrees {
		parsed := snapshotTime(worktree.LastSnapshot)
		if parsed.IsZero() {
			continue
		}
		if newest == "" || parsed.After(newestTime) {
			newest = worktree.LastSnapshot
			newestTime = parsed
		}
	}
	return newest
}

func snapshotTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func (a *App) rootsSnapshot() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	roots := make([]string, len(a.cfg.Roots))
	copy(roots, a.cfg.Roots)
	return roots
}
