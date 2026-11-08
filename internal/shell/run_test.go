package shell

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestEnvironmentSetsMarkers(t *testing.T) {
	t.Setenv(EnvProject, "old")
	candidate := Candidate{
		Project:      "alpha",
		Path:         "/code/alpha",
		ID:           "a1",
		Branch:       "main",
		MainWorktree: "/code/alpha",
		HistoryRepo:  "/hist/alpha",
	}

	values := envValues(environment(candidate))
	want := map[string]string{
		EnvProject:      "alpha",
		EnvWorktree:     "/code/alpha",
		EnvWorktreeID:   "a1",
		EnvBranch:       "main",
		EnvMainWorktree: "/code/alpha",
		EnvHistoryRepo:  "/hist/alpha",
	}
	for key, value := range want {
		if values[key] != value {
			t.Errorf("%s = %q, want %q", key, values[key], value)
		}
	}
}

func TestEnvironmentKeepsInheritedEmptyMarkers(t *testing.T) {
	t.Setenv(EnvWorktreeID, "inherited")
	candidate := Candidate{Project: "alpha", Path: "/code/alpha"}

	values := envValues(environment(candidate))
	if values[EnvWorktreeID] != "inherited" {
		t.Errorf("%s = %q, want inherited", EnvWorktreeID, values[EnvWorktreeID])
	}
}

func TestEnvironmentHasNoDuplicateMarkers(t *testing.T) {
	candidate := Candidate{Project: "alpha", Path: "/code/alpha", ID: "a1"}
	counts := make(map[string]int)
	for _, entry := range environment(candidate) {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			counts[key]++
		}
	}
	for key, count := range counts {
		if count > 1 {
			t.Errorf("environment contains %s %d times", key, count)
		}
	}
}

func TestBanner(t *testing.T) {
	candidate := Candidate{
		Project:  "alpha",
		Worktree: "alpha--feature",
		Path:     "/code/alpha--feature",
		Branch:   "feature/x",
	}

	plain := Banner(candidate, false)
	for _, want := range []string{"tachograph shell", "alpha", "alpha--feature", "feature/x", "/code/alpha--feature"} {
		if !strings.Contains(plain, want) {
			t.Errorf("Banner() = %q, missing %q", plain, want)
		}
	}
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("plain banner contains ANSI escapes: %q", plain)
	}

	lines := strings.Split(plain, "\n")
	if len(lines) < 6 {
		t.Fatalf("Banner() has %d lines, want at least 6", len(lines))
	}
	width := utf8.RuneCountInString(lines[0])
	for index, line := range lines {
		if got := utf8.RuneCountInString(line); got != width {
			t.Errorf("line %d width = %d, want %d: %q", index, got, width, line)
		}
	}

	colored := Banner(candidate, true)
	if !strings.Contains(colored, ansiCyan) || !strings.Contains(colored, ansiReset) {
		t.Errorf("colored banner has no ANSI styles: %q", colored)
	}
}

func TestRunRejectsMissingWorktree(t *testing.T) {
	candidate := Candidate{Path: filepath.Join(t.TempDir(), "missing")}
	err := Run(candidate)
	if err == nil || !strings.Contains(err.Error(), "worktree") {
		t.Fatalf("Run() error = %v, want missing-worktree error", err)
	}
}

func envValues(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}
