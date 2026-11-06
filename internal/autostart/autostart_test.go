package autostart

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSyncLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux desktop entry")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	executable := filepath.Join(t.TempDir(), "My Apps", "tachograph")
	opts := Options{
		Executable: executable,
		Args:       []string{"app", "--no-open", "--config", "/tmp/config file.yaml"},
	}

	result, err := Sync(true, opts)
	if err != nil {
		t.Fatalf("Sync(true): %v", err)
	}
	if !result.Enabled || result.Path == "" {
		t.Fatalf("result = %+v", result)
	}
	content, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "Exec=") || !strings.Contains(text, "--no-open") {
		t.Errorf("desktop entry = %q, want the no-open command", text)
	}
	if !strings.Contains(text, `"`+executable+`"`) {
		t.Errorf("desktop entry does not quote the executable with spaces:\n%s", text)
	}
	if strings.Contains(text, "--open") && !strings.Contains(text, "--no-open") {
		t.Errorf("desktop entry opens the UI:\n%s", text)
	}

	if _, err := Sync(true, opts); err != nil {
		t.Fatalf("second Sync(true): %v", err)
	}

	if _, err := Sync(false, opts); err != nil {
		t.Fatalf("Sync(false): %v", err)
	}
	if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
		t.Errorf("desktop entry still exists after disabling: %v", err)
	}
}

func TestLaunchAgentAndWindowsCommand(t *testing.T) {
	opts := Options{Executable: `/Applications/Tachograph & Co.app`, Args: []string{"app", "--no-open"}}
	plist := launchAgent(opts)
	if !strings.Contains(plist, "<string>--no-open</string>") {
		t.Errorf("launch agent is missing the argument:\n%s", plist)
	}
	if !strings.Contains(plist, "Tachograph &amp; Co.app") {
		t.Errorf("launch agent did not escape the executable:\n%s", plist)
	}
	if command := windowsCommand(opts); !strings.Contains(command, `app --no-open`) {
		t.Errorf("windows command = %q", command)
	}
}
