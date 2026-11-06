package app

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/TheFranconianCoder/tachograph/internal/web"
)

// Smart open prefers the installed PWA's own OS launcher over the generic URL
// handler. A browser that installs a PWA writes a real launcher; starting it
// opens the app window instead of handing a URL to whichever handler happens to
// be registered. The path is best-effort: no launcher means the OS handler.
type pwaLauncher struct {
	Path string
}

var (
	pwaNameOnce sync.Once
	pwaNameVal  string
)

// pwaName is the name the installed PWA carries, read from the embedded
// manifest so there is one source of truth.
func pwaName() string {
	pwaNameOnce.Do(func() {
		blob, err := web.Asset("manifest.webmanifest")
		if err != nil {
			return
		}
		var manifest struct {
			Name      string `json:"name"`
			ShortName string `json:"short_name"`
		}
		if err := json.Unmarshal(blob, &manifest); err != nil {
			return
		}
		pwaNameVal = strings.TrimSpace(manifest.Name)
		if pwaNameVal == "" {
			pwaNameVal = strings.TrimSpace(manifest.ShortName)
		}
	})
	return pwaNameVal
}

// openUISmart opens the UI, preferring the installed PWA's launcher and falling
// back to the OS URL handler. via is "pwa" or "os".
func openUISmart(url, name string) (via, launcher string, err error) {
	if entry, ok := findPWALauncher(name); ok {
		if launchErr := launchPWALauncher(entry); launchErr == nil {
			return "pwa", entry.Path, nil
		}
		return "os", entry.Path, OpenURL(url)
	}
	return "os", "", OpenURL(url)
}

// OpenUI opens a running instance's UI, preferring the installed PWA.
func OpenUI(url string) error {
	_, _, err := openUISmart(url, pwaName())
	return err
}

// openURL opens this instance's UI through the same path as the tray.
func (a *App) openURL() {
	url := a.uiURL()
	via, launcher, err := openUISmart(url, pwaName())
	if err != nil {
		a.log.Warn("could not open the UI", "url", url, "err", err)
		return
	}
	a.log.Info("opened the UI", "url", url, "via", via, "launcher", launcher)
}
