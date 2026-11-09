package app

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
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

// sourceState describes one real checkout: uncommitted changes plus the branch
// position against its configured upstream. The counts compare local commits
// and remote-tracking refs, so they are as fresh as the last fetch.
type sourceState struct {
	Dirty    bool
	Upstream string
	Ahead    int
	Behind   int
}

// dirtyState caches the source worktree state for the project list. The state
// describes the real checkout, not the snapshot store.
type dirtyState struct {
	mu     sync.Mutex
	values map[string]sourceState
	wake   chan struct{}
}

func newDirtyState() *dirtyState {
	return &dirtyState{
		values: make(map[string]sourceState),
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
func (d *dirtyState) snapshot() map[string]sourceState {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	values := make(map[string]sourceState, len(d.values))
	for path, state := range d.values {
		values[path] = state
	}
	return values
}

// set records one worktree and reports whether the visible value changed.
func (d *dirtyState) set(path string, state sourceState) bool {
	key := filepath.Clean(path)
	d.mu.Lock()
	defer d.mu.Unlock()
	previous, known := d.values[key]
	d.values[key] = state
	return !known || previous != state
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
		state sourceState
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
			state, err := worktreeSourceState(ctx, path)
			if err != nil {
				if a.log != nil {
					a.log.Debug("could not read worktree status", "path", path, "err", err)
				}
				return
			}
			results <- result{path: path, state: state}
		}()
	}
	workers.Wait()
	close(results)

	changed := false
	for item := range results {
		if a.dirty.set(item.path, item.state) {
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

// worktreeSourceState reports uncommitted changes plus the branch position
// against the configured upstream. Plain `git status` already computes
// ahead/behind by default; `--porcelain=v2 --branch` only prints the result, so
// no extra process or network access is needed. --no-optional-locks keeps the
// background check from writing the index or competing with the user's own git
// commands.
func worktreeSourceState(ctx context.Context, path string) (sourceState, error) {
	ctx, cancel := context.WithTimeout(ctx, dirtyCheckTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", path,
		"status", "--porcelain=v2", "--branch", "--untracked-files=normal")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return sourceState{}, fmt.Errorf("git status: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return parseSourceState(stdout.String()), nil
}

// parseSourceState reads the branch headers of a `git status --porcelain=v2
// --branch` output. Every non-header line describes a pending change.
func parseSourceState(output string) sourceState {
	var state sourceState
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.upstream "):
			state.Upstream = strings.TrimSpace(strings.TrimPrefix(line, "# branch.upstream "))
		case strings.HasPrefix(line, "# branch.ab "):
			fields := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(fields) != 2 {
				continue
			}
			if value, err := strconv.Atoi(strings.TrimPrefix(fields[0], "+")); err == nil {
				state.Ahead = value
			}
			if value, err := strconv.Atoi(strings.TrimPrefix(fields[1], "-")); err == nil {
				state.Behind = value
			}
		case strings.HasPrefix(line, "# "):
			// Other branch headers (oid, head, stash) do not affect the state.
		case strings.TrimSpace(line) == "":
			// The trailing newline of the output.
		default:
			state.Dirty = true
		}
	}
	return state
}
