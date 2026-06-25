package tui

import ("strings"; "testing"; tea "charm.land/bubbletea/v2")

func TestIsBareModelCommand(t *testing.T) {
	if !isBareModelCommand("/model") || isBareModelCommand("/model gpt-4") { t.Fatal("bad") }
}
func TestOpenModelSelect_SetsFormOpen(t *testing.T) {
	m := testChatModel(); next, cmd := m.openModelSelect()
	if !next.formOpen || next.activeForm == nil || cmd == nil { t.Fatal("bad") }
}
func TestCloseForm_ClearsState(t *testing.T) {
	m := testChatModel(); m.formOpen = true; m.activeForm = newModelSelectForm([]string{"auto"}, "auto", nil)
	c := m.closeForm(); if c.formOpen || c.activeForm != nil { t.Fatal("bad") }
}
func TestBareModelSubmitOpensFormNotIPC(t *testing.T) {
	m := testChatModel(); m.textarea.SetValue("/model")
	next, cmd := m.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !next.(Model).formOpen || cmd == nil { t.Fatal("bad") }
}
func TestRenderFormOverlay_UsesPanel(t *testing.T) {
	m := testChatModel(); m.width = 60; m.height = 10; m.formOpen = true
	m.activeForm = newModelSelectForm([]string{"auto"}, "auto", nil)
	if m.renderFormOverlay(strings.Repeat("L", 60)) == strings.Repeat("L", 60) { t.Fatal("bad") }
}
