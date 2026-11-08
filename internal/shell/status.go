// Package shell opens an interactive shell in a registered worktree. The CLI
// resolves the worktree against the running app's status and then replaces
// itself with the user's shell.
package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// statusTimeout bounds the request to the local service.
const statusTimeout = 3 * time.Second

// Status is the part of GET /api/v1/status the shell command needs.
type Status struct {
	Projects []Project `json:"projects"`
}

// Project is one repository and its worktrees.
type Project struct {
	Name         string     `json:"name"`
	CommonDir    string     `json:"commonDir"`
	MainWorktree string     `json:"mainWorktree"`
	Worktrees    []Worktree `json:"worktrees"`
}

// Worktree is one working directory as reported by the app.
type Worktree struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	Branch       string `json:"branch"`
	Head         string `json:"head"`
	HistoryRepo  string `json:"historyRepo"`
	State        string `json:"state"`
	Watcher      string `json:"watcher"`
	Snapshots    int    `json:"snapshots"`
	LastSnapshot string `json:"lastSnapshot"`
}

// Fetch reads the status of the running app from the configured port.
func Fetch(ctx context.Context, port int) (Status, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/v1/status", port)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Status{}, fmt.Errorf("create status request: %w", err)
	}

	client := &http.Client{Timeout: statusTimeout}
	response, err := client.Do(request)
	if err != nil {
		return Status{}, fmt.Errorf("tachograph is not running: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return Status{}, fmt.Errorf("status returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var status Status
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return Status{}, fmt.Errorf("decode status response: %w", err)
	}
	return status, nil
}
