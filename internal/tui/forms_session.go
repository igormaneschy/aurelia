package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func isBareNewSessionCommand(text string) bool {
	return strings.TrimSpace(text) == "/new"
}

// openNewSessionForm opens the /new session creator. Implemented in PR-B.
func (m Model) openNewSessionForm() (Model, tea.Cmd) {
	return m, nil
}

func (m Model) handleNewSessionFormKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	return m, nil, false
}

func (m Model) advanceNewSessionForm(hf *huhForm) (Model, tea.Cmd) {
	return m, nil
}