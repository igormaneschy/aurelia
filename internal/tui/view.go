package tui

import (
	"fmt"
	"strings"

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

var (
	userStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))

	assistantStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("39"))

	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1)

	inputWaitingStyle = inputBoxStyle.
				BorderForeground(lipgloss.Color("205"))

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)

	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1).
			Width(sidebarWidth)

	sidebarTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205"))

	sidebarMutedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("244"))

	sidebarActiveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)

	sidebarCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("226")).
				Bold(true)

	headerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205"))

	headerMetaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	headerRuleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238"))

	messageSeparatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("238"))

	statusReadyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	statusBusyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	statusErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	// chatModeStyle highlights that file system tools are disabled.
	chatModeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")). // amber
			Italic(true)
)

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
			sidebarStyle.Render(sidebar),
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
		errorStyle.Render(errText),
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
	// Show pending image badges above the input.
	badges := m.renderPendingImageBadges()

	promptText := "> "
	if m.waiting {
		promptText = "… "
	}
	prompt := inputPromptStyle.Render(promptText)
	input := renderPromptedTextarea(prompt, promptText, m.textarea.View())
	// Lipgloss width is applied before border/padding, so leave enough room
	// to avoid terminal wrapping artifacts when toggling the sidebar.
	boxWidth := inputBoxContentWidth(m.width)
	style := inputBoxStyle
	if m.waiting {
		style = inputWaitingStyle
	}
	content := style.Width(boxWidth).Render(input)
	if badges != "" {
		content = badges + "\n" + content
	}
	return content
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
		state = statusBusyStyle.Render("● connecting")
	case "waiting":
		state = statusBusyStyle.Render("● waiting")
	case "error":
		state = statusErrorStyle.Render("● error")
	default:
		state = statusReadyStyle.Render("● ready")
	}

	// Items ordered by priority — less critical ones are dropped on narrow
	// terminals so the status bar never wraps to a second line.
	// Thresholds account for: content width = m.width - 4 (Width - 2, Padding - 2).
	// Cumulative visible widths: state(7) +sep(7)+ send(6) +sep(7)+ newline(17)
	// +sep(7)+ scroll(9) +sep(7)+ esc(10) +sep(7)+ clear(8) +sep(7)+ tab(11) +sep(7)+ quit(7).
	allParts := []statusBarItem{
		{label: state, min: 0},
		{label: "↵ send", min: 24},
		{label: fmt.Sprintf("%s newline", newlineFallbackKey), min: 48},
		{label: "pg scroll", min: 64},
		{label: "esc cancel", min: 82},
		{label: "⌃L clear", min: 98},
		{label: "⌃P project", min: 114},
		{label: "⌃S/f2 sessions", min: 136},
		{label: "tab sidebar", min: 156},
		{label: "⌃C quit", min: 172},
	}

	parts := []string{}
	for _, item := range allParts {
		if m.width >= item.min {
			parts = append(parts, item.label)
		}
	}

	return statusBarStyle.Width(maxInt(20, m.width-2)).Render(strings.Join(parts, "   ·   "))
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
		projectPart = chatModeStyle.Render("chat mode")
	} else {
		projectName := truncateMiddle(projectName(m.cwdPath), maxInt(12, m.contentWidth()/3))
		projectPart = headerMetaStyle.Render("project " + projectName)
	}
	meta := fmt.Sprintf("%s   ·   daemon %s   ·   %s", projectPart, m.daemonLabel, stateLabel)
	header := lipgloss.JoinVertical(
		lipgloss.Left,
		headerTitleStyle.Render("Aurelia / "+sessionName)+"  "+headerMetaStyle.Render(meta),
		headerRuleStyle.Render(strings.Repeat("─", maxInt(20, m.contentWidth()-2))),
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
		sidebarTitleStyle.Render("Aurelia"),
		sidebarMutedStyle.Render("local terminal"),
		"",
		sidebarTitleStyle.Render("Sessions"),
	}

	if len(m.sessions) == 0 {
		lines = append(lines, sidebarMutedStyle.Render("  (no sessions)"))
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
				lines = append(lines, sidebarActiveStyle.Render(prefix+" "+label))
			case isActive:
				prefix = "●"
				lines = append(lines, sidebarActiveStyle.Render(prefix+" "+label))
			case isCursor:
				prefix = "▶"
				lines = append(lines, sidebarCursorStyle.Render(prefix+" "+label))
			default:
				prefix = "○"
				lines = append(lines, sidebarMutedStyle.Render(prefix+" "+label))
			}
		}
	}

	// Sidebar navigation hints when focused.
	if m.sidebarFocused {
		lines = append(lines, "",
			sidebarMutedStyle.Render("↑↓ navigate"),
			sidebarMutedStyle.Render("enter open"),
			sidebarMutedStyle.Render("n new"),
			sidebarMutedStyle.Render("d delete"),
			sidebarMutedStyle.Render("esc exit"),
		)
	} else {
		lines = append(lines, "",
			sidebarTitleStyle.Render("Project"),
			truncateMiddle(projectName(m.cwdPath), sidebarWidth-4),
			sidebarMutedStyle.Render(truncateMiddle(m.cwdPath, sidebarWidth-4)),
		)
		if m.isChatMode() {
			lines = append(lines, chatModeStyle.Render("(chat mode)"))
		}
		lines = append(lines, "",
			sidebarTitleStyle.Render("Daemon"),
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
		glamour.WithStandardStyle("dark"),
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
		return renderEmptyState(width)
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
			b.WriteString(userStyle.Render(header))
			b.WriteString("\n")
			b.WriteString(messageSeparatorStyle.Render(strings.Repeat("─", maxInt(20, width-4))))
			b.WriteString("\n")
			b.WriteString(msg.Text)
		case "Aurelia":
			header := fmt.Sprintf("▶ Aurelia · %s", timestamp)
			b.WriteString(assistantStyle.Render(header))
			b.WriteString("\n")
			rendered, err := renderer.Render(msg.Text)
			if err != nil || rendered == "" {
				b.WriteString(msg.Text)
			} else {
				b.WriteString(strings.TrimSpace(rendered))
			}
		default:
			header := fmt.Sprintf("▶ %s · %s", msg.Sender, timestamp)
			b.WriteString(errorStyle.Render(header))
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
func renderEmptyState(width int) string {
	contentWidth := width - 8
	if contentWidth < 30 {
		contentWidth = 30
	}

	title := headerTitleStyle.Render("Aurelia TUI")
	hint := sidebarMutedStyle.Render(
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
	b.WriteString(headerTitleStyle.Render("Project State"))
	b.WriteString("\n\n")

	if state == nil {
		b.WriteString(sidebarMutedStyle.Render("Loading..."))
		return b.String()
	}

	// CWD
	cwdDisplay := state.CWD
	if cwdDisplay == "" {
		cwdDisplay = sidebarMutedStyle.Render("not set")
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
		bridgeLabel = statusReadyStyle.Render("online")
	} else {
		bridgeLabel = statusErrorStyle.Render("offline")
	}
	fmt.Fprintf(&b, "🧠 Bridge: %s\n", bridgeLabel)

	// Memory layers
	if len(state.MemoryLayers) > 0 {
		b.WriteString("\n")
		b.WriteString(headerTitleStyle.Render("Memory"))
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
		b.WriteString(headerTitleStyle.Render("Latest Run"))
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
	b.WriteString(sidebarMutedStyle.Render("Ctrl+P to close"))

	return b.String()
}

// overlayPanel renders the full view with a centered panel overlay.
// Uses lipgloss.Place for correct ANSI-aware centering.
func (m Model) overlayPanel(view, panel string) string {
	panelWidth := maxInt(50, minInt(m.width-8, 70))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Padding(1, 2).
		Width(panelWidth).
		Render(panel)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
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
