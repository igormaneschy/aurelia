package pipeline

import (
	"strings"
	"testing"
)

func TestSummarizeToolInput_Bash(t *testing.T) {
	got := SummarizeToolInput("bash", map[string]any{
		"command": "go test ./internal/tui/... -short",
	})
	if got != "go test ./internal/tui/... -short" {
		t.Fatalf("got %q", got)
	}
}

func TestSummarizeToolInput_WritePath(t *testing.T) {
	got := SummarizeToolInput("write", map[string]any{
		"file_path": "/Users/igor/aurelia/internal/tui/tool_activity.go",
	})
	if got != "tool_activity.go" {
		t.Fatalf("got %q", got)
	}
}

func TestSummarizeToolInput_Grep(t *testing.T) {
	got := SummarizeToolInput("grep", map[string]any{
		"pattern": "ReportTool",
		"path":    "internal/pipeline",
	})
	if got != "ReportTool in pipeline" {
		t.Fatalf("got %q", got)
	}
}

func TestSummarizeToolInput_RedactsSecrets(t *testing.T) {
	got := SummarizeToolInput("bash", map[string]any{
		"command": "export TOKEN=sk-ant-secret123456789012345678 && ./run.sh",
	})
	if strings.Contains(got, "sk-ant") {
		t.Fatalf("expected redacted command, got %q", got)
	}
}