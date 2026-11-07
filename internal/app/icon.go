package app

import _ "embed"

// The tray icon carries the recorder state as a badge in its lower-right
// corner. The source of truth is design/pictograms/tachograph.svg in the
// studio-page repository; the rasters are regenerated from it by
// web/scripts/render-icons.mjs.
//
//go:embed icon-idle.png
var trayIconIdlePNG []byte

//go:embed icon-polling.png
var trayIconPollingPNG []byte

//go:embed icon-watching.png
var trayIconWatchingPNG []byte

// trayIcon returns the icon bytes for a recorder state.
func trayStateIcon(state string) []byte {
	switch state {
	case trayStateWatching:
		return trayIconWatchingPNG
	case trayStatePolling:
		return trayIconPollingPNG
	default:
		return trayIconIdlePNG
	}
}
