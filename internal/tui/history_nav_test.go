package tui

import (
	"strings"
	"testing"
)

func makeMessages(n int) []chatMessage {
	out := make([]chatMessage, n)
	for i := range out {
		out[i] = chatMessage{Sender: "Igor", Text: strings.Repeat("x", i+1)}
	}
	return out
}

func TestHistoryNav_PageSlice_50PerPage(t *testing.T) {
	nav := newHistoryNav()
	msgs := makeMessages(120)
	nav.resetToLastPage(len(msgs))

	nav.paginator.Page = 0
	slice := nav.pageSlice(msgs)
	if len(slice) != 50 {
		t.Fatalf("page 0 len = %d, want 50", len(slice))
	}

	nav.paginator.Page = 2
	slice = nav.pageSlice(msgs)
	if len(slice) != 20 {
		t.Fatalf("page 2 len = %d, want 20", len(slice))
	}
}

func TestHistoryNav_SyncMessageCount_SetsNewBelow(t *testing.T) {
	nav := newHistoryNav()
	msgs := makeMessages(60)
	nav.resetToLastPage(len(msgs))
	nav.paginator.Page = 0

	nav.syncMessageCount(len(msgs) + 1)
	if !nav.hasNewBelow {
		t.Fatal("expected hasNewBelow on append while not on last page")
	}

	nav.paginator.Page = nav.paginator.TotalPages - 1
	nav.syncMessageCount(len(msgs) + 2)
	if nav.hasNewBelow {
		t.Fatal("expected hasNewBelow cleared on last page")
	}
}

func TestHistoryNav_PageLabel(t *testing.T) {
	nav := newHistoryNav()
	if nav.pageLabel() != "" {
		t.Fatal("expected empty label for single page")
	}
	nav.resetToLastPage(120)
	nav.paginator.Page = 1
	if got := nav.pageLabel(); got != "[2/3]" {
		t.Fatalf("label = %q, want [2/3]", got)
	}
}

func TestHistoryNextPage_AdvancesAndShowsEarlierMessages(t *testing.T) {
	m := testChatModel()
	m.width = 80
	m.height = 24
	m.messages = makeMessages(75)
	m.historyNav.resetToLastPage(len(m.messages))
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true

	next, cmd := m.historyPrevPage()
	if cmd != nil {
		t.Fatal("expected nil cmd on first page")
	}
	if next.historyNav.paginator.Page != 0 {
		t.Fatalf("page = %d, want 0", next.historyNav.paginator.Page)
	}

	slice := next.historyNav.pageSlice(next.messages)
	if len(slice) != 50 || slice[0].Text != "x" {
		t.Fatalf("unexpected first message on page 0: %#v", slice[0])
	}
}

func TestHistoryNextPage_FromFirstReachesLast(t *testing.T) {
	m := testChatModel()
	m.width = 80
	m.height = 24
	m.messages = makeMessages(75)
	m.historyNav.resetToLastPage(len(m.messages))
	m.historyNav.paginator.Page = 0
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true

	next, _ := m.historyNextPage()
	if next.historyNav.paginator.Page != 1 {
		t.Fatalf("page = %d, want 1", next.historyNav.paginator.Page)
	}
	if next.historyNav.hasNewBelow {
		t.Fatal("expected hasNewBelow cleared when reaching last page")
	}
}