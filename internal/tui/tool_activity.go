package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const maxToolActivityLines = 4

// toolInfo represents an active tool execution during a streaming response.
type toolInfo struct {
	Name   string // Bash, Read, Write, Edit, Grep, etc.
	Detail string // display label (usually same as Name)
	Done   bool   // true when tool_result received (reserved for future use)
}

// parseToolChunk detects tool activity indicators in stream chunks.
// The daemon sends "\n🔧 ToolName...\n" as stream_chunk text.
func parseToolChunk(body string) (string, bool) {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "🔧 ") {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(trimmed, "🔧 "), "...")
	if name == "" {
		return "", false
	}
	return name, true
}

// toolIcon returns an emoji icon for the given tool name.
func toolIcon(name string) string {
	switch name {
	case "Bash":
		return "⚡"
	case "Read":
		return "📖"
	case "Write":
		return "✏️"
	case "Edit":
		return "📝"
	case "Grep":
		return "🔍"
	case "Glob":
		return "🌐"
	case "LS", "List":
		return "📂"
	default:
		return "🔧"
	}
}

// renderToolActivity renders the tool execution activity panel shown during
// streaming responses. Each active tool gets a compact line with icon + name.
func (m Model) renderToolActivity() string {
	if len(m.activeTools) == 0 {
		return ""
	}

	tools := m.activeTools
	if len(tools) > maxToolActivityLines {
		tools = tools[len(tools)-maxToolActivityLines:]
	}

	var lines []string
	for _, t := range tools {
		icon := toolIcon(t.Name)
		marker := "…"
		if t.Done {
			marker = "✓"
		}
		lines = append(lines, fmt.Sprintf(" %s %s %s", icon, t.Detail, marker))
	}

	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Width(inputBoxContentWidth(m.width)).
		MaxHeight(maxToolActivityLines)

	return style.Render(strings.Join(lines, "\n"))
}