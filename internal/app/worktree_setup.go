package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheFranconianCoder/tachograph/internal/history"
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
	var request struct {
		Path string `json:"path"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	request.Path = strings.TrimSpace(request.Path)

	a.mu.RLock()
	globalActions := append([]LifecycleAction(nil), a.cfg.Worktrees.OnCreate...)
	a.mu.RUnlock()

	known, err := a.historyStore.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if request.Path != "" {
		project, err := a.rootProject(request.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		known = filterProjectWorktrees(known, project)
	}

	type projectSetup struct {
		actions []LifecycleAction
		err     error
	}
	cache := make(map[string]projectSetup)

	applied := 0
	copied := 0
	failures := []string{}
	for _, entry := range known {
		if _, err := os.Stat(entry.Path); err != nil {
			continue
		}
		recorder := a.historyStore.RecorderFromKnown(entry)
		mainWorktree := recorder.Project.MainWorktree

		setup, ok := cache[mainWorktree]
		if !ok {
			projectActions, err := loadProjectActions(mainWorktree)
			setup = projectSetup{actions: projectActions, err: err}
			cache[mainWorktree] = setup
		}
		if setup.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", entry.Path, setup.err))
		}

		actions := copyActions(mapActions(mergeLifecycleActions(globalActions, setup.actions)))
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

// rootProject resolves the project whose root worktree contains path. A linked
// worktree is rejected: apply is a root-only operation.
func (a *App) rootProject(path string) (ProjectStatus, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return ProjectStatus{}, fmt.Errorf("resolve path: %w", err)
	}

	projects := a.projectsSnapshot()
	bestProject, bestWorktree, bestLength := -1, -1, 0
	for projectIndex, project := range projects {
		for worktreeIndex, worktree := range project.Worktrees {
			if !pathWithin(worktree.Path, absolute) {
				continue
			}
			if bestProject < 0 || len(worktree.Path) > bestLength {
				bestProject, bestWorktree = projectIndex, worktreeIndex
				bestLength = len(worktree.Path)
			}
		}
	}
	if bestProject < 0 {
		return ProjectStatus{}, fmt.Errorf("%s is not inside a managed worktree", absolute)
	}

	project := projects[bestProject]
	worktree := project.Worktrees[bestWorktree]
	if project.MainWorktree == "" {
		return ProjectStatus{}, fmt.Errorf("project %q has no root worktree", project.Name)
	}
	if !samePath(project.MainWorktree, worktree.Path) {
		if samePath(absolute, worktree.Path) {
			return ProjectStatus{}, fmt.Errorf("%s is a linked worktree; run tachograph apply from the root worktree %s",
				absolute, project.MainWorktree)
		}
		return ProjectStatus{}, fmt.Errorf("%s is inside the linked worktree %s; run tachograph apply from the root worktree %s",
			absolute, worktree.Path, project.MainWorktree)
	}
	return project, nil
}

// filterProjectWorktrees keeps the linked worktrees of one project. The root
// worktree is the copy source and is not part of the distribution.
func filterProjectWorktrees(known []history.KnownWorktree, project ProjectStatus) []history.KnownWorktree {
	filtered := make([]history.KnownWorktree, 0, len(known))
	for _, entry := range known {
		if !sameProject(entry, project) {
			continue
		}
		if samePath(entry.Path, project.MainWorktree) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func sameProject(entry history.KnownWorktree, project ProjectStatus) bool {
	if entry.CommonDir != "" && project.CommonDir != "" {
		return samePath(entry.CommonDir, project.CommonDir)
	}
	for _, worktree := range project.Worktrees {
		if samePath(worktree.Path, entry.Path) {
			return true
		}
	}
	return false
}

// pathWithin reports whether target is base itself or below it.
func pathWithin(base, target string) bool {
	if base == "" || target == "" {
		return false
	}
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	if samePath(base, target) {
		return true
	}
	relative, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
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
