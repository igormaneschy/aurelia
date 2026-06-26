package tui

import (
	"strings"
	"testing"
)

func TestParseToolChunk(t *testing.T) {
	tests := []struct {
		body    string
		want    string
		wantOK  bool
	}{
		{"🔧 Bash...", "Bash", true},
		{"\n🔧 Read...\n", "Read", true},
		{"  🔧 Grep...  ", "Grep", true},
		{"Hello world", "", false},
		{"🔧 ", "", false},
		{"🔧 ...", "", false},
		{"not a tool", "", false},
	}
	for _, tt := range tests {
		got, ok := parseToolChunk(tt.body)
		if ok != tt.wantOK || got != tt.want {
			t.Errorf("parseToolChunk(%q) = (%q, %v), want (%q, %v)", tt.body, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestToolIcon(t *testing.T) {
	if got := toolIcon("Bash"); got != "⚡" {
		t.Errorf("toolIcon(Bash) = %q, want ⚡", got)
	}
	if got := toolIcon("Unknown"); got != "🔧" {
		t.Errorf("toolIcon(Unknown) = %q, want 🔧", got)
	}
}

func TestRenderToolActivity_Empty(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	if got := m.renderToolActivity(); got != "" {
		t.Errorf("expected empty tool activity, got %q", got)
	}
}

func TestRenderToolActivity_ShowsTools(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 80
	m.activeTools = []toolInfo{{Name: "Bash", Detail: "Bash"}}
	got := m.renderToolActivity()
	if got == "" {
		t.Fatal("expected non-empty tool activity panel")
	}
	if !strings.Contains(got, "⚡") || !strings.Contains(got, "Bash") {
		t.Errorf("expected Bash tool line, got %q", got)
	}
}