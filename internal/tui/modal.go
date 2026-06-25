package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

const (
	modalMinWidth     = 50
	modalMaxWidth     = 70
	modalWideMaxWidth = 76
	modalHMargin      = 8
	modalVMargin      = 2
)

// modalOpen reports whether a full-screen modal owns the foreground.
func (m Model) modalOpen() bool {
	return m.formOpen || m.helpVisible() || m.projectPanelOpen
}

// openHelpOverlay opens the keyboard-shortcuts help modal.
func (m Model) openHelpOverlay() Model {
	m.helpModel.ShowAll = true
	return m
}

// closeHelpOverlay closes the help modal.
func (m Model) closeHelpOverlay() Model {
	m.helpModel.ShowAll = false
	return m
}

// toggleProjectPanel opens or closes the project state modal.
func (m Model) toggleProjectPanel() (Model, tea.Cmd) {
	m.projectPanelOpen = !m.projectPanelOpen
	if m.projectPanelOpen {
		return m, projectPanelOpenCmd(m.ipcClient, m.activeSession)
	}
	return m, nil
}

// openProjectPanel opens the project state modal (no-op when already open).
func (m Model) openProjectPanel() (Model, tea.Cmd) {
	if m.projectPanelOpen {
		return m, nil
	}
	m.projectPanelOpen = true
	return m, projectPanelOpenCmd(m.ipcClient, m.activeSession)
}

// closeProjectPanel closes the project state modal.
func (m Model) closeProjectPanel() Model {
	m.projectPanelOpen = false
	return m
}

func projectPanelOpenCmd(client *ipc.Client, chatID int64) tea.Cmd {
	return tea.Batch(
		fetchTUIProjectState(client, chatID),
		scheduleProjectStatePoll(),
	)
}

// renderModalOverlay dims the chat layout and centers a bordered panel.
func (m Model) renderModalOverlay(bg, panel string, wide bool) string {
	maxW := modalMaxWidth
	if wide {
		maxW = modalWideMaxWidth
	}
	panelWidth := maxInt(modalMinWidth, minInt(m.width-modalHMargin, maxW))
	panel = m.clipModalPanel(panel, panelWidth)

	borderColor := m.styles.HeaderTitleStyle.GetForeground()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 2).
		Width(panelWidth).
		Render(panel)

	return m.overlayCentered(m.dimBackground(bg), box)
}

func (m Model) dimBackground(bg string) string {
	dim := m.styles.SidebarMutedStyle
	lines := strings.Split(bg, "\n")
	for i, line := range lines {
		if strings.TrimSpace(stripANSI(line)) == "" {
			lines[i] = strings.Repeat(" ", m.width)
			continue
		}
		lines[i] = dim.Render(line)
	}
	return strings.Join(lines, "\n")
}

func (m Model) clipModalPanel(panel string, width int) string {
	maxLines := m.height - modalVMargin*2
	if maxLines < 8 {
		maxLines = 8
	}
	lines := strings.Split(panel, "\n")
	if len(lines) <= maxLines {
		return panel
	}
	keep := maxLines - 1
	if keep < 1 {
		keep = 1
	}
	lines = lines[:keep]
	lines = append(lines, m.styles.SidebarMutedStyle.Render("… (scroll terminal taller for more)"))
	return strings.Join(lines, "\n")
}

// overlayCentered composites a bordered box over a dimmed background.
func (m Model) overlayCentered(bg, box string) string {
	bgLines := strings.Split(bg, "\n")
	panelLines := strings.Split(box, "\n")

	startRow := (len(bgLines) - len(panelLines)) / 2
	if startRow < modalVMargin {
		startRow = modalVMargin
	}
	boxWidth := lipgloss.Width(box)
	startCol := (m.width - boxWidth) / 2
	if startCol < 0 {
		startCol = 0
	}

	var out []string
	for i, line := range bgLines {
		if i >= startRow && i-startRow < len(panelLines) {
			out = append(out, compositeOverlayRow(line, panelLines[i-startRow], startCol, m.width))
		} else {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}