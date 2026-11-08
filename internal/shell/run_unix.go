//go:build !windows

package shell

import (
	"fmt"
	"os"
	"syscall"
)

func runShell(candidate Candidate) error {
	executable, err := shellExecutable("SHELL", "sh")
	if err != nil {
		return err
	}
	if err := os.Chdir(candidate.Path); err != nil {
		return fmt.Errorf("change to worktree %s: %w", candidate.Path, err)
	}
	fmt.Fprintln(os.Stderr, Banner(candidate, colorEnabled()))
	return syscall.Exec(executable, []string{executable}, environment(candidate))
}
