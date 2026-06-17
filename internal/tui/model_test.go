package tui

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSIForTest(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}

func TestModel_InitialLoadingState(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	if m.state != stateLoading {
		t.Errorf("expected stateLoading, got %v", m.state)
	}
}

func TestModel_LoadingToErrorOnUnreachableDaemon(t *testing.T) {
	m := NewModel("/nonexistent/socket.sock")

	updated, _ := m.Update(daemonUnreachableMsg{err: errors.New("connection refused")})
	m2 := updated.(Model)

	if m2.state != stateError {
		t.Errorf("expected stateError, got %v", m2.state)
	}
	if m2.err == nil {
		t.Error("expected non-nil error")
	}
}

func TestModel_LoadingToChatOnReachableDaemon(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	updated, _ := m.Update(daemonReachableMsg{})
	m2 := updated.(Model)

	if m2.state != stateChat {
		t.Errorf("expected stateChat, got %v", m2.state)
	}
	if len(m2.messages) == 0 {
		t.Error("expected welcome message")
	}
}

func TestModel_SubmitNonEmptyTextCreatesSendMessage(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("hello")
	m := Model{
		state:      stateChat,
		ready:      true,
		messages:   []chatMessage{},
		textarea:   ta,
		waiting:    false,
		socketPath: "/tmp/test.sock",
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)

	if cmd == nil {
		t.Error("expected non-nil command after submit")
		return
	}
	if m2.waiting != true {
		t.Error("expected waiting=true after submit")
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea after submit, got %q", m2.textarea.Value())
	}
	if len(m2.messages) < 1 || m2.messages[0].Sender != "Igor" {
		t.Errorf("expected Igor message, got %+v", m2.messages)
	}
}

func TestModel_SubmitEmptyTextDoesNothing(t *testing.T) {
	m := Model{
		state:    stateChat,
		ready:    true,
		messages: []chatMessage{},
		textarea: textarea.New(),
		waiting:  false,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected nil command for empty text")
	}
	if m2.waiting {
		t.Error("expected waiting=false for empty text")
	}
}

func TestModel_SubmitWhileWaitingIsNoop(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("hello")
	m := Model{
		state:    stateChat,
		ready:    true,
		textarea: ta,
		waiting:  true,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected nil command when already waiting")
	}
	if m2.textarea.Value() != "hello" {
		t.Errorf("expected textarea unchanged, got %q", m2.textarea.Value())
	}
}

func TestModel_StreamChunkUpdatesMessage(t *testing.T) {
	m := Model{
		state:   stateChat,
		ready:   true,
		waiting: true,
	}

	// First chunk creates Aurelia message.
	updated, _ := m.handleStreamEvent(ipc.IPCEvent{Type: "stream_chunk", Body: "Hello"})
	m2 := updated.(Model)

	if len(m2.messages) != 1 || m2.messages[0].Sender != "Aurelia" {
		t.Fatalf("expected one Aurelia message, got %+v", m2.messages)
	}
	if m2.messages[0].Text != "Hello" {
		t.Errorf("expected text 'Hello', got %q", m2.messages[0].Text)
	}

	// Second chunk updates the same message.
	updated2, _ := m2.handleStreamEvent(ipc.IPCEvent{Type: "stream_chunk", Body: " World"})
	m3 := updated2.(Model)

	if len(m3.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m3.messages))
	}
	if m3.messages[0].Text != "Hello World" {
		t.Errorf("expected text 'Hello World', got %q", m3.messages[0].Text)
	}
}

func TestModel_StreamEndReturnsToReady(t *testing.T) {
	m := Model{
		state:     stateChat,
		ready:     true,
		waiting:   true,
		streamBuf: "some text",
	}

	updated, _ := m.handleStreamEvent(ipc.IPCEvent{Type: "stream_end"})
	m2 := updated.(Model)

	if m2.waiting {
		t.Error("expected waiting=false after stream_end")
	}
	if m2.streamBuf != "" {
		t.Error("expected streamBuf cleared after stream_end")
	}
	if m2.reader != nil {
		t.Error("expected reader cleared after stream_end")
	}
}

func TestModel_ErrorMessage(t *testing.T) {
	m := Model{
		state:   stateChat,
		ready:   true,
		waiting: true,
	}

	updated, _ := m.handleStreamEvent(ipc.IPCEvent{Type: "error", Error: "something failed"})
	m2 := updated.(Model)

	if m2.waiting {
		t.Error("expected waiting=false after error")
	}
	if len(m2.messages) < 1 {
		t.Fatal("expected error message")
	}
	if m2.messages[0].Sender != "⚠️" {
		t.Errorf("expected ⚠️ sender, got %q", m2.messages[0].Sender)
	}
	if m2.messages[0].Text != "something failed" {
		t.Errorf("expected error text, got %q", m2.messages[0].Text)
	}
	if m2.reader != nil {
		t.Error("expected reader cleared after error")
	}
}

func TestModel_CtrlLClearsMessages(t *testing.T) {
	m := Model{
		state:    stateChat,
		ready:    true,
		messages: []chatMessage{{Sender: "Igor", Text: "hello"}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for ctrl+l")
	}
	if len(m2.messages) != 0 {
		t.Errorf("expected empty messages, got %d", len(m2.messages))
	}
}

func TestModel_CtrlLShowsEmptyStateWhenViewportReady(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = 80
	m.height = 24
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}
	m.updateViewport()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	m2 := updated.(Model)

	if len(m2.messages) != 0 {
		t.Fatalf("expected empty messages, got %d", len(m2.messages))
	}
	content := stripANSIForTest(m2.viewport.View())
	if !strings.Contains(content, "Aurelia TUI") {
		t.Errorf("expected empty state after ctrl+l, got:\n%s", content)
	}
}

func TestModel_CtrlCQuits(t *testing.T) {
	m := Model{
		state: stateChat,
		ready: true,
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command for ctrl+c")
	}
}

func TestModel_EscCancelsStreaming(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.ready = true
	m.waiting = true
	m.streamBuf = "partial response"
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command after esc cancel")
	}
	if m2.waiting {
		t.Error("expected waiting=false after esc cancel")
	}
	if m2.streamBuf != "" {
		t.Error("expected streamBuf cleared after esc cancel")
	}
	if m2.reader != nil {
		t.Error("expected reader nil after esc cancel")
	}
	if len(m2.messages) != 2 {
		t.Fatalf("expected 2 messages (original + cancel), got %d", len(m2.messages))
	}
	last := m2.messages[len(m2.messages)-1]
	if last.Sender != "⚠️" || last.Text != "(cancelled)" {
		t.Errorf("expected cancel message, got sender=%q text=%q", last.Sender, last.Text)
	}
}

func TestModel_EscDoesNothingWhenNotWaiting(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.ready = true
	m.waiting = false
	m.textarea.SetValue("hello")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for esc when not waiting")
	}
	if m2.textarea.Value() != "hello" {
		t.Errorf("expected textarea unchanged, got %q", m2.textarea.Value())
	}
}

func TestModel_HealthCheckResultUpdatesDaemonLabel(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.daemonLabel = "ready"

	// Successful health check updates latency.
	updated, cmd := m.Update(healthCheckResultMsg{latency: 42 * time.Millisecond})
	m2 := updated.(Model)

	if m2.daemonLabel != "ready" {
		t.Errorf("expected daemonLabel=ready, got %q", m2.daemonLabel)
	}
	if m2.connectLatency != 42*time.Millisecond {
		t.Errorf("expected latency=42ms, got %v", m2.connectLatency)
	}
	if cmd == nil {
		t.Error("expected next health check to be scheduled")
	}

	// Failed health check marks daemon offline without going to error state.
	updated, _ = m.Update(healthCheckResultMsg{err: errors.New("timeout")})
	m3 := updated.(Model)

	if m3.daemonLabel != "offline" {
		t.Errorf("expected daemonLabel=offline, got %q", m3.daemonLabel)
	}
	if m3.state != stateChat {
		t.Errorf("expected state=chat (not error), got %v", m3.state)
	}
}

func TestModel_HealthCheckTickTriggersPing(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat

	updated, cmd := m.Update(healthCheckTickMsg{})
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command (health check ping)")
	}
	// Model should not change state from the tick itself.
	if m2.state != stateChat {
		t.Errorf("expected state=chat, got %v", m2.state)
	}
}

func TestModel_PageUpScrollsViewport(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = 100
	m.height = 12
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.viewport.SetContent(strings.Repeat("line\n", 40))
	m.viewport.GotoBottom()
	bottomOffset := m.viewport.YOffset

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for viewport scroll")
	}
	if m2.viewport.YOffset >= bottomOffset {
		t.Fatalf("expected page up to reduce viewport offset from %d, got %d", bottomOffset, m2.viewport.YOffset)
	}
}

func TestModel_MouseWheelScrollsViewport(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = 100
	m.height = 12
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.viewport.SetContent(strings.Repeat("line\n", 40))
	m.viewport.GotoBottom()
	bottomOffset := m.viewport.YOffset

	updated, _ := m.Update(tea.MouseMsg{
		Type:   tea.MouseWheelUp,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	m2 := updated.(Model)

	if m2.viewport.YOffset >= bottomOffset {
		t.Fatalf("expected mouse wheel up to reduce viewport offset from %d, got %d", bottomOffset, m2.viewport.YOffset)
	}
}

func TestModel_CtrlUDelegatesToTextarea(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.ready = true
	m.textarea.SetValue("hello world")
	m.textarea.CursorEnd()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m2 := updated.(Model)

	if m2.textarea.Value() == "hello world" {
		t.Fatal("expected ctrl+u to edit textarea, but value was unchanged")
	}
}

func TestModel_TabTogglesSidebar(t *testing.T) {
	m := Model{
		state:       stateChat,
		ready:       true,
		showSidebar: false,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := updated.(Model)

	if !m2.showSidebar {
		t.Error("expected showSidebar=true after tab")
	}

	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyTab})
	m3 := updated2.(Model)

	if m3.showSidebar {
		t.Error("expected showSidebar=false after second tab")
	}
}

func TestModel_TabRuneDoesNotWriteToInput(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.showSidebar = false

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\t'}})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for tab rune toggle")
	}
	if !m2.showSidebar {
		t.Error("expected sidebar toggled by tab rune")
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea after tab, got %q", m2.textarea.Value())
	}
}

func TestModel_UnhandledAltShortcutDoesNotWriteToInput(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.ready = true
	m.textarea.SetValue("hello")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}, Alt: true})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for ignored alt shortcut")
	}
	if m2.textarea.Value() != "hello" {
		t.Errorf("expected textarea unchanged after alt+s, got %q", m2.textarea.Value())
	}
}

func TestModel_TerminalColorReportDoesNotWriteToInput(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.ready = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1;rgb:158e/193a/1e75")})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for terminal color report residue")
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea after terminal color report, got %q", m2.textarea.Value())
	}
}

func TestModel_PastedTerminalColorLikeTextWritesToInput(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.ready = true

	text := "1;rgb:158e/193a/1e75"
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	m2 := updated.(Model)

	if m2.textarea.Value() != text {
		t.Errorf("expected pasted terminal-color-like text to remain, got %q", m2.textarea.Value())
	}
}

func TestModel_TextareaAltShortcutDoesNotWriteLiteralRune(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.ready = true
	m.textarea.SetValue("hello world")
	m.textarea.CursorStart()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}, Alt: true})
	m2 := updated.(Model)

	if m2.textarea.Value() != "hello world" {
		t.Errorf("expected textarea shortcut not to insert literal f, got %q", m2.textarea.Value())
	}
	if got := m2.textarea.LineInfo().CharOffset; got != len("hello") {
		t.Errorf("expected alt+f to move cursor to first word end, got offset %d", got)
	}
}

func TestModel_AltEnterInsertsNewline(t *testing.T) {
	ta := textarea.New()
	m := Model{
		state:    stateChat,
		ready:    true,
		textarea: ta,
		waiting:  false,
	}

	updated, cmd := m.Update(tea.KeyMsg{
		Type: tea.KeyEnter,
		Alt:  true,
	})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil cmd for alt+enter")
	}
	if m2.textarea.Value() != "\n" {
		t.Errorf("expected newline in textarea, got %q", m2.textarea.Value())
	}
}

func TestModel_CtrlJInsertsNewline(t *testing.T) {
	ta := textarea.New()
	m := Model{
		state:    stateChat,
		ready:    true,
		textarea: ta,
		waiting:  false,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil cmd for ctrl+j")
	}
	if m2.textarea.Value() != "\n" {
		t.Errorf("expected newline in textarea, got %q", m2.textarea.Value())
	}
}

func TestInputTextareaWidthLeavesRoomForPrompt(t *testing.T) {
	terminalWidth := 80
	got := inputTextareaWidth(terminalWidth)
	boxWidth := inputBoxContentWidth(terminalWidth)

	if got >= boxWidth {
		t.Fatalf("expected textarea width %d to be less than box width %d", got, boxWidth)
	}
	if got >= terminalWidth-4 {
		t.Fatalf("expected textarea width %d to be narrower than old terminal-based width", got)
	}
}

func TestRenderPromptedTextareaIndentsContinuationLines(t *testing.T) {
	got := stripANSIForTest(renderPromptedTextarea("> ", "> ", "first\nsecond"))
	want := "> first\n  second"

	if got != want {
		t.Fatalf("expected prompted textarea %q, got %q", want, got)
	}
}

func TestModel_SendCommandForSlashText(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/status")
	m := Model{
		state:      stateChat,
		ready:      true,
		textarea:   ta,
		waiting:    false,
		socketPath: "/tmp/test.sock",
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected command for /status")
	}
	if m2.waiting != true {
		t.Error("expected waiting=true after command submit")
	}
}

func TestModel_DaemonErrorDoesNotPanic(t *testing.T) {
	m := Model{
		state: stateChat,
		ready: true,
	}

	updated, _ := m.Update(daemonErrorMsg{err: errors.New("something went wrong")})
	m2 := updated.(Model)

	if m2.waiting {
		t.Error("expected waiting=false after error")
	}
	if len(m2.messages) == 0 {
		t.Fatal("expected error message in chat")
	}
}

func TestModel_InputCharacterHandling(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.ready = true

	// Simulate typing "hello" via textarea updates.
	chars := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'h'}},
		{Type: tea.KeyRunes, Runes: []rune{'e'}},
		{Type: tea.KeyRunes, Runes: []rune{'l'}},
		{Type: tea.KeyRunes, Runes: []rune{'l'}},
		{Type: tea.KeyRunes, Runes: []rune{'o'}},
	}
	for _, k := range chars {
		var cmd tea.Cmd
		updated, cmd := m.Update(k)
		m = updated.(Model)
		if cmd != nil {
			_ = cmd()
		}
	}

	if m.textarea.Value() != "hello" {
		t.Errorf("expected textarea 'hello', got %q", m.textarea.Value())
	}

	// Backspace should remove last char via textarea.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = m2.(Model)
	if m.textarea.Value() != "hell" {
		t.Errorf("expected textarea 'hell', got %q", m.textarea.Value())
	}
}

func TestModel_ErrorStateKeys(t *testing.T) {
	m := Model{
		state: stateError,
		err:   errors.New("test error"),
	}

	// Ctrl+C in error state quits.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("expected quit command in error state")
	}

	// Enter in error state retries (goes to loading).
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)
	if m2.state != stateLoading {
		t.Errorf("expected stateLoading after enter in error, got %v", m2.state)
	}
}

func TestModel_AppendOrUpdateAureliaMessage(t *testing.T) {
	tests := []struct {
		name     string
		initial  []chatMessage
		text     string
		expected int
		lastText string
	}{
		{
			name:     "empty creates new",
			initial:  []chatMessage{},
			text:     "hello",
			expected: 1,
			lastText: "hello",
		},
		{
			name: "last is Igor creates new",
			initial: []chatMessage{
				{Sender: "Igor", Text: "hi"},
			},
			text:     "world",
			expected: 2,
			lastText: "world",
		},
		{
			name: "last is Aurelia updates",
			initial: []chatMessage{
				{Sender: "Aurelia", Text: "hel"},
			},
			text:     "hello",
			expected: 1,
			lastText: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{messages: tt.initial}
			m.appendOrUpdateAureliaMessage(tt.text)
			if len(m.messages) != tt.expected {
				t.Errorf("expected %d messages, got %d", tt.expected, len(m.messages))
			}
			if m.messages[len(m.messages)-1].Text != tt.lastText {
				t.Errorf("expected last text %q, got %q", tt.lastText, m.messages[len(m.messages)-1].Text)
			}
		})
	}
}

func TestModel_WelcomeMessageOnConnect(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	updated, _ := m.Update(daemonReachableMsg{})
	m2 := updated.(Model)

	if m2.state != stateChat {
		t.Errorf("expected stateChat, got %v", m2.state)
	}
	if len(m2.messages) < 1 {
		t.Fatal("expected at least one message")
	}
	if m2.messages[0].Sender != "Aurelia" {
		t.Errorf("expected Aurelia welcome, got %q", m2.messages[0].Sender)
	}
}

func TestModel_MessageReplacesStreamBuf(t *testing.T) {
	m := Model{
		state:     stateChat,
		ready:     true,
		waiting:   true,
		streamBuf: "partial text",
	}

	// "message" event replaces, not appends to, streamBuf.
	updated, _ := m.handleStreamEvent(ipc.IPCEvent{Type: "message", Body: "final text"})
	m2 := updated.(Model)

	if m2.streamBuf != "final text" {
		t.Errorf("expected streamBuf 'final text', got %q", m2.streamBuf)
	}
	if len(m2.messages) < 1 {
		t.Fatal("expected at least one message")
	}
	if m2.messages[len(m2.messages)-1].Text != "final text" {
		t.Errorf("expected last message text 'final text', got %q", m2.messages[len(m2.messages)-1].Text)
	}
}

func TestModel_StreamDoneMsgUnblocks(t *testing.T) {
	m := Model{
		state:   stateChat,
		ready:   true,
		waiting: true,
	}

	updated, _ := m.Update(streamDoneMsg{})
	m2 := updated.(Model)

	if m2.waiting {
		t.Error("expected waiting=false after streamDoneMsg")
	}
	if m2.reader != nil {
		t.Error("expected reader cleared after streamDoneMsg")
	}
}

func TestModel_StreamErrMsgUnblocks(t *testing.T) {
	m := Model{
		state:   stateChat,
		ready:   true,
		waiting: true,
	}

	updated, _ := m.Update(streamErrMsg{err: errors.New("connection lost")})
	m2 := updated.(Model)

	if m2.waiting {
		t.Error("expected waiting=false after streamErrMsg")
	}
	if m2.reader != nil {
		t.Error("expected reader cleared after streamErrMsg")
	}
	if len(m2.messages) == 0 {
		t.Fatal("expected error message")
	}
	if m2.messages[len(m2.messages)-1].Sender != "⚠️" {
		t.Errorf("expected ⚠️ sender, got %q", m2.messages[len(m2.messages)-1].Sender)
	}
}

func TestModel_NewlineFallbackKeyConstant(t *testing.T) {
	if newlineFallbackKey != "alt+enter" {
		t.Errorf("expected newlineFallbackKey='alt+enter', got %q", newlineFallbackKey)
	}
}

// Regression: loading receives WindowSizeMsg, then daemonReachableMsg →
// viewportSet=true and rendered content includes welcome/Aurelia text.
func TestModel_ViewportInitializedFromLoadingWindowSizeMsg(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	// WindowSizeMsg arrives during loading.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m2 := updated.(Model)

	if m2.width != 100 {
		t.Errorf("expected width=100, got %d", m2.width)
	}
	if m2.height != 40 {
		t.Errorf("expected height=40, got %d", m2.height)
	}
	if m2.viewportSet {
		t.Error("viewport should not be set yet — still in stateLoading")
	}

	// Daemon becomes reachable.
	updated, _ = m2.Update(daemonReachableMsg{})
	m3 := updated.(Model)

	if m3.state != stateChat {
		t.Errorf("expected stateChat, got %v", m3.state)
	}
	if !m3.viewportSet {
		t.Fatal("expected viewportSet after daemonReachableMsg with stored dimensions")
	}

	vpContent := m3.viewport.View()
	if !strings.Contains(vpContent, "Aurelia") {
		t.Errorf("expected viewport to contain 'Aurelia', got:\n%s", vpContent)
	}
}

// Regression: loading WindowSizeMsg, daemon reachable, user submits text,
// then receives message → rendered content includes both user and assistant text.
func TestModel_ViewportShowsIgorAndAureliaAfterFullFlow(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	// WindowSizeMsg during loading stores dimensions.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m2 := updated.(Model)

	// Daemon reachable transitions to chat and initializes viewport.
	updated, _ = m2.Update(daemonReachableMsg{})
	m3 := updated.(Model)

	if !m3.viewportSet {
		t.Fatal("viewport should be initialized before text submission")
	}

	// Set textarea to a non-empty value and submit.
	ta := textarea.New()
	ta.SetValue("hello")
	m3.textarea = ta
	m3.waiting = false

	updated, cmd := m3.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m4 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command after submit")
	}

	// Igor message should be visible in viewport immediately.
	vpContent := m4.viewport.View()
	if !strings.Contains(vpContent, "hello") {
		t.Errorf("expected viewport to contain 'hello', got:\n%s", vpContent)
	}

	// Receive a final message event from the daemon.
	updated, _ = m4.handleStreamEvent(ipc.IPCEvent{Type: "message", Body: "ok tui"})
	m5 := updated.(Model)

	vpContent2 := stripANSIForTest(m5.viewport.View())
	if !strings.Contains(vpContent2, "hello") {
		t.Errorf("expected viewport to contain 'hello', got:\n%s", vpContent2)
	}
	if !strings.Contains(vpContent2, "ok tui") {
		t.Errorf("expected viewport to contain 'ok tui', got:\n%s", vpContent2)
	}
}

// Edge: updateViewport with no dimensions still no-ops safely.
func TestModel_UpdateViewportNoopsWithoutDimensions(t *testing.T) {
	m := Model{
		state:    stateChat,
		ready:    true,
		messages: []chatMessage{{Sender: "Igor", Text: "hello"}},
	}

	// Should not panic and should not initialize viewport.
	m.updateViewport()

	if m.viewportSet {
		t.Error("expected viewportSet to remain false")
	}
	if m.viewport.Height != 0 {
		t.Errorf("expected viewport height 0, got %d", m.viewport.Height)
	}
}

func TestModel_DefaultChromeState(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	if !m.showSidebar {
		t.Error("expected sidebar enabled by default")
	}
	if m.daemonLabel != "connecting" {
		t.Errorf("expected daemonLabel=connecting, got %q", m.daemonLabel)
	}
	if m.cwdPath != "not set" {
		t.Errorf("expected cwdPath=not set, got %q", m.cwdPath)
	}
}

func TestModel_TextareaPromptDisabledForCustomInputChrome(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	if m.textarea.Prompt != "" {
		t.Fatalf("expected textarea internal prompt disabled, got %q", m.textarea.Prompt)
	}
}

func TestModel_ChatViewStartsWithTopMargin(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = 100
	m.height = 40
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true

	view := m.View()
	firstLine, _, _ := strings.Cut(view, "\n")

	if len(firstLine) != m.width {
		t.Fatalf("expected top margin width %d, got %d", m.width, len(firstLine))
	}
	if strings.TrimSpace(firstLine) != "" {
		t.Fatalf("expected blank top margin, got %q", firstLine)
	}
}

func TestModel_StatusBarUsesCompactShortcuts(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = 150

	status := m.renderStatusBar()

	for _, want := range []string{"↵ send", "alt+enter newline", "esc cancel", "⌃L clear", "tab sidebar", "⌃C quit"} {
		if !strings.Contains(status, want) {
			t.Errorf("expected status bar to contain %q, got %q", want, status)
		}
	}
}

func TestModel_StatusBarDropsItemsOnNarrowTerminal(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = 50

	status := stripANSIForTest(m.renderStatusBar())

	// On a narrow terminal, low-priority items should be dropped.
	if strings.Contains(status, "⌃C quit") {
		t.Errorf("expected '⌃C quit' to be dropped on width=50, got %q", status)
	}
	if strings.Contains(status, "tab sidebar") {
		t.Errorf("expected 'tab sidebar' to be dropped on width=50, got %q", status)
	}
	// High-priority items should remain.
	if !strings.Contains(status, "↵ send") {
		t.Errorf("expected '↵ send' to remain on width=50, got %q", status)
	}
}

func TestModel_StatusBarNeverWraps(t *testing.T) {
	for _, width := range []int{40, 60, 80, 100, 120, 150} {
		m := NewModel("/tmp/test.sock")
		m.state = stateChat
		m.width = width

		status := stripANSIForTest(m.renderStatusBar())
		lineCount := strings.Count(status, "\n") + 1

		if lineCount > 1 {
			t.Fatalf("width %d: expected status bar to be 1 line, got %d: %q", width, lineCount, status)
		}
	}
}

func TestModel_ChatHeaderShowsProjectAndDaemon(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = 100
	m.height = 40
	m.cwdPath = "/Users/igor/dev/aurelia"
	m.daemonLabel = "ready"

	header := stripANSIForTest(m.renderChatHeader())

	for _, want := range []string{"Aurelia / DM", "project aurelia", "daemon ready"} {
		if !strings.Contains(header, want) {
			t.Errorf("expected header to contain %q, got %q", want, header)
		}
	}
}

func TestModel_ChatHeaderShowsThinkingWhenWaiting(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = 100
	m.height = 40
	m.waiting = true

	header := stripANSIForTest(m.renderChatHeader())

	if !strings.Contains(header, "thinking") {
		t.Errorf("expected header to show 'thinking' when waiting, got %q", header)
	}
}

func TestModel_EmptyStateShowsWelcome(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = 80
	m.height = 24
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.messages = nil
	m.updateViewport()

	content := stripANSIForTest(m.viewport.View())

	for _, want := range []string{"Aurelia TUI", "/help", "/cwd"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected empty state to contain %q, got:\n%s", want, content)
		}
	}
}

func TestModel_MessageRenderingUsesBlockFormat(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = 80
	m.height = 40
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.messages = []chatMessage{
		{Sender: "Igor", Text: "hello world", Timestamp: time.Now()},
		{Sender: "Aurelia", Text: "hi there", Timestamp: time.Now()},
	}
	m.updateViewport()

	content := stripANSIForTest(m.viewport.View())

	// User messages get a ▶ marker and separator line.
	if !strings.Contains(content, "▶ Igor") {
		t.Errorf("expected '▶ Igor' marker in rendered messages, got:\n%s", content)
	}
	if !strings.Contains(content, "hello world") {
		t.Errorf("expected user text in rendered messages, got:\n%s", content)
	}
	// Aurelia messages get a ▶ marker.
	if !strings.Contains(content, "▶ Aurelia") {
		t.Errorf("expected '▶ Aurelia' marker in rendered messages, got:\n%s", content)
	}
	if !strings.Contains(content, "hi there") {
		t.Errorf("expected assistant text in rendered messages, got:\n%s", content)
	}
}

func TestModel_ViewportHeightAccountsForChatHeader(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.width = 100
	m.height = 40

	vp := viewportForSize(m.contentWidth(), m.height)
	want := m.height - inputHeight - statusBarHeight - topMarginHeight - chatHeaderHeight

	if vp.Height != want {
		t.Fatalf("expected viewport height %d, got %d", want, vp.Height)
	}
}

func TestModel_ViewportShrinksBelowMinimumOnVeryShortTerminal(t *testing.T) {
	for _, height := range []int{12, 13} {
		m := NewModel("/tmp/test.sock")
		m.width = minSidebarScreenWidth
		m.height = height

		vp := viewportForSize(m.contentWidth(), m.height)
		want := height - inputHeight - statusBarHeight - topMarginHeight - chatHeaderHeight

		if vp.Height != want {
			t.Fatalf("height %d: expected viewport height %d, got %d", height, want, vp.Height)
		}
	}
}

func TestModel_TuiStatusUpdatesCWD(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat

	updated, _ := m.Update(tuiStatusMsg{cwd: "/Users/igor/dev/aurelia"})
	m2 := updated.(Model)

	if m2.cwdPath != "/Users/igor/dev/aurelia" {
		t.Errorf("expected cwdPath updated, got %q", m2.cwdPath)
	}
}

func TestModel_MessageEventUpdatesCWDForSidebar(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat

	updated, _ := m.handleStreamEvent(ipc.IPCEvent{
		Type: ipc.EventTypeMessage,
		Body: "✅ Project set to: `/Users/igor/dev/aurelia`",
	})
	m2 := updated.(Model)

	if m2.cwdPath != "/Users/igor/dev/aurelia" {
		t.Errorf("expected cwdPath from message event, got %q", m2.cwdPath)
	}
}

func TestModel_SidebarHiddenOnNarrowTerminal(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.width = minSidebarScreenWidth - 1
	m.height = minSidebarScreenHeight

	if m.shouldShowSidebar() {
		t.Error("expected sidebar hidden on narrow terminal")
	}
}

func TestModel_SidebarHiddenOnShortTerminal(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.width = minSidebarScreenWidth
	m.height = minSidebarScreenHeight - 1

	if m.shouldShowSidebar() {
		t.Error("expected sidebar hidden on short terminal")
	}
}

func TestModel_ChatViewDoesNotExceedShortTerminalHeight(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	m.state = stateChat
	m.width = minSidebarScreenWidth
	m.height = minSidebarScreenHeight - 1
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true

	view := m.View()
	lineCount := strings.Count(view, "\n") + 1

	if lineCount > m.height {
		t.Fatalf("expected view height <= %d lines, got %d", m.height, lineCount)
	}
}

func TestModel_ChatViewDoesNotExceedVeryShortTerminalHeight(t *testing.T) {
	for _, height := range []int{12, 13} {
		m := NewModel("/tmp/test.sock")
		m.state = stateChat
		m.width = minSidebarScreenWidth
		m.height = height
		m.viewport = viewportForSize(m.contentWidth(), m.height)
		m.viewportSet = true

		view := m.View()
		lineCount := strings.Count(view, "\n") + 1

		if lineCount > m.height {
			t.Fatalf("height %d: expected view height <= %d lines, got %d", height, height, lineCount)
		}
	}
}
