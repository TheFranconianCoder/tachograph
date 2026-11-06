package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheFranconianCoder/tachograph/internal/lifecycle"
)

// LifecycleCopyStatus is one copy rule shown and edited by the UI.
type LifecycleCopyStatus struct {
	Path      string `json:"path"`
	Optional  bool   `json:"optional"`
	Overwrite bool   `json:"overwrite"`
}

// LifecycleRunStatus is a read-only view of a configured run action.
type LifecycleRunStatus struct {
	Command string `json:"command"`
	OnlyIf  string `json:"onlyIf,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

// WorktreeSetupStatus is the lifecycle configuration exposed to the UI.
type WorktreeSetupStatus struct {
	Copy []LifecycleCopyStatus `json:"copy"`
	Run  []LifecycleRunStatus  `json:"run"`
}

const (
	lifecycleMaxPatterns    = 50
	lifecycleMaxPatternSize = 200
)

func (a *App) worktreeSetupStatus() WorktreeSetupStatus {
	a.mu.RLock()
	actions := append([]LifecycleAction(nil), a.cfg.Worktrees.OnCreate...)
	a.mu.RUnlock()

	setup := WorktreeSetupStatus{
		Copy: []LifecycleCopyStatus{},
		Run:  []LifecycleRunStatus{},
	}
	for _, action := range actions {
		if action.Copy != nil {
			setup.Copy = append(setup.Copy, LifecycleCopyStatus{
				Path:      action.Copy.Path,
				Optional:  action.Copy.Optional,
				Overwrite: action.Copy.Overwrite,
			})
		}
		if action.Run != nil {
			setup.Run = append(setup.Run, LifecycleRunStatus{
				Command: action.Run.Command,
				OnlyIf:  action.Run.OnlyIf,
				Timeout: action.Run.Timeout,
			})
		}
	}
	return setup
}

func (a *App) handleWorktreeSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, a.worktreeSetupStatus())
		return
	}

	var request struct {
		Copy      []string `json:"copy"`
		Overwrite bool     `json:"overwrite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	patterns, err := cleanCopyPatterns(request.Copy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	copies := make([]LifecycleAction, 0, len(patterns))
	for _, pattern := range patterns {
		copies = append(copies, LifecycleAction{
			Copy: &CopyAction{Path: pattern, Optional: true, Overwrite: request.Overwrite},
		})
	}

	a.mu.Lock()
	runs := make([]LifecycleAction, 0, len(a.cfg.Worktrees.OnCreate))
	for _, action := range a.cfg.Worktrees.OnCreate {
		if action.Copy == nil {
			runs = append(runs, action)
		}
	}
	cfg := a.cfg
	// Copies run before the remaining setup commands, so a command can rely on
	// the files being present.
	cfg.Worktrees.OnCreate = append(copies, runs...)
	if err := SaveConfig(a.cfgPath, cfg); err != nil {
		a.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.cfg = cfg
	a.mu.Unlock()
	a.notifyStatus()

	writeJSON(w, http.StatusOK, a.worktreeSetupStatus())
}

func (a *App) handleApplyWorktreeSetup(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	actions := copyActions(mapActions(a.cfg.Worktrees.OnCreate))
	a.mu.RUnlock()

	known, err := a.historyStore.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	applied := 0
	copied := 0
	failures := []string{}
	for _, entry := range known {
		if _, err := os.Stat(entry.Path); err != nil {
			continue
		}
		recorder := a.historyStore.RecorderFromKnown(entry)
		result, err := lifecycle.CopyOnly(recorder, actions, a.log)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", entry.Path, err))
			continue
		}
		applied++
		copied += len(result.Copied)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"worktrees": applied,
		"copied":    copied,
		"errors":    failures,
	})
}

func copyActions(actions []lifecycle.Action) []lifecycle.Action {
	copies := make([]lifecycle.Action, 0, len(actions))
	for _, action := range actions {
		if action.Copy != nil {
			copies = append(copies, action)
		}
	}
	return copies
}

// cleanCopyPatterns validates GUI input before it reaches the config file.
func cleanCopyPatterns(values []string) ([]string, error) {
	patterns := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > lifecycleMaxPatternSize {
			return nil, fmt.Errorf("copy pattern is longer than %d characters", lifecycleMaxPatternSize)
		}
		if filepath.IsAbs(value) {
			return nil, fmt.Errorf("copy pattern %q must be relative", value)
		}
		clean := filepath.Clean(value)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("copy pattern %q escapes the worktree", value)
		}
		if clean == ".git" || strings.HasPrefix(clean, ".git"+string(filepath.Separator)) {
			return nil, fmt.Errorf("copy pattern %q is inside the Git directory", value)
		}
		if _, err := filepath.Match(clean, "x"); err != nil {
			return nil, fmt.Errorf("copy pattern %q is not a valid glob: %w", value, err)
		}
		pattern := filepath.ToSlash(clean)
		if seen[pattern] {
			continue
		}
		seen[pattern] = true
		patterns = append(patterns, pattern)
	}
	if len(patterns) > lifecycleMaxPatterns {
		return nil, fmt.Errorf("at most %d copy patterns are allowed", lifecycleMaxPatterns)
	}
	return patterns, nil
}
