package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/huh"
)

const newSessionFormHints = "enter submit · esc cancel"

func isBareNewSessionCommand(text string) bool {
	return strings.TrimSpace(text) == "/new"
}

func newNewSessionForm(defaultName string) *huhForm {
	hf := &huhForm{
		kind:        formKindNewSession,
		sessionName: defaultName,
	}
	hf.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("New session").
				Description(newSessionFormHints).
				Placeholder("my-project").
				Value(&hf.sessionName),
		),
	).WithShowHelp(true).WithWidth(60)
	return hf
}

func (m Model) openNewSessionForm() (Model, tea.Cmd) {
	name := fmt.Sprintf("session-%d", len(m.sessions))
	m.formOpen = true
	m.activeForm = newNewSessionForm(name)
	return m, m.initActiveForm()
}

func (m Model) handleNewSessionFormKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if msg.String() == "esc" {
		return m.closeForm(), nil, true
	}
	return m, nil, false
}

func (m Model) advanceNewSessionForm(hf *huhForm) (Model, tea.Cmd) {
	name := strings.TrimSpace(hf.sessionName)
	if name == "" {
		return m, nil
	}
	m = m.closeForm()
	return m, createTUISession(m.ipcClient, name)
}