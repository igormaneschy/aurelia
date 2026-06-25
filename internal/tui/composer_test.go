package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestComposerHints_HiddenWhenTyping(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.textarea.SetValue("hello")
	if m.renderComposerHints() != "" {
		t.Fatal("expected no hints while typing")
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