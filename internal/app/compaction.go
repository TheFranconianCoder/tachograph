package app

import (
	"context"
	"fmt"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/history"
)

// compactionOptions converts the config strings into the retention policy.
// Empty values fall back to the defaults so a directly constructed Config also
// works.
func compactionOptions(cfg CompactionConfig) (history.RetentionOptions, time.Duration, error) {
	parse := func(name, value, fallback string) (time.Duration, error) {
		if value == "" {
			value = fallback
		}
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return 0, fmt.Errorf("history.compaction.%s: %w", name, err)
		}
		if parsed < 0 {
			parsed = 0
		}
		return parsed, nil
	}

	interval, err := parse("interval", cfg.Interval, "1h")
	if err != nil {
		return history.RetentionOptions{}, 0, err
	}
	keepAll, err := parse("keepAllFor", cfg.KeepAllFor, "24h")
	if err != nil {
		return history.RetentionOptions{}, 0, err
	}
	hourly, err := parse("hourlyFor", cfg.HourlyFor, "168h")
	if err != nil {
		return history.RetentionOptions{}, 0, err
	}
	daily, err := parse("dailyFor", cfg.DailyFor, "720h")
	if err != nil {
		return history.RetentionOptions{}, 0, err
	}
	maxAge, err := parse("maxAge", cfg.MaxAge, "2160h")
	if err != nil {
		return history.RetentionOptions{}, 0, err
	}

	if interval <= 0 {
		interval = time.Hour
	}
	keepMinimum := cfg.KeepMinimum
	if keepMinimum < 0 {
		keepMinimum = 0
	}
	if hourly < keepAll {
		hourly = keepAll
	}
	if daily < hourly {
		daily = hourly
	}
	if maxAge > 0 && maxAge < daily {
		maxAge = daily
	}

	return history.RetentionOptions{
		KeepMinimum: keepMinimum,
		KeepAllFor:  keepAll,
		HourlyFor:   hourly,
		DailyFor:    daily,
		MaxAge:      maxAge,
		Archive:     cfg.Archive,
		AutoGC:      cfg.AutoGC,
	}, interval, nil
}

func (a *App) compactionLoop(ctx context.Context) {
	if !a.compactionEnabled {
		return
	}

	// Let the first scan settle, then compact what is already on disk. A short
	// configured interval also shortens the first run.
	delay := a.compactionInterval
	if delay > time.Minute {
		delay = time.Minute
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	a.compactAll(ctx)

	ticker := time.NewTicker(a.compactionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.compactAll(ctx)
		}
	}
}

func (a *App) compactAll(ctx context.Context) {
	entries, err := a.historyStore.List(ctx)
	if err != nil {
		a.log.Warn("compaction scan failed", "err", err)
		return
	}

	projects := make(map[string]bool)
	for _, entry := range entries {
		projects[entry.ProjectDir] = true
	}
	for project := range projects {
		result, err := a.historyStore.CompactProject(ctx, project, a.compaction)
		if err != nil {
			a.log.Warn("history compaction failed", "project", project, "err", err)
			continue
		}
		switch {
		case result.Rewritten > 0:
			a.log.Info("history compacted",
				"project", project,
				"branches", result.Branches,
				"rewritten", result.Rewritten,
				"kept", result.Kept,
				"dropped", result.Dropped,
				"gc", result.GarbageCollected,
			)
			a.refreshSnapshots(ctx, project)
		case result.GarbageCollected:
			a.log.Debug("history packed", "project", project, "branches", result.Branches)
		}
	}
	a.notifyStatus()
}

// refreshSnapshots updates the supervisor counters after a branch rewrite. The
// tip is unchanged, so the visible state stays the same.
func (a *App) refreshSnapshots(ctx context.Context, project string) {
	entries, err := a.historyStore.List(ctx)
	if err != nil {
		a.log.Warn("compaction refresh failed", "project", project, "err", err)
		return
	}
	for _, entry := range entries {
		if entry.ProjectDir != project {
			continue
		}
		recorder := a.historyStore.RecorderFromKnown(entry)
		summary, err := recorder.Summary(ctx)
		if err != nil {
			a.log.Debug("summary after compaction unavailable", "path", entry.Path, "err", err)
			continue
		}
		a.supervisor.Resync(entry.Path, summary.Snapshots, summary.LastSnapshot, summary.LastHash)
	}
}
