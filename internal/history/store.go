// Package history keeps a Git-backed local history for Git worktrees.
//
// One history repository is created per project. Every project worktree gets a
// linked-worktree placeholder inside that history repository; the placeholder
// has no files of its own and points its per-worktree core.worktree at the real
// worktree. Each placeholder has its own index, HEAD and branch, while all of
// them share the history repository's object store.
package history

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/difft"
)

// ErrNoChanges means the worktree is identical to its last snapshot.
var ErrNoChanges = errors.New("no changes to snapshot")

// Store owns the history repositories below Root.
type Store struct {
	Root string
	// Diff selects the renderer for snapshot patches.
	Diff difft.Tool

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New creates a store rooted at dir.
func New(root string) *Store {
	return &Store{Root: root, locks: make(map[string]*sync.Mutex)}
}

// repoLock returns the lock that serializes snapshots, worktree changes and
// compaction for one project repository.
func (s *Store) repoLock(projectDir string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks == nil {
		s.locks = make(map[string]*sync.Mutex)
	}
	lock, ok := s.locks[projectDir]
	if !ok {
		lock = &sync.Mutex{}
		s.locks[projectDir] = lock
	}
	return lock
}

// DefaultRoot returns the per-user history root.
func DefaultRoot() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "tachograph", "history"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "tachograph", "history"), nil
}

// Project identifies a Git repository by its common Git directory.
type Project struct {
	ID           string
	Name         string
	Slug         string
	Dir          string
	CommonDir    string
	MainWorktree string
}

// Worktree identifies one working directory of a project.
type Worktree struct {
	ID            string
	Path          string
	Branch        string
	Head          string
	Slug          string
	Dir           string
	HistoryBranch string
}

// Recorder is the bound recorder for one worktree.
type Recorder struct {
	Project     Project
	Worktree    Worktree
	HistoryRepo string
	Placeholder string
	Diff        difft.Tool

	lock *sync.Mutex
}

// ChangeStats describes the size of one snapshot.
type ChangeStats struct {
	Files   int `json:"files"`
	Added   int `json:"added"`
	Deleted int `json:"deleted"`
}

// Snapshot is one recorded state.
type Snapshot struct {
	Hash      string
	Message   string
	CreatedAt time.Time
	Stats     *ChangeStats
}

// HistorySummary describes what already exists on the history branch.
type HistorySummary struct {
	Snapshots    int
	LastHash     string
	LastSnapshot time.Time
}

// Summary reads the snapshot count and newest snapshot from the history branch.
func (r *Recorder) Summary(ctx context.Context) (HistorySummary, error) {
	if r.lock != nil {
		r.lock.Lock()
		defer r.lock.Unlock()
	}
	countOutput, err := runGit(ctx, r.Placeholder, "rev-list", "--count", "--grep=^snap:", "HEAD")
	if err != nil {
		return HistorySummary{}, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(countOutput))
	if err != nil {
		return HistorySummary{}, fmt.Errorf("parse history count: %w", err)
	}
	summary := HistorySummary{}
	if count == 0 {
		return summary, nil
	}
	summary.Snapshots = count

	output, err := runGit(ctx, r.Placeholder, "log", "-1", "--grep=^snap:", "--format=%H%x00%aI", "HEAD")
	if err != nil {
		return summary, err
	}
	parts := strings.SplitN(output, "\x00", 2)
	if len(parts) != 2 {
		return summary, nil
	}
	summary.LastHash = strings.TrimSpace(parts[0])
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[1])); err == nil {
		summary.LastSnapshot = parsed
	}
	return summary, nil
}

// Ensure resolves the worktree and creates or reuses its history store and
// placeholder. It is idempotent.
func (s *Store) Ensure(ctx context.Context, worktreePath string) (*Recorder, error) {
	absolute, err := filepath.Abs(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("resolve worktree path: %w", err)
	}

	worktreeRoot, err := runGit(ctx, absolute, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a Git worktree: %w", err)
	}
	worktreeRoot = filepath.Clean(worktreeRoot)

	commonDir, err := runGit(ctx, worktreeRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("resolve common Git directory: %w", err)
	}
	commonDir = filepath.Clean(commonDir)

	branch, err := runGit(ctx, worktreeRoot, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil || branch == "" {
		branch = "detached"
	}
	head, err := runGit(ctx, worktreeRoot, "rev-parse", "--short", "HEAD")
	if err != nil || head == "" {
		head = "none"
	}

	name, mainWorktree := projectInfo(ctx, worktreeRoot, commonDir)
	projectSlug := slugOr(name, "project")
	project := Project{
		ID:           shortHash(commonDir, 12),
		Name:         name,
		Slug:         projectSlug,
		Dir:          projectSlug + "-" + shortHash(commonDir, 8),
		CommonDir:    commonDir,
		MainWorktree: mainWorktree,
	}

	worktreeName := filepath.Base(worktreeRoot)
	worktreeSlug := slugOr(worktreeName, "worktree")
	worktree := Worktree{
		ID:            shortHash(worktreeRoot, 12),
		Path:          worktreeRoot,
		Branch:        branch,
		Head:          head,
		Slug:          worktreeSlug,
		Dir:           worktreeSlug + "-" + shortHash(worktreeRoot, 8),
		HistoryBranch: "wt/" + worktreeSlug + "-" + shortHash(worktreeRoot, 8),
	}

	historyRepo := filepath.Join(s.Root, project.Dir, "repo")
	placeholder := filepath.Join(s.Root, project.Dir, "worktrees", worktree.Dir)

	lock := s.repoLock(project.Dir)
	lock.Lock()
	defer lock.Unlock()

	if err := ensureHistoryRepo(ctx, historyRepo); err != nil {
		return nil, err
	}
	if err := ensurePlaceholder(ctx, historyRepo, placeholder, worktree, project); err != nil {
		return nil, err
	}

	return &Recorder{
		Project:     project,
		Worktree:    worktree,
		HistoryRepo: historyRepo,
		Placeholder: placeholder,
		Diff:        s.Diff,
		lock:        lock,
	}, nil
}

// Exclusive runs fn while holding the project repository lock. Lifecycle
// copies use it so a secret file cannot be staged between write and exclude.
func (r *Recorder) Exclusive(fn func() error) error {
	if r.lock != nil {
		r.lock.Lock()
		defer r.lock.Unlock()
	}
	return fn()
}

// Snapshot stages the worktree and commits it in the history store. It returns
// ErrNoChanges when the tree equals the previous snapshot.
func (r *Recorder) Snapshot(ctx context.Context) (Snapshot, error) {
	if r.lock != nil {
		r.lock.Lock()
		defer r.lock.Unlock()
	}
	if err := r.stageWorktree(ctx); err != nil {
		return Snapshot{}, err
	}

	numstat, err := runGit(ctx, r.Placeholder, "diff", "--cached", "--numstat")
	if err != nil {
		return Snapshot{}, fmt.Errorf("compare worktree: %w", err)
	}
	if strings.TrimSpace(numstat) == "" {
		return Snapshot{}, ErrNoChanges
	}
	stats := parseNumstat(numstat)
	// The first snapshot copies the whole source tree. Its diffstats describe
	// the initialization, not a change, so leave them empty. An empty archive
	// root is the same state after compaction.
	if subject, err := runGit(ctx, r.Placeholder, "show", "-s", "--format=%s", "HEAD"); err == nil &&
		(strings.HasPrefix(subject, "base:") ||
			(strings.HasPrefix(subject, "archive:") && r.treeEmpty(ctx, "HEAD"))) {
		stats = nil
	}

	// The project branch or HEAD may have moved since Ensure; the snapshot
	// message should describe the state that was actually recorded.
	branch := r.Worktree.Branch
	if current, err := runGit(ctx, r.Worktree.Path, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && current != "" {
		branch = current
	}
	head := r.Worktree.Head
	if current, err := runGit(ctx, r.Worktree.Path, "rev-parse", "--short", "HEAD"); err == nil && current != "" {
		head = current
	}

	createdAt := time.Now()
	message := fmt.Sprintf("snap: %s [%s@%s]", createdAt.Format(time.RFC3339), branch, head)
	if _, err := runGit(ctx, r.Placeholder, "commit", "-m", message); err != nil {
		return Snapshot{}, fmt.Errorf("commit snapshot: %w", err)
	}
	hash, err := runGit(ctx, r.Placeholder, "rev-parse", "HEAD")
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve snapshot hash: %w", err)
	}

	return Snapshot{Hash: hash, Message: message, CreatedAt: createdAt, Stats: stats}, nil
}

// parseNumstat sums Git's --numstat output. Binary files report "-" for both
// counts and only contribute to the file count.
func parseNumstat(output string) *ChangeStats {
	stats := &ChangeStats{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		stats.Files++
		if added, err := strconv.Atoi(fields[0]); err == nil {
			stats.Added += added
		}
		if deleted, err := strconv.Atoi(fields[1]); err == nil {
			stats.Deleted += deleted
		}
	}
	return stats
}

// stageWorktree stages the worktree. Git refuses to index a nested repository
// that has no commit checked out; such paths are excluded from the history store
// and the add is retried once, so one broken embedded repo cannot block the whole
// worktree.
func (r *Recorder) stageWorktree(ctx context.Context) error {
	_, stderr, err := runGitRaw(ctx, r.Placeholder, "add", "-A", "--ignore-errors")
	if err == nil {
		return nil
	}

	paths := quotedRepositoryPaths(stderr)
	if len(paths) == 0 {
		return fmt.Errorf("stage worktree: %w (%s)", err, strings.TrimSpace(stderr))
	}
	if err := r.Exclude(paths...); err != nil {
		return fmt.Errorf("exclude nested repository: %w", err)
	}

	_, stderr, err = runGitRaw(ctx, r.Placeholder, "add", "-A", "--ignore-errors")
	if err != nil {
		return fmt.Errorf("stage worktree: %w (%s)", err, strings.TrimSpace(stderr))
	}
	return nil
}

// quotedRepositoryPaths extracts the paths Git quotes in errors such as
// "error: 'nested/' does not have a commit checked out".
func quotedRepositoryPaths(message string) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, line := range strings.Split(message, "\n") {
		for {
			start := strings.Index(line, "'")
			if start < 0 {
				break
			}
			end := strings.Index(line[start+1:], "'")
			if end < 0 {
				break
			}
			value := strings.TrimSpace(line[start+1 : start+1+end])
			line = line[start+1+end+1:]
			if value == "" || !strings.Contains(value, "/") || seen[value] {
				continue
			}
			seen[value] = true
			paths = append(paths, value)
		}
	}
	return paths
}

func ensureHistoryRepo(ctx context.Context, repo string) error {
	if _, err := os.Stat(filepath.Join(repo, ".git")); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(repo, 0o755); err != nil {
			return fmt.Errorf("create history directory: %w", err)
		}
		if _, err := runGit(ctx, repo, "init", "-b", "main"); err != nil {
			return fmt.Errorf("initialize history repository: %w", err)
		}
	}

	settings := [][]string{
		{"config", "extensions.worktreeConfig", "true"},
		{"config", "user.name", "Tachograph"},
		{"config", "user.email", "tachograph@localhost"},
		{"config", "commit.gpgsign", "false"},
	}
	for _, args := range settings {
		if _, err := runGit(ctx, repo, args...); err != nil {
			return fmt.Errorf("configure history repository: %w", err)
		}
	}

	if err := ensureExclude(repo); err != nil {
		return err
	}

	if _, err := runGit(ctx, repo, "rev-parse", "--verify", "HEAD"); err != nil {
		if _, err := runGit(ctx, repo, "commit", "--allow-empty", "-m", "base: history initialized"); err != nil {
			return fmt.Errorf("create history base commit: %w", err)
		}
	}
	return nil
}

var defaultExcludes = []string{
	"/.git",
	"/.jj",
	".env",
	".env.local",
	".env.*.local",
	"*.pem",
	"*.key",
}

func ensureExclude(repo string) error {
	return ensureExcludePaths(repo, defaultExcludes...)
}

func ensureExcludePaths(repo string, patterns ...string) error {
	path := filepath.Join(repo, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create history exclude directory: %w", err)
	}

	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read history exclude file: %w", err)
	}

	seen := make(map[string]bool)
	for _, line := range strings.Split(string(existing), "\n") {
		seen[strings.TrimSpace(line)] = true
	}

	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	added := false
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || seen[pattern] {
			continue
		}
		content += pattern + "\n"
		seen[pattern] = true
		added = true
	}
	if !added {
		return nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write history exclude file: %w", err)
	}
	return nil
}

// Exclude adds paths or patterns to the history repository's info/exclude, so
// lifecycle-copied files never become part of a snapshot.
func (r *Recorder) Exclude(paths ...string) error {
	return ensureExcludePaths(r.HistoryRepo, paths...)
}

// Config reads a per-worktree config value. A missing value returns an empty
// string and no error.
func (r *Recorder) Config(ctx context.Context, key string) (string, error) {
	value, code, err := runGitStatus(ctx, r.Placeholder, "config", "--worktree", "--get", key)
	if err != nil {
		return "", err
	}
	if code == 1 {
		return "", nil
	}
	if code != 0 {
		return "", fmt.Errorf("git config --get %s exited with code %d", key, code)
	}
	return value, nil
}

// SetConfig writes a per-worktree config value.
func (r *Recorder) SetConfig(ctx context.Context, key, value string) error {
	if _, err := runGit(ctx, r.Placeholder, "config", "--worktree", key, value); err != nil {
		return fmt.Errorf("set history config %s: %w", key, err)
	}
	return nil
}

func ensurePlaceholder(ctx context.Context, repo, placeholder string, worktree Worktree, project Project) error {
	if _, err := os.Stat(placeholder); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(placeholder), 0o755); err != nil {
			return fmt.Errorf("create placeholder directory: %w", err)
		}

		branch := worktree.HistoryBranch
		args := []string{"worktree", "add", "--no-checkout"}
		if _, err := runGit(ctx, repo, "rev-parse", "--verify", "refs/heads/"+branch); err != nil {
			args = append(args, "-b", branch)
		}
		args = append(args, placeholder)
		if _, err := runGit(ctx, repo, args...); err != nil {
			return fmt.Errorf("create history worktree placeholder: %w", err)
		}
	}

	if _, err := runGit(ctx, placeholder, "rev-parse", "--show-toplevel"); err != nil {
		return fmt.Errorf("placeholder %s is not a valid Git worktree: %w", placeholder, err)
	}

	settings := [][]string{
		{"config", "--worktree", "core.worktree", worktree.Path},
		{"config", "--worktree", "local-history.worktree", worktree.Path},
		{"config", "--worktree", "local-history.project", project.ID},
		{"config", "--worktree", "local-history.branch", worktree.Branch},
		{"config", "--worktree", "local-history.historyBranch", worktree.HistoryBranch},
	}
	for _, args := range settings {
		if _, err := runGit(ctx, placeholder, args...); err != nil {
			return fmt.Errorf("configure history worktree: %w", err)
		}
	}
	return nil
}

func projectInfo(ctx context.Context, worktree, commonDir string) (string, string) {
	if output, err := runGit(ctx, worktree, "worktree", "list", "--porcelain"); err == nil {
		mainWorktree := ""
		for _, line := range strings.Split(output, "\n") {
			path, ok := strings.CutPrefix(line, "worktree ")
			if !ok {
				continue
			}
			path = filepath.Clean(path)
			if mainWorktree == "" {
				mainWorktree = path
			}
			if name := filepath.Base(path); name != "" && name != "." {
				return name, mainWorktree
			}
		}
		if mainWorktree != "" {
			return filepath.Base(mainWorktree), mainWorktree
		}
	}
	return filepath.Base(filepath.Dir(commonDir)), ""
}

func shortHash(value string, length int) string {
	sum := sha1.Sum([]byte(value))
	encoded := hex.EncodeToString(sum[:])
	if length > len(encoded) {
		length = len(encoded)
	}
	return encoded[:length]
}

// slugOr turns a human name into a path- and branch-safe slug. Anything that is
// not a lowercase letter or digit becomes a single dash; the result is trimmed
// and capped so a long name cannot dominate the path. The stable identity is the
// hash suffix, not the slug.
func slugOr(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case builder.Len() > 0 && !lastDash:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		return fallback
	}
	return slug
}
