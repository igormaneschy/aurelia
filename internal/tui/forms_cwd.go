package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func isBareCwdCommand(text string) bool {
	return strings.TrimSpace(text) == "/cwd"
}

// openCwdForm opens the /cwd directory picker. Implemented in PR-A.
func (m Model) openCwdForm() (Model, tea.Cmd) {
	return m, nil
}

func (m Model) handleCwdFormKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	return m, nil, false
}

func (m Model) advanceCwdForm(hf *huhForm) (Model, tea.Cmd) {
	return m, nil
}