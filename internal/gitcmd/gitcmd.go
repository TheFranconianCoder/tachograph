// Package gitcmd classifies git subcommands for diagnostic logging.
package gitcmd

// ShouldLog reports whether a git invocation is worth a debug line: a durable
// write, not index-only staging. `git add` runs on every poll even when nothing
// changed, so it stays silent; the commit is the effective write.
func ShouldLog(args []string) bool {
	if len(args) == 0 || args[0] == "add" {
		return false
	}
	return IsWrite(args)
}

// IsWrite reports whether the git invocation changes the repository, index,
// refs or configuration.
func IsWrite(args []string) bool {
	if len(args) == 0 {
		return false
	}

	switch args[0] {
	case "worktree":
		if len(args) < 2 {
			return false
		}
		switch args[1] {
		case "add", "remove", "move", "prune", "repair":
			return true
		default:
			return false
		}
	case "config":
		for _, arg := range args[1:] {
			switch arg {
			case "--get", "--get-all", "--get-regexp", "--get-urlmatch", "--list", "-l":
				return false
			}
		}
		return true
	case "branch":
		for _, arg := range args[1:] {
			switch arg {
			case "--list", "-l", "--show-current":
				return false
			}
		}
		return true
	case "stash":
		if len(args) > 1 && (args[1] == "list" || args[1] == "show") {
			return false
		}
		return true
	case "remote":
		if len(args) > 1 && (args[1] == "-v" || args[1] == "show" || args[1] == "get-url") {
			return false
		}
		return true
	case "tag":
		for _, arg := range args[1:] {
			if arg == "--list" || arg == "-l" || arg == "-n" {
				return false
			}
		}
		return true
	}

	switch args[0] {
	case "add", "commit", "commit-tree", "init", "update-ref", "reset", "checkout", "switch",
		"restore", "clean", "rm", "mv", "merge", "rebase", "cherry-pick", "revert",
		"gc", "repack", "prune", "reflog", "fetch", "push", "pull", "clone":
		return true
	default:
		return false
	}
}
