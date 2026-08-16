package pipeline

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/pkg/idgen"
)

// newRetryTestService builds a Service ready for retryAfterProcessDeath with
// a runlog state already started (runLogStarted=true semantics).
func newRetryTestService(t *testing.T, startedAt time.Time) (*Service, *spyRunLogStore, string) {
	t.Helper()
	spy := &spyRunLogStore{}
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		sessions:     session.NewStore(),
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(9, 0, 900)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: startedAt}
	s.runLogMu.Unlock()
	return s, spy, runID
}

// retrySuccessStream returns a scripted execute func whose retry stream
// produces assistant + result events (a successful retry).
func retrySuccessStream() func(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error) {
	return func(_ context.Context, _ bridge.Request) (<-chan bridge.Event, error) {
		ch := make(chan bridge.Event, 4)
		ch <- bridge.Event{Type: "assistant", Text: "recovered "}
		ch <- bridge.Event{Type: "result", Content: "recovered answer", RequestID: "retry-1"}
		close(ch)
		return ch, nil
	}
}

// TestRetryAfterProcessDeath_SuccessCompletesSingleRun covers the process
// death -> retry success matrix: the runlog is NOT completed before the
// retry; bridge_process_death, retry telemetry and retry feedback all land in
// the same run; exactly one terminal completion (RunCompleted) is persisted
// with the aggregates.
func TestRetryAfterProcessDeath_SuccessCompletesSingleRun(t *testing.T) {
	s, spy, runID := newRetryTestService(t, time.Now().Add(-10*time.Second))

	// Simulate the process death event recorded by executeAsync before the
	// retry (runlog state still alive -> lands in pendingEvents).
	s.recordPipelineEvent(9, 0, 900, observability.NewErrorEvent("",
		observability.PhaseBridgeProcessDeath, "bridge process exited during Execute"))

	s.retryAfterProcessDeath(
		context.Background(), context.Background(), func() {},
		9, 0, 42, bridge.Request{Command: "query", Prompt: "hello"},
		"hello", 900, false, &fakeProgress{}, true, nil, nil,
		newRunTimeoutTracker(), retrySuccessStream(),
	)

	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1 (retry success must complete one run)", len(comps))
	}
	if comps[0].status != runlog.RunCompleted {
		t.Fatalf("status = %s, want completed (retry success must not persist failed)", comps[0].status)
	}
	if comps[0].runID != runID {
		t.Fatalf("completion runID = %q, want %q (same run through the retry)", comps[0].runID, runID)
	}
	if comps[0].agg.FirstFeedbackMs < 0 {
		t.Fatalf("first_feedback_ms = %d, want >= 0", comps[0].agg.FirstFeedbackMs)
	}

	// The pre-retry process death event and the retry flow live in the same
	// run: no event is dropped for a missing RunID.
	evs := spy.recordedEvents()
	var deathEv, retryEv, resultEv *runlog.RunEvent
	for i := range evs {
		switch evs[i].Phase {
		case string(observability.PhaseBridgeProcessDeath):
			deathEv = &evs[i]
		case string(observability.PhaseRetryStarted):
			retryEv = &evs[i]
		case string(observability.PhaseBridgeResult):
			resultEv = &evs[i]
		}
	}
	if deathEv == nil || retryEv == nil || resultEv == nil {
		t.Fatalf("missing same-run events: death=%v retry=%v result=%v",
			deathEv != nil, retryEv != nil, resultEv != nil)
	}
	for _, ev := range []*runlog.RunEvent{deathEv, retryEv, resultEv} {
		if ev.RunID != runID {
			t.Fatalf("event %s has RunID %q, want %q (same run)", ev.Phase, ev.RunID, runID)
		}
	}
}

func TestRetryAfterProcessDeath_UsesFreshRequestID(t *testing.T) {
	s, _, _ := newRetryTestService(t, time.Now().Add(-time.Second))
	var seen bridge.Request
	execute := func(_ context.Context, req bridge.Request) (<-chan bridge.Event, error) {
		seen = req
		ch := make(chan bridge.Event, 1)
		ch <- bridge.Event{Type: "result", Content: "ok"}
		close(ch)
		return ch, nil
	}

	s.retryAfterProcessDeath(
		context.Background(), context.Background(), func() {},
		9, 0, 42, bridge.Request{Command: "query", RequestID: "original-request", Prompt: "hello"},
		"hello", 900, false, &fakeProgress{}, false, nil, nil,
		newRunTimeoutTracker(), execute,
	)

	if seen.RequestID == "" || seen.RequestID == "original-request" || !strings.HasPrefix(seen.RequestID, "retry-") {
		t.Fatalf("retry request_id = %q, want fresh retry-* ID", seen.RequestID)
	}
}

// TestRetryAfterProcessDeath_ExecuteErrorCompletesRunFailed covers the
// retry Execute-error path: exactly one runlog completion with RunFailed, and
// the retry_failed event recorded in the same run.
func TestRetryAfterProcessDeath_ExecuteErrorCompletesRunFailed(t *testing.T) {
	s, spy, runID := newRetryTestService(t, time.Now().Add(-10*time.Second))

	execute := func(_ context.Context, _ bridge.Request) (<-chan bridge.Event, error) {
		return nil, errors.New("bridge process died again")
	}
	s.retryAfterProcessDeath(
		context.Background(), context.Background(), func() {},
		9, 0, 42, bridge.Request{Command: "query", Prompt: "hello"},
		"hello", 900, false, &fakeProgress{}, true, nil, nil,
		newRunTimeoutTracker(), execute,
	)

	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1 (Execute error path)", len(comps))
	}
	if comps[0].status != runlog.RunFailed {
		t.Fatalf("status = %s, want failed", comps[0].status)
	}
	if comps[0].runID != runID {
		t.Fatalf("completion runID = %q, want %q", comps[0].runID, runID)
	}

	evs := spy.recordedEvents()
	var retryFailed *runlog.RunEvent
	for i := range evs {
		if evs[i].Phase == string(observability.PhaseRetryFailed) {
			retryFailed = &evs[i]
		}
	}
	if retryFailed == nil {
		t.Fatal("retry_failed event missing")
	}
	if retryFailed.RunID != runID {
		t.Fatalf("retry_failed RunID = %q, want %q", retryFailed.RunID, runID)
	}
}

// TestRetryAfterProcessDeath_CooldownCompletesRunFailed covers the cooldown
// path: the retry is skipped but the runlog is still completed exactly once
// (RunFailed), so no run is left dangling as running.
func TestRetryAfterProcessDeath_CooldownCompletesRunFailed(t *testing.T) {
	s, spy, runID := newRetryTestService(t, time.Now().Add(-10*time.Second))

	// Prime the failure tracker into cooldown (3 failures in the window).
	for i := 0; i < failureWindowMax; i++ {
		s.bridgeFailures.record()
	}

	executed := false
	execute := func(_ context.Context, _ bridge.Request) (<-chan bridge.Event, error) {
		executed = true
		return nil, errors.New("must not execute during cooldown")
	}
	s.retryAfterProcessDeath(
		context.Background(), context.Background(), func() {},
		9, 0, 42, bridge.Request{Command: "query", Prompt: "hello"},
		"hello", 900, false, &fakeProgress{}, true, nil, nil,
		newRunTimeoutTracker(), execute,
	)

	if executed {
		t.Fatal("retry executed despite cooldown")
	}
	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1 (cooldown path)", len(comps))
	}
	if comps[0].status != runlog.RunFailed {
		t.Fatalf("status = %s, want failed", comps[0].status)
	}
	if comps[0].runID != runID {
		t.Fatalf("completion runID = %q, want %q", comps[0].runID, runID)
	}
}

// TestRetryAfterProcessDeath_SecondProcessDeathCompletesRunFailed covers the
// retry process-death-again path: the retry stream closes without a terminal
// event and exactly one runlog is completed (RunFailed via handleRetryOutcome).
func TestRetryAfterProcessDeath_SecondProcessDeathCompletesRunFailed(t *testing.T) {
	s, spy, runID := newRetryTestService(t, time.Now().Add(-10*time.Second))

	execute := func(_ context.Context, _ bridge.Request) (<-chan bridge.Event, error) {
		ch := make(chan bridge.Event)
		close(ch) // channel closed without terminal event = process death
		return ch, nil
	}
	s.retryAfterProcessDeath(
		context.Background(), context.Background(), func() {},
		9, 0, 42, bridge.Request{Command: "query", Prompt: "hello"},
		"hello", 900, false, &fakeProgress{}, true, nil, nil,
		newRunTimeoutTracker(), execute,
	)

	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1 (second process death path)", len(comps))
	}
	if comps[0].status != runlog.RunFailed {
		t.Fatalf("status = %s, want failed", comps[0].status)
	}
	if comps[0].runID != runID {
		t.Fatalf("completion runID = %q, want %q", comps[0].runID, runID)
	}
}

func TestRetryAfterProcessDeath_SupersededOwnerTerminalizesDetachedRun(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{output: &fakeOutput{}, runLog: spy, runLogStates: make(map[string]*runLogState)}
	oldOwner, replacement := newActiveRun(), newActiveRun()
	const chatID, userID = int64(9), int64(900)
	oldID, replacementID := "old-retry", "replacement"
	oldState := &runLogState{runID: oldID, owner: oldOwner, startedAt: time.Now()}
	replacementState := &runLogState{runID: replacementID, owner: replacement, startedAt: time.Now()}
	oldOwner.runLogState = oldState
	s.runLogStates[runLogKey(chatID, 0, userID)] = oldState
	s.activeSessions.Store(sessionKey(chatID, 0, userID), oldOwner)
	activeRunSlotMu.Lock()
	markRunSuperseded(oldOwner)
	s.activeSessions.Store(sessionKey(chatID, 0, userID), replacement)
	s.runLogStates[runLogKey(chatID, 0, userID)] = replacementState
	activeRunSlotMu.Unlock()

	s.retryAfterProcessDeath(context.Background(), context.Background(), func() {}, chatID, 0, 1,
		bridge.Request{RequestID: "dead"}, "hello", userID, false, &fakeProgress{}, true, nil, nil,
		newRunTimeoutTracker(), func(context.Context, bridge.Request) (<-chan bridge.Event, error) {
			t.Fatal("superseded retry must not execute")
			return nil, nil
		}, runOwnership{runID: oldID, owner: oldOwner})

	comps := spy.recordedCompletions()
	if len(comps) != 1 || comps[0].runID != oldID || comps[0].status != runlog.RunCanceled {
		t.Fatalf("completions = %+v, want one canceled old run", comps)
	}
	if s.runLogStates[runLogKey(chatID, 0, userID)] != replacementState || replacementState.finalized {
		t.Fatal("replacement state was mutated by stale retry")
	}
}

// TestRetryAfterProcessDeath_UserCancelDuringRetryExecuteKeepsCanceled covers
// A2: a user cancel observed while execute(retryReq) is still in flight must
// complete the run as RunCanceled (never RunFailed), with the same run ID.
func TestRetryAfterProcessDeath_UserCancelDuringRetryExecuteKeepsCanceled(t *testing.T) {
	s, spy, runID := newRetryTestService(t, time.Now().Add(-10*time.Second))

	parentCtx, cancelParent := context.WithCancel(context.Background())
	cancelParent() // user canceled before/during the retry Execute

	execute := func(_ context.Context, _ bridge.Request) (<-chan bridge.Event, error) {
		return nil, context.Canceled
	}
	s.retryAfterProcessDeath(
		parentCtx, parentCtx, func() {},
		9, 0, 42, bridge.Request{Command: "query", Prompt: "hello"},
		"hello", 900, false, &fakeProgress{}, true, nil, nil,
		newRunTimeoutTracker(), execute,
	)

	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1", len(comps))
	}
	if comps[0].status != runlog.RunCanceled {
		t.Fatalf("status = %s, want canceled (user cancel must not degrade to failed)", comps[0].status)
	}
	if comps[0].runID != runID {
		t.Fatalf("completion runID = %q, want %q (same run)", comps[0].runID, runID)
	}
}

// TestRetryAfterProcessDeath_TimeoutDuringRetryExecuteKeepsTimedOut covers
// A2: a run timeout observed while execute(retryReq) is in flight must
// complete the run as RunTimedOut with the timeout origin preserved (never
// RunFailed).
func TestRetryAfterProcessDeath_TimeoutDuringRetryExecuteKeepsTimedOut(t *testing.T) {
	s, spy, runID := newRetryTestService(t, time.Now().Add(-10*time.Second))

	// ctx canceled (timeout) while parentCtx is still alive.
	ctx, cancelCtx := context.WithCancel(context.Background())
	cancelCtx()
	tracker := newRunTimeoutTracker()
	tracker.mark(timeoutOriginIdleBridge)

	execute := func(_ context.Context, _ bridge.Request) (<-chan bridge.Event, error) {
		return nil, context.Canceled
	}
	s.retryAfterProcessDeath(
		context.Background(), ctx, func() {},
		9, 0, 42, bridge.Request{Command: "query", Prompt: "hello"},
		"hello", 900, false, &fakeProgress{}, true, nil, nil,
		tracker, execute,
	)

	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1", len(comps))
	}
	if comps[0].status != runlog.RunTimedOut {
		t.Fatalf("status = %s, want timed_out (timeout must not degrade to failed)", comps[0].status)
	}
	if comps[0].runID != runID {
		t.Fatalf("completion runID = %q, want %q (same run)", comps[0].runID, runID)
	}
	// The checkpoint carries the timeout origin (preserved, not blank).
	if !strings.Contains(comps[0].checkpoint, timeoutOriginIdleBridge) {
		t.Fatalf("checkpoint = %q, want origin %q preserved", comps[0].checkpoint, timeoutOriginIdleBridge)
	}
}
