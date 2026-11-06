package history

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/gitcmd"
)

// runGit runs git in dir and returns trimmed stdout. A non-zero exit is an
// error carrying git's stderr, so callers do not have to decode it.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	stdout, stderr, err := runGitRaw(ctx, dir, args...)
	if err != nil {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = strings.TrimSpace(stdout)
		}
		return strings.TrimSpace(stdout), fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, message)
	}
	return strings.TrimSpace(stdout), nil
}

// runGitStatus is runGit for commands where exit code 1 is a normal answer, such
// as `git diff --cached --quiet`.
func runGitStatus(ctx context.Context, dir string, args ...string) (string, int, error) {
	stdout, stderr, err := runGitRaw(ctx, dir, args...)
	if err == nil {
		return strings.TrimSpace(stdout), 0, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return strings.TrimSpace(stdout), exitErr.ExitCode(), nil
	}
	return strings.TrimSpace(stdout), -1, fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr))
}

func runGitRaw(ctx context.Context, dir string, args ...string) (string, string, error) {
	return runGitRawWithEnv(ctx, dir, nil, args...)
}

// runGitEnv is runGit with extra environment variables, used to preserve commit
// dates when the history is rewritten.
func runGitEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	stdout, stderr, err := runGitRawWithEnv(ctx, dir, env, args...)
	if err != nil {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = strings.TrimSpace(stdout)
		}
		return strings.TrimSpace(stdout), fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, message)
	}
	return strings.TrimSpace(stdout), nil
}

func runGitRawWithEnv(ctx context.Context, dir string, env []string, args ...string) (string, string, error) {
	write := gitcmd.ShouldLog(args)
	started := time.Now()

	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	if len(env) > 0 {
		command.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()

	if write {
		attributes := []any{
			"dir", dir,
			"args", strings.Join(args, " "),
			"duration", time.Since(started),
		}
		if err != nil {
			attributes = append(attributes, "err", err, "stderr", strings.TrimSpace(stderr.String()))
			slog.Debug("git failed", attributes...)
		} else {
			slog.Debug("git", attributes...)
		}
	}

	return stdout.String(), stderr.String(), err
}
