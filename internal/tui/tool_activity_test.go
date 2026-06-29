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
	got := stripANSIForTest(m.renderToolActivity())
	if got == "" {
		t.Fatal("expected non-empty tool activity panel")
	}
	if !strings.Contains(got, "Running") {
		t.Errorf("expected Running label, got %q", got)
	}
	if !strings.Contains(got, "⚡") || !strings.Contains(got, "bash") {
		t.Errorf("expected bash tool chip, got %q", got)
	}
}

func TestAlignToChatColumn_IndentsToolActivity(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.showSidebar = true
	m.sessions = []tuiSessionInfo{{ChatID: -2, Name: "DM"}}
	m.activeTools = []toolInfo{{Name: "Write", Detail: "Write"}}
	m.ensureViewport()

	layout := m.renderChatBaseLayout()
	lines := strings.Split(layout, "\n")
	sidebarW := m.sidebarColumnWidth()

	var toolLine string
	for _, line := range lines {
		plain := stripANSIForTest(line)
		if strings.Contains(plain, "Running") && strings.Contains(plain, "write") {
			toolLine = line
			break
		}
	}
	if toolLine == "" {
		t.Fatalf("expected tool activity line in layout, got last lines:\n%s", strings.Join(lines[maxInt(0, len(lines)-8):], "\n"))
	}
	idx := strings.Index(stripANSIForTest(toolLine), "Running")
	if idx < m.mainPaneStartX()-2 {
		t.Fatalf("tool line should start in chat column (idx=%d mainPaneStartX=%d sidebarW=%d): %q", idx, m.mainPaneStartX(), sidebarW, stripANSIForTest(toolLine))
	}
}