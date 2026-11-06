// Package discovery finds Git repositories and their worktrees below configured
// roots. It never mutates a repository; it only asks git what exists.
package discovery

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/gitcmd"
)

const (
	maxDepth          = 5
	maxPatternMatches = 500
)

// Project is a discovered Git repository and all of its worktrees.
type Project struct {
	Name         string
	CommonDir    string
	MainWorktree string
	Worktrees    []Worktree
}

// Worktree is one working directory of a project.
type Worktree struct {
	Path   string
	Branch string
	Head   string
}

// Result is the outcome of a scan. Errors are collected per root or repository
// so one broken path does not hide the rest.
type Result struct {
	Projects []Project
	Errors   []error
}

// Scan walks roots for Git repositories and additionally expands worktree
// patterns. Both sources are merged into one de-duplicated, deterministic list.
func Scan(ctx context.Context, roots, patterns []string, log *slog.Logger) Result {
	result := Result{}
	if log == nil {
		log = slog.Default()
	}

	repositories := make(map[string]bool)
	seenProjects := make(map[string]bool)

	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("resolve root %q: %w", root, err))
			continue
		}
		info, err := os.Stat(absolute)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("root %q: %w", root, err))
			continue
		}
		if !info.IsDir() {
			result.Errors = append(result.Errors, fmt.Errorf("root %q is not a directory", root))
			continue
		}

		walkErrors := walkRoot(absolute, func(path string) {
			if repositories[path] {
				return
			}
			repositories[path] = true

			project, err := inspect(ctx, path)
			if err != nil {
				result.Errors = append(result.Errors, err)
				return
			}
			if seenProjects[project.CommonDir] {
				return
			}
			seenProjects[project.CommonDir] = true
			result.Projects = append(result.Projects, project)
			log.Debug("discovered project", "name", project.Name, "worktrees", len(project.Worktrees))
		})
		result.Errors = append(result.Errors, walkErrors...)
	}

	// Patterns point at worktree directories (or repositories) outside the
	// roots. A pattern without a match is normal; only malformed patterns and
	// unreadable candidates are reported.
	for _, pattern := range patterns {
		matches, err := expandPattern(pattern)
		if err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}
		if len(matches) > maxPatternMatches {
			matches = matches[:maxPatternMatches]
		}
		for _, path := range matches {
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() || !isGitRepo(path) {
				continue
			}
			if repositories[path] {
				continue
			}
			repositories[path] = true

			project, err := inspect(ctx, path)
			if err != nil {
				result.Errors = append(result.Errors, err)
				continue
			}
			if seenProjects[project.CommonDir] {
				continue
			}
			seenProjects[project.CommonDir] = true
			result.Projects = append(result.Projects, project)
			log.Debug("discovered project via pattern", "name", project.Name, "worktree", path)
		}
	}

	sort.Slice(result.Projects, func(i, j int) bool {
		if result.Projects[i].Name == result.Projects[j].Name {
			return result.Projects[i].CommonDir < result.Projects[j].CommonDir
		}
		return result.Projects[i].Name < result.Projects[j].Name
	})
	for i := range result.Projects {
		sort.Slice(result.Projects[i].Worktrees, func(a, b int) bool {
			return result.Projects[i].Worktrees[a].Path < result.Projects[i].Worktrees[b].Path
		})
	}

	return result
}

// expandPattern expands ~ and glob metacharacters. Relative patterns are
// rejected: they would depend on the process working directory.
func expandPattern(pattern string) ([]string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, nil
	}
	if pattern == "~" || strings.HasPrefix(pattern, "~/") || strings.HasPrefix(pattern, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home for pattern %q: %w", pattern, err)
		}
		pattern = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(pattern, "~"), string(os.PathSeparator)))
	}
	if !filepath.IsAbs(pattern) {
		return nil, fmt.Errorf("worktree pattern %q must be absolute or start with ~", pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("worktree pattern %q: %w", pattern, err)
	}
	return matches, nil
}

func walkRoot(root string, visit func(path string)) []error {
	var errors []error
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			errors = append(errors, fmt.Errorf("walk %q: %w", path, err))
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}
			if depth(root, path) > maxDepth {
				return filepath.SkipDir
			}
		}
		if isGitRepo(path) {
			visit(path)
			return filepath.SkipDir
		}
		return nil
	})
	return errors
}

func depth(root, path string) int {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." {
		return 0
	}
	return len(strings.Split(relative, string(os.PathSeparator)))
}

func skipDir(name string) bool {
	switch name {
	case ".git", ".jj", "node_modules", "dist", "build", ".cache", ".venv", "venv",
		"target", ".next", ".astro", "vendor", "tmp", "logs", "coverage":
		return true
	default:
		return false
	}
}

func isGitRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}

// existingWorktrees drops entries whose directory is gone. Git keeps them in
// `git worktree list` until `git worktree prune`, but they are not worktrees.
func existingWorktrees(worktrees []Worktree) []Worktree {
	existing := worktrees[:0]
	for _, worktree := range worktrees {
		info, err := os.Stat(worktree.Path)
		if err != nil || !info.IsDir() {
			continue
		}
		existing = append(existing, worktree)
	}
	return existing
}

func inspect(ctx context.Context, path string) (Project, error) {
	commonDir, err := git(ctx, path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Project{}, fmt.Errorf("inspect %q: %w", path, err)
	}
	commonDir = filepath.Clean(commonDir)

	output, err := git(ctx, path, "worktree", "list", "--porcelain")
	if err != nil {
		return Project{}, fmt.Errorf("list worktrees for %q: %w", path, err)
	}

	var worktrees []Worktree
	var current *Worktree
	appendCurrent := func() {
		if current != nil && current.Path != "" {
			if current.Branch == "" {
				current.Branch = "detached"
			}
			worktrees = append(worktrees, *current)
		}
	}

	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			appendCurrent()
			current = &Worktree{Path: filepath.Clean(strings.TrimPrefix(line, "worktree "))}
		case current == nil:
			continue
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch refs/heads/"):
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(line, "branch ")
		}
	}
	appendCurrent()

	// `git worktree list` keeps deleted worktrees until they are pruned. They
	// must not come back through discovery, so only existing directories count.
	worktrees = existingWorktrees(worktrees)
	if len(worktrees) == 0 {
		return Project{}, fmt.Errorf("repository %q has no worktrees", path)
	}

	mainWorktree := worktrees[0].Path
	name := filepath.Base(mainWorktree)
	if name == "" || name == "." {
		name = filepath.Base(filepath.Dir(commonDir))
	}

	return Project{
		Name:         name,
		CommonDir:    commonDir,
		MainWorktree: mainWorktree,
		Worktrees:    worktrees,
	}, nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	write := gitcmd.ShouldLog(args)
	started := time.Now()

	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
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

	if err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
