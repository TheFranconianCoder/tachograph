// Package difft resolves the optional delta or difftastic binary that renders
// snapshot patches. When neither is present, Tachograph falls back to Git's
// unified diff.
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
	ModeDelta = "delta"
)

var (
	deltaNames = []string{"delta", "git-delta"}
	difftNames = []string{"difft", "difftastic"}
)

// Tool identifies the renderer for snapshot patches. An empty Path means Git's
// built-in unified diff.
type Tool struct {
	Name string
	Path string
}

// Enabled reports whether an external diff renderer is available.
func (t Tool) Enabled() bool {
	return t.Path != "" && (t.Name == ModeDifft || t.Name == ModeDelta)
}

// Resolve picks the renderer. Delta has priority in auto mode, then difftastic.
// An explicit path or name wins, then PATH, then a mise installation or the
// common cargo/local bin locations.
func Resolve(mode, configured string) Tool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeGit:
		return Tool{Name: ModeGit}
	case ModeDelta:
		if path := find(configured, deltaNames); path != "" {
			return Tool{Name: ModeDelta, Path: path}
		}
		return Tool{Name: ModeGit}
	case ModeDifft:
		if path := find(configured, difftNames); path != "" {
			return Tool{Name: ModeDifft, Path: path}
		}
		return Tool{Name: ModeGit}
	default:
		if configured = strings.TrimSpace(configured); configured != "" {
			if path := executable(configured); path != "" {
				return Tool{Name: toolName(path), Path: path}
			}
		}
		if path := find("", deltaNames); path != "" {
			return Tool{Name: ModeDelta, Path: path}
		}
		if path := find("", difftNames); path != "" {
			return Tool{Name: ModeDifft, Path: path}
		}
		return Tool{Name: ModeGit}
	}
}

// toolName infers an auto-detected renderer from its binary name.
func toolName(path string) string {
	if strings.Contains(strings.ToLower(filepath.Base(path)), "delta") {
		return ModeDelta
	}
	return ModeDifft
}

func find(configured string, names []string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return executable(configured)
	}

	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil && isExecutable(path) {
			return path
		}
	}

	var candidates []string
	for _, root := range miseRoots() {
		for _, name := range names {
			candidates = append(candidates, filepath.Join(root, "shims", name))
			candidates = append(candidates, glob(filepath.Join(root, "installs", name, "*", name))...)
			candidates = append(candidates, glob(filepath.Join(root, "installs", name, "*", ".mise-bins", name))...)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, name := range names {
			candidates = append(candidates,
				filepath.Join(home, ".cargo", "bin", name),
				filepath.Join(home, ".local", "bin", name),
			)
		}
	}
	for i := len(candidates) - 1; i >= 0; i-- {
		if isExecutable(candidates[i]) {
			return candidates[i]
		}
	}
	return ""
}

func executable(configured string) string {
	if path, err := exec.LookPath(configured); err == nil && isExecutable(path) {
		return path
	}
	if isExecutable(configured) {
		return configured
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
