package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	maxToolActivityLines  = 3
	maxToolSummaryDisplay = 48
)

// toolInfo represents an active tool execution during a streaming response.
type toolInfo struct {
	Name   string // Bash, Read, Write, Edit, Grep, etc.
	Detail string // short summary (command, path, pattern)
	Done   bool
}

// parseToolChunk detects tool activity indicators in stream chunks.
// Format: "🔧 ToolName" or "🔧 ToolName|summary".
func parseToolChunk(body string) (name, detail string, ok bool) {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "🔧 ") {
		return "", "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "🔧 "))
	if rest == "" {
		return "", "", false
	}
	if idx := strings.Index(rest, "|"); idx >= 0 {
		name = strings.TrimSpace(rest[:idx])
		detail = strings.TrimSpace(rest[idx+1:])
		return name, detail, name != ""
	}
	rest = strings.TrimSuffix(rest, "...")
	if rest == "" {
		return "", "", false
	}
	return rest, "", true
}

func parseToolDone(body string) bool {
	return strings.TrimSpace(body) == "✅ tool_done"
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

func formatToolActivityLine(styles themeStyles, t toolInfo, active bool) string {
	summary := strings.TrimSpace(t.Detail)
	if summary == "" {
		summary = strings.ToLower(t.Name)
	}
	summary = truncateMiddle(summary, maxToolSummaryDisplay)

	var line string
	if strings.EqualFold(summary, t.Name) || strings.EqualFold(summary, strings.ToLower(t.Name)) {
		line = fmt.Sprintf(" %s %s", toolIcon(t.Name), summary)
	} else {
		line = fmt.Sprintf(" %s %s · %s", toolIcon(t.Name), strings.ToLower(t.Name), summary)
	}

	switch {
	case active && !t.Done:
		return styles.StatusBusyStyle.Render(line + " …")
	case t.Done:
		return styles.ChipStyle.Render(line + " ✓")
	default:
		return styles.ChipStyle.Render(line)
	}
}

// renderToolActivity renders a compact activity panel above the composer.
func (m Model) renderToolActivity() string {
	if len(m.activeTools) == 0 {
		return ""
	}

	tools := m.activeTools
	if len(tools) > maxToolActivityLines {
		tools = tools[len(tools)-maxToolActivityLines:]
	}

	lines := make([]string, len(tools))
	for i, t := range tools {
		lines[i] = formatToolActivityLine(m.styles, t, i == len(tools)-1)
	}

	header := m.styles.SidebarSectionStyle.Render("Running")
	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Width(m.composerColumnWidth()).
		Render(header + "\n" + body)
}