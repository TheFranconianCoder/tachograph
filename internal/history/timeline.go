package history

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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
	Path   string `json:"path"`
	Status string `json:"status"`
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

// SnapshotDetail returns the file list and patch for one commit. When file is
// set, the file list stays complete but the patch is limited to that path.
func (r *Recorder) SnapshotDetail(ctx context.Context, hash, file string) (SnapshotDetail, error) {
	hash = strings.TrimSpace(hash)
	if !commitHashPattern.MatchString(hash) {
		return SnapshotDetail{}, fmt.Errorf("invalid commit hash")
	}
	file = strings.TrimSpace(file)

	detail, err := r.commitDetail(ctx, hash)
	if err != nil {
		return SnapshotDetail{}, err
	}
	detail.File = file

	// Base and archive roots are states, not changes: they have no files or
	// patch of their own.
	if detail.Kind == "snapshot" && detail.Parent != "" {
		files, truncated, err := r.changedFiles(ctx, detail.Parent, detail.Hash)
		if err != nil {
			return SnapshotDetail{}, err
		}
		detail.Files = files
		detail.Truncated = truncated
		if file != "" && !hasSnapshotFile(detail.Files, file) {
			return SnapshotDetail{}, fmt.Errorf("%w: %s", ErrFileNotInSnapshot, file)
		}
		detail.Diff, detail.DiffTool, err = r.diffBetween(ctx, detail.Parent, detail.Hash, file)
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
func (r *Recorder) RangeDetail(ctx context.Context, from, to, file string) (SnapshotDetail, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if !commitHashPattern.MatchString(from) || !commitHashPattern.MatchString(to) {
		return SnapshotDetail{}, fmt.Errorf("invalid commit hash")
	}
	file = strings.TrimSpace(file)

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
	detail.File = file
	// A range is not a single snapshot; labelling it would be ambiguous.
	detail.Label = ""

	files, truncated, err := r.changedFiles(ctx, start, toHash)
	if err != nil {
		return SnapshotDetail{}, err
	}
	detail.Files = files
	detail.Truncated = truncated
	if file != "" && !hasSnapshotFile(detail.Files, file) {
		return SnapshotDetail{}, fmt.Errorf("%w: %s", ErrFileNotInSnapshot, file)
	}
	detail.Diff, detail.DiffTool, err = r.diffBetween(ctx, start, toHash, file)
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
		if len(status) > 1 {
			status = status[:1]
		}
		files = append(files, SnapshotFile{Path: path, Status: status})
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

// diffBetween renders the patch between two commits. difftastic is used when it
// was resolved and produces output; Git's unified diff is the fallback.
func (r *Recorder) diffBetween(ctx context.Context, from, to, file string) (string, string, error) {
	pathspec := []string{}
	if file != "" {
		pathspec = []string{"--", literalPathspec(file)}
	}

	if r.Diff.Enabled() {
		env := []string{
			"GIT_EXTERNAL_DIFF=" + r.Diff.Path,
			"DFT_DISPLAY=inline",
			"DFT_COLOR=always",
			"DFT_WIDTH=160",
			"DFT_CONTEXT=3",
			"DFT_SYNTAX_HIGHLIGHT=on",
			"DFT_BACKGROUND=dark",
		}
		args := []string{
			"diff", "--find-renames", "--unified=3", "--no-textconv",
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

	args := []string{
		"diff", "--find-renames", "--unified=3",
		"--no-color", "--no-ext-diff", "--no-textconv", from, to,
	}
	args = append(args, pathspec...)
	diff, err := runGit(ctx, r.Placeholder, args...)
	if err != nil {
		return "", "", fmt.Errorf("snapshot diff: %w", err)
	}
	return diff, "git", nil
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
