package history

import (
	"context"
	"strings"
)

type diffThemeKey struct{}

// WithDiffTheme marks a request's ANSI diff theme. The server renders the diff
// before the browser paints it, so the UI has to pass its effective theme.
func WithDiffTheme(ctx context.Context, theme string) context.Context {
	return context.WithValue(ctx, diffThemeKey{}, normalizeDiffTheme(theme))
}

func diffTheme(ctx context.Context) string {
	if value, ok := ctx.Value(diffThemeKey{}).(string); ok && value != "" {
		return value
	}
	return "dark"
}

func normalizeDiffTheme(theme string) string {
	if strings.EqualFold(strings.TrimSpace(theme), "light") {
		return "light"
	}
	return "dark"
}
