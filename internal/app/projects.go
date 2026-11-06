package app

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
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

	// Projects are ordered by activity: the newest snapshot first. Inside a
	// project the main worktree is pinned to the top, the others follow
	// alphabetically by directory name.
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

// sortWorktrees pins the main worktree to the first position and orders the
// remaining worktrees by directory name, case-insensitively.
func sortWorktrees(worktrees []WorktreeStatus, mainWorktree string) {
	sort.SliceStable(worktrees, func(left, right int) bool {
		a := worktrees[left]
		b := worktrees[right]
		aMain := samePath(a.Path, mainWorktree)
		bMain := samePath(b.Path, mainWorktree)
		if aMain != bMain {
			return aMain
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
