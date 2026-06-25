package tui

import (
	"strings"
	"testing"
	tea "charm.land/bubbletea/v2"
	"github.com/igormaneschy/aurelia/internal/ipc"
)

func prepSidebarMouseTest(m *Model) {
	m.state = stateChat
	m.width = 100
	m.height = 30
	m.showSidebar = true
	m.mouseEnabled = true
	m.resizeSidebarTable()
	prepSidebarTest(m)
}

func TestSidebarTableFirstRowY(t *testing.T) {
	want := topMarginHeight + sidebarBorderLines + sidebarTitleLines + sidebarTableHeaderLines
	if got := sidebarTableFirstRowY(); got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}

func TestSidebarRowAt_MapsVisibleRows(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "dm"}, {ChatID: -9000002, Name: "work"}, {ChatID: -9000003, Name: "research"}}
	prepSidebarMouseTest(&m)
	firstRowY := sidebarTableFirstRowY()
	for y, want := range map[int]int{firstRowY - 1: -1, firstRowY: 0, firstRowY + 1: 1, firstRowY + 2: 2, firstRowY + m.sidebarTable.Height(): -1} {
		if got := m.sidebarRowAt(y); got != want {
			t.Errorf("y=%d got %d want %d", y, got, want)
		}
	}
}

func TestSidebarMouseHitX(t *testing.T) {
	if !sidebarMouseHitX(0) || sidebarMouseHitX(sidebarWidth) {
		t.Fatal("unexpected hit results")
	}
}

func TestHandleSidebarMouse_ClickOpensSession(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.activeSession = ipc.ReservedTUIChatID
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "dm"}, {ChatID: -9000002, Name: "work"}}
	prepSidebarMouseTest(&m)
	updated, cmd := m.Update(tea.MouseClickMsg{X: 2, Y: sidebarTableFirstRowY() + 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	if updated.(Model).sidebarCursor != 1 {
		t.Fatal("expected cursor 1")
	}
}

func TestHandleSidebarMouse_MotionSetsHoverRow(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "dm"}, {ChatID: -9000002, Name: "work"}}
	prepSidebarMouseTest(&m)
	updated, _ := m.Update(tea.MouseMotionMsg{X: 2, Y: sidebarTableFirstRowY() + 1})
	if updated.(Model).sidebarHoverRow != 1 {
		t.Fatal("expected hover row 1")
	}
	if !strings.Contains(sidebarViewForTest(updated.(Model)), "work") {
		t.Fatal("expected work in view")
	}
}
