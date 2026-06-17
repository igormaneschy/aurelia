package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
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

	inputWaitingStyle = inputBoxStyle.Copy().
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

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderTopMargin(),
		body,
		inputBar,
		statusBar,
	)
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
	return style.Width(boxWidth).Render(input)
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
	chromeState := m.chromeState()
	state := statusReadyStyle.Render("● ready")
	if chromeState == "connecting" {
		state = statusBusyStyle.Render("● connecting")
	} else if chromeState == "waiting" {
		state = statusBusyStyle.Render("● waiting")
	} else if chromeState == "error" {
		state = statusErrorStyle.Render("● error")
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
		{label: "⌃L clear", min: 96},
		{label: "tab sidebar", min: 114},
		{label: "⌃C quit", min: 128},
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
	project := truncateMiddle(projectName(m.cwdPath), maxInt(12, m.contentWidth()/3))
	stateLabel := m.chromeState()
	if m.waiting {
		stateLabel = m.spinner.View() + " thinking"
	}
	meta := fmt.Sprintf("project %s   ·   daemon %s   ·   %s", project, m.daemonLabel, stateLabel)
	header := lipgloss.JoinVertical(
		lipgloss.Left,
		headerTitleStyle.Render("Aurelia / DM")+"  "+headerMetaStyle.Render(meta),
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
		sidebarTitleStyle.Render("Session"),
		sidebarActiveStyle.Render("● DM"),
		sidebarMutedStyle.Render("  direct chat"),
		"",
		sidebarTitleStyle.Render("Project"),
		truncateMiddle(projectName(m.cwdPath), sidebarWidth-4),
		sidebarMutedStyle.Render(truncateMiddle(m.cwdPath, sidebarWidth-4)),
		"",
		sidebarTitleStyle.Render("Daemon"),
		m.daemonLabel,
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
		b.WriteString(fmt.Sprintf("%s:\n%s", msg.Sender, msg.Text))
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

func truncateMiddle(value string, width int) string {
	if width < 8 || len(value) <= width {
		return value
	}
	keep := (width - 1) / 2
	return value[:keep] + "…" + value[len(value)-keep:]
}
