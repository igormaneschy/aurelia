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