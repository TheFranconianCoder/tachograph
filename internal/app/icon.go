package app

import _ "embed"

// trayIconPNG is the Tachograph pictogram rendered to a small PNG for the tray.
// The source of truth is design/pictograms/tachograph.svg in the studio-page
// repository; the raster is regenerated from it.
//
//go:embed icon.png
var trayIconPNG []byte

// trayIcon returns the tray icon bytes.
func trayIcon() []byte { return trayIconPNG }
