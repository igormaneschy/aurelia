package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestKeyHelpersString(t *testing.T) {
	cases := map[string]tea.KeyPressMsg{
		"ctrl+c":    keyCtrl('c'),
		"ctrl+o":    keyCtrl('o'),
		"ctrl+l":    keyCtrl('l'),
		"ctrl+y":    keyCtrl('y'),
		"ctrl+p":    keyCtrl('p'),
		"ctrl+s":    keyCtrl('s'),
		"ctrl+j":    keyCtrl('j'),
		"alt+enter": keyAltEnter(),
		"up":        keyPress(tea.KeyUp),
		"down":      keyPress(tea.KeyDown),
		"enter":     keyPress(tea.KeyEnter),
		"esc":       keyPress(tea.KeyEsc),
	}
	for want, msg := range cases {
		if got := msg.String(); got != want {
			t.Errorf("%v.String() = %q, want %q (keystroke=%q)", msg, got, want, msg.Keystroke())
		}
	}
}