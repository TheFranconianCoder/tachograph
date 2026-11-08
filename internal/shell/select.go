package shell

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Candidate is one selectable worktree, flattened from the status response.
type Candidate struct {
	Project      string
	CommonDir    string
	Worktree     string
	Path         string
	Branch       string
	Head         string
	HistoryRepo  string
	ID           string
	State        string
	Watcher      string
	Snapshots    int
	LastSnapshot string
	MainWorktree string
	Main         bool
}

// Candidates flattens the status into the order the app reports: project by
// project, the main worktree first, the linked worktrees by activity.
func Candidates(status Status) []Candidate {
	var candidates []Candidate
	seen := make(map[string]bool)
	for _, project := range status.Projects {
		for _, worktree := range project.Worktrees {
			path := filepath.Clean(worktree.Path)
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			candidates = append(candidates, Candidate{
				Project:      project.Name,
				CommonDir:    project.CommonDir,
				Worktree:     filepath.Base(path),
				Path:         path,
				Branch:       worktree.Branch,
				Head:         worktree.Head,
				HistoryRepo:  worktree.HistoryRepo,
				ID:           worktree.ID,
				State:        worktree.State,
				Watcher:      worktree.Watcher,
				Snapshots:    worktree.Snapshots,
				LastSnapshot: worktree.LastSnapshot,
				MainWorktree: project.MainWorktree,
				Main:         pathEqual(path, project.MainWorktree),
			})
		}
	}
	return candidates
}

// Label is the one-line name used by the picker and error lists: project,
// directory, branch and recorder state.
func (c Candidate) Label() string {
	name := c.Project
	if !c.Main {
		name = c.Project + " / " + c.Worktree
	}
	parts := []string{name}
	if c.Branch != "" && c.Branch != "detached" {
		parts = append(parts, c.Branch)
	}
	if state := c.RecorderState(); state != "" {
		parts = append(parts, state)
	}
	return strings.Join(parts, "  ")
}

// RecorderState returns the watcher state, or the lifecycle state when the app
// has not assigned a watcher yet.
func (c Candidate) RecorderState() string {
	if c.Watcher != "" {
		return c.Watcher
	}
	return c.State
}

// Current returns the managed worktree that contains dir. When worktrees nest,
// the longest path wins. The boolean is false when dir is outside every
// registered worktree.
func Current(candidates []Candidate, dir string) (Candidate, bool) {
	if dir == "" {
		return Candidate{}, false
	}
	target := resolvePath(dir)
	best := -1
	bestLength := 0
	for index, candidate := range candidates {
		path := resolvePath(candidate.Path)
		if !pathWithin(path, target) {
			continue
		}
		if best < 0 || len(path) > bestLength {
			best = index
			bestLength = len(path)
		}
	}
	if best < 0 {
		return Candidate{}, false
	}
	return candidates[best], true
}

// Related returns the worktrees of the same project as current, in app order.
// The common Git directory is the project identity; the display name can
// collide when two projects share a directory name.
func Related(candidates []Candidate, current Candidate) []Candidate {
	related := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if sameProject(candidate, current) {
			related = append(related, candidate)
		}
	}
	return related
}

// Children returns the linked worktrees of current's project. The root
// worktree is the place the shell is started from, not a target.
func Children(candidates []Candidate, current Candidate) []Candidate {
	related := Related(candidates, current)
	children := make([]Candidate, 0, len(related))
	for _, candidate := range related {
		if candidate.Main {
			continue
		}
		children = append(children, candidate)
	}
	return children
}

func sameProject(left, right Candidate) bool {
	if left.CommonDir != "" && right.CommonDir != "" {
		return pathEqual(left.CommonDir, right.CommonDir)
	}
	return left.Project != "" && left.Project == right.Project
}

// resolvePath follows symlinks when the path exists, so a worktree reached
// through a symlinked parent is still recognized as managed.
func resolvePath(path string) string {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved)
	}
	return clean
}

// Match returns the candidates the query identifies, in app order. Exact
// categories win over substring matches: a path, project/worktree, directory
// name, then branch. Every category treats multiple hits as one result set, so
// the caller can still show a picker.
func Match(candidates []Candidate, query string) []Candidate {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	if matches := matchPath(candidates, query); len(matches) > 0 {
		return matches
	}
	if matches := matchProjectWorktree(candidates, query); len(matches) > 0 {
		return matches
	}
	if matches := matchWorktree(candidates, query); len(matches) > 0 {
		return matches
	}
	if matches := matchBranch(candidates, query); len(matches) > 0 {
		return matches
	}
	return matchSubstring(candidates, query)
}

// matchPath accepts absolute paths, ~ paths and relative paths, including a
// subdirectory of a worktree. A relative path only counts when it exists, so a
// branch such as feature/x is not mistaken for a path. An exact worktree path
// wins over a parent.
func matchPath(candidates []Candidate, query string) []Candidate {
	if !looksLikePath(query) {
		return nil
	}
	target := expandHome(query)
	if !filepath.IsAbs(target) {
		absolute, err := filepath.Abs(target)
		if err != nil {
			return nil
		}
		target = absolute
	}
	target = filepath.Clean(target)
	if !pathIsExplicit(query) {
		if _, err := os.Stat(target); err != nil {
			return nil
		}
	}

	for _, candidate := range candidates {
		if pathEqual(candidate.Path, target) {
			return []Candidate{candidate}
		}
	}

	best := -1
	for index, candidate := range candidates {
		if !pathWithin(candidate.Path, target) {
			continue
		}
		if best < 0 || len(filepath.Clean(candidate.Path)) > len(filepath.Clean(candidates[best].Path)) {
			best = index
		}
	}
	if best >= 0 {
		return []Candidate{candidates[best]}
	}
	return nil
}

// pathIsExplicit reports whether the query is a path even when it does not
// exist. Dot-relative and home paths cannot be branch names.
func pathIsExplicit(query string) bool {
	if query == "." || query == ".." || query == "~" {
		return true
	}
	if filepath.IsAbs(query) {
		return true
	}
	return strings.HasPrefix(query, "."+string(filepath.Separator)) ||
		strings.HasPrefix(query, ".."+string(filepath.Separator)) ||
		strings.HasPrefix(query, "~/") ||
		strings.HasPrefix(query, `~\`)
}

func looksLikePath(query string) bool {
	if query == "." || query == ".." || query == "~" {
		return true
	}
	return filepath.IsAbs(query) ||
		strings.HasPrefix(query, "."+string(filepath.Separator)) ||
		strings.HasPrefix(query, ".."+string(filepath.Separator)) ||
		strings.HasPrefix(query, "~/") ||
		strings.HasPrefix(query, `~\`) ||
		strings.ContainsRune(query, '/') ||
		strings.ContainsRune(query, '\\')
}

// matchProjectWorktree accepts "project/worktree" and "project/worktree-dir".
func matchProjectWorktree(candidates []Candidate, query string) []Candidate {
	parts := strings.FieldsFunc(query, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) < 2 {
		return nil
	}
	project := strings.Join(parts[:len(parts)-1], "/")
	worktree := parts[len(parts)-1]

	var matches []Candidate
	for _, candidate := range candidates {
		if strings.EqualFold(candidate.Project, project) && strings.EqualFold(candidate.Worktree, worktree) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

// matchWorktree accepts the directory name, case-insensitively.
func matchWorktree(candidates []Candidate, query string) []Candidate {
	var matches []Candidate
	for _, candidate := range candidates {
		if strings.EqualFold(candidate.Worktree, query) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

// matchBranch accepts the full branch name.
func matchBranch(candidates []Candidate, query string) []Candidate {
	var matches []Candidate
	for _, candidate := range candidates {
		if candidate.Branch == query {
			matches = append(matches, candidate)
		}
	}
	return matches
}

// matchSubstring is the last resort: a case-insensitive substring over the
// display fields. Joining with NUL prevents matches across field boundaries.
func matchSubstring(candidates []Candidate, query string) []Candidate {
	needle := strings.ToLower(query)
	var matches []Candidate
	for _, candidate := range candidates {
		fields := strings.ToLower(strings.Join([]string{
			candidate.Project,
			candidate.Worktree,
			candidate.Path,
			candidate.Branch,
		}, "\x00"))
		if strings.Contains(fields, needle) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

func pathEqual(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func pathWithin(base, target string) bool {
	if base == "" || target == "" {
		return false
	}
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	if pathEqual(base, target) {
		return true
	}
	prefix := base + string(os.PathSeparator)
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(target), strings.ToLower(prefix))
	}
	return strings.HasPrefix(target, prefix)
}
