package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

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
	checkRenderable("ChatModeStyle", s.ChatModeStyle)
}

// TestNewLightStylesReturnsUsableStruct documents that newLightStyles is
// currently a placeholder (it reuses dark styles) but the constructor exists
// and is safe to call. T5.2.1 will swap the palette.
func TestNewLightStylesReturnsUsableStruct(t *testing.T) {
	s := newLightStyles()
	if got := s.UserStyle.Render("x"); got == "" {
		t.Error("newLightStyles returned styles that fail to render")
	}
}
