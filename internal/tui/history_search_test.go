package tui

import (
	"strings"
	"testing"
)

func TestFindSearchMatches_MultipleOccurrences(t *testing.T) {
	msgs := []chatMessage{
		{Sender: "Igor", Text: "hello world hello"},
		{Sender: "Aurelia", Text: "no match here"},
	}
	matches := findSearchMatches(msgs, "hello")
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].messageIndex != 0 || matches[1].messageIndex != 0 {
		t.Fatalf("unexpected message indices: %+v", matches)
	}
}

func TestHistorySearch_TypeAndNavigate(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 30
	m.messages = []chatMessage{
		{Sender: "Igor", Text: "find me alpha"},
		{Sender: "Aurelia", Text: "beta find me again"},
	}

	updated, _ := m.handleKeyMsg(keyCtrl('s'))
	m2 := updated.(Model)
	if !m2.historySearch.active {
		t.Fatal("expected search mode active")
	}

	m3, _ := m2.handleHistorySearchKey(keyText("f"))
	m4, _ := m3.handleHistorySearchKey(keyText("i"))
	m5, _ := m4.handleHistorySearchKey(keyText("n"))
	m6, _ := m5.handleHistorySearchKey(keyText("d"))

	if len(m6.historySearch.matches) < 2 {
		t.Fatalf("expected at least 2 matches for 'find', got %d", len(m6.historySearch.matches))
	}

	m7, _ := m6.handleHistorySearchKey(keyCtrl('s'))
	if m7.historySearch.matchCursor != 1 {
		t.Fatalf("expected match cursor 1 after next, got %d", m7.historySearch.matchCursor)
	}

	bar := stripANSIForTest(m7.renderSearchBar())
	if !strings.Contains(bar, "(2/") && !strings.Contains(bar, "(3/") {
		t.Fatalf("expected match position in search bar, got %q", bar)
	}
}

func TestHighlightSearchText_ActiveMatchBold(t *testing.T) {
	matches := []searchMatch{{messageIndex: 0, start: 6, end: 8}}
	style := newStylesForTheme(ThemeDark).SearchHighlightStyle
	got := highlightSearchText("hello world", 0, 0, matches, style)
	if got == "hello world" {
		t.Fatal("expected highlighted output")
	}
	if !strings.Contains(got, "wo") {
		t.Fatalf("expected match text preserved, got %q", got)
	}
	if got == stripANSIForTest(got) {
		t.Fatal("expected active match styling")
	}
}

func TestScrollViewportToSearchMatch_JumpsWithinPage(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 30
	m.messages = make([]chatMessage, 55)
	for i := range m.messages {
		m.messages[i] = chatMessage{Sender: "Igor", Text: strings.Repeat("line\n", 20) + "TARGET here"}
	}
	m.messages[54] = chatMessage{Sender: "Igor", Text: "needle at end"}

	m.historyNav.resetToLastPage(len(m.messages))
	m.ensureViewport()

	m.historySearch.active = true
	m.historySearch.query = "needle"
	m.historySearch.matches = findSearchMatches(m.messages, "needle")
	m.historySearch.matchCursor = 0
	m.jumpToSearchMatch(0)

	if m.historyNav.paginator.Page != 1 {
		t.Fatalf("expected page 1, got %d", m.historyNav.paginator.Page)
	}
	if m.viewport.YOffset() == 0 {
		t.Fatal("expected viewport scrolled toward match")
	}
}