package tui

import (
	"strings"
	"testing"
)

func TestRenderChatHeader_ShowsChipsNotDaemon(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 40
	m.cwdPath = "/Users/igor/dev/aurelia"
	m.daemonLabel = "ready"
	m.activeModel = "deepseek-v4"

	header := stripANSIForTest(m.renderChatHeader())

	for _, want := range []string{"Aurelia / DM", "deepseek-v4", "ready"} {
		if !strings.Contains(header, want) {
			t.Errorf("expected header to contain %q, got %q", want, header)
		}
	}
	for _, absent := range []string{"daemon", "project aurelia"} {
		if strings.Contains(header, absent) {
			t.Errorf("expected header NOT to contain %q, got %q", absent, header)
		}
	}
}

func TestRenderChatHeader_OfflineHealthChip(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 40
	m.daemonLabel = "offline"

	header := stripANSIForTest(m.renderChatHeader())
	if !strings.Contains(header, "offline") {
		t.Errorf("expected offline in header, got %q", header)
	}
	if strings.Contains(header, "daemon") {
		t.Errorf("expected no daemon label, got %q", header)
	}
}

func TestDecorativeHeaderRule_NoCorruptUTF8(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 100
	m.height = 30
	rule := stripANSIForTest(m.decorativeHeaderRule(40))
	for _, r := range rule {
		if r != '░' && r != '▒' && r != '▓' && r != '─' {
			t.Fatalf("unexpected rune %U in rule %q", r, rule)
		}
	}
}

func TestRenderChatHeader_MetaRightAligned(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 40
	m.daemonLabel = "ready"
	m.activeModel = "deepseek-v4-flash"

	line := stripANSIForTest(strings.Split(m.renderChatHeader(), "\n")[0])
	titleIdx := strings.Index(line, "Aurelia /")
	modelIdx := strings.Index(line, "deepseek-v4-flash")
	if titleIdx < 0 || modelIdx < 0 || modelIdx <= titleIdx+len("Aurelia / DM") {
		t.Fatalf("expected model chip to the right of title, got %q", line)
	}
}

func TestRenderChatHeader_ChatModeChip(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 40
	m.daemonLabel = "ready"

	header := stripANSIForTest(m.renderChatHeader())
	if !strings.Contains(header, "chat mode") {
		t.Errorf("expected chat mode chip, got %q", header)
	}
}