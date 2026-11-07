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
	delta := filepath.Join(dir, "delta")
	if err := os.WriteFile(delta, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if tool := Resolve(ModeGit, binary); tool.Name != ModeGit || tool.Path != "" {
		t.Errorf("Resolve(git) = %+v, want git without a path", tool)
	}
	if tool := Resolve(ModeAuto, binary); !tool.Enabled() || tool.Name != ModeDifft || tool.Path != binary {
		t.Errorf("Resolve(auto, difft) = %+v, want the configured difftastic binary", tool)
	}
	if tool := Resolve(ModeAuto, delta); !tool.Enabled() || tool.Name != ModeDelta || tool.Path != delta {
		t.Errorf("Resolve(auto, delta) = %+v, want the configured delta binary", tool)
	}
	if tool := Resolve(ModeDifft, binary); !tool.Enabled() || tool.Path != binary {
		t.Errorf("Resolve(difft, configured) = %+v, want the configured binary", tool)
	}
	if tool := Resolve(ModeDelta, delta); !tool.Enabled() || tool.Path != delta {
		t.Errorf("Resolve(delta, configured) = %+v, want the configured binary", tool)
	}
	if tool := Resolve(ModeDifft, filepath.Join(dir, "missing")); tool.Name != ModeGit {
		t.Errorf("Resolve(difft, missing) = %+v, want the git fallback", tool)
	}
	if tool := Resolve(ModeDelta, filepath.Join(dir, "missing")); tool.Name != ModeGit {
		t.Errorf("Resolve(delta, missing) = %+v, want the git fallback", tool)
	}

	if tool := Resolve(ModeAuto, ""); tool.Enabled() {
		if _, err := os.Stat(tool.Path); err != nil {
			t.Errorf("Resolve(auto) returned a missing binary: %+v", tool)
		}
	} else if tool.Name != ModeGit {
		t.Errorf("Resolve(auto) = %+v, want the git fallback", tool)
	}
}

func TestResolveAutoPrefersDelta(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}

	dir := t.TempDir()
	for _, name := range []string{"delta", "difft"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if tool := Resolve(ModeAuto, ""); tool.Name != ModeDelta {
		t.Errorf("Resolve(auto) = %+v, want delta to win over difftastic", tool)
	}
}
