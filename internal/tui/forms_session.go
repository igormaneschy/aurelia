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

// nextSessionDefaultName returns the next "session-N" name, always
// monotonically increasing. It scans existing sessions for names matching the
// pattern "session-N" (N >= 1), finds the largest N, and returns N+1. It never
// reuses numbers from deleted sessions, so the user never sees a previously
// deleted name reappear. Names that don't match the pattern (e.g. custom names
// like "work" or non-integer suffixes like "session-abc") are skipped.
func nextSessionDefaultName(sessions []tuiSessionInfo) string {
	maxN := 0
	for _, s := range sessions {
		var n int
		if _, err := fmt.Sscanf(s.Name, "session-%d", &n); err == nil && n >= 1 {
			if n > maxN {
				maxN = n
			}
		}
	}
	return fmt.Sprintf("session-%d", maxN+1)
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