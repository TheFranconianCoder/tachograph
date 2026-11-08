package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// KnownWorktree is one worktree discovered from the history store rather than
// from a configured root. Reading the per-worktree config file keeps startup
// fast: no git process is needed for a normal, already-initialized store.
type KnownWorktree struct {
	ID            string
	ProjectDir    string
	WorktreeDir   string
	Path          string
	CommonDir     string
	Branch        string
	HistoryBranch string
	SetupHash     string
	SetupAt       string
	InitAt        string
	InitFirst     string
	InitDone      bool
}

// List enumerates the existing placeholders below the history root.
func (s *Store) List(ctx context.Context) ([]KnownWorktree, error) {
	projects, err := os.ReadDir(s.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history root %s: %w", s.Root, err)
	}

	var known []KnownWorktree
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		projectDir := project.Name()
		// Pre-slug stores used a bare 12-hex project id. They are still on disk
		// after an upgrade, but the current Ensure creates the readable directory;
		// listing the legacy store would only produce a hash-named duplicate.
		if isLegacyProjectDir(projectDir) {
			continue
		}
		worktreesDir := filepath.Join(s.Root, projectDir, "worktrees")
		entries, err := os.ReadDir(worktreesDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read history worktrees %s: %w", worktreesDir, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			placeholder := filepath.Join(worktreesDir, entry.Name())
			configPath := filepath.Join(s.Root, projectDir, "repo", ".git", "worktrees", entry.Name(), "config.worktree")
			config := readWorktreeConfig(configPath)

			path := config["local-history.worktree"]
			if path == "" {
				path = config["core.worktree"]
			}
			if path == "" {
				path, _ = runGit(ctx, placeholder, "rev-parse", "--show-toplevel")
			}
			if path == "" {
				continue
			}
			path = filepath.Clean(path)

			branch := config["local-history.branch"]
			historyBranch := config["local-history.historyBranch"]
			if branch == "" || historyBranch == "" {
				value := configValue(ctx, placeholder, "local-history.branch")
				if branch == "" {
					branch = value
				}
				value = configValue(ctx, placeholder, "local-history.historyBranch")
				if historyBranch == "" {
					historyBranch = value
				}
			}

			known = append(known, KnownWorktree{
				ID:            shortHash(path, 12),
				ProjectDir:    projectDir,
				WorktreeDir:   entry.Name(),
				Path:          path,
				CommonDir:     commonDirForWorktree(path),
				Branch:        branch,
				HistoryBranch: historyBranch,
				SetupHash:     config["local-history.setupHash"],
				SetupAt:       config["local-history.setupAt"],
				InitAt:        config[initConfigAt],
				InitFirst:     config[initConfigFirst],
				InitDone:      strings.EqualFold(config[initConfigDone], "true"),
			})
		}
	}

	// A deleted worktree has no .git file to read, so its CommonDir is empty. It
	// still belongs to the same project store; inherit the value from a sibling
	// so the UI does not split one project into two cards.
	commonByProject := make(map[string]string)
	for _, entry := range known {
		if entry.CommonDir != "" {
			commonByProject[entry.ProjectDir] = entry.CommonDir
		}
	}
	for i := range known {
		if known[i].CommonDir == "" {
			known[i].CommonDir = commonByProject[known[i].ProjectDir]
		}
	}

	sort.Slice(known, func(i, j int) bool { return known[i].Path < known[j].Path })
	return known, nil
}

// readWorktreeConfig reads the small subset of Git config syntax that the
// per-worktree config.worktree file uses. A missing or unreadable file yields an
// empty map; List falls back to git in that case.
func readWorktreeConfig(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	values := make(map[string]string)
	section := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if section != "" {
			key = section + "." + key
		}
		values[key] = unquoteConfigValue(value)
	}
	return values
}

func unquoteConfigValue(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
		return value[1 : len(value)-1]
	}
	return value
}

// gitDirForWorktree reads the .git pointer instead of spawning git. A linked
// worktree's .git file points at <common>/.git/worktrees/<name>; the main
// worktree's .git is the directory itself.
func gitDirForWorktree(worktree string) string {
	gitPath := filepath.Join(worktree, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return filepath.Clean(gitPath)
	}

	data, err := os.ReadFile(gitPath)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	gitDir, ok := strings.CutPrefix(content, "gitdir: ")
	if !ok {
		return ""
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktree, gitDir)
	}
	return filepath.Clean(gitDir)
}

// commonDirForWorktree returns the shared Git directory for a linked worktree.
// The main worktree keeps its own .git directory.
func commonDirForWorktree(worktree string) string {
	gitDir := gitDirForWorktree(worktree)
	if gitDir == "" {
		return ""
	}
	if filepath.Base(gitDir) == ".git" {
		return gitDir
	}
	if parent := filepath.Dir(gitDir); filepath.Base(parent) == "worktrees" {
		return filepath.Dir(parent)
	}
	return ""
}

// RemoveWorktree removes the placeholder and snapshot branch of a worktree that
// no longer exists. When it was the project's last worktree, the whole project
// store (the shared history repository) is removed as well.
func (s *Store) RemoveWorktree(ctx context.Context, known KnownWorktree) error {
	if known.ProjectDir == "" || known.WorktreeDir == "" {
		return fmt.Errorf("worktree identity is incomplete")
	}

	lock := s.repoLock(known.ProjectDir)
	lock.Lock()
	defer lock.Unlock()

	projectDir := filepath.Join(s.Root, known.ProjectDir)
	placeholder := filepath.Join(projectDir, "worktrees", known.WorktreeDir)
	if err := os.RemoveAll(placeholder); err != nil {
		return fmt.Errorf("remove placeholder %s: %w", placeholder, err)
	}

	repo := filepath.Join(projectDir, "repo")
	if _, err := os.Stat(repo); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_ = os.RemoveAll(projectDir)
			return nil
		}
		return fmt.Errorf("inspect history repo %s: %w", repo, err)
	}
	if _, stderr, err := runGitRaw(ctx, repo, "worktree", "prune"); err != nil {
		return fmt.Errorf("prune worktrees in %s: %w (%s)", repo, err, strings.TrimSpace(stderr))
	}

	branch := known.HistoryBranch
	if branch == "" {
		branch = "wt/" + known.WorktreeDir
	}
	if _, stderr, err := runGitRaw(ctx, repo, "update-ref", "-d", "refs/heads/"+branch); err != nil {
		return fmt.Errorf("delete history branch %s: %w (%s)", branch, err, strings.TrimSpace(stderr))
	}
	// The initialization baseline belongs to the deleted branch. It must go
	// before the GC, otherwise it would pin the branch's objects forever.
	if _, _, err := runGitRaw(ctx, repo, "update-ref", "-d", initRefName(known.WorktreeDir)); err != nil {
		return fmt.Errorf("delete initialization baseline of %s: %w", known.WorktreeDir, err)
	}

	remaining, err := runGit(ctx, repo, "for-each-ref", "--format=%(refname)", "refs/heads/wt/")
	if err != nil {
		return fmt.Errorf("list remaining history branches: %w", err)
	}
	if strings.TrimSpace(remaining) == "" {
		// No worktree of this project is left; the store has no purpose.
		if err := os.RemoveAll(projectDir); err != nil {
			return fmt.Errorf("remove project store %s: %w", projectDir, err)
		}
		return nil
	}

	// Reclaim the deleted branch's objects immediately.
	if _, stderr, err := runGitRaw(ctx, repo, "reflog", "expire", "--expire=now", "--all"); err != nil {
		return fmt.Errorf("expire reflog in %s: %w (%s)", repo, err, strings.TrimSpace(stderr))
	}
	if _, stderr, err := runGitRaw(ctx, repo, "gc", "--prune=now", "--quiet"); err != nil {
		return fmt.Errorf("gc %s: %w (%s)", repo, err, strings.TrimSpace(stderr))
	}
	return nil
}

// RecorderFromKnown builds the lightweight recorder for an already-initialized
// worktree. Snapshot only needs the history paths and the real worktree path;
// no Ensure and no git process are required to create it.
func (s *Store) RecorderFromKnown(known KnownWorktree) *Recorder {
	id := known.ID
	if id == "" {
		id = shortHash(known.Path, 12)
	}
	recorder := &Recorder{
		Project: Project{
			Dir:          known.ProjectDir,
			CommonDir:    known.CommonDir,
			MainWorktree: mainWorktreeFromCommonDir(known.CommonDir),
		},
		Worktree: Worktree{
			ID:            id,
			Path:          known.Path,
			Branch:        known.Branch,
			HistoryBranch: known.HistoryBranch,
			Dir:           known.WorktreeDir,
		},
		HistoryRepo:    filepath.Join(s.Root, known.ProjectDir, "repo"),
		Placeholder:    filepath.Join(s.Root, known.ProjectDir, "worktrees", known.WorktreeDir),
		Diff:           s.Diff,
		lock:           s.repoLock(known.ProjectDir),
		worktreeGitDir: gitDirForWorktree(known.Path),
	}
	recorder.initManaged = known.InitAt != ""
	if recorder.initManaged {
		recorder.initAt, _ = time.Parse(time.RFC3339, known.InitAt)
		recorder.initFirst, _ = time.Parse(time.RFC3339, known.InitFirst)
		recorder.initPending.Store(!known.InitDone)
	}
	return recorder
}

// mainWorktreeFromCommonDir derives the main worktree from a normal
// non-bare repository's common Git directory. Bare repositories have none.
func mainWorktreeFromCommonDir(commonDir string) string {
	if filepath.Base(commonDir) != ".git" {
		return ""
	}
	parent := filepath.Dir(commonDir)
	if info, err := os.Stat(parent); err == nil && info.IsDir() {
		return parent
	}
	return ""
}

func isLegacyProjectDir(value string) bool {
	if len(value) != 12 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func configValue(ctx context.Context, dir, key string) string {
	value, code, err := runGitStatus(ctx, dir, "config", "--worktree", "--get", key)
	if err != nil || code != 0 {
		return ""
	}
	return value
}
