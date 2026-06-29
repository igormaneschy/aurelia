package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
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

func TestMaterializeSoftWraps_RealisticPrompt(t *testing.T) {
	text := "ok , em relação ao ponto das memorias existentes, o que vc está propondo exatamente ? minha ideia seria mesmo unificar, ou seja a tui e o telegram"
	wrapped := materializeSoftWraps(text, 80)
	lines := strings.Split(wrapped, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected readable wrap at 80 cols, got %d lines: %q", len(lines), wrapped)
	}
}

func TestUserMessageWrapWidth_CapsWideTerminal(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 200
	got := m.userMessageWrapWidth(196)
	if got != userMessageMaxWrapWidth {
		t.Fatalf("userMessageWrapWidth(196) at width 200 = %d, want %d", got, userMessageMaxWrapWidth)
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

func TestModel_SubmitMaterializesUserMessageWrap(t *testing.T) {
	longPrompt := strings.Repeat("palavra ", 25)
	ta := textarea.New()
	ta.SetValue(longPrompt)
	m := testChatModelWithTextarea(ta)
	m.width = 160
	m.messages = []chatMessage{}

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("expected submit command")
	}
	m2 := updated.(Model)
	if len(m2.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m2.messages))
	}
	if strings.Count(m2.messages[0].Text, "\n") < 1 {
		t.Fatalf("expected materialized wraps in stored message, got %q", m2.messages[0].Text)
	}
}

func TestModel_UserMessageWrapsInViewport(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 160
	m.height = 40
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	longPrompt := "ok , em relação ao ponto das memorias existentes, o que vc está propondo exatamente ? minha ideia seria mesmo unificar, ou seja a tui e o telegram"
	m.messages = []chatMessage{
		{Sender: "Igor", Text: longPrompt, Timestamp: time.Now()},
	}
	m.updateViewport()

	content := stripANSIForTest(m.viewport.View())
	palavraLines := 0
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "memorias") || strings.Contains(trimmed, "unificar") {
			palavraLines++
			if lipgloss.Width(trimmed) > userMessageMaxWrapWidth+5 {
				t.Fatalf("user line too wide (%d): %q", lipgloss.Width(trimmed), trimmed)
			}
		}
	}
	if palavraLines < 2 {
		t.Fatalf("expected wrapped user message across multiple lines, got:\n%s", content)
	}
}