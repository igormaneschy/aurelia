package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/igormaneschy/aurelia/internal/ipc"
)

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

func TestHeaderModelHit_WhenModelSet(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.activeModel = "claude-sonnet"

	y := m.chatHeaderY()
	plain := stripANSIForTest(strings.Split(m.renderChatHeader(), "\n")[0])
	idx := strings.Index(plain, "claude-sonnet")
	if idx < 0 {
		t.Fatalf("model not in header %q", plain)
	}
	x := m.mainPaneStartX() + idx + 2
	if !m.headerModelHit(x, y) {
		t.Fatal("expected model chip click in header")
	}
	if m.headerModelHit(x, y+5) {
		t.Fatal("expected miss below header line")
	}
}

func TestHeaderModelHit_PlaceholderWhenEmpty(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30

	y := m.chatHeaderY()
	plain := stripANSIForTest(strings.Split(m.renderChatHeader(), "\n")[0])
	idx := strings.Index(plain, sidebarModelPlaceholder)
	if idx < 0 {
		t.Fatalf("placeholder not in header %q", plain)
	}
	x := m.mainPaneStartX() + idx
	if !m.headerModelHit(x, y) {
		t.Fatal("expected placeholder model chip click in header")
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

func TestHandleChatMouse_ProjectTitleOpensCwdForm(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "dm"}}
	m.cwdPath = "/Users/igor/dev/aurelia"
	prepSidebarMouseTest(&m)

	cwdY := m.sidebarLineY("📂")
	if cwdY < 0 {
		t.Fatal("expected cwd line in sidebar context panel")
	}

	handled, model, _ := m.handleChatMouse(tea.MouseClickMsg{X: 2, Y: cwdY, Button: tea.MouseLeft})
	if !handled {
		t.Fatal("expected click on Project title to open cwd form")
	}
	next := model.(Model)
	if !next.formOpen || next.activeForm == nil || next.activeForm.kind != formKindCwd {
		t.Fatalf("expected cwd form, got %#v", next.activeForm)
	}
	if next.activeForm.selected != m.cwdPath {
		t.Fatalf("selected = %q, want %q", next.activeForm.selected, m.cwdPath)
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

func TestStatusBarHit_HelpLabel(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 24

	line := m.statusBarPlainLine()
	idx := indexOf(line, statusBarHelpToken)
	if idx < 0 {
		t.Fatalf("help label not found in status line %q", line)
	}

	mid := idx + len(statusBarHelpToken)/2
	if m.statusBarHit(mid) != statusBarHitHelp {
		t.Fatalf("expected help hit at x=%d in %q", mid, line)
	}
}

func TestHandleChatMouse_HelpInStatusBar(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30

	line := m.statusBarPlainLine()
	idx := indexOf(line, statusBarHelpToken)
	if idx < 0 {
		t.Fatalf("help label not in status line %q", line)
	}

	clickY := m.statusBarFooterStartY()
	for y := m.statusBarFooterStartY(); y < len(m.layoutLines()); y++ {
		if strings.Contains(stripANSIForTest(m.layoutLines()[y]), statusBarHelpToken) {
			clickY = y
			break
		}
	}
	handled, model, _ := m.handleChatMouse(tea.MouseClickMsg{
		X: idx + 2, Y: clickY, Button: tea.MouseLeft,
	})
	if !handled {
		t.Fatal("expected help click to open overlay")
	}
	if !model.(Model).helpVisible() {
		t.Fatal("expected help overlay open")
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