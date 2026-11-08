package shell

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchReadsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status" {
			t.Errorf("request path = %q, want /api/v1/status", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"projects":[{"name":"alpha","commonDir":"/git/alpha","mainWorktree":"/code/alpha",
			"worktrees":[{"path":"/code/alpha","branch":"main","watcher":"watching"}]}]}`)
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	status, err := Fetch(context.Background(), port)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(status.Projects) != 1 {
		t.Fatalf("Fetch() returned %d projects, want 1", len(status.Projects))
	}
	if status.Projects[0].Name != "alpha" {
		t.Errorf("project name = %q, want alpha", status.Projects[0].Name)
	}
	if got := status.Projects[0].Worktrees[0].Watcher; got != "watching" {
		t.Errorf("watcher = %q, want watching", got)
	}
}

func TestFetchNotRunning(t *testing.T) {
	_, err := Fetch(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "tachograph is not running") {
		t.Fatalf("Fetch() error = %v, want not-running error", err)
	}
}

func TestFetchErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	_, err := Fetch(context.Background(), port)
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Fetch() error = %v, want 500 with body", err)
	}
}
