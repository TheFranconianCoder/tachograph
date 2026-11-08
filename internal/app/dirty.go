package app

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// dirtyRefreshInterval keeps the source worktree marker responsive without
	// running git status for every status frame.
	dirtyRefreshInterval = 10 * time.Second
	dirtyCheckTimeout    = 5 * time.Second
	dirtyCheckWorkers    = 4
)

// dirtyState caches the source worktree state for the project list. The state
// describes the real checkout, not the snapshot store.
type dirtyState struct {
	mu     sync.Mutex
	values map[string]bool
	wake   chan struct{}
}

func newDirtyState() *dirtyState {
	return &dirtyState{
		values: make(map[string]bool),
		wake:   make(chan struct{}, 1),
	}
}

// wakeUp asks dirtyLoop for an immediate refresh. The signal is coalesced, so a
// scan that publishes many worktrees still costs one pass.
func (d *dirtyState) wakeUp() {
	if d == nil {
		return
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// snapshot returns a copy for the status builder. Callers never hold the lock
// while encoding or sending a frame.
func (d *dirtyState) snapshot() map[string]bool {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	values := make(map[string]bool, len(d.values))
	for path, dirty := range d.values {
		values[path] = dirty
	}
	return values
}

// set records one worktree and reports whether the visible value changed.
func (d *dirtyState) set(path string, dirty bool) bool {
	key := filepath.Clean(path)
	d.mu.Lock()
	defer d.mu.Unlock()
	previous, known := d.values[key]
	d.values[key] = dirty
	return !known || previous != dirty
}

// prune drops worktrees that no longer exist.
func (d *dirtyState) prune(live map[string]bool) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	changed := false
	for path := range d.values {
		if !live[path] {
			delete(d.values, path)
			changed = true
		}
	}
	return changed
}

func (d *dirtyState) forget(path string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	delete(d.values, filepath.Clean(path))
	d.mu.Unlock()
}

// dirtyLoop refreshes the source state while a UI is connected. Without a
// subscriber the marker cannot be seen, so the work is skipped like a
// hibernated watcher.
func (a *App) dirtyLoop(ctx context.Context) {
	if a.dirty == nil {
		return
	}
	ticker := time.NewTicker(dirtyRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.dirty.wake:
		case <-ticker.C:
		}
		if a.hub == nil || a.hub.Subscribers() == 0 {
			continue
		}
		a.refreshDirty(ctx)
	}
}

// refreshDirty reads git status for every known worktree and publishes only a
// changed set. Errors keep the previous value: a transient index lock must not
// make the marker flicker.
func (a *App) refreshDirty(ctx context.Context) {
	paths := a.worktreePaths()
	if len(paths) == 0 {
		return
	}

	live := make(map[string]bool, len(paths))
	for _, path := range paths {
		live[path] = true
	}

	type result struct {
		path  string
		dirty bool
	}
	results := make(chan result, len(paths))
	semaphore := make(chan struct{}, dirtyCheckWorkers)
	var workers sync.WaitGroup
	for _, path := range paths {
		path := path
		workers.Add(1)
		go func() {
			defer workers.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			dirty, err := worktreeDirty(ctx, path)
			if err != nil {
				if a.log != nil {
					a.log.Debug("could not read worktree status", "path", path, "err", err)
				}
				return
			}
			results <- result{path: path, dirty: dirty}
		}()
	}
	workers.Wait()
	close(results)

	changed := false
	for item := range results {
		if a.dirty.set(item.path, item.dirty) {
			changed = true
		}
	}
	if a.dirty.prune(live) {
		changed = true
	}
	if changed {
		a.notifyStatus()
	}
}

// worktreePaths returns the distinct real worktree paths of the current scan.
func (a *App) worktreePaths() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	seen := make(map[string]bool, len(a.projects))
	paths := make([]string, 0, len(a.projects))
	for _, project := range a.projects {
		for _, worktree := range project.Worktrees {
			if worktree.Path == "" {
				continue
			}
			path := filepath.Clean(worktree.Path)
			if seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

// worktreeDirty reports whether the source checkout has uncommitted changes
// against HEAD. --no-optional-locks keeps the background check from writing the
// index or competing with the user's own git commands.
func worktreeDirty(ctx context.Context, path string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dirtyCheckTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", path,
		"status", "--porcelain", "--untracked-files=normal")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return false, fmt.Errorf("git status: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()) != "", nil
}
