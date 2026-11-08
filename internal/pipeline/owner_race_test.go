package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/runlog"
)

func TestCancelStartedRunLog_OwnerLostAfterStart(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{runLog: spy, runLogStates: make(map[string]*runLogState)}
	oldRun, newRun := newActiveRun(), newActiveRun()
	const (
		chatID = int64(903)
		userID = int64(904)
	)
	key := runLogKey(chatID, 0, userID)
	oldState := &runLogState{
		runID: "old-canceled-run", owner: oldRun,
		pendingEvents: []runlog.RunEvent{{RunID: "old-canceled-run", Phase: "bridge_request_started"}},
		startedAt:     time.Now(),
	}
	newState := &runLogState{runID: "replacement-run", owner: newRun}
	oldRun.runLogState = oldState
	s.runLogStates[key] = oldState
	s.activeSessions.Store(sessionKey(chatID, 0, userID), oldRun)

	// Replacement removes the old session-key state, but the old owner retains
	// its detached state capability for terminal persistence.
	activeRunSlotMu.Lock()
	markRunSuperseded(oldRun)
	s.runLogStates[key] = newState
	s.activeSessions.Store(sessionKey(chatID, 0, userID), newRun)
	activeRunSlotMu.Unlock()

	s.cancelStartedRunLog(pipelineInput{chatID: chatID, threadID: 0, userID: userID},
		runOwnership{runID: oldState.runID, owner: oldRun})

	completions := spy.recordedCompletions()
	if len(completions) != 1 || completions[0].runID != oldState.runID || completions[0].status != runlog.RunCanceled {
		t.Fatalf("old completions = %+v, want one canceled completion for %q", completions, oldState.runID)
	}
	events := spy.recordedEvents()
	if len(events) != 2 {
		t.Fatalf("old events = %+v, want pending plus terminal event", events)
	}
	for _, event := range events {
		if event.RunID != oldState.runID {
			t.Fatalf("event RunID = %q, want old run %q", event.RunID, oldState.runID)
		}
	}
	if s.runLogStates[key] != newState {
		t.Fatal("replacement runlog state was deleted or overwritten")
	}
	if newState.finalized || newState.finalizing {
		t.Fatal("replacement runlog state was finalized by stale owner")
	}

	// A competing terminal callback cannot produce a second completion.
	s.completeRunLog(chatID, 0, userID, runlog.RunFailed, "", "late", runOwnership{runID: oldState.runID, owner: oldRun})
	if got := len(spy.recordedCompletions()); got != 1 {
		t.Fatalf("completions after duplicate callback = %d, want 1", got)
	}
}

func TestPostStartOwnershipGate_CurrentOwnerContinues(t *testing.T) {
	s := &Service{}
	owner := newActiveRun()
	const chatID, threadID, userID = int64(907), 0, int64(908)
	s.activeSessions.Store(sessionKey(chatID, threadID, userID), owner)

	continued := false
	if !s.withRunOwnership(chatID, threadID, userID, []runOwnership{{owner: owner}}, func() {
		continued = true
	}) {
		t.Fatal("current owner failed the post-start ownership boundary")
	}
	if !continued {
		t.Fatal("current owner was not allowed to continue past the post-start gate")
	}
}

func TestHandleContextOutcome_StaleOwnerDoesNotPatchOrNotify(t *testing.T) {
	spy := &spyRunLogStore{}
	output := &fakeOutput{}
	s := &Service{output: output, runLog: spy, runLogStates: make(map[string]*runLogState)}
	oldRun, replacement := newActiveRun(), newActiveRun()
	const chatID, userID = int64(905), int64(906)
	oldID := "old-stale-context"
	oldState := &runLogState{runID: oldID, owner: oldRun, pendingEvents: []runlog.RunEvent{{RunID: oldID, Phase: "bridge_request_started"}}}
	replacementState := &runLogState{runID: "replacement-context", owner: replacement}
	oldRun.runLogState = oldState
	key := runLogKey(chatID, 0, userID)
	s.runLogStates[key] = oldState
	s.activeSessions.Store(sessionKey(chatID, 0, userID), oldRun)
	activeRunSlotMu.Lock()
	markRunSuperseded(oldRun)
	s.activeSessions.Store(sessionKey(chatID, 0, userID), replacement)
	s.runLogStates[key] = replacementState
	activeRunSlotMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !s.handleContextOutcome(ctx, context.Background(), chatID, 0, userID, false, nil,
		runOwnership{runID: oldID, owner: oldRun}) {
		t.Fatal("stale cancellation was not handled")
	}
	if got := len(spy.recordedCompletions()); got != 1 {
		t.Fatalf("completions = %d, want 1 detached completion", got)
	}
	completion := spy.recordedCompletions()[0]
	if completion.runID != oldID || completion.status != runlog.RunCanceled {
		t.Fatalf("completion = %+v, want canceled old run", completion)
	}
	if output.lastError != "" || output.lastReply != "" {
		t.Fatalf("stale user-facing output: error=%q reply=%q", output.lastError, output.lastReply)
	}
	if s.runLogStates[key] != replacementState || replacementState.finalized || replacementState.finalizing {
		t.Fatal("replacement state was mutated by stale context outcome")
	}
}
