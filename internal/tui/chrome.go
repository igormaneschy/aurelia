package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// chromeModel owns terminal layout, sidebar chrome, status bar data, overlays,
// and the visual theme palette.
type chromeModel struct {
	width  int
	height int

	showSidebar     bool
	sidebarFocused  bool
	sidebarTable    table.Model
	sessions         []tuiSessionInfo
	sessionUnread    map[int64]int
	sessionSeenCount map[int64]int
	sidebarCursor    int
	sidebarHoverRow int // hovered session row from mouse motion; -1 = none
	statusBarHover  statusBarHitKind

	spinner spinner.Model

	helpModel  help.Model
	activeForm *huhForm
	formOpen   bool

	projectPanelOpen bool
	projectState     *ipc.ProjectStatePayload

	daemonLabel    string
	cwdPath        string
	connectLatency time.Duration
	activeModel    string
	turnStart      time.Time
	mouseEnabled      bool
	noMouse           bool
	sessionFlashUntil time.Time
	streamProgress    streamProgress
	attachProgress    attachProgress
	animations        animState
	activeTools       []toolInfo

	styles      themeStyles
	theme       Theme
	transparent bool
}

// shouldShowSidebar returns true when sidebar is enabled and the terminal is
// wide enough to keep the chat readable.
func (m Model) shouldShowSidebar() bool {
	return m.showSidebar && m.width >= minSidebarScreenWidth && m.height >= minSidebarScreenHeight
}

// contentWidth returns the chat viewport width after optional sidebar space.
func (m Model) contentWidth() int {
	if m.shouldShowSidebar() {
		return maxInt(40, m.width-sidebarWidth-5)
	}
	return maxInt(40, m.width)
}

// composerColumnWidth is the width available for input, tool activity, and
// footer chrome in the main chat column.
func (m Model) composerColumnWidth() int {
	if m.shouldShowSidebar() {
		return maxInt(20, m.contentWidth())
	}
	return inputBoxContentWidth(m.width)
}

// sidebarColumnWidth returns the rendered outer width of the sidebar panel.
func (m Model) sidebarColumnWidth() int {
	if !m.shouldShowSidebar() {
		return 0
	}
	rendered := m.styles.SidebarStyle.Render(m.renderSidebarTable())
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 || lines[0] == "" {
		return sidebarWidth + 4
	}
	return lipgloss.Width(lines[0])
}

// alignToChatColumn places footer content in the main chat column when the
// sidebar is visible, so tool activity does not appear under sidebar Actions.
func (m Model) alignToChatColumn(content string) string {
	if content == "" || !m.shouldShowSidebar() {
		return content
	}
	spacer := lipgloss.NewStyle().Width(m.sidebarColumnWidth()).Render("")
	column := lipgloss.NewStyle().Width(m.composerColumnWidth()).Render(content)
	return lipgloss.JoinHorizontal(lipgloss.Top, spacer, column)
}

func (m Model) chromeState() string {
	if m.daemonLabel == "offline" {
		return "offline"
	}
	if m.waiting {
		return "waiting"
	}
	if m.state == stateLoading {
		return "connecting"
	}
	if m.state == stateError {
		return "error"
	}
	return "ready"
}

// activeModelLabel returns the model label for the status bar.
// Returns empty string if no model is known.
func (m Model) activeModelLabel() string {
	if m.activeModel == "" {
		return ""
	}
	return m.activeModel
}

func (m Model) mouseStatusLabel() string {
	if m.mouseEnabled {
		return "🖱️ mouse"
	}
	if m.noMouse {
		return "✋ mouse"
	}
	return "✋ mouse (Ctrl+O)"
}