package app

import (
	"bytes"
	"testing"

	"github.com/TheFranconianCoder/tachograph/internal/recorder"
)

func TestTraySummaryFoldsWorktreeStates(t *testing.T) {
	tests := []struct {
		name     string
		projects []ProjectStatus
		paused   bool
		state    string
		watching int
		polling  int
		idle     int
	}{
		{
			name:  "no worktrees is idle",
			state: trayStateIdle,
		},
		{
			name: "polling only",
			projects: []ProjectStatus{{
				Worktrees: []WorktreeStatus{{Watcher: "polling"}, {Watcher: "idle"}},
			}},
			state:   trayStatePolling,
			polling: 1,
			idle:    1,
		},
		{
			name: "watching wins over polling",
			projects: []ProjectStatus{{
				Worktrees: []WorktreeStatus{{Watcher: "polling"}, {Watcher: "watching"}},
			}},
			state:    trayStateWatching,
			watching: 1,
			polling:  1,
		},
		{
			name: "hibernated worktrees are idle",
			projects: []ProjectStatus{{
				Worktrees: []WorktreeStatus{{Watcher: "idle"}, {Watcher: "idle"}},
			}},
			state: trayStateIdle,
			idle:  2,
		},
		{
			name: "errors do not hide a live watcher",
			projects: []ProjectStatus{{
				Worktrees: []WorktreeStatus{{Watcher: "error"}, {Watcher: "watching"}},
			}},
			state:    trayStateWatching,
			watching: 1,
		},
		{
			name: "paused is idle",
			projects: []ProjectStatus{{
				Worktrees: []WorktreeStatus{{Watcher: "watching"}, {Watcher: "polling"}},
			}},
			paused:   true,
			state:    trayStateIdle,
			watching: 1,
			polling:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := &App{projects: test.projects, tray: &trayIcon{}}
			if test.paused {
				app.supervisor = recorder.New(recorder.Options{})
				app.supervisor.Pause()
			}

			summary := app.traySummary()
			if summary.State != test.state {
				t.Errorf("state = %q, want %q", summary.State, test.state)
			}
			if summary.Watching != test.watching || summary.Polling != test.polling || summary.Idle != test.idle {
				t.Errorf("counts = watching %d, polling %d, idle %d; want %d, %d, %d",
					summary.Watching, summary.Polling, summary.Idle,
					test.watching, test.polling, test.idle)
			}
		})
	}
}

func TestTrayIconHasDistinctStateAssets(t *testing.T) {
	idle := trayStateIcon(trayStateIdle)
	polling := trayStateIcon(trayStatePolling)
	watching := trayStateIcon(trayStateWatching)

	for state, icon := range map[string][]byte{
		trayStateIdle:     idle,
		trayStatePolling:  polling,
		trayStateWatching: watching,
	} {
		if len(icon) == 0 {
			t.Errorf("%s icon is empty", state)
		}
	}
	if bytes.Equal(idle, polling) || bytes.Equal(idle, watching) || bytes.Equal(polling, watching) {
		t.Error("tray state icons are not distinct")
	}
}
