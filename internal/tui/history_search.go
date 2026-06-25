package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type searchMatch struct {
	messageIndex int
	start        int
	end          int
}

// historySearch drives inline transcript search (Ctrl+S).
type historySearch struct {
	active      bool
	query       string
	matchCursor int
	matches     []searchMatch
}

func findSearchMatches(messages []chatMessage, query string) []searchMatch {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	lowerQ := strings.ToLower(query)
	var matches []searchMatch
	for i, msg := range messages {
		text := msg.Text
		lower := strings.ToLower(text)
		off := 0
		for {
			idx := strings.Index(lower[off:], lowerQ)
			if idx < 0 {
				break
			}
			abs := off + idx
			matches = append(matches, searchMatch{
				messageIndex: i,
				start:        abs,
				end:          abs + len(query),
			})
			off = abs + len(query)
		}
	}
	return matches
}

func (m Model) openHistorySearch() (Model, tea.Cmd) {
	m.historySearch.active = true
	if m.historySearch.query == "" {
		m.historySearch.matches = nil
		m.historySearch.matchCursor = 0
		return m, nil
	}
	return m.refreshSearchMatches()
}

func (m Model) closeHistorySearch() Model {
	m.historySearch.active = false
	m.historySearch.matches = nil
	m.historySearch.matchCursor = 0
	return m
}

func (m Model) refreshSearchMatches() (Model, tea.Cmd) {
	m.historySearch.matches = findSearchMatches(m.messages, m.historySearch.query)
	m.historySearch.matchCursor = 0
	if len(m.historySearch.matches) > 0 {
		m.jumpToSearchMatch(0)
	}
	m.updateViewport()
	return m, nil
}

func (m Model) nextSearchMatch() (Model, tea.Cmd) {
	if len(m.historySearch.matches) == 0 {
		return m, nil
	}
	m.historySearch.matchCursor = (m.historySearch.matchCursor + 1) % len(m.historySearch.matches)
	m.jumpToSearchMatch(m.historySearch.matchCursor)
	m.updateViewport()
	return m, nil
}

func (m *Model) jumpToSearchMatch(cursor int) {
	if cursor < 0 || cursor >= len(m.historySearch.matches) {
		return
	}
	match := m.historySearch.matches[cursor]
	m.historyNav.paginator.SetTotalPages(len(m.messages))
	page := match.messageIndex / historyPageSize
	if page < 0 {
		page = 0
	}
	if page >= m.historyNav.paginator.TotalPages {
		page = maxInt(0, m.historyNav.paginator.TotalPages-1)
	}
	m.historyNav.paginator.Page = page
	m.historyNav.hasNewBelow = page < m.historyNav.paginator.TotalPages-1
	m.updateViewportToPage()
	m.scrollViewportToSearchMatch(cursor)
}

func messageHasSearchMatch(msgIndex int, matches []searchMatch) bool {
	for _, match := range matches {
		if match.messageIndex == msgIndex {
			return true
		}
	}
	return false
}

func (m *Model) scrollViewportToSearchMatch(cursor int) {
	if cursor < 0 || cursor >= len(m.historySearch.matches) || !m.viewportSet {
		return
	}
	match := m.historySearch.matches[cursor]
	pageStart, _ := m.historyNav.paginator.GetSliceBounds(len(m.messages))
	localIdx := match.messageIndex - pageStart
	if localIdx < 0 {
		return
	}

	contentWidth := m.contentWidth()
	pageMsgs := m.historyNav.pageSlice(m.messages)
	content := m.renderMessages(pageMsgs, contentWidth)
	lineOffset := lineOffsetBeforeMessage(content, localIdx)

	if localIdx < len(pageMsgs) {
		msgText := pageMsgs[localIdx].Text
		start := match.start
		if start > len(msgText) {
			start = len(msgText)
		}
		lineOffset += strings.Count(msgText[:start], "\n")
	}

	maxOffset := maxInt(0, m.viewport.TotalLineCount()-m.viewport.Height())
	if lineOffset > maxOffset {
		lineOffset = maxOffset
	}
	m.viewport.SetYOffset(lineOffset)
}

func (m Model) handleHistorySearchKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m = m.closeHistorySearch()
		m.updateViewport()
		return m, nil
	case "enter":
		return m.nextSearchMatch()
	case "backspace":
		runes := []rune(m.historySearch.query)
		if len(runes) > 0 {
			m.historySearch.query = string(runes[:len(runes)-1])
		}
		return m.refreshSearchMatches()
	case "ctrl+s":
		return m.nextSearchMatch()
	default:
		if k := msg.Key(); len(k.Text) == 1 && k.Mod == 0 {
			m.historySearch.query += k.Text
			return m.refreshSearchMatches()
		}
	}
	return m, nil
}

func (m Model) renderSearchBar() string {
	if !m.historySearch.active {
		return ""
	}
	count := len(m.historySearch.matches)
	pos := ""
	if count > 0 {
		pos = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).
			Render(strings.TrimSpace(" " + formatSearchPos(m.historySearch.matchCursor+1, count)))
	}
	query := m.historySearch.query
	if query == "" {
		query = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("244")).Render("type to search…")
	} else {
		query = m.styles.SearchHighlightStyle.Render(query)
	}
	line := lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Render("🔍 ") + query + pos
	return lipgloss.NewStyle().Width(maxInt(20, m.width-4)).Render(line)
}

func formatSearchPos(cur, total int) string {
	if total == 0 {
		return "(0/0)"
	}
	return "(" + strconv.Itoa(cur) + "/" + strconv.Itoa(total) + ")"
}

func activeSearchStyle(style lipgloss.Style) lipgloss.Style {
	return style.Copy().Bold(true).Reverse(true)
}

func highlightSearchText(text string, msgIndex int, activeCursor int, matches []searchMatch, style lipgloss.Style) string {
	if len(matches) == 0 {
		return text
	}
	activeStyle := activeSearchStyle(style)
	var parts []string
	last := 0
	for i, match := range matches {
		if match.messageIndex != msgIndex {
			continue
		}
		if match.start < last || match.start > len(text) {
			continue
		}
		end := match.end
		if end > len(text) {
			end = len(text)
		}
		parts = append(parts, text[last:match.start])
		chunk := text[match.start:end]
		if i == activeCursor {
			parts = append(parts, activeStyle.Render(chunk))
		} else {
			parts = append(parts, style.Render(chunk))
		}
		last = end
	}
	if last < len(text) {
		parts = append(parts, text[last:])
	}
	if len(parts) == 0 {
		return text
	}
	return strings.Join(parts, "")
}

func lineOffsetBeforeMessage(content string, messageIndex int) int {
	if messageIndex <= 0 || content == "" {
		return 0
	}
	sep := "\n\n"
	pos := 0
	for i := 0; i < messageIndex; i++ {
		idx := strings.Index(content[pos:], sep)
		if idx < 0 {
			return strings.Count(content, "\n")
		}
		pos += idx + len(sep)
	}
	return strings.Count(content[:pos], "\n")
}