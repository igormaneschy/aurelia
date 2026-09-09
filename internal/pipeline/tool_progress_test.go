package pipeline

import (
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// TestProcessBridgeEvents_ToolRunningIsProgressNotStall covers the core fix:
// a tool in flight is progress, never a model stall. tool_slow keeps the
// honest long-command wording instead of the "model struggling" copy.
func TestProcessBridgeEvents_ToolRunningIsProgressNotStall(t *testing.T) {
	s := &Service{output: &fakeOutput{}}
	progress := &recordingProgress{}

	ch := make(chan bridge.Event, 3)
	ch <- bridge.Event{Type: "tool_running", Name: "Bash", ElapsedMs: 195_000, ToolCallID: "t1"}
	ch <- bridge.Event{Type: "tool_slow", Name: "Bash", ElapsedMs: 700_000, ToolCallID: "t1"}
	ch <- bridge.Event{Type: "result", Content: "done"}
	close(ch)

	if outcome := s.ProcessBridgeEvents(1, 0, 100, ch, progress, "hello", nil, 100, false, nil, nil); outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want OutcomeSuccess", outcome)
	}

	want := []ProgressState{
		ProgressStateToolRunning,
		ProgressStateToolSlow,
		ProgressStateDone,
	}
	states := progress.recorded()
	if len(states) != len(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states[%d] = %s, want %s", i, states[i], want[i])
		}
		if states[i] == ProgressStateStallWarning || states[i] == ProgressStateStallUrgent {
			t.Fatalf("tool progress produced a model-stall state: %v", states)
		}
	}

	details := progress.recordedDetails()
	if !strings.Contains(details[0], "Bash") || !strings.Contains(details[0], "3m15s") {
		t.Fatalf("tool_running detail = %q, want label + elapsed", details[0])
	}
	if !strings.Contains(details[1], "comando longo") {
		t.Fatalf("tool_slow detail = %q, want honest long-command wording", details[1])
	}
	if strings.Contains(details[0], "dificuldade") || strings.Contains(details[1], "dificuldade") {
		t.Fatalf("tool progress reused the model-stall copy: %v", details)
	}
}

// TestToolProgressDetail_BoundsAndLabels fixes the detail contract: bounded
// safe label, formatted elapsed, and a fallback label when the bridge label is
// missing or unknown.
func TestToolProgressDetail_BoundsAndLabels(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		elapsed  int64
		slow     bool
		contains []string
	}{
		{"bash", "bash", 195_000, false, []string{"Bash", "3m15s"}},
		{"read", "read", 5_000, false, []string{"Read", "5s"}},
		{"slow", "bash", 700_000, true, []string{"Bash", "comando longo"}},
		{"unknown label", "", 1_000, false, []string{"ferramenta", "1s"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toolProgressDetail(tc.tool, tc.elapsed, tc.slow)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Fatalf("toolProgressDetail(%q, %d, %v) = %q, want %q", tc.tool, tc.elapsed, tc.slow, got, want)
				}
			}
		})
	}
}

// TestStallPriorityReporter_ToolRunningHoldsWaiting proves a tool-in-flight
// state clears a stale stall line and is not clobbered by the heartbeat
// Waiting re-beat within the hold window.
func TestStallPriorityReporter_ToolRunningHoldsWaiting(t *testing.T) {
	inner := &recordingProgress{}
	r := &stallPriorityReporter{inner: inner}

	r.ReportState(ProgressStateStallWarning, "silêncio de 61s")
	r.ReportState(ProgressStateToolRunning, "Bash em execução há 1m1s")
	r.ReportState(ProgressStateWaiting, "") // heartbeat re-beat: suppressed

	states := inner.recorded()
	want := []ProgressState{ProgressStateStallWarning, ProgressStateToolRunning}
	if len(states) != len(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states[%d] = %s, want %s", i, states[i], want[i])
		}
	}
}
