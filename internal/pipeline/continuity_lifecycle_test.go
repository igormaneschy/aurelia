package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/users"
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

	svc.afterSuccessfulTurn(42, 0, "user text", "assistant response", "run-abc", 100, false)

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
	_ = svc.handleContextOutcome(parentCtx, ctx, 42, 0, 100, false, tracker)

	state, err := contStore.Get(context.Background(), 42, 0, 100)
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
	outcome := svc.handleResultEvent(42, 0, 100, ev, &assistantText, "user intent", 100, false)

	if outcome != OutcomeLLMError {
		t.Fatalf("expected OutcomeLLMError, got %v", outcome)
	}

	state, err := contStore.Get(context.Background(), 42, 0, 100)
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
	outcome := svc.handleErrorEvent(42, 0, 100, ev, 100, false)

	if outcome != OutcomeLLMError {
		t.Fatalf("expected OutcomeLLMError, got %v", outcome)
	}

	state, err := contStore.Get(context.Background(), 42, 0, 100)
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

// --- Project Work State dual-write tests ---

// TestProjectWorkState_DualWriteOnSuccess verifies that after a successful turn
// with /cwd active, a ProjectWorkState row is created for (userID, projectSlug).
func TestProjectWorkState_DualWriteOnSuccess(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	svc := &Service{
		continuity: contStore,
		sessions:   ss,
		runLog:     &fakeRunLogStore{},
		entryPoint: "telegram",
	}

	// Set CWD via session so effectiveCwd resolves
	ss.SetCwd(42, 0, "/Users/test/my-project")
	ss.SetSession(42, 0, 100, "sid-100")

	svc.afterSuccessfulTurn(42, 0, "analyze the code", "here is the analysis", "run-001", 100, false)

	ctx := t.Context()
	slug := runtime.ProjectSlug("/Users/test/my-project")
	state, err := contStore.GetProjectWork(ctx, 100, slug)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected ProjectWorkState to be created")
	}
	if state.LastUserIntent != "analyze the code" {
		t.Fatalf("LastUserIntent = %q, want %q", state.LastUserIntent, "analyze the code")
	}
	if !strings.Contains(state.LastAssistantSummary, "here is the analysis") {
		t.Fatalf("LastAssistantSummary = %q, want to contain %q", state.LastAssistantSummary, "here is the analysis")
	}
	if state.LastRunID != "run-001" {
		t.Fatalf("LastRunID = %q, want %q", state.LastRunID, "run-001")
	}
	if state.LastRunStatus != "completed" {
		t.Fatalf("LastRunStatus = %q, want %q", state.LastRunStatus, "completed")
	}
	if state.LastEntrypoint != "telegram" {
		t.Fatalf("LastEntrypoint = %q, want %q", state.LastEntrypoint, "telegram")
	}
	if state.LastChatID != 42 {
		t.Fatalf("LastChatID = %d, want %d", state.LastChatID, 42)
	}
}

// TestProjectWorkState_DualWriteOnFailure verifies that after a failed turn
// with /cwd active, ProjectWorkState receives checkpoint and status.
func TestProjectWorkState_DualWriteOnFailure(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	svc := &Service{
		continuity:   contStore,
		sessions:     ss,
		runLog:       &fakeRunLogStore{},
		runLogStates: make(map[string]*runLogState),
		output:       &fakeOutput{},
		entryPoint:   "tui",
	}

	// Set CWD so effectiveCwd resolves
	ss.SetCwd(42, 0, "/Users/test/project-b")
	ss.SetSession(42, 0, 100, "sid-100")

	ev := newFakeErrorEvent("rate limit exceeded")
	svc.handleErrorEvent(42, 0, 100, ev, 100, false)

	ctx := t.Context()
	slug := runtime.ProjectSlug("/Users/test/project-b")
	state, err := contStore.GetProjectWork(ctx, 100, slug)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected ProjectWorkState to be created on failure")
	}
	if state.LastRunStatus != "failed" {
		t.Fatalf("LastRunStatus = %q, want %q", state.LastRunStatus, "failed")
	}
	if state.LastCheckpoint == "" {
		t.Fatal("expected LastCheckpoint to be set")
	}
	if state.LastEntrypoint != "tui" {
		t.Fatalf("LastEntrypoint = %q, want %q", state.LastEntrypoint, "tui")
	}
}

// TestProjectWorkState_NoWriteWithoutCwd verifies that without /cwd active,
// no ProjectWorkState row is created.
func TestProjectWorkState_NoWriteWithoutCwd(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	svc := &Service{
		continuity: contStore,
		sessions:   ss,
		runLog:     &fakeRunLogStore{},
		entryPoint: "telegram",
	}

	// No CWD set — effectiveCwd returns ""
	svc.afterSuccessfulTurn(42, 0, "hello", "hi there", "run-002", 100, false)

	ctx := t.Context()
	// Try a few possible slugs — none should exist
	for _, slug := range []string{"", "hello", "42"} {
		state, _ := contStore.GetProjectWork(ctx, 100, slug)
		if state != nil {
			t.Fatalf("unexpected ProjectWorkState for slug %q", slug)
		}
	}
}

// TestProjectWorkState_NoWriteWithZeroUserID verifies that userID=0 skips
// the project write (no isolation possible).
func TestProjectWorkState_NoWriteWithZeroUserID(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	svc := &Service{
		continuity: contStore,
		sessions:   ss,
		runLog:     &fakeRunLogStore{},
		entryPoint: "telegram",
	}

	ss.SetCwd(42, 0, "/Users/test/project-c")
	svc.afterSuccessfulTurn(42, 0, "hello", "hi", "run-003", 0, false)

	ctx := t.Context()
	slug := runtime.ProjectSlug("/Users/test/project-c")
	state, _ := contStore.GetProjectWork(ctx, 0, slug)
	if state != nil {
		t.Fatal("should not create ProjectWorkState for userID=0")
	}
}

// TestProjectWorkState_CrossChatIDSameSlug verifies that different chatIDs
// with the same userID and cwd produce the same ProjectWorkState (cross-surface).
func TestProjectWorkState_CrossChatIDSameSlug(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	svc := &Service{
		continuity: contStore,
		sessions:   ss,
		runLog:     &fakeRunLogStore{},
		entryPoint: "telegram",
	}

	// Turn 1: Telegram chat
	ss.SetCwd(50929027, 0, "/Users/test/aurelia")
	svc.afterSuccessfulTurn(50929027, 0, "start analysis", "analysis done", "run-tg", 100, false)

	// Turn 2: TUI chat (different chatID, same user+cwd)
	svc.entryPoint = "tui"
	ss.SetCwd(-9000001, 0, "/Users/test/aurelia")
	svc.afterSuccessfulTurn(-9000001, 0, "continue where we left off", "resuming", "run-tui", 100, false)

	ctx := t.Context()
	slug := runtime.ProjectSlug("/Users/test/aurelia")
	state, err := contStore.GetProjectWork(ctx, 100, slug)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected ProjectWorkState to exist")
	}
	// The second turn should have updated the state
	if state.LastUserIntent != "continue where we left off" {
		t.Fatalf("LastUserIntent = %q, want %q (cross-surface update)", state.LastUserIntent, "continue where we left off")
	}
	if state.LastEntrypoint != "tui" {
		t.Fatalf("LastEntrypoint = %q, want %q (latest entrypoint)", state.LastEntrypoint, "tui")
	}
	if state.LastChatID != -9000001 {
		t.Fatalf("LastChatID = %d, want %d (latest chatID)", state.LastChatID, -9000001)
	}
}

// TestProjectWorkState_DualWriteDefaultCWD verifies that when a private chat
// has no explicit /cwd but the user profile has DefaultCWD, a successful turn
// writes a ProjectWorkState row for the resolved slug (DefaultCWD fallback).
func TestProjectWorkState_DualWriteDefaultCWD(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	resolver := users.NewResolver(dir)
	usersStore := users.NewStore(resolver)
	if err := usersStore.Save(&users.Profile{
		UserID:     100,
		DefaultCWD: dir,
	}); err != nil {
		t.Fatal(err)
	}

	ss := session.NewStore()
	svc := &Service{
		continuity: contStore,
		sessions:   ss,
		usersStore: usersStore,
		runLog:     &fakeRunLogStore{},
		entryPoint: "telegram",
	}

	// No explicit SetCwd — effectiveCwdForContext falls back to DefaultCWD
	svc.afterSuccessfulTurn(42, 0, "analyze the code", "analysis done", "run-dd", 100, true)

	ctx := t.Context()
	slug := runtime.ProjectSlug(resolved)
	state, err := contStore.GetProjectWork(ctx, 100, slug)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected ProjectWorkState to be created via DefaultCWD fallback")
	}
	if state.LastUserIntent != "analyze the code" {
		t.Fatalf("LastUserIntent = %q, want %q", state.LastUserIntent, "analyze the code")
	}
}

// TestProjectWorkState_SessionColdPreservesCWD verifies that
// patchContinuitySessionCold with /cwd active writes the resolved CWD
// to the ProjectWorkState row.
func TestProjectWorkState_SessionColdPreservesCWD(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	svc := &Service{
		continuity: contStore,
		sessions:   ss,
		entryPoint: "telegram",
	}

	ss.SetCwd(42, 0, "/Users/test/project-cold")

	svc.patchContinuitySessionCold(42, 0, "bridge died", 100, false)

	ctx := t.Context()
	slug := runtime.ProjectSlug("/Users/test/project-cold")
	state, err := contStore.GetProjectWork(ctx, 100, slug)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected ProjectWorkState to exist after session cold")
	}
	if state.CWD != "/Users/test/project-cold" {
		t.Fatalf("CWD = %q, want %q", state.CWD, "/Users/test/project-cold")
	}
	if state.LastRunStatus != "cold" {
		t.Fatalf("LastRunStatus = %q, want %q", state.LastRunStatus, "cold")
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
