package tui

import (
	"fmt"

	"charm.land/bubbles/v2/paginator"
	tea "charm.land/bubbletea/v2"
)

const historyPageSize = 50

// historyNav paginates chat transcript messages for ctrl+f / ctrl+b navigation.
type historyNav struct {
	paginator        paginator.Model
	hasNewBelow      bool
	lastMessageCount int
}

func (n *historyNav) ensurePager() {
	if n.paginator.PerPage < 1 {
		n.paginator.PerPage = historyPageSize
	}
	if n.paginator.TotalPages < 1 {
		n.paginator.TotalPages = 1
	}
}

func newHistoryNav() historyNav {
	p := paginator.New(paginator.WithPerPage(historyPageSize))
	p.Type = paginator.Arabic
	p.ArabicFormat = "%d/%d"
	p.TotalPages = 1
	return historyNav{paginator: p}
}

func (n *historyNav) resetToLastPage(count int) {
	n.ensurePager()
	n.hasNewBelow = false
	n.lastMessageCount = 0
	if count < 1 {
		n.paginator.Page = 0
		n.paginator.TotalPages = 1
		return
	}
	n.paginator.SetTotalPages(count)
	n.paginator.Page = n.paginator.TotalPages - 1
	n.lastMessageCount = count
}

func (n *historyNav) syncMessageCount(count int) {
	n.ensurePager()
	prev := n.lastMessageCount
	if count < 1 {
		n.paginator.Page = 0
		n.paginator.TotalPages = 1
		n.hasNewBelow = false
		n.lastMessageCount = 0
		return
	}
	totalPages := n.paginator.SetTotalPages(count)
	if n.paginator.Page >= totalPages {
		n.paginator.Page = totalPages - 1
	}
	if count > prev && !n.paginator.OnLastPage() {
		n.hasNewBelow = true
	}
	if n.paginator.OnLastPage() {
		n.hasNewBelow = false
	}
	n.lastMessageCount = count
}

func (n historyNav) pageSlice(messages []chatMessage) []chatMessage {
	if len(messages) == 0 {
		return messages
	}
	start, end := n.paginator.GetSliceBounds(len(messages))
	return messages[start:end]
}

func (n historyNav) pageLabel() string {
	if n.paginator.TotalPages <= 1 {
		return ""
	}
	return fmt.Sprintf("[%d/%d]", n.paginator.Page+1, n.paginator.TotalPages)
}

func (m Model) historyNextPage() (Model, tea.Cmd) {
	if len(m.messages) == 0 || m.historyNav.paginator.OnLastPage() {
		return m, nil
	}
	m.historyNav.paginator.NextPage()
	if m.historyNav.paginator.OnLastPage() {
		m.historyNav.hasNewBelow = false
	}
	m.updateViewportToPage()
	return m, nil
}

func (m Model) historyPrevPage() (Model, tea.Cmd) {
	if len(m.messages) == 0 || m.historyNav.paginator.OnFirstPage() {
		return m, nil
	}
	m.historyNav.paginator.PrevPage()
	m.updateViewportToPage()
	return m, nil
}

func (m *Model) updateViewportToPage() {
	m.ensureViewport()
	if !m.viewportSet || m.viewport.Height() <= 0 {
		return
	}
	contentWidth := m.contentWidth()
	pageMsgs := m.historyNav.pageSlice(m.messages)
	m.viewport.SetWidth(contentWidth)
	m.viewport.SetContent(m.renderMessages(pageMsgs, contentWidth))
	m.viewport.GotoTop()
}