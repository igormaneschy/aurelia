package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const maxToolActivityChips = 3

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

func formatToolChip(styles themeStyles, t toolInfo, active bool) string {
	label := fmt.Sprintf("%s %s", toolIcon(t.Name), strings.ToLower(t.Name))
	if active && !t.Done {
		return styles.StatusBusyStyle.Render(label + " …")
	}
	if t.Done {
		return styles.ChipStyle.Render(label + " ✓")
	}
	return styles.ChipStyle.Render(label)
}

// renderToolActivity renders a compact tool strip above the composer during
// streaming. It stays in the chat column and reads as activity, not sidebar
// actions.
func (m Model) renderToolActivity() string {
	if len(m.activeTools) == 0 {
		return ""
	}

	tools := m.activeTools
	if len(tools) > maxToolActivityChips {
		tools = tools[len(tools)-maxToolActivityChips:]
	}

	chips := make([]string, len(tools))
	for i, t := range tools {
		chips[i] = formatToolChip(m.styles, t, i == len(tools)-1)
	}

	label := m.styles.SidebarSectionStyle.Render("Running")
	line := label + "  " + strings.Join(chips, "  ")

	return lipgloss.NewStyle().
		Width(m.composerColumnWidth()).
		Render(line)
}