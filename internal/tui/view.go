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

	mainContent := m.renderMainContent()

	inputBar := m.renderInput()

	statusBar := m.renderStatusBar()

	viewHeight := m.height
	inputH := inputHeight
	statusH := statusBarHeight
	contentH := viewHeight - topMarginHeight - inputH - statusH

	if contentH < minViewportHeight {
		contentH = minViewportHeight
	}

	mainContentHeight := contentH

	var body string
	if m.shouldShowSidebar() {
		sidebar := m.renderSidebar()
		viewContent := lipgloss.NewStyle().Height(mainContentHeight).Width(m.contentWidth()).Render(mainContent)
		body = lipgloss.JoinHorizontal(
			lipgloss.Top,
			sidebarStyle.Render(sidebar),
			viewContent,
		)
	} else {
		body = lipgloss.NewStyle().Height(mainContentHeight).Width(m.width).Render(mainContent)
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
	// Lipgloss width is applied before border/padding, so leave enough room
	// to avoid terminal wrapping artifacts when toggling the sidebar.
	boxWidth := maxInt(20, m.width-6)
	return inputBoxStyle.Width(boxWidth).Render(prompt + m.textarea.View())
}

func (m Model) renderStatusBar() string {
	state := "ready"
	if m.state == stateLoading {
		state = "connecting"
	} else if m.waiting {
		state = "waiting"
	}
	parts := []string{
		"● " + state,
		"↵ send",
		fmt.Sprintf("%s newline", newlineFallbackKey),
		"⌃L clear",
		"tab sidebar",
		"⌃C quit",
	}
	return statusBarStyle.Width(maxInt(20, m.width-2)).Render(strings.Join(parts, "   ·   "))
}

func (m Model) renderSidebar() string {
	lines := []string{
		sidebarTitleStyle.Render("Aurelia"),
		sidebarMutedStyle.Render("local TUI"),
		"",
		sidebarTitleStyle.Render("Session"),
		sidebarActiveStyle.Render("● DM"),
		sidebarMutedStyle.Render("  direct channel"),
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
		return ""
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
		header := fmt.Sprintf("%s [%s]", msg.Sender, timestamp)

		switch msg.Sender {
		case "Igor":
			b.WriteString(userStyle.Render(header))
		case "Aurelia":
			b.WriteString(assistantStyle.Render(header))
		default:
			b.WriteString(errorStyle.Render(header))
		}

		b.WriteString("\n")

		if msg.Sender == "Aurelia" {
			rendered, err := renderer.Render(msg.Text)
			if err != nil || rendered == "" {
				b.WriteString(msg.Text)
			} else {
				b.WriteString(strings.TrimSpace(rendered))
			}
		} else {
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
	vp := viewport.New(width, maxInt(minViewportHeight, height-viewBottomHeight(height)))
	vp.YPosition = topMarginHeight
	return vp
}

// viewBottomHeight returns the height of non-viewport UI elements.
func viewBottomHeight(totalHeight int) int {
	return inputHeight + statusBarHeight + topMarginHeight
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
