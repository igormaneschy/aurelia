package tui

import (
	"charm.land/bubbles/v2/textarea"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// testChatModel builds a chat-state Model for tests. Promoted fields from
// embedded component structs cannot be set in composite literals.
func testChatModel() Model {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.ready = true
	return m
}

func testChatModelWithTextarea(ta textarea.Model) Model {
	m := testChatModel()
	m.textarea = ta
	return m
}

func testProjectPanelModel(ps *ipc.ProjectStatePayload) Model {
	m := testChatModel()
	m.projectPanelOpen = true
	m.projectState = ps
	m.width = 80
	m.height = 40
	return m
}
