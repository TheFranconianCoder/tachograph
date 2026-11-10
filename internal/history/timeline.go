package history

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/difft"
)

// TimelineEntry is one commit on a worktree's history branch.
type TimelineEntry struct {
	Hash    string `json:"hash"`
	Time    string `json:"time"`
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Branch  string `json:"branch,omitempty"`
	Head    string `json:"head,omitempty"`
	Label   string `json:"label,omitempty"`
}

// TimelinePage is one page of the timeline.
type TimelinePage struct {
	Entries []TimelineEntry `json:"entries"`
	HasMore bool            `json:"hasMore"`
}

// SnapshotFile is one path changed by a snapshot.
type SnapshotFile struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"`
}

// SnapshotDetail is the metadata, file list and patch of one commit.
type SnapshotDetail struct {
	Hash      string         `json:"hash"`
	Parent    string         `json:"parent,omitempty"`
	From      string         `json:"from,omitempty"`
	Count     int            `json:"count,omitempty"`
	Time      string         `json:"time"`
	Kind      string         `json:"kind"`
	Subject   string         `json:"subject"`
	Label     string         `json:"label,omitempty"`
	Files     []SnapshotFile `json:"files"`
	File      string         `json:"file,omitempty"`
	Diff      string         `json:"diff,omitempty"`
	DiffTool  string         `json:"diffTool,omitempty"`
	Truncated bool           `json:"truncated"`
}

const (
	timelineDefaultLimit = 50
	timelineMaxLimit     = 200
	snapshotMaxFiles     = 500
	snapshotMaxDiff      = 256 * 1024
)

// ErrSnapshotNotFound means the commit is not in the history store, usually
// because a deep link survived a compaction that rewrote the branch.
var ErrSnapshotNotFound = errors.New("snapshot not found")

// ErrFileNotInSnapshot means the requested path is not part of the snapshot.
var ErrFileNotInSnapshot = errors.New("file not in snapshot")

var commitHashPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// Timeline reads the branch of this worktree, newest first. file, when set, is
// a path relative to the worktree root.
func (r *Recorder) Timeline(ctx context.Context, limit, offset int, file string) (TimelinePage, error) {
	if limit <= 0 {
		limit = timelineDefaultLimit
	}
	if limit > timelineMaxLimit {
		limit = timelineMaxLimit
	}
	if offset < 0 {
		offset = 0
	}

	args := []string{
		"log",
		"--format=%H%x00%aI%x00%s",
		"--max-count=" + strconv.Itoa(limit+1),
		"--skip=" + strconv.Itoa(offset),
	}
	if lower := r.timelineLowerBound(ctx); lower != "" {
		args = append(args, lower+"..HEAD")
	}
	if file != "" {
		args = append(args, "--", literalPathspec(file))
	}

	output, err := runGit(ctx, r.Placeholder, args...)
	if err != nil {
		return TimelinePage{}, fmt.Errorf("timeline: %w", err)
	}

	labels := r.labelMap()
	page := TimelinePage{Entries: make([]TimelineEntry, 0, limit)}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		kind, branch, head := classifySubject(parts[2])
		page.Entries = append(page.Entries, TimelineEntry{
			Hash:    parts[0],
			Time:    parts[1],
			Kind:    kind,
			Subject: parts[2],
			Branch:  branch,
			Head:    head,
			Label:   labels[parts[0]],
		})
	}
	if len(page.Entries) > limit {
		page.Entries = page.Entries[:limit]
		page.HasMore = true
	}
	return page, nil
}

// timelineLowerBound returns the newest commit that should stay out of the
// timeline. The root is always a state, not a change: the initialization
// commit, an archive root, or the first source copy. For linked worktrees the
// explicit initialization ref is authoritative; legacy stores fall back to the
// leading run of addition-only snapshots.
func (r *Recorder) timelineLowerBound(ctx context.Context) string {
	// The project's main branch is the source of truth. Show its complete
	// history, including the initialization state and the first copy.
	if r.isMainWorktree() {
		return ""
	}
	if baseline := r.initBaseline(ctx); baseline != "" {
		return baseline
	}

	root, err := runGit(ctx, r.Placeholder, "rev-list", "--max-parents=0", "HEAD")
	if err != nil || root == "" {
		return ""
	}
	subject, err := runGit(ctx, r.Placeholder, "show", "-s", "--format=%s", root)
	if err != nil {
		return ""
	}

	lower := root
	if !strings.HasPrefix(subject, "base:") &&
		!(strings.HasPrefix(subject, "archive:") && r.treeEmpty(ctx, root)) {
		return lower
	}
	first := r.firstCommitAfter(ctx, root)
	if first == "" {
		return lower
	}
	return r.legacyInitializationBound(ctx, root, first)
}

// initBaseline returns the recorded initialization baseline when it is still
// part of this branch.
func (r *Recorder) initBaseline(ctx context.Context) string {
	hash := r.initRefHash(ctx)
	if hash == "" {
		return ""
	}
	if _, code, err := runGitStatus(ctx, r.Placeholder, "merge-base", "--is-ancestor", hash, "HEAD"); err != nil || code != 0 {
		return ""
	}
	return hash
}

// legacyInitializationBound hides a leading run of addition-only snapshots that
// belong to the same initialization burst. It is used for stores created before
// the explicit init ref existed. A real edit or a pause ends the run, so a later
// new file stays visible.
func (r *Recorder) legacyInitializationBound(ctx context.Context, root, first string) string {
	output, err := runGit(ctx, r.Placeholder, "log", "--reverse", "--format=%H%x00%aI", "--ancestry-path", root+"..HEAD")
	if err != nil {
		return first
	}
	type initCommit struct {
		hash string
		at   time.Time
	}
	commits := make([]initCommit, 0, 8)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		at, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			continue
		}
		commits = append(commits, initCommit{hash: parts[0], at: at})
	}
	if len(commits) == 0 {
		return first
	}

	lower := commits[0].hash
	start := commits[0].at
	previous := commits[0]
	for i := 1; i < len(commits) && i < initLegacyMaxPrefix; i++ {
		current := commits[i]
		gap := current.at.Sub(previous.at)
		if gap < 0 {
			gap = -gap
		}
		if gap > initLegacyGap || current.at.Sub(start) > initLegacySpan {
			break
		}
		if !r.onlyAddedBetween(ctx, previous.hash, current.hash) {
			break
		}
		lower = current.hash
		previous = current
	}
	return lower
}

// onlyAddedBetween reports whether the change between two commits contains no
// modification, deletion, rename or type change.
func (r *Recorder) onlyAddedBetween(ctx context.Context, from, to string) bool {
	output, err := runGit(ctx, r.Placeholder, "diff-tree", "--no-commit-id", "--name-only", "-r", "--diff-filter=MDRTU", from, to)
	return err == nil && strings.TrimSpace(output) == ""
}

// BackfillInitialization records the legacy initialization boundary as a ref.
// It makes the first timeline read after an upgrade cheap and keeps the result
// stable. The prefix must be old enough that it cannot still be growing.
func (r *Recorder) BackfillInitialization(ctx context.Context) error {
	if r.initManaged || r.isMainWorktree() || r.initRefHash(ctx) != "" {
		return nil
	}
	return r.Exclusive(func() error {
		if r.initRefHash(ctx) != "" {
			return nil
		}
		root, err := runGit(ctx, r.Placeholder, "rev-list", "--max-parents=0", "HEAD")
		if err != nil || root == "" {
			return nil
		}
		subject, err := runGit(ctx, r.Placeholder, "show", "-s", "--format=%s", root)
		if err != nil {
			return nil
		}
		if !strings.HasPrefix(subject, "base:") &&
			!(strings.HasPrefix(subject, "archive:") && r.treeEmpty(ctx, root)) {
			return nil
		}
		first := r.firstCommitAfter(ctx, root)
		if first == "" {
			return nil
		}
		bound := r.legacyInitializationBound(ctx, root, first)
		if bound == "" || bound == root {
			return nil
		}
		date, err := runGit(ctx, r.Placeholder, "show", "-s", "--format=%aI", bound)
		if err != nil {
			return nil
		}
		at, err := time.Parse(time.RFC3339, date)
		if err != nil || time.Since(at) < initAdoptWindow {
			return nil
		}
		if err := r.setInitRef(ctx, bound); err != nil {
			return err
		}
		// Mark the store as managed from now on so later scans skip the walk.
		// initDone goes first: a partial write must never look like a new,
		// unfinished initialization window.
		if err := r.SetConfig(ctx, initConfigDone, "true"); err != nil {
			return err
		}
		stamp := at.UTC().Format(time.RFC3339)
		if err := r.SetConfig(ctx, initConfigAt, stamp); err != nil {
			return err
		}
		return r.SetConfig(ctx, initConfigFirst, stamp)
	})
}

// isMainWorktree reports whether this recorder belongs to the project's main
// checkout rather than a linked worktree.
func (r *Recorder) isMainWorktree() bool {
	if r.Project.MainWorktree == "" || r.Worktree.Path == "" {
		return false
	}
	return filepath.Clean(r.Project.MainWorktree) == filepath.Clean(r.Worktree.Path)
}

// treeEmpty reports whether a commit's tree contains no files.
func (r *Recorder) treeEmpty(ctx context.Context, hash string) bool {
	output, err := runGit(ctx, r.Placeholder, "ls-tree", "-r", "--name-only", hash)
	return err == nil && strings.TrimSpace(output) == ""
}

// firstCommitAfter returns the oldest commit that is an ancestor of HEAD but
// not of hash.
func (r *Recorder) firstCommitAfter(ctx context.Context, hash string) string {
	commits, err := runGit(ctx, r.Placeholder, "rev-list", "--ancestry-path", "--reverse", hash+"..HEAD")
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(strings.TrimSpace(commits), "\n")
	return first
}

// SnapshotDetail returns the file list and patch for one commit. When files are
// set, the file list stays complete but the patch is limited to those paths.
func (r *Recorder) SnapshotDetail(ctx context.Context, hash string, files ...string) (SnapshotDetail, error) {
	hash = strings.TrimSpace(hash)
	if !commitHashPattern.MatchString(hash) {
		return SnapshotDetail{}, fmt.Errorf("invalid commit hash")
	}
	selected := cleanFileSelection(files)

	detail, err := r.commitDetail(ctx, hash)
	if err != nil {
		return SnapshotDetail{}, err
	}
	if len(selected) > 0 {
		detail.File = selected[0]
	}

	// Base and archive roots are states, not changes: they have no files or
	// patch of their own.
	if detail.Kind == "snapshot" && detail.Parent != "" {
		changed, truncated, err := r.changedFiles(ctx, detail.Parent, detail.Hash)
		if err != nil {
			return SnapshotDetail{}, err
		}
		detail.Files = changed
		detail.Truncated = truncated
		for _, file := range selected {
			if !hasSnapshotFile(detail.Files, file) {
				return SnapshotDetail{}, fmt.Errorf("%w: %s", ErrFileNotInSnapshot, file)
			}
		}
		detail.Diff, detail.DiffTool, err = r.diffBetween(ctx, detail.Parent, detail.Hash, selected)
		if err != nil {
			return SnapshotDetail{}, err
		}
		detail.Diff, detail.Truncated = boundDiff(detail.Diff, detail.Truncated)
	}
	return detail, nil
}

// RangeDetail returns the combined change of a contiguous snapshot range. from
// is the oldest and to the newest selection; the patch starts at the parent of
// from so the oldest selected change is included. When from is a root (base or
// archive) its tree is the starting point.
func (r *Recorder) RangeDetail(ctx context.Context, from, to string, files ...string) (SnapshotDetail, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if !commitHashPattern.MatchString(from) || !commitHashPattern.MatchString(to) {
		return SnapshotDetail{}, fmt.Errorf("invalid commit hash")
	}
	selected := cleanFileSelection(files)

	fromHash, err := r.resolveCommit(ctx, from)
	if err != nil {
		return SnapshotDetail{}, err
	}
	toHash, err := r.resolveCommit(ctx, to)
	if err != nil {
		return SnapshotDetail{}, err
	}
	if _, code, err := runGitStatus(ctx, r.Placeholder, "merge-base", "--is-ancestor", fromHash, toHash); err != nil {
		return SnapshotDetail{}, fmt.Errorf("compare snapshots: %w", err)
	} else if code != 0 {
		return SnapshotDetail{}, fmt.Errorf("snapshots are not on the same timeline")
	}

	start := fromHash
	if parent, err := runGit(ctx, r.Placeholder, "rev-parse", "--verify", fromHash+"^"); err == nil {
		if value := strings.TrimSpace(parent); value != "" {
			start = value
		}
	}

	detail, err := r.commitDetail(ctx, toHash)
	if err != nil {
		return SnapshotDetail{}, err
	}
	detail.Kind = "range"
	detail.From = fromHash
	if len(selected) > 0 {
		detail.File = selected[0]
	}
	// A range is not a single snapshot; labelling it would be ambiguous.
	detail.Label = ""

	changed, truncated, err := r.changedFiles(ctx, start, toHash)
	if err != nil {
		return SnapshotDetail{}, err
	}
	detail.Files = changed
	detail.Truncated = truncated
	for _, file := range selected {
		if !hasSnapshotFile(detail.Files, file) {
			return SnapshotDetail{}, fmt.Errorf("%w: %s", ErrFileNotInSnapshot, file)
		}
	}
	detail.Diff, detail.DiffTool, err = r.diffBetween(ctx, start, toHash, selected)
	if err != nil {
		return SnapshotDetail{}, err
	}
	detail.Diff, detail.Truncated = boundDiff(detail.Diff, detail.Truncated)

	if output, err := runGit(ctx, r.Placeholder, "rev-list", "--count", start+".."+toHash); err == nil {
		if count, err := strconv.Atoi(strings.TrimSpace(output)); err == nil {
			if start == fromHash {
				// The root is part of the selection and is not in start..to.
				count++
			}
			detail.Count = count
		}
	}
	return detail, nil
}

// WorktreeDetail returns the combined change from the hidden initialization
// baseline to the newest snapshot. It is the "all snapshots" view: new
// snapshots are included automatically because HEAD is resolved per request.
func (r *Recorder) WorktreeDetail(ctx context.Context, files ...string) (SnapshotDetail, error) {
	selected := cleanFileSelection(files)

	head, err := runGit(ctx, r.Placeholder, "rev-parse", "HEAD")
	if err != nil {
		return SnapshotDetail{}, fmt.Errorf("resolve newest snapshot: %w", err)
	}
	if head == "" {
		return SnapshotDetail{}, fmt.Errorf("resolve newest snapshot")
	}
	start := r.timelineLowerBound(ctx)
	if start == "" {
		root, rootErr := runGit(ctx, r.Placeholder, "rev-list", "--max-parents=0", "HEAD")
		if rootErr != nil {
			return SnapshotDetail{}, fmt.Errorf("resolve initialization baseline: %w", rootErr)
		}
		start = root
	}
	if start == "" {
		return SnapshotDetail{}, fmt.Errorf("resolve initialization baseline")
	}
	if _, code, err := runGitStatus(ctx, r.Placeholder, "merge-base", "--is-ancestor", start, head); err != nil || code != 0 {
		return SnapshotDetail{}, fmt.Errorf("initialization baseline is not part of this timeline")
	}

	detail, err := r.commitDetail(ctx, head)
	if err != nil {
		return SnapshotDetail{}, err
	}
	detail.Kind = "all"
	detail.From = start
	// The all view is not a single snapshot; labelling it would be ambiguous.
	detail.Label = ""
	if len(selected) > 0 {
		detail.File = selected[0]
	}
	if start == head {
		for _, file := range selected {
			return SnapshotDetail{}, fmt.Errorf("%w: %s", ErrFileNotInSnapshot, file)
		}
		detail.Files = []SnapshotFile{}
		return detail, nil
	}

	changed, truncated, err := r.changedFiles(ctx, start, head)
	if err != nil {
		return SnapshotDetail{}, err
	}
	detail.Files = changed
	detail.Truncated = truncated
	for _, file := range selected {
		if !hasSnapshotFile(detail.Files, file) {
			return SnapshotDetail{}, fmt.Errorf("%w: %s", ErrFileNotInSnapshot, file)
		}
	}
	detail.Diff, detail.DiffTool, err = r.diffBetween(ctx, start, head, selected)
	if err != nil {
		return SnapshotDetail{}, err
	}
	detail.Diff, detail.Truncated = boundDiff(detail.Diff, detail.Truncated)

	if output, err := runGit(ctx, r.Placeholder, "rev-list", "--count", start+".."+head); err == nil {
		if count, err := strconv.Atoi(strings.TrimSpace(output)); err == nil {
			detail.Count = count
		}
	}
	return detail, nil
}

func (r *Recorder) commitDetail(ctx context.Context, hash string) (SnapshotDetail, error) {
	meta, err := runGit(ctx, r.Placeholder, "show", "-s", "--format=%H%x00%P%x00%aI%x00%s", hash)
	if err != nil {
		if _, _, checkErr := runGitRaw(ctx, r.Placeholder, "cat-file", "-e", hash+"^{commit}"); checkErr != nil {
			return SnapshotDetail{}, fmt.Errorf("%w: %s", ErrSnapshotNotFound, hash)
		}
		return SnapshotDetail{}, fmt.Errorf("snapshot metadata: %w", err)
	}
	parts := strings.SplitN(meta, "\x00", 4)
	if len(parts) != 4 {
		return SnapshotDetail{}, fmt.Errorf("unexpected snapshot metadata")
	}

	detail := SnapshotDetail{
		Hash:    parts[0],
		Time:    parts[2],
		Subject: parts[3],
		Files:   []SnapshotFile{},
	}
	if parent := strings.Fields(parts[1]); len(parent) > 0 {
		detail.Parent = parent[0]
	}
	detail.Kind, _, _ = classifySubject(detail.Subject)
	detail.Label = r.labelMap()[detail.Hash]
	return detail, nil
}

func (r *Recorder) resolveCommit(ctx context.Context, hash string) (string, error) {
	full, err := runGit(ctx, r.Placeholder, "rev-parse", "--verify", hash+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrSnapshotNotFound, hash)
	}
	return strings.TrimSpace(full), nil
}

// changedFiles lists the paths between two commits. The byte-for-byte cap keeps
// a hostile or enormous snapshot from filling the UI.
func (r *Recorder) changedFiles(ctx context.Context, from, to string) ([]SnapshotFile, bool, error) {
	output, err := runGit(ctx, r.Placeholder, "diff-tree", "--no-commit-id", "--name-status", "-r", from, to)
	if err != nil {
		return nil, false, fmt.Errorf("snapshot files: %w", err)
	}
	files := make([]SnapshotFile, 0)
	truncated := false
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		path := fields[len(fields)-1]
		oldPath := ""
		if len(status) > 1 {
			if status[0] == 'R' || status[0] == 'C' {
				if len(fields) >= 3 {
					oldPath = fields[1]
				}
			}
			status = status[:1]
		}
		files = append(files, SnapshotFile{Path: path, OldPath: oldPath, Status: status})
		if len(files) >= snapshotMaxFiles {
			truncated = true
			break
		}
	}
	return files, truncated, nil
}

func boundDiff(diff string, truncated bool) (string, bool) {
	if len(diff) <= snapshotMaxDiff {
		return diff, truncated
	}
	cut := strings.LastIndexByte(diff[:snapshotMaxDiff], '\n')
	if cut <= 0 {
		cut = snapshotMaxDiff
	}
	return diff[:cut], true
}

// diffBetween renders the patch between two commits. Delta or difftastic is
// used when it was resolved and produces output; Git's unified diff is the
// fallback.
func (r *Recorder) diffBetween(ctx context.Context, from, to string, files []string) (string, string, error) {
	pathspec := []string{}
	if len(files) > 0 {
		pathspec = append(pathspec, "--")
		for _, file := range files {
			pathspec = append(pathspec, literalPathspec(file))
		}
	}
	theme := diffTheme(ctx)
	tabWidth := difft.NormalizeTabWidth(r.DiffTabWidth)

	if r.Diff.Enabled() {
		switch r.Diff.Name {
		case difft.ModeDelta:
			args := []string{
				"diff", "--find-renames", "--unified=1",
				"--no-color", "--no-ext-diff", "--no-textconv", from, to,
			}
			args = append(args, pathspec...)
			diff, err := runGit(ctx, r.Placeholder, args...)
			if err == nil {
				if text := strings.TrimSpace(diff); text != "" {
					rendered, renderErr := runDelta(ctx, r.Diff.Path, text, theme, tabWidth)
					if renderErr == nil && strings.TrimSpace(rendered) != "" {
						return rendered, "delta", nil
					}
				}
				return expandDiffTabs(diff, tabWidth), "git", nil
			}
		default:
			env := []string{
				"GIT_EXTERNAL_DIFF=" + r.Diff.Path,
				"DFT_DISPLAY=inline",
				"DFT_COLOR=always",
				"DFT_WIDTH=160",
				"DFT_CONTEXT=1",
				"DFT_SYNTAX_HIGHLIGHT=on",
				"DFT_BACKGROUND=" + theme,
				"DFT_TAB_WIDTH=" + strconv.Itoa(tabWidth),
			}
			args := []string{
				"diff", "--find-renames", "--unified=1", "--no-textconv",
				"--ext-diff", from, to,
			}
			args = append(args, pathspec...)
			output, err := runGitEnv(ctx, r.Placeholder, env, args...)
			if err == nil {
				if text := strings.TrimSpace(output); text != "" {
					return text, "difft", nil
				}
			}
		}
	}

	args := []string{
		"diff", "--find-renames", "--unified=1",
		"--no-color", "--no-ext-diff", "--no-textconv", from, to,
	}
	args = append(args, pathspec...)
	diff, err := runGit(ctx, r.Placeholder, args...)
	if err != nil {
		return "", "", fmt.Errorf("snapshot diff: %w", err)
	}
	return expandDiffTabs(diff, tabWidth), "git", nil
}

// expandDiffTabs replaces tabs with spaces in Git's unified diff, which has no
// tab-width option of its own.
func expandDiffTabs(diff string, tabWidth int) string {
	return strings.ReplaceAll(diff, "\t", strings.Repeat(" ", difft.NormalizeTabWidth(tabWidth)))
}

// literalPathspec disables Git pathspec magic so a file name such as ":foo"
// is matched literally.
func literalPathspec(file string) string {
	return ":(literal)" + file
}

func hasSnapshotFile(files []SnapshotFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

// cleanFileSelection trims, deduplicates and drops empty pathspecs.
func cleanFileSelection(files []string) []string {
	seen := make(map[string]bool, len(files))
	selected := make([]string, 0, len(files))
	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		selected = append(selected, file)
	}
	return selected
}

// classifySubject turns a history commit subject into its kind and, for
// snapshots, the project branch and HEAD it recorded.
func classifySubject(subject string) (kind, branch, head string) {
	if strings.HasPrefix(subject, "snap: ") {
		kind = "snapshot"
		rest := strings.TrimPrefix(subject, "snap: ")
		open := strings.LastIndex(rest, " [")
		if open > 0 && strings.HasSuffix(rest, "]") {
			inner := rest[open+2 : len(rest)-1]
			if at := strings.LastIndex(inner, "@"); at > 0 {
				branch = inner[:at]
				head = inner[at+1:]
			}
		}
		return kind, branch, head
	}
	if strings.HasPrefix(subject, "archive:") {
		return "archive", "", ""
	}
	if strings.HasPrefix(subject, "base:") {
		return "base", "", ""
	}
	return "commit", "", ""
}
