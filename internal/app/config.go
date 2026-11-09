package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultPort              = 9211
	defaultDebounceMS        = 1500
	defaultPollIntervalMS    = 10000
	defaultPollIntervalMaxMS = 120000
	defaultHibernateAfterMS  = 300000
	defaultRecorderWorkers   = 4
	defaultWatcherEngine     = "auto"
)

// Config is the on-disk configuration.
type Config struct {
	Server    ServerConfig   `yaml:"server"`
	UI        UIConfig       `yaml:"ui"`
	Watcher   WatcherConfig  `yaml:"watcher"`
	Worktrees WorktreeConfig `yaml:"worktrees"`
	History   HistoryConfig  `yaml:"history"`
	Diff      DiffConfig     `yaml:"diff"`
	// Autostart keeps a per-user login entry for the tray app. The entry starts
	// the app without opening the UI.
	Autostart bool     `yaml:"autostart"`
	Roots     []string `yaml:"roots"`
}

// DiffConfig selects the renderer for snapshot patches.
type DiffConfig struct {
	// Tool is auto (prefer delta, then difftastic), git (unified diff always),
	// delta or difft.
	Tool string `yaml:"tool"`
	// Path is an optional explicit binary for the selected renderer.
	Path string `yaml:"path,omitempty"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type UIConfig struct {
	Open bool `yaml:"open"`
	// ActiveOnly keeps only projects with relevant worktree state in the API
	// and UI payload.
	ActiveOnly bool `yaml:"activeOnly"`
}

type WatcherConfig struct {
	// Engine selects the trigger: auto uses watchexec when it is available and
	// polling otherwise; watchexec and poll force one path.
	Engine            string `yaml:"engine"`
	WatchexecPath     string `yaml:"watchexecPath,omitempty"`
	DebounceMS        int    `yaml:"debounceMs"`
	PollIntervalMS    int    `yaml:"pollIntervalMs"`
	PollIntervalMaxMS int    `yaml:"pollIntervalMaxMs"`
	// HibernateAfterMS parks a project's watcher after this much silence and
	// returns it to polling. Zero disables hibernation.
	HibernateAfterMS int      `yaml:"hibernateAfterMs"`
	Workers          int      `yaml:"workers"`
	Ignore           []string `yaml:"ignore"`
}

// WorktreeConfig holds lifecycle actions. They run once when a worktree is
// discovered, before the first snapshot.
type WorktreeConfig struct {
	OnCreate []LifecycleAction `yaml:"on_create"`
	// Patterns locate worktrees outside the configured roots. Entries are
	// absolute glob patterns or start with ~.
	Patterns []string `yaml:"patterns,omitempty"`
}

type LifecycleAction struct {
	Copy *CopyAction `yaml:"copy,omitempty"`
	Run  *RunAction  `yaml:"run,omitempty"`
}

type CopyAction struct {
	Path      string `yaml:"path"`
	Optional  bool   `yaml:"optional,omitempty"`
	Overwrite bool   `yaml:"overwrite,omitempty"`
}

type RunAction struct {
	Command string `yaml:"command"`
	OnlyIf  string `yaml:"only_if,omitempty"`
	Timeout string `yaml:"timeout,omitempty"`
}

// HistoryConfig controls where the Git-backed history stores live and how old
// snapshots are compacted.
type HistoryConfig struct {
	Root       string           `yaml:"root,omitempty"`
	Compaction CompactionConfig `yaml:"compaction"`
}

// CompactionConfig thins old snapshots and packs the history repositories.
type CompactionConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Interval    string `yaml:"interval"`
	AutoGC      bool   `yaml:"autoGc"`
	KeepMinimum int    `yaml:"keepMinimum"`
	KeepAllFor  string `yaml:"keepAllFor"`
	HourlyFor   string `yaml:"hourlyFor"`
	DailyFor    string `yaml:"dailyFor"`
	MaxAge      string `yaml:"maxAge"`
	Archive     bool   `yaml:"archive"`
}

// DefaultConfig returns the configuration a fresh install starts with.
func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{Port: defaultPort},
		UI:     UIConfig{Open: true},
		Watcher: WatcherConfig{
			Engine:            defaultWatcherEngine,
			DebounceMS:        defaultDebounceMS,
			PollIntervalMS:    defaultPollIntervalMS,
			PollIntervalMaxMS: defaultPollIntervalMaxMS,
			HibernateAfterMS:  defaultHibernateAfterMS,
			Workers:           defaultRecorderWorkers,
			Ignore: []string{
				".git",
				"node_modules",
				"dist",
				"build",
				".cache",
			},
		},
		Worktrees: WorktreeConfig{OnCreate: defaultLifecycleActions(), Patterns: []string{}},
		Diff:      DiffConfig{Tool: "auto"},
		Autostart: false,
		History: HistoryConfig{
			Compaction: CompactionConfig{
				Enabled:     true,
				Interval:    "1h",
				AutoGC:      true,
				KeepMinimum: 50,
				KeepAllFor:  "24h",
				HourlyFor:   "168h",
				DailyFor:    "720h",
				MaxAge:      "2160h",
				Archive:     true,
			},
		},
		Roots: []string{},
	}
}

func defaultLifecycleActions() []LifecycleAction {
	return []LifecycleAction{
		{Copy: &CopyAction{Path: ".env", Optional: true}},
		{Copy: &CopyAction{Path: ".env.local", Optional: true}},
		{Copy: &CopyAction{Path: ".env.*.local", Optional: true}},
	}
}

// ConfigPath returns the per-user configuration file path.
func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(dir, "tachograph", "config.yaml"), nil
}

// LoadConfig reads the configuration, returning defaults when the file does not
// exist yet. A malformed file is an error: silently ignoring it would make the
// app behave differently than the user wrote down.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = defaultPort
	}
	if cfg.Watcher.DebounceMS <= 0 {
		cfg.Watcher.DebounceMS = defaultDebounceMS
	}
	cfg.Watcher.Engine = strings.ToLower(strings.TrimSpace(cfg.Watcher.Engine))
	switch cfg.Watcher.Engine {
	case "watchexec", "poll":
	case "auto", "":
		cfg.Watcher.Engine = defaultWatcherEngine
	default:
		cfg.Watcher.Engine = defaultWatcherEngine
	}
	if cfg.Watcher.PollIntervalMS <= 0 {
		cfg.Watcher.PollIntervalMS = defaultPollIntervalMS
	}
	if cfg.Watcher.PollIntervalMaxMS <= 0 {
		cfg.Watcher.PollIntervalMaxMS = defaultPollIntervalMaxMS
	}
	if cfg.Watcher.PollIntervalMaxMS < cfg.Watcher.PollIntervalMS {
		cfg.Watcher.PollIntervalMaxMS = cfg.Watcher.PollIntervalMS
	}
	if cfg.Watcher.HibernateAfterMS < 0 {
		cfg.Watcher.HibernateAfterMS = 0
	}
	if cfg.Watcher.Workers <= 0 {
		cfg.Watcher.Workers = defaultRecorderWorkers
	}
	if cfg.Watcher.Ignore == nil {
		cfg.Watcher.Ignore = DefaultConfig().Watcher.Ignore
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"interval", cfg.History.Compaction.Interval},
		{"keepAllFor", cfg.History.Compaction.KeepAllFor},
		{"hourlyFor", cfg.History.Compaction.HourlyFor},
		{"dailyFor", cfg.History.Compaction.DailyFor},
		{"maxAge", cfg.History.Compaction.MaxAge},
	} {
		if _, err := time.ParseDuration(field.value); err != nil {
			return cfg, fmt.Errorf("history.compaction.%s: %w", field.name, err)
		}
	}
	if cfg.History.Compaction.KeepMinimum < 0 {
		cfg.History.Compaction.KeepMinimum = 0
	}
	if cfg.Worktrees.OnCreate == nil {
		cfg.Worktrees.OnCreate = defaultLifecycleActions()
	}
	if cfg.Worktrees.Patterns == nil {
		cfg.Worktrees.Patterns = []string{}
	}
	cfg.Diff.Tool = strings.ToLower(strings.TrimSpace(cfg.Diff.Tool))
	switch cfg.Diff.Tool {
	case "git", "difft", "delta":
	case "auto", "":
		cfg.Diff.Tool = "auto"
	default:
		cfg.Diff.Tool = "auto"
	}
	cfg.Diff.Path = strings.TrimSpace(cfg.Diff.Path)
	if cfg.Roots == nil {
		cfg.Roots = []string{}
	}

	return cfg, nil
}

// SaveConfig writes the configuration atomically.
func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace config %s: %w", path, err)
	}
	return nil
}

// WriteDefaultConfig writes the default configuration if the file is missing.
func WriteDefaultConfig(path string, cfg Config) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return SaveConfig(path, cfg)
}
