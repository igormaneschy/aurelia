package tui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/huh"
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

func newCwdForm(startDir string) *huhForm {
	hf := &huhForm{kind: formKindCwd}
	hf.form = huh.NewForm(
		huh.NewGroup(
			huh.NewFilePicker().
				Title("Select project directory").
				Description(cwdFormHints).
				CurrentDirectory(startDir).
				DirAllowed(true).
				FileAllowed(false).
				Value(&hf.selected),
		),
	).WithShowHelp(true).WithWidth(60)
	return hf
}

func (m Model) openCwdForm() (Model, tea.Cmd) {
	m.formOpen = true
	m.activeForm = newCwdForm(cwdPickerStartDir(m.cwdPath))
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