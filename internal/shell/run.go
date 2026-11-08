package shell

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"
)

// Environment markers set in the child shell. Existing values are replaced.
const (
	EnvProject      = "TACHOGRAPH_PROJECT"
	EnvWorktree     = "TACHOGRAPH_WORKTREE"
	EnvWorktreeID   = "TACHOGRAPH_WORKTREE_ID"
	EnvBranch       = "TACHOGRAPH_BRANCH"
	EnvMainWorktree = "TACHOGRAPH_MAIN_WORKTREE"
	EnvHistoryRepo  = "TACHOGRAPH_HISTORY_REPO"
)

// ExitError carries the exit code of a shell that ran as a child process. It
// is only used on Windows; Unix replaces the process instead.
type ExitError struct{ Code int }

func (e *ExitError) Error() string {
	return fmt.Sprintf("shell exited with code %d", e.Code)
}

// Run starts an interactive shell inside the worktree.
func Run(candidate Candidate) error {
	info, err := os.Stat(candidate.Path)
	if err != nil {
		return fmt.Errorf("worktree %s: %w", candidate.Path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("worktree %s is not a directory", candidate.Path)
	}
	return runShell(candidate)
}

// Banner is the multi-line context printed before the shell starts.
func Banner(candidate Candidate, color bool) string {
	branch := candidate.Branch
	if branch == "" {
		branch = "detached"
	}
	rows := [][2]string{
		{"project", candidate.Project},
		{"worktree", candidate.Worktree},
		{"branch", branch},
		{"path", candidate.Path},
	}

	const (
		keyWidth = 8
		title    = "tachograph shell"
	)
	width := len(title)
	for _, row := range rows {
		if line := keyWidth + 2 + utf8.RuneCountInString(row[1]); line > width {
			width = line
		}
	}

	style := func(code, text string) string {
		if !color {
			return text
		}
		return code + text + ansiReset
	}
	border := func(left, right string) string {
		return style(ansiCyan, left+strings.Repeat("─", width+2)+right)
	}

	lines := make([]string, 0, len(rows)+3)
	lines = append(lines, border("╭", "╮"))
	lines = append(lines, "│ "+style(ansiBold+ansiCyan, title)+strings.Repeat(" ", width-len(title))+" │")
	lines = append(lines, "│ "+strings.Repeat(" ", width)+" │")
	for _, row := range rows {
		key := fmt.Sprintf("%-*s", keyWidth, row[0])
		padding := strings.Repeat(" ", width-keyWidth-2-utf8.RuneCountInString(row[1]))
		value := row[1]
		if color && row[0] == "worktree" {
			value = ansiBold + value + ansiReset
		}
		lines = append(lines, "│ "+style(ansiDim, key)+"  "+value+padding+" │")
	}
	lines = append(lines, border("╰", "╯"))
	return strings.Join(lines, "\n")
}

// colorEnabled reports whether the banner should use ANSI colors. It follows
// NO_COLOR and falls back to plain text when stderr is not a terminal.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiCyan  = "\x1b[36m"
)

// environment returns the process environment with the Tachograph markers set.
func environment(candidate Candidate) []string {
	markers := []struct {
		key   string
		value string
	}{
		{EnvProject, candidate.Project},
		{EnvWorktree, candidate.Path},
		{EnvWorktreeID, candidate.ID},
		{EnvBranch, candidate.Branch},
		{EnvMainWorktree, candidate.MainWorktree},
		{EnvHistoryRepo, candidate.HistoryRepo},
	}

	overrides := make(map[string]string, len(markers))
	for _, marker := range markers {
		if marker.value != "" {
			overrides[marker.key] = marker.value
		}
	}

	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replaced := overrides[key]; replaced {
				continue
			}
		}
		env = append(env, entry)
	}
	for _, marker := range markers {
		if value, ok := overrides[marker.key]; ok {
			env = append(env, marker.key+"="+value)
		}
	}
	return env
}

// shellExecutable resolves the shell from envKey, then the OS fallback.
func shellExecutable(envKey, fallback string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		path, err := exec.LookPath(value)
		if err != nil {
			return "", fmt.Errorf("%s %q: %w", envKey, value, err)
		}
		return path, nil
	}
	path, err := exec.LookPath(fallback)
	if err != nil {
		return "", fmt.Errorf("no shell found: %w", err)
	}
	return path, nil
}
