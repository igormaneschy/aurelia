package tui

import (
	"strings"
	"testing"
)

func TestCompositeOverlayRow_PreservesLateralBackground(t *testing.T) {
	bgLine := strings.Repeat("A", 20) + strings.Repeat("B", 20) + strings.Repeat("C", 20)
	panelLine := "PANEL"
	startCol := 20
	width := 60

	got := compositeOverlayRow(bgLine, panelLine, startCol, width)
	plain := stripANSIForTest(got)

	if !strings.HasPrefix(plain, strings.Repeat("A", 20)) {
		t.Fatalf("expected left background preserved, got prefix %q", plain[:minInt(25, len(plain))])
	}
	if !strings.Contains(plain, "PANEL") {
		t.Fatal("expected panel content in overlay row")
	}
	if !strings.HasSuffix(plain, strings.Repeat("C", 20)) {
		t.Fatalf("expected right background preserved, got suffix %q", plain[maxInt(0, len(plain)-25):])
	}
}

func TestOverlayPanel_PreservesLateralBackground(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 60
	m.height = 10

	// Enough background rows for the bordered panel to center vertically.
	bgLines := make([]string, m.height)
	for i := range bgLines {
		bgLines[i] = strings.Repeat("L", 60)
	}
	out := m.overlayPanel(strings.Join(bgLines, "\n"), "TEST PANEL")
	lines := strings.Split(out, "\n")

	var overlayLine string
	for _, line := range lines {
		if strings.Contains(stripANSIForTest(line), "TEST PANEL") {
			overlayLine = stripANSIForTest(line)
			break
		}
	}
	if overlayLine == "" {
		t.Fatal("expected overlay line with panel content")
	}
	// Panel is centered: (60 - 52) / 2 = 4 columns of chat on each side.
	const lateralCols = 4
	if !strings.HasPrefix(overlayLine, strings.Repeat("L", lateralCols)) {
		t.Fatalf("expected left chat visible, got %q", overlayLine[:minInt(15, len(overlayLine))])
	}
	if !strings.HasSuffix(overlayLine, strings.Repeat("L", lateralCols)) {
		t.Fatalf("expected right chat visible, got %q", overlayLine[maxInt(0, len(overlayLine)-15):])
	}
}