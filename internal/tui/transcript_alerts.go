package tui

import (
	"strings"
)

// isAlertLine reports whether a transcript line is a system alert (e.g. Telegram
// notifications) that should be highlighted separately from markdown body text.
func isAlertLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(lower, "alerta:") || strings.HasPrefix(lower, "alert:")
}

// splitAlertLines separates alert lines from the rest of a message body.
func splitAlertLines(text string) (alerts []string, rest string) {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && isAlertLine(trimmed) {
			alerts = append(alerts, trimmed)
			continue
		}
		kept = append(kept, line)
	}
	rest = strings.TrimLeft(strings.Join(kept, "\n"), "\n")
	return alerts, rest
}

func (m Model) renderAlertLine(alert string) string {
	label := alert
	if idx := strings.Index(label, ":"); idx >= 0 {
		prefix := strings.TrimSpace(label[:idx+1])
		body := strings.TrimSpace(label[idx+1:])
		if body != "" {
			label = prefix + " " + body
		}
	}
	return m.styles.AlertChipStyle.Render("📢 " + label)
}
