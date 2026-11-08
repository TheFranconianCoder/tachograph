package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjectConfig(t *testing.T, mainWorktree, content string) {
	t.Helper()
	path := filepath.Join(mainWorktree, ProjectConfigFile)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
}

func TestProjectConfigPath(t *testing.T) {
	if got := ProjectConfigPath(""); got != "" {
		t.Errorf("ProjectConfigPath(\"\") = %q, want empty", got)
	}
	want := filepath.Join("/repo", ProjectConfigFile)
	if got := ProjectConfigPath("/repo"); got != want {
		t.Errorf("ProjectConfigPath(\"/repo\") = %q, want %q", got, want)
	}
}

func TestLoadProjectConfigMissing(t *testing.T) {
	for _, mainWorktree := range []string{"", t.TempDir()} {
		cfg, err := LoadProjectConfig(mainWorktree)
		if err != nil {
			t.Fatalf("LoadProjectConfig(%q): %v", mainWorktree, err)
		}
		if len(cfg.Copy) != 0 {
			t.Errorf("LoadProjectConfig(%q) = %+v, want empty", mainWorktree, cfg)
		}
	}
}

func TestLoadProjectConfigCopies(t *testing.T) {
	mainWorktree := t.TempDir()
	writeProjectConfig(t, mainWorktree, `copy:
  - .env.production
  - config/*.yaml
  - path: secrets/required.key
    optional: false
  - path: generated/config.yaml
    overwrite: true
`)

	cfg, err := LoadProjectConfig(mainWorktree)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	actions, err := cfg.Actions()
	if err != nil {
		t.Fatalf("Actions: %v", err)
	}
	if len(actions) != 4 {
		t.Fatalf("Actions() returned %d entries, want 4", len(actions))
	}

	// Scalar entries default to optional.
	for _, index := range []int{0, 1} {
		if actions[index].Copy == nil || !actions[index].Copy.Optional {
			t.Errorf("actions[%d] = %+v, want optional copy", index, actions[index])
		}
	}
	if got := actions[0].Copy.Path; got != ".env.production" {
		t.Errorf("actions[0].Path = %q", got)
	}
	if got := actions[3].Copy.Path; got != "generated/config.yaml" || !actions[3].Copy.Overwrite {
		t.Errorf("actions[3] = %+v, want overwrite copy", actions[3].Copy)
	}
	if actions[2].Copy.Optional {
		t.Errorf("actions[2] = %+v, want required copy", actions[2].Copy)
	}
}

func TestLoadProjectConfigRejectsInvalidFiles(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"invalid yaml", "copy: ["},
		{"absolute path", "copy:\n  - /etc/passwd\n"},
		{"parent escape", "copy:\n  - ../secret\n"},
		{"git directory", "copy:\n  - .git/config\n"},
		{"empty path", "copy:\n  - \"\"\n"},
		{"duplicate path", "copy:\n  - .env\n  - .env\n"},
		{"unknown setting", "copyy:\n  - .env\n"},
		{"unknown entry field", "copy:\n  - path: .env\n    overwrit: true\n"},
		{"global worktrees block", "worktrees:\n  on_create: []\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			mainWorktree := t.TempDir()
			writeProjectConfig(t, mainWorktree, test.content)
			if _, err := LoadProjectConfig(mainWorktree); err == nil {
				t.Fatal("LoadProjectConfig accepted an invalid file")
			}
		})
	}
}

func TestMergeLifecycleActions(t *testing.T) {
	global := []LifecycleAction{
		{Copy: &CopyAction{Path: ".env", Optional: true}},
		{Copy: &CopyAction{Path: "config/*.yaml", Optional: true}},
		{Run: &RunAction{Command: "task setup"}},
	}
	project := []LifecycleAction{
		{Copy: &CopyAction{Path: "./.env", Overwrite: true}},
		{Copy: &CopyAction{Path: "deploy/*.yaml", Optional: true}},
	}

	merged := mergeLifecycleActions(global, project)
	if len(merged) != 4 {
		t.Fatalf("merged %d actions, want 4: %+v", len(merged), merged)
	}
	if merged[0].Copy == nil || !merged[0].Copy.Overwrite || merged[0].Copy.Optional {
		t.Errorf("merged[0] = %+v, want the project .env override", merged[0].Copy)
	}
	if merged[1].Copy == nil || merged[1].Copy.Path != "config/*.yaml" {
		t.Errorf("merged[1] = %+v, want the global config glob", merged[1].Copy)
	}
	if merged[2].Run == nil || merged[2].Run.Command != "task setup" {
		t.Errorf("merged[2] = %+v, want the global run action", merged[2])
	}
	if merged[3].Copy == nil || merged[3].Copy.Path != "deploy/*.yaml" {
		t.Errorf("merged[3] = %+v, want the project glob", merged[3].Copy)
	}
}

func TestLoadProjectActions(t *testing.T) {
	mainWorktree := t.TempDir()
	writeProjectConfig(t, mainWorktree, `copy:
  - extras/*
`)

	actions, err := loadProjectActions(mainWorktree)
	if err != nil {
		t.Fatalf("loadProjectActions: %v", err)
	}
	if len(actions) != 1 || actions[0].Copy == nil || actions[0].Copy.Path != "extras/*" {
		t.Fatalf("loadProjectActions = %+v", actions)
	}
	if !actions[0].Copy.Optional {
		t.Errorf("project copy is not optional by default")
	}
}

func TestLoadProjectConfigErrorMentionsFile(t *testing.T) {
	mainWorktree := t.TempDir()
	writeProjectConfig(t, mainWorktree, "copy:\n  - ../secret\n")

	_, err := LoadProjectConfig(mainWorktree)
	if err == nil {
		t.Fatal("LoadProjectConfig accepted an escape")
	}
	if !strings.Contains(err.Error(), ProjectConfigFile) {
		t.Errorf("error %q does not mention %s", err, ProjectConfigFile)
	}
}
