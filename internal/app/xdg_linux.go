//go:build linux

package app

import (
	"os"
	"path/filepath"
	"strings"
)

func xdgDataHome() string {
	if dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); filepath.IsAbs(dir) {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "tachograph-data")
	}
	return filepath.Join(home, ".local", "share")
}
