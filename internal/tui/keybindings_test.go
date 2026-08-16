package tui

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func TestDefaultKeyMap_HelpBinding(t *testing.T) {
	km := defaultKeyMap()

	if !key.Matches(keyPress(tea.KeyF1), km.Help) {
		t.Fatal("expected F1 to match Help binding")
	}
	if key.Matches(keyText("?"), km.Help) {
		t.Fatal("expected ? not to match Help binding")
	}
}

func TestDefaultKeyMap_MainShortcuts(t *testing.T) {
	km := defaultKeyMap()

	cases := []struct {
		msg  tea.KeyPressMsg
		want key.Binding
	}{
		{keyPress(tea.KeyEnter), km.Submit},
		{keyPress(tea.KeyEsc), km.Cancel},
		{keyCtrl('c'), km.Quit},
		{keyCtrl('l'), km.Clear},
		{keyCtrl('o'), km.MouseToggle},
		{keyCtrl('p'), km.ProjectPanel},
		{keyCtrl('s'), km.HistorySearch},
		{keyPress(tea.KeyF2), km.SidebarFocus},
		{keyPress(tea.KeyPgUp), km.PageUp},
		{keyPress(tea.KeyPgDown), km.PageDown},
	}

	for _, tc := range cases {
		if !key.Matches(tc.msg, tc.want) {
			t.Errorf("expected %q to match %q", tc.msg.String(), tc.want.Help().Key)
		}
	}
}

func TestKeyMap_FullHelpIncludesCommands(t *testing.T) {
	km := defaultKeyMap()
	groups := km.FullHelp()
	if len(groups) < 2 {
		t.Fatalf("expected at least 2 help groups, got %d", len(groups))
	}

	foundHelp := false
	for _, binding := range groups[1] {
		if binding.Help().Key == "/help" {
			foundHelp = true
			break
		}
	}
	if !foundHelp {
		t.Fatal("expected /help command in FullHelp commands group")
	}
}

func TestNewHelpModel_UsesThemeStyles(t *testing.T) {
	styles := newStylesForTheme(ThemeDark)
	h := newHelpModel(styles, ThemeDark)
	if h.Styles.FullKey.Render("test") == "test" {
		t.Fatal("expected themed help key style to add formatting")
	}
}
