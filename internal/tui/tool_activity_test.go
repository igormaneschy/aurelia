package tui

import (
	"strings"
	"testing"
)

func TestParseToolChunk(t *testing.T) {
	tests := []struct {
		body       string
		wantName   string
		wantDetail string
		wantOK     bool
	}{
		{"🔧 Bash...", "Bash", "", true},
		{"🔧 Bash|go test ./...", "Bash", "go test ./...", true},
		{"\n🔧 Read|internal/tui/view.go\n", "Read", "internal/tui/view.go", true},
		{"  🔧 Grep|pattern  ", "Grep", "pattern", true},
		{"Hello world", "", "", false},
		{"🔧 ", "", "", false},
		{"🔧 ...", "", "", false},
	}
	for _, tt := range tests {
		gotName, gotDetail, ok := parseToolChunk(tt.body)
		if ok != tt.wantOK || gotName != tt.wantName || gotDetail != tt.wantDetail {
			t.Errorf("parseToolChunk(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.body, gotName, gotDetail, ok, tt.wantName, tt.wantDetail, tt.wantOK)
		}
	}
}

func TestParseToolDone(t *testing.T) {
	if !parseToolDone("\n✅ tool_done\n") {
		t.Fatal("expected tool_done marker")
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

func TestToolActivityDisplay_ActiveTool(t *testing.T) {
	tools := []toolInfo{
		{Name: "Bash", Detail: "go test", Done: true},
		{Name: "Write", Detail: "wrap.go"},
	}
	show, active, done := toolActivityDisplay(tools)
	if show == nil || show.Name != "Write" || !active || done != 1 {
		t.Fatalf("got show=%v active=%v done=%d", show, active, done)
	}
}

func TestToolActivityDisplay_LastDoneStaysVisible(t *testing.T) {
	tools := []toolInfo{
		{Name: "Bash", Detail: "go test", Done: true},
	}
	show, active, done := toolActivityDisplay(tools)
	if show == nil || show.Name != "Bash" || active || done != 0 {
		t.Fatalf("expected last done tool to stay visible, got show=%v active=%v done=%d", show, active, done)
	}
}

func TestToolActivityDisplay_OnlyCounterWhenNoLine(t *testing.T) {
	// When all tools are done and collapsed into counter only — not applicable
	// with current model; the last tool always shows. Multiple done tools:
	tools := []toolInfo{
		{Name: "Bash", Detail: "go test", Done: true},
		{Name: "Write", Detail: "wrap.go", Done: true},
	}
	show, active, done := toolActivityDisplay(tools)
	if show == nil || show.Name != "Write" || active || done != 1 {
		t.Fatalf("expected last done tool visible with prior collapsed, got show=%v active=%v done=%d", show, active, done)
	}
}

func TestRenderToolActivity_Empty(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	if got := m.renderToolActivity(); got != "" {
		t.Errorf("expected empty tool activity, got %q", got)
	}
}

func TestRenderToolActivity_ShowsSummary(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 120
	m.activeTools = []toolInfo{{Name: "Bash", Detail: "go test ./internal/tui/..."}}
	got := stripANSIForTest(m.renderToolActivity())
	if strings.Contains(got, "Running") {
		t.Fatalf("unexpected Running header, got %q", got)
	}
	if !strings.Contains(got, "bash") || !strings.Contains(got, "go test") {
		t.Errorf("expected bash summary, got %q", got)
	}
}

func TestRenderToolActivity_CollapsesDoneTools(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 120
	m.activeTools = []toolInfo{
		{Name: "Bash", Detail: "go test ./...", Done: true},
		{Name: "Read", Detail: "view.go", Done: true},
		{Name: "Write", Detail: "tool_activity.go"},
	}
	got := stripANSIForTest(m.renderToolActivity())
	if !strings.Contains(got, "+2 done") {
		t.Fatalf("expected collapsed done count, got %q", got)
	}
	if strings.Count(got, "bash") != 0 || strings.Count(got, "read") != 0 {
		t.Fatalf("expected only active tool detail, got %q", got)
	}
	if !strings.Contains(got, "write") || !strings.Contains(got, "tool_activity.go") {
		t.Fatalf("expected active write summary, got %q", got)
	}
	idxWrite := strings.Index(got, "write")
	idxDone := strings.Index(got, "+2 done")
	if idxWrite < 0 || idxDone < 0 || idxWrite > idxDone {
		t.Fatalf("tool summary should appear before +N done on the same line, got %q", got)
	}
}

func TestRenderToolActivity_LastDoneShowsCheckmark(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 120
	m.activeTools = []toolInfo{
		{Name: "Write", Detail: "wrap.go", Done: true},
	}
	got := stripANSIForTest(m.renderToolActivity())
	if !strings.Contains(got, "write") || !strings.Contains(got, "wrap.go") {
		t.Fatalf("expected finished tool to remain visible, got %q", got)
	}
	if strings.Contains(got, "+1 done") {
		t.Fatalf("expected no counter for single finished tool, got %q", got)
	}
}

func TestAlignToChatColumn_IndentsToolActivity(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.showSidebar = true
	m.sessions = []tuiSessionInfo{{ChatID: -2, Name: "DM"}}
	m.activeTools = []toolInfo{{Name: "Write", Detail: "tool_activity.go"}}
	m.ensureViewport()

	layout := m.renderChatBaseLayout()
	lines := strings.Split(layout, "\n")

	var toolLine string
	for _, line := range lines {
		plain := stripANSIForTest(line)
		if strings.Contains(plain, "write") && strings.Contains(plain, "tool_activity.go") {
			toolLine = line
			break
		}
	}
	if toolLine == "" {
		t.Fatalf("expected tool activity line in layout, got last lines:\n%s", strings.Join(lines[maxInt(0, len(lines)-8):], "\n"))
	}
	idx := strings.Index(stripANSIForTest(toolLine), "✏️")
	if idx < 0 {
		idx = strings.Index(stripANSIForTest(toolLine), "write")
	}
	if idx < m.mainPaneStartX()-2 {
		t.Fatalf("tool line should start in chat column (idx=%d mainPaneStartX=%d): %q", idx, m.mainPaneStartX(), stripANSIForTest(toolLine))
	}
}