package app

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// Git writes a directory per linked worktree below <common-dir>/worktrees.
	// Watching that metadata is much cheaper than rescanning every root.
	worktreeWatchInterval = 2 * time.Second
	worktreeScanDebounce  = 600 * time.Millisecond
)

// worktreeWatchLoop detects new and removed linked worktrees between the slow
// root scans and registers them immediately. The root scan remains the source
// of truth; this only decides when to run it.
func (a *App) worktreeWatchLoop(ctx context.Context) {
	ticker := time.NewTicker(worktreeWatchInterval)
	defer ticker.Stop()

	var last string
	initialized := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			signature := a.worktreeMetadataSignature()
			if !initialized {
				last = signature
				initialized = true
				continue
			}
			if signature == last {
				continue
			}
			last = signature

			// `git worktree add` creates the metadata before the checkout is
			// complete; wait briefly so discovery sees a usable worktree.
			select {
			case <-ctx.Done():
				return
			case <-time.After(worktreeScanDebounce):
			}

			for a.scanningNow() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
			}

			a.log.Info("worktree metadata changed; rescanning")
			if err := a.Scan(ctx); err != nil {
				a.log.Warn("rescan after worktree change failed", "err", err)
			}
		}
	}
}

// worktreeMetadataSignature is a stable fingerprint of the linked-worktree
// directories of every known project.
func (a *App) worktreeMetadataSignature() string {
	projects := a.projectsSnapshot()
	seen := make(map[string]bool)
	parts := make([]string, 0, len(projects))
	for _, project := range projects {
		if project.CommonDir == "" || seen[project.CommonDir] {
			continue
		}
		seen[project.CommonDir] = true

		entries, err := os.ReadDir(filepath.Join(project.CommonDir, "worktrees"))
		if err != nil {
			parts = append(parts, project.CommonDir+"|missing")
			continue
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		parts = append(parts, project.CommonDir+"|"+strings.Join(names, ","))
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func (a *App) scanningNow() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.scanning
}
