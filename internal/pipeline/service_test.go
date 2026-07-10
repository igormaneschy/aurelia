package pipeline

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/pkg/idgen"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/session"
)

func TestParseSessionKey_Valid(t *testing.T) {
	tests := []struct {
		key          string
		wantChatID   int64
		wantThreadID int
		wantUserID   int64
	}{
		{"42:7:100", 42, 7, 100},
		{"0:0:0", 0, 0, 0},
		{"-1:3:999", -1, 3, 999},
		{"12345:999:88888", 12345, 999, 88888},
	}
	for _, tt := range tests {
		chatID, threadID, userID, ok := parseSessionKey(tt.key)
		if !ok {
			t.Errorf("parseSessionKey(%q) = (_, _, _, false), want true", tt.key)
			continue
		}
		if chatID != tt.wantChatID {
			t.Errorf("parseSessionKey(%q) chatID = %d, want %d", tt.key, chatID, tt.wantChatID)
		}
		if threadID != tt.wantThreadID {
			t.Errorf("parseSessionKey(%q) threadID = %d, want %d", tt.key, threadID, tt.wantThreadID)
		}
		if userID != tt.wantUserID {
			t.Errorf("parseSessionKey(%q) userID = %d, want %d", tt.key, userID, tt.wantUserID)
		}
	}
}

func TestParseSessionKey_Malformed(t *testing.T) {
	tests := []string{
		"",
		"abc:def:ghi",
		"42:7",           // missing userID
		"42:7:100:extra", // extra trailing field
		"not-a-key",
		"42::100",   // empty threadID
		":7:100",    // empty chatID
		"42:7:",     // empty userID
		"42:7:100x", // trailing non-digit in userID field
		"42:7:100 ", // trailing whitespace
		" 42:7:100", // leading whitespace
	}
	for _, key := range tests {
		_, _, _, ok := parseSessionKey(key)
		if ok {
			t.Errorf("parseSessionKey(%q) = (_, _, _, true), want false", key)
		}
	}
}

// addActiveSession is a test helper to populate the activeSessions map.
// Returns the context so callers can check ctx.Done() for cancellation.
func addActiveSession(s *Service, chatID int64, threadID int, userID int64) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	s.activeSessions.Store(sessionKey(chatID, threadID, userID), cancel)
	return ctx
}

func TestScopedAbortRequest_SetsCommandAndOptions(t *testing.T) {
	req := scopedAbortRequest(42, 7, 100)

	if req.Command != "abort" {
		t.Errorf("Command = %q, want %q", req.Command, "abort")
	}
	if req.Options.ChatID != 42 {
		t.Errorf("ChatID = %d, want %d", req.Options.ChatID, 42)
	}
	if req.Options.ThreadID != 7 {
		t.Errorf("ThreadID = %d, want %d", req.Options.ThreadID, 7)
	}
	if req.Options.UserID != 100 {
		t.Errorf("UserID = %d, want %d", req.Options.UserID, 100)
	}
}

func TestScopedAbortRequest_ZeroOptions(t *testing.T) {
	req := scopedAbortRequest(0, 0, 0)

	if req.Command != "abort" {
		t.Errorf("Command = %q, want %q", req.Command, "abort")
	}
	if req.Options.ChatID != 0 {
		t.Errorf("ChatID = %d, want 0", req.Options.ChatID)
	}
	if req.Options.ThreadID != 0 {
		t.Errorf("ThreadID = %d, want 0", req.Options.ThreadID)
	}
	if req.Options.UserID != 0 {
		t.Errorf("UserID = %d, want 0", req.Options.UserID)
	}
}

func TestCancelAllForUser_NilService(t *testing.T) {
	if (*Service)(nil).CancelAllForUser(42) {
		t.Error("CancelAllForUser on nil service returned true, want false")
	}
}

func TestCancelAllForUser_NoMatch(t *testing.T) {
	s := &Service{}
	addActiveSession(s, 1, 2, 100)

	if s.CancelAllForUser(200) {
		t.Error("CancelAllForUser(200) returned true when only user 100 has sessions")
	}

	// Verify user 100's session is still intact
	_, ok := s.activeSessions.Load(sessionKey(1, 2, 100))
	if !ok {
		t.Error("user 100 session was removed after CancelAllForUser for user 200")
	}
}

func TestCancelAllForUser_TwoUsersSameChatSameThread(t *testing.T) {
	s := &Service{}

	chatID := int64(42)
	threadID := 7
	userA := int64(100)
	userB := int64(200)

	ctxA := addActiveSession(s, chatID, threadID, userA)
	addActiveSession(s, chatID, threadID, userB)

	// Verify both sessions exist before cancel
	if _, ok := s.activeSessions.Load(sessionKey(chatID, threadID, userA)); !ok {
		t.Fatal("userA session not found before cancel")
	}
	if _, ok := s.activeSessions.Load(sessionKey(chatID, threadID, userB)); !ok {
		t.Fatal("userB session not found before cancel")
	}

	// Cancel only user A
	if !s.CancelAllForUser(userA) {
		t.Error("CancelAllForUser(userA) returned false, want true")
	}

	// User A's session should be cancelled (context done) and removed
	if _, ok := s.activeSessions.Load(sessionKey(chatID, threadID, userA)); ok {
		t.Error("userA session still in activeSessions after CancelAllForUser")
	}
	select {
	case <-ctxA.Done():
		// expected — context was cancelled
	default:
		t.Error("userA cancel func was not called")
	}

	// User B's session should remain active and not cancelled
	if _, ok := s.activeSessions.Load(sessionKey(chatID, threadID, userB)); !ok {
		t.Error("userB session was removed after CancelAllForUser for userA")
	}
}

func TestCancelAllForUser_MultipleSessionsSameUser(t *testing.T) {
	s := &Service{}

	userID := int64(42)

	// User has sessions across different chats/threads
	sessions := []struct {
		chatID   int64
		threadID int
	}{
		{1, 0},
		{1, 1},
		{2, 0},
		{3, 5},
	}

	ctxs := make([]context.Context, len(sessions))
	for i, sess := range sessions {
		ctxs[i] = addActiveSession(s, sess.chatID, sess.threadID, userID)
	}

	// Also add a session for another user to verify it's not affected
	addActiveSession(s, 1, 0, 999)

	if !s.CancelAllForUser(userID) {
		t.Error("CancelAllForUser(userID) returned false, want true")
	}

	// All user's sessions should be removed and cancelled
	for i, sess := range sessions {
		key := sessionKey(sess.chatID, sess.threadID, userID)
		if _, ok := s.activeSessions.Load(key); ok {
			t.Errorf("user session key=%q still in map", key)
		}
		select {
		case <-ctxs[i].Done():
			// expected
		default:
			t.Errorf("user session %d cancel func was not called", i)
		}
	}

	// Other user's session remains
	if _, ok := s.activeSessions.Load(sessionKey(1, 0, 999)); !ok {
		t.Error("other user's session was removed")
	}
}

func TestCancelAllForUser_UserIDZero(t *testing.T) {
	s := &Service{}

	// UserID 0 sessions exist
	ctxZero := addActiveSession(s, 1, 0, 0)
	// Another user with ID 0 should also match and be cancelled
	addActiveSession(s, 2, 0, 0)
	// A non-zero user should be unaffected
	addActiveSession(s, 1, 0, 100)

	if !s.CancelAllForUser(0) {
		t.Error("CancelAllForUser(0) returned false, want true")
	}

	select {
	case <-ctxZero.Done():
		// expected
	default:
		t.Error("userID 0 cancel func was not called")
	}

	// Non-zero user remains
	if _, ok := s.activeSessions.Load(sessionKey(1, 0, 100)); !ok {
		t.Error("non-zero user session was incorrectly removed")
	}
}

func TestCancelAllForUser_NonCancelValueInMap(t *testing.T) {
	// Should not panic if stored value isn't a context.CancelFunc
	s := &Service{}
	s.activeSessions.Store(sessionKey(1, 0, 100), "not-a-cancel-func")

	// Should not panic
	if s.CancelAllForUser(100) {
		t.Error("CancelAllForUser returned true when value is not a CancelFunc")
	}

	// The key should still be deleted from the map
	if _, ok := s.activeSessions.Load(sessionKey(1, 0, 100)); ok {
		t.Error("key not deleted from activeSessions even though value was wrong type")
	}
}

func TestCancelAllForUser_MalformedKeyInMap(t *testing.T) {
	s := &Service{}

	// Store a malformed key directly
	s.activeSessions.Store("not-a-valid-key", context.CancelFunc(func() {}))

	// Store valid keys for both users
	addActiveSession(s, 1, 0, 100)
	addActiveSession(s, 1, 0, 200)

	// Should not panic
	if !s.CancelAllForUser(100) {
		t.Error("CancelAllForUser(100) returned false, want true")
	}

	// User 200 session still exists
	if _, ok := s.activeSessions.Load(sessionKey(1, 0, 200)); !ok {
		t.Error("user 200 session was removed")
	}

	// Malformed key should still be in the map (we skip, don't delete malformed keys)
	if _, ok := s.activeSessions.Load("not-a-valid-key"); !ok {
		t.Error("malformed key was deleted — we should skip, not delete")
	}
}

func TestCancelAllForUser_EmptyMap(t *testing.T) {
	s := &Service{}
	if s.CancelAllForUser(42) {
		t.Error("CancelAllForUser on empty map returned true, want false")
	}
}

func TestCancel_NilService(t *testing.T) {
	if (*Service)(nil).Cancel(1, 0) {
		t.Error("Cancel on nil service returned true, want false")
	}
}

func TestCancel_NilBridge(t *testing.T) {
	s := &Service{}
	if s.Cancel(1, 0) {
		t.Error("Cancel with nil bridge returned true, want false")
	}
}

func TestWorkStatus_NilService(t *testing.T) {
	text, turns := (*Service)(nil).WorkStatus(1, 0)
	if text != "" {
		t.Errorf("WorkStatus on nil service returned text=%q, want empty", text)
	}
	if turns != 0 {
		t.Errorf("WorkStatus on nil service returned turns=%d, want 0", turns)
	}
}

func TestWorkStatus_NilBridge(t *testing.T) {
	s := &Service{}
	text, turns := s.WorkStatus(1, 0)
	if text != "" {
		t.Errorf("WorkStatus with nil bridge returned text=%q, want empty", text)
	}
	if turns != 0 {
		t.Errorf("WorkStatus with nil bridge returned turns=%d, want 0", turns)
	}
}

func TestRecordToolUse_NoDeadlockWithCompleteRunLog(t *testing.T) {
	// This test ensures the WaitGroup added to runLogState prevents
	// race conditions between recordToolUse async DB update and
	// completeRunLog cleanup, without causing deadlocks.

	// Create a minimal Service with a mock runLog
	s := &Service{
		runLog:       &fakeRunLogStore{},
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}

	// Simulate startRunLog by creating a minimal state
	runID := idgen.New()
	key := runLogKey(42, 7, 100)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{
		runID: runID,
	}
	s.runLogMu.Unlock()

	// Verify initial state
	s.runLogMu.Lock()
	state := s.runLogStates[key]
	s.runLogMu.Unlock()
	if state == nil {
		t.Fatal("expected runLogState to exist")
	}

	// Simulate what recordToolUse does
	state.mu.Lock()
	state.summaryCount = 5 // Force an update
	state.summary.WriteString("Read")
	state.mu.Unlock()

	// Now completeRunLog should work without hanging
	done := make(chan struct{})
	go func() {
		s.completeRunLog(42, 7, 100, runlog.RunCompleted, "", "")
		close(done)
	}()

	select {
	case <-done:
		// success — no deadlock
	case <-time.After(3 * time.Second):
		t.Fatal("completeRunLog deadlocked or hung")
	}
}

func TestRunLogKey_IncludesUserID(t *testing.T) {
	if runLogKey(1, 2, 100) == runLogKey(1, 2, 200) {
		t.Fatal("runLogKey must isolate different users in the same chat/thread")
	}
}

func TestSavePartialAssistant_RedactsBeforeCheckpoint(t *testing.T) {
	s := &Service{runLogStates: make(map[string]*runLogState)}
	key := runLogKey(1, 0, 100)
	s.runLogStates[key] = &runLogState{runID: "run-1"}

	s.savePartialAssistant(1, 0, 100, "partial token sk-proj-abc123def4567890abcdef")

	state := s.runLogStates[key]
	state.mu.Lock()
	partial := state.partialAssistant
	state.mu.Unlock()
	if strings.Contains(partial, "sk-proj-") {
		t.Fatalf("partial assistant leaked secret: %q", partial)
	}
	if !strings.Contains(partial, "REDACTED") {
		t.Fatalf("partial assistant missing redaction marker: %q", partial)
	}
}

func TestRecordPipelineEvent_RunIDFallback(t *testing.T) {
	// Ensures recordPipelineEvent uses ev.RunID as fallback when the
	// runLogStates entry has been deleted by completeRunLog.
	// This prevents silent dropping of run_completed events.

	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}

	// Simulate: state was deleted (e.g. by completeRunLog), but caller
	// passes a captured runID in the event.
	fallbackRunID := "run-after-state-cleanup"
	s.recordPipelineEvent(42, 7, 100, observability.NewEvent(fallbackRunID,
		observability.PhaseRunCompleted, "status=completed"))

	evs := spy.recordedEvents()
	if len(evs) != 1 {
		t.Fatalf("expected 1 recorded event, got %d", len(evs))
	}
	if evs[0].RunID != fallbackRunID {
		t.Errorf("RunID = %q, want %q", evs[0].RunID, fallbackRunID)
	}
	if evs[0].Phase != string(observability.PhaseRunCompleted) {
		t.Errorf("Phase = %q, want %q", evs[0].Phase, observability.PhaseRunCompleted)
	}
}

func TestUpdateRunLogSessionFile_PersistsSessionFile(t *testing.T) {
	// Ensures updateRunLogSessionFile writes the session_file through the
	// runlog.Store Update path, enabling GetLastOutboundMessage to bridge
	// PI sessions to Telegram outbound messages.

	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}

	runID := idgen.New()
	key := runLogKey(42, 7, 100)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID}
	s.runLogMu.Unlock()

	sessionFile := "/home/user/.pi/agent/sessions/session-abc.json"
	s.updateRunLogSessionFile(42, 7, 100, sessionFile)

	updates := spy.recordedUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if updates[0].RunID != runID {
		t.Errorf("RunID = %q, want %q", updates[0].RunID, runID)
	}
	if updates[0].SessionFile == nil {
		t.Fatal("SessionFile should not be nil")
	}
	if *updates[0].SessionFile != sessionFile {
		t.Errorf("SessionFile = %q, want %q", *updates[0].SessionFile, sessionFile)
	}
}

// spyRunLogStore captures RecordEvent and Update calls for test assertions.
type spyRunLogStore struct {
	mu      sync.Mutex
	events  []runlog.RunEvent
	updates []runlog.RunUpdate
}

func (s *spyRunLogStore) Start(_ context.Context, _ runlog.RunRecord) error { return nil }
func (s *spyRunLogStore) Update(_ context.Context, u runlog.RunUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, u)
	return nil
}
func (s *spyRunLogStore) Complete(_ context.Context, _ string, _ runlog.RunStatus, _, _, _ string) error {
	return nil
}
func (s *spyRunLogStore) RecordEvents(_ context.Context, events []runlog.RunEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, events...)
	return nil
}
func (s *spyRunLogStore) Prune(_ context.Context, _ runlog.PruneOptions) (runlog.PruneResult, error) {
	return runlog.PruneResult{}, nil
}
func (s *spyRunLogStore) Latest(_ context.Context, _ int64, _ int) (*runlog.RunRecord, error) {
	return nil, nil
}
func (s *spyRunLogStore) RecordEvent(_ context.Context, ev runlog.RunEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return nil
}
func (s *spyRunLogStore) ListEvents(_ context.Context, _ string) ([]runlog.RunEvent, error) {
	return nil, nil
}
func (s *spyRunLogStore) GetRun(_ context.Context, _ string) (*runlog.RunRecord, error) {
	return nil, nil
}
func (s *spyRunLogStore) ListRuns(_ context.Context, _ int64, _ int) ([]runlog.RunRecord, error) {
	return nil, nil
}
func (s *spyRunLogStore) Metrics(_ context.Context, _ runlog.MetricsFilter) (*runlog.MetricsResult, error) {
	return nil, nil
}
func (s *spyRunLogStore) GetLastOutboundMessage(_ context.Context, _ string) (int64, int, int64, error) {
	return 0, 0, 0, nil
}
func (s *spyRunLogStore) Close() error { return nil }

func (s *spyRunLogStore) recordedEvents() []runlog.RunEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]runlog.RunEvent, len(s.events))
	copy(out, s.events)
	return out
}

func (s *spyRunLogStore) recordedUpdates() []runlog.RunUpdate {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]runlog.RunUpdate, len(s.updates))
	copy(out, s.updates)
	return out
}

// TestRecordPipelineEvent_DropsEventWithoutRunID ensures events with empty RunID
// are silently dropped when no runLogState exists (e.g. after completeRunLog without
// capturing the runID first). This is the failure mode that the fixes in
// handleContextOutcome and executeAsync prevent.
func TestRecordPipelineEvent_DropsEventWithoutRunID(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}

	// No state exists — caller passes empty runID.
	s.recordPipelineEvent(42, 7, 100, observability.NewEvent("",
		observability.PhaseRunCanceled, "cancelado pelo usuário"))

	evs := spy.recordedEvents()
	if len(evs) != 0 {
		t.Fatalf("expected 0 recorded events (should be dropped), got %d", len(evs))
	}
}

// TestHandleContextOutcome_CancelledCapturesRunID verifies that handleContextOutcome
// captures the runID before calling completeRunLog, so the terminal run_canceled
// event is recorded even after state deletion.
func TestHandleContextOutcome_CancelledCapturesRunID(t *testing.T) {
	spy := &spyRunLogStore{}
	runID := idgen.New()
	s := &Service{
		runLog: spy,
		runLogStates: map[string]*runLogState{
			runLogKey(1, 0, 100): {runID: runID},
		},
		runLogMu: sync.Mutex{},
	}

	// Create a cancelled parent context to trigger the cancel path.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handled := s.handleContextOutcome(ctx, context.Background(), 1, 0, 100, false)
	if !handled {
		t.Fatal("expected handleContextOutcome to return true for cancelled parentCtx")
	}

	// Verify the run_canceled event was recorded with the correct runID.
	evs := spy.recordedEvents()
	var found bool
	for _, ev := range evs {
		if ev.Phase == string(observability.PhaseRunCanceled) {
			found = true
			if ev.RunID != runID {
				t.Errorf("run_canceled event has runID=%q, want %q", ev.RunID, runID)
			}
		}
	}
	if !found {
		t.Error("no run_canceled event recorded")
	}

	// Verify completeRunLog cleaned up the state.
	s.runLogMu.Lock()
	_, exists := s.runLogStates[runLogKey(1, 0, 100)]
	s.runLogMu.Unlock()
	if exists {
		t.Error("runLogState was not cleaned up by completeRunLog")
	}
}

// TestNewService_SharesInjectedSharedState verifies that when Config provides
// NudgeBuffer, MemoryCache, and TokenGuard, NewService reuses them instead of
// creating fresh instances. This is the wiring that lets Telegram (singleton
// pipeline) and TUI (per-send pipeline) share state across frontends.
func TestNewService_SharesInjectedSharedState(t *testing.T) {
	sharedBuffer := session.NewNudgeBuffer()
	sharedCache := NewMemoryCache()
	sharedGuard := session.NewTokenGuard()

	svc := NewService(Config{
		NudgeBuffer: sharedBuffer,
		MemoryCache: sharedCache,
		TokenGuard:  sharedGuard,
	})

	if svc.NudgeBuffer() != sharedBuffer {
		t.Error("NudgeBuffer(): injected instance not retained (sharing broken)")
	}
	if svc.MemoryCache() != sharedCache {
		t.Error("MemoryCache(): injected instance not retained (sharing broken)")
	}
	if svc.TokenGuard() != sharedGuard {
		t.Error("TokenGuard(): injected instance not retained (sharing broken)")
	}
}

// TestNewService_CreatesFreshWhenNotInjected verifies backward compat:
// when Config leaves shared-state fields nil, NewService creates fresh ones.
func TestNewService_CreatesFreshWhenNotInjected(t *testing.T) {
	svc := NewService(Config{})

	if svc.NudgeBuffer() == nil {
		t.Error("NudgeBuffer(): nil when not injected, expected fresh instance")
	}
	if svc.MemoryCache() == nil {
		t.Error("MemoryCache(): nil when not injected, expected fresh instance")
	}
	if svc.TokenGuard() == nil {
		t.Error("TokenGuard(): nil when not injected, expected fresh instance")
	}
}

// --- EntryPoint normalization ---

func TestNormalizeEntryPoint_EmptyDefaultsToTelegram(t *testing.T) {
	if got := normalizeEntryPoint(""); got != observability.EntryPointTelegram {
		t.Errorf("normalizeEntryPoint(\"\") = %q, want %q", got, observability.EntryPointTelegram)
	}
}

func TestNormalizeEntryPoint_PreservesKnownValues(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{observability.EntryPointTelegram, observability.EntryPointTelegram},
		{observability.EntryPointTUI, observability.EntryPointTUI},
		{observability.EntryPointCron, observability.EntryPointCron},
	}
	for _, tc := range tests {
		if got := normalizeEntryPoint(tc.input); got != tc.want {
			t.Errorf("normalizeEntryPoint(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeEntryPoint_UnknownFallsBackToTelegram(t *testing.T) {
	if got := normalizeEntryPoint("unknown-surface"); got != observability.EntryPointTelegram {
		t.Errorf("normalizeEntryPoint(\"unknown-surface\") = %q, want %q", got, observability.EntryPointTelegram)
	}
}

func TestNewService_EntryPointDefault(t *testing.T) {
	// When EntryPoint is empty, NewService defaults to Telegram.
	svc := NewService(Config{})
	if svc.EntryPoint() != observability.EntryPointTelegram {
		t.Errorf("EntryPoint() = %q, want %q", svc.EntryPoint(), observability.EntryPointTelegram)
	}
}

func TestNewService_EntryPointTUI(t *testing.T) {
	// When EntryPoint is tui, NewService preserves it.
	svc := NewService(Config{EntryPoint: observability.EntryPointTUI})
	if svc.EntryPoint() != observability.EntryPointTUI {
		t.Errorf("EntryPoint() = %q, want %q", svc.EntryPoint(), observability.EntryPointTUI)
	}
}

// captureRunLogStore records the RunRecord passed to the first Start call.
type captureRunLogStore struct {
	fakeRunLogStore
	started *runlog.RunRecord
}

func (c *captureRunLogStore) Start(ctx context.Context, record runlog.RunRecord) error {
	c.started = &record
	return nil
}

func TestStartRunLog_EntryPointTUI(t *testing.T) {
	// Assertion 3: TUI-configured service persists "tui" in runlog entrypoint.
	store := &captureRunLogStore{}
	s := &Service{
		runLog:       store,
		entryPoint:   observability.EntryPointTUI,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}

	ok := s.startRunLog(startRunLogParams{
		ChatID:     42,
		ThreadID:   7,
		RequestID:  "req-123",
		MessageID:  1,
		EntryPoint: s.entryPoint,
	})
	if !ok {
		t.Fatal("startRunLog returned false")
	}
	if store.started == nil {
		t.Fatal("startRunLog did not call runLog.Start")
	}
	if store.started.EntryPoint != observability.EntryPointTUI {
		t.Errorf("runlog EntryPoint = %q, want %q", store.started.EntryPoint, observability.EntryPointTUI)
	}
}

func TestStartRunLog_DefaultEntryPointTelegram(t *testing.T) {
	// Assertion 4: Empty entrypoint defaults to telegram in runlog.
	store := &captureRunLogStore{}
	s := &Service{
		runLog:       store,
		entryPoint:   observability.EntryPointTelegram,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}

	ok := s.startRunLog(startRunLogParams{
		ChatID:     42,
		ThreadID:   7,
		RequestID:  "req-456",
		MessageID:  1,
		EntryPoint: s.entryPoint,
	})
	if !ok {
		t.Fatal("startRunLog returned false")
	}
	if store.started == nil {
		t.Fatal("startRunLog did not call runLog.Start")
	}
	if store.started.EntryPoint != observability.EntryPointTelegram {
		t.Errorf("runlog EntryPoint = %q, want %q", store.started.EntryPoint, observability.EntryPointTelegram)
	}
}
