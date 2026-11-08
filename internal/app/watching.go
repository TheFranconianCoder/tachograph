package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/history"
	"github.com/TheFranconianCoder/tachograph/internal/recorder"
)

const (
	activityFlushInterval = 15 * time.Second
	hibernateTickInterval = 15 * time.Second
	activityMaxAge        = 30 * 24 * time.Hour
	maxActivity           = 50
	activityStateVersion  = 4
)

// Activity is one recent recorder event shown in the UI.
type Activity struct {
	Time    string               `json:"time"`
	Kind    string               `json:"kind"`
	Path    string               `json:"path,omitempty"`
	Message string               `json:"message,omitempty"`
	Hash    string               `json:"hash,omitempty"`
	Stats   *history.ChangeStats `json:"stats,omitempty"`
}

// watchGroup is the watcher state of one project. All worktrees of a project
// share one watchexec process.
type watchGroup struct {
	key        string
	origin     string
	paths      []string
	hibernated bool
	// graceUntil keeps a watcher open for a short window after app start even
	// when the persisted activity is already older than the idle threshold.
	graceUntil time.Time
}

// recordRecorderEvent stores the newest event per worktree and wakes a
// hibernated watcher when a snapshot was written.
func (a *App) recordRecorderEvent(event recorder.Event) {
	entry := Activity{
		Time:    event.Time.UTC().Format(time.RFC3339),
		Kind:    event.Kind,
		Path:    event.Path,
		Message: event.Message,
		Hash:    event.Hash,
		Stats:   event.Stats,
	}

	a.activityMu.Lock()
	if a.activity == nil {
		a.activity = make(map[string]Activity)
	}
	a.activity[event.Path] = entry
	a.activityMu.Unlock()

	a.watchMu.Lock()
	a.stateDirty = true
	a.watchMu.Unlock()

	if event.Kind != "snapshot" {
		a.notifyStatus()
		return
	}
	a.markActive(event.Path, event.Time)
	a.wakeWatcher(event.Path)
	a.notifyStatus()
}

// activitySnapshot returns the newest event per worktree, newest first.
func (a *App) activitySnapshot() []Activity {
	a.activityMu.Lock()
	activity := make([]Activity, 0, len(a.activity))
	for _, entry := range a.activity {
		activity = append(activity, entry)
	}
	a.activityMu.Unlock()

	mainWorktrees := a.mainWorktreePaths()
	for i := range activity {
		for _, main := range mainWorktrees {
			if samePath(main, activity[i].Path) {
				// Diffstats describe a worktree change. The main checkout is
				// the source tree, so its activity stays without numbers.
				activity[i].Stats = nil
				break
			}
		}
	}

	sort.Slice(activity, func(left, right int) bool {
		return activity[left].Time > activity[right].Time
	})
	if len(activity) > maxActivity {
		activity = activity[:maxActivity]
	}
	return activity
}

// mainWorktreePaths returns the main checkout of every known project.
func (a *App) mainWorktreePaths() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	paths := make([]string, 0, len(a.projects))
	for _, project := range a.projects {
		if project.MainWorktree != "" {
			paths = append(paths, project.MainWorktree)
		}
	}
	return paths
}

// observeLastSnapshot seeds the activity map from the history store. It lets
// the first start after an upgrade honor "recently active" even before the
// state file exists.
func (a *App) observeLastSnapshot(path string, at time.Time) {
	if path == "" || at.IsZero() {
		return
	}
	a.watchMu.Lock()
	if a.lastActive == nil {
		a.lastActive = make(map[string]time.Time)
	}
	if existing, ok := a.lastActive[path]; !ok || at.After(existing) {
		a.lastActive[path] = at
		a.stateDirty = true
	}
	a.watchMu.Unlock()
}

// forgetWorktree drops the UI activity and the persisted last-active entry of a
// worktree that no longer exists.
func (a *App) forgetWorktree(path string) {
	a.activityMu.Lock()
	delete(a.activity, path)
	a.activityMu.Unlock()

	a.watchMu.Lock()
	delete(a.lastActive, path)
	a.stateDirty = true
	a.watchMu.Unlock()
}

// markActive records the last committed snapshot of a worktree. It is the input
// for hibernation and for the initial watcher selection.
func (a *App) markActive(path string, at time.Time) {
	a.watchMu.Lock()
	if a.lastActive == nil {
		a.lastActive = make(map[string]time.Time)
	}
	a.lastActive[path] = at
	a.stateDirty = true
	a.watchMu.Unlock()
}

// updateWatchGroups reconciles the watcher processes with the scanned projects:
// one watcher per project, started only for recently active projects.
func (a *App) updateWatchGroups(projects map[string]*ProjectStatus) {
	if a.watchers == nil {
		return
	}

	now := time.Now()
	type action struct {
		key    string
		origin string
		paths  []string
	}
	var start, stop []action
	keep := make(map[string]bool)

	a.watchMu.Lock()
	for key, project := range projects {
		paths := make([]string, 0, len(project.Worktrees))
		for _, worktree := range project.Worktrees {
			if worktree.Path == "" || worktree.State == "missing" || worktree.State == "error" {
				continue
			}
			paths = append(paths, worktree.Path)
		}
		if len(paths) == 0 {
			continue
		}
		keep[key] = true
		group, ok := a.watchGroups[key]
		if !ok {
			group = &watchGroup{key: key}
			a.watchGroups[key] = group
		}
		group.origin = project.MainWorktree
		if group.origin == "" && len(paths) > 0 {
			group.origin = paths[0]
		}
		group.paths = paths
		if a.initialScan && a.recentlyActiveLocked(paths, now) {
			group.graceUntil = now.Add(a.hibernateAfter)
		}

		active := a.groupActiveLocked(group, now)
		switch {
		case active && group.hibernated:
			group.hibernated = false
			start = append(start, action{key: key, origin: group.origin, paths: append([]string(nil), paths...)})
		case active:
			start = append(start, action{key: key, origin: group.origin, paths: append([]string(nil), paths...)})
		case !group.hibernated:
			group.hibernated = true
			stop = append(stop, action{key: key, paths: append([]string(nil), paths...)})
		}
	}
	for key, group := range a.watchGroups {
		if keep[key] {
			continue
		}
		stop = append(stop, action{key: key, paths: append([]string(nil), group.paths...)})
		delete(a.watchGroups, key)
	}
	a.watchMu.Unlock()

	for _, item := range stop {
		a.watchers.Remove(item.key)
		for _, path := range item.paths {
			a.supervisor.SetCold(path, true)
		}
	}
	for _, item := range start {
		a.watchers.Ensure(item.key, item.origin, item.paths)
	}
}

// groupActiveLocked reports whether a project should have a live watcher. The
// caller must hold watchMu.
func (a *App) groupActiveLocked(group *watchGroup, now time.Time) bool {
	if a.hibernateAfter <= 0 {
		return true
	}
	if now.Before(group.graceUntil) {
		return true
	}
	return a.recentlyActiveLocked(group.paths, now)
}

func (a *App) recentlyActiveLocked(paths []string, now time.Time) bool {
	if a.hibernateAfter <= 0 {
		return true
	}
	for _, path := range paths {
		if at, ok := a.lastActive[path]; ok && now.Sub(at) <= a.hibernateAfter {
			return true
		}
	}
	return false
}

// wakeWatcher starts the project's watcher again after a snapshot. It also
// leaves the cold state so the poll returns to its normal cadence.
func (a *App) wakeWatcher(path string) {
	if a.watchers == nil {
		return
	}

	a.watchMu.Lock()
	var key, origin string
	var paths []string
	wasHibernated := false
	for groupKey, group := range a.watchGroups {
		for _, candidate := range group.paths {
			if candidate == path {
				key, origin = groupKey, group.origin
				paths = append([]string(nil), group.paths...)
				wasHibernated = group.hibernated
				group.hibernated = false
				break
			}
		}
		if key != "" {
			break
		}
	}
	a.watchMu.Unlock()
	if key == "" {
		return
	}

	if wasHibernated {
		for _, candidate := range paths {
			a.supervisor.SetCold(candidate, false)
		}
	}
	a.watchers.Ensure(key, origin, paths)
}

// watchStateChanged maps a watcher process start/exit to the recorder state of
// every worktree in the project.
func (a *App) watchStateChanged(key string, running bool) {
	a.watchMu.Lock()
	group := a.watchGroups[key]
	var paths []string
	hibernated := false
	if group != nil {
		paths = append([]string(nil), group.paths...)
		hibernated = group.hibernated
	}
	a.watchMu.Unlock()

	for _, path := range paths {
		switch {
		case running:
			a.supervisor.SetWatching(path, true)
		case hibernated:
			a.supervisor.SetCold(path, true)
		default:
			a.supervisor.SetWatching(path, false)
		}
	}
	a.notifyStatus()
}

func (a *App) hibernateLoop(ctx context.Context) {
	if a.watchers == nil || a.hibernateAfter <= 0 {
		return
	}
	interval := a.hibernateAfter / 3
	if interval < time.Second {
		interval = time.Second
	}
	if interval > hibernateTickInterval {
		interval = hibernateTickInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.hibernateIdle(time.Now())
		}
	}
}

// hibernateIdle stops watchers of projects that have not produced a snapshot
// within the idle threshold. The next poll still detects changes and starts the
// watcher again.
func (a *App) hibernateIdle(now time.Time) {
	type stop struct {
		key   string
		paths []string
	}
	var stops []stop

	a.watchMu.Lock()
	for key, group := range a.watchGroups {
		if group.hibernated || a.groupActiveLocked(group, now) {
			continue
		}
		group.hibernated = true
		stops = append(stops, stop{key: key, paths: append([]string(nil), group.paths...)})
	}
	a.watchMu.Unlock()

	for _, item := range stops {
		a.log.Debug("watcher hibernated", "project", item.key, "worktrees", len(item.paths))
		a.watchers.Remove(item.key)
		for _, path := range item.paths {
			a.supervisor.SetCold(path, true)
		}
	}
	if len(stops) > 0 {
		a.notifyStatus()
	}
}

// flushActivityLoop persists the last-active map in the background.
func (a *App) flushActivityLoop(ctx context.Context) {
	ticker := time.NewTicker(activityFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.saveActivityState()
		}
	}
}

type activityState struct {
	Version   int               `json:"version"`
	Worktrees map[string]string `json:"worktrees"`
	// Activity is the newest event per worktree. It is intentionally the same
	// shape the UI receives, so restoring it needs no second model.
	Activity []Activity `json:"activity,omitempty"`
}

// activityStatePath returns the per-user state file. It lives under
// XDG_STATE_HOME so it survives a restart without polluting the config.
func activityStatePath() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "tachograph", "activity.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "tachograph", "activity.json")
}

// loadActivityState restores both the last-active timestamps (hibernation) and
// the newest activity entry per worktree (the UI table).
func loadActivityState(log *slog.Logger) (map[string]time.Time, []Activity) {
	state := make(map[string]time.Time)
	activity := []Activity{}
	path := activityStatePath()
	if path == "" {
		return state, activity
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) && log != nil {
			log.Warn("could not read activity state", "path", path, "err", err)
		}
		return state, activity
	}

	var stored activityState
	if err := json.Unmarshal(data, &stored); err != nil {
		if log != nil {
			log.Warn("could not parse activity state", "path", path, "err", err)
		}
		return state, activity
	}

	if stored.Version < activityStateVersion {
		// Version 4 switched to cumulative diffstats. Drop cached
		// per-snapshot values so old entries cannot show misleading numbers.
		for i := range stored.Activity {
			stored.Activity[i].Stats = nil
		}
	}

	cutoff := time.Now().Add(-activityMaxAge)
	for worktree, value := range stored.Worktrees {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil || parsed.Before(cutoff) {
			continue
		}
		state[worktree] = parsed
	}

	byPath := make(map[string]Activity)
	for _, entry := range stored.Activity {
		parsed, err := time.Parse(time.RFC3339, entry.Time)
		if entry.Path == "" || err != nil || parsed.Before(cutoff) {
			continue
		}
		if existing, ok := byPath[entry.Path]; !ok || entry.Time > existing.Time {
			byPath[entry.Path] = entry
		}
	}
	activity = make([]Activity, 0, len(byPath))
	for _, entry := range byPath {
		activity = append(activity, entry)
	}
	sort.Slice(activity, func(left, right int) bool {
		return activity[left].Time > activity[right].Time
	})
	if len(activity) > maxActivity {
		activity = activity[:maxActivity]
	}
	return state, activity
}

// activityMap turns the persisted list back into the per-worktree map the
// recorder events update.
func activityMap(entries []Activity) map[string]Activity {
	activity := make(map[string]Activity, len(entries))
	for _, entry := range entries {
		if entry.Path != "" {
			activity[entry.Path] = entry
		}
	}
	return activity
}

// saveActivityState writes the map atomically when it changed.
func (a *App) saveActivityState() {
	a.watchMu.Lock()
	if !a.stateDirty || a.statePath == "" {
		a.watchMu.Unlock()
		return
	}
	cutoff := time.Now().Add(-activityMaxAge)
	stored := activityState{
		Version:   activityStateVersion,
		Worktrees: make(map[string]string, len(a.lastActive)),
	}
	for path, at := range a.lastActive {
		if at.Before(cutoff) {
			continue
		}
		stored.Worktrees[path] = at.UTC().Format(time.RFC3339)
	}
	a.stateDirty = false
	a.watchMu.Unlock()

	a.activityMu.Lock()
	activity := make([]Activity, 0, len(a.activity))
	for _, entry := range a.activity {
		parsed, err := time.Parse(time.RFC3339, entry.Time)
		if err != nil || parsed.Before(cutoff) {
			continue
		}
		activity = append(activity, entry)
	}
	a.activityMu.Unlock()
	sort.Slice(activity, func(left, right int) bool {
		return activity[left].Time > activity[right].Time
	})
	if len(activity) > maxActivity {
		activity = activity[:maxActivity]
	}
	stored.Activity = activity

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		a.log.Warn("could not encode activity state", "err", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(a.statePath), 0o755); err != nil {
		a.log.Warn("could not create state dir", "err", err)
		return
	}
	tmp := a.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		a.log.Warn("could not write activity state", "path", tmp, "err", err)
		return
	}
	if err := os.Rename(tmp, a.statePath); err != nil {
		a.log.Warn("could not replace activity state", "path", a.statePath, "err", err)
		_ = os.Remove(tmp)
	}
}
