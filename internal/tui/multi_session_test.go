package tui

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

func TestSessionsFromEvents_ParsesList(t *testing.T) {
	body, _ := json.Marshal([]struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "work"},
	})
	events := []ipc.IPCEvent{
		{Type: ipc.EventTypeSessions, Body: string(body)},
		{Type: ipc.EventTypeStreamEnd, Done: true},
	}

	msg := sessionsFromEvents(events)
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	if len(msg.sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(msg.sessions))
	}
	if msg.sessions[0].ChatID != -9000001 || msg.sessions[0].Name != "dm" {
		t.Errorf("sessions[0] = %+v, want {ChatID:-9000001 Name:dm}", msg.sessions[0])
	}
	if msg.sessions[1].ChatID != -9000002 || msg.sessions[1].Name != "work" {
		t.Errorf("sessions[1] = %+v, want {ChatID:-9000002 Name:work}", msg.sessions[1])
	}
}

func TestSessionsFromEvents_EmptyBody(t *testing.T) {
	msg := sessionsFromEvents([]ipc.IPCEvent{
		{Type: ipc.EventTypeSessions, Body: ""},
	})
	if msg.err != nil {
		t.Errorf("unexpected error: %v", msg.err)
	}
	if len(msg.sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(msg.sessions))
	}
}

func TestSessionCreatedFromEvents_ParsesSession(t *testing.T) {
	body, _ := json.Marshal(struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}{ChatID: -9000003, Name: "research"})

	msg := sessionCreatedFromEvents([]ipc.IPCEvent{
		{Type: ipc.EventTypeSessionCreated, Body: string(body)},
	})
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	if msg.session.ChatID != -9000003 || msg.session.Name != "research" {
		t.Errorf("session = %+v, want {ChatID:-9000003 Name:research}", msg.session)
	}
}

func TestSessionOpenedFromEvents_ParsesSession(t *testing.T) {
	body, _ := json.Marshal(struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}{ChatID: -9000002, Name: "work"})

	msg := sessionOpenedFromEvents([]ipc.IPCEvent{
		{Type: ipc.EventTypeSessionOpened, Body: string(body)},
	})
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	if msg.session.ChatID != -9000002 || msg.session.Name != "work" {
		t.Errorf("session = %+v, want {ChatID:-9000002 Name:work}", msg.session)
	}
}

func TestTUISessionsMsg_UpdatesSessionList(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sessions = nil

	updated, _ := m.Update(tuiSessionsMsg{
		sessions: []tuiSessionInfo{
			{ChatID: -9000001, Name: "dm"},
			{ChatID: -9000002, Name: "work"},
		},
	})
	m2 := updated.(Model)

	if len(m2.sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(m2.sessions))
	}
}

func TestTUISessionsMsg_EnsuresDefaultDMInList(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat

	// Daemon returns only a named session, not the DM.
	updated, _ := m.Update(tuiSessionsMsg{
		sessions: []tuiSessionInfo{
			{ChatID: -9000002, Name: "work"},
		},
	})
	m2 := updated.(Model)

	// The default DM should be prepended.
	if len(m2.sessions) != 2 {
		t.Fatalf("expected 2 sessions (DM + work), got %d", len(m2.sessions))
	}
	if m2.sessions[0].ChatID != ipc.ReservedTUIChatID {
		t.Errorf("sessions[0].ChatID = %d, want ReservedTUIChatID (DM)", m2.sessions[0].ChatID)
	}
	if m2.sessions[1].Name != "work" {
		t.Errorf("sessions[1].Name = %q, want %q", m2.sessions[1].Name, "work")
	}
}

func TestTUISessionOpenedMsg_SwitchesActiveSession(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.activeSession = ipc.ReservedTUIChatID
	m.messages = []chatMessage{{Sender: "Aurelia", Text: "old message"}}
	m.viewportSet = true

	updated, _ := m.Update(tuiSessionOpenedMsg{
		session: tuiSessionInfo{ChatID: -9000002, Name: "work"},
	})
	m2 := updated.(Model)

	if m2.activeSession != -9000002 {
		t.Errorf("activeSession = %d, want -9000002", m2.activeSession)
	}
	if len(m2.messages) != 0 {
		t.Errorf("expected messages cleared on session switch, got %d", len(m2.messages))
	}
	if m2.sidebarFocused {
		t.Error("expected sidebarFocused=false after opening session")
	}
	if !m2.switchingSession {
		t.Error("expected switchingSession=true after opening session")
	}
}

func TestTUIHistoryMsg_AppliedOnSessionSwitch(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.activeSession = -9000002
	m.messages = []chatMessage{} // empty after session switch
	m.switchingSession = true

	history := []chatMessage{
		{Sender: "Igor", Text: "previous question"},
		{Sender: "Aurelia", Text: "previous answer"},
	}

	updated, _ := m.Update(tuiHistoryMsg{messages: history})
	m2 := updated.(Model)

	if len(m2.messages) != 2 {
		t.Fatalf("expected 2 history messages, got %d", len(m2.messages))
	}
	if m2.messages[0].Text != "previous question" {
		t.Errorf("messages[0].Text = %q, want %q", m2.messages[0].Text, "previous question")
	}
	if m2.switchingSession {
		t.Error("expected switchingSession=false after history applied")
	}
}

func TestTUIHistoryMsg_NotAppliedWhenNotSwitchingAndNoStartupMessage(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.messages = []chatMessage{{Sender: "Igor", Text: "my message"}}
	m.switchingSession = false

	history := []chatMessage{
		{Sender: "Igor", Text: "old question"},
	}

	updated, _ := m.Update(tuiHistoryMsg{messages: history})
	m2 := updated.(Model)

	// Should NOT replace — we're not switching sessions and the startup
	// condition (1 "Connected" message) is not met.
	if len(m2.messages) != 1 || m2.messages[0].Text != "my message" {
		t.Errorf("expected original message preserved, got %v", m2.messages)
	}
}

func TestTUISessionCreatedMsg_SwitchesToNewSession(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.activeSession = ipc.ReservedTUIChatID

	updated, _ := m.Update(tuiSessionCreatedMsg{
		session: tuiSessionInfo{ChatID: -9000003, Name: "research"},
	})
	m2 := updated.(Model)

	if m2.activeSession != -9000003 {
		t.Errorf("activeSession = %d, want -9000003", m2.activeSession)
	}
	if len(m2.messages) != 0 {
		t.Errorf("expected messages cleared, got %d", len(m2.messages))
	}
}

func TestTUISessionDeletedMsg_FallsBackToDM(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.activeSession = -9000002 // on a named session

	updated, _ := m.Update(tuiSessionDeletedMsg{chatID: -9000002})
	m2 := updated.(Model)

	if m2.activeSession != ipc.ReservedTUIChatID {
		t.Errorf("activeSession = %d, want ReservedTUIChatID (fallback)", m2.activeSession)
	}
}

func TestTUISessionDeletedMsg_OtherSessionKeepsActive(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.activeSession = -9000003 // on a different session

	updated, _ := m.Update(tuiSessionDeletedMsg{chatID: -9000002})
	m2 := updated.(Model)

	if m2.activeSession != -9000003 {
		t.Errorf("activeSession = %d, want -9000003 (unchanged)", m2.activeSession)
	}
}

func TestHandleSidebarKey_UpNavigatesCursor(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	m.sidebarCursor = 2
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "work"},
		{ChatID: -9000003, Name: "research"},
	}
	prepSidebarTest(&m)

	updated, _ := m.handleKeyMsg(keyPress(tea.KeyUp))
	m2 := updated.(Model)

	if m2.sidebarCursor != 1 {
		t.Errorf("sidebarCursor = %d, want 1", m2.sidebarCursor)
	}
}

func TestHandleSidebarKey_DownNavigatesCursor(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	m.sidebarCursor = 0
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "work"},
	}
	prepSidebarTest(&m)

	updated, _ := m.handleKeyMsg(keyPress(tea.KeyDown))
	m2 := updated.(Model)

	if m2.sidebarCursor != 1 {
		t.Errorf("sidebarCursor = %d, want 1", m2.sidebarCursor)
	}
}

func TestHandleSidebarKey_DownClampsAtEnd(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	m.sidebarCursor = 1
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "work"},
	}
	prepSidebarTest(&m)

	updated, _ := m.handleKeyMsg(keyPress(tea.KeyDown))
	m2 := updated.(Model)

	if m2.sidebarCursor != 1 {
		t.Errorf("sidebarCursor = %d, want 1 (clamped)", m2.sidebarCursor)
	}
}

func TestHandleSidebarKey_EscExitsFocus(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true

	updated, _ := m.handleKeyMsg(keyPress(tea.KeyEsc))
	m2 := updated.(Model)

	if m2.sidebarFocused {
		t.Error("expected sidebarFocused=false after esc")
	}
}

func TestHandleSidebarKey_EnterOpensSession(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	m.sidebarCursor = 1
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "work"},
	}
	m.activeSession = -9000001
	prepSidebarTest(&m)

	updated, cmd := m.handleKeyMsg(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	// Should return a command (openTUISession).
	if cmd == nil {
		t.Fatal("expected non-nil command after opening session")
	}

	// Sidebar should still be focused until the opened msg arrives.
	if !m2.sidebarFocused {
		t.Error("expected sidebarFocused=true until session opens")
	}
}

func TestHandleSidebarKey_EnterOnActiveSessionJustUnfocuses(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	m.sidebarCursor = 0
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
	}
	m.activeSession = -9000001

	updated, cmd := m.handleKeyMsg(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected nil command when opening already-active session")
	}
	if m2.sidebarFocused {
		t.Error("expected sidebarFocused=false after enter on active session")
	}
}

func TestHandleSidebarKey_NCreatesSession(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
	}

	updated, cmd := m.handleKeyMsg(keyText("n"))
	_ = updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command after pressing n")
	}
}

func TestHandleSidebarKey_DDeletesSession(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	m.sidebarCursor = 1
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "work"},
	}
	prepSidebarTest(&m)

	updated, cmd := m.handleKeyMsg(keyText("d"))
	_ = updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command after pressing d on non-DM session")
	}
}

func TestHandleSidebarKey_DOnDMShowsMessage(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	m.sidebarCursor = 0
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
	}

	updated, cmd := m.handleKeyMsg(keyText("d"))
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected nil command when pressing d on DM session")
	}
	if len(m2.messages) == 0 {
		t.Error("expected warning message about not deleting DM")
	}
}

func TestCtrlS_FocusesSidebar(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.showSidebar = true
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
	}
	m.activeSession = -9000001

	// ctrl+s
	updated, _ := m.handleKeyMsg(keyCtrl('s'))
	m2 := updated.(Model)

	if !m2.sidebarFocused {
		t.Error("expected sidebarFocused=true after ctrl+s")
	}
	if m2.sidebarCursor != 0 {
		t.Errorf("sidebarCursor = %d, want 0 (active session)", m2.sidebarCursor)
	}

	// f2 (fallback for terminals that intercept ctrl+s)
	updated2, _ := m.handleKeyMsg(keyPress(tea.KeyF2))
	m3 := updated2.(Model)

	if !m3.sidebarFocused {
		t.Error("expected sidebarFocused=true after f2")
	}
}

func TestCtrlS_NoopWhenSidebarHidden(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.showSidebar = false

	updated, _ := m.handleKeyMsg(keyCtrl('s'))
	m2 := updated.(Model)

	if m2.sidebarFocused {
		t.Error("expected sidebarFocused=false when sidebar is hidden")
	}
}

func TestRenderSidebar_ShowsSessionList(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "work"},
		{ChatID: -9000003, Name: "research"},
	}
	m.activeSession = -9000002

	sidebar := sidebarViewForTest(m)
	plain := stripANSIForTest(sidebar)

	if !containsStr(plain, "Aurelia") {
		t.Error("sidebar should show 'Aurelia' title")
	}
	if !containsStr(plain, "work") {
		t.Error("sidebar should show 'work' session")
	}
	if !containsStr(plain, "research") {
		t.Error("sidebar should show 'research' session")
	}
}

func TestRenderSidebar_FocusedShowsHints(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
	}

	sidebar := sidebarViewForTest(m)
	plain := stripANSIForTest(sidebar)

	if !containsStr(plain, "navigate") {
		t.Error("focused sidebar should show navigation hints")
	}
	if !containsStr(plain, "enter open") {
		t.Error("focused sidebar should show 'enter open' hint")
	}
}

func TestRenderChatHeader_ShowsActiveSessionName(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "work"},
	}
	m.activeSession = -9000002

	header := m.renderChatHeader()
	plain := stripANSIForTest(header)

	if !containsStr(plain, "Aurelia / work") {
		t.Errorf("header should show 'Aurelia / work', got: %s", plain)
	}
}

func TestRenderChatHeader_DefaultDM(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.activeSession = ipc.ReservedTUIChatID

	header := m.renderChatHeader()
	plain := stripANSIForTest(header)

	if !containsStr(plain, "Aurelia / DM") {
		t.Errorf("header should show 'Aurelia / DM', got: %s", plain)
	}
}

func TestSafeSessionLabel_StripsControlCharacters(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: "normal", expected: "normal"},
		{input: "line\nbreak", expected: "linebreak"},
		{input: "tab\there", expected: "tabhere"},
		{input: "carriage\rreturn", expected: "carriagereturn"},
		{input: "na\x00me", expected: "name"},
		{input: "\x1b[31mred\x1b[0m", expected: "red"},
		{input: "\x1b]0;title\x07name\x07", expected: "name"},
		{input: "na\x7fme", expected: "name"},
		{input: "mixed\n\t\rESC:\x1b[1mok", expected: "mixedESC:ok"},
		{input: "c1\x80test\x9f", expected: "c1test"},
		{input: "CSI:\x9b1m", expected: "CSI:1m"},
	}
	for _, tt := range tests {
		got := safeSessionLabel(tt.input)
		if got != tt.expected {
			t.Errorf("safeSessionLabel(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRenderSidebar_SanitizesLegacySessionNames(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.messages = append(m.messages, chatMessage{
		Sender: "Aurelia",
		Text:   "Connected.",
	})
	m.viewportSet = true
	m.width = 120
	m.height = 30
	// Legacy session name with ANSI escape, newline, and tab.
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "normal"},
		{ChatID: -9000003, Name: "mal\x1b[31micious\x1b[0m"},
		{ChatID: -9000004, Name: "bad\nname"},
		{ChatID: -9000005, Name: "tab\tname"},
	}
	m.activeSession = -9000003

	sidebar := sidebarViewForTest(m)
	plain := stripANSIForTest(sidebar)

	// The sanitized versions should appear in the sidebar, not the raw strings.
	if !containsStr(plain, "normal") {
		t.Error("sidebar should show 'normal' session")
	}
	if !containsStr(plain, "malicious") {
		t.Error("sidebar should show sanitized 'malicious' (ESC stripped)")
	}
	// Newline from "bad\nname" should be stripped — "badname" should appear.
	if !containsStr(plain, "badname") {
		t.Errorf("sidebar should show sanitized 'badname' (newline stripped), got: %q", plain)
	}
	// Tab from "tab\tname" should be stripped — "tabname" should appear.
	if !containsStr(plain, "tabname") {
		t.Errorf("sidebar should show sanitized 'tabname' (tab stripped), got: %q", plain)
	}
}

func TestRenderChatHeader_SanitizesLegacySessionNames(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "normal"},
		{ChatID: -9000003, Name: "hack\x1b[31med\x1b[0m"},
	}
	m.activeSession = -9000003

	header := m.renderChatHeader()
	plain := stripANSIForTest(header)

	if !containsStr(plain, "Aurelia / hacked") {
		t.Errorf("expected header to contain 'Aurelia / hacked' (ESC stripped), got: %s", plain)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && indexOfStr(s, substr) >= 0
}

func indexOfStr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
