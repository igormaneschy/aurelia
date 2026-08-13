package pipeline

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// recordingProgress captures ReportState calls for assertions. Other
// ProgressReporter methods are no-ops.
type recordingProgress struct {
	mu     sync.Mutex
	states []ProgressState
}

func (r *recordingProgress) ReportTool(_, _ string)    {}
func (r *recordingProgress) ReportToolResult(_ string) {}
func (r *recordingProgress) ReportText(_ string)       {}
func (r *recordingProgress) ReportState(s ProgressState, _ string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}
func (r *recordingProgress) Delete() {}

func (r *recordingProgress) recorded() []ProgressState {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProgressState, len(r.states))
	copy(out, r.states)
	return out
}

// TestProcessBridgeEvents_ProgressStates covers the surface-neutral state
// ladder: stall warning, stall urgent, steer resume, tool_use working and
// result done — all emitted without a runlog (progress must not depend on
// telemetry persistence).
func TestProcessBridgeEvents_ProgressStates(t *testing.T) {
	s := &Service{output: &fakeOutput{}}
	progress := &recordingProgress{}

	ch := make(chan bridge.Event, 8)
	ch <- bridge.Event{Type: "stall", Severity: "warning", SilentMs: 45000}
	ch <- bridge.Event{Type: "stall", Severity: "urgent", SilentMs: 120000}
	ch <- bridge.Event{Type: "steer", Severity: "warning", SilentMs: 0}
	ch <- bridge.Event{Type: "tool_use", Name: "Read", Input: map[string]any{"path": "/tmp/x"}}
	ch <- bridge.Event{Type: "result", Content: "final answer"}
	close(ch)

	outcome := s.ProcessBridgeEvents(1, 0, 100, ch, progress, "hello", nil, 100, false, nil, nil)
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want OutcomeSuccess", outcome)
	}

	want := []ProgressState{
		ProgressStateStallWarning,
		ProgressStateStallUrgent,
		ProgressStateWorking, // steer resumes
		ProgressStateWorking, // tool_use
		ProgressStateDone,    // result
	}
	states := progress.recorded()
	if len(states) != len(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states[%d] = %s, want %s (full: %v)", i, states[i], want[i], states)
		}
	}
}

// TestProcessBridgeEvents_UnknownSeverityFallsBackToWarning ensures arbitrary
// severity text cannot produce an unexpected state — it degrades to warning.
func TestProcessBridgeEvents_UnknownSeverityFallsBackToWarning(t *testing.T) {
	s := &Service{output: &fakeOutput{}}
	progress := &recordingProgress{}

	ch := make(chan bridge.Event, 2)
	ch <- bridge.Event{Type: "stall", Severity: "arbitrary user-controlled text", SilentMs: 60000}
	ch <- bridge.Event{Type: "result", Content: "ok"}
	close(ch)

	outcome := s.ProcessBridgeEvents(1, 0, 100, ch, progress, "hello", nil, 100, false, nil, nil)
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want OutcomeSuccess", outcome)
	}
	states := progress.recorded()
	if len(states) < 1 || states[0] != ProgressStateStallWarning {
		t.Fatalf("states = %v, want first state StallWarning", states)
	}
}

// TestProcessBridgeEvents_ErrorEmitsFailedState covers the error terminal.
func TestProcessBridgeEvents_ErrorEmitsFailedState(t *testing.T) {
	s := &Service{output: &fakeOutput{}}
	progress := &recordingProgress{}

	ch := make(chan bridge.Event, 1)
	ch <- bridge.Event{Type: "error", Message: "query timeout"}
	close(ch)

	outcome := s.ProcessBridgeEvents(1, 0, 100, ch, progress, "hello", nil, 100, false, nil, nil)
	if outcome != OutcomeLLMError {
		t.Fatalf("outcome = %v, want OutcomeLLMError", outcome)
	}
	states := progress.recorded()
	if len(states) != 1 || states[0] != ProgressStateFailed {
		t.Fatalf("states = %v, want [failed]", states)
	}
}

// TestProcessBridgeEvents_StaleOwnerEmitsCanceledState covers the canceled
// terminal when ownership is lost before the run finishes.
func TestProcessBridgeEvents_StaleOwnerEmitsCanceledState(t *testing.T) {
	s := &Service{output: &fakeOutput{}}
	current := newActiveRun()
	stale := newActiveRun()
	s.activeSessions.Store(sessionKey(10, 0, 1000), current)
	progress := &recordingProgress{}

	ch := make(chan bridge.Event, 1)
	ch <- bridge.Event{Type: "result", Content: "stale result"}
	close(ch)

	outcome := s.ProcessBridgeEvents(10, 0, 99, ch, progress, "hello", nil, 1000, false, nil, nil,
		runOwnership{owner: stale})
	if outcome != OutcomeCanceled {
		t.Fatalf("outcome = %v, want OutcomeCanceled for superseded owner", outcome)
	}
	states := progress.recorded()
	if len(states) != 1 || states[0] != ProgressStateCanceled {
		t.Fatalf("states = %v, want [canceled]", states)
	}
}

// TestHeartbeatMonitor_EmitsWaitingState drives the real heartbeat state
// machine with short intervals: the waiting state fires after the threshold,
// and a tool signal resets the gap so a second waiting state can fire.
func TestHeartbeatMonitor_EmitsWaitingState(t *testing.T) {
	progress := &recordingProgress{}
	done := make(chan struct{})
	defer close(done)
	sig := make(chan struct{}, 16)
	tracker := newToolCallTracker(1, 0, nil, nil, 0, 0)

	go heartbeatMonitorWithIntervals(done, sig, tracker, progress, 10*time.Millisecond, 20*time.Millisecond)

	waitForStates := func(min int) []ProgressState {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if states := progress.recorded(); len(states) >= min {
				return states
			}
			time.Sleep(5 * time.Millisecond)
		}
		return progress.recorded()
	}

	states := waitForStates(1)
	if len(states) != 1 || states[0] != ProgressStateWaiting {
		t.Fatalf("states = %v, want exactly [waiting] after threshold", states)
	}

	// Tool signal resets the gap; a second waiting state must fire.
	sig <- struct{}{}
	states = waitForStates(2)
	if len(states) < 2 || states[1] != ProgressStateWaiting {
		t.Fatalf("states = %v, want second [waiting] after reset", states)
	}
}

// TestHeartbeatMonitor_NilProgressIsSafe ensures the monitor exits cleanly
// when no progress reporter is wired (e.g. nil adapters).
func TestHeartbeatMonitor_NilProgressIsSafe(t *testing.T) {
	done := make(chan struct{})
	sig := make(chan struct{}, 16)
	tracker := newToolCallTracker(1, 0, nil, nil, 0, 0)
	finished := make(chan struct{})
	go func() {
		heartbeatMonitorWithIntervals(done, sig, tracker, nil, time.Millisecond, time.Millisecond)
		close(finished)
	}()
	close(done)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeatMonitor did not exit on doneCh with nil progress")
	}
}

// TestBuildHeartbeatMessage_MilestonesEscalate verifies the staggered copy:
// short silences use the simple line, longer ones escalate, and the
// every-N consolidation variant still wins when tools ran.
func TestBuildHeartbeatMessage_MilestonesEscalate(t *testing.T) {
	cases := []struct {
		name    string
		elapsed time.Duration
		beat    int
		tools   int
		wantSub string
		notWant string
	}{
		{"under a minute", 30 * time.Second, 1, 0, "Ainda estou processando", "consolidar"},
		{"one minute milestone", 2 * time.Minute, 1, 0, "Ainda estou trabalhando no pedido", "consolidar"},
		{"three minute milestone", 5 * time.Minute, 1, 0, "obrigado pela paciência", "consolidar"},
		{"every-N consolidation wins", 30 * time.Second, heartbeatToolThreshold, 5, "consolidar", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := buildHeartbeatMessage(tc.elapsed, tc.beat, tc.tools)
			if !strings.Contains(msg, tc.wantSub) {
				t.Errorf("message = %q, want substring %q", msg, tc.wantSub)
			}
			if tc.notWant != "" && strings.Contains(msg, tc.notWant) {
				t.Errorf("message = %q, must not contain %q", msg, tc.notWant)
			}
			if strings.Contains(msg, "chamadas de ferramenta") || strings.Contains(msg, "calls") {
				t.Errorf("technical terms leaked into message: %q", msg)
			}
		})
	}
}

// TestProgressSilenceDetail_FormatsBoundedDuration checks the stall detail
// formatting is human-readable and bounded.
func TestProgressSilenceDetail_FormatsBoundedDuration(t *testing.T) {
	if got := progressSilenceDetail(45_000); got != "silêncio de 45s" {
		t.Fatalf("detail = %q, want %q", got, "silêncio de 45s")
	}
	if got := progressSilenceDetail(0); got != "silêncio de 0s" {
		t.Fatalf("detail = %q, want %q", got, "silêncio de 0s")
	}
}
