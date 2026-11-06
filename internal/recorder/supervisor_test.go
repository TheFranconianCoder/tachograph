package recorder

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/history"
)

func TestPollRecordsChangesAndHonoursPause(t *testing.T) {
	ctx := context.Background()
	recorder := testRecorder(t)
	supervisor := New(Options{Interval: time.Hour, Workers: 1, Log: discardLogger()})
	supervisor.Set(recorder.Worktree.Path, recorder)

	supervisor.Poll(ctx)
	if got := snapshots(supervisor, recorder.Worktree.Path); got != 1 {
		t.Fatalf("initial snapshots = %d, want 1", got)
	}
	status, _ := supervisor.Status(recorder.Worktree.Path)
	if status.State != "polling" {
		t.Errorf("state = %q, want polling", status.State)
	}

	supervisor.Poll(ctx)
	if got := snapshots(supervisor, recorder.Worktree.Path); got != 1 {
		t.Fatalf("unchanged snapshots = %d, want 1", got)
	}

	writeFile(t, filepath.Join(recorder.Worktree.Path, "a.txt"), "changed\n")
	supervisor.Poll(ctx)
	if got := snapshots(supervisor, recorder.Worktree.Path); got != 2 {
		t.Fatalf("changed snapshots = %d, want 2", got)
	}

	supervisor.Pause()
	writeFile(t, filepath.Join(recorder.Worktree.Path, "a.txt"), "paused\n")
	supervisor.Poll(ctx)
	if got := snapshots(supervisor, recorder.Worktree.Path); got != 2 {
		t.Fatalf("paused snapshots = %d, want 2", got)
	}
	status, _ = supervisor.Status(recorder.Worktree.Path)
	if status.State != "paused" {
		t.Errorf("state while paused = %q, want paused", status.State)
	}

	supervisor.Resume()
	supervisor.Poll(ctx)
	if got := snapshots(supervisor, recorder.Worktree.Path); got != 3 {
		t.Fatalf("resumed snapshots = %d, want 3", got)
	}
}

func TestRunPollsOnItsOwn(t *testing.T) {
	recorder := testRecorder(t)
	supervisor := New(Options{Interval: 25 * time.Millisecond, Workers: 1, Log: discardLogger()})
	supervisor.Set(recorder.Worktree.Path, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go supervisor.Run(ctx)

	waitFor(t, 2*time.Second, func() bool {
		return snapshots(supervisor, recorder.Worktree.Path) >= 1
	})

	writeFile(t, filepath.Join(recorder.Worktree.Path, "a.txt"), "run\n")
	waitFor(t, 2*time.Second, func() bool {
		return snapshots(supervisor, recorder.Worktree.Path) >= 2
	})
}

func TestNextIntervalDoublesAndCaps(t *testing.T) {
	minimum := 10 * time.Second
	maximum := 5 * time.Minute

	cases := []struct {
		current time.Duration
		want    time.Duration
	}{
		{current: 0, want: maximum},
		{current: minimum, want: 20 * time.Second},
		{current: 20 * time.Second, want: 40 * time.Second},
		{current: 160 * time.Second, want: maximum},
		{current: maximum, want: maximum},
	}
	for _, test := range cases {
		if got := nextInterval(test.current, maximum); got != test.want {
			t.Errorf("nextInterval(%s) = %s, want %s", test.current, got, test.want)
		}
	}
}

func TestIdlePollBacksOffToMax(t *testing.T) {
	fake := &fakeSnapshotter{result: history.ErrNoChanges}
	supervisor := New(Options{
		Interval:    20 * time.Millisecond,
		MaxInterval: 80 * time.Millisecond,
		Workers:     1,
		Log:         discardLogger(),
	})
	supervisor.Set("/worktree", fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go supervisor.Run(ctx)

	waitFor(t, 2*time.Second, func() bool {
		status, _ := supervisor.Status("/worktree")
		return status.IntervalMs == 80
	})
	if calls := fake.Calls(); calls < 2 {
		t.Fatalf("snapshot calls = %d, want at least 2", calls)
	}
}

func TestUnchangedPollDoesNotSpin(t *testing.T) {
	fake := &fakeSnapshotter{result: history.ErrNoChanges}
	supervisor := New(Options{
		Interval:    50 * time.Millisecond,
		MaxInterval: 200 * time.Millisecond,
		Workers:     1,
		Log:         discardLogger(),
	})
	supervisor.Set("/worktree", fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go supervisor.Run(ctx)

	waitFor(t, 2*time.Second, func() bool {
		status, _ := supervisor.Status("/worktree")
		return status.IntervalMs == 200
	})
	calls := fake.Calls()
	time.Sleep(100 * time.Millisecond)
	if got := fake.Calls(); got != calls {
		t.Fatalf("unchanged worktree polled %d more times during idle, want 0", got-calls)
	}
}

func TestWatchingKeepsPollSlow(t *testing.T) {
	fake := &fakeSnapshotter{}
	supervisor := New(Options{
		Interval:    20 * time.Millisecond,
		MaxInterval: 500 * time.Millisecond,
		Workers:     1,
		Log:         discardLogger(),
	})
	supervisor.Set("/worktree", fake)
	supervisor.SetWatching("/worktree", true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go supervisor.Run(ctx)

	waitFor(t, 2*time.Second, func() bool { return fake.Calls() >= 1 })
	waitFor(t, 2*time.Second, func() bool {
		status, _ := supervisor.Status("/worktree")
		return status.IntervalMs == 500
	})
	time.Sleep(100 * time.Millisecond)
	if calls := fake.Calls(); calls != 1 {
		t.Fatalf("snapshot calls = %d, want 1 before the slow interval elapses", calls)
	}
}

func TestTriggerRunsWithoutWaitingForTheNextPoll(t *testing.T) {
	fake := &fakeSnapshotter{result: history.ErrNoChanges}
	supervisor := New(Options{
		Interval:    time.Hour,
		MaxInterval: time.Hour,
		Workers:     1,
		Log:         discardLogger(),
	})
	supervisor.Set("/worktree", fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go supervisor.Run(ctx)

	waitFor(t, 2*time.Second, func() bool { return fake.Calls() >= 1 })
	supervisor.Trigger("/worktree")
	waitFor(t, 2*time.Second, func() bool { return fake.Calls() >= 2 })
}

func TestTriggerWhileBusyCoalescesIntoOneRerun(t *testing.T) {
	fake := &fakeSnapshotter{block: make(chan struct{})}
	supervisor := New(Options{
		Interval:    time.Hour,
		MaxInterval: time.Hour,
		Workers:     1,
		Log:         discardLogger(),
	})
	supervisor.Set("/worktree", fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go supervisor.Run(ctx)

	waitFor(t, 2*time.Second, func() bool { return fake.Calls() == 1 })
	supervisor.Trigger("/worktree")
	supervisor.Trigger("/worktree")
	close(fake.block)

	waitFor(t, 2*time.Second, func() bool { return fake.Calls() == 2 })
	time.Sleep(100 * time.Millisecond)
	if calls := fake.Calls(); calls != 2 {
		t.Fatalf("snapshot calls = %d, want exactly 2", calls)
	}
}

func TestColdWorktreePollsAtMaxAndWakesOnDemand(t *testing.T) {
	fake := &fakeSnapshotter{result: history.ErrNoChanges}
	supervisor := New(Options{
		Interval:    50 * time.Millisecond,
		MaxInterval: 400 * time.Millisecond,
		Workers:     1,
		Log:         discardLogger(),
	})
	supervisor.Set("/worktree", fake)
	supervisor.SetCold("/worktree", true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go supervisor.Run(ctx)

	waitFor(t, 2*time.Second, func() bool { return fake.Calls() >= 1 })
	status, _ := supervisor.Status("/worktree")
	if status.State != "idle" {
		t.Errorf("state = %q, want idle", status.State)
	}
	if status.IntervalMs != 400 {
		t.Errorf("interval = %dms, want 400ms", status.IntervalMs)
	}

	calls := fake.Calls()
	time.Sleep(150 * time.Millisecond)
	if got := fake.Calls(); got != calls {
		t.Fatalf("cold worktree polled %d more times during its max interval", got-calls)
	}

	supervisor.SetCold("/worktree", false)
	waitFor(t, 2*time.Second, func() bool { return fake.Calls() > calls })
	status, _ = supervisor.Status("/worktree")
	if status.State != "polling" {
		t.Errorf("state after wake = %q, want polling", status.State)
	}
}

func TestPauseStopsTheScheduler(t *testing.T) {
	fake := &fakeSnapshotter{}
	supervisor := New(Options{
		Interval:    20 * time.Millisecond,
		MaxInterval: 80 * time.Millisecond,
		Workers:     1,
		Log:         discardLogger(),
	})
	supervisor.Set("/worktree", fake)
	supervisor.Pause()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go supervisor.Run(ctx)

	time.Sleep(100 * time.Millisecond)
	if calls := fake.Calls(); calls != 0 {
		t.Fatalf("snapshot calls while paused = %d, want 0", calls)
	}

	supervisor.Resume()
	waitFor(t, 2*time.Second, func() bool { return fake.Calls() >= 1 })
}

type fakeSnapshotter struct {
	mu    sync.Mutex
	calls int
	block chan struct{}
	// result is returned from every snapshot; nil means a successful snapshot.
	result error
}

func (f *fakeSnapshotter) Snapshot(ctx context.Context) (history.Snapshot, error) {
	f.mu.Lock()
	f.calls++
	block := f.block
	f.mu.Unlock()

	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return history.Snapshot{}, ctx.Err()
		}
	}

	if f.result != nil {
		return history.Snapshot{}, f.result
	}
	return history.Snapshot{Hash: "hash", CreatedAt: time.Now()}, nil
}

func (f *fakeSnapshotter) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func testRecorder(t *testing.T) *history.Recorder {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktree := filepath.Join(base, "worktree")

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "init", "-b", "main")
	mustGit(t, project, "config", "user.name", "Test")
	mustGit(t, project, "config", "user.email", "test@example.com")
	mustGit(t, project, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(project, "a.txt"), "base\n")
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-m", "base")
	mustGit(t, project, "worktree", "add", "-b", "feature", worktree)

	recorder, err := history.New(filepath.Join(base, "history")).Ensure(ctx, worktree)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	return recorder
}

func snapshots(supervisor *Supervisor, path string) int {
	status, ok := supervisor.Status(path)
	if !ok {
		return -1
	}
	return status.Snapshots
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
