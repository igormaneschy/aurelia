package tui

import (
	"strings"
	"testing"
)

func TestCycleTheme_RotatesDarkLightAuto(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	(&m).cycleTheme()
	if m.theme != ThemeLight {
		t.Fatalf("got %q want light", m.theme)
	}
	(&m).cycleTheme()
	if m.theme != ThemeAuto {
		t.Fatalf("got %q want auto", m.theme)
	}
	(&m).cycleTheme()
	if m.theme != ThemeDark {
		t.Fatalf("got %q want dark", m.theme)
	}
}

func TestToggleTransparency_StripsStatusBarBackground(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	if m.transparent {
		t.Fatal("expected opaque by default")
	}
	(&m).toggleTransparency()
	if !m.transparent {
		t.Fatal("expected transparent after toggle")
	}
	rendered := m.styles.StatusBarStyle.Render("status")
	if strings.Contains(rendered, "48;") {
		t.Fatalf("expected no background SGR in transparent status bar, got %q", rendered)
	}
}

func TestThemeToggleKey_CtrlT(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	updated, _ := m.Update(keyCtrl('t'))
	got := updated.(Model)
	if got.theme != ThemeLight {
		t.Fatalf("expected theme light after ctrl+t, got %q", got.theme)
	}
}

func TestSaveAndLoadTUIPrefs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := saveTUIPrefs(TUIPrefs{Theme: ThemeLight, Transparent: true}); err != nil {
		t.Fatal(err)
	}
	got := LoadTUIPrefs()
	if got.Theme != ThemeLight || !got.Transparent {
		t.Fatalf("got %+v", got)
	}
}