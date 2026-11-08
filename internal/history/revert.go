package history

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// emptyTree is Git's well-known empty tree hash. It is the base for a revert
// whose oldest selected snapshot has no parent.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// RevertResult lists the paths written back, deleted and left unchanged.
type RevertResult struct {
	Restored  []string `json:"restored"`
	Deleted   []string `json:"deleted"`
	Unchanged []string `json:"unchanged"`
	Skipped   []string `json:"skipped"`
}

type restoreEntry struct {
	Path string
	Hash string
	Mode uint32
	Type string
}

type restoreOutcome struct {
	Restored  []string
	Unchanged []string
	Skipped   []string
}

// Revert undoes the changes introduced by the selected snapshots. from and to
// are the range endpoints; for a single snapshot both are the same hash. The
// base is the parent of from, so the first visible snapshot can be reverted
// even when its parent is the hidden initialization copy. Only files changed
// in that range are touched.
func (r *Recorder) Revert(ctx context.Context, from, to string) (RevertResult, error) {
	if r.lock != nil {
		r.lock.Lock()
		defer r.lock.Unlock()
	}

	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if !commitHashPattern.MatchString(from) || !commitHashPattern.MatchString(to) {
		return RevertResult{}, fmt.Errorf("invalid commit hash")
	}

	fromHash, err := r.resolveCommit(ctx, from)
	if err != nil {
		return RevertResult{}, err
	}
	toHash, err := r.resolveCommit(ctx, to)
	if err != nil {
		return RevertResult{}, err
	}
	if _, code, err := runGitStatus(ctx, r.Placeholder, "merge-base", "--is-ancestor", fromHash, toHash); err != nil {
		return RevertResult{}, fmt.Errorf("compare snapshots: %w", err)
	} else if code != 0 {
		return RevertResult{}, fmt.Errorf("snapshots are not on the same timeline")
	}

	base := emptyTree
	if parent, err := runGit(ctx, r.Placeholder, "rev-parse", "--verify", fromHash+"^"); err == nil {
		if value := strings.TrimSpace(parent); value != "" {
			base = value
		}
	}

	files, truncated, err := r.changedFiles(ctx, base, toHash)
	if err != nil {
		return RevertResult{}, err
	}
	if truncated {
		return RevertResult{}, fmt.Errorf(
			"selection has more than %d changed files; revert it in smaller ranges",
			snapshotMaxFiles,
		)
	}

	root, err := worktreeRoot(r)
	if err != nil {
		return RevertResult{}, err
	}

	result := RevertResult{
		Restored:  []string{},
		Deleted:   []string{},
		Unchanged: []string{},
		Skipped:   []string{},
	}
	for _, file := range files {
		switch file.Status {
		case "A", "C":
			changed, err := deleteRestoredPath(root, file.Path)
			if err != nil {
				result.Skipped = append(result.Skipped, file.Path)
			} else if changed {
				result.Deleted = append(result.Deleted, file.Path)
			} else {
				result.Unchanged = append(result.Unchanged, file.Path)
			}
		case "R":
			if file.OldPath == "" {
				result.Skipped = append(result.Skipped, file.Path)
				continue
			}
			outcome, err := r.restoreFromCommit(ctx, base, file.OldPath, root)
			if err != nil {
				result.Skipped = append(result.Skipped, file.OldPath)
			} else {
				mergeRestoreOutcome(&result, outcome)
			}
			changed, err := deleteRestoredPath(root, file.Path)
			if err != nil {
				result.Skipped = append(result.Skipped, file.Path)
			} else if changed {
				result.Deleted = append(result.Deleted, file.Path)
			} else {
				result.Unchanged = append(result.Unchanged, file.Path)
			}
		default:
			outcome, err := r.restoreFromCommit(ctx, base, file.Path, root)
			if err != nil {
				result.Skipped = append(result.Skipped, file.Path)
			} else {
				mergeRestoreOutcome(&result, outcome)
			}
		}
	}
	return result, nil
}

func mergeRestoreOutcome(result *RevertResult, outcome restoreOutcome) {
	result.Restored = append(result.Restored, outcome.Restored...)
	result.Unchanged = append(result.Unchanged, outcome.Unchanged...)
	result.Skipped = append(result.Skipped, outcome.Skipped...)
}

func worktreeRoot(r *Recorder) (string, error) {
	root, err := filepath.Abs(r.Worktree.Path)
	if err != nil {
		return "", fmt.Errorf("resolve worktree: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root, nil
}

// restoreFromCommit writes one path from a commit tree into the worktree. The
// caller holds the repository lock and has already resolved the root.
func (r *Recorder) restoreFromCommit(ctx context.Context, hash, relPath, root string) (restoreOutcome, error) {
	cleaned, err := cleanRestorePath(relPath)
	if err != nil {
		return restoreOutcome{}, err
	}
	entries, err := restoreEntries(ctx, r.Placeholder, hash, cleaned)
	if err != nil {
		return restoreOutcome{}, err
	}
	if len(entries) == 0 {
		return restoreOutcome{}, fmt.Errorf("path %q does not exist in the snapshot", cleaned)
	}

	hashes := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type == "blob" {
			hashes = append(hashes, entry.Hash)
		}
	}
	blobs, err := readBlobs(ctx, r.Placeholder, hashes)
	if err != nil {
		return restoreOutcome{}, err
	}

	outcome := restoreOutcome{Restored: []string{}, Unchanged: []string{}, Skipped: []string{}}
	blobIndex := 0
	for _, entry := range entries {
		if entry.Type != "blob" {
			outcome.Skipped = append(outcome.Skipped, entry.Path)
			continue
		}
		content := blobs[blobIndex]
		blobIndex++
		changed, err := writeRestoredFile(root, entry, content)
		if err != nil {
			outcome.Skipped = append(outcome.Skipped, entry.Path)
			continue
		}
		if changed {
			outcome.Restored = append(outcome.Restored, entry.Path)
		} else {
			outcome.Unchanged = append(outcome.Unchanged, entry.Path)
		}
	}
	return outcome, nil
}

func cleanRestorePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path is required")
	}
	cleaned := filepath.Clean(value)
	if cleaned == "." || cleaned == ".." || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("path must be relative to the worktree")
	}
	if strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must stay inside the worktree")
	}
	cleaned = filepath.ToSlash(cleaned)
	if cleaned == ".git" || strings.HasPrefix(cleaned, ".git/") {
		return "", fmt.Errorf("the Git directory cannot be restored")
	}
	return cleaned, nil
}

func restoreEntries(ctx context.Context, placeholder, hash, relPath string) ([]restoreEntry, error) {
	output, err := runGit(ctx, placeholder, "ls-tree", "-r", "-l", "-z", hash, "--", relPath)
	if err != nil {
		return nil, fmt.Errorf("list snapshot paths: %w", err)
	}

	entries := make([]restoreEntry, 0)
	for _, line := range strings.Split(output, "\x00") {
		if line == "" {
			continue
		}
		meta, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 4 {
			continue
		}
		mode, err := strconv.ParseUint(fields[0], 8, 32)
		if err != nil {
			continue
		}
		entries = append(entries, restoreEntry{
			Path: path,
			Type: fields[1],
			Hash: fields[2],
			Mode: uint32(mode),
		})
	}
	return entries, nil
}

// readBlobs reads all blob contents in one git process. cat-file --batch emits
// `<hash> <type> <size>` followed by the exact bytes, in input order.
func readBlobs(ctx context.Context, placeholder string, hashes []string) ([][]byte, error) {
	if len(hashes) == 0 {
		return [][]byte{}, nil
	}

	command := exec.CommandContext(ctx, "git", "-C", placeholder, "cat-file", "--batch")
	command.Stdin = strings.NewReader(strings.Join(hashes, "\n") + "\n")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("read snapshot objects: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	reader := bufio.NewReader(bytes.NewReader(stdout.Bytes()))
	blobs := make([][]byte, 0, len(hashes))
	for range hashes {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read snapshot object header: %w", err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unexpected snapshot object header %q", strings.TrimSpace(header))
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("parse snapshot object size: %w", err)
		}
		content := make([]byte, size)
		if _, err := io.ReadFull(reader, content); err != nil {
			return nil, fmt.Errorf("read snapshot object: %w", err)
		}
		// Trailing newline after the binary payload.
		if _, err := reader.ReadByte(); err != nil {
			return nil, fmt.Errorf("read snapshot object separator: %w", err)
		}
		blobs = append(blobs, content)
	}
	return blobs, nil
}

// writeRestoredFile writes one entry and reports whether the worktree changed.
func writeRestoredFile(root string, entry restoreEntry, content []byte) (bool, error) {
	name := filepath.Clean(filepath.FromSlash(entry.Path))
	if name == "." || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("path escapes the worktree")
	}
	dest := filepath.Join(root, name)

	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return false, err
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return false, err
	}
	if !withinRoot(root, realParent) {
		return false, fmt.Errorf("parent escapes the worktree")
	}

	if entry.Mode&0o170000 == 0o120000 {
		if current, err := os.Readlink(dest); err == nil && current == string(content) {
			return false, nil
		}
		if info, err := os.Lstat(dest); err == nil && info.IsDir() {
			return false, fmt.Errorf("destination is a directory")
		}
		_ = os.Remove(dest)
		return true, os.Symlink(string(content), dest)
	}

	info, err := os.Lstat(dest)
	if err == nil && info.IsDir() {
		return false, fmt.Errorf("destination is a directory")
	}
	if err == nil {
		current, readErr := os.ReadFile(dest)
		if readErr == nil && bytes.Equal(current, content) && modeMatches(info.Mode(), entry.Mode) {
			return false, nil
		}
	}

	mode := os.FileMode(0o644)
	if entry.Mode&0o111 != 0 {
		mode = 0o755
	}
	tmp, err := os.CreateTemp(parent, ".tachograph-restore-*")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return false, err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return false, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return false, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return false, err
	}
	return true, nil
}

func modeMatches(current os.FileMode, entryMode uint32) bool {
	want := os.FileMode(0o644)
	if entryMode&0o111 != 0 {
		want = 0o755
	}
	return current.Perm() == want
}

// deleteRestoredPath removes a path that was added in the reverted range. It
// refuses directories and paths that escape the worktree.
func deleteRestoredPath(root, relPath string) (bool, error) {
	cleaned, err := cleanRestorePath(relPath)
	if err != nil {
		return false, err
	}
	name := filepath.Clean(filepath.FromSlash(cleaned))
	if name == "." || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("path escapes the worktree")
	}
	dest := filepath.Join(root, name)
	parent := filepath.Dir(dest)
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !withinRoot(root, realParent) {
		return false, fmt.Errorf("parent escapes the worktree")
	}
	info, err := os.Lstat(dest)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("destination is a directory")
	}
	if err := os.Remove(dest); err != nil {
		return false, err
	}
	return true, nil
}

func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
