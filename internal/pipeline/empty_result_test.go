package pipeline

import (
	"context"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/session"
	"strings"
	"sync"
	"testing"
)

func TestEmptyResultHadWork_NoWork_ReturnsFalse(t *testing.T) {
	ev := bridge.Event{Type: "result"}
	if emptyResultHadWork(ev) {
		t.Fatal("expected false for zero-value event")
	}
}

func TestEmptyResultHadWork_NumTurns_ReturnsTrue(t *testing.T) {
	ev := bridge.Event{Type: "result", NumTurns: 1}
	if !emptyResultHadWork(ev) {
		t.Fatal("expected true when NumTurns > 0")
	}
}

func TestEmptyResultHadWork_InputTokens_ReturnsTrue(t *testing.T) {
	ev := bridge.Event{Type: "result", InputTokens: 100}
	if !emptyResultHadWork(ev) {
		t.Fatal("expected true when InputTokens > 0")
	}
}

func TestEmptyResultHadWork_OutputTokens_ReturnsTrue(t *testing.T) {
	ev := bridge.Event{Type: "result", OutputTokens: 50}
	if !emptyResultHadWork(ev) {
		t.Fatal("expected true when OutputTokens > 0")
	}
}

func TestEmptyResultHadWork_CostUSD_ReturnsTrue(t *testing.T) {
	ev := bridge.Event{Type: "result", CostUSD: 0.01}
	if !emptyResultHadWork(ev) {
		t.Fatal("expected true when CostUSD > 0")
	}
}

func TestBuildEmptyResultRecoveryMessage_WithToolSummary(t *testing.T) {
	msg := buildEmptyResultRecoveryMessage("Read, Write, Bash")
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	if !strings.Contains(msg, "Ferramentas") {
		t.Fatal("expected tool summary in message")
	}
	if !strings.Contains(msg, "/status") {
		t.Fatal("expected /status suggestion in message")
	}
	if !strings.Contains(msg, "checkpoint") {
		t.Fatal("expected checkpoint mention in message")
	}
}

func TestBuildEmptyResultRecoveryMessage_WithoutToolSummary(t *testing.T) {
	msg := buildEmptyResultRecoveryMessage("")
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	if strings.Contains(msg, "Ferramentas") {
		t.Fatal("expected no tool summary in message when empty")
	}
	if !strings.Contains(msg, "/status") {
		t.Fatal("expected /status suggestion in message")
	}
}

func TestBuildEmptyResultRecoveryMessage_TruncatesLongSummary(t *testing.T) {
	long := strings.Repeat("x", 3000)
	msg := buildEmptyResultRecoveryMessage(long)
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestHandleResultEvent_EmptyAfterWork_DeactivatesSession(t *testing.T) {
	fo := &fakeOutput{}
	ss := session.NewStore()
	ss.SetSession(1, 0, 100, "sid-123")
	s := &Service{
		output:   fo,
		sessions: ss,
	}

	ev := bridge.Event{
		Type:         "result",
		Content:      "",
		NumTurns:     5,
		InputTokens:  1000,
		OutputTokens: 200,
		CostUSD:      0.05,
	}
	var assistantText strings.Builder

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "hello", 100, false)

	if outcome != OutcomeLLMError {
		t.Fatalf("expected OutcomeLLMError, got %v", outcome)
	}

	// Session should be deactivated (not cleared)
	_, active := ss.GetSessionWithState(1, 0, 100)
	if active {
		t.Fatal("expected session to be deactivated after empty result with work")
	}

	// Should have sent a recovery message, not the generic one
	if fo.lastError == bridgeEmptyResultMessage {
		t.Fatal("expected recovery message, got generic empty result message")
	}
	if !strings.Contains(fo.lastError, "trabalhou") {
		t.Fatal("expected recovery message mentioning work, got:", fo.lastError)
	}

	if !fo.confirmCalled {
		t.Fatal("expected ConfirmMessage to be called")
	}
}

func TestHandleResultEvent_EmptyNoWork_UsesGenericMessage(t *testing.T) {
	fo := &fakeOutput{}
	ss := session.NewStore()
	ss.SetSession(1, 0, 100, "sid-123")
	s := &Service{
		output:   fo,
		sessions: ss,
	}

	ev := bridge.Event{Type: "result", Content: ""}
	var assistantText strings.Builder

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "hello", 100, false)

	if outcome != OutcomeLLMError {
		t.Fatalf("expected OutcomeLLMError, got %v", outcome)
	}

	// Session should still be active (no deactivation)
	_, active := ss.GetSessionWithState(1, 0, 100)
	if !active {
		t.Fatal("expected session to remain active when no work was done")
	}

	if fo.lastError != bridgeEmptyResultMessage {
		t.Fatalf("expected generic error %q, got %q", bridgeEmptyResultMessage, fo.lastError)
	}
}

func TestHandleResultEvent_EmptyAfterWorkWithToolSummary_IncludesToolSummary(t *testing.T) {
	fo := &fakeOutput{}
	ss := session.NewStore()
	ss.SetSession(1, 0, 100, "sid-123")

	// Create a service with a fake runlog store and pre-populated tool summary
	runLogStore := &fakeRunLogStore{}
	s := &Service{
		output:       fo,
		sessions:     ss,
		runLog:       runLogStore,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}

	// Pre-populate the in-memory tool summary
	key := runLogKey(1, 0, 100)
	state := &runLogState{runID: "test-run-id"}
	state.summary.WriteString("Read, Write")
	state.summaryCount = 2
	s.runLogStates[key] = state

	ev := bridge.Event{
		Type:         "result",
		Content:      "",
		NumTurns:     3,
		InputTokens:  500,
		OutputTokens: 100,
		CostUSD:      0.02,
	}
	var assistantText strings.Builder

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "hello", 100, false)

	if outcome != OutcomeLLMError {
		t.Fatalf("expected OutcomeLLMError, got %v", outcome)
	}

	// Verify recovery message includes the tool summary
	if !strings.Contains(fo.lastError, "Read") {
		t.Fatal("expected tool summary 'Read' in recovery message, got:", fo.lastError)
	}
	if !strings.Contains(fo.lastError, "Write") {
		t.Fatal("expected tool summary 'Write' in recovery message, got:", fo.lastError)
	}
}

// fakeRunLogStore is a no-op runlog.Store for tests that need a non-nil runLog.
type fakeRunLogStore struct{}

func (f *fakeRunLogStore) Start(_ context.Context, _ runlog.RunRecord) error  { return nil }
func (f *fakeRunLogStore) Update(_ context.Context, _ runlog.RunUpdate) error { return nil }
func (f *fakeRunLogStore) Complete(_ context.Context, _ string, _ runlog.RunStatus, _, _, _ string) error {
	return nil
}
func (f *fakeRunLogStore) RecordEvents(_ context.Context, _ []runlog.RunEvent) error { return nil }
func (f *fakeRunLogStore) Prune(_ context.Context, _ runlog.PruneOptions) (runlog.PruneResult, error) {
	return runlog.PruneResult{}, nil
}
func (f *fakeRunLogStore) Latest(_ context.Context, _ int64, _ int) (*runlog.RunRecord, error) {
	return nil, nil
}
func (f *fakeRunLogStore) RecordEvent(_ context.Context, _ runlog.RunEvent) error { return nil }
func (f *fakeRunLogStore) ListEvents(_ context.Context, _ string) ([]runlog.RunEvent, error) {
	return nil, nil
}
func (f *fakeRunLogStore) GetRun(_ context.Context, _ string) (*runlog.RunRecord, error) {
	return nil, nil
}
func (f *fakeRunLogStore) ListRuns(_ context.Context, _ int64, _ int) ([]runlog.RunRecord, error) {
	return nil, nil
}
func (f *fakeRunLogStore) Metrics(_ context.Context, _ runlog.MetricsFilter) (*runlog.MetricsResult, error) {
	return nil, nil
}
func (f *fakeRunLogStore) GetLastOutboundMessage(_ context.Context, _ string) (int64, int, int64, error) {
	return 0, 0, 0, nil
}
func (f *fakeRunLogStore) MarkStaleRunsInterrupted(_ context.Context) (int64, error) {
	return 0, nil
}
func (f *fakeRunLogStore) Close() error { return nil }
