package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// messageBodyWidth is the wrap width for plain chat message bodies. It matches
// the glamour renderer width used for assistant markdown.
func messageBodyWidth(viewportWidth int) int {
	w := viewportWidth - 4
	if w < 40 {
		return 40
	}
	return w
}

// wrapPlainText wraps long lines for terminal display while preserving
// explicit newlines. ANSI sequences are preserved when present.
func wrapPlainText(text string, width int) string {
	if width < 1 || text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	wrapped := make([]string, len(lines))
	style := lipgloss.NewStyle().Width(width)
	for i, line := range lines {
		wrapped[i] = style.Render(line)
	}
	return strings.Join(wrapped, "\n")
}