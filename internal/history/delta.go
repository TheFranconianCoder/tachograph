package history

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runDelta pipes a unified diff through delta and returns its ANSI output. The
// render is deterministic: the user's gitconfig is ignored, paging is off and
// the theme comes from the UI request.
func runDelta(ctx context.Context, binary, input, theme string) (string, error) {
	command := exec.CommandContext(ctx, binary, deltaArgs(theme)...)
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("delta: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// deltaArgs keeps delta's colors close to the UI palette: soft change
// backgrounds, themed syntax highlighting, muted chrome and app accent colors
// for the file header.
func deltaArgs(theme string) []string {
	args := []string{
		"--paging=never",
		"--line-numbers",
		"--no-gitconfig",
		"--width=160",
		"--true-color=always",
	}
	if normalizeDiffTheme(theme) == "light" {
		return append(args,
			"--light",
			"--minus-style", `syntax "#fbecea"`,
			"--plus-style", `syntax "#e8f3ec"`,
			"--minus-emph-style", `syntax bold "#f5d3cf"`,
			"--plus-emph-style", `syntax bold "#cfe9d9"`,
			"--file-style", `bold "#c2410c"`,
			"--file-decoration-style", `"#d4ccbe" box`,
			"--hunk-header-style", `"#746f66"`,
			"--hunk-header-decoration-style", `"#d4ccbe" box`,
			"--line-numbers-left-style", `"#a29c91"`,
			"--line-numbers-right-style", `"#a29c91"`,
			"--line-numbers-minus-style", `"#b42318"`,
			"--line-numbers-plus-style", `"#2f7d4f"`,
			"--line-numbers-zero-style", `"#a29c91"`,
		)
	}
	return append(args,
		"--dark",
		"--minus-style", `syntax "#2a1a1e"`,
		"--plus-style", `syntax "#132820"`,
		"--minus-emph-style", `syntax bold "#5a2529"`,
		"--plus-emph-style", `syntax bold "#1f4a32"`,
		"--file-style", `bold "#fb923c"`,
		"--file-decoration-style", `"#303a46" box`,
		"--hunk-header-style", `"#96a0ab"`,
		"--hunk-header-decoration-style", `"#303a46" box`,
		"--line-numbers-left-style", `"#66717d"`,
		"--line-numbers-right-style", `"#66717d"`,
		"--line-numbers-minus-style", `"#f87171"`,
		"--line-numbers-plus-style", `"#4ade80"`,
		"--line-numbers-zero-style", `"#66717d"`,
	)
}
