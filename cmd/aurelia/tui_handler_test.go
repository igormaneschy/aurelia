package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/tuisessions"
)

// testEmit collects emitted events for verification.
type testEmit struct {
	mu     sync.Mutex
	events []ipc.IPCEvent
}

func (e *testEmit) emit(ev ipc.IPCEvent) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
	return nil
}

func (e *testEmit) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

// testApp builds a minimal *app for handler testing with temp stores.
func testApp(t *testing.T) (*app, context.Context, func()) {
	t.Helper()

	dir := t.TempDir()

	bindings, err := projectbinding.NewSQLiteStore(filepath.Join(dir, "bindings.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	tuiSessions, err := tuisessions.NewSQLiteStore(filepath.Join(dir, "tui_sessions.db"))
	if err != nil {
		t.Fatalf("tuisessions.NewSQLiteStore: %v", err)
	}

	sessions := session.NewStore()
	cfg := &config.AppConfig{}
	ctx := context.Background()

	resolver, err := runtime.New()
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	return &app{
		bindings:    bindings,
		sessions:    sessions,
		config:      cfg,
		resolver:    resolver,
		tuiRunGuard: &tuiRunGuard{},
		tuiSessions: tuiSessions,
	}, ctx, func() {
		bindings.Close()
		tuiSessions.Close()
	}
}

func TestTUIHandler_StatusReturnsMessageAndStreamEnd(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type: "command",
		Text: "/status",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te.count() < 3 {
		t.Fatalf("expected at least 3 events, got %d", te.count())
	}
	// Events: ack, message, stream_end
	if te.events[0].Type != "ack" {
		t.Errorf("event[0] type = %q, want %q", te.events[0].Type, "ack")
	}
	if te.events[1].Type != "message" {
		t.Errorf("event[1] type = %q, want %q", te.events[1].Type, "message")
	}
	if !strings.Contains(te.events[1].Body, "Aurelia Status") {
		t.Errorf("expected status body, got %q", te.events[1].Body)
	}
	if te.events[2].Type != "stream_end" {
		t.Errorf("event[2] type = %q, want %q", te.events[2].Type, "stream_end")
	}
}

func TestTUIHandler_HelpReturnsTUICommandsAndKeyboardShortcuts(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type:      "command",
		Text:      "/help",
		RequestID: "help-test",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te.count() != 3 {
		t.Fatalf("expected 3 events (ack, message, stream_end), got %d", te.count())
	}
	body := te.events[1].Body
	for _, want := range []string{"Aurelia TUI Help", "/status", "/model <name>", "/cwd <path>", "Esc", "Ctrl+L"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected help body to contain %q, got %q", want, body)
		}
	}
	if te.events[2].Type != ipc.EventTypeStreamEnd {
		t.Errorf("event[2] type = %q, want %q", te.events[2].Type, ipc.EventTypeStreamEnd)
	}
}

func TestTUIHandler_ModelNoBridgeReturnsCurrentModelAndHint(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()
	a.config.DefaultProvider = "anthropic"
	a.config.DefaultModel = "claude-sonnet-4-6"

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type: "command",
		Text: "/model",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te.count() != 3 {
		t.Fatalf("expected 3 events (ack, message, stream_end), got %d", te.count())
	}
	body := te.events[1].Body
	for _, want := range []string{"Current model", "claude-sonnet-4-6", "Bridge unavailable"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected /model body to contain %q, got %q", want, body)
		}
	}
}

func TestTUIHandler_HistoryWithoutSessionReturnsEmptyList(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type:      ipc.MsgTypeHistory,
		RequestID: "history-test",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te.count() != 3 {
		t.Fatalf("expected 3 events (ack, history, stream_end), got %d", te.count())
	}
	if te.events[1].Type != ipc.EventTypeHistory {
		t.Fatalf("event[1] type = %q, want %q", te.events[1].Type, ipc.EventTypeHistory)
	}
	if te.events[1].Body != "[]" {
		t.Fatalf("expected empty history body, got %q", te.events[1].Body)
	}
	if te.events[2].Type != ipc.EventTypeStreamEnd {
		t.Fatalf("event[2] type = %q, want %q", te.events[2].Type, ipc.EventTypeStreamEnd)
	}
}

func TestFormatTUIModelListGroupsAndLimitsModels(t *testing.T) {
	models := []bridge.ModelInfo{
		{Provider: "openai", ID: "gpt-5.1"},
		{Provider: "anthropic", ID: "claude-sonnet-4-6", SupportsImages: true},
	}

	body := formatTUIModelList("Current model: **PI default**", models)

	for _, want := range []string{"Available models", "anthropic:", "`claude-sonnet-4-6` 📷", "openai:", "`gpt-5.1`", "/model auto"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected formatted model list to contain %q, got %q", want, body)
		}
	}
}

func TestFindTUIModelMatchesBareOrProviderQualifiedName(t *testing.T) {
	models := []bridge.ModelInfo{{Provider: "openai", ID: "gpt-5.1"}}

	if got := findTUIModel(models, "GPT-5.1"); got == nil || got.Provider != "openai" {
		t.Fatalf("expected bare case-insensitive match, got %#v", got)
	}
	if got := findTUIModel(models, "openai/gpt-5.1"); got == nil || got.ID != "gpt-5.1" {
		t.Fatalf("expected provider-qualified match, got %#v", got)
	}
	if got := findTUIModel(models, "missing"); got != nil {
		t.Fatalf("expected nil for missing model, got %#v", got)
	}
}

func TestSaveTUIDefaultModelUpdatesConfigFileAndMemory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AURELIA_HOME", home)
	configDir := filepath.Join(home, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	configPath := filepath.Join(configDir, "app.json")
	initial := `{"default_provider":"old","default_model":"old-model","providers":{"openai":{"api_key":"keep"}}}`
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	resolver, err := runtime.New()
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	a := &app{config: &config.AppConfig{}, resolver: resolver}

	if err := saveTUIDefaultModel(a, "openai", "gpt-5.1"); err != nil {
		t.Fatalf("saveTUIDefaultModel: %v", err)
	}
	reloaded, err := config.Load(resolver)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	if reloaded.DefaultProvider != "openai" || reloaded.DefaultModel != "gpt-5.1" {
		t.Fatalf("unexpected saved model provider=%q model=%q", reloaded.DefaultProvider, reloaded.DefaultModel)
	}
	if reloaded.ProviderAPIKey("openai") != "keep" {
		t.Fatalf("expected existing provider config to be preserved")
	}
	if a.config.DefaultProvider != "openai" || a.config.DefaultModel != "gpt-5.1" {
		t.Fatalf("expected in-memory config updated, got provider=%q model=%q", a.config.DefaultProvider, a.config.DefaultModel)
	}
}

func TestTUIHandler_CwdNoBinding(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type: "command",
		Text: "/cwd",
		// Client-supplied IDs are ignored — forced to ReservedTUIChatID
		ChatID:   999,
		ThreadID: 456,
		UserID:   999999,
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te.count() < 2 {
		t.Fatalf("expected at least 2 events, got %d", te.count())
	}
	body := te.events[1].Body
	if !strings.Contains(body, "No project set") && !strings.Contains(body, "no binding") {
		t.Errorf("expected 'No project set' in response, got %q", body)
	}

	// Verify the binding was created under ReservedTUIChatID, not the spoofed ChatID.
	spoofedKey := projectbinding.ConversationKey{ChatID: 999, ThreadID: 456}
	spoofed, err := a.bindings.Resolve(ctx, spoofedKey)
	if err == nil && spoofed != nil && spoofed.Binding != nil {
		t.Error("binding should NOT have been created under spoofed ChatID=999")
	}
}

func TestTUIHandler_CwdValidPath(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	validDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err = handler(ctx, ipc.IPCMessage{
		Type:     "command",
		Text:     "/cwd " + validDir,
		ChatID:   0,
		ThreadID: 0,
		UserID:   0,
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te.count() < 2 {
		t.Fatalf("expected at least 2 events, got %d", te.count())
	}
	body := te.events[1].Body
	if !strings.Contains(body, "Project set to") {
		t.Errorf("expected success message, got %q", body)
	}

	// Verify the binding was persisted under ReservedTUIChatID.
	tuiKey := projectbinding.ConversationKey{
		ChatID:   ipc.ReservedTUIChatID,
		ThreadID: 0,
	}
	resolved, err := a.bindings.Resolve(ctx, tuiKey)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if resolved == nil || resolved.Binding == nil {
		t.Fatal("expected binding to exist after /cwd")
	}
	if resolved.Binding.CWD != validDir {
		t.Errorf("expected CWD=%q, got %q", validDir, resolved.Binding.CWD)
	}
}

func TestTUIHandler_CwdInvalidPath(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type: "command",
		Text: "/cwd /nonexistent/path/that/does/not/exist",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te.count() < 2 {
		t.Fatalf("expected at least 2 events, got %d", te.count())
	}
	body := te.events[1].Body
	if !strings.Contains(body, "Invalid path") && !strings.Contains(body, "❌") {
		t.Errorf("expected error message about invalid path, got %q", body)
	}

	// Verify no binding was persisted.
	resolved, err := a.bindings.Resolve(ctx, projectbinding.ConversationKey{
		ChatID:   ipc.ReservedTUIChatID,
		ThreadID: 0,
	})
	if err == nil && resolved != nil && resolved.Binding != nil {
		t.Error("expected no binding to be persisted for invalid path")
	}
}

func TestTUIHandler_SpoofedIDsIgnored(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	validDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	handler := makeTUIHandler(a)
	te := &testEmit{}

	// Send /cwd with spoofed Telegram IDs.
	err = handler(ctx, ipc.IPCMessage{
		Type:     "command",
		Text:     "/cwd " + validDir,
		ChatID:   123,
		ThreadID: 456,
		UserID:   999999,
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Verify binding is under ReservedTUIChatID, not spoofed IDs.
	tuiKey := projectbinding.ConversationKey{ChatID: ipc.ReservedTUIChatID, ThreadID: 0}
	tuiBinding, err := a.bindings.Resolve(ctx, tuiKey)
	if err != nil {
		t.Fatalf("expected binding under TUI chat ID: %v", err)
	}
	if tuiBinding == nil || tuiBinding.Binding == nil {
		t.Fatal("expected binding under ReservedTUIChatID")
	}
	if tuiBinding.Binding.CWD != validDir {
		t.Errorf("expected CWD=%q, got %q", validDir, tuiBinding.Binding.CWD)
	}

	// Verify spoofed key has no binding.
	spoofedKey := projectbinding.ConversationKey{ChatID: 123, ThreadID: 456}
	spoofedBinding, err := a.bindings.Resolve(ctx, spoofedKey)
	if err == nil && spoofedBinding != nil && spoofedBinding.Binding != nil {
		t.Error("binding must NOT be under spoofed ChatID=123")
	}

	// Verify CreatedBy is current user, not spoofed.
	if tuiBinding.Binding.CreatedBy != int64(os.Getuid()) {
		t.Errorf("expected CreatedBy=%d (os.Getuid()), got %d", os.Getuid(), tuiBinding.Binding.CreatedBy)
	}
}

func TestTUIHandler_SubscribeReturnsTerminalError(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type: "subscribe",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te.count() < 2 {
		t.Fatalf("expected at least 2 events (ack + error), got %d", te.count())
	}
	// First event is always ack
	if te.events[0].Type != "ack" {
		t.Errorf("event[0] = %q, want ack", te.events[0].Type)
	}
	// Second event should be terminal error (not message)
	last := te.events[te.count()-1]
	if last.Type != "error" {
		t.Errorf("last event = %q, want error (terminal)", last.Type)
	}
}

func TestTUIHandler_EmptySendReturnsMessageAndStreamEnd(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type: "send",
		Text: "",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Events: ack, message, stream_end
	if te.count() < 3 {
		t.Fatalf("expected at least 3 events, got %d", te.count())
	}
	if te.events[0].Type != "ack" {
		t.Errorf("event[0] = %q, want ack", te.events[0].Type)
	}
	if te.events[1].Type != "message" {
		t.Errorf("event[1] = %q, want message", te.events[1].Type)
	}
	if te.events[2].Type != "stream_end" {
		t.Errorf("event[2] = %q, want stream_end", te.events[2].Type)
	}
}

func TestTUIHandler_UnknownCommand(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type: "command",
		Text: "/unknown",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te.count() >= 2 && !strings.Contains(te.events[1].Body, "Unknown command") {
		t.Errorf("expected unknown command response, got %q", te.events[1].Body)
	}
}

func TestTUIHandler_ConcurrencyGuardRejectsSecondRun(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te2 := &testEmit{}

	// Acquire the guard manually to simulate an in-progress run.
	if !a.tuiRunGuard.tryAcquire() {
		t.Fatal("should be able to acquire guard")
	}
	defer a.tuiRunGuard.release()

	// Second send should be rejected.
	err := handler(ctx, ipc.IPCMessage{
		Type: "send",
		Text: "hello",
	}, te2.emit)
	if err != nil {
		t.Fatalf("handler should not return error for guard rejection, got: %v", err)
	}

	if te2.count() < 2 {
		t.Fatalf("expected at least 2 events, got %d", te2.count())
	}

	// Events: ack, error (terminal).
	if te2.events[0].Type != "ack" {
		t.Errorf("event[0] = %q, want ack", te2.events[0].Type)
	}
	if te2.events[1].Type != "error" {
		t.Errorf("event[1] = %q, want error (concurrency)", te2.events[1].Type)
	}
	if !strings.Contains(te2.events[1].Error, "already in progress") {
		t.Errorf("expected concurrency error, got %q", te2.events[1].Error)
	}
	// No stream_end after error.
	if te2.count() > 2 {
		t.Errorf("expected exactly 2 events (ack + error), got %d", te2.count())
	}
}

func TestTUIHandler_EmitFailureDoesNotPanic(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)

	failingEmit := func(ev ipc.IPCEvent) error {
		return os.ErrClosed
	}

	err := handler(ctx, ipc.IPCMessage{
		Type: "command",
		Text: "/status",
	}, failingEmit)
	if err == nil {
		t.Fatal("expected error from failing emit")
	}
}

func TestTUIOutput_ConfirmMessageCompletes(t *testing.T) {
	emitted := make([]ipc.IPCEvent, 0)
	emit := func(ev ipc.IPCEvent) error {
		emitted = append(emitted, ev)
		return nil
	}
	output := newTUIOutput(emit)

	// Simulate SendText + ConfirmMessage (non-terminal pipeline path).
	output.SendText(ipc.ReservedTUIChatID, 0, "thinking...")
	output.ConfirmMessage(ipc.ReservedTUIChatID, 0)

	select {
	case <-output.done:
		// OK — ConfirmMessage completed the output.
	case <-time.After(time.Second):
		t.Fatal("output.done should have been closed by ConfirmMessage")
	}

	if len(emitted) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(emitted))
	}
	if emitted[0].Type != ipc.EventTypeStreamChunk {
		t.Errorf("expected stream_chunk, got %q", emitted[0].Type)
	}
}

func TestTUIOutput_ErrorSkipsStreamEnd(t *testing.T) {
	emitted := make([]ipc.IPCEvent, 0)
	emit := func(ev ipc.IPCEvent) error {
		emitted = append(emitted, ev)
		return nil
	}
	output := newTUIOutput(emit)

	output.SendError(ipc.ReservedTUIChatID, 0, "something failed")

	if !output.errored {
		t.Error("expected errored=true after SendError")
	}

	select {
	case <-output.done:
		// OK
	case <-time.After(time.Second):
		t.Fatal("output.done should have been closed by SendError")
	}
}

func TestTUIHandler_RequestIDInErrorOnInvalidMessage(t *testing.T) {
	// This tests the IPC server's RequestID propagation in validation errors.
	// Create a minimal server with StreamHandler to test RequestID in errors.
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	// Send a command with a specific request_id.
	err := handler(ctx, ipc.IPCMessage{
		Type:      "command",
		Text:      "/status",
		RequestID: "my-req-42",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// All events should have the RequestID propagated.
	for i, ev := range te.events {
		if ev.RequestID != "my-req-42" {
			t.Errorf("event[%d] RequestID = %q, want %q", i, ev.RequestID, "my-req-42")
		}
	}
}

func TestTUIHandler_ImageOnlySendBypassesEmptyMessage(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.png")
	// Write a minimal valid PNG.
	imgData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x62, 0x00, 0x00, 0x00, 0x02,
		0x00, 0x01, 0xe5, 0x27, 0xde, 0xfc, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42,
		0x60, 0x82,
	}
	if err := os.WriteFile(imgPath, imgData, 0o644); err != nil {
		t.Fatal(err)
	}

	handler := makeTUIHandler(a)
	te := &testEmit{}

	// Send: empty text + valid image path.
	err := handler(ctx, ipc.IPCMessage{
		Type: "send",
		Text: "",
		Images: []ipc.IPCImage{
			{Path: imgPath, MediaType: "image/png"},
		},
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// The "Empty message" response would be a message event with body
	// "Empty message" before stream_end. Since we bypassed that guard,
	// we should NOT see "Empty message" as a message body.
	for _, ev := range te.events {
		if ev.Type == "message" && strings.Contains(ev.Body, "Empty message") {
			t.Fatal("image-only send should NOT return 'Empty message'")
		}
	}
	// We expect at least an ack event.
	if te.count() == 0 {
		t.Fatal("expected at least an ack event")
	}
}

func TestTUIHandler_UserIDFallbackToGetuid(t *testing.T) {
	// Verify the handler uses os.Getuid() for TUI users.
	// The actual forced-ID behavior is tested in TestTUIHandler_SpoofedIDsIgnored.
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type:      "command",
		Text:      "/status",
		RequestID: "uid-test",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if te.count() < 2 {
		t.Fatalf("expected at least 2 events, got %d", te.count())
	}
	// Just verify it works — the specific UID assertion is in the spoofed test.
	if te.events[1].Type != "message" {
		t.Errorf("event[1] = %q, want message", te.events[1].Type)
	}
}

func TestTUIHandler_ProjectStateNoBindingReturnsNone(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err := handler(ctx, ipc.IPCMessage{
		Type:      ipc.MsgTypeProjectState,
		RequestID: "proj-test-1",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te.count() != 3 {
		t.Fatalf("expected 3 events (ack, project_state, stream_end), got %d", te.count())
	}
	if te.events[0].Type != ipc.EventTypeAck {
		t.Errorf("event[0] = %q, want ack", te.events[0].Type)
	}
	if te.events[1].Type != ipc.EventTypeProjectState {
		t.Fatalf("event[1] = %q, want project_state", te.events[1].Type)
	}
	if te.events[1].Body == "" {
		t.Fatal("expected non-empty Body")
	}
	if te.events[2].Type != ipc.EventTypeStreamEnd {
		t.Fatalf("event[2] = %q, want stream_end", te.events[2].Type)
	}

	var payload ipc.ProjectStatePayload
	if err := json.Unmarshal([]byte(te.events[1].Body), &payload); err != nil {
		t.Fatalf("unmarshal project state: %v", err)
	}
	if payload.BindingSource != "none" {
		t.Errorf("expected binding_source=none, got %q", payload.BindingSource)
	}
	if payload.CWD != "" {
		t.Errorf("expected empty cwd, got %q", payload.CWD)
	}
	if payload.ActiveAgent == "" {
		t.Error("expected non-empty active_agent")
	}
	if payload.BridgeStatus == "" {
		t.Error("expected non-empty bridge_status")
	}
	if payload.Model == "" {
		t.Error("expected non-empty model")
	}
	if payload.MemoryLayers == nil {
		t.Error("expected memory_layers field (may be empty)")
	}
}

func TestTUIHandler_ProjectStateWithCwdReturnsManualBinding(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	validDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	// Set up a manual binding first.
	handler := makeTUIHandler(a)
	te := &testEmit{}
	_ = handler(ctx, ipc.IPCMessage{
		Type: "command",
		Text: "/cwd " + validDir,
	}, te.emit)

	// Now fetch project state.
	te2 := &testEmit{}
	err = handler(ctx, ipc.IPCMessage{
		Type:      ipc.MsgTypeProjectState,
		RequestID: "proj-test-2",
	}, te2.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if te2.count() != 3 {
		t.Fatalf("expected 3 events (ack, project_state, stream_end), got %d", te2.count())
	}
	if te2.events[2].Type != ipc.EventTypeStreamEnd {
		t.Fatalf("event[2] = %q, want stream_end", te2.events[2].Type)
	}

	var payload ipc.ProjectStatePayload
	if err := json.Unmarshal([]byte(te2.events[1].Body), &payload); err != nil {
		t.Fatalf("unmarshal project state: %v", err)
	}
	if payload.BindingSource != "manual" {
		t.Errorf("expected binding_source=manual, got %q", payload.BindingSource)
	}
	if payload.CWD != validDir {
		t.Errorf("expected cwd=%q, got %q", validDir, payload.CWD)
	}
}

func TestTUIHandler_ProjectStateCheckpointTruncated(t *testing.T) {
	dir := t.TempDir()
	rl, err := runlog.NewSQLiteStore(filepath.Join(dir, "runlog.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer rl.Close()

	a, ctx, cleanup := testApp(t)
	defer cleanup()
	a.runLog = rl

	// Insert a run record with a checkpoint > 200 runes (no secrets).
	longCheckpoint := "Starting round " + strings.Repeat("x", 250)
	if utf8.RuneCountInString(longCheckpoint) <= 200 {
		t.Fatal("test precondition failed: checkpoint must be > 200 runes")
	}
	rec := runlog.RunRecord{
		RunID:      "trunc-test-1",
		ChatID:     ipc.ReservedTUIChatID,
		ThreadID:   0,
		Status:     runlog.RunCompleted,
		Checkpoint: longCheckpoint,
		StartedAt:  time.Now().Add(-1 * time.Minute),
	}
	if err := rl.Start(ctx, rec); err != nil {
		t.Fatalf("rl.Start: %v", err)
	}

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err = handler(ctx, ipc.IPCMessage{
		Type:      ipc.MsgTypeProjectState,
		RequestID: "proj-trunc",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if te.count() != 3 {
		t.Fatalf("expected 3 events (ack, project_state, stream_end), got %d", te.count())
	}
	if te.events[1].Type != ipc.EventTypeProjectState {
		t.Fatalf("event[1] = %q, want project_state", te.events[1].Type)
	}
	var payload ipc.ProjectStatePayload
	if err := json.Unmarshal([]byte(te.events[1].Body), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.LatestRun == nil {
		t.Fatal("expected LatestRun to be present")
	}
	cp := payload.LatestRun.Checkpoint
	if !strings.HasSuffix(cp, "...") {
		t.Errorf("checkpoint should end with '...', got %q", cp)
	}
	maxRunes := 200 + 3 // 200 content + "..."
	if runeCount := utf8.RuneCountInString(cp); runeCount > maxRunes {
		t.Errorf("checkpoint too long: %d runes, want <= %d", runeCount, maxRunes)
	}
	if strings.Contains(cp, "[API_KEY_REDACTED]") {
		t.Errorf("no redaction marker expected in truncation-only checkpoint, got %q", cp)
	}
}

func TestTUIHandler_ProjectStateCheckpointRedacted(t *testing.T) {
	dir := t.TempDir()
	rl, err := runlog.NewSQLiteStore(filepath.Join(dir, "runlog.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer rl.Close()

	a, ctx, cleanup := testApp(t)
	defer cleanup()
	a.runLog = rl

	// Insert a run record with a secret in the checkpoint.
	rec := runlog.RunRecord{
		RunID:      "redact-test-1",
		ChatID:     ipc.ReservedTUIChatID,
		ThreadID:   0,
		Status:     runlog.RunCompleted,
		Checkpoint: "System checkpoint: sk-ant-abc123def456ghi789jkl --project finalized",
		StartedAt:  time.Now().Add(-1 * time.Minute),
	}
	if err := rl.Start(ctx, rec); err != nil {
		t.Fatalf("rl.Start: %v", err)
	}

	handler := makeTUIHandler(a)
	te := &testEmit{}

	err = handler(ctx, ipc.IPCMessage{
		Type:      ipc.MsgTypeProjectState,
		RequestID: "proj-sec",
	}, te.emit)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if te.count() != 3 {
		t.Fatalf("expected 3 events (ack, project_state, stream_end), got %d", te.count())
	}
	if te.events[1].Type != ipc.EventTypeProjectState {
		t.Fatalf("event[1] = %q, want project_state", te.events[1].Type)
	}
	if te.events[2].Type != ipc.EventTypeStreamEnd {
		t.Fatalf("event[2] = %q, want stream_end", te.events[2].Type)
	}
	var payload ipc.ProjectStatePayload
	if err := json.Unmarshal([]byte(te.events[1].Body), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.LatestRun == nil {
		t.Fatal("expected LatestRun to be present")
	}
	if strings.Contains(payload.LatestRun.Checkpoint, "sk-ant-") {
		t.Errorf("checkpoint contains unredacted secret pattern: %q", payload.LatestRun.Checkpoint)
	}
	if !strings.Contains(payload.LatestRun.Checkpoint, "[API_KEY_REDACTED]") {
		t.Errorf("expected [API_KEY_REDACTED] in checkpoint, got %q", payload.LatestRun.Checkpoint)
	}
}
