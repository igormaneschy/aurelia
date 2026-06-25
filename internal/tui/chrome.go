package tui

import (
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"

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
	sessions        []tuiSessionInfo
	sidebarCursor   int
	sidebarHoverRow int // hovered session row from mouse motion; -1 = none

	spinner spinner.Model

	helpModel help.Model
	projectPanelOpen bool
	projectState     *ipc.ProjectStatePayload

	daemonLabel    string
	cwdPath        string
	connectLatency time.Duration
	activeModel    string
	turnStart      time.Time
	mouseEnabled   bool
	streamProgress streamProgress

	styles themeStyles
	theme  Theme
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
	return "✋ mouse"
}