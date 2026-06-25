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
	if model := m.activeModelLabel(); model != "" && strings.Contains(plain, model) {
		return true
	}
	return false
}

func (m Model) statusBarY() int {
	lines := m.layoutLines()
	for i := len(lines) - 1; i >= 0; i-- {
		if m.isStatusBarLine(lines[i]) {
			return i
		}
	}
	if m.height > 0 {
		return m.height - 1
	}
	return -1
}

func (m Model) statusBarPlainLine() string {
	lines := m.layoutLines()
	y := m.statusBarY()
	if y >= 0 && y < len(lines) {
		return stripANSI(lines[y])
	}
	return stripANSI(m.renderStatusBar())
}

func (m Model) statusBarHitAt(y, x int) statusBarHitKind {
	model := m.activeModelLabel()
	if model == "" || x < 0 || y < 0 {
		return statusBarHitNone
	}
	lines := m.layoutLines()
	if y >= len(lines) || !m.isStatusBarLine(lines[y]) {
		return statusBarHitNone
	}
	plain := stripANSI(lines[y])
	idx := strings.Index(plain, model)
	if idx < 0 {
		return statusBarHitNone
	}
	if x >= idx && x < idx+len(model) {
		return statusBarHitModel
	}
	return statusBarHitNone
}

func (m Model) statusBarHit(x int) statusBarHitKind {
	return m.statusBarHitAt(m.statusBarY(), x)
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

func (m Model) sidebarProjectHit(x, y int) bool {
	if !m.shouldShowSidebar() || m.sidebarFocused || m.isChatMode() {
		return false
	}
	if !sidebarMouseHitX(x) {
		return false
	}
	path := truncateMiddle(m.cwdPath, sidebarWidth-4)
	if path == "" {
		return false
	}
	hintY := m.sidebarLineY(path)
	return hintY >= 0 && y == hintY
}

func (m Model) handleChatMouse(msg tea.MouseMsg) (handled bool, model tea.Model, cmd tea.Cmd) {
	if m.formOpen || m.helpVisible() || m.projectPanelOpen {
		return false, m, nil
	}
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return false, m, nil
		}
		switch m.statusBarHitAt(msg.Y, msg.X) {
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