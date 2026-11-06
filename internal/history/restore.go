package history

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// RestoreResult lists the paths written back and the ones that were skipped.
type RestoreResult struct {
	Restored []string `json:"restored"`
	Skipped  []string `json:"skipped"`
}

type restoreEntry struct {
	Path string
	Hash string
	Mode uint32
	Type string
}

// Restore writes a file or directory tree from a snapshot back into the real
// worktree. It never deletes files that are absent from the snapshot and skips
// submodules; symlinks are restored as symlinks.
func (r *Recorder) Restore(ctx context.Context, hash, relPath string) (RestoreResult, error) {
	if r.lock != nil {
		r.lock.Lock()
		defer r.lock.Unlock()
	}
	hash = strings.TrimSpace(hash)
	if !commitHashPattern.MatchString(hash) {
		return RestoreResult{}, fmt.Errorf("invalid commit hash")
	}
	cleaned, err := cleanRestorePath(relPath)
	if err != nil {
		return RestoreResult{}, err
	}

	entries, err := restoreEntries(ctx, r.Placeholder, hash, cleaned)
	if err != nil {
		return RestoreResult{}, err
	}
	if len(entries) == 0 {
		return RestoreResult{}, fmt.Errorf("path %q does not exist in the snapshot", cleaned)
	}

	hashes := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type == "blob" {
			hashes = append(hashes, entry.Hash)
		}
	}
	blobs, err := readBlobs(ctx, r.Placeholder, hashes)
	if err != nil {
		return RestoreResult{}, err
	}

	root, err := filepath.Abs(r.Worktree.Path)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolve worktree: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	result := RestoreResult{Restored: []string{}, Skipped: []string{}}
	blobIndex := 0
	for _, entry := range entries {
		if entry.Type != "blob" {
			result.Skipped = append(result.Skipped, entry.Path)
			continue
		}
		content := blobs[blobIndex]
		blobIndex++
		if err := writeRestoredFile(root, entry, content); err != nil {
			result.Skipped = append(result.Skipped, entry.Path)
			continue
		}
		result.Restored = append(result.Restored, entry.Path)
	}
	return result, nil
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

func writeRestoredFile(root string, entry restoreEntry, content []byte) error {
	name := filepath.Clean(filepath.FromSlash(entry.Path))
	if name == "." || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes the worktree")
	}
	dest := filepath.Join(root, name)

	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	if !withinRoot(root, realParent) {
		return fmt.Errorf("parent escapes the worktree")
	}

	if entry.Mode&0o170000 == 0o120000 {
		if info, err := os.Lstat(dest); err == nil && info.IsDir() {
			return fmt.Errorf("destination is a directory")
		}
		_ = os.Remove(dest)
		return os.Symlink(string(content), dest)
	}

	info, err := os.Lstat(dest)
	if err == nil && info.IsDir() {
		return fmt.Errorf("destination is a directory")
	}

	mode := os.FileMode(0o644)
	if entry.Mode&0o111 != 0 {
		mode = 0o755
	}
	tmp, err := os.CreateTemp(parent, ".tachograph-restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
