package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestUIContext_Sidebar(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	if m.uiContext() != uiContextSidebar {
		t.Fatalf("expected sidebar context, got %v", m.uiContext())
	}
	if m.helpPanelTitle() != "Sidebar Shortcuts" {
		t.Fatal("expected sidebar help title")
	}
}

func TestUIContext_Search(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.historySearch.active = true
	if m.uiContext() != uiContextSearch {
		t.Fatal("expected search context")
	}
}

func TestCtrlN_OpensNewSessionForm(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	updated, _ := m.handleKeyMsg(keyCtrl('n'))
	m2 := updated.(Model)
	if !m2.formOpen {
		t.Fatal("expected new session form after ctrl+n")
	}
}

func TestToggleMouse_NoMouseFlagBlocks(t *testing.T) {
	m := NewModelWithOptions("/tmp/test.sock", ThemeDark, ModelOptions{NoMouse: true})
	updated, _ := m.toggleMouseCapture()
	m2 := updated.(Model)
	if m2.mouseEnabled {
		t.Fatal("expected mouse to stay disabled with --no-mouse")
	}
}

func TestHelpPanel_SearchContextIncludesNextMatch(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 30
	m.historySearch.active = true
	panel := m.renderHelpPanel()
	plain := stripANSIForTest(panel)
	if !strings.Contains(plain, "Next match") {
		t.Fatalf("expected search help in panel, got %q", plain)
	}
}

func TestDefaultKeyMap_CtrlN(t *testing.T) {
	km := defaultKeyMap()
	if !key.Matches(keyCtrl('n'), km.NewSession) {
		t.Fatal("expected ctrl+n to match NewSession binding")
	}
}

func TestHelpPanel_SidebarContext(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 30
	m.sidebarFocused = true
	panel := m.renderHelpPanel()
	plain := stripANSIForTest(panel)
	if !strings.Contains(plain, "Rename session") {
		t.Fatalf("expected sidebar help, got %q", plain)
	}
}