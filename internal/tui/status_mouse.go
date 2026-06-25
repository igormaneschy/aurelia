package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type statusBarHitKind int

const (
	statusBarHitNone statusBarHitKind = iota
	statusBarHitModel
)

func (m Model) statusBarY() int {
	if m.height <= 0 {
		return -1
	}
	return m.height - 1
}

func (m Model) statusBarHit(x int) statusBarHitKind {
	if x < 0 || m.width <= 0 {
		return statusBarHitNone
	}
	regions := m.statusBarRegions()
	for _, r := range regions {
		if x >= r.start && x < r.end {
			return r.kind
		}
	}
	return statusBarHitNone
}

type statusBarRegion struct {
	kind  statusBarHitKind
	start int
	end   int
}

func (m Model) statusBarRegions() []statusBarRegion {
	var regions []statusBarRegion
	pos := 0
	sep := "   ·   "

	addPart := func(label string, kind statusBarHitKind, clickable bool) {
		if label == "" {
			return
		}
		if pos > 0 {
			pos += len(sep)
		}
		start := pos
		pos += len(stripANSI(label))
		if clickable && kind != statusBarHitNone {
			regions = append(regions, statusBarRegion{kind: kind, start: start, end: pos})
		}
	}

	// Mirror renderStatusBar priority filtering.
	var state string
	switch m.chromeState() {
	case "connecting":
		state = "● connecting"
	case "waiting":
		state = "● waiting"
	case "error":
		state = "● error"
	case "offline":
		state = "● offline"
	default:
		state = "● ready"
	}

	allParts := []struct {
		label       string
		min         int
		kind        statusBarHitKind
		clickable   bool
	}{
		{state, 0, statusBarHitNone, false},
		{m.activeModelLabel(), 14, statusBarHitModel, true},
		{m.pendingCountLabel(), 24, statusBarHitNone, false},
		{m.historyNav.pageLabel(), 28, statusBarHitNone, false},
		{m.elapsedLabel(), 34, statusBarHitNone, false},
		{"↵ send", 44, statusBarHitNone, false},
		{newlineFallbackKey + " newline", 62, statusBarHitNone, false},
		{m.mouseStatusLabel(), 80, statusBarHitNone, false},
	}

	for _, item := range allParts {
		if item.label == "" {
			continue
		}
		if m.width >= item.min {
			addPart(item.label, item.kind, item.clickable)
		}
	}
	return regions
}

func (m Model) headerProjectHit(x, y int) bool {
	if y != topMarginHeight+1 || m.isChatMode() {
		return false
	}
	// Title line: "Aurelia / Session  project Foo   ·   ..."
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
	// Rough hit: click anywhere on project segment (after "project ").
	start := idx
	if x < start || x >= len(plain) {
		return false
	}
	return true
}

func (m Model) sidebarNewSessionHit(x, y int) bool {
	if !m.shouldShowSidebar() || m.sidebarFocused || len(m.sessions) == 0 {
		return false
	}
	if !sidebarMouseHitX(x) {
		return false
	}
	// "+ New session (click)" is the last hint row below the table.
	offset := 8 // blank, project block, daemon block
	if m.isChatMode() {
		offset++
	}
	hintY := sidebarTableFirstRowY() + m.sidebarTable.Height() + offset
	return y == hintY || y == hintY+1
}

func (m Model) sidebarProjectHit(x, y int) bool {
	if !m.shouldShowSidebar() || m.sidebarFocused || m.isChatMode() {
		return false
	}
	if !sidebarMouseHitX(x) {
		return false
	}
	// Project path line in sidebar hints (below "Project" title).
	pathY := sidebarTableFirstRowY() + m.sidebarTable.Height() + sidebarTitleLines + 1
	return y == pathY
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
		if msg.Y == m.statusBarY() {
			switch m.statusBarHit(msg.X) {
			case statusBarHitModel:
				next, c := m.openModelSelect()
				return true, next, c
			}
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