//go:build linux

package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// On Linux a Chromium-family browser that installs a PWA writes a .desktop
// file into the user's application directory. Starting that launcher opens the
// PWA in its own window; it is exactly the file the desktop shows in the app
// list. There is no browser API to ask for "the installed PWA".

func findPWALauncher(name string) (pwaLauncher, bool) {
	return findPWALauncherDirs(pwaLauncherDirs(), name)
}

// findPWALauncherDirs is findPWALauncher over an explicit directory list, so it
// can be tested without reading the user's real application data.
func findPWALauncherDirs(dirs []string, name string) (pwaLauncher, bool) {
	want := strings.TrimSpace(name)
	if want == "" {
		return pwaLauncher{}, false
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".desktop") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			desktop, ok := parseDesktopEntry(string(data))
			if !ok || desktop.noDisplay || desktop.hidden {
				continue
			}
			// A Chromium PWA launcher carries --app-id=<id>. The browser's own
			// entry has none, so the browser itself is never mistaken for the app.
			if !isChromiumPWALauncher(desktop.exec) {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(desktop.name), want) {
				continue
			}
			return pwaLauncher{Path: path}, true
		}
	}
	return pwaLauncher{}, false
}

// pwaLauncherDirs are the XDG application directories, user data home first.
func pwaLauncherDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	add(filepath.Join(xdgDataHome(), "applications"))
	raw := os.Getenv("XDG_DATA_DIRS")
	if raw == "" {
		raw = "/usr/local/share:/usr/share"
	}
	for _, dir := range filepath.SplitList(raw) {
		if strings.TrimSpace(dir) != "" {
			add(filepath.Join(dir, "applications"))
		}
	}
	return dirs
}

// isChromiumPWALauncher reports whether an Exec line starts a Chromium-family
// browser in app mode, which is how an installed PWA is launched.
func isChromiumPWALauncher(exec string) bool {
	return strings.Contains(exec, "--app-id=")
}

// launchPWALauncher starts a .desktop launcher the way the desktop would.
// `gio launch` reads the entry and applies its quoting; `gtk-launch` is the
// older equivalent. Without either, the caller falls back to the URL handler.
func launchPWALauncher(launcher pwaLauncher) error {
	if _, err := exec.LookPath("gio"); err == nil {
		return exec.CommandContext(context.Background(), "gio", "launch", launcher.Path).Start()
	}
	if _, err := exec.LookPath("gtk-launch"); err == nil {
		id := strings.TrimSuffix(filepath.Base(launcher.Path), ".desktop")
		return exec.CommandContext(context.Background(), "gtk-launch", id).Start()
	}
	return errors.New("neither gio nor gtk-launch is available")
}

// desktopEntry is the little of a .desktop file the discovery needs.
type desktopEntry struct {
	name      string
	exec      string
	noDisplay bool
	hidden    bool
}

// parseDesktopEntry reads the [Desktop Entry] group. Only the unlocalized Name
// is read, so the entry is matched by the name every locale agrees on.
func parseDesktopEntry(data string) (desktopEntry, bool) {
	var (
		entry   desktopEntry
		hasName bool
		inGroup bool
	)
	for raw := range strings.SplitSeq(data, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inGroup = line == "[Desktop Entry]"
			continue
		}
		if !inGroup {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Name":
			entry.name, hasName = strings.TrimSpace(value), true
		case "Exec":
			entry.exec = strings.TrimSpace(value)
		case "NoDisplay":
			entry.noDisplay = strings.EqualFold(strings.TrimSpace(value), "true")
		case "Hidden":
			entry.hidden = strings.EqualFold(strings.TrimSpace(value), "true")
		}
	}
	return entry, hasName
}
