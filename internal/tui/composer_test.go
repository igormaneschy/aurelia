package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/igormaneschy/aurelia/internal/ipc"
)

func TestComposerPlaceholder_ChatMode(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.cwdPath = "not set"
	if got := m.composerPlaceholder(); !strings.Contains(got, "Chat mode") {
		t.Fatalf("got %q", got)
	}
}

func TestComposerPlaceholder_WithCWD(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.cwdPath = "/Users/igor/dev/aurelia"
	if got := m.composerPlaceholder(); !strings.Contains(got, "aurelia") {
		t.Fatalf("got %q", got)
	}
}

func TestComposerPlaceholder_Waiting(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.waiting = true
	if got := m.composerPlaceholder(); !strings.Contains(got, "pensar") {
		t.Fatalf("got %q", got)
	}
}

func TestComposerHints_WhenEmpty(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.textarea.Reset()
	if got := m.renderComposerHints(); !strings.Contains(got, "/help") || !strings.Contains(got, "F1") {
		t.Fatalf("got %q", got)
	}
}

func TestComposerHints_ShowsSendWhileTyping(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.textarea.SetValue("hello")
	got := m.renderComposerHints()
	if !strings.Contains(got, "send") {
		t.Fatalf("expected send hint while typing, got %q", got)
	}
	if strings.Contains(got, "/help") {
		t.Fatalf("expected only send hint while typing, got %q", got)
	}
}

func TestRenderInput_IncludesComposerSpacer(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 24
	rendered := stripANSIForTest(m.renderInput())
	if !strings.Contains(rendered, "···") && !strings.Contains(rendered, "·") {
		t.Fatalf("expected composer spacer dots, got:\n%s", rendered)
	}
}

func TestComposerPlaceholder_SyncedOnUpdate(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.cwdPath = "/tmp/foo"
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := updated.(Model).textarea.Placeholder
	if !strings.Contains(got, "foo") {
		t.Fatalf("placeholder = %q", got)
	}
}

func TestComposerTextareaWidthMatchesSidebarColumn(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 120
	m.showSidebar = true
	m.sessions = []tuiSessionInfo{{ChatID: -2, Name: "Trade"}}
	m.syncTextareaDimensions()

	taW := m.composerTextareaWidth()
	boxW := m.composerColumnWidth()
	inner := boxW - inputBoxChromeWidth
	if taW > inner-composerPromptRunes {
		t.Fatalf("textarea width %d exceeds inner box %d (composer=%d)", taW, inner, boxW)
	}
	if taW >= inputTextareaWidth(120) {
		t.Fatalf("sidebar textarea %d should be narrower than terminal-wide %d", taW, inputTextareaWidth(120))
	}
}

func TestRenderInput_LongTextWrapsWithinComposerColumn(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.showSidebar = true
	m.sessions = []tuiSessionInfo{{ChatID: -2, Name: "Trade"}}
	long := strings.Repeat("palavra ", 30)
	m.textarea.SetValue(long)
	m.syncTextareaDimensions()

	rendered := stripANSIForTest(m.renderInput())
	maxW := m.composerColumnWidth()
	for _, line := range strings.Split(rendered, "\n") {
		if !strings.Contains(line, "palavra") {
			continue
		}
		if w := lipgloss.Width(line); w > maxW {
			t.Fatalf("wrapped line too wide (%d > %d): %q", w, maxW, line)
		}
	}
	if strings.Count(rendered, "palavra") < 2 {
		t.Fatalf("expected wrapped text across multiple lines, got:\n%s", rendered)
	}
}

func TestComposerTextareaLineCount_GrowsWithWrappedText(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 120
	m.showSidebar = true
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "dm"}}
	m.textarea.SetValue(strings.Repeat("abcde ", 80))
	m.syncTextareaDimensions()

	if got := m.composerTextareaLineCount(); got <= composerTextareaMinHeight {
		t.Fatalf("expected textarea to grow beyond %d lines, got %d", composerTextareaMinHeight, got)
	}
	if got := m.composerTextareaLineCount(); got > composerTextareaMaxHeight {
		t.Fatalf("textarea lines %d exceed max %d", got, composerTextareaMaxHeight)
	}
}

func TestLayout_LongComposerShrinksViewportNotStatusBar(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.showSidebar = true
	m.sessions = []tuiSessionInfo{{ChatID: -2, Name: "Trade"}}
	m.ensureViewport()

	baseVP := m.viewportHeight()
	m.textarea.SetValue(strings.Repeat("palavra ", 40))
	m.syncTextareaDimensions()
	m.syncViewportDimensions()

	if m.viewportHeight() >= baseVP {
		t.Fatalf("expected smaller viewport with tall composer: base=%d tall=%d", baseVP, m.viewportHeight())
	}
	lines := strings.Count(m.renderChatBaseLayout(), "\n") + 1
	if lines != m.height {
		t.Fatalf("layout lines=%d want height=%d", lines, m.height)
	}
	if m.statusBarY() != m.height-1 {
		t.Fatalf("statusBarY=%d want %d", m.statusBarY(), m.height-1)
	}
}