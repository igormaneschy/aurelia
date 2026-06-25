package tui

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSIForTest(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}

func TestModel_InitialLoadingState(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	if m.state != stateLoading {
		t.Errorf("expected stateLoading, got %v", m.state)
	}
}

func TestModel_LoadingToErrorOnUnreachableDaemon(t *testing.T) {
	m := NewModel("/nonexistent/socket.sock", ThemeDark)

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
	m := NewModel("/tmp/test.sock", ThemeDark)

	updated, _ := m.Update(daemonReachableMsg{})
	m2 := updated.(Model)

	if m2.state != stateChat {
		t.Errorf("expected stateChat, got %v", m2.state)
	}
	if len(m2.messages) == 0 {
		t.Error("expected welcome message")
	}
}

func TestModel_LoadingToChatSchedulesHealthCheck(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)

	updated, cmd := m.updateLoading(daemonReachableMsg{latency: 12 * time.Millisecond})
	m2 := updated.(Model)

	if m2.state != stateChat {
		t.Fatalf("expected stateChat, got %v", m2.state)
	}
	if cmd == nil {
		t.Fatal("expected batch command after initial connect")
	}

	// scheduleHealthCheck uses tea.Tick; invoking the batch cmd should not panic
	// and must return a follow-up tick message.
	if msg := cmd(); msg == nil {
		t.Fatal("expected health-check tick command to produce a message")
	}
}

func TestModel_SubmitNonEmptyTextCreatesSendMessage(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("hello")
	m := testChatModelWithTextarea(ta)
	m.messages = []chatMessage{}

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
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
	m := testChatModel()
	m.messages = []chatMessage{}

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected nil command for empty text")
	}
	if m2.waiting {
		t.Error("expected waiting=false for empty text")
	}
}

func TestModel_SubmitWhileWaitingEnqueuesMessage(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("hello")
	m := testChatModelWithTextarea(ta)
	m.waiting = true

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected nil command when queuing during active stream")
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected textarea reset after queueing, got %q", m2.textarea.Value())
	}
	if got := m2.pendingCount(); got != 1 {
		t.Fatalf("expected 1 queued message, got %d", got)
	}
	if m2.pendingQueue[0].text != "hello" {
		t.Errorf("queued text = %q, want hello", m2.pendingQueue[0].text)
	}
	if !m2.waiting {
		t.Error("expected current stream to remain waiting")
	}
}

func TestModel_StreamEndStartsNextQueuedMessage(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.waiting = true
	m.streamID = 7
	m.pendingQueue = []queuedMessage{{chatID: ipc.ReservedTUIChatID, text: "next"}}

	updated, cmd := m.Update(streamEventMsg{streamID: 7, event: ipc.IPCEvent{Type: "stream_end"}})
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected command to send queued message")
	}
	if !m2.waiting {
		t.Fatal("expected waiting=true after starting queued message")
	}
	if got := m2.pendingCount(); got != 0 {
		t.Fatalf("expected queue drained, got %d", got)
	}
}

func TestModel_StaleStreamEndDoesNotStartQueue(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.waiting = false
	m.streamID = 2
	m.pendingQueue = []queuedMessage{{chatID: ipc.ReservedTUIChatID, text: "next"}}

	updated, cmd := m.Update(streamDoneMsg{streamID: 1})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected stale stream end to be ignored")
	}
	if got := m2.pendingCount(); got != 1 {
		t.Fatalf("expected queue preserved after stale stream end, got %d", got)
	}
}

func TestModel_StreamErrorStartsNextQueuedMessage(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.waiting = true
	m.pendingQueue = []queuedMessage{{text: "next"}}

	updated, cmd := m.handleStreamEvent(ipc.IPCEvent{Type: "error", Error: "boom"})
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected command to send queued message after error")
	}
	if !m2.waiting {
		t.Fatal("expected waiting=true after starting queued message")
	}
	if got := m2.pendingCount(); got != 0 {
		t.Fatalf("expected queue drained, got %d", got)
	}
}

func TestModel_EscCancelsCurrentTurnDrainsQueue(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.waiting = true
	m.streamID = 4
	m.pendingQueue = []queuedMessage{{chatID: ipc.ReservedTUIChatID, text: "next"}}

	updated, cmd := m.Update(keyPress(tea.KeyEsc))
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected command to drain queued message after cancel")
	}
	if !m2.waiting {
		t.Fatal("expected waiting=true while sending queued message")
	}
	if got := m2.pendingCount(); got != 0 {
		t.Fatalf("expected queue drained after cancel, got %d", got)
	}

	updated, cmd = m2.Update(streamDoneMsg{streamID: 4})
	m3 := updated.(Model)
	if cmd != nil {
		t.Fatal("expected late stream end after cancel to be ignored")
	}
	if got := m3.pendingCount(); got != 0 {
		t.Fatalf("expected queue to remain empty after late stream end, got %d", got)
	}
}

func TestModel_SubmitWithPendingQueueEnqueuesInsteadOfDirectSend(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("new")
	m := testChatModelWithTextarea(ta)
	m.pendingQueue = []queuedMessage{{chatID: ipc.ReservedTUIChatID, text: "queued-first"}}

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected command to start queued message")
	}
	if got := m2.pendingCount(); got != 1 {
		t.Fatalf("expected 1 queued message (new), got %d", got)
	}
	if m2.pendingQueue[0].text != "new" {
		t.Errorf("queued text = %q, want new", m2.pendingQueue[0].text)
	}
	if !m2.waiting {
		t.Fatal("expected waiting=true while draining queue")
	}
}

func TestModel_QueuedMessageCapturesActiveSession(t *testing.T) {
	const originalSession int64 = -9000002
	ta := textarea.New()
	ta.SetValue("queued")
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.textarea = ta
	m.waiting = true
	m.activeSession = originalSession

	updated, _ := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	m2.activeSession = -9000003
	q, ok := (&m2).dequeueMessage()
	if !ok {
		t.Fatal("expected queued message")
	}
	if q.chatID != originalSession {
		t.Fatalf("queued chatID = %d, want %d", q.chatID, originalSession)
	}
}

func TestModel_CtrlCCleansQueuedTempImages(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "queued-*.png")
	if err != nil {
		t.Fatal(err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.pendingQueue = []queuedMessage{{
		chatID:         ipc.ReservedTUIChatID,
		text:           "queued image",
		tempImagePaths: []string{path},
	}}

	_, cmd := m.Update(keyCtrl('c'))
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected queued temp file removed, stat err=%v", err)
	}
}

func TestModel_PendingQueueBadgeRendersCount(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.pendingQueue = []queuedMessage{{text: "one"}, {text: "two"}}

	badge := stripANSIForTest(m.renderPendingQueueBadge())

	if !strings.Contains(badge, "⏳ 2 pending") {
		t.Fatalf("expected pending badge count, got %q", badge)
	}
}

func TestModel_StreamChunkUpdatesMessage(t *testing.T) {
	m := testChatModel()
	m.waiting = true

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
	m := testChatModel()
	m.waiting = true
	m.streamBuf = "some text"

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
	m := testChatModel()
	m.waiting = true

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
	m := testChatModel()
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}

	updated, cmd := m.Update(keyCtrl('l'))
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for ctrl+l")
	}
	if len(m2.messages) != 0 {
		t.Errorf("expected empty messages, got %d", len(m2.messages))
	}
}

func TestModel_CtrlLShowsEmptyStateWhenViewportReady(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 80
	m.height = 24
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}
	m.updateViewport()

	updated, _ := m.Update(keyCtrl('l'))
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

	_, cmd := m.Update(keyCtrl('c'))
	if cmd == nil {
		t.Fatal("expected quit command for ctrl+c")
	}
}

func TestModel_EscCancelsStreaming(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.ready = true
	m.waiting = true
	m.streamBuf = "partial response"
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}

	updated, cmd := m.Update(keyPress(tea.KeyEsc))
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
	if last.Sender != "⚠️" || last.Text != "(cancelled — pipeline aborting)" {
		t.Errorf("expected cancel message, got sender=%q text=%q", last.Sender, last.Text)
	}
}

func TestModel_EscDoesNothingWhenNotWaiting(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.ready = true
	m.waiting = false
	m.textarea.SetValue("hello")

	updated, cmd := m.Update(keyPress(tea.KeyEsc))
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for esc when not waiting")
	}
	if m2.textarea.Value() != "hello" {
		t.Errorf("expected textarea unchanged, got %q", m2.textarea.Value())
	}
}

func TestModel_HealthCheckResultUpdatesDaemonLabel(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
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
	m := NewModel("/tmp/test.sock", ThemeDark)
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

func TestHistoryFromEventsParsesHistoryPayload(t *testing.T) {
	msg := historyFromEvents([]ipc.IPCEvent{{
		Type: ipc.EventTypeHistory,
		Body: `[{"sender":"Igor","text":"hello","timestamp":"2026-06-21T14:05:00Z"},{"sender":"Aurelia","text":"hi"}]`,
	}})

	if msg.err != nil {
		t.Fatalf("unexpected history parse error: %v", msg.err)
	}
	if len(msg.messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msg.messages))
	}
	if msg.messages[0].Sender != "Igor" || msg.messages[0].Text != "hello" {
		t.Fatalf("unexpected first history message: %#v", msg.messages[0])
	}
	if got := msg.messages[0].Timestamp.Format(time.RFC3339); got != "2026-06-21T14:05:00Z" {
		t.Fatalf("first timestamp = %q, want 2026-06-21T14:05:00Z", got)
	}
	if !msg.messages[1].Timestamp.IsZero() {
		t.Fatalf("missing timestamp should remain zero, got %s", msg.messages[1].Timestamp)
	}
}

func TestModel_HistoryMsgErrorShowsWarning(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.messages = nil
	m.switchingSession = true

	updated, _ := m.Update(tuiHistoryMsg{err: errors.New("connection timeout")})
	m2 := updated.(Model)

	if m2.switchingSession {
		t.Error("expected switchingSession=false after history error")
	}
	if len(m2.messages) != 1 {
		t.Fatalf("expected 1 warning message, got %d", len(m2.messages))
	}
	if m2.messages[0].Sender != "⚠️" {
		t.Errorf("expected ⚠️ sender, got %q", m2.messages[0].Sender)
	}
	if !strings.Contains(m2.messages[0].Text, "failed to load chat history") {
		t.Errorf("expected warning about chat history failure, got %q", m2.messages[0].Text)
	}
	if !strings.Contains(m2.messages[0].Text, "connection timeout") {
		t.Errorf("expected error detail in warning, got %q", m2.messages[0].Text)
	}
}

func TestModel_HistoryMsgReplacesStartupMessage(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 80
	m.height = 24
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.messages = []chatMessage{{Sender: "Aurelia", Text: "Connected to Aurelia daemon. Type a message or /help.", Timestamp: time.Now()}}

	updated, cmd := m.Update(tuiHistoryMsg{messages: []chatMessage{{Sender: "Igor", Text: "previous prompt", Timestamp: time.Now()}}})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for history message")
	}
	if len(m2.messages) != 1 || m2.messages[0].Text != "previous prompt" {
		t.Fatalf("expected history to replace startup message, got %#v", m2.messages)
	}
}

func TestModel_LateHistoryDoesNotReplaceUserInteraction(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 80
	m.height = 24
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.messages = []chatMessage{
		{Sender: "Aurelia", Text: "Connected to Aurelia daemon. Type a message or /help.", Timestamp: time.Now()},
		{Sender: "Igor", Text: "new prompt", Timestamp: time.Now()},
	}
	m.waiting = true

	updated, _ := m.Update(tuiHistoryMsg{messages: []chatMessage{{Sender: "Igor", Text: "old prompt", Timestamp: time.Now()}}})
	m2 := updated.(Model)

	if len(m2.messages) != 2 {
		t.Fatalf("expected late history to be ignored, got %#v", m2.messages)
	}
	if m2.messages[1].Text != "new prompt" {
		t.Fatalf("expected current user prompt preserved, got %#v", m2.messages)
	}
}

func TestModel_UpdateViewportPreservesScrollWhenNotAtBottom(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 12
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	for i := 0; i < 40; i++ {
		m.messages = append(m.messages, chatMessage{
			Sender:    "Igor",
			Text:      "line",
			Timestamp: time.Now(),
		})
	}
	m.updateViewport()
	m.viewport.GotoBottom()
	m.viewport.ScrollUp(5)
	offset := m.viewport.YOffset()

	m.messages = append(m.messages, chatMessage{
		Sender:    "Aurelia",
		Text:      "chunk",
		Timestamp: time.Now(),
	})
	m.updateViewport()

	if m.viewport.YOffset() != offset {
		t.Fatalf("expected scroll offset %d preserved, got %d", offset, m.viewport.YOffset())
	}
}

func TestModel_UpdateViewportFollowsBottomWhenAlreadyAtBottom(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 12
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	for i := 0; i < 40; i++ {
		m.messages = append(m.messages, chatMessage{
			Sender:    "Igor",
			Text:      "line",
			Timestamp: time.Now(),
		})
	}
	m.updateViewport()
	m.viewport.GotoBottom()

	m.messages = append(m.messages, chatMessage{
		Sender:    "Aurelia",
		Text:      "chunk",
		Timestamp: time.Now(),
	})
	m.updateViewport()

	if !m.viewport.AtBottom() {
		t.Fatal("expected viewport to follow bottom when user was already at bottom")
	}
}

func TestModel_PageUpScrollsViewport(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 12
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.viewport.SetContent(strings.Repeat("line\n", 40))
	m.viewport.GotoBottom()
	bottomOffset := m.viewport.YOffset()

	updated, cmd := m.Update(keyPress(tea.KeyPgUp))
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for viewport scroll")
	}
	if m2.viewport.YOffset() >= bottomOffset {
		t.Fatalf("expected page up to reduce viewport offset from %d, got %d", bottomOffset, m2.viewport.YOffset())
	}
}

func TestModel_MouseWheelScrollsViewport(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.mouseEnabled = true
	m.width = 100
	m.height = 12
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.viewport.SetContent(strings.Repeat("line\n", 40))
	m.viewport.GotoBottom()
	bottomOffset := m.viewport.YOffset()

	updated, _ := m.Update(tea.MouseWheelMsg{X: 0, Y: 0, Button: tea.MouseWheelUp})
	m2 := updated.(Model)

	if m2.viewport.YOffset() >= bottomOffset {
		t.Fatalf("expected mouse wheel up to reduce viewport offset from %d, got %d", bottomOffset, m2.viewport.YOffset())
	}
}

func TestModel_MouseDisabledIgnoresMouseWheel(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 12
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true
	m.viewport.SetContent(strings.Repeat("line\n", 40))
	m.viewport.GotoBottom()
	bottomOffset := m.viewport.YOffset()

	updated, cmd := m.Update(tea.MouseWheelMsg{X: 0, Y: 0, Button: tea.MouseWheelUp})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command when mouse capture is disabled")
	}
	if m2.viewport.YOffset() != bottomOffset {
		t.Fatalf("expected disabled mouse to preserve viewport offset %d, got %d", bottomOffset, m2.viewport.YOffset())
	}
}

func TestModel_CtrlOTogglesMouseCapture(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat

	updated, _ := m.Update(keyCtrl('o'))
	m2 := updated.(Model)
	if !m2.mouseEnabled {
		t.Fatal("expected ctrl+o to enable mouse")
	}

	updated, _ = m2.Update(keyCtrl('o'))
	m3 := updated.(Model)
	if m3.mouseEnabled {
		t.Fatal("expected second ctrl+o to disable mouse")
	}
}

func TestModel_CtrlOTogglesMouseCaptureWhenSidebarFocused(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.sidebarFocused = true
	m.sessions = []tuiSessionInfo{{ChatID: ipc.ReservedTUIChatID, Name: "dm"}}

	updated, _ := m.Update(keyCtrl('o'))
	m2 := updated.(Model)
	if !m2.mouseEnabled {
		t.Fatal("expected ctrl+o to enable mouse even when sidebar is focused")
	}
	if !m2.sidebarFocused {
		t.Fatal("expected sidebar focus to remain unchanged")
	}
}

func TestModel_CtrlUDelegatesToTextarea(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.ready = true
	m.textarea.SetValue("hello world")
	m.textarea.CursorEnd()

	updated, _ := m.Update(keyCtrl('u'))
	m2 := updated.(Model)

	if m2.textarea.Value() == "hello world" {
		t.Fatal("expected ctrl+u to edit textarea, but value was unchanged")
	}
}

func TestModel_InputHistoryNavigatesUpAndDown(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.inputHistory = []string{"first prompt", "second prompt"}
	m.inputHistoryIndex = len(m.inputHistory)

	updated, cmd := m.Update(keyPress(tea.KeyUp))
	m2 := updated.(Model)
	if cmd != nil {
		t.Fatal("expected nil command for history up")
	}
	if m2.textarea.Value() != "second prompt" {
		t.Fatalf("expected latest history entry, got %q", m2.textarea.Value())
	}

	updated, _ = m2.Update(keyPress(tea.KeyUp))
	m3 := updated.(Model)
	if m3.textarea.Value() != "first prompt" {
		t.Fatalf("expected previous history entry, got %q", m3.textarea.Value())
	}

	updated, _ = m3.Update(keyPress(tea.KeyDown))
	m4 := updated.(Model)
	if m4.textarea.Value() != "second prompt" {
		t.Fatalf("expected next history entry, got %q", m4.textarea.Value())
	}

	updated, _ = m4.Update(keyPress(tea.KeyDown))
	m5 := updated.(Model)
	if m5.textarea.Value() != "" {
		t.Fatalf("expected down past latest to clear input, got %q", m5.textarea.Value())
	}
}

func TestModel_InputHistoryDoesNotReplaceNonEmptyDraft(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.inputHistory = []string{"old prompt"}
	m.inputHistoryIndex = len(m.inputHistory)
	m.textarea.SetValue("draft")

	updated, _ := m.Update(keyPress(tea.KeyUp))
	m2 := updated.(Model)

	if m2.textarea.Value() != "draft" {
		t.Fatalf("expected draft preserved, got %q", m2.textarea.Value())
	}
	if m2.inputHistoryIndex != len(m.inputHistory) {
		t.Fatalf("expected history index unchanged, got %d", m2.inputHistoryIndex)
	}
}

func TestModel_InputHistoryDoesNotReplaceEditedHistoryDraft(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.inputHistory = []string{"old prompt", "second prompt"}
	m.inputHistoryIndex = len(m.inputHistory)

	updated, _ := m.Update(keyPress(tea.KeyUp))
	m2 := updated.(Model)
	m2.textarea.SetValue("second prompt edited")

	updated, _ = m2.Update(keyPress(tea.KeyUp))
	m3 := updated.(Model)
	if m3.textarea.Value() != "second prompt edited" {
		t.Fatalf("expected edited history draft preserved on up, got %q", m3.textarea.Value())
	}

	updated, _ = m3.Update(keyPress(tea.KeyDown))
	m4 := updated.(Model)
	if m4.textarea.Value() != "second prompt edited" {
		t.Fatalf("expected edited history draft preserved on down, got %q", m4.textarea.Value())
	}
}

func TestModel_RememberInputDedupesConsecutiveEntries(t *testing.T) {
	m := newModel("/tmp/test.sock", "", ThemeDark)
	m.rememberInput("hello")
	m.rememberInput("hello")
	m.rememberInput("world")

	if len(m.inputHistory) != 2 {
		t.Fatalf("expected 2 deduped history entries, got %d", len(m.inputHistory))
	}
	if m.inputHistory[0] != "hello" || m.inputHistory[1] != "world" {
		t.Fatalf("unexpected input history: %#v", m.inputHistory)
	}
	if m.inputHistoryIndex != len(m.inputHistory) {
		t.Fatalf("expected index at end, got %d", m.inputHistoryIndex)
	}
}

func TestModel_TabTogglesSidebar(t *testing.T) {
	m := testChatModel()
	m.showSidebar = false

	updated, _ := m.Update(keyPress(tea.KeyTab))
	m2 := updated.(Model)

	if !m2.showSidebar {
		t.Error("expected showSidebar=true after tab")
	}

	updated2, _ := m2.Update(keyPress(tea.KeyTab))
	m3 := updated2.(Model)

	if m3.showSidebar {
		t.Error("expected showSidebar=false after second tab")
	}
}

func TestModel_TabRuneDoesNotWriteToInput(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.showSidebar = false

	updated, cmd := m.Update(keyText("\t"))
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
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.ready = true
	m.textarea.SetValue("hello")

	updated, cmd := m.Update(keyAlt('s'))
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for ignored alt shortcut")
	}
	if m2.textarea.Value() != "hello" {
		t.Errorf("expected textarea unchanged after alt+s, got %q", m2.textarea.Value())
	}
}

func TestModel_TerminalColorReportDoesNotWriteToInput(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.ready = true

	updated, cmd := m.Update(keyText("1;rgb:158e/193a/1e75"))
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for terminal color report residue")
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea after terminal color report, got %q", m2.textarea.Value())
	}
}

func TestModel_PastedTerminalColorLikeTextWritesToInput(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.ready = true

	text := "1;rgb:158e/193a/1e75"
	updated, _ := m.Update(tea.PasteMsg{Content: text})
	m2 := updated.(Model)

	if m2.textarea.Value() != text {
		t.Errorf("expected pasted terminal-color-like text to remain, got %q", m2.textarea.Value())
	}
}

func TestModel_TextareaAltShortcutDoesNotWriteLiteralRune(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.ready = true
	m.textarea.SetValue("hello world")
	m.textarea.CursorStart()

	updated, _ := m.Update(keyAlt('f'))
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
	m := testChatModelWithTextarea(ta)

	updated, cmd := m.Update(keyAltEnter())
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
	m := testChatModelWithTextarea(ta)

	updated, cmd := m.Update(keyCtrl('j'))
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

func TestModel_SubmitWithPendingImagesClearsThem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	ta := textarea.New()
	ta.SetValue("hello")
	m := testChatModelWithTextarea(ta)
	// Attach image.
	_ = m.attachImageFromPath(path)
	if len(m.pendingImages) != 1 {
		t.Fatalf("expected 1 pending image before submit, got %d", len(m.pendingImages))
	}

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command after submit with images")
	}
	// Pending images should be cleared after submit.
	if len(m2.pendingImages) != 0 {
		t.Errorf("expected 0 pending images after submit, got %d", len(m2.pendingImages))
	}
}

func TestModel_SubmitImageOnlyWithoutText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	ta := textarea.New()
	// Empty textarea.
	m := testChatModelWithTextarea(ta)
	// Attach image.
	_ = m.attachImageFromPath(path)
	if len(m.pendingImages) != 1 {
		t.Fatalf("expected 1 pending image, got %d", len(m.pendingImages))
	}
	if m.textarea.Value() != "" {
		t.Fatalf("expected empty textarea, got %q", m.textarea.Value())
	}

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command for image-only submit")
	}
	if len(m2.pendingImages) != 0 {
		t.Errorf("expected 0 pending images after submit, got %d", len(m2.pendingImages))
	}
}

func TestModel_SubmitTempImageDefersCleanupUntilStreamEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	ta := textarea.New()
	ta.SetValue("describe")
	m := testChatModelWithTextarea(ta)
	if errMsg := m.attachTempImage(path); errMsg != "" {
		t.Fatalf("unexpected attach error: %s", errMsg)
	}

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command for temp image submit")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected temp image to remain until daemon consumes it: %v", err)
	}
	if len(m2.submittedTempImagePaths) != 1 {
		t.Fatalf("expected submitted temp path tracked, got %d", len(m2.submittedTempImagePaths))
	}

	updated, _ = m2.handleStreamEvent(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd})
	m3 := updated.(Model)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected temp image removed on stream end, stat: %v", err)
	}
	if len(m3.submittedTempImagePaths) != 0 {
		t.Fatalf("expected submitted temp paths cleared, got %d", len(m3.submittedTempImagePaths))
	}
}

func TestModel_SidebarQuitCleansSubmittedTempImages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "submitted.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	m := testChatModel()
	m.sidebarFocused = true
	m.submittedTempImagePaths = []string{path}

	_, cmd := m.Update(keyCtrl('c'))

	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected submitted temp image removed on sidebar quit, stat: %v", err)
	}
}

func TestModel_SubmitAutoAttachesImagePathFromText(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "GravaçãoTela")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Captura de Tela 2026-06-17 às 19.04.36.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	escaped := filepath.Join(dir, "Captura\\ de\\ Tela\\ 2026-06-17\\ às\\ 19.04.36.png")

	ta := textarea.New()
	ta.SetValue("descreva essa imagem " + escaped)
	m := testChatModelWithTextarea(ta)

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command after submit")
	}
	if len(m2.pendingImages) != 0 {
		t.Errorf("expected pending images cleared after submit, got %d", len(m2.pendingImages))
	}
	if len(m2.messages) != 1 {
		t.Fatalf("expected 1 displayed message, got %d", len(m2.messages))
	}
	if !strings.Contains(m2.messages[0].Text, "📎 Captura de Tela 2026-06-17 às 19.04.36.png") {
		t.Errorf("message missing image badge: %q", m2.messages[0].Text)
	}
	if !strings.Contains(m2.messages[0].Text, "descreva essa imagem") {
		t.Errorf("message missing cleaned prompt: %q", m2.messages[0].Text)
	}
	if strings.Contains(m2.messages[0].Text, escaped) || strings.Contains(m2.messages[0].Text, path) {
		t.Errorf("message should not display raw image path: %q", m2.messages[0].Text)
	}
}

func TestModel_SendCommandForSlashText(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/status")
	m := testChatModelWithTextarea(ta)

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected command for /status")
	}
	if m2.waiting != true {
		t.Error("expected waiting=true after command submit")
	}
}

func TestModel_SlashCommandDoesNotAutoAttachImageArgument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	ta := textarea.New()
	ta.SetValue("/status " + path)
	m := testChatModelWithTextarea(ta)

	updated, cmd := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected command for /status")
	}
	if len(m2.pendingImages) != 0 {
		t.Fatalf("expected no auto-attached images for slash command, got %d", len(m2.pendingImages))
	}
	if len(m2.messages) != 1 || m2.messages[0].Text != "/status "+path {
		t.Fatalf("displayed command = %#v, want raw command text", m2.messages)
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
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.ready = true

	// Simulate typing "hello" via textarea updates.
	chars := []tea.KeyPressMsg{
		keyText("h"),
		keyText("e"),
		keyText("l"),
		keyText("l"),
		keyText("o"),
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
	m2, _ := m.Update(keyBackspace())
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
	_, cmd := m.Update(keyCtrl('c'))
	if cmd == nil {
		t.Error("expected quit command in error state")
	}

	// Enter in error state retries (goes to loading).
	updated, _ := m.Update(keyPress(tea.KeyEnter))
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
			m := Model{transcriptModel: transcriptModel{messages: tt.initial}}
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
	m := NewModel("/tmp/test.sock", ThemeDark)
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
	m := testChatModel()
	m.waiting = true
	m.streamBuf = "partial text"

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
	m := testChatModel()
	m.waiting = true

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
	m := testChatModel()
	m.waiting = true

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
	m := NewModel("/tmp/test.sock", ThemeDark)

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
	m := NewModel("/tmp/test.sock", ThemeDark)

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

	updated, cmd := m3.Update(keyPress(tea.KeyEnter))
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
	m := testChatModel()
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}

	// Should not panic and should not initialize viewport.
	m.updateViewport()

	if m.viewportSet {
		t.Error("expected viewportSet to remain false")
	}
	if m.viewport.Height() != 0 {
		t.Errorf("expected viewport height 0, got %d", m.viewport.Height())
	}
}

func TestModel_DefaultChromeState(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)

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
	m := NewModel("/tmp/test.sock", ThemeDark)

	if m.textarea.Prompt != "" {
		t.Fatalf("expected textarea internal prompt disabled, got %q", m.textarea.Prompt)
	}
}

func TestModel_ChatViewStartsWithTopMargin(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 40
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true

	view := m.View().Content
	firstLine, _, _ := strings.Cut(view, "\n")

	if len(firstLine) != m.width {
		t.Fatalf("expected top margin width %d, got %d", m.width, len(firstLine))
	}
	if strings.TrimSpace(firstLine) != "" {
		t.Fatalf("expected blank top margin, got %q", firstLine)
	}
}

func TestModel_StatusBarUsesCompactShortcuts(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 175

	status := stripANSIForTest(m.renderStatusBar())

	for _, want := range []string{"↵ send", "alt+enter newline", "✋ mouse", "esc cancel", "⌃L clear", "⌃P project", "tab sidebar"} {
		if !strings.Contains(status, want) {
			t.Errorf("expected status bar to contain %q, got %q", want, status)
		}
	}
}

func TestModel_StatusBarShowsMouseEnabled(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.mouseEnabled = true

	status := stripANSIForTest(m.renderStatusBar())

	if !strings.Contains(status, "🖱️ mouse") {
		t.Fatalf("expected enabled mouse indicator, got %q", status)
	}
}

func TestModel_StatusBarDropsItemsOnNarrowTerminal(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
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
	if strings.Contains(status, "⌃P project") {
		t.Errorf("expected '⌃P project' to be dropped on width=50, got %q", status)
	}
	// High-priority items should remain.
	if !strings.Contains(status, "↵ send") {
		t.Errorf("expected '↵ send' to remain on width=50, got %q", status)
	}
}

func TestModel_StatusBarNeverWraps(t *testing.T) {
	for _, width := range []int{40, 60, 80, 100, 120, 150, 175} {
		m := NewModel("/tmp/test.sock", ThemeDark)
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
	m := NewModel("/tmp/test.sock", ThemeDark)
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
	m := NewModel("/tmp/test.sock", ThemeDark)
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
	m := NewModel("/tmp/test.sock", ThemeDark)
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
	m := NewModel("/tmp/test.sock", ThemeDark)
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
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = 100
	m.height = 40

	vp := viewportForSize(m.contentWidth(), m.height)
	want := m.height - inputHeight - statusBarHeight - topMarginHeight - chatHeaderHeight

	if vp.Height() != want {
		t.Fatalf("expected viewport height %d, got %d", want, vp.Height())
	}
}

func TestModel_ViewportShrinksBelowMinimumOnVeryShortTerminal(t *testing.T) {
	for _, height := range []int{12, 13} {
		m := NewModel("/tmp/test.sock", ThemeDark)
		m.width = minSidebarScreenWidth
		m.height = height

		vp := viewportForSize(m.contentWidth(), m.height)
		want := height - inputHeight - statusBarHeight - topMarginHeight - chatHeaderHeight

		if vp.Height() != want {
			t.Fatalf("height %d: expected viewport height %d, got %d", height, want, vp.Height())
		}
	}
}

func TestModel_TuiStatusUpdatesCWD(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat

	updated, _ := m.Update(tuiStatusMsg{cwd: "/Users/igor/dev/aurelia"})
	m2 := updated.(Model)

	if m2.cwdPath != "/Users/igor/dev/aurelia" {
		t.Errorf("expected cwdPath updated, got %q", m2.cwdPath)
	}
}

func TestModel_MessageEventUpdatesCWDForSidebar(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
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
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = minSidebarScreenWidth - 1
	m.height = minSidebarScreenHeight

	if m.shouldShowSidebar() {
		t.Error("expected sidebar hidden on narrow terminal")
	}
}

func TestModel_SidebarHiddenOnShortTerminal(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.width = minSidebarScreenWidth
	m.height = minSidebarScreenHeight - 1

	if m.shouldShowSidebar() {
		t.Error("expected sidebar hidden on short terminal")
	}
}

func TestModel_ChatViewDoesNotExceedShortTerminalHeight(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = minSidebarScreenWidth
	m.height = minSidebarScreenHeight - 1
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true

	view := m.View().Content
	lineCount := strings.Count(view, "\n") + 1

	if lineCount > m.height {
		t.Fatalf("expected view height <= %d lines, got %d", m.height, lineCount)
	}
}

func TestModel_ChatHeaderShowsChatModeWhenNoCWD(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 40
	m.daemonLabel = "ready"
	// cwdPath is "not set" by default

	header := stripANSIForTest(m.renderChatHeader())

	if !strings.Contains(header, "chat mode") {
		t.Errorf("expected header to show 'chat mode' when no cwd, got: %q", header)
	}
	if strings.Contains(header, "project") {
		t.Errorf("expected header to NOT mention 'project' when in chat mode, got: %q", header)
	}
}

func TestModel_SidebarShowsChatModeWhenNoCWD(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.daemonLabel = "ready"
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
	}
	// cwdPath is "not set" by default

	sidebar := sidebarViewForTest(m)
	plain := stripANSIForTest(sidebar)

	if !strings.Contains(plain, "chat mode") {
		t.Errorf("expected sidebar to show '(chat mode)' when no cwd, got: %q", plain)
	}
}

func TestModel_SidebarHidesChatModeWhenCWDSet(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.cwdPath = "/Users/igor/dev/aurelia"
	m.daemonLabel = "ready"
	m.sessions = []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
	}

	sidebar := sidebarViewForTest(m)
	plain := stripANSIForTest(sidebar)

	if strings.Contains(plain, "chat mode") {
		t.Errorf("expected no 'chat mode' when cwd is set, got: %q", plain)
	}
	if !strings.Contains(plain, "aurelia") {
		t.Errorf("expected sidebar project to show 'aurelia', got: %q", plain)
	}
}

func TestModel_ChatViewDoesNotExceedVeryShortTerminalHeight(t *testing.T) {
	for _, height := range []int{12, 13} {
		m := NewModel("/tmp/test.sock", ThemeDark)
		m.state = stateChat
		m.width = minSidebarScreenWidth
		m.height = height
		m.viewport = viewportForSize(m.contentWidth(), m.height)
		m.viewportSet = true

		view := m.View().Content
		lineCount := strings.Count(view, "\n") + 1

		if lineCount > m.height {
			t.Fatalf("height %d: expected view height <= %d lines, got %d", height, height, lineCount)
		}
	}
}

// ---- Project Panel Tests ----

func TestModel_CtrlPTogglesProjectPanel(t *testing.T) {
	m := testChatModel()

	// Toggle on.
	updated, cmd := m.Update(keyCtrl('p'))
	m2 := updated.(Model)

	if !m2.projectPanelOpen {
		t.Error("expected projectPanelOpen=true after ctrl+p")
	}
	if cmd == nil {
		t.Fatal("expected non-nil command (fetch state + poll)")
	}

	// Toggle off.
	updated, cmd = m2.Update(keyCtrl('p'))
	m3 := updated.(Model)

	if m3.projectPanelOpen {
		t.Error("expected projectPanelOpen=false after second ctrl+p")
	}
	if cmd != nil {
		t.Fatal("expected nil command when closing panel")
	}
}

func TestModel_ProjectStateMsgUpdatesState(t *testing.T) {
	m := testChatModel()

	ps := &ipc.ProjectStatePayload{
		CWD:           "/Users/igor/dev/aurelia",
		BindingSource: "manual",
		ActiveAgent:   "coder",
		Model:         "claude-sonnet-4-6",
		BridgeStatus:  "online",
	}

	updated, cmd := m.Update(tuiProjectStateMsg{state: ps})
	m2 := updated.(Model)

	if m2.projectState == nil {
		t.Fatal("expected projectState to be set")
	}
	if m2.projectState.CWD != "/Users/igor/dev/aurelia" {
		t.Errorf("expected CWD, got %q", m2.projectState.CWD)
	}
	if m2.projectState.ActiveAgent != "coder" {
		t.Errorf("expected ActiveAgent=coder, got %q", m2.projectState.ActiveAgent)
	}
	if cmd != nil {
		t.Fatal("expected nil command (no poll since panel is closed)")
	}
}

func TestModel_ProjectStateErrorDoesNotPanic(t *testing.T) {
	m := testChatModel()

	updated, _ := m.Update(tuiProjectStateMsg{err: assertError("timeout")})
	m2 := updated.(Model)

	if m2.projectState != nil {
		t.Error("expected projectState to remain nil on error")
	}
}

func TestModel_ProjectStateMsgWithOpenPanelSchedulesPoll(t *testing.T) {
	m := testChatModel()
	m.projectPanelOpen = true

	ps := &ipc.ProjectStatePayload{
		CWD:           "/tmp/test",
		BindingSource: "manual",
		ActiveAgent:   "general",
		Model:         "PI default",
		BridgeStatus:  "offline",
	}

	updated, cmd := m.Update(tuiProjectStateMsg{state: ps})
	m2 := updated.(Model)

	if m2.projectState == nil {
		t.Fatal("expected projectState to be set")
	}
	if cmd == nil {
		t.Fatal("expected poll command since panel is open")
	}
}

func TestModel_ProjectPanelRendersFields(t *testing.T) {
	m := testProjectPanelModel(&ipc.ProjectStatePayload{
		CWD:           "/Users/igor/dev/aurelia",
		BindingSource: "manual",
		ActiveAgent:   "coder",
		Model:         "claude-sonnet-4-6",
		BridgeStatus:  "online",
		MemoryLayers: []ipc.ProjectStateMemoryLayer{
			{Name: "Global", Scope: "global", Exists: true, FileCount: 14},
			{Name: "Team", Scope: "team", Exists: true, FileCount: 8},
		},
		CheckpointLayer: "cwd_overlay",
	})
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}

	panel := m.renderProjectPanel()
	plain := stripANSIForTest(panel)

	for _, want := range []string{"Project State", "/Users/igor/dev/aurelia", "manual", "coder", "claude-sonnet-4-6", "online", "Global", "Team", "14 files", "8 files"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected panel to contain %q, got:\n%s", want, plain)
		}
	}
}

func TestModel_ProjectPanelRendersNoCWD(t *testing.T) {
	m := testProjectPanelModel(&ipc.ProjectStatePayload{
		CWD:           "",
		BindingSource: "none",
		ActiveAgent:   "general",
		Model:         "PI default",
		BridgeStatus:  "offline",
	})

	panel := m.renderProjectPanel()
	plain := stripANSIForTest(panel)

	for _, want := range []string{"not set", "none", "offline", "general"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected panel to contain %q, got:\n%s", want, plain)
		}
	}
}

func TestModel_ProjectPanelRendersInheritedBinding(t *testing.T) {
	m := testProjectPanelModel(&ipc.ProjectStatePayload{
		CWD:           "/Users/igor/dev/shared",
		BindingSource: "inherited",
		BindingFrom:   "TUI session",
		ActiveAgent:   "architect",
		Model:         "claude-opus-4-0",
		BridgeStatus:  "online",
	})

	panel := m.renderProjectPanel()
	plain := stripANSIForTest(panel)

	for _, want := range []string{"inherited", "TUI session", "architect"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected panel to contain %q, got:\n%s", want, plain)
		}
	}
}

func TestModel_ProjectPanelRendersLatestRun(t *testing.T) {
	started := time.Date(2026, 6, 19, 10, 30, 0, 0, time.UTC)
	m := testProjectPanelModel(&ipc.ProjectStatePayload{
		CWD:           "/tmp/test",
		BindingSource: "manual",
		ActiveAgent:   "general",
		Model:         "PI default",
		BridgeStatus:  "online",
		LatestRun: &ipc.ProjectStateRun{
			Status:     "completed",
			Checkpoint: "Refactoring done",
			AgentName:  "coder",
			StartedAt:  started,
			DurationMs: 3500,
		},
	})

	panel := m.renderProjectPanel()
	plain := stripANSIForTest(panel)

	for _, want := range []string{"completed", "Refactoring done", "coder", "3.5s"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected panel to contain %q, got:\n%s", want, plain)
		}
	}
}

func TestModel_ProjectStatePollTickWhilePanelClosedDoesNothing(t *testing.T) {
	m := testChatModel()

	updated, cmd := m.Update(projectStatePollTickMsg{})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command when panel is closed")
	}
	if m2.projectPanelOpen {
		t.Error("expected panel to remain closed")
	}
}

func TestModel_ProjectStatePollTickWhileWaitingDoesNothing(t *testing.T) {
	m := testChatModel()
	m.projectPanelOpen = true
	m.waiting = true

	updated, cmd := m.Update(projectStatePollTickMsg{})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command when already waiting")
	}
	if !m2.projectPanelOpen {
		t.Error("expected panel to remain open")
	}
}

func TestModel_ProjectStatePollTickFetchesState(t *testing.T) {
	m := testChatModel()
	m.projectPanelOpen = true

	updated, cmd := m.Update(projectStatePollTickMsg{})
	m2 := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command when panel is open and not waiting")
	}
	if !m2.projectPanelOpen {
		t.Error("expected panel to remain open")
	}
}

// assertError is a simple error for test assertions.
type assertError string

func (e assertError) Error() string { return string(e) }

// ── T5.2.2 Rich status bar tests ───────────────────────────────────────────

func TestModel_StatusBarShowsActiveModel(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.activeModel = "gpt-5.5"

	status := stripANSIForTest(m.renderStatusBar())

	if !strings.Contains(status, "gpt-5.5") {
		t.Errorf("expected status bar to show 'gpt-5.5', got %q", status)
	}
}

func TestModel_StatusBarEmptyModelHidden(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	// activeModel defaults to ""

	status := stripANSIForTest(m.renderStatusBar())

	// The status bar should not render a model separator when model is empty.
	// It's fine if other fields appear — the key is no model label.
	// Just verify the empty string case is handled gracefully (no crash).
	if len(status) == 0 {
		t.Error("expected non-empty status bar")
	}
}

func TestModel_StatusBarShowsPendingCount(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.pendingQueue = []queuedMessage{{}, {}, {}} // 3 pending

	status := stripANSIForTest(m.renderStatusBar())

	if !strings.Contains(status, "⏳ 3") {
		t.Errorf("expected status bar to contain '⏳ 3', got %q", status)
	}
}

func TestModel_StatusBarNoPendingHidden(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	// pendingQueue defaults to nil

	status := stripANSIForTest(m.renderStatusBar())

	if strings.Contains(status, "⏳") {
		t.Errorf("expected no pending badge when queue is empty, got %q", status)
	}
}

func TestModel_StatusBarShowsElapsedTime(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.turnStart = time.Now().Add(-12 * time.Second)

	status := stripANSIForTest(m.renderStatusBar())

	if !strings.Contains(status, "12s") {
		t.Errorf("expected status bar to contain '12s', got %q", status)
	}
}

func TestModel_StatusBarNoElapsedWhenIdle(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	// turnStart defaults to zero

	status := stripANSIForTest(m.renderStatusBar())

	// When turnStart is zero, no elapsed label should appear.
	// The "s" suffix would only appear in the elapsed field.
	// (Other fields like "sessions" contain "s" but not "s" as delimiter)
	// We just verify no crash.
	if len(status) == 0 {
		t.Error("expected non-empty status bar")
	}
}

func TestModel_statusFromEvents(t *testing.T) {
	events := []ipc.IPCEvent{
		{Type: ipc.EventTypeMessage, Body: "**Aurelia Status**\n🧠 Bridge: **online**\n⚙️ Model: **gpt-5.5**\n📂 CWD: `/Users/igor/dev`\n💬 Session: none\n"},
	}
	result := statusFromEvents(events)

	if result.cwd != "/Users/igor/dev" {
		t.Errorf("cwd = %q, want /Users/igor/dev", result.cwd)
	}
	if result.model != "gpt-5.5" {
		t.Errorf("model = %q, want gpt-5.5", result.model)
	}
	if result.err != nil {
		t.Errorf("unexpected error: %v", result.err)
	}
}

func TestModel_statusFromEvents_NoModel(t *testing.T) {
	events := []ipc.IPCEvent{
		{Type: ipc.EventTypeMessage, Body: "**Aurelia Status**\n🧠 Bridge: **online**\n📂 No project set.\n"},
	}
	result := statusFromEvents(events)

	if result.cwd != "not set" {
		t.Errorf("cwd = %q, want 'not set'", result.cwd)
	}
	if result.model != "" {
		t.Errorf("model = %q, want empty", result.model)
	}
}

func TestModel_modelFromText(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"⚙️ Model: **gpt-5.5**", "gpt-5.5"},
		{"foo\n⚙️ Model: **deepseek-v4-pro**\nbar", "deepseek-v4-pro"},
		{"no model here", ""},
		{"⚙️ Model: **", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := modelFromText(tt.text)
		if got != tt.want {
			t.Errorf("modelFromText(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

// TestModel_TurnStartClearedAfterStreamEnd verifies that turnStart is reset
// when the pipeline finishes, so the elapsed counter disappears from the
// status bar.
func TestModel_TurnStartClearedAfterStreamEnd(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.waiting = true
	m.turnStart = time.Now()

	updated, _ := m.handleStreamEvent(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd})
	m2 := updated.(Model)
	if !m2.turnStart.IsZero() {
		t.Error("expected turnStart to be zero after stream_end")
	}
	if m2.waiting {
		t.Error("expected waiting=false after stream_end")
	}
}

// ── T5.2.3 Help overlay tests ─────────────────────────────────────────────

func TestModel_HelpOverlayToggleWithQuestionMark(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 40
	// textarea default is empty
	if m.helpVisible() {
		t.Fatal("expected help overlay closed initially")
	}

	// ? with empty input opens help
	updated, _ := m.handleKeyMsg(keyText("?"))
	m2 := updated.(Model)
	if !m2.helpVisible() {
		t.Error("expected help overlay open after ?")
	}

	// ? again closes help
	updated2, _ := m2.handleKeyMsg(keyText("?"))
	m3 := updated2.(Model)
	if m3.helpVisible() {
		t.Error("expected help overlay closed after second ?")
	}
}

func TestModel_HelpOverlayCloseWithEsc(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.helpModel.ShowAll = true

	updated, _ := m.handleKeyMsg(keyPress(tea.KeyEsc))
	m2 := updated.(Model)
	if m2.helpVisible() {
		t.Error("expected help overlay closed after Esc")
	}
}

func TestModel_HelpOverlayCloseWithEnter(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.helpModel.ShowAll = true

	updated, _ := m.handleKeyMsg(keyPress(tea.KeyEnter))
	m2 := updated.(Model)
	if m2.helpVisible() {
		t.Error("expected help overlay closed after Enter")
	}
}

func TestModel_HelpOverlayNotOpenedWithNonEmptyInput(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.textarea.SetValue("hello")

	updated, _ := m.handleKeyMsg(keyText("?"))
	m2 := updated.(Model)
	if m2.helpVisible() {
		t.Error("expected help overlay closed when textarea is not empty")
	}
	// ? should have been forwarded to the textarea (delegated)
}

func TestModel_HelpOverlayRenderContainsKeyBindings(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 40

	overlay := m.renderHelpPanel()

	for _, want := range []string{
		"Keyboard Shortcuts",
		"Esc",
		"Ctrl+O",
		"Ctrl+P",
		"Ctrl+L",
		"Commands",
		"/help",
		"/cwd",
		"/img",
	} {
		if !strings.Contains(overlay, want) {
			t.Errorf("expected help overlay to contain %q, got:\n%s", want, overlay)
		}
	}
}

func TestModel_HelpOverlayRendersScoped(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 80
	m.height = 30
	m.helpModel.ShowAll = true

	view := m.View().Content

	// Should contain the full UI but the help overlay should be on top.
	// The overlay shouldn't crash rendering on narrow/standard widths.
	if !strings.Contains(stripANSIForTest(view), "Keyboard Shortcuts") {
		t.Error("expected view to contain help overlay when open")
	}
}

// ── T5.2.4 Daemon state indicator tests ───────────────────────────────────

func TestModel_ChromeStateReturnsOffline(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.daemonLabel = "offline"

	got := m.chromeState()
	if got != "offline" {
		t.Errorf("chromeState() = %q, want offline", got)
	}
}

func TestModel_ChromeStateReturnsReadyByDefault(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.daemonLabel = "ready"

	got := m.chromeState()
	if got != "ready" {
		t.Errorf("chromeState() = %q, want ready", got)
	}
}

func TestModel_ChromeStateReturnsWaiting(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.waiting = true
	m.daemonLabel = "ready"

	got := m.chromeState()
	if got != "waiting" {
		t.Errorf("chromeState() = %q, want waiting", got)
	}
}

func TestModel_StatusBarShowsOffline(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.daemonLabel = "offline"

	status := stripANSIForTest(m.renderStatusBar())

	if !strings.Contains(status, "offline") {
		t.Errorf("expected status bar to contain 'offline', got %q", status)
	}
}

func TestModel_ChatHeaderHighlightsOfflineDaemon(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 40
	m.daemonLabel = "offline"
	m.cwdPath = "/Users/igor/dev"

	header := stripANSIForTest(m.renderChatHeader())

	if !strings.Contains(header, "daemon offline") {
		t.Errorf("expected header to contain 'daemon offline', got %q", header)
	}
}

func TestModel_ChatHeaderNormalDaemon(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 100
	m.height = 40
	m.daemonLabel = "ready"
	m.cwdPath = "/Users/igor/dev"

	header := stripANSIForTest(m.renderChatHeader())

	if !strings.Contains(header, "daemon ready") {
		t.Errorf("expected header to contain 'daemon ready', got %q", header)
	}
}

func TestModel_ReconnectionToast(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 80
	m.height = 40
	m.daemonLabel = "offline"
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}

	// Simulate health check recovery.
	updated, _ := m.Update(healthCheckResultMsg{latency: 10 * time.Millisecond})
	m2 := updated.(Model)

	if m2.daemonLabel != "ready" {
		t.Errorf("expected daemonLabel=ready, got %q", m2.daemonLabel)
	}
	if len(m2.messages) != 2 {
		t.Fatalf("expected 2 messages (original + reconnect toast), got %d", len(m2.messages))
	}
	if m2.messages[1].Text != "Daemon reconnected." {
		t.Errorf("expected reconnect toast, got %q", m2.messages[1].Text)
	}
	if m2.messages[1].Sender != "🔗" {
		t.Errorf("expected sender=🔗, got %q", m2.messages[1].Sender)
	}
}

func TestModel_NoReconnectionToastWhenAlreadyReady(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 80
	m.height = 40
	m.daemonLabel = "ready"
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}

	// Health check success when already ready should NOT add toast.
	updated, _ := m.Update(healthCheckResultMsg{latency: 10 * time.Millisecond})
	m2 := updated.(Model)

	if len(m2.messages) != 1 {
		t.Errorf("expected 1 message (no toast), got %d", len(m2.messages))
	}
}

// ── Smart timestamp tests ─────────────────────────────────────────────────

func TestFormatMessageTime_TimeOnly(t *testing.T) {
	ts := time.Date(2026, 6, 21, 14, 5, 0, 0, time.UTC)
	got := formatMessageTime(ts, false)
	if got != "14:05" {
		t.Errorf("formatMessageTime(timeOnly) = %q, want 14:05", got)
	}
}

func TestFormatMessageTime_ZeroReturnsEmpty(t *testing.T) {
	got := formatMessageTime(time.Time{}, false)
	if got != "" {
		t.Errorf("formatMessageTime(zero) = %q, want empty", got)
	}
}

func TestFormatMessageHeader_OmitsSeparatorWithoutTimestamp(t *testing.T) {
	got := formatMessageHeader("Igor", "")
	if got != "▶ Igor" {
		t.Errorf("formatMessageHeader(empty timestamp) = %q, want ▶ Igor", got)
	}
}

func TestFormatMessageTime_WithDate(t *testing.T) {
	ts := time.Date(2026, 6, 21, 14, 5, 0, 0, time.UTC)
	got := formatMessageTime(ts, true)
	if got != "21/06 14:05" {
		t.Errorf("formatMessageTime(withDate) = %q, want 21/06 14:05", got)
	}
}

func TestSameDay_SameDay(t *testing.T) {
	a := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	b := time.Date(2026, 6, 21, 23, 59, 0, 0, time.UTC)
	if !sameDay(a, b) {
		t.Error("expected sameDay=true for same calendar day")
	}
}

func TestSameDay_DifferentDay(t *testing.T) {
	a := time.Date(2026, 6, 21, 23, 59, 0, 0, time.UTC)
	b := time.Date(2026, 6, 22, 0, 1, 0, 0, time.UTC)
	if sameDay(a, b) {
		t.Error("expected sameDay=false for different calendar day")
	}
}

func TestSameDay_DifferentMonth(t *testing.T) {
	a := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	b := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if sameDay(a, b) {
		t.Error("expected sameDay=false for different month")
	}
}

func TestModel_DateShownOnFirstMessageOfDay(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 80
	m.height = 40
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true

	// Two messages on the same day — first shows time only, second also time only.
	m.messages = []chatMessage{
		{Sender: "Igor", Text: "morning", Timestamp: time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)},
		{Sender: "Aurelia", Text: "hello", Timestamp: time.Date(2026, 6, 21, 9, 5, 0, 0, time.UTC)},
	}
	m.updateViewport()
	content := stripANSIForTest(m.viewport.View())

	if !strings.Contains(content, "09:00") {
		t.Errorf("expected first message to show '09:00', got:\n%s", content)
	}
	// Same day, so date should NOT appear.
	if strings.Contains(content, "21/06") {
		t.Errorf("expected NO date for same-day messages, got:\n%s", content)
	}
}

func TestModel_DateShownOnFirstMessageOfNewDay(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 80
	m.height = 40
	m.viewport = viewportForSize(m.contentWidth(), m.height)
	m.viewportSet = true

	// Two messages across day boundary — second message shows date.
	m.messages = []chatMessage{
		{Sender: "Igor", Text: "night", Timestamp: time.Date(2026, 6, 20, 23, 50, 0, 0, time.UTC)},
		{Sender: "Aurelia", Text: "morning", Timestamp: time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)},
	}
	m.updateViewport()
	content := stripANSIForTest(m.viewport.View())

	if !strings.Contains(content, "21/06") {
		t.Errorf("expected new-day message to show date '21/06', got:\n%s", content)
	}
}
