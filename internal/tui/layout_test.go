package tui

import (
	"strings"
	"testing"
)

func TestLayout_StatusBarStaysOnScreenWithSearch(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.activeModel = "claude-sonnet"
	m.ensureViewport()

	baseLines := strings.Count(m.renderChatBaseLayout(), "\n") + 1
	m.historySearch.active = true
	m.historySearch.query = "demo"
	m.syncViewportDimensions()
	searchLines := strings.Count(m.renderChatBaseLayout(), "\n") + 1

	if baseLines != m.height {
		t.Fatalf("base layout lines=%d want terminal height=%d", baseLines, m.height)
	}
	if searchLines != m.height {
		t.Fatalf("search layout lines=%d want terminal height=%d", searchLines, m.height)
	}
	if m.statusBarY() != m.height-1 {
		t.Fatalf("statusBarY=%d want %d", m.statusBarY(), m.height-1)
	}
}

func TestLayout_SearchShrinksViewportNotFooter(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.ensureViewport()

	baseVP := m.viewportHeight()
	m.historySearch.active = true
	m.syncViewportDimensions()
	searchVP := m.viewportHeight()

	if searchVP >= baseVP {
		t.Fatalf("expected smaller viewport with search bar: base=%d search=%d", baseVP, searchVP)
	}
}