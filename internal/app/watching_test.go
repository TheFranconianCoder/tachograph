package app

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/recorder"
)

func TestActivityKeepsNewestEventPerWorktree(t *testing.T) {
	a := &App{activity: make(map[string]Activity)}
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

	a.recordRecorderEvent(recorder.Event{Time: base, Path: "/a", Kind: "snapshot", Hash: "1"})
	a.recordRecorderEvent(recorder.Event{Time: base.Add(time.Minute), Path: "/b", Kind: "snapshot", Hash: "2"})
	a.recordRecorderEvent(recorder.Event{Time: base.Add(2 * time.Minute), Path: "/a", Kind: "error", Message: "boom"})

	got := a.activitySnapshot()
	if len(got) != 2 {
		t.Fatalf("activity entries = %d, want 2 (one per worktree)", len(got))
	}
	if got[0].Path != "/a" || got[0].Kind != "error" || got[0].Message != "boom" {
		t.Errorf("newest entry = %+v, want the error for /a", got[0])
	}
	if got[1].Path != "/b" {
		t.Errorf("second entry = %+v, want /b", got[1])
	}
}

func TestActivityStateRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	statePath := activityStatePath()
	if statePath == "" {
		t.Fatal("activityStatePath returned an empty path")
	}

	now := time.Now().UTC().Truncate(time.Second)
	a := &App{
		log:        discardAppLogger(),
		statePath:  statePath,
		lastActive: map[string]time.Time{"/worktree": now},
		activity: map[string]Activity{
			"/worktree": {
				Time: now.Format(time.RFC3339),
				Kind: "snapshot",
				Path: "/worktree",
				Hash: "abc123",
			},
		},
		stateDirty: true,
	}
	a.saveActivityState()

	loadedLastActive, loadedActivity := loadActivityState(a.log)
	got, ok := loadedLastActive["/worktree"]
	if !ok {
		t.Fatalf("loaded activity = %v, missing /worktree", loadedLastActive)
	}
	if !got.Equal(now) {
		t.Errorf("loaded time = %s, want %s", got, now)
	}
	if len(loadedActivity) != 1 || loadedActivity[0].Hash != "abc123" {
		t.Errorf("loaded entries = %+v, want the snapshot activity", loadedActivity)
	}
}

func TestGroupActiveUsesActivityAndGrace(t *testing.T) {
	now := time.Now()
	a := &App{
		hibernateAfter: 5 * time.Minute,
		lastActive:     map[string]time.Time{"/a": now.Add(-10 * time.Minute)},
	}
	group := &watchGroup{paths: []string{"/a"}}

	if a.groupActiveLocked(group, now) {
		t.Fatal("a group idle for ten minutes must not be active")
	}

	group.graceUntil = now.Add(time.Minute)
	if !a.groupActiveLocked(group, now) {
		t.Fatal("the startup grace must keep the group active")
	}

	a.lastActive["/a"] = now.Add(-time.Minute)
	if !a.groupActiveLocked(group, now) {
		t.Fatal("a recent snapshot must keep the group active")
	}
}

func discardAppLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
