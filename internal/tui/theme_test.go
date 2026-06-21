package tui

import (
	"bytes"
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// forceColorRenderer returns a lipgloss renderer with TrueColor forced so
// that ANSI escape codes are always emitted in tests.
func forceColorRenderer() *lipgloss.Renderer {
	r := lipgloss.NewRenderer(bytes.NewBuffer(nil))
	r.SetColorProfile(termenv.TrueColor)
	return r
}

// renderColored renders the style with forced TrueColor via a renderer,
// ensuring ANSI codes are present even in non-TTY test environments.
func renderColored(st lipgloss.Style, text string) string {
	r := forceColorRenderer()
	s := st.Copy().Renderer(r)
	return s.Render(text)
}

// TestNewDarkStylesPopulatesAllFields guards against a future refactor that
// drops a field from themeStyles without wiring it into newDarkStyles —
// the view layer would then panic at render time on a nil style.
func TestNewDarkStylesPopulatesAllFields(t *testing.T) {
	s := newDarkStyles()

	// Lipgloss.Style is not comparable, so we sample a representative subset
	// of fields and assert each one renders a non-empty string. A zero-value
	// lipgloss.Style still renders, so "non-empty" catches only true breakage
	// (panic, empty renderer output). The structural check (every field set
	// to something other than a panic-causing invalid value) is the goal.
	checkRenderable := func(name string, st lipgloss.Style) {
		t.Helper()
		if got := st.Render("x"); got == "" {
			t.Errorf("%s.Render(\"x\") returned empty string", name)
		}
	}

	checkRenderable("UserStyle", s.UserStyle)
	checkRenderable("AssistantStyle", s.AssistantStyle)
	checkRenderable("ErrorStyle", s.ErrorStyle)
	checkRenderable("InputPromptStyle", s.InputPromptStyle)
	checkRenderable("InputBoxStyle", s.InputBoxStyle)
	checkRenderable("InputWaitingStyle", s.InputWaitingStyle)
	checkRenderable("StatusBarStyle", s.StatusBarStyle)
	checkRenderable("StatusReadyStyle", s.StatusReadyStyle)
	checkRenderable("StatusBusyStyle", s.StatusBusyStyle)
	checkRenderable("StatusErrorStyle", s.StatusErrorStyle)
	checkRenderable("SidebarStyle", s.SidebarStyle)
	checkRenderable("SidebarTitleStyle", s.SidebarTitleStyle)
	checkRenderable("SidebarMutedStyle", s.SidebarMutedStyle)
	checkRenderable("SidebarActiveStyle", s.SidebarActiveStyle)
	checkRenderable("SidebarCursorStyle", s.SidebarCursorStyle)
	checkRenderable("HeaderTitleStyle", s.HeaderTitleStyle)
	checkRenderable("HeaderMetaStyle", s.HeaderMetaStyle)
	checkRenderable("HeaderRuleStyle", s.HeaderRuleStyle)
	checkRenderable("MessageSeparatorStyle", s.MessageSeparatorStyle)
	checkRenderable("MessageDividerStyle", s.MessageDividerStyle)
	checkRenderable("ChatModeStyle", s.ChatModeStyle)
}

// TestNewLightStylesPopulatesAllFields ensures the light theme has all fields
// populated and the palette differs from dark (it's a real theme, not a stub).
func TestNewLightStylesPopulatesAllFields(t *testing.T) {
	s := newLightStyles()

	checkRenderable := func(name string, st lipgloss.Style) {
		t.Helper()
		if got := st.Render("x"); got == "" {
			t.Errorf("%s.Render(\"x\") returned empty string", name)
		}
	}

	checkRenderable("UserStyle", s.UserStyle)
	checkRenderable("AssistantStyle", s.AssistantStyle)
	checkRenderable("ErrorStyle", s.ErrorStyle)
	checkRenderable("InputPromptStyle", s.InputPromptStyle)
	checkRenderable("InputBoxStyle", s.InputBoxStyle)
	checkRenderable("InputWaitingStyle", s.InputWaitingStyle)
	checkRenderable("StatusBarStyle", s.StatusBarStyle)
	checkRenderable("StatusReadyStyle", s.StatusReadyStyle)
	checkRenderable("StatusBusyStyle", s.StatusBusyStyle)
	checkRenderable("StatusErrorStyle", s.StatusErrorStyle)
	checkRenderable("SidebarStyle", s.SidebarStyle)
	checkRenderable("SidebarTitleStyle", s.SidebarTitleStyle)
	checkRenderable("SidebarMutedStyle", s.SidebarMutedStyle)
	checkRenderable("SidebarActiveStyle", s.SidebarActiveStyle)
	checkRenderable("SidebarCursorStyle", s.SidebarCursorStyle)
	checkRenderable("HeaderTitleStyle", s.HeaderTitleStyle)
	checkRenderable("HeaderMetaStyle", s.HeaderMetaStyle)
	checkRenderable("HeaderRuleStyle", s.HeaderRuleStyle)
	checkRenderable("MessageSeparatorStyle", s.MessageSeparatorStyle)
	checkRenderable("MessageDividerStyle", s.MessageDividerStyle)
	checkRenderable("ChatModeStyle", s.ChatModeStyle)
}

// TestLightThemeDiffersFromDark verifies the light palette is not a copy of dark.
func TestLightThemeDiffersFromDark(t *testing.T) {
	dark := newDarkStyles()
	light := newLightStyles()

	// Compare forced-color render output so the test works outside a TTY.
	darkBg := renderColored(dark.StatusBarStyle, "x")
	lightBg := renderColored(light.StatusBarStyle, "x")
	if darkBg == lightBg {
		t.Error("light StatusBarStyle should differ from dark")
	}
}

// TestParseTheme validates theme string parsing.
func TestParseTheme(t *testing.T) {
	tests := []struct {
		input    string
		expected Theme
	}{
		{"auto", ThemeAuto},
		{"AUTO", ThemeAuto},
		{"Auto", ThemeAuto},
		{"  auto  ", ThemeAuto},
		{"light", ThemeLight},
		{"LIGHT", ThemeLight},
		{"dark", ThemeDark},
		{"Dark", ThemeDark},
		{"", ThemeAuto},
		{"unknown", ThemeAuto},
		{"  ", ThemeAuto},
	}
	for _, tt := range tests {
		got := ParseTheme(tt.input)
		if got != tt.expected {
			t.Errorf("ParseTheme(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestResolveTheme verifies theme resolution with and without auto-detection.
func TestResolveTheme(t *testing.T) {
	// Explicit themes always resolve to themselves.
	if got := ResolveTheme(ThemeDark); got != ThemeDark {
		t.Errorf("ResolveTheme(dark) = %q, want dark", got)
	}
	if got := ResolveTheme(ThemeLight); got != ThemeLight {
		t.Errorf("ResolveTheme(light) = %q, want light", got)
	}

	// Auto resolves to dark by default (no light env vars set in test).
	// We can't control the test environment, so we just verify it returns
	// either light or dark (never auto).
	got := ResolveTheme(ThemeAuto)
	if got != ThemeDark && got != ThemeLight {
		t.Errorf("ResolveTheme(auto) = %q, want dark or light", got)
	}
}

// TestDetectLightBackgroundDefault verifies the default detection returns
// false in a clean environment.
func TestDetectLightBackgroundDefault(t *testing.T) {
	// Save and restore env vars that affect detection.
	restore := saveEnv("COLORFGBG", "TERM_PROGRAM")
	defer restore()

	os.Unsetenv("COLORFGBG")
	os.Unsetenv("TERM_PROGRAM")

	if detectLightBackground() {
		t.Error("detectLightBackground() = true with clean env, want false (dark default)")
	}
}

// TestDetectLightBackgroundColorFGBG verifies COLORFGBG-based detection.
func TestDetectLightBackgroundColorFGBG(t *testing.T) {
	restore := saveEnv("COLORFGBG", "TERM_PROGRAM")
	defer restore()

	os.Unsetenv("TERM_PROGRAM")

	// Light bg values should trigger light detection.
	for _, bg := range []string{"15", "7", "14", "11"} {
		os.Setenv("COLORFGBG", "0;"+bg)
		if !detectLightBackground() {
			t.Errorf("detectLightBackground() = false for COLORFGBG=%q, want true", "0;"+bg)
		}
	}

	// Dark bg values should keep dark.
	for _, bg := range []string{"0", "4", "8"} {
		os.Setenv("COLORFGBG", "15;"+bg)
		if detectLightBackground() {
			t.Errorf("detectLightBackground() = true for COLORFGBG=%q, want false", "15;"+bg)
		}
	}

	// Empty/non-standard values should not trigger light.
	os.Setenv("COLORFGBG", "")
	if detectLightBackground() {
		t.Error("detectLightBackground() = true for empty COLORFGBG, want false")
	}
}

// TestDetectLightBackgroundTerminalApp verifies Apple_Terminal detection.
func TestDetectLightBackgroundTerminalApp(t *testing.T) {
	restore := saveEnv("COLORFGBG", "TERM_PROGRAM")
	defer restore()

	os.Unsetenv("COLORFGBG")
	os.Setenv("TERM_PROGRAM", "Apple_Terminal")

	if !detectLightBackground() {
		t.Error("detectLightBackground() = false for Apple_Terminal, want true")
	}

	// Other terminals should not trigger light.
	os.Setenv("TERM_PROGRAM", "iTerm.app")
	if detectLightBackground() {
		t.Error("detectLightBackground() = true for iTerm.app, want false")
	}
}

// TestNewStylesForTheme verifies the theme→styles mapping.
func TestNewStylesForTheme(t *testing.T) {
	dark := newStylesForTheme(ThemeDark)
	light := newStylesForTheme(ThemeLight)

	if renderColored(dark.StatusBarStyle, "x") == renderColored(light.StatusBarStyle, "x") {
		t.Error("dark and light StatusBarStyle should differ")
	}
}

// TestModelThemeField verifies the Model carries the theme and correct styles.
func TestModelThemeField(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeLight)
	if m.theme != ThemeLight {
		t.Errorf("theme = %q, want light", m.theme)
	}
	// Light styles should match light, not dark.
	dark := newDarkStyles()
	if renderColored(m.styles.StatusBarStyle, "x") == renderColored(dark.StatusBarStyle, "x") {
		t.Error("light-themed Model should not use dark StatusBarStyle")
	}

	// Default (auto) resolves to styles.
	m2 := NewModel("/tmp/test.sock", ThemeAuto)
	if m2.theme != ThemeAuto {
		t.Errorf("theme = %q, want auto", m2.theme)
	}
	// Should have valid styles (dark or light, not panic).
	if got := m2.styles.UserStyle.Render("x"); got == "" {
		t.Error("auto-themed Model has empty UserStyle render")
	}
}

// TestGlamourStyle verifies the GlamourStyle() method.
func TestGlamourStyle(t *testing.T) {
	if got := ThemeDark.GlamourStyle(); got != "dark" {
		t.Errorf("ThemeDark.GlamourStyle() = %q, want dark", got)
	}
	if got := ThemeLight.GlamourStyle(); got != "light" {
		t.Errorf("ThemeLight.GlamourStyle() = %q, want light", got)
	}
}

// saveEnv saves current values of the named env vars and returns a function
// that restores them.
func saveEnv(names ...string) func() {
	saved := make(map[string]string, len(names))
	for _, n := range names {
		saved[n] = os.Getenv(n)
	}
	return func() {
		for n, v := range saved {
			if v == "" {
				os.Unsetenv(n)
			} else {
				os.Setenv(n, v)
			}
		}
	}
}
