//go:build linux

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const chromePWALauncher = "[Desktop Entry]\n" +
	"Version=1.0\n" +
	"Name=Tachograph\n" +
	"Exec=/usr/bin/google-chrome --profile-directory=Default --app-id=abcdefghijklmnop\n" +
	"Terminal=false\n" +
	"Type=Application\n"

func writeDesktop(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindPWALauncherDirsFindsChromePWA(t *testing.T) {
	dir := t.TempDir()
	writeDesktop(t, dir, "chrome-abcdefghijklmnop-Default.desktop", chromePWALauncher)
	writeDesktop(t, dir, "google-chrome.desktop",
		"[Desktop Entry]\nName=Google Chrome\nExec=/usr/bin/google-chrome %U\n")

	got, ok := findPWALauncherDirs([]string{dir}, "Tachograph")
	if !ok {
		t.Fatal("expected to find the PWA launcher")
	}
	if filepath.Base(got.Path) != "chrome-abcdefghijklmnop-Default.desktop" {
		t.Fatalf("found %q, want the PWA launcher (not the browser entry)", got.Path)
	}
}

func TestFindPWALauncherDirsSkipsNonMatches(t *testing.T) {
	dir := t.TempDir()
	writeDesktop(t, dir, "chrome-pwa.desktop", chromePWALauncher)
	writeDesktop(t, dir, "other.desktop",
		"[Desktop Entry]\nName=Other App\nExec=/usr/bin/other --app-id=x\n")
	writeDesktop(t, dir, "hidden.desktop",
		"[Desktop Entry]\nName=Tachograph\nExec=/usr/bin/chrome --app-id=x\nHidden=true\n")
	writeDesktop(t, dir, "nodisplay.desktop",
		"[Desktop Entry]\nName=Tachograph\nExec=/usr/bin/chrome --app-id=x\nNoDisplay=true\n")

	if _, ok := findPWALauncherDirs([]string{dir}, "Thought Keeper"); ok {
		t.Error("matched a different app name")
	}
	if _, ok := findPWALauncherDirs([]string{filepath.Join(t.TempDir(), "nope")}, "Tachograph"); ok {
		t.Error("matched a missing directory")
	}
	if _, ok := findPWALauncherDirs([]string{dir}, "   "); ok {
		t.Error("matched an empty name")
	}
}

func TestIsChromiumPWALauncher(t *testing.T) {
	if !isChromiumPWALauncher("/usr/bin/google-chrome --profile-directory=Default --app-id=abc") {
		t.Error("an --app-id Exec is a PWA launcher")
	}
	if isChromiumPWALauncher("/usr/bin/google-chrome %U") {
		t.Error("the browser's own entry is not a PWA launcher")
	}
}

func TestParseDesktopEntry(t *testing.T) {
	entry, ok := parseDesktopEntry(chromePWALauncher)
	if !ok || entry.name != "Tachograph" || !isChromiumPWALauncher(entry.exec) {
		t.Fatalf("entry = %+v, ok = %v", entry, ok)
	}
	if entry.noDisplay || entry.hidden {
		t.Errorf("entry flags = hidden:%v noDisplay:%v", entry.hidden, entry.noDisplay)
	}
}

func TestOpenUISmartUsesPWALauncher(t *testing.T) {
	dataHome := t.TempDir()
	applications := filepath.Join(dataHome, "applications")
	writeDesktop(t, applications, "chrome-pwa.desktop", chromePWALauncher)

	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "launched.txt")
	writeExecutable(t, filepath.Join(bin, "gio"), "#!/bin/sh\nprintf '%s' \"$*\" > "+marker+"\n")
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_DATA_DIRS", "")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	via, launcher, err := openUISmart("http://127.0.0.1:9/", "Tachograph")
	if err != nil {
		t.Fatalf("openUISmart: %v", err)
	}
	if via != "pwa" {
		t.Fatalf("via = %q, want pwa (launcher %q)", via, launcher)
	}
	content := waitForFile(t, marker)
	if !strings.Contains(content, "launch") || !strings.Contains(content, "chrome-pwa.desktop") {
		t.Errorf("launcher invocation = %q, want the desktop entry", content)
	}
}

func TestOpenUISmartFallsBackToOSHandler(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_DATA_DIRS", "")

	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "os.txt")
	writeExecutable(t, filepath.Join(bin, "xdg-open"), "#!/bin/sh\nprintf '%s' \"$*\" > "+marker+"\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	via, _, err := openUISmart("http://127.0.0.1:9/", "Tachograph")
	if err != nil {
		t.Fatalf("openUISmart: %v", err)
	}
	if via != "os" {
		t.Fatalf("via = %q, want os", via)
	}
	if content := waitForFile(t, marker); !strings.Contains(content, "http://127.0.0.1:9/") {
		t.Errorf("OS handler received %q", content)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func waitForFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			return string(data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s was not written", path)
	return ""
}
