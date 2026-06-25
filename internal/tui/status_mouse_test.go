package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/igormaneschy/aurelia/internal/ipc"
)

func TestStatusBarHit_ModelLabel(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 24
	m.activeModel = "claude-sonnet"

	line := m.statusBarPlainLine()
	idx := indexOf(line, "claude-sonnet")
	if idx < 0 {
		t.Fatalf("model not found in status line %q", line)
	}

	mid := idx + len("claude-sonnet")/2
	if m.statusBarHit(mid) != statusBarHitModel {
		t.Fatalf("expected model hit at x=%d in %q", mid, line)
	}
	if m.statusBarHit(0) == statusBarHitModel {
		t.Fatal("expected state label at x=0 not to be model hit")
	}
}

func TestStatusBarY_StaysAtBottomWithSearchBar(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.activeModel = "claude-sonnet"

	baseY := m.statusBarY()
	m.historySearch.active = true
	m.historySearch.query = "test"
	withSearchY := m.statusBarY()
	if withSearchY != baseY || withSearchY != m.height-1 {
		t.Fatalf("expected status bar pinned to bottom: base=%d search=%d height=%d", baseY, withSearchY, m.height)
	}
}

func TestHeaderProjectHit_WhenProjectSet(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.cwdPath = "/Users/igor/myproject"

	y := m.chatHeaderY()
	x := m.mainPaneStartX() + 40
	if !m.headerProjectHit(x, y) {
		t.Fatal("expected project segment click in header")
	}
	if m.headerProjectHit(x, y+5) {
		t.Fatal("expected miss below header line")
	}
}

func TestHeaderProjectHit_ChatModeMiss(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.cwdPath = "not set"

	y := m.chatHeaderY()
	if m.headerProjectHit(m.mainPaneStartX()+40, y) {
		t.Fatal("expected no project hit in chat mode")
	}
}

func TestHandleSidebarMouse_ClickBelowTablePassesThrough(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "dm"}}
	prepSidebarMouseTest(&m)

	hintY := m.sidebarLineY("+ New session")
	if hintY < 0 {
		t.Fatal("expected + New session hint line")
	}

	handled, _, _ := m.handleSidebarMouse(tea.MouseClickMsg{X: 2, Y: hintY, Button: tea.MouseLeft})
	if handled {
		t.Fatal("expected sidebar handler to pass through click on hint row")
	}
}

func TestHandleChatMouse_NewSessionOpensForm(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "dm"}}
	prepSidebarMouseTest(&m)

	hintY := m.sidebarLineY("+ New session")
	if hintY < 0 {
		t.Fatal("expected + New session hint line")
	}

	handled, model, _ := m.handleChatMouse(tea.MouseClickMsg{X: 2, Y: hintY, Button: tea.MouseLeft})
	if !handled {
		t.Fatal("expected chat mouse handler to open new session form")
	}
	if !model.(Model).formOpen {
		t.Fatal("expected formOpen after clicking + New session")
	}
}

func TestHandleChatMouse_ModelInStatusBar(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.activeModel = "claude-sonnet"

	line := m.statusBarPlainLine()
	idx := indexOf(line, "claude-sonnet")
	if idx < 0 {
		t.Fatalf("model not in status line %q", line)
	}

	clickY := m.statusBarFooterStartY()
	for y := m.statusBarFooterStartY(); y < len(m.layoutLines()); y++ {
		if strings.Contains(stripANSIForTest(m.layoutLines()[y]), "claude-sonnet") {
			clickY = y
			break
		}
	}
	handled, model, _ := m.handleChatMouse(tea.MouseClickMsg{
		X: idx + 2, Y: clickY, Button: tea.MouseLeft,
	})
	if !handled {
		t.Fatal("expected model click to open form")
	}
	if !model.(Model).formOpen {
		t.Fatal("expected model picker form open")
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}