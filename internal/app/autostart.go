package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/TheFranconianCoder/tachograph/internal/autostart"
)

func (a *App) autostartSnapshot() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.Autostart
}

// autostartOptions describes the login command. It deliberately always passes
// --no-open: an autostart launch belongs in the tray, not in a browser tab.
func (a *App) autostartOptions() (autostart.Options, error) {
	executable, err := os.Executable()
	if err != nil {
		return autostart.Options{}, err
	}
	args := []string{"app", "--no-open"}
	if defaultPath, err := ConfigPath(); err == nil &&
		filepath.Clean(a.cfgPath) != filepath.Clean(defaultPath) {
		args = append(args, "--config", a.cfgPath)
	}
	return autostart.Options{Executable: executable, Args: args}, nil
}

// syncAutostart checks and refreshes the login entry on every start. Failures
// are logged by the caller; autostart must never keep the app from running.
func (a *App) syncAutostart() {
	options, err := a.autostartOptions()
	if err != nil {
		a.log.Warn("could not resolve the autostart command", "err", err)
		return
	}
	result, err := autostart.Sync(a.cfg.Autostart, options)
	if err != nil {
		a.log.Warn("could not sync autostart", "err", err)
		return
	}
	if a.cfg.Autostart {
		a.log.Info("autostart enabled", "entry", result.Path)
	} else {
		a.log.Debug("autostart disabled", "entry", result.Path)
	}
}

func (a *App) handleAutostart(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	options, err := a.autostartOptions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := autostart.Sync(request.Enabled, options); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update autostart: "+err.Error())
		return
	}

	a.mu.Lock()
	cfg := a.cfg
	cfg.Autostart = request.Enabled
	if err := SaveConfig(a.cfgPath, cfg); err != nil {
		a.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.cfg = cfg
	a.mu.Unlock()
	a.notifyStatus()

	writeJSON(w, http.StatusOK, map[string]bool{"autostart": request.Enabled})
}
