package tui

import "strings"

// sidebarChromeLineCount is non-table sidebar chrome (title, sections, hints).
func (m Model) sidebarChromeLineCount() int {
	if len(m.sessions) == 0 {
		return sidebarEmptyLines
	}
	n := sidebarTitleLines + sidebarSectionHeader + sidebarTableHeaderLines
	if m.sidebarFocused {
		return n + sidebarFocusedHintLines
	}
	// Sessions section rule + context + actions panels.
	n += sidebarSectionHeader + sidebarContextLines + sidebarSectionHeader + sidebarActionsLines
	return n
}

// sidebarTableHeightForBody sizes the session table to fit the dynamic body height.
func (m Model) sidebarTableHeightForBody() int {
	if !m.shouldShowSidebarTable() {
		return sidebarTableHeightForTerminal(m.height)
	}
	h := m.chatBodyHeight() - m.sidebarChromeLineCount()
	if h < 4 {
		return 4
	}
	if h > 20 {
		return 20
	}
	return h
}

// footerLineCount returns rendered footer rows (input badges, progress, status bar).
func (m Model) footerLineCount() int {
	n := strings.Count(m.renderInput(), "\n") + 1
	if pb := m.footerProgressBar(); pb != "" {
		n += strings.Count(pb, "\n") + 1
	}
	n += strings.Count(m.renderStatusBar(), "\n") + 1
	return n
}

// chatBodyHeight is the vertical space for sidebar + chat header + viewport.
func (m Model) chatBodyHeight() int {
	h := m.height - topMarginHeight - m.footerLineCount()
	if h < 1 {
		return 1
	}
	return h
}

// viewportHeight is the scrollable transcript area below the chat header.
func (m Model) viewportHeight() int {
	h := m.chatBodyHeight() - chatHeaderHeight
	if m.historyNav.hasNewBelow {
		h--
	}
	if h < 1 {
		return 1
	}
	if h < minViewportHeight && m.height >= minViewportHeight+topMarginHeight+chatHeaderHeight+m.footerLineCount() {
		return h
	}
	return h
}

func (m *Model) syncViewportDimensions() {
	if m.height <= 0 {
		return
	}
	m.resizeSidebarTable()
	if !m.viewportSet {
		return
	}
	m.viewport.SetWidth(m.contentWidth())
	m.viewport.SetHeight(m.viewportHeight())
}