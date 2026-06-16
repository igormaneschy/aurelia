package tui

import (
	"path/filepath"
	"strings"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// cwdFromEvents extracts the project cwd from daemon status/cwd responses.
func cwdFromEvents(events []ipc.IPCEvent) string {
	for _, event := range events {
		if event.Type != ipc.EventTypeMessage || event.Body == "" {
			continue
		}
		if cwd := cwdFromText(event.Body); cwd != "" {
			return cwd
		}
	}
	return ""
}

// cwdFromText extracts a markdown-backticked cwd from known daemon messages.
func cwdFromText(text string) string {
	for _, marker := range []string{"📂 CWD:", "📂 Path:", "Project set to:"} {
		if cwd := valueAfterMarker(text, marker); cwd != "" {
			return cwd
		}
	}
	if strings.Contains(text, "Project binding removed") || strings.Contains(text, "No project set") {
		return "not set"
	}
	return ""
}

// projectName returns a short display label for a cwd path.
func projectName(cwd string) string {
	if cwd == "" || cwd == "not set" {
		return "no project"
	}
	base := filepath.Base(cwd)
	if base == "." || base == string(filepath.Separator) {
		return cwd
	}
	return base
}

func valueAfterMarker(text, marker string) string {
	idx := strings.Index(text, marker)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(marker):]
	start := strings.Index(rest, "`")
	if start < 0 {
		return ""
	}
	rest = rest[start+1:]
	end := strings.Index(rest, "`")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}
