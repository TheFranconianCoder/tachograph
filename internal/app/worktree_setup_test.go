package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheFranconianCoder/tachograph/internal/history"
)

func TestWorktreeSetupHandlerPersistsCopyRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	cfg.Worktrees.OnCreate = append(cfg.Worktrees.OnCreate,
		LifecycleAction{Run: &RunAction{Command: "task setup", OnlyIf: "Taskfile.yml"}})
	app := &App{cfg: cfg, cfgPath: path}

	body := `{"copy":[".env",".env.*.local","config/*.local",".env"],"overwrite":true}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/lifecycle", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	app.handleWorktreeSetup(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response WorktreeSetupStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Copy) != 3 {
		t.Fatalf("copy rules = %+v, want 3 deduplicated rules", response.Copy)
	}
	for _, rule := range response.Copy {
		if !rule.Optional || !rule.Overwrite {
			t.Errorf("rule = %+v, want optional and overwrite", rule)
		}
	}
	if len(response.Run) != 1 || response.Run[0].Command != "task setup" {
		t.Errorf("run rules = %+v, want the preserved setup command", response.Run)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	copies, runs := 0, 0
	for _, action := range loaded.Worktrees.OnCreate {
		if action.Copy != nil {
			copies++
		}
		if action.Run != nil {
			runs++
		}
	}
	if copies != 3 || runs != 1 {
		t.Errorf("persisted actions = %d copies, %d runs; want 3 and 1", copies, runs)
	}

	// Removing every copy rule is persisted too, without the defaults coming
	// back through the nil-slice fallback.
	request = httptest.NewRequest(http.MethodPut, "/api/v1/lifecycle", strings.NewReader(`{"copy":[]}`))
	recorder = httptest.NewRecorder()
	app.handleWorktreeSetup(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("clear status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	loaded, err = LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after clear: %v", err)
	}
	for _, action := range loaded.Worktrees.OnCreate {
		if action.Copy != nil {
			t.Errorf("copy rule came back after clearing: %+v", action.Copy)
		}
	}
}

func TestCleanCopyPatterns(t *testing.T) {
	patterns, err := cleanCopyPatterns([]string{" .env ", ".env*", ".env", "", "config/*.local"})
	if err != nil {
		t.Fatalf("cleanCopyPatterns: %v", err)
	}
	if strings.Join(patterns, ",") != ".env,.env*,config/*.local" {
		t.Errorf("patterns = %v, want trimmed and deduplicated", patterns)
	}

	for _, value := range []string{"../secret", "/etc/passwd", ".git/config", "config/[bad"} {
		if _, err := cleanCopyPatterns([]string{value}); err == nil {
			t.Errorf("cleanCopyPatterns(%q) accepted an invalid pattern", value)
		}
	}
}

func TestApplyWorktreeSetupUsesProjectConfig(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	worktree := filepath.Join(base, "worktree")

	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	testGit(t, project, "init", "-b", "main")
	testGit(t, project, "config", "user.name", "Test")
	testGit(t, project, "config", "user.email", "test@example.com")
	testGit(t, project, "config", "commit.gpgsign", "false")
	testWrite(t, filepath.Join(project, "a.txt"), "base\n")
	testGit(t, project, "add", "-A")
	testGit(t, project, "commit", "-m", "base")
	testGit(t, project, "worktree", "add", "-b", "feature", worktree)

	// The global rule and the repository-local rule both copy untracked files.
	testWrite(t, filepath.Join(project, ".env"), "GLOBAL=1\n")
	testWrite(t, filepath.Join(project, "main-only.txt"), "project\n")
	testWrite(t, filepath.Join(project, ProjectConfigFile), `copy:
  - main-only.txt
`)

	store := history.New(filepath.Join(base, "history"))
	if _, err := store.Ensure(ctx, worktree); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	commonDir := strings.TrimSpace(testGit(t, project, "rev-parse", "--path-format=absolute", "--git-common-dir"))

	cfg := DefaultConfig()
	cfg.Worktrees.OnCreate = []LifecycleAction{{Copy: &CopyAction{Path: ".env", Optional: true}}}
	app := &App{
		cfg:          cfg,
		cfgPath:      filepath.Join(base, "config.yaml"),
		historyStore: store,
		log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		projects: []ProjectStatus{{
			Name:         "project",
			CommonDir:    commonDir,
			MainWorktree: project,
			Worktrees: []WorktreeStatus{
				{Path: project},
				{Path: worktree},
			},
		}},
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/lifecycle/apply", nil)
	recorder := httptest.NewRecorder()
	app.handleApplyWorktreeSetup(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Worktrees int      `json:"worktrees"`
		Copied    int      `json:"copied"`
		Errors    []string `json:"errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Errors) != 0 {
		t.Fatalf("errors = %v", response.Errors)
	}
	if response.Worktrees != 1 || response.Copied != 2 {
		t.Errorf("response = %+v, want 1 worktree and 2 copied files", response)
	}
	for _, name := range []string{".env", "main-only.txt"} {
		if _, err := os.Stat(filepath.Join(worktree, name)); err != nil {
			t.Errorf("%s was not copied: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(worktree, ProjectConfigFile)); err == nil {
		t.Errorf("%s itself must not be copied", ProjectConfigFile)
	}

	// A scoped apply from the linked worktree is rejected with the root path.
	body := strings.NewReader(`{"path":"` + worktree + `"}`)
	request = httptest.NewRequest(http.MethodPost, "/api/v1/lifecycle/apply", body)
	recorder = httptest.NewRecorder()
	app.handleApplyWorktreeSetup(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("linked-worktree status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), project) {
		t.Errorf("linked-worktree error does not name the root: %s", recorder.Body.String())
	}

	// The same apply from the root is scoped to this project's child worktrees.
	body = strings.NewReader(`{"path":"` + project + `"}`)
	request = httptest.NewRequest(http.MethodPost, "/api/v1/lifecycle/apply", body)
	recorder = httptest.NewRecorder()
	app.handleApplyWorktreeSetup(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("root status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode scoped response: %v", err)
	}
	if response.Worktrees != 1 || len(response.Errors) != 0 {
		t.Errorf("scoped response = %+v, want one child worktree", response)
	}
}

func TestRootProjectRejectsLinkedWorktree(t *testing.T) {
	app := &App{projects: []ProjectStatus{{
		Name:         "alpha",
		CommonDir:    "/git/alpha",
		MainWorktree: "/code/alpha",
		Worktrees: []WorktreeStatus{
			{Path: "/code/alpha"},
			{Path: "/code/alpha--feature"},
		},
	}}}

	project, err := app.rootProject("/code/alpha/src")
	if err != nil {
		t.Fatalf("rootProject(root): %v", err)
	}
	if project.CommonDir != "/git/alpha" {
		t.Errorf("rootProject(root) = %+v", project)
	}

	_, err = app.rootProject("/code/alpha--feature/src")
	if err == nil {
		t.Fatal("rootProject(linked) accepted a linked worktree")
	}
	if !strings.Contains(err.Error(), "/code/alpha") {
		t.Errorf("rootProject(linked) error = %v, want the root path", err)
	}

	if _, err := app.rootProject("/code/unmanaged"); err == nil || !strings.Contains(err.Error(), "not inside") {
		t.Errorf("rootProject(unmanaged) error = %v", err)
	}
}

func TestFilterProjectWorktrees(t *testing.T) {
	project := ProjectStatus{
		Name:         "alpha",
		CommonDir:    "/git/alpha",
		MainWorktree: "/code/alpha",
	}
	known := []history.KnownWorktree{
		{Path: "/code/alpha", CommonDir: "/git/alpha"},
		{Path: "/code/alpha--feature", CommonDir: "/git/alpha"},
		{Path: "/code/beta", CommonDir: "/git/beta"},
	}

	filtered := filterProjectWorktrees(known, project)
	if len(filtered) != 1 || filtered[0].Path != "/code/alpha--feature" {
		t.Fatalf("filterProjectWorktrees = %+v, want only the child", filtered)
	}
}

func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func testWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
