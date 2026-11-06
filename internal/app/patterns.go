package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type patternRequest struct {
	Pattern string `json:"pattern"`
}

func (a *App) worktreePatternsSnapshot() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	patterns := make([]string, len(a.cfg.Worktrees.Patterns))
	copy(patterns, a.cfg.Worktrees.Patterns)
	return patterns
}

func (a *App) handlePatternsList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"patterns": a.worktreePatternsSnapshot()})
}

func (a *App) handlePatternAdd(w http.ResponseWriter, r *http.Request) {
	var request patternRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	pattern, err := cleanWorktreePattern(request.Pattern)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	a.mu.Lock()
	for _, existing := range a.cfg.Worktrees.Patterns {
		if existing == pattern {
			a.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"patterns": a.worktreePatternsSnapshot()})
			return
		}
	}
	cfg := a.cfg
	cfg.Worktrees.Patterns = append(append([]string(nil), a.cfg.Worktrees.Patterns...), pattern)
	if err := SaveConfig(a.cfgPath, cfg); err != nil {
		a.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.cfg = cfg
	a.mu.Unlock()
	a.notifyStatus()
	a.scanInBackground("pattern added")

	writeJSON(w, http.StatusOK, map[string]any{"patterns": a.worktreePatternsSnapshot()})
}

func (a *App) handlePatternRemove(w http.ResponseWriter, r *http.Request) {
	var request patternRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	pattern := strings.TrimSpace(request.Pattern)
	if pattern == "" {
		writeError(w, http.StatusBadRequest, "pattern is required")
		return
	}

	a.mu.Lock()
	patterns := make([]string, 0, len(a.cfg.Worktrees.Patterns))
	for _, existing := range a.cfg.Worktrees.Patterns {
		if existing != pattern {
			patterns = append(patterns, existing)
		}
	}
	cfg := a.cfg
	cfg.Worktrees.Patterns = patterns
	if err := SaveConfig(a.cfgPath, cfg); err != nil {
		a.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.cfg = cfg
	a.mu.Unlock()
	a.notifyStatus()
	a.scanInBackground("pattern removed")

	writeJSON(w, http.StatusOK, map[string]any{"patterns": a.worktreePatternsSnapshot()})
}

func (a *App) scanInBackground(reason string) {
	if a.historyStore == nil || a.supervisor == nil {
		return
	}
	go func() {
		if err := a.Scan(context.Background()); err != nil {
			a.log.Warn("scan after "+reason+" failed", "err", err)
		}
	}()
}

// cleanWorktreePattern validates an absolute or ~-prefixed glob pattern before
// it is written to the config.
func cleanWorktreePattern(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("pattern is required")
	}
	expanded := value
	if expanded == "~" || strings.HasPrefix(expanded, "~/") || strings.HasPrefix(expanded, "~"+string(os.PathSeparator)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		expanded = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(expanded, "~"), string(os.PathSeparator)))
	}
	if !filepath.IsAbs(expanded) {
		return "", fmt.Errorf("pattern %q must be absolute or start with ~", value)
	}
	if _, err := filepath.Glob(expanded); err != nil {
		return "", fmt.Errorf("pattern %q: %w", value, err)
	}
	return value, nil
}
