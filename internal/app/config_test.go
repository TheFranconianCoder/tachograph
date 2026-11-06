package app

import (
	"strings"
	"testing"
	"time"
)

func TestResolveWatcherForcedPoll(t *testing.T) {
	engine, binary, err := resolveWatcher(WatcherConfig{Engine: "poll"})
	if err != nil {
		t.Fatalf("resolveWatcher(poll): %v", err)
	}
	if engine != "poll" || binary != "" {
		t.Errorf("resolveWatcher(poll) = %q, %q; want poll, empty", engine, binary)
	}
}

func TestResolveWatcherMissingForcedBinary(t *testing.T) {
	_, _, err := resolveWatcher(WatcherConfig{
		Engine:        "watchexec",
		WatchexecPath: "/nonexistent/watchexec",
	})
	if err == nil {
		t.Fatal("resolveWatcher(watchexec, missing path) returned no error")
	}
	if !strings.Contains(err.Error(), "watchexec") {
		t.Errorf("error = %q, want a watchexec hint", err)
	}
}

func TestLoadConfigClampsPollIntervals(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Watcher.PollIntervalMS = 30000
	cfg.Watcher.PollIntervalMaxMS = 1000

	path := t.TempDir() + "/config.yaml"
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Watcher.PollIntervalMaxMS != loaded.Watcher.PollIntervalMS {
		t.Errorf("max interval = %d, want it clamped to %d",
			loaded.Watcher.PollIntervalMaxMS, loaded.Watcher.PollIntervalMS)
	}
	if loaded.Watcher.Engine != "auto" {
		t.Errorf("engine = %q, want auto", loaded.Watcher.Engine)
	}
}

func TestCompactionOptionsDefaultsAndClamping(t *testing.T) {
	opts, interval, err := compactionOptions(CompactionConfig{})
	if err != nil {
		t.Fatalf("compactionOptions(default): %v", err)
	}
	if interval != time.Hour {
		t.Errorf("interval = %s, want 1h", interval)
	}
	if opts.KeepAllFor != 24*time.Hour || opts.HourlyFor != 168*time.Hour {
		t.Errorf("retention defaults = %+v", opts)
	}

	clamped, _, err := compactionOptions(CompactionConfig{
		KeepAllFor: "24h",
		HourlyFor:  "1h",
		DailyFor:   "2h",
		MaxAge:     "1h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if clamped.HourlyFor != 24*time.Hour {
		t.Errorf("hourly = %s, want it clamped to 24h", clamped.HourlyFor)
	}
	if clamped.DailyFor != 24*time.Hour {
		t.Errorf("daily = %s, want it clamped to 24h", clamped.DailyFor)
	}
	if clamped.MaxAge != 24*time.Hour {
		t.Errorf("maxAge = %s, want it clamped to 24h", clamped.MaxAge)
	}
}

func TestLoadConfigRejectsInvalidCompactionDuration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.History.Compaction.KeepAllFor = "daily"

	path := t.TempDir() + "/config.yaml"
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("LoadConfig accepted an invalid duration")
	}
}
