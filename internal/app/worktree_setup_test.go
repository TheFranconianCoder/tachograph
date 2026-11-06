package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
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
