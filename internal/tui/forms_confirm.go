package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type confirmAction int

const (
	confirmActionNone confirmAction = iota
	confirmActionClear
	confirmActionReset
	confirmActionDeleteSession
)

func isBareClearCommand(text string) bool {
	return strings.TrimSpace(text) == "/clear"
}

func isBareResetCommand(text string) bool {
	return strings.TrimSpace(text) == "/reset"
}

// openConfirmForm opens a huh confirmation dialog. Implemented in PR-C.
func (m Model) openConfirmForm(action confirmAction) (Model, tea.Cmd) {
	return m, nil
}

func (m Model) handleConfirmFormKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	return m, nil, false
}

func (m Model) advanceConfirmForm(hf *huhForm) (Model, tea.Cmd) {
	return m, nil
}