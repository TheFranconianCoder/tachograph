package app

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenURL is the plain OS URL-handler fallback. Prefer OpenUI, which starts the
// installed PWA's own launcher when one exists.
func OpenURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s: %w", url, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
