package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/session"
)

// TestContinuityAfterSuccessfulTurn verifies that after a successful turn,
// continuity state contains user intent, assistant summary, and run status.
func TestContinuityAfterSuccessfulTurn(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	ss.SetSession(42, 0, 100, "sid-user-100")
	ss.SetSession(42, 0, 200, "sid-user-200")
	svc := &Service{
		continuity: contStore,
		sessions:   ss,
		runLog:     &fakeRunLogStore{},
	}

	svc.afterSuccessfulTurn(42, 0, "user text", "assistant response", "run-abc", 100)

	ctx := t.Context()
	state, err := contStore.Get(ctx, 42, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected non-nil continuity state")
	}
	if state.LastUserIntent != "user text" {
		t.Fatalf("LastUserIntent = %q, want %q", state.LastUserIntent, "user text")
	}
	if !strings.Contains(state.LastAssistantSummary, "assistant response") {
		t.Fatalf("LastAssistantSummary = %q, want to contain %q", state.LastAssistantSummary, "assistant response")
	}
	if state.LastRunStatus != "completed" {
		t.Fatalf("LastRunStatus = %q, want %q", state.LastRunStatus, "completed")
	}
	if state.SessionID != "sid-user-100" {
		t.Fatalf("SessionID = %q, want sender session", state.SessionID)
	}
	if state.SessionCold {
		t.Fatal("SessionCold should be false after successful turn")
	}
}

// TestContinuityAfterTimeout verifies that after a timeout, continuity state
// marks session cold with the timeout reason.
func TestContinuityAfterTimeout(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	ss.SetSession(42, 0, 100, "sid-user-100")
	ss.SetSession(42, 0, 200, "sid-user-200")
	svc := &Service{
		continuity: contStore,
		sessions:   ss,
	}

	// Simulate handleContextOutcome timeout path
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	parentCtx := context.Background()

	// Need a fake output to satisfy handleContextOutcome
	fo := &fakeOutput{}
	svc.output = fo

	tracker := newRunTimeoutTracker()
	tracker.mark(timeoutOriginMaxExecution)
	_ = svc.handleContextOutcome(parentCtx, ctx, 42, 0, 100, tracker)

	state, err := contStore.Get(context.Background(), 42, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected non-nil continuity state after timeout")
	}
	if !state.SessionCold {
		t.Fatal("SessionCold should be true after timeout")
	}
	if state.LastRunStatus != "timed_out" {
		t.Fatalf("LastRunStatus = %q, want %q", state.LastRunStatus, "timed_out")
	}
	if _, active := ss.GetSessionWithState(42, 0, 100); active {
		t.Fatal("sender session should be inactive after timeout")
	}
	if _, active := ss.GetSessionWithState(42, 0, 200); !active {
		t.Fatal("other user session should remain active after timeout")
	}
}

// TestContinuityAfterEmptyResult verifies that after an empty result with work,
// continuity state marks session cold with failure reason.
func TestContinuityAfterEmptyResult(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	ss.SetSession(42, 0, 100, "sid-user-100")
	ss.SetSession(42, 0, 200, "sid-user-200")

	// Need a runLogStore to prevent panic in completeRunLog
	runLogStore := &fakeRunLogStore{}
	svc := &Service{
		continuity:   contStore,
		sessions:     ss,
		runLog:       runLogStore,
		runLogStates: make(map[string]*runLogState),
		output:       &fakeOutput{},
	}

	// Trigger empty result with work via handleResultEvent
	var assistantText strings.Builder
	ev := newFakeResultEvent("", 5, 1000, 200, 0.05)
	outcome := svc.handleResultEvent(42, 0, 100, ev, &assistantText, "user intent", 100)

	if outcome != OutcomeLLMError {
		t.Fatalf("expected OutcomeLLMError, got %v", outcome)
	}

	state, err := contStore.Get(context.Background(), 42, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected non-nil continuity state after empty result")
	}
	if !state.SessionCold {
		t.Fatal("SessionCold should be true after empty result")
	}
	if state.LastRunStatus != "failed" {
		t.Fatalf("LastRunStatus = %q, want %q", state.LastRunStatus, "failed")
	}
	if _, active := ss.GetSessionWithState(42, 0, 100); active {
		t.Fatal("sender session should be inactive after empty result")
	}
	if _, active := ss.GetSessionWithState(42, 0, 200); !active {
		t.Fatal("other user session should remain active after empty result")
	}
}

// TestContinuityAfterError verifies that after a bridge error, continuity state
// marks session cold with the error message.
func TestContinuityAfterError(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	svc := &Service{
		continuity:   contStore,
		sessions:     ss,
		runLog:       &fakeRunLogStore{},
		runLogStates: make(map[string]*runLogState),
		output:       &fakeOutput{},
	}

	ev := newFakeErrorEvent("API rate limit exceeded")
	outcome := svc.handleErrorEvent(42, 0, 100, ev, 100)

	if outcome != OutcomeLLMError {
		t.Fatalf("expected OutcomeLLMError, got %v", outcome)
	}

	state, err := contStore.Get(context.Background(), 42, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected non-nil continuity state after error")
	}
	if !state.SessionCold {
		t.Fatal("SessionCold should be true after error")
	}
	if state.LastRunStatus != "failed" {
		t.Fatalf("LastRunStatus = %q, want %q", state.LastRunStatus, "failed")
	}
}

// TestContinuitySessionID verifies that system events update the session ID.
func TestContinuitySessionID(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	svc := &Service{
		continuity: contStore,
		sessions:   ss,
	}

	svc.patchContinuitySessionID(42, 0, "sid-new-session", 100)

	ctx := context.Background()
	state, err := contStore.Get(ctx, 42, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected non-nil continuity state")
	}
	if state.SessionID != "sid-new-session" {
		t.Fatalf("SessionID = %q, want %q", state.SessionID, "sid-new-session")
	}
}

// TestContinuityMarkColdForSessions verifies MarkColdForSessions works.
func TestContinuityMarkColdForSessions(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ctx := context.Background()
	_ = contStore.Upsert(ctx, continuity.ConversationState{
		ChatID:    1,
		ThreadID:  0,
		UserID:    0,
		SessionID: "sid-1",
		UpdatedAt: time.Now(),
	})
	_ = contStore.Upsert(ctx, continuity.ConversationState{
		ChatID:    2,
		ThreadID:  0,
		UserID:    0,
		SessionID: "sid-2",
		UpdatedAt: time.Now(),
	})
	_ = contStore.Upsert(ctx, continuity.ConversationState{
		ChatID:    3,
		ThreadID:  0,
		UserID:    0,
		SessionID: "", // no session
		UpdatedAt: time.Now(),
	})

	err := contStore.MarkColdForSessions(ctx, "bridge died")
	if err != nil {
		t.Fatal(err)
	}

	// Rows with session_id should be cold
	for _, chatID := range []int64{1, 2} {
		state, _ := contStore.Get(ctx, chatID, 0, 0)
		if state == nil {
			t.Fatalf("expected state for chat %d", chatID)
		}
		if !state.SessionCold {
			t.Fatalf("chat %d should be cold after MarkColdForSessions", chatID)
		}
		if state.ResetReason != "bridge died" {
			t.Fatalf("chat %d ResetReason = %q, want %q", chatID, state.ResetReason, "bridge died")
		}
	}

	// Row without session_id should remain unchanged
	noSession, _ := contStore.Get(ctx, 3, 0, 0)
	if noSession == nil {
		t.Fatal("expected state for chat 3")
	}
	if noSession.SessionCold {
		t.Fatal("chat 3 should NOT be cold (no session_id)")
	}
}

// helpers

func newContinuityTestStore(t *testing.T) *continuity.SQLiteStore {
	t.Helper()
	store, err := continuity.NewSQLiteStore(filepath.Join(t.TempDir(), "cont_test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return store
}

func newFakeResultEvent(content string, turns int, inTokens, outTokens int, costUSD float64) bridge.Event {
	return bridge.Event{
		Type:         "result",
		Content:      content,
		NumTurns:     turns,
		InputTokens:  inTokens,
		OutputTokens: outTokens,
		CostUSD:      costUSD,
	}
}

func newFakeErrorEvent(message string) bridge.Event {
	return bridge.Event{
		Type:    "error",
		Message: message,
	}
}
