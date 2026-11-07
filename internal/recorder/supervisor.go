// Package recorder supervises active worktree recorders. watchexec events are
// the primary trigger; polling is the fallback. Both share one per-worktree
// schedule, so an active worktree reacts within seconds while idle worktrees
// back off to one poll every few minutes.
package recorder

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/history"
)

const (
	snapshotTimeout = 30 * time.Second
	errorBackoff    = 30 * time.Second
	schedulerIdle   = time.Hour
	minSchedulerGap = 10 * time.Millisecond
)

// Snapshotter is the part of a history recorder the supervisor needs.
type Snapshotter interface {
	Snapshot(ctx context.Context) (history.Snapshot, error)
}

// Event is emitted when a snapshot succeeds or fails.
type Event struct {
	Time    time.Time
	Path    string
	Kind    string
	Hash    string
	Message string
	Stats   *history.ChangeStats
}

// Options configures the supervisor.
type Options struct {
	// Interval is the active (fast) poll interval.
	Interval time.Duration
	// MaxInterval is the idle (slow) poll interval.
	MaxInterval time.Duration
	Workers     int
	Log         *slog.Logger
	OnEvent     func(Event)
}

// Status is the live state of one recorder.
type Status struct {
	State        string `json:"state"`
	LastSnapshot string `json:"lastSnapshot,omitempty"`
	LastHash     string `json:"lastHash,omitempty"`
	Snapshots    int    `json:"snapshots"`
	IntervalMs   int    `json:"intervalMs"`
	Error        string `json:"error,omitempty"`
}

type entry struct {
	path     string
	recorder Snapshotter
	busy     atomic.Bool
	pending  atomic.Bool
	watching atomic.Bool

	mu           sync.Mutex
	lastSnapshot time.Time
	lastHash     string
	snapshots    int
	lastError    string
	backoffUntil time.Time
	interval     time.Duration
	nextPoll     time.Time
	cold         bool
}

// Supervisor owns the active recorders and their poll schedule.
type Supervisor struct {
	minInterval time.Duration
	maxInterval time.Duration
	workers     int
	log         *slog.Logger
	onEvent     func(Event)

	mu      sync.RWMutex
	entries map[string]*entry

	paused  atomic.Bool
	running atomic.Bool
	wake    chan struct{}
}

// New creates a supervisor. Zero values fall back to a ten-second active
// interval, five-minute idle interval and four workers.
func New(opts Options) *Supervisor {
	minInterval := opts.Interval
	if minInterval <= 0 {
		minInterval = 10 * time.Second
	}
	maxInterval := opts.MaxInterval
	if maxInterval <= 0 {
		maxInterval = 5 * time.Minute
	}
	if maxInterval < minInterval {
		maxInterval = minInterval
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = 4
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Supervisor{
		minInterval: minInterval,
		maxInterval: maxInterval,
		workers:     workers,
		log:         log,
		onEvent:     opts.OnEvent,
		entries:     make(map[string]*entry),
		wake:        make(chan struct{}, 1),
	}
}

// Run polls until ctx is cancelled.
func (s *Supervisor) Run(ctx context.Context) {
	s.running.Store(true)
	defer s.running.Store(false)

	for {
		timer := time.NewTimer(s.wait())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
			s.dispatchDue(ctx)
		}
	}
}

// wait returns the time until the earliest scheduled poll.
func (s *Supervisor) wait() time.Duration {
	if s.paused.Load() {
		return schedulerIdle
	}

	now := time.Now()
	wait := schedulerIdle
	s.mu.RLock()
	for _, item := range s.entries {
		item.mu.Lock()
		at := item.nextPoll
		item.mu.Unlock()
		if at.IsZero() || !at.After(now) {
			s.mu.RUnlock()
			return minSchedulerGap
		}
		if remaining := at.Sub(now); remaining < wait {
			wait = remaining
		}
	}
	s.mu.RUnlock()

	if wait < minSchedulerGap {
		return minSchedulerGap
	}
	return wait
}

// dispatchDue snapshots every entry whose scheduled poll is due. It waits for
// the batch, so the next schedule starts from the actual completion time.
func (s *Supervisor) dispatchDue(ctx context.Context) {
	if s.paused.Load() {
		return
	}

	now := time.Now()
	s.mu.RLock()
	entries := make([]*entry, 0, len(s.entries))
	for _, item := range s.entries {
		item.mu.Lock()
		due := !now.Before(item.nextPoll)
		if due && now.Before(item.backoffUntil) {
			item.nextPoll = item.backoffUntil
			due = false
		}
		if due {
			item.nextPoll = now.Add(item.interval)
		}
		item.mu.Unlock()
		if due {
			entries = append(entries, item)
		}
	}
	s.mu.RUnlock()

	s.runBatch(ctx, entries)
}

// Poll snapshots every active worktree now. It waits for the workers, so a
// manual call is deterministic.
func (s *Supervisor) Poll(ctx context.Context) {
	if s.paused.Load() {
		return
	}

	now := time.Now()
	s.mu.RLock()
	entries := make([]*entry, 0, len(s.entries))
	for _, item := range s.entries {
		item.mu.Lock()
		item.nextPoll = now
		item.mu.Unlock()
		entries = append(entries, item)
	}
	s.mu.RUnlock()

	s.runBatch(ctx, entries)
}

// Trigger asks for a snapshot of one worktree as soon as a worker is free. A
// trigger that arrives while a snapshot is running is folded into exactly one
// follow-up run.
func (s *Supervisor) Trigger(path string) {
	s.mu.RLock()
	item, ok := s.entries[path]
	s.mu.RUnlock()
	if !ok {
		return
	}
	item.pending.Store(true)
	item.mu.Lock()
	item.nextPoll = time.Now()
	item.mu.Unlock()
	s.signalWake()
}

func (s *Supervisor) runBatch(ctx context.Context, entries []*entry) {
	if len(entries) == 0 {
		return
	}

	jobs := make(chan *entry)
	var workers sync.WaitGroup
	for i := 0; i < s.workers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				s.snapshot(ctx, item)
			}
		}()
	}

	for _, item := range entries {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		case jobs <- item:
		}
	}
	close(jobs)
	workers.Wait()
}

func (s *Supervisor) snapshot(ctx context.Context, item *entry) {
	if !item.busy.CompareAndSwap(false, true) {
		return
	}
	defer item.busy.Store(false)

	// Any trigger that arrived before this run starts is covered by it; only a
	// trigger that arrives while Snapshot is running schedules the follow-up.
	item.pending.Store(false)

	item.mu.Lock()
	if time.Now().Before(item.backoffUntil) {
		item.nextPoll = item.backoffUntil
		item.mu.Unlock()
		return
	}
	item.mu.Unlock()

	snapshotCtx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	snapshot, err := item.recorder.Snapshot(snapshotCtx)
	cancel()

	now := time.Now()
	item.mu.Lock()
	switch {
	case errors.Is(err, history.ErrNoChanges):
		item.lastError = ""
		item.backoffUntil = time.Time{}
		item.interval = nextInterval(item.interval, s.maxInterval)
	case err != nil:
		item.lastError = err.Error()
		item.backoffUntil = now.Add(errorBackoff)
	default:
		item.lastSnapshot = snapshot.CreatedAt
		item.lastHash = snapshot.Hash
		item.snapshots++
		item.lastError = ""
		item.backoffUntil = time.Time{}
		item.interval = s.activeInterval(item.watching.Load())
	}
	pending := item.pending.Swap(false)
	switch {
	case pending:
		item.nextPoll = now
	case err != nil && !errors.Is(err, history.ErrNoChanges):
		item.nextPoll = item.backoffUntil
	default:
		item.nextPoll = now.Add(item.interval)
	}
	item.mu.Unlock()

	if pending {
		s.signalWake()
	}

	switch {
	case errors.Is(err, history.ErrNoChanges):
		s.log.Debug("worktree unchanged", "path", item.path)
	case err != nil:
		s.log.Warn("snapshot failed", "path", item.path, "err", err)
		s.emit(Event{Time: now, Path: item.path, Kind: "error", Message: err.Error()})
	default:
		s.log.Info("snapshot recorded", "path", item.path, "hash", snapshot.Hash)
		s.emit(Event{Time: snapshot.CreatedAt, Path: item.path, Kind: "snapshot", Hash: snapshot.Hash, Stats: snapshot.Stats})
	}
}

// activeInterval is the interval used after a snapshot. With a live watcher the
// poll is only a liveness check, so it stays slow; without one it becomes fast.
func (s *Supervisor) activeInterval(watching bool) time.Duration {
	if watching {
		return s.maxInterval
	}
	return s.minInterval
}

func (s *Supervisor) emit(event Event) {
	if s.onEvent != nil {
		s.onEvent(event)
	}
}

func (s *Supervisor) signalWake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Set registers a worktree. An existing entry keeps its state; the recorder is
// stable for the path.
func (s *Supervisor) Set(path string, recorder Snapshotter) {
	s.mu.Lock()
	if _, exists := s.entries[path]; exists {
		s.mu.Unlock()
		return
	}
	s.entries[path] = &entry{
		path:     path,
		recorder: recorder,
		interval: s.minInterval,
		nextPoll: time.Now(),
	}
	s.mu.Unlock()
	s.signalWake()
}

// Has reports whether a worktree is already supervised.
func (s *Supervisor) Has(path string) bool {
	s.mu.RLock()
	_, ok := s.entries[path]
	s.mu.RUnlock()
	return ok
}

// SetWatching tells the schedule whether a watchexec process is live for the
// worktree. Without one the poll falls back to the active interval.
func (s *Supervisor) SetWatching(path string, watching bool) {
	s.mu.RLock()
	item, ok := s.entries[path]
	s.mu.RUnlock()
	if !ok {
		return
	}

	item.watching.Store(watching)
	item.mu.Lock()
	item.cold = false
	if watching {
		item.interval = s.maxInterval
	} else {
		item.interval = s.minInterval
		if item.nextPoll.After(time.Now().Add(item.interval)) {
			item.nextPoll = time.Now().Add(item.interval)
		}
	}
	item.mu.Unlock()
	s.signalWake()
}

// SetCold parks a worktree on the slow poll after its watcher was hibernated.
// Clearing the flag returns it to the active interval and schedules a poll now.
func (s *Supervisor) SetCold(path string, cold bool) {
	s.mu.RLock()
	item, ok := s.entries[path]
	s.mu.RUnlock()
	if !ok {
		return
	}

	item.watching.Store(false)
	item.mu.Lock()
	item.cold = cold
	now := time.Now()
	if cold {
		item.interval = s.maxInterval
		if item.nextPoll.Before(now) {
			item.nextPoll = now.Add(item.interval)
		}
	} else {
		item.interval = s.minInterval
		if item.nextPoll.After(now.Add(item.interval)) {
			item.nextPoll = now.Add(item.interval)
		}
	}
	item.mu.Unlock()
	s.signalWake()
}

// Seed initializes the visible history from the store. It only fills an empty
// entry; a running recorder is never overwritten.
func (s *Supervisor) Seed(path string, snapshots int, last time.Time, hash string) {
	s.mu.RLock()
	item, ok := s.entries[path]
	s.mu.RUnlock()
	if !ok {
		return
	}

	item.mu.Lock()
	defer item.mu.Unlock()
	if item.snapshots != 0 || !item.lastSnapshot.IsZero() {
		return
	}
	item.snapshots = snapshots
	item.lastSnapshot = last
	item.lastHash = hash
}

// Resync replaces the visible counters after the history branch was rewritten.
func (s *Supervisor) Resync(path string, snapshots int, last time.Time, hash string) {
	s.mu.RLock()
	item, ok := s.entries[path]
	s.mu.RUnlock()
	if !ok {
		return
	}

	item.mu.Lock()
	item.snapshots = snapshots
	item.lastSnapshot = last
	item.lastHash = hash
	item.mu.Unlock()
}

// Remove stops supervising one worktree.
func (s *Supervisor) Remove(path string) {
	s.mu.Lock()
	delete(s.entries, path)
	s.mu.Unlock()
	s.signalWake()
}

// Retain removes every entry whose path is not in keep.
func (s *Supervisor) Retain(keep map[string]bool) {
	s.mu.Lock()
	for path := range s.entries {
		if !keep[path] {
			delete(s.entries, path)
		}
	}
	s.mu.Unlock()
	s.signalWake()
}

func (s *Supervisor) Pause() { s.paused.Store(true) }
func (s *Supervisor) Paused() bool {
	return s.paused.Load()
}

// Resume restarts every worktree immediately instead of waiting for its idle
// interval.
func (s *Supervisor) Resume() {
	s.paused.Store(false)
	now := time.Now()
	s.mu.RLock()
	for _, item := range s.entries {
		item.mu.Lock()
		item.nextPoll = now
		item.mu.Unlock()
	}
	s.mu.RUnlock()
	s.signalWake()
}

// Recording is true while the supervisor is running and not paused.
func (s *Supervisor) Recording() bool {
	return s.running.Load() && !s.paused.Load()
}

// Status returns the live state for one worktree.
func (s *Supervisor) Status(path string) (Status, bool) {
	s.mu.RLock()
	item, ok := s.entries[path]
	s.mu.RUnlock()
	if !ok {
		return Status{}, false
	}

	item.mu.Lock()
	defer item.mu.Unlock()

	state := "polling"
	switch {
	case s.paused.Load():
		state = "paused"
	case item.lastError != "":
		state = "error"
	case item.cold:
		state = "idle"
	case item.watching.Load():
		state = "watching"
	}

	status := Status{
		State:      state,
		LastHash:   item.lastHash,
		Snapshots:  item.snapshots,
		IntervalMs: int(item.interval / time.Millisecond),
		Error:      item.lastError,
	}
	if !item.lastSnapshot.IsZero() {
		status.LastSnapshot = item.lastSnapshot.Format(time.RFC3339)
	}
	return status, true
}

// nextInterval doubles the idle interval, capped at max.
func nextInterval(current, max time.Duration) time.Duration {
	if current <= 0 {
		return max
	}
	next := current * 2
	if next > max || next <= 0 {
		return max
	}
	return next
}
