package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/huh"
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

func confirmFormCopy(action confirmAction, targetName string) (title, description string) {
	switch action {
	case confirmActionClear:
		return "Clear chat history?", "Removes messages from the viewport. Esc to keep them."
	case confirmActionReset:
		return "Reset session?", "Clears PI context for this session. This cannot be undone."
	case confirmActionDeleteSession:
		if targetName != "" {
			return fmt.Sprintf("Delete session %q?", targetName), "Removes the session and its binding. This cannot be undone."
		}
		return "Delete session?", "Removes the session and its binding. This cannot be undone."
	default:
		return "Confirm?", ""
	}
}

func newConfirmForm(action confirmAction, title, description string) *huhForm {
	hf := &huhForm{
		kind:          formKindConfirm,
		confirmAction: action,
	}
	hf.form = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Description(description).
				Affirmative("Yes").
				Negative("No").
				Value(&hf.confirmed),
		),
	).WithShowHelp(true).WithWidth(60)
	return hf
}

func (m Model) openConfirmForm(action confirmAction) (Model, tea.Cmd) {
	title, description := confirmFormCopy(action, "")
	m.formOpen = true
	m.activeForm = newConfirmForm(action, title, description)
	return m, m.initActiveForm()
}

func (m Model) openDeleteSessionConfirm(chatID int64, name string) (Model, tea.Cmd) {
	title, description := confirmFormCopy(confirmActionDeleteSession, name)
	m.formOpen = true
	m.activeForm = newConfirmForm(confirmActionDeleteSession, title, description)
	m.activeForm.deleteChatID = chatID
	return m, m.initActiveForm()
}

func (m Model) handleConfirmFormKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if msg.String() == "esc" {
		return m.closeForm(), nil, true
	}
	return m, nil, false
}

func (m Model) advanceConfirmForm(hf *huhForm) (Model, tea.Cmd) {
	if !hf.confirmed {
		return m.closeForm(), nil
	}
	switch hf.confirmAction {
	case confirmActionClear:
		m = m.closeForm()
		m.messages = nil
		m.updateViewport()
		return m, nil
	case confirmActionReset:
		m = m.closeForm()
		m.messages = nil
		m.updateViewport()
		m.waiting = true
		m.streamID++
		return m, tea.Batch(m.sendCommand("/reset"), spinnerTickCmd())
	case confirmActionDeleteSession:
		chatID := hf.deleteChatID
		m = m.closeForm()
		if chatID == 0 {
			return m, nil
		}
		return m, deleteTUISession(m.ipcClient, chatID)
	default:
		return m.closeForm(), nil
	}
}