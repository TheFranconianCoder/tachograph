// Package app is the desktop composition root: configuration, HTTP server, tray
// and the lifecycle that ties them together.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/difft"
	"github.com/TheFranconianCoder/tachograph/internal/discovery"
	"github.com/TheFranconianCoder/tachograph/internal/history"
	"github.com/TheFranconianCoder/tachograph/internal/lifecycle"
	"github.com/TheFranconianCoder/tachograph/internal/recorder"
	"github.com/TheFranconianCoder/tachograph/internal/stream"
	"github.com/TheFranconianCoder/tachograph/internal/version"
	"github.com/TheFranconianCoder/tachograph/internal/watcher"
)

const scanInterval = 2 * time.Minute

// Options configures how the app is started.
type Options struct {
	Headless bool
	NoOpen   bool
}

// App is the running desktop application.
type App struct {
	cfg          Config
	cfgPath      string
	log          *slog.Logger
	opts         Options
	historyStore *history.Store
	supervisor   *recorder.Supervisor
	watchers     *watcher.Manager
	hub          *stream.Hub
	tray         *trayIcon

	statusMu    sync.Mutex
	statusTimer *time.Timer

	watcherEngine string

	compaction         history.RetentionOptions
	compactionEnabled  bool
	compactionInterval time.Duration

	mu        sync.RWMutex
	projects  []ProjectStatus
	scanning  bool
	lastScan  time.Time
	scanError string

	activityMu sync.Mutex
	activity   map[string]Activity

	watchMu        sync.Mutex
	watchGroups    map[string]*watchGroup
	lastActive     map[string]time.Time
	stateDirty     bool
	statePath      string
	hibernateAfter time.Duration
	initialScan    bool

	started time.Time
	addr    string
	server  *http.Server
	quit    chan struct{}
	once    sync.Once
}

// New creates an app from an already loaded configuration.
func New(cfg Config, cfgPath string, opts Options, log *slog.Logger) (*App, error) {
	if log == nil {
		log = slog.Default()
	}

	historyRoot := cfg.History.Root
	if historyRoot == "" {
		var err error
		historyRoot, err = history.DefaultRoot()
		if err != nil {
			return nil, fmt.Errorf("resolve history root: %w", err)
		}
	}

	historyStore := history.New(historyRoot)
	historyStore.Diff = difft.Resolve(cfg.Diff.Tool, cfg.Diff.Path)
	lastActive, persistedActivity := loadActivityState(log)
	if historyStore.Diff.Enabled() {
		log.Info("difftastic diff enabled", "binary", historyStore.Diff.Path)
	} else if cfg.Diff.Tool == "difft" {
		log.Warn("diff.tool is difft but no difftastic binary was found; using git diff")
	}

	instance := &App{
		cfg:          cfg,
		cfgPath:      cfgPath,
		log:          log,
		opts:         opts,
		historyStore: historyStore,
		hub:          stream.NewHub(),
		tray:         &trayIcon{},
		projects:     []ProjectStatus{},
		quit:         make(chan struct{}),
		activity:     activityMap(persistedActivity),
		watchGroups:  make(map[string]*watchGroup),
		lastActive:   lastActive,
		statePath:    activityStatePath(),
		initialScan:  true,
	}
	instance.hibernateAfter = time.Duration(cfg.Watcher.HibernateAfterMS) * time.Millisecond
	compaction, compactionInterval, err := compactionOptions(cfg.History.Compaction)
	if err != nil {
		return nil, err
	}
	instance.compaction = compaction
	instance.compactionEnabled = cfg.History.Compaction.Enabled
	instance.compactionInterval = compactionInterval
	engine, binary, err := resolveWatcher(cfg.Watcher)
	if err != nil {
		return nil, err
	}
	instance.watcherEngine = engine
	instance.supervisor = recorder.New(recorder.Options{
		Interval:    time.Duration(cfg.Watcher.PollIntervalMS) * time.Millisecond,
		MaxInterval: time.Duration(cfg.Watcher.PollIntervalMaxMS) * time.Millisecond,
		Workers:     cfg.Watcher.Workers,
		Log:         log,
		OnEvent:     instance.recordRecorderEvent,
	})
	if engine == "watchexec" {
		instance.watchers = watcher.NewManager(watcher.Options{
			Binary:   binary,
			Debounce: time.Duration(cfg.Watcher.DebounceMS) * time.Millisecond,
			Ignore:   cfg.Watcher.Ignore,
			Log:      log,
			OnChange: instance.supervisor.Trigger,
			OnState:  instance.watchStateChanged,
		})
		log.Info("watchexec trigger enabled", "binary", binary)
	} else {
		reason := "watchexec not found"
		if cfg.Watcher.Engine == "poll" {
			reason = "configured"
		}
		log.Info("polling trigger active", "reason", reason)
	}

	return instance, nil
}

// resolveWatcher picks the trigger engine and the watchexec binary. Auto uses
// watchexec when it can be found and polling otherwise.
func resolveWatcher(cfg WatcherConfig) (string, string, error) {
	binary := watcher.Resolve(cfg.WatchexecPath)
	switch cfg.Engine {
	case "watchexec":
		if binary == "" {
			return "", "", fmt.Errorf("watcher.engine is watchexec but no watchexec binary was found; install watchexec, set watcher.watchexecPath, or use watcher.engine: poll")
		}
		return "watchexec", binary, nil
	case "poll":
		return "poll", "", nil
	default:
		if binary != "" {
			return "watchexec", binary, nil
		}
		return "poll", "", nil
	}
}

// Run starts the service and blocks until the process is asked to stop.
func (a *App) Run(ctx context.Context) error {
	a.started = time.Now()
	if a.watchers != nil {
		defer a.watchers.Close()
	}
	defer a.saveActivityState()

	if err := WriteDefaultConfig(a.cfgPath, a.cfg); err != nil {
		a.log.Warn("could not write default config", "err", err)
	}
	a.syncAutostart()

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", a.cfg.Server.Port))
	if err != nil {
		return fmt.Errorf("listen on 127.0.0.1:%d: %w", a.cfg.Server.Port, err)
	}
	a.addr = listener.Addr().String()
	a.server = &http.Server{
		Handler:           a.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := a.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	go a.scanLoop(ctx)
	go a.worktreeWatchLoop(ctx)
	go a.supervisor.Run(ctx)
	go a.hibernateLoop(ctx)
	go a.flushActivityLoop(ctx)
	go a.compactionLoop(ctx)

	a.log.Info("tachograph is listening", "url", a.uiURL(), "version", version.String())

	if !a.opts.NoOpen && a.cfg.UI.Open {
		go a.openURL()
	}

	if a.opts.Headless {
		select {
		case <-ctx.Done():
		case err := <-serveErr:
			return err
		}
	} else if err := a.runTray(ctx); err != nil {
		a.log.Warn("tray unavailable, serving without it", "err", err)
		select {
		case <-ctx.Done():
		case err := <-serveErr:
			return err
		}
	}

	a.shutdown()
	return nil
}

func (a *App) scanLoop(ctx context.Context) {
	if err := a.Scan(ctx); err != nil {
		a.log.Warn("initial scan failed", "err", err)
	}
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.Scan(ctx); err != nil {
				a.log.Warn("scan failed", "err", err)
			}
		}
	}
}

// Scan merges two sources: the existing history stores (so known projects are
// visible immediately, even without a root) and a fresh discovery scan below
// the configured roots (so new projects and worktrees appear automatically).
func (a *App) Scan(ctx context.Context) error {
	a.mu.Lock()
	if a.scanning {
		a.mu.Unlock()
		return nil
	}
	a.scanning = true
	roots := append([]string(nil), a.cfg.Roots...)
	patterns := append([]string(nil), a.cfg.Worktrees.Patterns...)
	actions := append([]LifecycleAction(nil), a.cfg.Worktrees.OnCreate...)
	a.scanError = ""
	a.mu.Unlock()
	a.notifyStatus()

	started := time.Now()
	mapped := mapActions(actions)
	hash := actionsHash(actions)
	var scanErrors []error

	projectsByKey := make(map[string]*ProjectStatus)
	activePaths := make(map[string]bool)
	type worktreeLocation struct {
		projectKey string
		index      int
	}
	worktreeLocations := make(map[string]worktreeLocation)

	ensureProject := func(key, name, commonDir, mainWorktree string) *ProjectStatus {
		if existing, ok := projectsByKey[key]; ok {
			if existing.Name == "" {
				existing.Name = name
			}
			if existing.CommonDir == "" {
				existing.CommonDir = commonDir
			}
			if existing.MainWorktree == "" {
				existing.MainWorktree = mainWorktree
			}
			return existing
		}
		project := &ProjectStatus{
			Name:         name,
			CommonDir:    commonDir,
			MainWorktree: mainWorktree,
			Worktrees:    []WorktreeStatus{},
		}
		projectsByKey[key] = project
		return project
	}

	addWorktree := func(key string, project *ProjectStatus, worktree WorktreeStatus) {
		if worktree.Path == "" {
			return
		}
		if _, ok := worktreeLocations[worktree.Path]; ok {
			return
		}
		project.Worktrees = append(project.Worktrees, worktree)
		worktreeLocations[worktree.Path] = worktreeLocation{
			projectKey: key,
			index:      len(project.Worktrees) - 1,
		}
	}

	buildStatus := func(recorder *history.Recorder) WorktreeStatus {
		status := WorktreeStatus{
			ID:          recorder.Worktree.ID,
			Path:        recorder.Worktree.Path,
			Branch:      recorder.Worktree.Branch,
			Head:        recorder.Worktree.Head,
			HistoryRepo: recorder.HistoryRepo,
			Placeholder: recorder.Placeholder,
			State:       "ready",
		}
		if len(mapped) > 0 {
			setup, err := lifecycle.Apply(ctx, recorder, lifecycle.Options{
				Actions: mapped,
				Hash:    hash,
				Log:     a.log,
			})
			if err != nil {
				status.State = "setup-error"
				status.SetupState = "error"
				status.Error = err.Error()
			} else {
				status.Setup = setupLabel(setup)
				status.SetupState = "ok"
			}
		} else {
			status.SetupState = "none"
		}
		return status
	}

	// Existing history stores first: they are the durable registry and make the
	// UI useful before any root has been configured.
	known, err := a.historyStore.List(ctx)
	if err != nil {
		scanErrors = append(scanErrors, err)
		a.log.Warn("history store scan failed", "err", err)
	}
	for _, entry := range known {
		// A worktree that no longer exists must not linger as a "missing" card.
		// Remove its placeholder from the history store; the history branch stays,
		// so recreating the worktree at the same path resumes its snapshots.
		if _, err := os.Stat(entry.Path); err != nil {
			a.supervisor.Remove(entry.Path)
			a.forgetWorktree(entry.Path)
			if err := a.historyStore.RemoveWorktree(ctx, entry); err != nil {
				a.log.Warn("could not remove deleted worktree", "path", entry.Path, "err", err)
			} else {
				a.log.Info("removed deleted worktree", "path", entry.Path)
			}
			continue
		}

		key := entry.CommonDir
		if key == "" {
			key = "history:" + entry.ProjectDir
		}
		project := ensureProject(key, projectNameFromDir(entry.ProjectDir), entry.CommonDir, mainWorktreeFromCommonDir(entry.CommonDir))
		if project.HistoryRepo == "" {
			project.HistoryRepo = filepath.Join(a.historyStore.Root, entry.ProjectDir, "repo")
		}
		status := WorktreeStatus{
			ID:          entry.ID,
			Path:        entry.Path,
			Branch:      entry.Branch,
			HistoryRepo: filepath.Join(a.historyStore.Root, entry.ProjectDir, "repo"),
			Placeholder: filepath.Join(a.historyStore.Root, entry.ProjectDir, "worktrees", entry.WorktreeDir),
			State:       "ready",
		}
		if entry.SetupHash != "" {
			status.Setup = "setup ok"
			status.SetupState = "ok"
		} else {
			status.SetupState = "none"
		}

		recorder := a.historyStore.RecorderFromKnown(entry)
		if !a.supervisor.Has(entry.Path) {
			summary, err := recorder.Summary(ctx)
			if err != nil {
				a.log.Debug("history summary unavailable", "path", entry.Path, "err", err)
			}
			a.observeLastSnapshot(entry.Path, summary.LastSnapshot)
			a.supervisor.Set(entry.Path, recorder)
			a.supervisor.Seed(entry.Path, summary.Snapshots, summary.LastSnapshot, summary.LastHash)
		}
		activePaths[entry.Path] = true
		addWorktree(key, project, status)
	}

	// Publish the known projects before discovery. A slow root scan must not keep
	// the durable registry off the screen.
	a.mu.Lock()
	a.projects = projectList(projectsByKey)
	a.mu.Unlock()

	// Then discovery: roots contribute new projects and worktrees. Worktrees
	// already known from the history store are de-duplicated by path.
	result := discovery.Scan(ctx, roots, patterns, a.log)
	scanErrors = append(scanErrors, result.Errors...)
	for _, project := range result.Projects {
		key := project.CommonDir
		if key == "" {
			key = "name:" + project.Name
		}
		status := ensureProject(key, project.Name, project.CommonDir, project.MainWorktree)
		for _, worktree := range project.Worktrees {
			if location, ok := worktreeLocations[worktree.Path]; ok {
				activePaths[worktree.Path] = true
				if existing, ok := projectsByKey[location.projectKey]; ok && location.index < len(existing.Worktrees) {
					if worktree.Branch != "" {
						existing.Worktrees[location.index].Branch = worktree.Branch
					}
					if worktree.Head != "" {
						existing.Worktrees[location.index].Head = worktree.Head
					}
				}
				continue
			}
			recorder, err := a.historyStore.Ensure(ctx, worktree.Path)
			if err != nil {
				a.supervisor.Remove(worktree.Path)
				addWorktree(key, status, WorktreeStatus{
					Path:   worktree.Path,
					Branch: worktree.Branch,
					Head:   worktree.Head,
					State:  "error",
					Error:  err.Error(),
				})
				continue
			}
			if status.HistoryRepo == "" {
				status.HistoryRepo = recorder.HistoryRepo
			}
			if !a.supervisor.Has(recorder.Worktree.Path) {
				summary, err := recorder.Summary(ctx)
				if err != nil {
					a.log.Debug("history summary unavailable", "path", recorder.Worktree.Path, "err", err)
				}
				a.observeLastSnapshot(recorder.Worktree.Path, summary.LastSnapshot)
				a.supervisor.Set(recorder.Worktree.Path, recorder)
				a.supervisor.Seed(recorder.Worktree.Path, summary.Snapshots, summary.LastSnapshot, summary.LastHash)
			}
			// A worktree that needed a new history store is new to the app:
			// treat it as active so its watcher starts immediately.
			a.markActive(recorder.Worktree.Path, time.Now())
			activePaths[recorder.Worktree.Path] = true
			addWorktree(key, status, buildStatus(recorder))
		}
	}

	a.supervisor.Retain(activePaths)
	a.updateWatchGroups(projectsByKey)
	a.initialScan = false

	projects := projectList(projectsByKey)

	errorsText := ""
	if len(scanErrors) > 0 {
		parts := make([]string, 0, len(scanErrors))
		for _, err := range scanErrors {
			a.log.Warn("scan error", "err", err)
			parts = append(parts, err.Error())
		}
		errorsText = strings.Join(parts, "; ")
	}

	a.mu.Lock()
	a.projects = projects
	a.scanning = false
	a.lastScan = time.Now()
	a.scanError = errorsText
	a.mu.Unlock()

	a.notifyStatus()
	a.log.Info("scan complete",
		"projects", len(projects),
		"errors", len(scanErrors),
		"duration", time.Since(started).String(),
	)
	return nil
}

func projectList(projectsByKey map[string]*ProjectStatus) []ProjectStatus {
	projects := make([]ProjectStatus, 0, len(projectsByKey))
	for _, project := range projectsByKey {
		worktrees := append([]WorktreeStatus(nil), project.Worktrees...)
		sort.Slice(worktrees, func(i, j int) bool {
			return worktrees[i].Path < worktrees[j].Path
		})
		snapshot := *project
		snapshot.Worktrees = worktrees
		projects = append(projects, snapshot)
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Name == projects[j].Name {
			return projects[i].CommonDir < projects[j].CommonDir
		}
		return projects[i].Name < projects[j].Name
	})
	return projects
}

func projectNameFromDir(dir string) string {
	index := strings.LastIndex(dir, "-")
	if index > 0 && len(dir)-index-1 == 8 && isHex(dir[index+1:]) {
		return dir[:index]
	}
	return dir
}

// mainWorktreeFromCommonDir derives the main worktree from a project's common
// git directory. For a normal repository that is the parent of ".git"; bare
// repositories have no such parent and return "".
func mainWorktreeFromCommonDir(commonDir string) string {
	if filepath.Base(commonDir) != ".git" {
		return ""
	}
	parent := filepath.Dir(commonDir)
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return ""
	}
	return parent
}

func isHex(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return value != ""
}

func mapActions(actions []LifecycleAction) []lifecycle.Action {
	mapped := make([]lifecycle.Action, 0, len(actions))
	for _, action := range actions {
		var item lifecycle.Action
		if action.Copy != nil {
			item.Copy = &lifecycle.CopyAction{
				Path:      action.Copy.Path,
				Optional:  action.Copy.Optional,
				Overwrite: action.Copy.Overwrite,
			}
		}
		if action.Run != nil {
			item.Run = &lifecycle.RunAction{
				Command: action.Run.Command,
				OnlyIf:  action.Run.OnlyIf,
				Timeout: action.Run.Timeout,
			}
		}
		mapped = append(mapped, item)
	}
	return mapped
}

func actionsHash(actions []LifecycleAction) string {
	data, err := json.Marshal(actions)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

func setupLabel(result lifecycle.Result) string {
	if result.Skipped {
		// The setup marker is present: the actions ran when the worktree first
		// appeared and are not repeated.
		return "setup ok"
	}
	var parts []string
	if len(result.Copied) > 5 {
		parts = append(parts, fmt.Sprintf("copied %d files", len(result.Copied)))
	} else if len(result.Copied) > 0 {
		parts = append(parts, "copied "+strings.Join(result.Copied, ", "))
	}
	if len(result.Ran) > 0 {
		parts = append(parts, "ran "+strings.Join(result.Ran, ", "))
	}
	if len(parts) == 0 {
		return "setup ok"
	}
	return strings.Join(parts, "; ")
}

// uiURL is the address the tray and the CLI open.
func (a *App) uiURL() string {
	return "http://" + a.addr + "/"
}

func (a *App) toggleRecording() {
	if a.supervisor.Paused() {
		a.supervisor.Resume()
		a.log.Info("recording resumed")
		a.notifyStatus()
		return
	}
	a.supervisor.Pause()
	a.log.Info("recording paused")
	a.notifyStatus()
}

// requestQuit closes the quit channel exactly once.
func (a *App) requestQuit() {
	a.once.Do(func() { close(a.quit) })
}

// shutdown stops the HTTP server with a short grace period.
func (a *App) shutdown() {
	if a.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.server.Shutdown(ctx); err != nil {
		a.log.Warn("server shutdown failed", "err", err)
	}
}
