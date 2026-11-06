// Package lifecycle runs local setup actions when a worktree appears: copy
// untracked files such as .env from the main worktree and optionally run a setup
// command.
package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/history"
)

// Action is one lifecycle step. Exactly one of Copy or Run is set.
type Action struct {
	Copy *CopyAction
	Run  *RunAction
}

type CopyAction struct {
	Path      string
	Optional  bool
	Overwrite bool
}

type RunAction struct {
	Command string
	OnlyIf  string
	Timeout string
}

type Options struct {
	Actions []Action
	Hash    string
	Force   bool
	Log     *slog.Logger
}

type Result struct {
	Skipped bool
	Copied  []string
	Ran     []string
}

// Apply runs the actions for one worktree unless a setup marker already exists.
// The marker is written only after every action succeeded.
func Apply(ctx context.Context, recorder *history.Recorder, opts Options) (Result, error) {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}

	marker, err := recorder.Config(ctx, "local-history.setupHash")
	if err != nil {
		return Result{}, err
	}
	if marker != "" && !opts.Force {
		return Result{Skipped: true}, nil
	}

	result := Result{}
	for _, action := range opts.Actions {
		switch {
		case action.Copy != nil:
			copied, err := applyCopy(recorder, *action.Copy, log)
			if err != nil {
				return result, err
			}
			result.Copied = append(result.Copied, copied...)
		case action.Run != nil:
			ran, err := applyRun(ctx, recorder, *action.Run, log)
			if err != nil {
				return result, err
			}
			if ran {
				result.Ran = append(result.Ran, action.Run.Command)
			}
		}
	}

	if opts.Hash != "" {
		if err := recorder.SetConfig(ctx, "local-history.setupHash", opts.Hash); err != nil {
			return result, err
		}
		if err := recorder.SetConfig(ctx, "local-history.setupAt", time.Now().Format(time.RFC3339)); err != nil {
			return result, err
		}
	}
	return result, nil
}

// applyCopy expands the configured pattern against the main worktree and copies
// every matching file to the same relative path in the new worktree. Globs are
// optional by nature: no match is only an error when the action is required.
func applyCopy(recorder *history.Recorder, action CopyAction, log *slog.Logger) ([]string, error) {
	pattern, err := cleanRelativePattern(action.Path)
	if err != nil {
		return nil, err
	}

	sourceRoot := recorder.Project.MainWorktree
	if sourceRoot == "" {
		sourceRoot = recorder.Worktree.Path
	}
	matches, err := filepath.Glob(filepath.Join(sourceRoot, filepath.FromSlash(pattern)))
	if err != nil {
		return nil, fmt.Errorf("lifecycle copy %q: invalid pattern: %w", pattern, err)
	}
	if len(matches) == 0 {
		if action.Optional {
			log.Debug("optional lifecycle copy skipped", "path", pattern)
			return nil, nil
		}
		return nil, fmt.Errorf("lifecycle copy %q: no source matched", pattern)
	}

	copied := make([]string, 0, len(matches))
	for _, source := range matches {
		info, err := os.Stat(source)
		if err != nil {
			return copied, fmt.Errorf("lifecycle copy %q: source %s: %w", pattern, source, err)
		}
		if info.IsDir() {
			log.Debug("lifecycle copy skipped directory", "path", source)
			continue
		}

		relative, err := filepath.Rel(sourceRoot, source)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return copied, fmt.Errorf("lifecycle copy %q: source escapes the main worktree", pattern)
		}
		relative = filepath.ToSlash(relative)
		target := filepath.Join(recorder.Worktree.Path, filepath.FromSlash(relative))

		if _, err := os.Stat(target); err == nil && !action.Overwrite {
			log.Debug("lifecycle copy skipped, target exists", "path", relative)
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return copied, fmt.Errorf("lifecycle copy %q: target %s: %w", relative, target, err)
		}

		data, err := os.ReadFile(source)
		if err != nil {
			return copied, fmt.Errorf("lifecycle copy %q: read source: %w", relative, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return copied, fmt.Errorf("lifecycle copy %q: create target directory: %w", relative, err)
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return copied, fmt.Errorf("lifecycle copy %q: write target: %w", relative, err)
		}

		// A copied secret must never become part of a snapshot.
		if err := recorder.Exclude(relative); err != nil {
			return copied, err
		}
		log.Info("lifecycle copied file", "path", relative, "from", source)
		copied = append(copied, relative)
	}
	return copied, nil
}

// CopyOnly applies the copy actions without touching run actions or the setup
// marker. It is used to fill missing local files in worktrees that already
// exist.
func CopyOnly(recorder *history.Recorder, actions []Action, log *slog.Logger) (Result, error) {
	if log == nil {
		log = slog.Default()
	}
	result := Result{Copied: []string{}}
	err := recorder.Exclusive(func() error {
		for _, action := range actions {
			if action.Copy == nil {
				continue
			}
			copied, err := applyCopy(recorder, *action.Copy, log)
			if err != nil {
				return err
			}
			result.Copied = append(result.Copied, copied...)
		}
		return nil
	})
	return result, err
}

func applyRun(ctx context.Context, recorder *history.Recorder, action RunAction, log *slog.Logger) (bool, error) {
	if strings.TrimSpace(action.Command) == "" {
		return false, fmt.Errorf("lifecycle run action has no command")
	}
	if action.OnlyIf != "" {
		if _, err := os.Stat(filepath.Join(recorder.Worktree.Path, action.OnlyIf)); err != nil {
			if os.IsNotExist(err) {
				log.Debug("lifecycle run skipped, condition missing", "only_if", action.OnlyIf)
				return false, nil
			}
			return false, fmt.Errorf("lifecycle run: check %s: %w", action.OnlyIf, err)
		}
	}

	timeout := 5 * time.Minute
	if action.Timeout != "" {
		parsed, err := time.ParseDuration(action.Timeout)
		if err != nil {
			return false, fmt.Errorf("lifecycle run: invalid timeout %q: %w", action.Timeout, err)
		}
		timeout = parsed
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(runCtx, "cmd", "/C", action.Command)
	} else {
		command = exec.CommandContext(runCtx, "sh", "-c", action.Command)
	}
	command.Dir = recorder.Worktree.Path
	output, err := command.CombinedOutput()
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return false, fmt.Errorf("lifecycle run %q timed out after %s", action.Command, timeout)
		}
		return false, fmt.Errorf("lifecycle run %q: %w: %s", action.Command, err, strings.TrimSpace(string(output)))
	}
	log.Info("lifecycle ran command", "command", action.Command, "output_bytes", len(output))
	return true, nil
}

func cleanRelative(path string) (string, error) {
	return cleanRelativePattern(path)
}

// cleanRelativePattern validates a relative copy pattern. Glob metacharacters
// are allowed; escapes and the Git directory are not.
func cleanRelativePattern(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("lifecycle copy action has no path")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("lifecycle copy path %q must be relative", path)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("lifecycle copy path %q escapes the worktree", path)
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git"+string(os.PathSeparator)) {
		return "", fmt.Errorf("lifecycle copy path %q is inside the Git directory", path)
	}
	if _, err := filepath.Match(clean, "x"); err != nil {
		return "", fmt.Errorf("lifecycle copy path %q is not a valid pattern: %w", path, err)
	}
	return clean, nil
}
