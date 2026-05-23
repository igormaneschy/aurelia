package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// UpdateTasksStatus updates a tasks.md file, marking checkboxes for tasks
// whose Status is TaskApproved. Tasks with other statuses (failed, skipped,
// unverified, escalated) are not marked as done.
//
// The caller should ensure the tasks.md exists before calling this function.
// If the file does not exist, an error is returned.
func UpdateTasksStatus(tasksPath string, results []TaskResult) error {
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("tasks.md not found at %s: %w", tasksPath, err)
		}
		return fmt.Errorf("reading tasks.md: %w", err)
	}

	content := string(data)
	resultMap := make(map[string]TaskResult)
	for _, r := range results {
		resultMap[r.TaskID] = r
	}

	lines := strings.Split(content, "\n")
	var currentTask string

	for i, line := range lines {
		// Detect task header (### T1: ..., ### T2: ...)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			// Extract task ID from header (e.g., "### T1: Create interface" → "T1")
			header := strings.TrimPrefix(trimmed, "### ")
			if colonIdx := strings.Index(header, ":"); colonIdx > 0 {
				currentTask = strings.TrimSpace(header[:colonIdx])
			}
		}

		// Only mark checkboxes for TaskApproved tasks
		if currentTask != "" {
			if r, ok := resultMap[currentTask]; ok && r.Status == TaskApproved {
				if strings.Contains(line, "- [ ]") {
					lines[i] = strings.Replace(line, "- [ ]", "- [x]", 1)
				}
			}
		}
	}

	// Append status summary at the end
	var sb strings.Builder
	sb.WriteString("\n\n---\n\n## Execution Status\n\n")
	for _, r := range results {
		status := formatTaskStatus(r.Status)
		if r.Status == TaskApproved {
			fmt.Fprintf(&sb, "- **%s**: %s (%.0fms)\n", r.TaskID, status, float64(r.DurationMs))
		} else {
			if r.Error != "" {
				fmt.Fprintf(&sb, "- **%s**: %s — %s (%.0fms)\n", r.TaskID, status, r.Error, float64(r.DurationMs))
			} else {
				fmt.Fprintf(&sb, "- **%s**: %s (%.0fms)\n", r.TaskID, status, float64(r.DurationMs))
			}
		}
	}

	updated := strings.Join(lines, "\n") + sb.String()
	return os.WriteFile(tasksPath, []byte(updated), 0o644)
}

func formatTaskStatus(s TaskStatus) string {
	switch s {
	case TaskApproved:
		return "✅ Approved"
	case TaskFailed:
		return "❌ Failed"
	case TaskSkipped:
		return "⏭️ Skipped"
	case TaskUnverified:
		return "⚠️ Unverified"
	case TaskEscalated:
		return "🚨 Escalated"
	case TaskPending:
		return "⏳ Pending"
	case TaskRunning:
		return "⚙️ Running"
	default:
		return string(s)
	}
}
