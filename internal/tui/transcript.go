package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
)

// transcriptModel owns chat history, the scrollable viewport, streaming buffer,
// and markdown rendering cache.
type transcriptModel struct {
	messages []chatMessage

	viewport    viewport.Model
	viewportSet bool

	streamBuf string

	glamourRenderer *glamour.TermRenderer
	rendererWidth   int
}

// appendOrUpdateAureliaMessage appends a new Aurelia message or updates the
// last one if it's already an Aurelia message (for streaming).
func (m *Model) appendOrUpdateAureliaMessage(text string) {
	now := time.Now()
	if len(m.messages) > 0 {
		last := &m.messages[len(m.messages)-1]
		if last.Sender == "Aurelia" {
			last.Text = text
			last.Timestamp = now
			return
		}
	}
	m.messages = append(m.messages, chatMessage{
		Sender:    "Aurelia",
		Text:      text,
		Timestamp: now,
	})
}

// ensureViewport lazily initializes the viewport if dimensions were stored
// during loading but the viewport hasn't been created yet.
func (m *Model) ensureViewport() {
	if m.width > 0 && m.height > 0 && !m.viewportSet {
		contentWidth := m.contentWidth()
		m.viewport = viewportForSize(contentWidth, m.height)
		m.viewportSet = true
		m.viewport.SetContent(m.renderMessages(m.messages, contentWidth))
		m.viewport.GotoBottom()
	}
}

// updateViewport refreshes the viewport content if initialized.
// Auto-scrolls to bottom only when the user is already at the bottom,
// so streaming does not yank the viewport while reading earlier messages.
func (m *Model) updateViewport() {
	m.ensureViewport()
	if m.viewportSet && m.viewport.Height() > 0 {
		contentWidth := m.contentWidth()
		followBottom := m.viewport.AtBottom()
		prevOffset := m.viewport.YOffset()
		m.viewport.SetWidth(contentWidth)
		m.viewport.SetContent(m.renderMessages(m.messages, contentWidth))
		if followBottom {
			m.viewport.GotoBottom()
		} else {
			m.viewport.SetYOffset(prevOffset)
		}
	}
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

		showDate := shouldShowMessageDate(messages, i)
		timestamp := formatMessageTime(msg.Timestamp, showDate)

		switch msg.Sender {
		case "Igor":
			header := formatMessageHeader("Igor", timestamp)
			b.WriteString(m.styles.UserStyle.Render(header))
			b.WriteString("\n")
			b.WriteString(m.styles.MessageSeparatorStyle.Render(strings.Repeat("─", maxInt(20, width-4))))
			b.WriteString("\n")
			b.WriteString(msg.Text)
			b.WriteString("\n")
		case "Aurelia":
			header := formatMessageHeader("Aurelia", timestamp)
			b.WriteString(m.styles.AssistantStyle.Render(header))
			b.WriteString("\n")
			rendered, err := renderer.Render(msg.Text)
			if err != nil || rendered == "" {
				b.WriteString(msg.Text)
			} else {
				b.WriteString(strings.TrimSpace(rendered))
			}
			b.WriteString("\n")
		default:
			header := formatMessageHeader(msg.Sender, timestamp)
			b.WriteString(m.styles.ErrorStyle.Render(header))
			b.WriteString("\n")
			b.WriteString(msg.Text)
			b.WriteString("\n")
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