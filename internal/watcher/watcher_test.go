package watcher

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestArgsIncludeWatchexecTriggerFlags(t *testing.T) {
	args := argsForGroup(
		"/worktree/main",
		[]string{"/worktree/main", "/worktree/feature", "", "  "},
		1500*time.Millisecond,
		[]string{".git", "node_modules", "", "  "},
	)
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"--only-emit-events",
		"--quiet",
		"--watch /worktree/main",
		"--watch /worktree/feature",
		"--project-origin /worktree/main",
		"--debounce 1.5s",
		"--ignore .git",
		"--ignore node_modules",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args = %q, missing %q", joined, want)
		}
	}
	if strings.Contains(joined, `--ignore ""`) || strings.Contains(joined, "--ignore  ") {
		t.Errorf("args contain an empty ignore: %q", joined)
	}
}

func TestMatchWorktreeUsesLongestPath(t *testing.T) {
	cases := []struct {
		candidate string
		paths     []string
		want      string
		ok        bool
	}{
		{"/a/b/file.txt", []string{"/a", "/a/b"}, "/a/b", true},
		{"/a/b", []string{"/a", "/a/b"}, "/a/b", true},
		{"/a/bc/file.txt", []string{"/a/b"}, "", false},
		{"/other/file.txt", []string{"/a", "/b"}, "", false},
	}
	for _, test := range cases {
		got, ok := matchWorktree(test.candidate, test.paths)
		if got != test.want || ok != test.ok {
			t.Errorf("matchWorktree(%q, %v) = %q, %v; want %q, %v",
				test.candidate, test.paths, got, ok, test.want, test.ok)
		}
	}
}

func TestResolveHonoursConfiguredPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "watchexec")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := Resolve(binary); got != binary {
		t.Errorf("Resolve(%q) = %q, want the configured path", binary, got)
	}

	notExecutable := filepath.Join(dir, "plain")
	if err := os.WriteFile(notExecutable, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Resolve(notExecutable); got != "" {
		t.Errorf("Resolve(non-executable) = %q, want empty", got)
	}
}

func TestManagerReportsWorktreeChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	script := writeScript(t, `#!/bin/sh
while true; do
  echo '{"tags":[{"kind":"path","absolute":"/worktree/a.txt"}]}'
  sleep 0.05
done
`)

	changes := make(chan string, 8)
	manager := NewManager(Options{
		Binary:   script,
		Debounce: 50 * time.Millisecond,
		Log:      discardLogger(),
		OnChange: func(path string) {
			select {
			case changes <- path:
			default:
			}
		},
	})
	defer manager.Close()
	manager.Ensure("project", "/worktree", []string{"/worktree"})

	select {
	case path := <-changes:
		if path != "/worktree" {
			t.Fatalf("change path = %q, want /worktree", path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no change reported")
	}

	manager.Remove("project")
	waitFor(t, 2*time.Second, func() bool { return !manager.Running("project") })
}

func TestManagerMapsEventsToTheRightWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	script := writeScript(t, `#!/bin/sh
echo '{"tags":[{"kind":"path","absolute":"/root/feature/file.txt"}]}'
echo '{"tags":[{"kind":"path","absolute":"/root/main/file.txt"}]}'
exec sleep 30
`)

	var mu sync.Mutex
	var seen []string
	manager := NewManager(Options{
		Binary:   script,
		Debounce: 10 * time.Millisecond,
		Log:      discardLogger(),
		OnChange: func(path string) {
			mu.Lock()
			seen = append(seen, path)
			mu.Unlock()
		},
	})
	defer manager.Close()
	manager.Ensure("project", "/root/main", []string{"/root/main", "/root/feature"})

	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) == 2
	})
	mu.Lock()
	defer mu.Unlock()
	if seen[0] != "/root/feature" || seen[1] != "/root/main" {
		t.Fatalf("reported paths = %v, want /root/feature then /root/main", seen)
	}
}

func TestManagerRestartsWhenThePathSetChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	script := writeScript(t, `#!/bin/sh
exec sleep 30
`)

	var starts atomic.Int32
	started := make(chan struct{}, 8)
	manager := NewManager(Options{
		Binary:   script,
		Debounce: 10 * time.Millisecond,
		Log:      discardLogger(),
		OnState: func(key string, running bool) {
			if !running {
				return
			}
			starts.Add(1)
			select {
			case started <- struct{}{}:
			default:
			}
		},
	})
	defer manager.Close()

	manager.Ensure("project", "/root/main", []string{"/root/main"})
	waitFor(t, 2*time.Second, func() bool { return manager.Running("project") })

	manager.Ensure("project", "/root/main", []string{"/root/main", "/root/feature"})
	waitFor(t, 2*time.Second, func() bool { return starts.Load() >= 2 })
}

func TestManagerRestartsAnExitedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	script := writeScript(t, `#!/bin/sh
echo '{"tags":[{"kind":"path","absolute":"/worktree/a.txt"}]}'
`)

	var calls atomic.Int32
	manager := NewManager(Options{
		Binary:   script,
		Debounce: 10 * time.Millisecond,
		Log:      discardLogger(),
		OnChange: func(string) {
			calls.Add(1)
		},
	})
	defer manager.Close()
	manager.Ensure("project", "/worktree", []string{"/worktree"})

	waitFor(t, 4*time.Second, func() bool {
		return calls.Load() >= 2
	})
}

func TestManagerWithRealWatchexec(t *testing.T) {
	binary := Resolve("")
	if binary == "" {
		t.Skip("watchexec is not installed")
	}
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}

	worktree := t.TempDir()
	changes := make(chan string, 8)
	manager := NewManager(Options{
		Binary:   binary,
		Debounce: 100 * time.Millisecond,
		Ignore:   []string{".git"},
		Log:      discardLogger(),
		OnChange: func(path string) {
			select {
			case changes <- path:
			default:
			}
		},
	})
	defer manager.Close()
	manager.Ensure("project", worktree, []string{worktree})

	waitFor(t, 5*time.Second, func() bool { return manager.Running("project") })
	if err := os.WriteFile(filepath.Join(worktree, "change.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case path := <-changes:
		if path != worktree {
			t.Fatalf("change path = %q, want %q", path, worktree)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchexec did not report the change")
	}
}

func writeScript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-watchexec.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
