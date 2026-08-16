package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/igormaneschy/aurelia/internal/ipc"
	"strings"
	"testing"
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
	want := topMarginHeight + sidebarBorderLines + sidebarTitleLines + sidebarSectionRuleLines + sidebarSectionHeader + sidebarTableHeaderLines
	if got := sidebarTableFirstRowY(); got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}

func TestHandleSidebarMouse_ClickDMRow(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.activeSession = -9000002
	m.sessions = []tuiSessionInfo{
		{ChatID: ipc.ReservedTUIChatID, Name: "dm"},
		{ChatID: -9000002, Name: "Trade"},
	}
	prepSidebarMouseTest(&m)

	updated, cmd := m.Update(tea.MouseClickMsg{X: 2, Y: sidebarTableFirstRowY(), Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected cmd to open DM session")
	}
	if updated.(Model).sidebarCursor != 0 {
		t.Fatalf("expected cursor on DM row 0, got %d", updated.(Model).sidebarCursor)
	}
}

func TestRenderSidebarPanels_IncludesSections(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "dm"}, {ChatID: -2, Name: "work"}}
	m.width = 100
	m.height = 30
	m.showSidebar = true
	m.cwdPath = "not set"
	prepSidebarTest(&m)

	view := stripANSIForTest(m.renderSidebarTable())
	for _, want := range []string{"Sessions", "Context", "Actions", "+ New session", "📂", "🤖"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected sidebar to contain %q, got:\n%s", want, view)
		}
	}
	if strings.Contains(view, "(click)") {
		t.Fatal("expected no (click) hint in sidebar")
	}
}

func TestSessionModelLabel_NeverEmpty(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.sessions = []tuiSessionInfo{{ChatID: -2, Name: "work"}}
	m.activeSession = ipc.ReservedTUIChatID
	label := stripANSIForTest(m.sessionModelLabel(m.sessions[0]))
	if label != sidebarModelPlaceholder {
		t.Fatalf("expected %q, got %q", sidebarModelPlaceholder, label)
	}
	m.activeModel = "gpt-5.1"
	m.activeSession = -2
	label = stripANSIForTest(m.sessionModelLabel(m.sessions[0]))
	if label == "" || label == sidebarModelPlaceholder {
		t.Fatalf("expected active model label, got %q", label)
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

func TestRenderSidebarFramed_PreservesBottomBorder(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.showSidebar = true
	m.sessions = []tuiSessionInfo{
		{ChatID: ipc.ReservedTUIChatID, Name: "dm"},
		{ChatID: -2, Name: "Trade"},
		{ChatID: -3, Name: "ChatGeral"},
	}
	m.sessionUnread = map[int64]int{-2: 99}
	prepSidebarTest(&m)

	height := m.chatBodyHeight()
	framed := m.renderSidebarFramed(height)
	lines := strings.Split(framed, "\n")
	if len(lines) < height-1 || len(lines) > height+1 {
		t.Fatalf("framed sidebar lines=%d want ~%d", len(lines), height)
	}
	last := lines[len(lines)-1]
	for i := len(lines) - 1; i >= 0 && strings.TrimSpace(last) == ""; i-- {
		last = lines[i]
	}
	if !strings.ContainsAny(last, "╯┘") {
		t.Fatalf("expected bottom border on last line, got %q", last)
	}
}

func TestSyncSidebarRows_LongNameWithUnreadBadge(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.activeSession = ipc.ReservedTUIChatID
	m.sessions = []tuiSessionInfo{
		{ChatID: ipc.ReservedTUIChatID, Name: "dm"},
		{ChatID: -2, Name: "Trade"},
	}
	m.sessionUnread = map[int64]int{-2: 6}
	prepSidebarTest(&m)

	view := stripANSIForTest(m.sidebarTable.View())
	if !strings.Contains(view, "Trade") {
		t.Fatalf("expected session name, got:\n%s", view)
	}
	if !strings.Contains(view, "6") {
		t.Fatalf("expected unread badge, got:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Trade") && strings.Contains(line, "6") {
			// Name and badge may share a row but must not be concatenated.
			if strings.Contains(line, "Trade 6") || strings.Contains(line, "Trade6") {
				t.Fatalf("name and badge should not be concatenated: %q", line)
			}
		}
	}
}

func TestHandleSidebarMouse_MotionSetsHoverRow(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "dm"}, {ChatID: -9000002, Name: "work"}}
	prepSidebarMouseTest(&m)
	base := sidebarViewForTest(m)
	updated, _ := m.Update(tea.MouseMotionMsg{X: 2, Y: sidebarTableFirstRowY() + 1})
	if updated.(Model).sidebarHoverRow != 1 {
		t.Fatal("expected hover row 1")
	}
	hovered := sidebarViewForTest(updated.(Model))
	if hovered == base {
		t.Fatal("expected hover to change sidebar render")
	}
	if !strings.Contains(hovered, "work") {
		t.Fatal("expected work in view")
	}
}
