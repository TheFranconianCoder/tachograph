package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/history"
	"github.com/TheFranconianCoder/tachograph/internal/version"
	"github.com/TheFranconianCoder/tachograph/internal/web"
)

// Status is the public, read-only view of the app.
type Status struct {
	Name             string              `json:"name"`
	Version          string              `json:"version"`
	UptimeSeconds    int64               `json:"uptimeSeconds"`
	Recording        bool                `json:"recording"`
	Paused           bool                `json:"paused"`
	WatcherEngine    string              `json:"watcherEngine"`
	DiffTool         string              `json:"diffTool"`
	WorktreeSetup    WorktreeSetupStatus `json:"worktreeSetup"`
	WorktreePatterns []string            `json:"worktreePatterns"`
	Autostart        bool                `json:"autostart"`
	Roots            []string            `json:"roots"`
	Projects         []ProjectStatus     `json:"projects"`
	Activity         []Activity          `json:"activity"`
	Scanning         bool                `json:"scanning"`
	LastScan         string              `json:"lastScan,omitempty"`
	ScanError        string              `json:"scanError,omitempty"`
}

func (a *App) status() Status {
	a.mu.RLock()
	roots := make([]string, len(a.cfg.Roots))
	copy(roots, a.cfg.Roots)
	patterns := make([]string, len(a.cfg.Worktrees.Patterns))
	copy(patterns, a.cfg.Worktrees.Patterns)
	scanning := a.scanning
	lastScan := ""
	if !a.lastScan.IsZero() {
		lastScan = a.lastScan.Format(time.RFC3339)
	}
	scanError := a.scanError
	a.mu.RUnlock()

	recording := false
	paused := false
	if a.supervisor != nil {
		recording = a.supervisor.Recording()
		paused = a.supervisor.Paused()
	}
	diffTool := "git"
	if a.historyStore != nil && a.historyStore.Diff.Enabled() {
		diffTool = a.historyStore.Diff.Name
	}

	return Status{
		Name:             "Tachograph",
		Version:          version.String(),
		UptimeSeconds:    int64(time.Since(a.started).Seconds()),
		Recording:        recording,
		Paused:           paused,
		WatcherEngine:    a.watcherEngine,
		DiffTool:         diffTool,
		WorktreeSetup:    a.worktreeSetupStatus(),
		WorktreePatterns: patterns,
		Autostart:        a.autostartSnapshot(),
		Roots:            roots,
		Projects:         a.projectsSnapshot(),
		Activity:         a.activitySnapshot(),
		Scanning:         scanning,
		LastScan:         lastScan,
		ScanError:        scanError,
	}
}

func (a *App) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", a.handleHealth)
	mux.HandleFunc("GET /api/v1/status", a.handleStatus)
	mux.HandleFunc("GET /api/v1/events", a.handleEvents)
	mux.HandleFunc("GET /api/v1/projects", a.handleProjects)
	mux.HandleFunc("GET /api/v1/activity", a.handleActivity)
	mux.HandleFunc("GET /api/v1/timeline", a.handleTimeline)
	mux.HandleFunc("GET /api/v1/snapshot", a.handleSnapshot)
	mux.HandleFunc("GET /api/v1/range", a.handleRange)
	mux.HandleFunc("PUT /api/v1/snapshot/label", a.handleSetLabel)
	mux.HandleFunc("POST /api/v1/revert", a.handleRevert)
	mux.HandleFunc("GET /api/v1/roots", a.handleRootsList)
	mux.HandleFunc("POST /api/v1/roots", a.handleRootAdd)
	mux.HandleFunc("DELETE /api/v1/roots", a.handleRootRemove)
	mux.HandleFunc("GET /api/v1/patterns", a.handlePatternsList)
	mux.HandleFunc("POST /api/v1/patterns", a.handlePatternAdd)
	mux.HandleFunc("DELETE /api/v1/patterns", a.handlePatternRemove)
	mux.HandleFunc("POST /api/v1/scan", a.handleScan)
	mux.HandleFunc("GET /api/v1/lifecycle", a.handleWorktreeSetup)
	mux.HandleFunc("PUT /api/v1/lifecycle", a.handleWorktreeSetup)
	mux.HandleFunc("POST /api/v1/lifecycle/apply", a.handleApplyWorktreeSetup)
	mux.HandleFunc("PUT /api/v1/autostart", a.handleAutostart)
	mux.HandleFunc("POST /api/v1/recording/pause", a.handlePause)
	mux.HandleFunc("POST /api/v1/recording/resume", a.handleResume)
	mux.HandleFunc("POST /api/v1/recording/poll", a.handlePoll)
	mux.Handle("/", web.Handler())
	return mux
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.status())
}

func (a *App) handleProjects(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.projectsSnapshot())
}

func (a *App) handleActivity(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.activitySnapshot())
}

func (a *App) handleRootsList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"roots": a.rootsSnapshot()})
}

type rootRequest struct {
	Path string `json:"path"`
}

func (a *App) handleRootAdd(w http.ResponseWriter, r *http.Request) {
	var request rootRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	path, err := absoluteDirectory(request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	a.mu.Lock()
	for _, root := range a.cfg.Roots {
		if root == path {
			a.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"roots": a.rootsSnapshot()})
			return
		}
	}
	cfg := a.cfg
	cfg.Roots = append(append([]string(nil), a.cfg.Roots...), path)
	if err := SaveConfig(a.cfgPath, cfg); err != nil {
		a.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.cfg = cfg
	a.mu.Unlock()
	a.notifyStatus()

	go func() {
		if err := a.Scan(context.Background()); err != nil {
			a.log.Warn("scan after adding root failed", "err", err)
		}
	}()
	writeJSON(w, http.StatusOK, map[string]any{"roots": a.rootsSnapshot()})
}

func (a *App) handleRootRemove(w http.ResponseWriter, r *http.Request) {
	var request rootRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	path, err := filepath.Abs(strings.TrimSpace(request.Path))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	a.mu.Lock()
	roots := make([]string, 0, len(a.cfg.Roots))
	for _, root := range a.cfg.Roots {
		if root != path {
			roots = append(roots, root)
		}
	}
	cfg := a.cfg
	cfg.Roots = roots
	if err := SaveConfig(a.cfgPath, cfg); err != nil {
		a.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.cfg = cfg
	a.mu.Unlock()
	a.notifyStatus()

	go func() {
		if err := a.Scan(context.Background()); err != nil {
			a.log.Warn("scan after removing root failed", "err", err)
		}
	}()
	writeJSON(w, http.StatusOK, map[string]any{"roots": a.rootsSnapshot()})
}

func (a *App) handleScan(w http.ResponseWriter, _ *http.Request) {
	go func() {
		if err := a.Scan(context.Background()); err != nil {
			a.log.Warn("manual scan failed", "err", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "scanning"})
}

func (a *App) handlePause(w http.ResponseWriter, _ *http.Request) {
	a.supervisor.Pause()
	a.notifyStatus()
	writeJSON(w, http.StatusOK, a.status())
}

func (a *App) handleResume(w http.ResponseWriter, _ *http.Request) {
	a.supervisor.Resume()
	a.notifyStatus()
	writeJSON(w, http.StatusOK, a.status())
}

func (a *App) handlePoll(w http.ResponseWriter, _ *http.Request) {
	go a.supervisor.Poll(context.Background())
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "polling"})
}

func (a *App) handleTimeline(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)
	file := strings.TrimSpace(r.URL.Query().Get("file"))
	if file != "" {
		cleaned, ok := worktreeRelativePath(file)
		if !ok {
			writeError(w, http.StatusBadRequest, "file must be a relative path inside the worktree")
			return
		}
		file = cleaned
	}

	recorder, err := a.recorderForPath(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	page, err := recorder.Timeline(r.Context(), limit, offset, file)
	if err != nil {
		a.log.Warn("timeline failed", "path", path, "err", err)
		writeError(w, http.StatusInternalServerError, "could not read the timeline")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *App) handleRange(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if path == "" || from == "" || to == "" {
		writeError(w, http.StatusBadRequest, "path, from and to are required")
		return
	}
	file := strings.TrimSpace(r.URL.Query().Get("file"))
	if file != "" {
		cleaned, ok := worktreeRelativePath(file)
		if !ok {
			writeError(w, http.StatusBadRequest, "file must be a relative path inside the worktree")
			return
		}
		file = cleaned
	}

	recorder, err := a.recorderForPath(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	ctx := history.WithDiffTheme(r.Context(), r.URL.Query().Get("theme"))
	detail, err := recorder.RangeDetail(ctx, from, to, file)
	if errors.Is(err, history.ErrSnapshotNotFound) {
		writeError(w, http.StatusNotFound, "snapshot not found; it may have been compacted away")
		return
	}
	if errors.Is(err, history.ErrFileNotInSnapshot) {
		writeError(w, http.StatusNotFound, "file not found in this range")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *App) handleSetLabel(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path  string `json:"path"`
		Hash  string `json:"hash"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(request.Path) == "" || strings.TrimSpace(request.Hash) == "" {
		writeError(w, http.StatusBadRequest, "path and hash are required")
		return
	}

	recorder, err := a.recorderForPath(r.Context(), request.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	label, err := recorder.SetLabel(r.Context(), request.Hash, request.Label)
	if errors.Is(err, history.ErrSnapshotNotFound) {
		writeError(w, http.StatusNotFound, "snapshot not found; it may have been compacted away")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"label": label})
}

func (a *App) handleRevert(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path string `json:"path"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(request.Path) == "" || strings.TrimSpace(request.From) == "" ||
		strings.TrimSpace(request.To) == "" {
		writeError(w, http.StatusBadRequest, "path, from and to are required")
		return
	}

	recorder, err := a.recorderForPath(r.Context(), request.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	result, err := recorder.Revert(r.Context(), request.From, request.To)
	if errors.Is(err, history.ErrSnapshotNotFound) {
		writeError(w, http.StatusNotFound, "snapshot not found; it may have been compacted away")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	if path == "" || hash == "" {
		writeError(w, http.StatusBadRequest, "path and hash are required")
		return
	}
	file := strings.TrimSpace(r.URL.Query().Get("file"))
	if file != "" {
		cleaned, ok := worktreeRelativePath(file)
		if !ok {
			writeError(w, http.StatusBadRequest, "file must be a relative path inside the worktree")
			return
		}
		file = cleaned
	}

	recorder, err := a.recorderForPath(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	ctx := history.WithDiffTheme(r.Context(), r.URL.Query().Get("theme"))
	detail, err := recorder.SnapshotDetail(ctx, hash, file)
	if errors.Is(err, history.ErrSnapshotNotFound) {
		writeError(w, http.StatusNotFound, "snapshot not found; it may have been compacted away")
		return
	}
	if errors.Is(err, history.ErrFileNotInSnapshot) {
		writeError(w, http.StatusNotFound, "file not found in this snapshot")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// recorderForPath resolves a real worktree path against the durable history
// registry. This also prevents the API from reading arbitrary Git repositories.
func (a *App) recorderForPath(ctx context.Context, path string) (*history.Recorder, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve worktree path: %w", err)
	}
	entries, err := a.historyStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read history stores: %w", err)
	}
	for _, entry := range entries {
		if filepath.Clean(entry.Path) == filepath.Clean(absolute) {
			return a.historyStore.RecorderFromKnown(entry), nil
		}
	}
	return nil, fmt.Errorf("unknown worktree")
}

func queryInt(r *http.Request, name string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return fallback
	}
	return value
}

func worktreeRelativePath(value string) (string, bool) {
	cleaned := filepath.Clean(value)
	if cleaned == "." || cleaned == ".." || filepath.IsAbs(cleaned) {
		return "", false
	}
	if strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(cleaned), true
}

func absoluteDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("path %s: %w", absolute, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %s is not a directory", absolute)
	}
	return absolute, nil
}

// handleEvents is the live channel the UI will use when recording arrives. It
// already sends a heartbeat so the client-side connection code can be built and
// tested against a real stream.
func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub := a.hub.Subscribe()
	defer a.hub.Unsubscribe(sub)

	// Every connection starts with a complete snapshot, so a reconnect needs no
	// replay and the client never depends on frames it missed. Subscribing first
	// means a change during the handshake is queued, not lost.
	if payload, err := a.statusEvent(); err == nil {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return
		}
		flusher.Flush()
	}

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case payload := <-sub.Out():
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
