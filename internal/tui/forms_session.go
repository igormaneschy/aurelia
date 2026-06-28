package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
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

// nextSessionDefaultName returns the smallest "session-N" name not already in
// use by the given sessions. It scans for names matching the pattern "session-N"
// (N >= 1) and finds the smallest N not present. Names that don't match the
// pattern (e.g. custom names like "work") are skipped, so they don't cause
// false collisions. A user-named session "session-abc" is also skipped because
// the integer parse fails.
func nextSessionDefaultName(sessions []tuiSessionInfo) string {
	used := make(map[int]bool)
	for _, s := range sessions {
		var n int
		if _, err := fmt.Sscanf(s.Name, "session-%d", &n); err == nil && n >= 1 {
			used[n] = true
		}
	}
	for n := 1; ; n++ {
		if !used[n] {
			return fmt.Sprintf("session-%d", n)
		}
	}
}

func (m Model) openNewSessionForm() (Model, tea.Cmd) {
	name := nextSessionDefaultName(m.sessions)
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