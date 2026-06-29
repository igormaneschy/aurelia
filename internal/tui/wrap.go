package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// userMessageMaxWrapWidth caps user message line length for readable chat
// bubbles on wide terminals. Without this, a ~120-character prompt fits on one
// line when the terminal is wider than the text.
const userMessageMaxWrapWidth = 80

// messageBodyWidth is the wrap width for plain chat message bodies. It matches
// the glamour renderer width used for assistant markdown.
func messageBodyWidth(viewportWidth int) int {
	w := viewportWidth - 4
	if w < 40 {
		return 40
	}
	return w
}

// userMessageWrapWidth picks a wrap width that fits the chat column, matches
// the composer textarea, and stays readable on wide terminals.
func (m Model) userMessageWrapWidth(viewportWidth int) int {
	chatW := messageBodyWidth(viewportWidth)
	inputW := inputTextareaWidth(m.width)
	w := chatW
	if inputW < w {
		w = inputW
	}
	if w > userMessageMaxWrapWidth {
		w = userMessageMaxWrapWidth
	}
	return maxInt(20, w)
}

// materializeSoftWraps converts logical lines into explicit newlines at the
// given width, matching bubbles textarea soft-wrap. Used for displayText on
// submit so the transcript shows the same breaks the user saw while typing.
func materializeSoftWraps(text string, width int) string {
	if width < 1 || text == "" {
		return text
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		wrapped := ansi.Hardwrap(ansi.Wordwrap(line, width, ""), width, true)
		out = append(out, wrapped)
	}
	return strings.Join(out, "\n")
}

// wrapPlainText wraps long lines for terminal display while preserving
// explicit newlines. ANSI sequences are preserved when present.
func wrapPlainText(text string, width int) string {
	return materializeSoftWraps(text, width)
}