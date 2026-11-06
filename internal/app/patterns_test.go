package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreePatternHandlers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	app := &App{cfg: DefaultConfig(), cfgPath: path}
	pattern := filepath.Join(t.TempDir(), "trees", "*")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/patterns",
		strings.NewReader(`{"pattern":"`+pattern+`"}`))
	recorder := httptest.NewRecorder()
	app.handlePatternAdd(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("add status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := readPatterns(t, recorder.Body.Bytes()); len(got) != 1 || got[0] != pattern {
		t.Errorf("patterns = %v, want [%s]", got, pattern)
	}

	// Adding the same pattern again is idempotent.
	request = httptest.NewRequest(http.MethodPost, "/api/v1/patterns",
		strings.NewReader(`{"pattern":"`+pattern+`"}`))
	recorder = httptest.NewRecorder()
	app.handlePatternAdd(recorder, request)
	if got := readPatterns(t, recorder.Body.Bytes()); len(got) != 1 {
		t.Errorf("patterns after duplicate = %v, want one", got)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(loaded.Worktrees.Patterns) != 1 || loaded.Worktrees.Patterns[0] != pattern {
		t.Errorf("persisted patterns = %v, want [%s]", loaded.Worktrees.Patterns, pattern)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/patterns",
		strings.NewReader(`{"pattern":"../relative/*"}`))
	recorder = httptest.NewRecorder()
	app.handlePatternAdd(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("relative pattern status = %d, want 400", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/v1/patterns",
		strings.NewReader(`{"pattern":"`+pattern+`"}`))
	recorder = httptest.NewRecorder()
	app.handlePatternRemove(recorder, request)
	if got := readPatterns(t, recorder.Body.Bytes()); len(got) != 0 {
		t.Errorf("patterns after remove = %v, want empty", got)
	}
	loaded, err = LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after remove: %v", err)
	}
	if len(loaded.Worktrees.Patterns) != 0 {
		t.Errorf("persisted patterns after remove = %v, want empty", loaded.Worktrees.Patterns)
	}
}

func readPatterns(t *testing.T, body []byte) []string {
	t.Helper()
	var response struct {
		Patterns []string `json:"patterns"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode patterns: %v", err)
	}
	return response.Patterns
}
