package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

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

func TestOpenNewSessionForm_OpensNameOnlyOverlay(t *testing.T) {
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
		t.Fatal("expected init command")
	}
	view := initModelFormView(next.activeForm)
	if strings.Contains(view, "Model") {
		t.Fatalf("new session form should not include model picker, got:\n%s", view)
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
	hf := newNewSessionForm("research")
	hf.sessionName = "research"
	next, cmd := m.advanceNewSessionForm(hf)
	if next.formOpen || next.pendingSessionModel != "" || cmd == nil {
		t.Fatalf("formOpen=%v pending=%q cmd=%v", next.formOpen, next.pendingSessionModel, cmd)
	}
}

func TestAdvanceNewSessionForm_EmptyNameStaysOpen(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	hf := newNewSessionForm("")
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
	m = next.closeHelpOverlay()

	next, cmd, handled = m.openFormForCommand("/status")
	if !handled || cmd == nil || !next.projectPanelOpen {
		t.Fatalf("/status handled=%v projectPanelOpen=%v cmd=%v", handled, next.projectPanelOpen, cmd)
	}
}

func TestUpdateChat_RoutesAllMessagesToFormWhenOpen(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newNewSessionForm("test")

	// Before the fix, updateChat only routed tea.KeyMsg and tea.WindowSizeMsg
	// to updateActiveForm. Any other message type (including huh's internal
	// nextFieldMsg, nextGroupMsg) was silently dropped.
	type unknownMsg struct{}
	gotI, cmd := m.updateChat(unknownMsg{})
	got := gotI.(Model)
	if !got.formOpen {
		t.Fatal("unknown message was dropped — form should still be open")
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd for unknown message, got %v", cmd)
	}
}

func TestUpdateActiveForm_DoesNotDropUnknownMessages(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newNewSessionForm("test-name")

	// Before the fix, updateActiveForm had a default: return m, nil that
	// dropped any message type that wasn't tea.KeyMsg or tea.WindowSizeMsg.
	type customMsg struct{}
	gotI, cmd := m.updateActiveForm(customMsg{})
	got := gotI.(Model)
	if !got.formOpen {
		t.Fatal("custom message was dropped by updateActiveForm — form should still be open")
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd for custom message, got %v", cmd)
	}
}

func TestFormCompletion_DetectedAfterNextFieldMsg(t *testing.T) {
	m := testChatModel()
	m.width = 100
	m.height = 40

	m, _ = m.openNewSessionForm()
	m.activeForm.sessionName = "test-session"

	// Send WindowSizeMsg so the form has dimensions
	iface, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = iface.(Model)

	// Send a KeyPressMsg for Enter — this should produce nextFieldMsg cmd
	iface, cmd1 := m.Update(keyPress(tea.KeyEnter))
	m = iface.(Model)
	if cmd1 == nil {
		t.Fatal("Enter should produce a command (nextFieldMsg)")
	}

	// Execute the cmd to get the message.
	// In bubbletea v2, a batch of cmds returns BatchMsg ([]Cmd).
	msg1 := cmd1()
	if batch, ok := msg1.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if sub == nil {
				continue
			}
			if next := sub(); next != nil {
				iface, _ = m.Update(next)
				m = iface.(Model)
			}
		}
	} else if msg1 != nil {
		iface, _ = m.Update(msg1)
		m = iface.(Model)
	}

	// Now send NextField() (exported huh function that returns nextFieldMsg).
	// This simulates what would happen when bubbletea dispatches the cmd.
	iface, cmd2 := m.Update(huh.NextField())
	m = iface.(Model)
	if cmd2 == nil {
		t.Fatal("nextFieldMsg should produce a command")
	}

	// Execute the returned cmd to get nextGroupMsg
	msg2 := cmd2()
	if batch, ok := msg2.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if sub == nil {
				continue
			}
			if next := sub(); next != nil {
				iface, _ = m.Update(next)
				m = iface.(Model)
			}
		}
	} else if msg2 != nil {
		iface, _ = m.Update(msg2)
		m = iface.(Model)
	}

	// After sending the messages, the form should have reached StateCompleted.
	if m.formOpen {
		t.Fatal("form should be closed after submission")
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

