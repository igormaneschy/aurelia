package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

func TestIsBareInteractiveCommands(t *testing.T) {
	cases := []struct {
		fn   func(string) bool
		ok   string
		fail string
	}{
		{isBareCwdCommand, "/cwd", "/cwd /tmp"},
		{isBareNewSessionCommand, "/new", "/new foo"},
		{isBareClearCommand, "/clear", "/clear all"},
		{isBareResetCommand, "/reset", "/reset now"},
		{isBareHelpCommand, "/help", "/help extra"},
		{isBareStatusCommand, "/status", "/status extra"},
	}
	for _, tc := range cases {
		if !tc.fn(tc.ok) || tc.fn(tc.fail) {
			t.Fatalf("bare command check failed for ok=%q fail=%q", tc.ok, tc.fail)
		}
	}
}

func TestOpenCwdForm_OpensOverlay(t *testing.T) {
	m := testChatModel()
	m.cwdPath = "/tmp"
	next, cmd := m.openCwdForm()
	if !next.formOpen || next.activeForm == nil || next.activeForm.kind != formKindCwd {
		t.Fatalf("form = %#v", next.activeForm)
	}
	if cmd == nil {
		t.Fatal("expected init command")
	}
}

func TestOpenCwdForm_ShowsCurrentPath(t *testing.T) {
	m := testChatModel()
	m.cwdPath = "/Users/igor/dev/aurelia"
	next, _ := m.openCwdForm()
	if next.activeForm.selected != m.cwdPath {
		t.Fatalf("selected = %q, want %q", next.activeForm.selected, m.cwdPath)
	}
	view := initModelFormView(next.activeForm)
	if !strings.Contains(view, m.cwdPath) {
		t.Fatalf("form view should show current path, got:\n%s", view)
	}
}

func TestOpenNewSessionForm_OpensOverlayAndFetchesModels(t *testing.T) {
	m := testChatModel()
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "DM"}}
	next, cmd := m.openNewSessionForm()
	if !next.formOpen || next.activeForm == nil || next.activeForm.kind != formKindNewSession {
		t.Fatalf("form = %#v", next.activeForm)
	}
	if next.activeForm.sessionName != "session-1" {
		t.Fatalf("name = %q", next.activeForm.sessionName)
	}
	if cmd == nil {
		t.Fatal("expected batch command")
	}
}

func TestOpenConfirmForm_Clear(t *testing.T) {
	m := testChatModel()
	next, cmd := m.openConfirmForm(confirmActionClear)
	if !next.formOpen || next.activeForm.kind != formKindConfirm {
		t.Fatalf("form = %#v", next.activeForm)
	}
	if cmd == nil {
		t.Fatal("expected init command")
	}
	view := initModelFormView(next.activeForm)
	if !strings.Contains(view, "Clear chat history") {
		t.Fatalf("view = %q", view)
	}
}

func TestAdvanceConfirmForm_ClearWipesMessages(t *testing.T) {
	m := testChatModel()
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}
	m.formOpen = true
	m.activeForm = newConfirmForm(confirmActionClear, "Clear chat history?", "test")
	m.activeForm.confirmed = true
	next, cmd := m.advanceConfirmForm(m.activeForm)
	if cmd != nil || next.formOpen || len(next.messages) != 0 {
		t.Fatalf("formOpen=%v messages=%d cmd=%v", next.formOpen, len(next.messages), cmd)
	}
}

func TestAdvanceConfirmForm_ResetSendsCommand(t *testing.T) {
	m := testChatModel()
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}
	m.formOpen = true
	m.activeForm = newConfirmForm(confirmActionReset, "Reset session?", "test")
	m.activeForm.confirmed = true
	next, cmd := m.advanceConfirmForm(m.activeForm)
	if !next.waiting || next.formOpen || cmd == nil {
		t.Fatalf("waiting=%v formOpen=%v cmd=%v", next.waiting, next.formOpen, cmd)
	}
	if len(next.messages) != 0 {
		t.Fatal("expected viewport cleared on reset")
	}
}

func TestAdvanceConfirmForm_DeleteSession(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newConfirmForm(confirmActionDeleteSession, "Delete?", "test")
	m.activeForm.confirmed = true
	m.activeForm.deleteChatID = -2
	next, cmd := m.advanceConfirmForm(m.activeForm)
	if next.formOpen || cmd == nil {
		t.Fatalf("formOpen=%v cmd=%v", next.formOpen, cmd)
	}
}

func TestAdvanceNewSessionForm_QueuesCreate(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	hf := newNewSessionForm(modelCatalog{}, "research")
	hf.sessionName = "research"
	hf.selected = "openai/gpt-5.1"
	next, cmd := m.advanceNewSessionForm(hf)
	if next.formOpen || next.pendingSessionModel != "openai/gpt-5.1" || cmd == nil {
		t.Fatalf("formOpen=%v pending=%q cmd=%v", next.formOpen, next.pendingSessionModel, cmd)
	}
}

func TestAdvanceNewSessionForm_EmptyNameStaysOpen(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	hf := newNewSessionForm(modelCatalog{}, "")
	hf.sessionName = "   "
	next, cmd := m.advanceNewSessionForm(hf)
	if !next.formOpen || cmd != nil {
		t.Fatalf("formOpen=%v cmd=%v", next.formOpen, cmd)
	}
}

func TestOpenFormForCommand_RoutesBareCommands(t *testing.T) {
	m := testChatModel()
	for _, text := range []string{"/model", "/cwd", "/new", "/clear", "/reset"} {
		next, cmd, handled := m.openFormForCommand(text)
		if !handled || !next.formOpen || cmd == nil {
			t.Fatalf("%q handled=%v formOpen=%v cmd=%v", text, handled, next.formOpen, cmd)
		}
		m = next.closeForm()
	}

	next, cmd, handled := m.openFormForCommand("/help")
	if !handled || cmd != nil || !next.helpVisible() {
		t.Fatalf("/help handled=%v helpVisible=%v cmd=%v", handled, next.helpVisible(), cmd)
	}
	next = next.closeHelpOverlay()

	next, cmd, handled = m.openFormForCommand("/status")
	if !handled || cmd == nil || !next.projectPanelOpen {
		t.Fatalf("/status handled=%v projectPanelOpen=%v cmd=%v", handled, next.projectPanelOpen, cmd)
	}
}

func TestHandleCwdFormKey_EscCloses(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newCwdForm("/tmp")
	next, cmd, handled := m.handleCwdFormKey(keyPress(tea.KeyEsc))
	if !handled || next.formOpen || cmd != nil {
		t.Fatalf("handled=%v formOpen=%v", handled, next.formOpen)
	}
}

func TestCatalogAllModelOptions_IncludesProviders(t *testing.T) {
	catalog := catalogFromIPCModels([]ipc.IPCEvent{{
		Type: ipc.EventTypeModels,
		Body: `[{"provider":"openai","id":"gpt-5.1"}]`,
	}})
	opts := catalog.allModelOptions()
	if len(opts) < 2 {
		t.Fatalf("options = %d", len(opts))
	}
}