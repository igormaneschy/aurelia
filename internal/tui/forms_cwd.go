package tui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

const cwdFormHints = "↑↓ navigate · enter select · esc cancel"

func isBareCwdCommand(text string) bool {
	return strings.TrimSpace(text) == "/cwd"
}

func cwdPickerStartDir(cwdPath string) string {
	cwdPath = strings.TrimSpace(cwdPath)
	if cwdPath != "" && cwdPath != "not set" {
		if info, err := os.Stat(cwdPath); err == nil && info.IsDir() {
			return cwdPath
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "/"
}

func cwdFormDescription(cwdPath string) string {
	cwdPath = strings.TrimSpace(cwdPath)
	if cwdPath == "" || cwdPath == "not set" {
		return "No project set\n" + cwdFormHints
	}
	return "Current: " + cwdPath + "\n" + cwdFormHints
}

func newCwdForm(cwdPath string) *huhForm {
	hf := &huhForm{kind: formKindCwd}
	cwdPath = strings.TrimSpace(cwdPath)
	if cwdPath != "" && cwdPath != "not set" {
		hf.selected = cwdPath
	}
	hf.form = huh.NewForm(
		huh.NewGroup(
			huh.NewFilePicker().
				Title("Select project directory").
				Description(cwdFormDescription(cwdPath)).
				CurrentDirectory(cwdPickerStartDir(cwdPath)).
				DirAllowed(true).
				FileAllowed(false).
				Value(&hf.selected),
		),
	).WithShowHelp(true).WithWidth(60)
	return hf
}

func (m Model) openCwdForm() (Model, tea.Cmd) {
	m.formOpen = true
	m.activeForm = newCwdForm(m.cwdPath)
	return m, m.initActiveForm()
}

func (m Model) handleCwdFormKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if msg.String() == "esc" {
		return m.closeForm(), nil, true
	}
	return m, nil, false
}

func (m Model) advanceCwdForm(hf *huhForm) (Model, tea.Cmd) {
	path := strings.TrimSpace(hf.selected)
	if path == "" {
		return m.closeForm(), nil
	}
	m = m.closeForm()
	m.waiting = true
	m.streamID++
	return m, tea.Batch(m.sendCommand("/cwd "+path), spinnerTickCmd())
}
