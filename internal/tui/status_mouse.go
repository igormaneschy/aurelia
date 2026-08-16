package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type statusBarHitKind int

const (
	statusBarHitNone statusBarHitKind = iota
	statusBarHitHelp
)

func (m Model) layoutLines() []string {
	layout := m.renderChatBaseLayout()
	if layout == "" {
		return nil
	}
	return strings.Split(layout, "\n")
}

func (m Model) isStatusBarLine(line string) bool {
	plain := stripANSI(line)
	return strings.Contains(plain, statusBarHelpToken) ||
		strings.Contains(plain, "🖱️ mouse") ||
		strings.Contains(plain, "✋ mouse") ||
		strings.Contains(plain, "↵ send")
}

func (m Model) statusBarY() int {
	if m.height <= 0 {
		return -1
	}
	// Footer is pinned to the terminal bottom once layout heights are synced.
	return m.height - 1
}

func (m Model) statusBarPlainLine() string {
	lines := m.layoutLines()
	for y := len(lines) - 1; y >= m.statusBarFooterStartY(); y-- {
		if m.isStatusBarLine(lines[y]) {
			return stripANSI(lines[y])
		}
	}
	rendered := strings.Split(m.renderStatusBar(), "\n")
	for i := len(rendered) - 1; i >= 0; i-- {
		if m.isStatusBarLine(rendered[i]) {
			return stripANSI(rendered[i])
		}
	}
	return stripANSI(m.renderStatusBar())
}

func (m Model) statusBarFooterStartY() int {
	return topMarginHeight + m.chatBodyHeight()
}

func (m Model) statusBarHitAt(y, x int) statusBarHitKind {
	if x < 0 || y < 0 || y < m.statusBarFooterStartY() {
		return statusBarHitNone
	}
	lines := m.layoutLines()
	if y >= len(lines) {
		return statusBarHitNone
	}
	if !m.isStatusBarLine(lines[y]) {
		return statusBarHitNone
	}
	plain := stripANSI(lines[y])
	if statusBarSegmentHit(plain, statusBarHelpToken, x) {
		return statusBarHitHelp
	}
	return statusBarHitNone
}

func statusBarSegmentHit(plain, token string, x int) bool {
	if token == "" {
		return false
	}
	idx := strings.Index(plain, token)
	if idx < 0 {
		return false
	}
	return x >= idx && x < idx+len(token)
}

func (m Model) statusBarHit(x int) statusBarHitKind {
	for y := m.statusBarFooterStartY(); y < len(m.layoutLines()); y++ {
		if kind := m.statusBarHitAt(y, x); kind != statusBarHitNone {
			return kind
		}
	}
	return statusBarHitNone
}

func (m Model) mainPaneStartX() int {
	if !m.shouldShowSidebar() {
		return 0
	}
	sidebar := m.styles.SidebarStyle.Render(m.renderSidebarTable())
	firstLine := sidebar
	if idx := strings.Index(sidebar, "\n"); idx >= 0 {
		firstLine = sidebar[:idx]
	}
	return lipgloss.Width(firstLine)
}

func (m Model) chatHeaderY() int {
	return topMarginHeight
}

func (m Model) sidebarRenderedLines() []string {
	if !m.shouldShowSidebar() {
		return nil
	}
	return strings.Split(m.styles.SidebarStyle.Render(m.renderSidebarTable()), "\n")
}

func (m Model) sidebarLineY(needle string) int {
	for i, line := range m.sidebarRenderedLines() {
		if strings.Contains(stripANSI(line), needle) {
			return topMarginHeight + i
		}
	}
	return -1
}

func (m Model) headerModelHit(x, y int) bool {
	if y != m.chatHeaderY() {
		return false
	}
	if x < m.mainPaneStartX() {
		return false
	}
	model := m.activeModelLabel()
	if model == "" {
		model = sidebarModelPlaceholder
	}
	header := m.renderChatHeader()
	lines := strings.Split(header, "\n")
	if len(lines) == 0 {
		return false
	}
	plain := stripANSI(lines[0])
	idx := strings.Index(plain, model)
	if idx < 0 {
		return false
	}
	relX := x - m.mainPaneStartX()
	return relX >= idx && relX < idx+len(model)
}

func (m Model) sidebarNewSessionHit(x, y int) bool {
	if !m.shouldShowSidebar() || m.sidebarFocused || len(m.sessions) == 0 {
		return false
	}
	if !sidebarMouseHitX(x) {
		return false
	}
	hintY := m.sidebarLineY("+ New session")
	return hintY >= 0 && y >= hintY && y <= hintY+1
}

func (m Model) sidebarCwdHit(x, y int) bool {
	if !m.shouldShowSidebar() || m.sidebarFocused {
		return false
	}
	if !sidebarMouseHitX(x) {
		return false
	}
	lineY := m.sidebarLineY("📂")
	return lineY >= 0 && y == lineY
}

func (m Model) sidebarModelHit(x, y int) bool {
	if !m.shouldShowSidebar() || m.sidebarFocused {
		return false
	}
	if !sidebarMouseHitX(x) {
		return false
	}
	lineY := m.sidebarLineY("🤖")
	return lineY >= 0 && y == lineY
}

func (m Model) sidebarProjectHit(x, y int) bool {
	return m.sidebarCwdHit(x, y)
}

func (m Model) handleChatMouse(msg tea.MouseMsg) (handled bool, model tea.Model, cmd tea.Cmd) {
	if m.modalOpen() {
		return false, m, nil
	}
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return false, m, nil
		}
		switch m.statusBarHitAt(msg.Y, msg.X) {
		case statusBarHitHelp:
			next := m
			if next.helpVisible() {
				next = next.closeHelpOverlay()
			} else {
				next = next.openHelpOverlay()
			}
			return true, next, nil
		}
		if m.headerModelHit(msg.X, msg.Y) {
			next, c := m.openModelSelect()
			return true, next, c
		}
		if m.sidebarProjectHit(msg.X, msg.Y) {
			next, c := m.openCwdForm()
			return true, next, c
		}
		if m.sidebarModelHit(msg.X, msg.Y) {
			next, c := m.openModelSelect()
			return true, next, c
		}
		if m.sidebarNewSessionHit(msg.X, msg.Y) {
			next, c := m.openNewSessionForm()
			return true, next, c
		}
	case tea.MouseMotionMsg:
		hover := m.statusBarHitAt(msg.Y, msg.X)
		if hover != m.statusBarHover {
			next := m
			next.statusBarHover = hover
			return true, next, nil
		}
	}
	return false, m, nil
}
