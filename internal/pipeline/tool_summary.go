package pipeline

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

const maxToolInputSummaryRunes = 56

// SummarizeToolInput builds a short, redacted label for live progress UIs.
func SummarizeToolInput(toolName string, input any) string {
	m := toolInputMap(input)
	if len(m) == 0 {
		return ""
	}

	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "bash":
		return summarizeCommand(stringField(m, "command"))
	case "read", "write", "edit":
		return summarizeToolPath(stringField(m, "file_path", "path"))
	case "grep":
		pattern := truncateToolSummary(stringField(m, "pattern"))
		if pattern == "" {
			return ""
		}
		if path := summarizeToolPath(stringField(m, "path", "file_path")); path != "" {
			return pattern + " in " + path
		}
		return pattern
	case "glob":
		return truncateToolSummary(stringField(m, "pattern", "glob_pattern"))
	case "list", "ls":
		return summarizeToolPath(stringField(m, "path"))
	default:
		return summarizeGenericToolInput(m)
	}
}

func toolInputMap(input any) map[string]any {
	if input == nil {
		return nil
	}
	if m, ok := input.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}

func stringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if s := strings.TrimSpace(anyString(v)); s != "" {
				return s
			}
		}
	}
	return ""
}

func anyString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}

func summarizeToolPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = strings.ReplaceAll(path, "\\", "/")
	path = redactSecrets(path)
	base := filepath.Base(path)
	if base == "." || base == "/" || base == "" {
		return truncateToolSummary(path)
	}
	if len([]rune(base)) <= maxToolInputSummaryRunes {
		return base
	}
	return truncateToolSummary(base)
}

func summarizeCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	command = strings.Join(strings.Fields(command), " ")
	command = redactSecrets(command)
	return truncateToolSummary(command)
}

func summarizeGenericToolInput(m map[string]any) string {
	priority := []string{"query", "url", "pattern", "file_path", "path", "command", "description", "name"}
	for _, key := range priority {
		if s := truncateToolSummary(stringField(m, key)); s != "" {
			return s
		}
	}
	for _, v := range m {
		if s := truncateToolSummary(anyString(v)); s != "" {
			return s
		}
	}
	return ""
}

func truncateToolSummary(s string) string {
	s = strings.TrimSpace(redactSecrets(s))
	if s == "" {
		return ""
	}
	return truncateRunes(s, maxToolInputSummaryRunes)
}