package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
)

// inputModel owns the multiline prompt, command autocomplete, input history,
// and pending attachment state before submit.
type inputModel struct {
	textarea textarea.Model

	autocompleteOptions []string
	autocompleteIndex   int

	inputHistory      []string
	inputHistoryIndex int
	historyPath       string

	pendingImages           []pendingImage
	submittedTempImagePaths []string
	pendingAttachments      []pendingAttachment

	// renameTargetChatID is non-zero when the user is renaming a session.
	renameTargetChatID int64
}

const maxInputHistory = 1000

func (m *Model) rememberInput(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	last := len(m.inputHistory) - 1
	if last < 0 || m.inputHistory[last] != text {
		m.inputHistory = append(m.inputHistory, text)
		if len(m.inputHistory) > maxInputHistory {
			m.inputHistory = m.inputHistory[len(m.inputHistory)-maxInputHistory:]
		}
	}
	m.inputHistoryIndex = len(m.inputHistory)
}

func (m Model) canNavigateInputHistory(direction int) bool {
	if m.waiting || len(m.inputHistory) == 0 {
		return false
	}
	if m.isViewingInputHistory() {
		return true
	}
	if direction < 0 {
		return strings.TrimSpace(m.textarea.Value()) == ""
	}
	return false
}

func (m Model) isViewingInputHistory() bool {
	if m.inputHistoryIndex < 0 || m.inputHistoryIndex >= len(m.inputHistory) {
		return false
	}
	return m.textarea.Value() == m.inputHistory[m.inputHistoryIndex]
}

func (m Model) navigateInputHistory(direction int) Model {
	if len(m.inputHistory) == 0 {
		return m
	}
	if m.inputHistoryIndex < 0 || m.inputHistoryIndex > len(m.inputHistory) {
		m.inputHistoryIndex = len(m.inputHistory)
	}

	if direction < 0 && m.inputHistoryIndex > 0 {
		m.inputHistoryIndex--
		m.textarea.SetValue(m.inputHistory[m.inputHistoryIndex])
		m.textarea.CursorEnd()
		return m
	}
	if direction > 0 && m.inputHistoryIndex < len(m.inputHistory) {
		m.inputHistoryIndex++
		if m.inputHistoryIndex == len(m.inputHistory) {
			m.textarea.Reset()
			return m
		}
		m.textarea.SetValue(m.inputHistory[m.inputHistoryIndex])
		m.textarea.CursorEnd()
	}
	return m
}

func (m Model) canApplyStartupHistory() bool {
	if m.waiting || m.reader != nil || len(m.messages) != 1 {
		return false
	}
	return m.messages[0].Sender == "Aurelia" && strings.HasPrefix(m.messages[0].Text, "Connected to Aurelia daemon")
}