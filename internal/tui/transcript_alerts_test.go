package tui

import (
	"strings"
	"testing"
	"time"
)

func TestSplitAlertLines(t *testing.T) {
	text := "Alerta: Telegram \"entry confirmed\"\n\n### Resumo\n\nbody"
	alerts, rest := splitAlertLines(text)
	if len(alerts) != 1 || alerts[0] != `Alerta: Telegram "entry confirmed"` {
		t.Fatalf("alerts = %#v", alerts)
	}
	if !strings.Contains(rest, "### Resumo") {
		t.Fatalf("rest = %q", rest)
	}
	if strings.Contains(rest, "Alerta:") {
		t.Fatal("alert should be removed from markdown body")
	}
}

func TestIsAlertLine_English(t *testing.T) {
	if !isAlertLine("Alert: broker disconnected") {
		t.Fatal("expected english alert prefix")
	}
}

func TestRenderMessages_HighlightsAlertLine(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 100
	text := "Alerta: Telegram \"entry confirmed\"\n\n### Resumo Visual"
	out := stripANSIForTest(m.renderMessages([]chatMessage{
		{Sender: "Aurelia", Text: text, Timestamp: time.Now()},
	}, m.contentWidth()))

	if !strings.Contains(out, "Alerta:") {
		t.Fatalf("expected alert text in output, got:\n%s", out)
	}
	if strings.Count(out, "Alerta:") != 1 {
		t.Fatalf("alert should appear once (chip only), got:\n%s", out)
	}
	if !strings.Contains(out, "Resumo Visual") {
		t.Fatalf("expected markdown body, got:\n%s", out)
	}
}
