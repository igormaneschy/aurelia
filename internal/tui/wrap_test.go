package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func TestWrapPlainText_LongLineBreaks(t *testing.T) {
	text := strings.Repeat("palavra ", 40)
	wrapped := wrapPlainText(text, 60)
	lines := strings.Split(wrapped, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped lines, got %d: %q", len(lines), wrapped)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 60 {
			t.Fatalf("line exceeds width 60 (%d): %q", lipgloss.Width(line), line)
		}
	}
}

func TestWrapPlainText_PreservesExplicitNewlines(t *testing.T) {
	text := "linha curta\n" + strings.Repeat("texto ", 30)
	wrapped := wrapPlainText(text, 50)
	lines := strings.Split(wrapped, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected multiple lines after wrap, got %d: %q", len(lines), wrapped)
	}
	if !strings.HasPrefix(strings.TrimRight(lines[0], " "), "linha curta") {
		t.Fatalf("expected first line preserved, got %q", lines[0])
	}
}

func TestWrapPlainText_EmptyAndZeroWidth(t *testing.T) {
	if got := wrapPlainText("", 60); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	long := strings.Repeat("x", 100)
	if got := wrapPlainText(long, 0); got != long {
		t.Fatalf("expected unchanged text for width 0, got %q", got)
	}
}

func TestModel_UserMessageWrapsInViewport(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 60
	m.height = 40
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.messages = []chatMessage{
		{Sender: "Igor", Text: strings.Repeat("palavra ", 30), Timestamp: time.Now()},
	}
	m.updateViewport()

	content := stripANSIForTest(m.viewport.View())
	lines := strings.Split(content, "\n")
	wrapped := 0
	for _, line := range lines {
		if strings.Contains(line, "palavra") && lipgloss.Width(strings.TrimSpace(line)) > 0 {
			if lipgloss.Width(line) <= m.contentWidth() {
				wrapped++
			}
		}
	}
	if wrapped < 2 {
		t.Fatalf("expected wrapped user message across multiple lines, got:\n%s", content)
	}
}