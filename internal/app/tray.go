package app

import (
	"context"
	"sync"

	"github.com/gogpu/systray"

	"github.com/TheFranconianCoder/tachograph/internal/version"
)

const (
	trayStateIdle     = "idle"
	trayStatePolling  = "polling"
	trayStateWatching = "watching"
)

// traySummary is the aggregate recorder state shown by the tray icon.
type traySummary struct {
	State    string
	Paused   bool
	Watching int
	Polling  int
	Idle     int
}

// trayIcon holds the platform handle and the last painted state. The tray is
// updated from background goroutines, so every access goes through the mutex.
type trayIcon struct {
	mu      sync.Mutex
	tray    *systray.SystemTray
	state   string
	tooltip string
}

// runTray shows the tray icon and blocks on the platform message loop. It
// returns when the tray is removed or when the platform rejects it (for example
// on a headless host).
func (a *App) runTray(ctx context.Context) error {
	tray := systray.New()

	menu := systray.NewMenu()
	menu.Add("Open Tachograph", a.openURL)
	menu.Add("Pause/Resume recording", func() {
		a.toggleRecording()
	})
	menu.AddSeparator()
	menu.Add("Quit", a.requestQuit)

	tray.SetMenu(menu)
	a.attachTray(tray)
	tray.OnClick(a.openURL)
	tray.Show()
	defer a.detachTray()

	go func() {
		<-a.quit
		a.detachTray()
		tray.Remove()
	}()
	go func() {
		<-ctx.Done()
		a.requestQuit()
	}()

	return tray.Run()
}

// attachTray publishes the platform handle and paints the current state.
func (a *App) attachTray(tray *systray.SystemTray) {
	if a.tray == nil {
		return
	}
	a.tray.mu.Lock()
	a.tray.tray = tray
	a.tray.state = ""
	a.tray.tooltip = ""
	a.tray.mu.Unlock()
	a.refreshTray()
}

// detachTray stops background state updates from touching a removed tray.
func (a *App) detachTray() {
	if a.tray == nil {
		return
	}
	a.tray.mu.Lock()
	a.tray.tray = nil
	a.tray.state = ""
	a.tray.tooltip = ""
	a.tray.mu.Unlock()
}

// refreshTray repaints the icon and tooltip when the aggregate state changed.
func (a *App) refreshTray() {
	if a.tray == nil {
		return
	}

	summary := a.traySummary()
	icon := trayStateIcon(summary.State)
	tooltip := trayTooltip(summary)

	a.tray.mu.Lock()
	defer a.tray.mu.Unlock()
	if a.tray.tray == nil {
		return
	}
	if a.tray.state != summary.State {
		a.tray.tray.SetIcon(icon)
		a.tray.state = summary.State
	}
	if a.tray.tooltip != tooltip {
		a.tray.tray.SetTooltip(tooltip)
		a.tray.tooltip = tooltip
	}
}

// traySummary folds the per-worktree states into one icon state. Watching wins
// over polling, polling over idle; a paused recorder is idle regardless of the
// watchers that were live before the pause.
func (a *App) traySummary() traySummary {
	paused := a.supervisor != nil && a.supervisor.Paused()
	summary := traySummary{Paused: paused}
	for _, project := range a.projectsSnapshot() {
		for _, worktree := range project.Worktrees {
			switch worktree.Watcher {
			case trayStateWatching:
				summary.Watching++
			case trayStatePolling:
				summary.Polling++
			case trayStateIdle:
				summary.Idle++
			}
		}
	}

	switch {
	case paused, summary.Watching == 0 && summary.Polling == 0:
		summary.State = trayStateIdle
	case summary.Watching > 0:
		summary.State = trayStateWatching
	default:
		summary.State = trayStatePolling
	}
	return summary
}

// trayTooltip is the hover text: the product, the version and the state.
func trayTooltip(summary traySummary) string {
	state := "idle"
	switch {
	case summary.Paused:
		state = "paused"
	case summary.State == trayStateWatching:
		state = "watching"
	case summary.State == trayStatePolling:
		state = "polling"
	}
	return "Tachograph " + version.String() + " — " + state
}
