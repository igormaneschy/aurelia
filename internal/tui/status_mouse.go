package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type statusBarHitKind int

const (
	statusBarHitNone statusBarHitKind = iota
	statusBarHitModel
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
	if strings.Contains(plain, "●") {
		return true
	}
	if strings.Contains(plain, statusBarHelpToken) {
		return true
	}
	if model := m.activeModelLabel(); model != "" && strings.Contains(plain, model) {
		return true
	}
	return false
}

func (m Model) statusBarY() int {
	if m.height <= 0 {
		return -1
	}
	// Footer is pinned to the terminal bottom once layout heights are synced.
	return m.height - 1
}

func (m Model) statusBarPlainLine() string {
	model := m.activeModelLabel()
	for _, line := range m.layoutLines() {
		plain := stripANSI(line)
		if model != "" && strings.Contains(plain, model) {
			return plain
		}
	}
	for i := len(m.layoutLines()) - 1; i >= 0; i-- {
		if m.isStatusBarLine(m.layoutLines()[i]) {
			return stripANSI(m.layoutLines()[i])
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
	plain := stripANSI(lines[y])
	if statusBarSegmentHit(plain, statusBarHelpToken, x) {
		return statusBarHitHelp
	}
	if model := m.activeModelLabel(); statusBarSegmentHit(plain, model, x) {
		return statusBarHitModel
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

func (m Model) headerProjectHit(x, y int) bool {
	if y != m.chatHeaderY() || m.isChatMode() {
		return false
	}
	if x < m.mainPaneStartX() {
		return false
	}
	header := m.renderChatHeader()
	lines := strings.Split(header, "\n")
	if len(lines) == 0 {
		return false
	}
	plain := stripANSI(lines[0])
	idx := strings.Index(plain, "project ")
	if idx < 0 {
		return false
	}
	relX := x - m.mainPaneStartX()
	return relX >= idx && relX < len(plain)
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

func (m Model) sidebarProjectBlockYRange() (start, end int) {
	start = m.sidebarLineY("Project")
	if start < 0 {
		return -1, -1
	}
	// Title, project name, and full cwd path lines.
	return start, start + 2
}

func (m Model) sidebarProjectHit(x, y int) bool {
	if !m.shouldShowSidebar() || m.sidebarFocused || m.isChatMode() {
		return false
	}
	if !sidebarMouseHitX(x) {
		return false
	}
	start, end := m.sidebarProjectBlockYRange()
	if start < 0 {
		return false
	}
	return y >= start && y <= end
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
		case statusBarHitModel:
			next, c := m.openModelSelect()
			return true, next, c
		}
		if m.headerProjectHit(msg.X, msg.Y) {
			next, c := m.openCwdForm()
			return true, next, c
		}
		if m.sidebarProjectHit(msg.X, msg.Y) {
			next, c := m.openCwdForm()
			return true, next, c
		}
		if m.sidebarNewSessionHit(msg.X, msg.Y) {
			next, c := m.openNewSessionForm()
			return true, next, c
		}
	case tea.MouseMotionMsg:
		// no-op for now
	}
	return false, m, nil
}