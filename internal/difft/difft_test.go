package difft

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolve(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}

	dir := t.TempDir()
	binary := filepath.Join(dir, "difft")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if tool := Resolve(ModeGit, binary); tool.Name != ModeGit || tool.Path != "" {
		t.Errorf("Resolve(git) = %+v, want git without a path", tool)
	}
	if tool := Resolve(ModeAuto, binary); !tool.Enabled() || tool.Path != binary {
		t.Errorf("Resolve(auto, configured) = %+v, want the configured binary", tool)
	}
	if tool := Resolve(ModeDifft, binary); !tool.Enabled() || tool.Path != binary {
		t.Errorf("Resolve(difft, configured) = %+v, want the configured binary", tool)
	}
	if tool := Resolve(ModeDifft, filepath.Join(dir, "missing")); tool.Name != ModeGit {
		t.Errorf("Resolve(difft, missing) = %+v, want the git fallback", tool)
	}

	if tool := Resolve(ModeAuto, ""); tool.Enabled() {
		if _, err := os.Stat(tool.Path); err != nil {
			t.Errorf("Resolve(auto) returned a missing binary: %+v", tool)
		}
	} else if tool.Name != ModeGit {
		t.Errorf("Resolve(auto) = %+v, want the git fallback", tool)
	}
}
