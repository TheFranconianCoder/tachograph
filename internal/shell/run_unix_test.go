//go:build !windows

package shell

import "testing"

func TestShellExecutableUsesSHELL(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	path, err := shellExecutable("SHELL", "sh")
	if err != nil {
		t.Fatalf("shellExecutable() error = %v", err)
	}
	if path != "/bin/sh" {
		t.Errorf("shellExecutable() = %q, want /bin/sh", path)
	}
}

func TestShellExecutableRejectsMissingSHELL(t *testing.T) {
	t.Setenv("SHELL", "/definitely/not/a/shell")
	if _, err := shellExecutable("SHELL", "sh"); err == nil {
		t.Fatal("shellExecutable() = nil error, want missing-shell error")
	}
}
