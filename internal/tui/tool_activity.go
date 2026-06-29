package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const maxToolSummaryDisplay = 56

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

func countDoneTools(tools []toolInfo) int {
	n := 0
	for _, t := range tools {
		if t.Done {
			n++
		}
	}
	return n
}

func renderToolActivityHeader(styles themeStyles, doneCount int) string {
	header := styles.SidebarSectionStyle.Render("Running")
	if doneCount > 0 {
		badge := styles.SidebarMutedStyle.Render(fmt.Sprintf("+%d done", doneCount))
		header += "  " + badge
	}
	return header
}

// renderToolActivity renders a compact activity panel above the composer.
// Only the active tool is shown in detail; completed steps collapse into +N done.
func (m Model) renderToolActivity() string {
	if len(m.activeTools) == 0 {
		return ""
	}

	doneCount := countDoneTools(m.activeTools)
	last := m.activeTools[len(m.activeTools)-1]

	var body strings.Builder
	body.WriteString(renderToolActivityHeader(m.styles, doneCount))
	if !last.Done {
		body.WriteString("\n")
		body.WriteString(formatToolActivityLine(m.styles, last, true))
	}

	return lipgloss.NewStyle().
		Width(m.composerColumnWidth()).
		Render(body.String())
}