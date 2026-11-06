package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAutostartHandler(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux desktop entry")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "config.yaml")
	app := &App{cfg: DefaultConfig(), cfgPath: path}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/autostart", strings.NewReader(`{"enabled":true}`))
	recorder := httptest.NewRecorder()
	app.handleAutostart(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !app.cfg.Autostart {
		t.Error("config was not updated to autostart=true")
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !loaded.Autostart {
		t.Error("autostart was not persisted")
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(configDir, "autostart", "tachograph.desktop")
	content, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("read desktop entry: %v", err)
	}
	if !strings.Contains(string(content), "--no-open") {
		t.Errorf("desktop entry does not start without the UI:\n%s", content)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/autostart", strings.NewReader(`{"enabled":false}`))
	recorder = httptest.NewRecorder()
	app.handleAutostart(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if app.cfg.Autostart {
		t.Error("config still has autostart=true")
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Errorf("desktop entry still exists after disabling: %v", err)
	}
}
