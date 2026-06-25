package tui

import (
	"fmt"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

const sidebarColIcon = 0
const sidebarColName = 1
const sidebarColModel = 2

const (
	sidebarTitleLines       = 2
	sidebarTableHeaderLines = 1
	sidebarBorderLines      = 1 // top border row before inner content
)

func sidebarTableFirstRowY() int {
	return topMarginHeight + sidebarBorderLines + sidebarTitleLines + sidebarTableHeaderLines
}

func sidebarMouseHitX(x int) bool { return x >= 0 && x < sidebarWidth }

func sidebarTableScrollStart(cursor, viewportHeight int) int {
	if cursor >= 0 { return clampInt(cursor-viewportHeight, 0, cursor) }
	return 0
}

func (m Model) sidebarRowAt(y int) int {
	if !m.shouldShowSidebar() || len(m.sessions) == 0 { return -1 }
	firstRowY := sidebarTableFirstRowY()
	if y < firstRowY { return -1 }
	visibleRow := y - firstRowY
	tableHeight := m.sidebarTable.Height()
	if visibleRow < 0 || visibleRow >= tableHeight { return -1 }
	row := sidebarTableScrollStart(m.sidebarCursor, tableHeight) + visibleRow
	if row < 0 || row >= len(m.sessions) { return -1 }
	return row
}

func clampInt(v, low, high int) int {
	if v < low { return low }
	if v > high { return high }
	return v
}


// newSidebarTable creates a table.Model for the session sidebar.
func newSidebarTable(styles themeStyles) table.Model {
	cols := []table.Column{
		{Title: "", Width: 3},     // icon
		{Title: "Session", Width: 16},
		{Title: "Model", Width: 0}, // flex: fills remaining
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(false),
		table.WithHeight(12),
	)

	t.SetWidth(sidebarWidth)
	t.SetHeight(12)
	t.SetStyles(sidebarTableStyles(styles))

	return t
}

func sidebarTableStyles(styles themeStyles) table.Styles {
	s := table.DefaultStyles()
	s.Header = styles.SidebarMutedStyle.Copy().Bold(true).Padding(0, 1)
	s.Cell = styles.SidebarMutedStyle.Copy().Padding(0, 1)
	s.Selected = styles.SidebarActiveStyle.Copy().Padding(0, 1)
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
	m.sidebarTable.SetWidth(sidebarWidth)
	m.sidebarTable.SetHeight(sidebarTableHeightForTerminal(m.height))
	m.syncSidebarRows()
}

// syncSidebarRows rebuilds the table rows from the current sessions slice.
func (m *Model) syncSidebarRows() {
	rows := make([]table.Row, 0, len(m.sessions))
	for i, s := range m.sessions {
		label := safeSessionLabel(s.Name)
		if s.ChatID == ipc.ReservedTUIChatID {
			label = "DM"
		}

		icon := "○"
		if s.ChatID == m.activeSession {
			icon = "●"
		}
		if m.sidebarFocused && i == m.sidebarCursor {
			icon = "▶"
			if s.ChatID == m.activeSession {
				icon = "▶ ●"
			}
		}

		modelCol := ""
		// Show model for active session
		if s.ChatID == m.activeSession && m.activeModel != "" {
			modelCol = m.activeModel
		}

		rows = append(rows, table.Row{icon, label, modelCol})
	}

	m.sidebarTable.SetRows(rows)
	m.sidebarTable.UpdateViewport()
	tableCursor := m.sidebarCursor
	if m.sidebarHoverRow >= 0 && !m.sidebarFocused { tableCursor = m.sidebarHoverRow }
	if tableCursor >= 0 && tableCursor < len(rows) { m.sidebarTable.SetCursor(tableCursor) }
}

// sidebarTableWidth returns the width of the sidebar table for layout.
func sidebarTableWidth(terminalWidth int) int {
	if terminalWidth < minSidebarScreenWidth {
		return 0
	}
	return sidebarWidth
}

// shouldShowSidebarTable returns true when sidebar table is visible.
func (m Model) shouldShowSidebarTable() bool {
	return m.showSidebar && m.width >= minSidebarScreenWidth && m.height >= minSidebarScreenHeight
}

// renderSidebarTable renders the sidebar using the table component.
func (m Model) renderSidebarTable() string {
	if len(m.sessions) == 0 {
		return lipgloss.NewStyle().
			Width(sidebarWidth).
			Padding(1, 2).
			Foreground(lipgloss.Color("243")).
			Render("(no sessions)")
	}

	// Build a styled container around the table
	title := m.styles.SidebarTitleStyle.Render("Aurelia") + "\n" +
		m.styles.SidebarMutedStyle.Render("local terminal")

	tableContent := m.sidebarTable.View()

	// Sidebar hints when focused
	var hints string
	if m.sidebarFocused {
		hints = fmt.Sprintf("\n%s\n%s\n%s\n%s\n%s\n%s",
			m.styles.SidebarMutedStyle.Render("↑↓ navigate"),
			m.styles.SidebarMutedStyle.Render("enter open"),
			m.styles.SidebarMutedStyle.Render("n new"),
			m.styles.SidebarMutedStyle.Render("r rename"),
			m.styles.SidebarMutedStyle.Render("d delete"),
			m.styles.SidebarMutedStyle.Render("esc exit"),
		)
	} else {
		hints = fmt.Sprintf("\n%s\n%s\n%s\n%s\n%s",
			m.styles.SidebarTitleStyle.Render("Project"),
			truncateMiddle(projectName(m.cwdPath), sidebarWidth-4),
			m.styles.SidebarMutedStyle.Render(truncateMiddle(m.cwdPath, sidebarWidth-4)),
			"",
			m.styles.SidebarTitleStyle.Render("Daemon")+"\n"+m.daemonLabel,
		)
		if m.isChatMode() {
			hints += "\n" + m.styles.ChatModeStyle.Render("(chat mode)")
		}
		hints += "\n" + m.styles.SidebarMutedStyle.Render("+ New session (click)")
	}

	return title + "\n" + tableContent + hints
}
