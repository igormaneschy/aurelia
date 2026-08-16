package tui

import tea "charm.land/bubbletea/v2"

// v2 key helpers for tests. Bubble Tea v2 uses KeyPressMsg with Key{Code, Text, Mod}.

func keyPress(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func keyCtrl(ch rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: ch, Mod: tea.ModCtrl})
}

func keyText(text string) tea.KeyPressMsg {
	k := tea.Key{Text: text}
	if len([]rune(text)) == 1 {
		k.Code = rune(text[0])
	}
	return tea.KeyPressMsg(k)
}

func keyAlt(ch rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: ch, Mod: tea.ModAlt})
}

func keyAltEnter() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Mod: tea.ModAlt})
}

func keyBackspace() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace})
}

// prepSidebarTest initializes sidebar table rows for tests that set m.sessions directly.
func prepSidebarTest(m *Model) {
	if m.sidebarFocused {
		m.sidebarTable.Focus()
	}
	m.syncSidebarRows()
	if m.sidebarCursor >= 0 && m.sidebarCursor < len(m.sessions) {
		m.sidebarTable.SetCursor(m.sidebarCursor)
		m.syncSidebarRows()
	}
}

// sidebarViewForTest renders the sidebar table after syncing rows.
func sidebarViewForTest(m Model) string {
	m.syncSidebarRows()
	return m.renderSidebarTable()
}
