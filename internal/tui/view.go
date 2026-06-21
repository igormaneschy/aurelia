package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

const (
	// minViewportHeight is the minimum height for the chat viewport.
	minViewportHeight = 5
	// inputHeight is the height reserved for the bordered two-line textarea.
	inputHeight = 5
	// statusBarHeight is the height reserved for the status bar.
	statusBarHeight = 1
	// topMarginHeight keeps the TUI from feeling glued to the terminal chrome.
	topMarginHeight = 1
	// chatHeaderHeight is reserved for the active session/project header.
	chatHeaderHeight = 2
	// sidebarWidth is the width of the sidebar panel when visible.
	sidebarWidth = 24
	// minSidebarScreenWidth avoids crushing chat content on narrow terminals.
	minSidebarScreenWidth = 90
	// minSidebarScreenHeight keeps the sidebar from pushing input/status offscreen.
	minSidebarScreenHeight = 22
)

// All lipgloss.Style instances live in theme.go as a themeStyles struct on
// the Model. This keeps the view layer testable and theme-swappable; the
// palette constructors (newDarkStyles, newLightStyles) own the actual colors.

// View implements tea.Model.
func (m Model) View() string {
	switch m.state {
	case stateLoading:
		return m.loadingView()
	case stateChat:
		return m.chatView()
	case stateError:
		return m.errorView()
	}
	return ""
}

func (m Model) loadingView() string {
	if m.width == 0 || m.height == 0 {
		return "Aurelia TUI — connecting..."
	}

	body := lipgloss.Place(
		m.width, maxInt(minViewportHeight, m.height-inputHeight-statusBarHeight-topMarginHeight),
		lipgloss.Center, lipgloss.Center,
		fmt.Sprintf("%s Connecting to local daemon...", m.spinner.View()),
	)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderTopMargin(),
		body,
		m.renderInput(),
		m.renderStatusBar(),
	)
}

func (m Model) chatView() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	inputBar := m.renderInput()

	statusBar := m.renderStatusBar()

	viewHeight := m.height
	inputH := inputHeight
	statusH := statusBarHeight
	contentH := viewHeight - topMarginHeight - inputH - statusH

	if contentH < 1 {
		contentH = 1
	}

	mainContentHeight := contentH

	var body string
	if m.shouldShowSidebar() {
		sidebar := m.renderSidebar()
		viewContent := m.renderMainPane(mainContentHeight, m.contentWidth())
		body = lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.styles.SidebarStyle.Render(sidebar),
			viewContent,
		)
	} else {
		body = m.renderMainPane(mainContentHeight, m.width)
	}

	full := lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderTopMargin(),
		body,
		inputBar,
		statusBar,
	)

	// Overlay project panel when open.
	if m.projectPanelOpen {
		panel := m.renderProjectPanel()
		return m.overlayPanel(full, panel)
	}

	// Overlay help when ? is pressed.
	if m.helpOverlayOpen {
		panel := m.renderHelpOverlay()
		// Use a slightly wider panel for help to fit keybinding descriptions.
		return m.overlayPanelWide(full, panel)
	}

	return full
}

func (m Model) renderTopMargin() string {
	return strings.Repeat(" ", maxInt(1, m.width))
}

func (m Model) errorView() string {
	if m.width == 0 || m.height == 0 {
		return fmt.Sprintf("Error: %s\n\nPress Enter to retry or Ctrl+C to quit.", m.err)
	}

	errText := fmt.Sprintf("⚠️  Error\n\n%s\n\nPress Enter to retry or Ctrl+C to quit.", m.err)
	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		m.styles.ErrorStyle.Render(errText),
	)
}

func (m Model) renderMainPane(height, width int) string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderChatHeader(),
		m.renderMainContent(),
	)
	return lipgloss.NewStyle().Height(height).Width(width).Render(content)
}

func (m Model) renderMainContent() string {
	if !m.viewportSet || m.viewport.Height <= 0 {
		return "Initializing..."
	}
	return m.viewport.View()
}

func (m Model) renderInput() string {
	// Show pending image and document badges above the input.
	imageBadges := m.renderPendingImageBadges()
	attachmentBadges := m.renderPendingAttachmentBadges()

	// Combine badges in separate lines: image badges first, attachment badges after.
	var badgeLines []string
	if imageBadges != "" {
		badgeLines = append(badgeLines, imageBadges)
	}
	if attachmentBadges != "" {
		badgeLines = append(badgeLines, attachmentBadges)
	}
	if pendingBadge := m.renderPendingQueueBadge(); pendingBadge != "" {
		badgeLines = append(badgeLines, pendingBadge)
	}
	if autocomplete := m.renderAutocomplete(); autocomplete != "" {
		badgeLines = append(badgeLines, autocomplete)
	}

	promptText := "> "
	if m.waiting {
		promptText = "… "
	}
	prompt := m.styles.InputPromptStyle.Render(promptText)
	input := renderPromptedTextarea(prompt, promptText, m.textarea.View())
	// Lipgloss width is applied before border/padding, so leave enough room
	// to avoid terminal wrapping artifacts when toggling the sidebar.
	boxWidth := inputBoxContentWidth(m.width)
	style := m.styles.InputBoxStyle
	if m.waiting {
		style = m.styles.InputWaitingStyle
	}
	content := style.Width(boxWidth).Render(input)
	if len(badgeLines) > 0 {
		content = strings.Join(badgeLines, "\n") + "\n" + content
	}
	return content
}

func (m Model) renderAutocomplete() string {
	if len(m.autocompleteOptions) == 0 {
		return ""
	}
	var parts []string
	for i, option := range m.autocompleteOptions {
		label := option
		if i == m.autocompleteIndex {
			label = "▶ " + label
		}
		parts = append(parts, label)
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("111")).
		Render(strings.Join(parts, "  "))
}

func (m Model) renderPendingQueueBadge() string {
	count := m.pendingCount()
	if count == 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Render(fmt.Sprintf("⏳ %d pending", count))
}

// renderPendingImageBadges renders a line of image badges above the input.
func (m Model) renderPendingImageBadges() string {
	if len(m.pendingImages) == 0 {
		return ""
	}
	var names []string
	for _, img := range m.pendingImages {
		names = append(names, img.name)
	}
	badgeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Italic(true)
	return badgeStyle.Render(fmt.Sprintf("📎 %s", strings.Join(names, ", ")))
}

// renderPendingAttachmentBadges renders a line of document attachment badges
// above the input. Uses distinct styling (yellow) from image badges (grey).
func (m Model) renderPendingAttachmentBadges() string {
	if len(m.pendingAttachments) == 0 {
		return ""
	}
	var badges []string
	for _, att := range m.pendingAttachments {
		badges = append(badges, fmt.Sprintf("[📎 %s]", att.name))
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("226")).
		Render(strings.Join(badges, " "))
}

func renderPromptedTextarea(prompt, rawPrompt, text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return prompt
	}
	indent := strings.Repeat(" ", len(rawPrompt))
	lines[0] = prompt + lines[0]
	for i := 1; i < len(lines); i++ {
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderStatusBar() string {
	var state string
	switch m.chromeState() {
	case "connecting":
		state = m.styles.StatusBusyStyle.Render("● connecting")
	case "waiting":
		state = m.styles.StatusBusyStyle.Render("● waiting")
	case "error":
		state = m.styles.StatusErrorStyle.Render("● error")
	default:
		state = m.styles.StatusReadyStyle.Render("● ready")
	}

	// Items ordered by priority — less critical ones are dropped on narrow
	// terminals so the status bar never wraps to a second line.
	// min values increased to accommodate new fields (model, pending, elapsed).
	allParts := []statusBarItem{
		{label: state, min: 0},

		// Active model — shown when known.
		{label: m.activeModelLabel(), min: 14},

		// Pending count — shown when > 0.
		{label: m.pendingCountLabel(), min: 24},

		// Elapsed time — shown when waiting.
		{label: m.elapsedLabel(), min: 34},

		{label: "↵ send", min: 44},
		{label: fmt.Sprintf("%s newline", newlineFallbackKey), min: 62},
		{label: m.mouseStatusLabel(), min: 80},
		{label: "pg scroll", min: 94},
		{label: "esc cancel", min: 106},
		{label: "⌃L clear", min: 118},
		{label: "⌃P project", min: 130},
		{label: "⌃S/f2 sessions", min: 146},
		{label: "tab sidebar", min: 162},
		{label: "⌃C quit", min: 178},
	}

	parts := []string{}
	for _, item := range allParts {
		if item.label == "" {
			continue
		}
		if m.width >= item.min {
			parts = append(parts, item.label)
		}
	}

	return m.styles.StatusBarStyle.Width(maxInt(20, m.width-2)).Render(strings.Join(parts, "   ·   "))
}

// activeModelLabel returns the model label for the status bar.
// Returns empty string if no model is known.
func (m Model) activeModelLabel() string {
	if m.activeModel == "" {
		return ""
	}
	return m.activeModel
}

// pendingCountLabel returns the pending count badge for the status bar.
// Returns empty string when there are no pending messages.
func (m Model) pendingCountLabel() string {
	count := len(m.pendingQueue)
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("⏳ %d", count)
}

// elapsedLabel returns the elapsed time label for the status bar.
// Returns empty string when no turn is active.
func (m Model) elapsedLabel() string {
	if m.turnStart.IsZero() {
		return ""
	}
	elapsed := time.Since(m.turnStart)
	return elapsed.Truncate(time.Second).String()
}

func (m Model) mouseStatusLabel() string {
	if m.mouseEnabled {
		return "🖱️ mouse"
	}
	return "✋ mouse"
}

type statusBarItem struct {
	label string
	min   int
}

func (m Model) renderChatHeader() string {
	stateLabel := m.chromeState()
	if m.waiting {
		stateLabel = m.spinner.View() + " thinking"
	}

	// Session name in the header. Use the safe label to protect against
	// legacy stored names that may contain terminal-control characters.
	sessionName := "DM"
	for _, s := range m.sessions {
		if s.ChatID == m.activeSession {
			if s.ChatID != ipc.ReservedTUIChatID {
				sessionName = safeSessionLabel(s.Name)
			}
			break
		}
	}

	var projectPart string
	if m.isChatMode() {
		projectPart = m.styles.ChatModeStyle.Render("chat mode")
	} else {
		projectName := truncateMiddle(projectName(m.cwdPath), maxInt(12, m.contentWidth()/3))
		projectPart = m.styles.HeaderMetaStyle.Render("project " + projectName)
	}
	meta := fmt.Sprintf("%s   ·   daemon %s   ·   %s", projectPart, m.daemonLabel, stateLabel)
	header := lipgloss.JoinVertical(
		lipgloss.Left,
		m.styles.HeaderTitleStyle.Render("Aurelia / "+sessionName)+"  "+m.styles.HeaderMetaStyle.Render(meta),
		m.styles.HeaderRuleStyle.Render(strings.Repeat("─", maxInt(20, m.contentWidth()-2))),
	)
	return lipgloss.NewStyle().Width(m.contentWidth()).Render(header)
}

func (m Model) chromeState() string {
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

func (m Model) renderSidebar() string {
	lines := []string{
		m.styles.SidebarTitleStyle.Render("Aurelia"),
		m.styles.SidebarMutedStyle.Render("local terminal"),
		"",
		m.styles.SidebarTitleStyle.Render("Sessions"),
	}

	if len(m.sessions) == 0 {
		lines = append(lines, m.styles.SidebarMutedStyle.Render("  (no sessions)"))
	} else {
		for i, s := range m.sessions {
			label := safeSessionLabel(s.Name)
			if s.ChatID == ipc.ReservedTUIChatID {
				label = "DM"
			}

			// Determine display style.
			isActive := s.ChatID == m.activeSession
			isCursor := m.sidebarFocused && i == m.sidebarCursor

			var prefix string
			switch {
			case isActive && isCursor:
				prefix = "▶ ●"
				lines = append(lines, m.styles.SidebarActiveStyle.Render(prefix+" "+label))
			case isActive:
				prefix = "●"
				lines = append(lines, m.styles.SidebarActiveStyle.Render(prefix+" "+label))
			case isCursor:
				prefix = "▶"
				lines = append(lines, m.styles.SidebarCursorStyle.Render(prefix+" "+label))
			default:
				prefix = "○"
				lines = append(lines, m.styles.SidebarMutedStyle.Render(prefix+" "+label))
			}
		}
	}

	// Sidebar navigation hints when focused.
	if m.sidebarFocused {
		lines = append(lines, "",
			m.styles.SidebarMutedStyle.Render("↑↓ navigate"),
			m.styles.SidebarMutedStyle.Render("enter open"),
			m.styles.SidebarMutedStyle.Render("n new"),
			m.styles.SidebarMutedStyle.Render("d delete"),
			m.styles.SidebarMutedStyle.Render("esc exit"),
		)
	} else {
		lines = append(lines, "",
			m.styles.SidebarTitleStyle.Render("Project"),
			truncateMiddle(projectName(m.cwdPath), sidebarWidth-4),
			m.styles.SidebarMutedStyle.Render(truncateMiddle(m.cwdPath, sidebarWidth-4)),
		)
		if m.isChatMode() {
			lines = append(lines, m.styles.ChatModeStyle.Render("(chat mode)"))
		}
		lines = append(lines, "",
			m.styles.SidebarTitleStyle.Render("Daemon"),
			m.daemonLabel,
		)
	}

	return strings.Join(lines, "\n")
}

// getOrCreateRenderer returns a cached glamour renderer, creating a new one
// if the width has changed or no renderer exists yet.
func (m *Model) getOrCreateRenderer(width int) (*glamour.TermRenderer, error) {
	if m.glamourRenderer != nil && m.rendererWidth == width {
		return m.glamourRenderer, nil
	}

	contentWidth := width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	renderer, err := glamour.NewTermRenderer(
		// Avoid auto background detection here: it can ask the terminal for
		// OSC color reports, and some terminals echo the response back into
		// Bubble Tea input as text (for example, "11;rgb:...").
		glamour.WithStandardStyle(m.theme.GlamourStyle()),
		glamour.WithWordWrap(contentWidth),
	)
	if err != nil {
		return nil, err
	}

	m.glamourRenderer = renderer
	m.rendererWidth = width
	return renderer, nil
}

// renderMessages renders the chat messages using Glamour markdown rendering.
// Uses a cached renderer to avoid expensive re-creation on every call.
func (m *Model) renderMessages(messages []chatMessage, width int) string {
	if len(messages) == 0 {
		return renderEmptyState(width, m.styles)
	}

	var b strings.Builder

	renderer, err := m.getOrCreateRenderer(width)
	if err != nil {
		m.renderMessagesPlain(&b, messages)
		return b.String()
	}

	for i, msg := range messages {
		if i > 0 {
			b.WriteString("\n\n")
		}

		timestamp := msg.Timestamp.Format("15:04")

		switch msg.Sender {
		case "Igor":
			header := fmt.Sprintf("▶ Igor · %s", timestamp)
			b.WriteString(m.styles.UserStyle.Render(header))
			b.WriteString("\n")
			b.WriteString(m.styles.MessageSeparatorStyle.Render(strings.Repeat("─", maxInt(20, width-4))))
			b.WriteString("\n")
			b.WriteString(msg.Text)
		case "Aurelia":
			header := fmt.Sprintf("▶ Aurelia · %s", timestamp)
			b.WriteString(m.styles.AssistantStyle.Render(header))
			b.WriteString("\n")
			rendered, err := renderer.Render(msg.Text)
			if err != nil || rendered == "" {
				b.WriteString(msg.Text)
			} else {
				b.WriteString(strings.TrimSpace(rendered))
			}
		default:
			header := fmt.Sprintf("▶ %s · %s", msg.Sender, timestamp)
			b.WriteString(m.styles.ErrorStyle.Render(header))
			b.WriteString("\n")
			b.WriteString(msg.Text)
		}
	}

	return b.String()
}

// renderMessagesPlain renders messages without markdown (fallback).
func (m *Model) renderMessagesPlain(b *strings.Builder, messages []chatMessage) {
	for i, msg := range messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(b, "%s:\n%s", msg.Sender, msg.Text)
	}
}

// renderEmptyState returns a friendly welcome panel shown when the chat
// history is empty (initial connect or after Ctrl+L clear).
func renderEmptyState(width int, styles themeStyles) string {
	contentWidth := width - 8
	if contentWidth < 30 {
		contentWidth = 30
	}

	title := styles.HeaderTitleStyle.Render("Aurelia TUI")
	hint := styles.SidebarMutedStyle.Render(
		"Type a message or /help to start.\n" +
			"/cwd to set a project directory.",
	)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(1, 2).
		Width(contentWidth).
		Render(title + "\n\n" + hint)

	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(box)
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

// viewportForSize creates a viewport with the given dimensions.
func viewportForSize(width, height int) viewport.Model {
	vp := viewport.New(width, viewportHeightForTerminal(height))
	vp.YPosition = topMarginHeight
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3
	return vp
}

func inputBoxContentWidth(terminalWidth int) int {
	return maxInt(20, terminalWidth-6)
}

func inputTextareaWidth(terminalWidth int) int {
	return maxInt(10, inputBoxContentWidth(terminalWidth)-3)
}

func viewportHeightForTerminal(height int) int {
	available := height - viewBottomHeight(height)
	if available < minViewportHeight {
		return maxInt(1, available)
	}
	return available
}

// viewBottomHeight returns the height of non-viewport UI elements.
func viewBottomHeight(totalHeight int) int {
	return inputHeight + statusBarHeight + topMarginHeight + chatHeaderHeight
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncateMiddle(value string, width int) string {
	if width < 8 || len(value) <= width {
		return value
	}
	keep := (width - 1) / 2
	return value[:keep] + "…" + value[len(value)-keep:]
}

// renderProjectPanel renders the project state overlay panel.
func (m Model) renderProjectPanel() string {
	state := m.projectState

	var b strings.Builder
	b.WriteString(m.styles.HeaderTitleStyle.Render("Project State"))
	b.WriteString("\n\n")

	if state == nil {
		b.WriteString(m.styles.SidebarMutedStyle.Render("Loading..."))
		return b.String()
	}

	// CWD
	cwdDisplay := state.CWD
	if cwdDisplay == "" {
		cwdDisplay = m.styles.SidebarMutedStyle.Render("not set")
	} else {
		cwdDisplay = truncateMiddle(state.CWD, 50)
	}
	fmt.Fprintf(&b, "📂 Path: %s\n", cwdDisplay)

	// Binding source
	b.WriteString("Binding: ")
	switch state.BindingSource {
	case "manual":
		b.WriteString("manual")
	case "inherited":
		if state.BindingFrom != "" {
			fmt.Fprintf(&b, "inherited (from %s)", state.BindingFrom)
		} else {
			b.WriteString("inherited")
		}
	default:
		b.WriteString("none")
	}
	b.WriteString("\n")

	// Active agent
	fmt.Fprintf(&b, "🤖 Agent: %s\n", state.ActiveAgent)

	// Model
	fmt.Fprintf(&b, "⚙️ Model: %s\n", state.Model)

	// Bridge status
	bridgeLabel := state.BridgeStatus
	if bridgeLabel == "online" {
		bridgeLabel = m.styles.StatusReadyStyle.Render("online")
	} else {
		bridgeLabel = m.styles.StatusErrorStyle.Render("offline")
	}
	fmt.Fprintf(&b, "🧠 Bridge: %s\n", bridgeLabel)

	// Memory layers
	if len(state.MemoryLayers) > 0 {
		b.WriteString("\n")
		b.WriteString(m.styles.HeaderTitleStyle.Render("Memory"))
		b.WriteString("\n")
		for _, l := range state.MemoryLayers {
			icon := "◯"
			if l.Exists {
				icon = "🟢"
			}
			fmt.Fprintf(&b, " %s %s: %d files\n", icon, l.Name, l.FileCount)
		}
		fmt.Fprintf(&b, " Checkpoint target: %s\n", state.CheckpointLayer)
	} else {
		b.WriteString("\nMemory: unavailable\n")
	}

	// Latest run
	if state.LatestRun != nil {
		b.WriteString("\n")
		b.WriteString(m.styles.HeaderTitleStyle.Render("Latest Run"))
		b.WriteString("\n")
		fmt.Fprintf(&b, " Status: %s\n", state.LatestRun.Status)
		if state.LatestRun.AgentName != "" {
			fmt.Fprintf(&b, " Agent: %s\n", state.LatestRun.AgentName)
		}
		if state.LatestRun.Checkpoint != "" {
			fmt.Fprintf(&b, " Checkpoint: %s\n", state.LatestRun.Checkpoint)
		}
		fmt.Fprintf(&b, " Started: %s\n", state.LatestRun.StartedAt.Format("15:04 02/01/2006"))
		if state.LatestRun.DurationMs > 0 {
			fmt.Fprintf(&b, " Duration: %.1fs\n", float64(state.LatestRun.DurationMs)/1000)
		}
	}

	// Footer hint
	b.WriteString("\n")
	b.WriteString(m.styles.SidebarMutedStyle.Render("Ctrl+P to close"))

	return b.String()
}

// overlayPanel renders the full view with a centered panel overlay on top of
// the background chat view. The panel replaces background rows line-by-line so
// the chat remains visible above, below, and to the sides of the overlay.
func (m Model) overlayPanel(bg, panel string) string {
	panelWidth := maxInt(50, minInt(m.width-8, 70))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Padding(1, 2).
		Width(panelWidth).
		Render(panel)

	bgLines := strings.Split(bg, "\n")
	panelLines := strings.Split(box, "\n")

	// Vertically center the panel in the background.
	startRow := (len(bgLines) - len(panelLines)) / 2
	if startRow < 0 {
		startRow = 0
	}
	// Horizontally center the box.
	boxWidth := lipgloss.Width(box)
	startCol := (m.width - boxWidth) / 2
	if startCol < 0 {
		startCol = 0
	}

	var out []string
	for i, line := range bgLines {
		if i >= startRow && i-startRow < len(panelLines) {
			pl := panelLines[i-startRow]
			pw := lipgloss.Width(pl)
			// Build the overlay line: left padding + panel line + right padding.
			var sb strings.Builder
			if startCol > 0 {
				sb.WriteString(strings.Repeat(" ", startCol))
			}
			sb.WriteString(pl)
			rightPad := m.width - startCol - pw
			if rightPad > 0 {
				sb.WriteString(strings.Repeat(" ", rightPad))
			}
			out = append(out, sb.String())
		} else {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// safeSessionLabel strips terminal-control characters from a session name
// for safe display in the sidebar and chat header. This is a defensive
// measure: names newly created via sanitizeSessionName are always clean,
// but legacy stored names from before sanitization existed could contain
// ANSI escapes, newlines, tabs, or other control sequences.
func safeSessionLabel(name string) string {
	var b strings.Builder
	i := 0
	runes := []rune(name)
	for i < len(runes) {
		r := runes[i]
		// ESC (0x1B): skip the entire ANSI/OSC sequence. Must be checked
		// before the general C0 check below (since ESC IS a C0 code).
		if r == 0x1B {
			i++
			if i >= len(runes) {
				break
			}
			next := runes[i]
			switch next {
			case '[':
				// ANSI CSI: ESC [ params... intermediate... final byte
				i++ // skip '['
				for i < len(runes) {
					ch := runes[i]
					if (ch >= 0x30 && ch <= 0x3F) || (ch >= 0x20 && ch <= 0x2F) {
						i++
						continue
					}
					if (ch >= 0x40 && ch <= 0x7E) || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
						i++
						break
					}
					break
				}
			case ']':
				// OSC: ESC ] ... BEL (0x07) or ESC \
				i++ // skip ']'
				for i < len(runes) {
					if runes[i] == 0x07 {
						i++
						break
					}
					if runes[i] == 0x1B && i+1 < len(runes) && runes[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			default:
				// Other ESC sequence — skip this byte and continue.
			}
			continue
		}
		// Skip C0 (NUL..US, 0x00-0x1F), DEL (0x7F), C1 (U+0080-U+009F),
		// and U+FFFD (replacement char for invalid UTF-8 bytes).
		// ESC (0x1B) was handled above, so the effective C0 range is
		// 0x00-0x1A, 0x1C-0x1F.
		if r <= 0x1F || r == 0x7F || r == 0xFFFD || (r >= 0x80 && r <= 0x9F) {
			i++
			continue
		}
		b.WriteRune(r)
		i++
	}
	return b.String()
}

// renderHelpOverlay renders the keyboard shortcuts and commands reference.
func (m Model) renderHelpOverlay() string {
	var b strings.Builder
	b.WriteString(m.styles.HeaderTitleStyle.Render("Keyboard Shortcuts"))
	b.WriteString("\n\n")

	// Two-column layout: keybinding | description.
	rows := [][2]string{
		{"Esc", "Cancel current response"},
		{"Enter", "Send message"},
		{"Alt+Enter / Ctrl+J", "Insert newline"},
		{"Ctrl+O", "Toggle mouse (scroll vs text selection)"},
		{"Ctrl+P", "Toggle project state panel"},
		{"Ctrl+S / F2", "Focus sidebar sessions"},
		{"Ctrl+L", "Clear chat screen"},
		{"Ctrl+Y", "Copy transcript to clipboard"},
		{"Ctrl+R", "Copy last response to clipboard"},
		{"Ctrl+X", "Clear pending images/docs"},
		{"Ctrl+V", "Paste image from clipboard"},
		{"Ctrl+C", "Quit"},
		{"? / Esc / Enter", "Close this help"},
		{"", ""},
		{"↑↓", "Navigate input history"},
		{"Tab", "Complete command or cycle sidebar"},
	}

	for _, row := range rows {
		if row[0] == "" {
			b.WriteString("\n")
			continue
		}
		keyCol := m.styles.UserStyle.Width(22).Render(row[0])
		descCol := m.styles.HeaderMetaStyle.Render(row[1])
		b.WriteString(keyCol)
		b.WriteString("  ")
		b.WriteString(descCol)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.styles.HeaderTitleStyle.Render("Commands"))
	b.WriteString("\n\n")

	cmds := [][2]string{
		{"/help", "Show this help"},
		{"/status", "Daemon, model, cwd, session status"},
		{"/model", "List available models"},
		{"/model <name>", "Switch model"},
		{"/model auto", "Use automatic model selection"},
		{"/model refresh", "Refresh model list"},
		{"/cwd", "Show current project binding"},
		{"/cwd <path>", "Set project working directory"},
		{"/cwd clear", "Remove project binding"},
		{"/img <path>", "Attach image (png, jpg, gif, webp)"},
		{"/attach <path>", "Attach document (md, docx, pdf, etc.)"},
	}

	for _, row := range cmds {
		keyCol := m.styles.UserStyle.Width(22).Render(row[0])
		descCol := m.styles.HeaderMetaStyle.Render(row[1])
		b.WriteString(keyCol)
		b.WriteString("  ")
		b.WriteString(descCol)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.styles.HeaderMetaStyle.Render("Images: /img <path>, Ctrl+V paste, drag & drop"))
	b.WriteString("\n")
	b.WriteString(m.styles.HeaderMetaStyle.Render("Docs: /attach <path>, multiple allowed per message"))

	return b.String()
}

// overlayPanelWide overlays a panel on the background using a wider panel
// (70-80 columns) suitable for help content.
func (m Model) overlayPanelWide(bg, panel string) string {
	panelWidth := maxInt(50, minInt(m.width-4, 76))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.HeaderTitleStyle.GetForeground()).
		Padding(1, 2).
		Width(panelWidth).
		Render(panel)

	bgLines := strings.Split(bg, "\n")
	panelLines := strings.Split(box, "\n")

	startRow := (len(bgLines) - len(panelLines)) / 2
	if startRow < 0 {
		startRow = 0
	}
	boxWidth := lipgloss.Width(box)
	startCol := (m.width - boxWidth) / 2
	if startCol < 0 {
		startCol = 0
	}

	var out []string
	for i, line := range bgLines {
		if i >= startRow && i-startRow < len(panelLines) {
			pl := panelLines[i-startRow]
			pw := lipgloss.Width(pl)
			var sb strings.Builder
			if startCol > 0 {
				sb.WriteString(strings.Repeat(" ", startCol))
			}
			sb.WriteString(pl)
			rightPad := m.width - startCol - pw
			if rightPad > 0 {
				sb.WriteString(strings.Repeat(" ", rightPad))
			}
			out = append(out, sb.String())
		} else {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
