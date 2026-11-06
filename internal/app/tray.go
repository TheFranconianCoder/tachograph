package app

import (
	"context"

	"github.com/gogpu/systray"

	"github.com/TheFranconianCoder/tachograph/internal/version"
)

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

	tray.SetIcon(trayIcon()).
		SetTooltip("Tachograph " + version.String()).
		SetMenu(menu)
	tray.OnClick(a.openURL)
	tray.Show()

	go func() {
		<-a.quit
		tray.Remove()
	}()
	go func() {
		<-ctx.Done()
		a.requestQuit()
	}()

	return tray.Run()
}
