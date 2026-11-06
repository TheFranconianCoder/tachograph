package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const labelMaxLength = 120

// labelFile is the on-disk label store of one project. Labels are keyed by the
// full commit hash and survive snapshots, restores and compaction (which remaps
// rewritten hashes).
type labelFile struct {
	Labels map[string]string `json:"labels"`
}

func labelPath(historyRepo string) string {
	return filepath.Join(filepath.Dir(historyRepo), "labels.json")
}

func readLabels(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file labelFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Labels == nil {
		return map[string]string{}, nil
	}
	return file.Labels, nil
}

func writeLabels(path string, labels map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(labelFile{Labels: labels}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// labelMap loads the project labels. A broken label file must not break the
// timeline, so it is logged and treated as empty.
func (r *Recorder) labelMap() map[string]string {
	path := labelPath(r.HistoryRepo)
	labels, err := readLabels(path)
	if err != nil {
		slog.Warn("could not read snapshot labels", "path", path, "err", err)
		return map[string]string{}
	}
	return labels
}

// SetLabel stores a label for one snapshot. An empty label removes it. The
// commit hash is resolved first, so short hashes (and deep links) work.
func (r *Recorder) SetLabel(ctx context.Context, hash, label string) (string, error) {
	if r.lock != nil {
		r.lock.Lock()
		defer r.lock.Unlock()
	}

	hash = strings.TrimSpace(hash)
	if !commitHashPattern.MatchString(hash) {
		return "", fmt.Errorf("invalid commit hash")
	}
	full, err := runGit(ctx, r.Placeholder, "rev-parse", "--verify", hash+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrSnapshotNotFound, hash)
	}
	full = strings.TrimSpace(full)

	label = cleanLabel(label)
	if utf8.RuneCountInString(label) > labelMaxLength {
		return "", fmt.Errorf("label is longer than %d characters", labelMaxLength)
	}

	path := labelPath(r.HistoryRepo)
	labels, err := readLabels(path)
	if err != nil {
		return "", fmt.Errorf("read snapshot labels: %w", err)
	}
	if label == "" {
		delete(labels, full)
	} else {
		labels[full] = label
	}
	if len(labels) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("remove empty snapshot labels: %w", err)
		}
		return label, nil
	}
	if err := writeLabels(path, labels); err != nil {
		return "", fmt.Errorf("write snapshot labels: %w", err)
	}
	return label, nil
}

// cleanLabel folds whitespace and drops control characters, so a label always
// stays one readable line.
func cleanLabel(label string) string {
	label = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, label)
	return strings.Join(strings.Fields(label), " ")
}

// remapLabels follows commit rewrites during compaction: kept snapshots keep
// their label under the new hash, dropped snapshots lose theirs.
func remapLabels(repo string, mapping map[string]string) error {
	path := labelPath(repo)
	labels, err := readLabels(path)
	if err != nil {
		return fmt.Errorf("read snapshot labels: %w", err)
	}
	if len(labels) == 0 {
		return nil
	}

	next := make(map[string]string, len(labels))
	changed := false
	for hash, label := range labels {
		mapped, ok := mapping[hash]
		if !ok {
			changed = true
			continue
		}
		next[mapped] = label
		if mapped != hash {
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if len(next) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeLabels(path, next)
}
