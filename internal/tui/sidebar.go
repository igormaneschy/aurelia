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

// newSidebarTable creates a table.Model for the session sidebar.
func newSidebarTable() table.Model {
	cols := []table.Column{
		{Title: "", Width: 3},   // icon
		{Title: "Session", Width: 14},
		{Title: "Model", Width: 0}, // flex
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(false),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("243")).Bold(true)
	s.Selected = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("255")).Background(lipgloss.Color("57"))
	s.Cell = lipgloss.NewStyle().Padding(0, 1)
	t.SetStyles(s)

	return t
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
	if m.sidebarCursor >= 0 && m.sidebarCursor < len(rows) {
		m.sidebarTable.SetCursor(m.sidebarCursor)
	}
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
		m.styles.SidebarMutedStyle.Render("local terminal") + "\n" +
		m.styles.SidebarTitleStyle.Render("Sessions")

	tableContent := m.sidebarTable.View()

	// Sidebar hints when focused
	var hints string
	if m.sidebarFocused {
		hints = fmt.Sprintf("\n%s\n%s\n%s\n%s\n%s\n%s\n%s",
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
	}

	return title + "\n" + tableContent + hints
}
