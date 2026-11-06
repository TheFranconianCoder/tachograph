// Package watcher runs one watchexec process per project and reports file
// changes to the recorder. watchexec is only a trigger: snapshots are still
// taken by the history store, and polling remains the fallback when the binary
// is missing, a project is hibernated, or a process exits.
package watcher

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	minBackoff = time.Second
	maxBackoff = time.Minute
	// A debounced event batch can quote many paths; keep the line buffer generous.
	maxEventLine = 4 << 20
	// A process that ran this long is considered healthy; its next restart starts
	// the backoff from the beginning.
	healthyAfter = 30 * time.Second
)

// Options configures the watcher manager.
type Options struct {
	Binary   string
	Debounce time.Duration
	Ignore   []string
	Log      *slog.Logger
	// OnChange reports a changed worktree (one of the watched paths).
	OnChange func(path string)
	// OnState reports whether the group's watcher process is live.
	OnState func(key string, running bool)
}

// Manager owns the watchexec processes, one per project. Ensure/Retain mirror
// the recorder supervisor: the worktrees of a project share one process.
type Manager struct {
	binary   string
	debounce time.Duration
	ignore   []string
	log      *slog.Logger
	onChange func(path string)
	onState  func(key string, running bool)

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.RWMutex
	groups map[string]*group
	closed bool
	wg     sync.WaitGroup
}

type group struct {
	key    string
	origin string
	paths  []string
	cancel context.CancelFunc
	active atomic.Bool
}

// NewManager creates a manager. A binary must be resolved by the caller.
func NewManager(opts Options) *Manager {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	debounce := opts.Debounce
	if debounce <= 0 {
		debounce = 1500 * time.Millisecond
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		binary:   opts.Binary,
		debounce: debounce,
		ignore:   append([]string(nil), opts.Ignore...),
		log:      log,
		onChange: opts.OnChange,
		onState:  opts.OnState,
		ctx:      ctx,
		cancel:   cancel,
		groups:   make(map[string]*group),
	}
}

// Ensure starts (or restarts) the watcher for one project. The process watches
// every worktree path and is restarted only when the path set or origin changes.
func (m *Manager) Ensure(key, origin string, paths []string) {
	if m == nil || m.binary == "" || key == "" {
		return
	}
	paths = normalizePaths(paths)
	if len(paths) == 0 {
		m.Remove(key)
		return
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if existing, ok := m.groups[key]; ok {
		if existing.origin == origin && slices.Equal(existing.paths, paths) {
			m.mu.Unlock()
			return
		}
		existing.cancel()
		delete(m.groups, key)
	}
	ctx, cancel := context.WithCancel(m.ctx)
	item := &group{key: key, origin: origin, paths: paths, cancel: cancel}
	m.groups[key] = item
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		m.supervise(ctx, item)
	}()
}

// Running reports whether the watcher process for a project is live.
func (m *Manager) Running(key string) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	item, ok := m.groups[key]
	m.mu.RUnlock()
	return ok && item.active.Load()
}

// Retain stops every watcher whose project key is not in keep.
func (m *Manager) Retain(keep map[string]bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	var stop []*group
	for key, item := range m.groups {
		if !keep[key] {
			stop = append(stop, item)
			delete(m.groups, key)
		}
	}
	m.mu.Unlock()
	for _, item := range stop {
		item.cancel()
	}
}

// Remove stops the watcher for one project.
func (m *Manager) Remove(key string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	item, ok := m.groups[key]
	delete(m.groups, key)
	m.mu.Unlock()
	if ok {
		item.cancel()
	}
}

// Close stops every watcher and waits for the processes to end.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.mu.Unlock()

	m.cancel()
	m.wg.Wait()
}

func (m *Manager) supervise(ctx context.Context, item *group) {
	backoff := minBackoff
	for {
		if ctx.Err() != nil {
			return
		}

		started := time.Now()
		command := exec.CommandContext(ctx, m.binary, m.args(item)...)
		// The process gets a moment to exit after cancellation before it is killed.
		command.WaitDelay = 2 * time.Second

		stdout, err := command.StdoutPipe()
		if err != nil {
			m.log.Warn("watcher stdout unavailable", "project", item.key, "err", err)
			return
		}
		stderr, err := command.StderrPipe()
		if err != nil {
			m.log.Warn("watcher stderr unavailable", "project", item.key, "err", err)
			return
		}

		if err := command.Start(); err != nil {
			m.log.Warn("watcher could not start", "project", item.key, "err", err)
			if !sleep(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		item.active.Store(true)
		m.setState(item, true)
		m.log.Debug("watcher started", "project", item.key, "worktrees", len(item.paths), "pid", command.Process.Pid)

		go m.drain(stderr, item.key)

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), maxEventLine)
		for scanner.Scan() {
			m.handleLine(item, scanner.Bytes())
		}

		waitErr := command.Wait()
		item.active.Store(false)
		m.setState(item, false)

		if ctx.Err() != nil {
			return
		}
		if time.Since(started) >= healthyAfter {
			backoff = minBackoff
		} else {
			backoff = nextBackoff(backoff)
		}
		m.log.Warn("watcher exited", "project", item.key, "err", waitErr, "restartIn", backoff)
		if !sleep(ctx, backoff) {
			return
		}
	}
}

// handleLine maps one debounced event batch to the worktrees it touched.
func (m *Manager) handleLine(item *group, line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}

	var batch struct {
		Tags []struct {
			Kind     string `json:"kind"`
			Absolute string `json:"absolute"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(line, &batch); err != nil {
		m.log.Debug("watcher event ignored", "project", item.key, "err", err)
		return
	}

	seen := make(map[string]bool)
	for _, tag := range batch.Tags {
		if tag.Kind != "path" || tag.Absolute == "" {
			continue
		}
		path, ok := matchWorktree(tag.Absolute, item.paths)
		if !ok || seen[path] {
			continue
		}
		seen[path] = true
		if m.onChange != nil {
			m.onChange(path)
		}
	}
}

func (m *Manager) drain(pipe io.Reader, key string) {
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 8*1024), 256*1024)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			m.log.Debug("watcher", "project", key, "line", line)
		}
	}
}

func (m *Manager) setState(item *group, running bool) {
	if !m.current(item) {
		return
	}
	if m.onState != nil {
		m.onState(item.key, running)
	}
}

func (m *Manager) current(item *group) bool {
	m.mu.RLock()
	current, ok := m.groups[item.key]
	m.mu.RUnlock()
	return ok && current == item
}

func (m *Manager) args(item *group) []string {
	args := []string{"--only-emit-events", "--quiet"}
	for _, path := range item.paths {
		args = append(args, "--watch", path)
	}
	if item.origin != "" {
		args = append(args, "--project-origin", item.origin)
	}
	args = append(args, "--debounce", m.debounce.String())
	for _, pattern := range m.ignore {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			args = append(args, "--ignore", pattern)
		}
	}
	return args
}

// argsForGroup exposes the argument list for tests.
func argsForGroup(origin string, paths []string, debounce time.Duration, ignore []string) []string {
	manager := &Manager{debounce: debounce, ignore: ignore}
	return manager.args(&group{origin: origin, paths: normalizePaths(paths)})
}

func normalizePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		normalized = append(normalized, path)
	}
	slices.Sort(normalized)
	return normalized
}

// matchWorktree returns the longest watched path that contains the event path.
func matchWorktree(candidate string, paths []string) (string, bool) {
	candidate = filepath.Clean(candidate)
	best := ""
	for _, path := range paths {
		if candidate == path || strings.HasPrefix(candidate, path+string(os.PathSeparator)) {
			if len(path) > len(best) {
				best = path
			}
		}
	}
	return best, best != ""
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > maxBackoff || next <= 0 {
		return maxBackoff
	}
	return next
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Resolve finds the watchexec binary. An explicit path or name wins, then PATH,
// then a mise installation (shims, .mise-bins, or the nested release binary).
func Resolve(configured string) string {
	if configured != "" {
		if path, err := exec.LookPath(configured); err == nil && isExecutable(path) {
			return path
		}
		if isExecutable(configured) {
			return configured
		}
		return ""
	}

	if path, err := exec.LookPath("watchexec"); err == nil && isExecutable(path) {
		return path
	}

	for _, root := range miseRoots() {
		candidates := []string{filepath.Join(root, "shims", "watchexec")}
		candidates = append(candidates, glob(filepath.Join(root, "installs", "watchexec", "*", ".mise-bins", "watchexec"))...)
		candidates = append(candidates, glob(filepath.Join(root, "installs", "watchexec", "*", "watchexec-*", "watchexec"))...)
		for i := len(candidates) - 1; i >= 0; i-- {
			if isExecutable(candidates[i]) {
				return candidates[i]
			}
		}
	}
	return ""
}

func miseRoots() []string {
	var roots []string
	if dir := strings.TrimSpace(os.Getenv("MISE_DATA_DIR")); dir != "" {
		roots = append(roots, dir)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, ".local", "share", "mise"))
	}
	return roots
}

func glob(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
