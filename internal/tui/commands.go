package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// openFormForCommand opens an interactive huh form for bare slash commands.
// Returns handled=true when the command was consumed.
func (m Model) openFormForCommand(text string) (Model, tea.Cmd, bool) {
	text = strings.TrimSpace(text)
	switch {
	case isBareModelCommand(text):
		next, cmd := m.openModelSelect()
		return next, cmd, true
	case isBareCwdCommand(text):
		next, cmd := m.openCwdForm()
		return next, cmd, cmd != nil
	case isBareNewSessionCommand(text):
		next, cmd := m.openNewSessionForm()
		return next, cmd, cmd != nil
	case isBareClearCommand(text):
		next, cmd := m.openConfirmForm(confirmActionClear)
		return next, cmd, cmd != nil
	case isBareResetCommand(text):
		next, cmd := m.openConfirmForm(confirmActionReset)
		return next, cmd, cmd != nil
	}
	return m, nil, false
}