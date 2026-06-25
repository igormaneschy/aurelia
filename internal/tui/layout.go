package tui

import "strings"

// footerLineCount returns rendered footer rows (input badges, progress, status bar).
func (m Model) footerLineCount() int {
	n := strings.Count(m.renderInput(), "\n") + 1
	if m.showStreamProgress() {
		if pb := m.renderStreamProgress(m.width); pb != "" {
			n += strings.Count(pb, "\n") + 1
		}
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
	if !m.viewportSet || m.height <= 0 {
		return
	}
	m.viewport.SetWidth(m.contentWidth())
	m.viewport.SetHeight(m.viewportHeight())
}