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

// TestProcessBridgeEvents_ProviderWaitIsProgressNotStall covers the prefill
// case: while the provider owes the first chunk of the turn, the silence is
// prefill (evidence: 52s for a 28k context, 185s for 92k, ~4min on a
// 366k-token session with a local model) and must never read as a model stall.
func TestProcessBridgeEvents_ProviderWaitIsProgressNotStall(t *testing.T) {
	s := &Service{output: &fakeOutput{}}
	progress := &recordingProgress{}

	ch := make(chan bridge.Event, 3)
	ch <- bridge.Event{Type: "provider_wait", ElapsedMs: 185_000}
	ch <- bridge.Event{Type: "provider_wait", ElapsedMs: 240_000}
	ch <- bridge.Event{Type: "result", Content: "done"}
	close(ch)

	if outcome := s.ProcessBridgeEvents(1, 0, 100, ch, progress, "hello", nil, 100, false, nil, nil); outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want OutcomeSuccess", outcome)
	}

	want := []ProgressState{
		ProgressStateProviderWait,
		ProgressStateProviderWait,
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
			t.Fatalf("provider wait produced a model-stall state: %v", states)
		}
	}

	details := progress.recordedDetails()
	if !strings.Contains(details[0], "3m5s") {
		t.Fatalf("provider_wait detail = %q, want the measured elapsed time", details[0])
	}
	for _, detail := range details[:2] {
		if strings.Contains(detail, "dificuldade") || strings.Contains(detail, "demorando") {
			t.Fatalf("provider wait reused the model-stall copy: %q", detail)
		}
	}
}

// TestProviderWaitDetail_FormatsElapsed pins the detail contract, including
// the sub-second clamp so a clock artifact never renders "0s".
func TestProviderWaitDetail_FormatsElapsed(t *testing.T) {
	if got := providerWaitDetail(185_000); got != "Aguardando o modelo há 3m5s" {
		t.Fatalf("providerWaitDetail(185000) = %q", got)
	}
	if got := providerWaitDetail(0); got != "Aguardando o modelo há 1s" {
		t.Fatalf("providerWaitDetail(0) = %q, want the 1s clamp", got)
	}
}

// TestStallPriorityReporter_ProviderWaitHoldsWaiting proves the provider-wait
// line survives the heartbeat Waiting re-beat: a long prefill must keep showing
// "aguardando o modelo" instead of collapsing back to generic waiting copy.
func TestStallPriorityReporter_ProviderWaitHoldsWaiting(t *testing.T) {
	inner := &recordingProgress{}
	r := &stallPriorityReporter{inner: inner}

	r.ReportState(ProgressStateProviderWait, "Aguardando o modelo há 2m10s")
	r.ReportState(ProgressStateWaiting, "") // heartbeat re-beat: suppressed

	states := inner.recorded()
	want := []ProgressState{ProgressStateProviderWait}
	if len(states) != len(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	if states[0] != want[0] {
		t.Fatalf("states[0] = %s, want %s", states[0], want[0])
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
