// Package version exposes build metadata for the binary.
package version

import "runtime/debug"

// These are overridden at build time via -ldflags "-X ...".
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns the short version, falling back to the module version when built
// with `go install`.
func String() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
}

// Full returns version, commit and date in one line for the status report.
func Full() string {
	return String() + " (" + Commit + ", " + Date + ")"
}
