// Command tachograph is the desktop application and its CLI. Launching it (or
// `app`) starts the tray and the local service; the other verbs are clients of
// the running instance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/TheFranconianCoder/tachograph/internal/app"
	"github.com/TheFranconianCoder/tachograph/internal/history"
	"github.com/TheFranconianCoder/tachograph/internal/shell"
	"github.com/TheFranconianCoder/tachograph/internal/tui"
	"github.com/TheFranconianCoder/tachograph/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		var exit *shell.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.Code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return withConfigPath(func(path string) error { return cmdApp(nil, path) })
	}

	switch args[0] {
	case "app", "serve":
		return withConfigPath(func(path string) error { return cmdApp(args[1:], path) })
	case "open":
		return withConfigPath(func(path string) error { return cmdOpen(args[1:], path) })
	case "status":
		return withConfigPath(func(path string) error { return cmdStatus(args[1:], path) })
	case "history":
		return withConfigPath(func(path string) error { return cmdHistory(args[1:], path) })
	case "shell":
		return withConfigPath(func(path string) error { return cmdShell(args[1:], path) })
	case "version", "-version", "--version":
		fmt.Println(version.Full())
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func withConfigPath(run func(path string) error) error {
	path, err := app.ConfigPath()
	if err != nil {
		return err
	}
	return run(path)
}

func usage() {
	fmt.Fprint(os.Stderr, `tachograph — the local history recorder for every worktree

Usage:
  tachograph [app] [--headless] [--no-open] [--debug] [--config PATH] [--port N]   start the desktop app
  tachograph open [--config PATH] [--port N]                                    open the running UI
  tachograph status [--config PATH] [--port N]                                  print the status report
  tachograph shell [--config PATH] [--port N] [WORKTREE]                        open a shell in a related worktree
  tachograph history init [--debug] [--data-dir DIR] [PATH]                       create/reuse a worktree's history store
  tachograph history snapshot [--debug] [--data-dir DIR] [PATH]                   snapshot a worktree
  tachograph version                                                            print the version

Run shell from the root worktree. It offers the linked worktrees of that project
and exits with the root path when started from a linked worktree. WORKTREE
matches a path (including a subdirectory), project/worktree, a directory name or
a branch; without it, an interactive picker opens. The shell exports
TACHOGRAPH_* markers and prints a short context block before it starts.

The app listens on 127.0.0.1 only. The default address is http://127.0.0.1:9211/.
`)
}

func historyUsage() {
	fmt.Fprint(os.Stderr, `tachograph history — the recorder core

Usage:
  tachograph history init [--debug] [--data-dir DIR] [PATH]       create/reuse the history store for a worktree
  tachograph history snapshot [--debug] [--data-dir DIR] [PATH]    snapshot the worktree

PATH defaults to the current directory. The history root comes from history.root
in the config, unless --data-dir is set; otherwise it falls back to
$XDG_DATA_HOME/tachograph/history (or ~/.local/share/tachograph/history).
`)
}

func cmdApp(args []string, defaultConfig string) error {
	flags := flag.NewFlagSet("app", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfig, "config file")
	headless := flags.Bool("headless", false, "run without a tray icon")
	noOpen := flags.Bool("no-open", false, "do not open the UI")
	debug := flags.Bool("debug", false, "log writing git commands and verbose diagnostics")
	port := flags.Int("port", 0, "override the configured port")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, err := app.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if *port != 0 {
		cfg.Server.Port = *port
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	instance, err := app.New(cfg, *configPath, app.Options{Headless: *headless, NoOpen: *noOpen}, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return instance.Run(ctx)
}

func cmdOpen(args []string, defaultConfig string) error {
	cfg, err := loadConfig(args, defaultConfig)
	if err != nil {
		return err
	}
	return app.OpenUI(fmt.Sprintf("http://127.0.0.1:%d/", cfg.Server.Port))
}

func cmdStatus(args []string, defaultConfig string) error {
	cfg, err := loadConfig(args, defaultConfig)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/status", cfg.Server.Port))
	if err != nil {
		return fmt.Errorf("tachograph is not running: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read status response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("status returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		fmt.Println(string(body))
		return nil
	}
	pretty, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Println(string(body))
		return nil
	}
	fmt.Println(string(pretty))
	return nil
}

func loadConfig(args []string, defaultConfig string) (app.Config, error) {
	flags := flag.NewFlagSet("config", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfig, "config file")
	port := flags.Int("port", 0, "override the configured port")
	if err := flags.Parse(args); err != nil {
		return app.Config{}, err
	}

	cfg, err := app.LoadConfig(*configPath)
	if err != nil {
		return app.Config{}, err
	}
	if *port != 0 {
		cfg.Server.Port = *port
	}
	return cfg, nil
}

func cmdHistory(args []string, configPath string) error {
	if len(args) == 0 {
		historyUsage()
		return fmt.Errorf("history requires a subcommand")
	}

	subcommand := args[0]
	switch subcommand {
	case "init", "snapshot":
		return cmdHistoryCommand(subcommand, args[1:], configPath)
	case "help", "-h", "--help":
		historyUsage()
		return nil
	default:
		historyUsage()
		return fmt.Errorf("unknown history command %q", subcommand)
	}
}

func cmdHistoryCommand(subcommand string, args []string, configPath string) error {
	flags := flag.NewFlagSet("history "+subcommand, flag.ContinueOnError)
	dataDir := flags.String("data-dir", "", "history root (default: from config or XDG data directory)")
	debug := flags.Bool("debug", false, "log writing git commands")
	if err := flags.Parse(args); err != nil {
		return err
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	path := flags.Arg(0)
	if path == "" {
		path = "."
	}

	root := *dataDir
	if root == "" {
		cfg, err := app.LoadConfig(configPath)
		if err != nil {
			return err
		}
		root = cfg.History.Root
	}
	if root == "" {
		var err error
		root, err = history.DefaultRoot()
		if err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := history.New(root)
	recorder, err := store.Ensure(ctx, path)
	if err != nil {
		return err
	}

	switch subcommand {
	case "init":
		fmt.Printf("project:     %s (%s)\n", recorder.Project.Name, recorder.Project.ID)
		fmt.Printf("worktree:    %s (%s)\n", recorder.Worktree.Path, recorder.Worktree.ID)
		fmt.Printf("branch:      %s\n", recorder.Worktree.HistoryBranch)
		fmt.Printf("history:     %s\n", recorder.HistoryRepo)
		fmt.Printf("placeholder: %s\n", recorder.Placeholder)
		return nil
	case "snapshot":
		snapshot, err := recorder.Snapshot(ctx)
		if errors.Is(err, history.ErrNoChanges) {
			fmt.Println("no changes")
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Printf("%s %s\n", snapshot.Hash, snapshot.Message)
		return nil
	default:
		return fmt.Errorf("unknown history command %q", subcommand)
	}
}

func cmdShell(args []string, defaultConfig string) error {
	flags := flag.NewFlagSet("shell", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfig, "config file")
	port := flags.Int("port", 0, "override the configured port")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, err := app.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if *port != 0 {
		cfg.Server.Port = *port
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := shell.Fetch(ctx, cfg.Server.Port)
	if err != nil {
		return err
	}
	candidates := shell.Candidates(status)
	if len(candidates) == 0 {
		return fmt.Errorf("no worktrees are registered; add a root and run a scan")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve current directory: %w", err)
	}
	current, ok := shell.Current(candidates, cwd)
	if !ok {
		return fmt.Errorf("%s is not inside a managed worktree; add a root that contains it in Tachograph", cwd)
	}
	if !current.Main {
		if current.MainWorktree == "" {
			return fmt.Errorf("%s is a linked worktree and the project has no known root worktree", cwd)
		}
		if filepath.Clean(cwd) == filepath.Clean(current.Path) {
			return fmt.Errorf("%s is a linked worktree; run tachograph shell from the root worktree %s",
				cwd, current.MainWorktree)
		}
		return fmt.Errorf("%s is inside the linked worktree %s; run tachograph shell from the root worktree %s",
			cwd, current.Path, current.MainWorktree)
	}
	children := shell.Children(candidates, current)
	if len(children) == 0 {
		return fmt.Errorf("project %q has no child worktrees; create one with git worktree add", current.Project)
	}

	query := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if query == "" {
		if !interactive() {
			return fmt.Errorf("child worktree is required when stdin is not a terminal; try one of:\n%s", candidateList(children))
		}
		chosen, ok, err := tui.Pick(children, "")
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		return shell.Run(chosen)
	}

	matches := shell.Match(children, query)
	switch len(matches) {
	case 0:
		return fmt.Errorf("no child worktree of project %q matches %q; try one of:\n%s", current.Project, query, candidateList(children))
	case 1:
		return shell.Run(matches[0])
	}

	if !interactive() {
		return fmt.Errorf("%q is ambiguous; try one of:\n%s", query, candidateList(matches))
	}
	chosen, ok, err := tui.Pick(children, query)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return shell.Run(chosen)
}

// interactive reports whether the picker can take over the terminal.
func interactive() bool {
	for _, file := range []*os.File{os.Stdin, os.Stdout} {
		info, err := file.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}

func candidateList(candidates []shell.Candidate) string {
	lines := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		lines = append(lines, fmt.Sprintf("  %s  (%s)", candidate.Label(), candidate.Path))
	}
	return strings.Join(lines, "\n")
}
