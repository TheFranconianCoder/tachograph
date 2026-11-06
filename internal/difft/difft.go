// Package difft resolves the optional difftastic binary that renders snapshot
// patches. When it is absent, Tachograph falls back to Git's unified diff.
package difft

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Modes accepted by diff.tool.
const (
	ModeAuto  = "auto"
	ModeGit   = "git"
	ModeDifft = "difft"
)

// Tool identifies the renderer for snapshot patches. An empty Path means Git's
// built-in unified diff.
type Tool struct {
	Name string
	Path string
}

// Enabled reports whether the structural diff renderer is available.
func (t Tool) Enabled() bool {
	return t.Name == ModeDifft && t.Path != ""
}

// Resolve picks the renderer. An explicit path or name wins, then PATH, then a
// mise installation or the common cargo/local bin locations.
func Resolve(mode, configured string) Tool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeGit:
		return Tool{Name: ModeGit}
	case ModeDifft:
		if path := find(configured); path != "" {
			return Tool{Name: ModeDifft, Path: path}
		}
		return Tool{Name: ModeGit}
	default:
		if path := find(configured); path != "" {
			return Tool{Name: ModeDifft, Path: path}
		}
		return Tool{Name: ModeGit}
	}
}

func find(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if path, err := exec.LookPath(configured); err == nil && isExecutable(path) {
			return path
		}
		if isExecutable(configured) {
			return configured
		}
		return ""
	}

	for _, name := range []string{"difft", "difftastic"} {
		if path, err := exec.LookPath(name); err == nil && isExecutable(path) {
			return path
		}
	}

	var candidates []string
	for _, root := range miseRoots() {
		candidates = append(candidates, filepath.Join(root, "shims", "difft"))
		for _, tool := range []string{"difftastic", "difft"} {
			candidates = append(candidates, glob(filepath.Join(root, "installs", tool, "*", "difft"))...)
			candidates = append(candidates, glob(filepath.Join(root, "installs", tool, "*", ".mise-bins", "difft"))...)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".cargo", "bin", "difft"),
			filepath.Join(home, ".local", "bin", "difft"),
		)
	}
	for i := len(candidates) - 1; i >= 0; i-- {
		if isExecutable(candidates[i]) {
			return candidates[i]
		}
	}
	return ""
}

func miseRoots() []string {
	var roots []string
	if dir := strings.TrimSpace(os.Getenv("MISE_DATA_DIR")); dir != "" {
		roots = append(roots, dir)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, ".local", "share", "mise"))
	}
	return roots
}

func glob(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
