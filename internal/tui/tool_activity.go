package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	maxToolSummaryDisplay   = 56
	toolPostDoneDisplay     = 2 * time.Second
)

// toolInfo represents an active tool execution during a streaming response.
type toolInfo struct {
	Name   string // Bash, Read, Write, Edit, Grep, etc.
	Detail string // short summary (command, path, pattern)
	Done   bool
	doneAt time.Time
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

// toolActivityDisplay picks the tool line to show and how many earlier steps
// collapsed into +N done. A just-finished tool stays visible briefly before
// collapsing unless a newer tool is already running.
func toolActivityDisplay(tools []toolInfo, now time.Time) (show *toolInfo, active bool, doneCount int) {
	if len(tools) == 0 {
		return nil, false, 0
	}

	showIdx := -1
	for i := len(tools) - 1; i >= 0; i-- {
		if !tools[i].Done {
			showIdx = i
			active = true
			break
		}
	}
	if showIdx < 0 {
		for i := len(tools) - 1; i >= 0; i-- {
			t := &tools[i]
			if t.Done && !t.doneAt.IsZero() && now.Sub(t.doneAt) < toolPostDoneDisplay {
				showIdx = i
				break
			}
		}
	}
	if showIdx >= 0 {
		show = &tools[showIdx]
	}
	for i, t := range tools {
		if !t.Done || i == showIdx {
			continue
		}
		doneCount++
	}
	return show, active, doneCount
}

func formatToolDoneBadge(styles themeStyles, doneCount int) string {
	if doneCount <= 0 {
		return ""
	}
	return styles.SidebarMutedStyle.Render(fmt.Sprintf("+%d done", doneCount))
}

func joinToolActivityLine(styles themeStyles, width int, line, badge string) string {
	if badge == "" {
		return line
	}
	if line == "" {
		return badge
	}
	gap := width - lipgloss.Width(line) - lipgloss.Width(badge)
	if gap < 2 {
		return line + "  " + badge
	}
	return line + strings.Repeat(" ", gap) + badge
}

// renderToolActivity renders a compact activity panel above the composer.
// The active (or just-finished) tool is the primary line; earlier steps
// collapse into a right-aligned +N done badge on the same row.
func (m Model) renderToolActivity() string {
	if len(m.activeTools) == 0 {
		return ""
	}

	show, active, doneCount := toolActivityDisplay(m.activeTools, time.Now())
	width := m.composerColumnWidth()
	badge := formatToolDoneBadge(m.styles, doneCount)

	var line string
	if show != nil {
		line = formatToolActivityLine(m.styles, *show, active)
	}

	content := joinToolActivityLine(m.styles, width, line, badge)
	if content == "" {
		return ""
	}

	return lipgloss.NewStyle().Width(width).Render(content)
}

func toolActivityRefreshCmd() tea.Cmd {
	return tea.Tick(toolPostDoneDisplay, func(time.Time) tea.Msg {
		return toolActivityTickMsg{}
	})
}