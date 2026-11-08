//go:build windows

package shell

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
)

func runShell(candidate Candidate) error {
	executable, err := shellExecutable("COMSPEC", "cmd.exe")
	if err != nil {
		return err
	}

	command := exec.Command(executable)
	command.Dir = candidate.Path
	command.Env = environment(candidate)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	fmt.Fprintln(os.Stderr, Banner(candidate, colorEnabled()))

	// The terminal delivers Ctrl-C to the child as well; the CLI must not exit
	// before the shell does.
	signal.Ignore(os.Interrupt)
	err = command.Run()
	signal.Reset(os.Interrupt)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &ExitError{Code: exitErr.ExitCode()}
		}
		return fmt.Errorf("start shell: %w", err)
	}
	return nil
}
