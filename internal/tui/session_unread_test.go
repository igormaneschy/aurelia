package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/igormaneschy/aurelia/internal/ipc"
)

func TestSessionUnread_BadgeInactiveOnly(t *testing.T) {
	m := testChatModel()
	m.activeSession = ipc.ReservedTUIChatID
	m.sessionUnread = map[int64]int{-2: 3}

	if got := m.sessionUnreadBadge(ipc.ReservedTUIChatID); got != "" {
		t.Fatalf("active session badge = %q, want empty", got)
	}
	if got := m.sessionUnreadBadge(-2); got != "3" {
		t.Fatalf("inactive badge = %q, want 3", got)
	}
}

func TestSessionUnread_MarkSeenClearsBadge(t *testing.T) {
	m := testChatModel()
	m.sessionUnread = map[int64]int{-2: 5}
	m.sessionSeenCount = map[int64]int{-2: 2}

	(&m).markSessionSeen(-2, 7)

	if m.sessionUnread[-2] != 0 {
		t.Fatalf("unread = %d, want 0", m.sessionUnread[-2])
	}
	if m.sessionSeenCount[-2] != 7 {
		t.Fatalf("seen = %d, want 7", m.sessionSeenCount[-2])
	}
}

func TestSessionUnread_UpdateIncrements(t *testing.T) {
	m := testChatModel()
	m.activeSession = ipc.ReservedTUIChatID
	m.sessionSeenCount = map[int64]int{-2: 4}

	(&m).updateSessionUnread(-2, 7)

	if m.sessionUnread[-2] != 3 {
		t.Fatalf("unread = %d, want 3", m.sessionUnread[-2])
	}
}

func TestSessionUnread_CapsAt99Plus(t *testing.T) {
	m := testChatModel()
	m.activeSession = ipc.ReservedTUIChatID
	m.sessionUnread = map[int64]int{-2: 120}

	if got := m.sessionUnreadBadge(-2); got != "99+" {
		t.Fatalf("badge = %q, want 99+", got)
	}
}

func TestApplySessionsUnread_UpdatesSidebar(t *testing.T) {
	m := testChatModel()
	m.activeSession = ipc.ReservedTUIChatID
	m.sessions = []tuiSessionInfo{
		{ChatID: ipc.ReservedTUIChatID, Name: "dm", MessageCount: 2},
		{ChatID: -2, Name: "work", MessageCount: 5},
	}
	m.sessionSeenCount = map[int64]int{
		ipc.ReservedTUIChatID: 2,
		-2:                    2,
	}

	m.applySessionsUnread(m.sessions)
	m.syncSidebarRows()

	view := stripANSIForTest(m.sidebarTable.View())
	if !strings.Contains(view, "3") {
		t.Fatalf("sidebar should show unread badge, got:\n%s", view)
	}
	if strings.Contains(view, "work 3") || strings.Contains(view, "work3") {
		t.Fatalf("badge should be in its own column, got:\n%s", view)
	}
}

func TestFormatSidebarBadgeCell_RightAligns(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	got := stripANSIForTest(formatSidebarBadgeCell(m.styles, "6"))
	if !strings.HasSuffix(strings.TrimSpace(got), "6") {
		t.Fatalf("expected right-aligned badge, got %q", got)
	}
	if lipgloss.Width(got) > sidebarColBadgeWidth {
		t.Fatalf("badge cell too wide: %d", lipgloss.Width(got))
	}
}
