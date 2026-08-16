package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

const (
	sidebarTitleLines       = 2
	sidebarTableHeaderLines = 1
	sidebarBorderLines      = 1 // top border row before inner content
	sidebarSectionRuleLines = 1
	sidebarSectionHeader    = 1
	sidebarContextLines     = 4 // cwd, model, health, spacing
	sidebarActionsLines     = 2 // section title + button
	sidebarFocusedHintLines = 7
	sidebarEmptyLines       = 4
)

const sidebarModelPlaceholder = "—"

const (
	// sidebarInnerWidth is the table width inside the bordered sidebar panel.
	sidebarInnerWidth    = 20
	sidebarColNavWidth   = 4
	sidebarColNameWidth  = 12
	sidebarColBadgeWidth = 4
)

func sidebarTableFirstRowY() int {
	return topMarginHeight + sidebarBorderLines + sidebarTitleLines + sidebarSectionRuleLines + sidebarSectionHeader + sidebarTableHeaderLines
}

func sidebarMouseHitX(x int) bool { return x >= 0 && x < sidebarWidth }

func sidebarTableScrollStart(cursor, viewportHeight int) int {
	if cursor >= 0 {
		return clampInt(cursor-viewportHeight, 0, cursor)
	}
	return 0
}

func (m Model) sidebarRowAt(y int) int {
	if !m.shouldShowSidebar() || len(m.sessions) == 0 {
		return -1
	}
	firstRowY := sidebarTableFirstRowY()
	if y < firstRowY {
		return -1
	}
	visibleRow := y - firstRowY
	tableHeight := m.sidebarTable.Height()
	if visibleRow < 0 || visibleRow >= tableHeight {
		return -1
	}
	row := sidebarTableScrollStart(m.sidebarCursor, tableHeight) + visibleRow
	if row < 0 || row >= len(m.sessions) {
		return -1
	}
	return row
}

func clampInt(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

// newSidebarTable creates a table.Model for the session sidebar.
func newSidebarTable(styles themeStyles) table.Model {
	cols := []table.Column{
		{Title: "", Width: sidebarColNavWidth},
		{Title: "Session", Width: sidebarColNameWidth},
		{Title: "", Width: sidebarColBadgeWidth},
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(false),
		table.WithHeight(12),
	)

	t.SetWidth(sidebarInnerWidth)
	t.SetHeight(12)
	t.SetStyles(sidebarTableStyles(styles))

	// Remove bare "d" from HalfPageDown — "d" is reserved for sidebar delete.
	km := table.DefaultKeyMap()
	km.HalfPageDown = key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "½ page down"),
	)
	t.KeyMap = km

	return t
}

func sidebarTableStyles(styles themeStyles) table.Styles {
	s := table.DefaultStyles()
	s.Header = styles.SidebarMutedStyle.Bold(true).Padding(0, 0)
	s.Cell = styles.SidebarMutedStyle.Padding(0, 0)
	s.Selected = styles.SidebarHoverStyle.Padding(0, 0)
	return s
}

// sidebarTableHeightForTerminal returns the table body height for the sidebar.
func sidebarTableHeightForTerminal(termHeight int) int {
	h := viewportHeightForTerminal(termHeight) - 8
	if h < 4 {
		return 4
	}
	if h > 20 {
		return 20
	}
	return h
}

// resizeSidebarTable updates sidebar table dimensions after terminal resize.
func (m *Model) resizeSidebarTable() {
	if !m.shouldShowSidebarTable() {
		return
	}
	m.sidebarTable.SetWidth(sidebarInnerWidth)
	m.sidebarTable.SetHeight(m.sidebarTableHeightForBody())
	m.syncSidebarRows()
}

func (m Model) sessionNavIcon(i int, s tuiSessionInfo) string {
	isActive := s.ChatID == m.activeSession
	isCursor := m.sidebarFocused && i == m.sidebarCursor
	switch {
	case isActive && isCursor:
		return "▶ ●"
	case isActive:
		return "●"
	case isCursor:
		return "▶"
	default:
		return "○"
	}
}

func (m Model) sessionEmoji(s tuiSessionInfo) string {
	if s.ChatID == ipc.ReservedTUIChatID {
		return "💬"
	}
	if !m.isChatMode() && s.ChatID == m.activeSession {
		return "📁"
	}
	return "💬"
}

func (m Model) sessionModelLabel(s tuiSessionInfo) string {
	if s.ChatID == m.activeSession && m.activeModel != "" {
		return truncateMiddle(m.activeModel, 10)
	}
	return m.styles.SidebarMutedStyle.Render(sidebarModelPlaceholder)
}

// syncSidebarRows rebuilds the table rows from the current sessions slice.
func (m *Model) syncSidebarRows() {
	rows := make([]table.Row, 0, len(m.sessions))
	for i, s := range m.sessions {
		label := safeSessionLabel(s.Name)
		if s.ChatID == ipc.ReservedTUIChatID {
			label = "DM"
		}
		label = truncateMiddle(label, sidebarColNameWidth)

		icon := m.sessionNavIcon(i, s) + " " + m.sessionEmoji(s)
		if s.ChatID == m.activeSession &&
			!m.sessionFlashUntil.IsZero() && time.Now().Before(m.sessionFlashUntil) &&
			m.animations.enabled && m.animations.BadgeScale() > 1.05 {
			icon = m.styles.SidebarActiveStyle.Render(icon)
		}

		rows = append(rows, table.Row{icon, label, formatSidebarBadgeCell(m.styles, m.sessionUnreadBadge(s.ChatID))})
	}

	m.sidebarTable.SetRows(rows)
	m.sidebarTable.UpdateViewport()
	tableCursor := m.sidebarCursor
	if m.sidebarHoverRow >= 0 && !m.sidebarFocused {
		tableCursor = m.sidebarHoverRow
	}
	if tableCursor >= 0 && tableCursor < len(rows) {
		m.sidebarTable.SetCursor(tableCursor)
	}
}

// shouldShowSidebarTable returns true when sidebar table is visible.
func (m Model) shouldShowSidebarTable() bool {
	return m.showSidebar && m.width >= minSidebarScreenWidth && m.height >= minSidebarScreenHeight
}

func (m Model) sidebarSectionRule() string {
	return m.styles.MessageSeparatorStyle.Render(strings.Repeat("─", maxInt(12, sidebarWidth-4)))
}

func (m Model) renderSidebarTitle() string {
	return m.styles.SidebarTitleStyle.Render("Aurelia") + "\n" +
		m.styles.SidebarMutedStyle.Render("local terminal")
}

func (m Model) renderSidebarSessionsPanel() string {
	section := m.styles.SidebarSectionStyle.Render("Sessions")
	return "\n" + m.sidebarSectionRule() + "\n" + section + "\n" + m.sidebarTable.View()
}

func (m Model) renderSidebarContextPanel() string {
	var cwdLabel string
	if m.isChatMode() {
		cwdLabel = "no project"
	} else {
		cwdLabel = truncateMiddle(projectName(m.cwdPath), sidebarWidth-6)
	}

	modelLabel := sidebarModelPlaceholder
	if m.activeModel != "" {
		modelLabel = truncateMiddle(m.activeModel, sidebarWidth-6)
	}

	section := m.styles.SidebarSectionStyle.Render("Context")
	cwdLine := "📂 " + cwdLabel
	modelLine := "🤖 " + modelLabel
	healthLine := m.renderSidebarHealthChip()

	return "\n" + m.sidebarSectionRule() + "\n" + section + "\n" +
		cwdLine + "\n" + modelLine + "\n" + healthLine
}

func (m Model) renderSidebarHealthChip() string {
	state := m.chromeState()
	label := fmt.Sprintf("%s %s", m.healthChipIcon(), state)
	switch state {
	case "offline", "error":
		return m.styles.StatusErrorStyle.Render(label)
	case "waiting", "connecting":
		return m.styles.StatusBusyStyle.Render(label)
	default:
		return m.styles.StatusReadyStyle.Render(label)
	}
}

func (m Model) renderSidebarActionsPanel() string {
	section := m.styles.SidebarSectionStyle.Render("Actions")
	button := m.styles.SidebarButtonStyle.Render(" + New session ")
	return "\n" + m.sidebarSectionRule() + "\n" + section + "\n" + button
}

func (m Model) renderSidebarFocusHints() string {
	return fmt.Sprintf("\n%s\n%s\n%s\n%s\n%s\n%s",
		m.styles.SidebarMutedStyle.Render("↑↓ navigate"),
		m.styles.SidebarMutedStyle.Render("enter open"),
		m.styles.SidebarMutedStyle.Render("n new"),
		m.styles.SidebarMutedStyle.Render("r rename"),
		m.styles.SidebarMutedStyle.Render("d delete"),
		m.styles.SidebarMutedStyle.Render("esc exit"),
	)
}

func formatSidebarBadgeCell(styles themeStyles, badge string) string {
	if badge == "" {
		return ""
	}
	rendered := styles.SidebarUnreadStyle.Render(badge)
	pad := sidebarColBadgeWidth - lipgloss.Width(rendered)
	if pad > 0 {
		rendered = strings.Repeat(" ", pad) + rendered
	}
	return rendered
}

// renderSidebarFramed renders the sidebar inside its bordered panel with a
// fixed height so the bottom border is never clipped by the chat layout.
func (m Model) renderSidebarFramed(height int) string {
	if height < 3 {
		height = 3
	}
	inner := m.renderSidebarTable()
	// Rounded border consumes two lines (top + bottom).
	maxInner := height - 2
	if maxInner < 1 {
		maxInner = 1
	}
	inner = clipLines(inner, maxInner)
	return m.styles.SidebarStyle.
		Height(height).
		Render(inner)
}

// renderSidebarTable renders the sidebar in Sessions · Context · Actions panels.
func (m Model) renderSidebarTable() string {
	if len(m.sessions) == 0 {
		return lipgloss.NewStyle().
			Width(sidebarWidth).
			Padding(1, 2).
			Foreground(lipgloss.Color("243")).
			Render("(no sessions)")
	}

	var b strings.Builder
	b.WriteString(m.renderSidebarTitle())
	b.WriteString(m.renderSidebarSessionsPanel())
	if m.sidebarFocused {
		b.WriteString(m.renderSidebarFocusHints())
		return b.String()
	}
	b.WriteString(m.renderSidebarContextPanel())
	b.WriteString(m.renderSidebarActionsPanel())
	return b.String()
}
