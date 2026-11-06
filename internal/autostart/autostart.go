// Package autostart keeps the per-user login entry for the tray app. The entry
// starts Tachograph without opening the UI; the app is expected to live in the
// tray after login.
package autostart

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Options is the command started at login.
type Options struct {
	Executable string
	Args       []string
}

// Result describes the managed entry.
type Result struct {
	Path    string
	Enabled bool
}

// Sync creates, refreshes or removes the login entry. It is intentionally
// best-effort: an unsupported platform or a missing tool is an error for the
// caller to log, never a reason to stop the app.
func Sync(enabled bool, opts Options) (Result, error) {
	if strings.TrimSpace(opts.Executable) == "" {
		return Result{}, fmt.Errorf("autostart: executable is required")
	}
	switch runtime.GOOS {
	case "linux":
		return syncLinux(enabled, opts)
	case "darwin":
		return syncDarwin(enabled, opts)
	case "windows":
		return syncWindows(enabled, opts)
	default:
		return Result{}, fmt.Errorf("autostart is not supported on %s", runtime.GOOS)
	}
}

func syncLinux(enabled bool, opts Options) (Result, error) {
	path, err := linuxPath()
	if err != nil {
		return Result{}, err
	}
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return Result{Path: path}, err
		}
		return Result{Path: path}, nil
	}
	if err := writeFile(path, desktopEntry(opts)); err != nil {
		return Result{Path: path}, err
	}
	return Result{Path: path, Enabled: true}, nil
}

func linuxPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config directory: %w", err)
	}
	return filepath.Join(configDir, "autostart", "tachograph.desktop"), nil
}

func desktopEntry(opts Options) string {
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Tachograph",
		"Comment=Local history recorder for Git worktrees",
		"Exec=" + desktopExec(opts),
		"Terminal=false",
		"X-GNOME-Autostart-enabled=true",
		"",
	}, "\n")
}

func desktopExec(opts Options) string {
	parts := make([]string, 0, len(opts.Args)+1)
	parts = append(parts, quoteDesktop(opts.Executable))
	for _, arg := range opts.Args {
		parts = append(parts, quoteDesktop(arg))
	}
	return strings.Join(parts, " ")
}

func quoteDesktop(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\"'\\><~|&;$*?#()`") {
		return value
	}
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", "\\$")
	return `"` + replacer.Replace(value) + `"`
}

func syncDarwin(enabled bool, opts Options) (Result, error) {
	path, err := darwinPath()
	if err != nil {
		return Result{}, err
	}
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return Result{Path: path}, err
		}
		return Result{Path: path}, nil
	}
	if err := writeFile(path, launchAgent(opts)); err != nil {
		return Result{Path: path}, err
	}
	return Result{Path: path, Enabled: true}, nil
}

func darwinPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", "dev.tachograph.tray.plist"), nil
}

func launchAgent(opts Options) string {
	var builder strings.Builder
	builder.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	builder.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	builder.WriteString("<plist version=\"1.0\">\n<dict>\n")
	builder.WriteString("  <key>Label</key><string>dev.tachograph.tray</string>\n")
	builder.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, value := range append([]string{opts.Executable}, opts.Args...) {
		builder.WriteString("    <string>")
		_ = xml.EscapeText(&builder, []byte(value))
		builder.WriteString("</string>\n")
	}
	builder.WriteString("  </array>\n")
	builder.WriteString("  <key>RunAtLoad</key><true/>\n")
	builder.WriteString("  <key>KeepAlive</key><false/>\n")
	builder.WriteString("</dict>\n</plist>\n")
	return builder.String()
}

func syncWindows(enabled bool, opts Options) (Result, error) {
	const key = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	const name = "Tachograph"
	path := key + `\` + name

	if !enabled {
		command := exec.Command("reg", "delete", key, "/v", name, "/f")
		output, err := command.CombinedOutput()
		if err != nil {
			message := strings.ToLower(string(output))
			if strings.Contains(message, "unable to find") || strings.Contains(message, "not find") {
				return Result{Path: path}, nil
			}
			return Result{Path: path}, fmt.Errorf("remove autostart entry: %w (%s)", err, strings.TrimSpace(string(output)))
		}
		return Result{Path: path}, nil
	}

	command := exec.Command("reg", "add", key, "/v", name, "/t", "REG_SZ", "/d", windowsCommand(opts), "/f")
	if output, err := command.CombinedOutput(); err != nil {
		return Result{Path: path}, fmt.Errorf("write autostart entry: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return Result{Path: path, Enabled: true}, nil
}

func windowsCommand(opts Options) string {
	parts := make([]string, 0, len(opts.Args)+1)
	parts = append(parts, quoteWindows(opts.Executable))
	for _, arg := range opts.Args {
		parts = append(parts, quoteWindows(arg))
	}
	return strings.Join(parts, " ")
}

func quoteWindows(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\"") {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
