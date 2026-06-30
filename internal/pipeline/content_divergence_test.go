package pipeline

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
)

func TestDetectContentDivergence(t *testing.T) {
	long := strings.Repeat("a", 600)
	short := strings.Repeat("b", 50)

	tests := []struct {
		name        string
		streamed    string
		result      string
		wantOK      bool
		wantSignif  bool
		wantDiff    int
		wantStream  int
		wantResult  int
	}{
		{name: "empty streamed", streamed: "", result: "x", wantOK: false},
		{name: "empty result", streamed: "x", result: "", wantOK: false},
		{name: "identical", streamed: "same", result: "same", wantOK: false},
		{name: "small gap", streamed: "short stream text", result: "short result", wantOK: true, wantSignif: false, wantDiff: 5, wantStream: 17, wantResult: 12},
		{name: "significant gap", streamed: long, result: short, wantOK: true, wantSignif: true, wantDiff: 550, wantStream: 600, wantResult: 50},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			div, ok := detectContentDivergence(tc.streamed, tc.result)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if div.significant() != tc.wantSignif {
				t.Fatalf("significant = %v, want %v (diff=%d)", div.significant(), tc.wantSignif, div.Diff)
			}
			if tc.wantDiff != 0 && div.Diff != tc.wantDiff {
				t.Fatalf("diff = %d, want %d", div.Diff, tc.wantDiff)
			}
			if tc.wantStream != 0 && div.StreamLen != tc.wantStream {
				t.Fatalf("stream_len = %d, want %d", div.StreamLen, tc.wantStream)
			}
			if tc.wantResult != 0 && div.ResultLen != tc.wantResult {
				t.Fatalf("result_len = %d, want %d", div.ResultLen, tc.wantResult)
			}
		})
	}
}

func TestContentDivergenceMetadataJSON(t *testing.T) {
	div, ok := detectContentDivergence(strings.Repeat("x", 700), "y")
	if !ok || !div.significant() {
		t.Fatal("expected significant divergence")
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(div.metadataJSON()), &meta); err != nil {
		t.Fatalf("metadata JSON: %v", err)
	}
	if meta["stream_len"] != float64(700) {
		t.Fatalf("stream_len = %v, want 700", meta["stream_len"])
	}
	if meta["result_len"] != float64(1) {
		t.Fatalf("result_len = %v, want 1", meta["result_len"])
	}
	if meta["authoritative"] != "result" {
		t.Fatalf("authoritative = %v, want result", meta["authoritative"])
	}
}

func TestHandleResultEvent_SignificantDivergence_RecordsWarn(t *testing.T) {
	spy := &spyRunLogStore{}
	runID := "run-divergence-test"
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: map[string]*runLogState{runLogKey(1, 0, 100): {runID: runID}},
		runLogMu:     sync.Mutex{},
	}

	streamed := strings.Repeat("s", 700)
	result := strings.Repeat("r", 100)
	ev := bridge.Event{Type: "result", Content: result}

	var assistantText strings.Builder
	assistantText.WriteString(streamed)

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "hello", 100, false)
	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}
	if got := assistantText.String(); got != result {
		t.Fatalf("assistant text = %q, want authoritative result %q", got, result)
	}

	evs := spy.recordedEvents()
	var diverge *runlog.RunEvent
	for i := range evs {
		if evs[i].Phase == string(observability.PhaseBridgeContentDiverges) {
			diverge = &evs[i]
			break
		}
	}
	if diverge == nil {
		t.Fatalf("expected bridge_content_diverges event, got phases: %v", eventPhases(evs))
	}
	if diverge.Level != observability.EventLevelWarn {
		t.Fatalf("level = %q, want warn", diverge.Level)
	}
	if diverge.MetadataJSON == "" {
		t.Fatal("expected metadata_json")
	}
}

func TestHandleResultEvent_SmallDivergence_NoWarnEvent(t *testing.T) {
	spy := &spyRunLogStore{}
	runID := "run-small-divergence"
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: map[string]*runLogState{runLogKey(2, 0, 100): {runID: runID}},
		runLogMu:     sync.Mutex{},
	}

	ev := bridge.Event{Type: "result", Content: "result-final"}
	var assistantText strings.Builder
	assistantText.WriteString("stream-partial")

	outcome := s.handleResultEvent(2, 0, 100, ev, &assistantText, "hello", 100, false)
	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}
	for _, ev := range spy.recordedEvents() {
		if ev.Phase == string(observability.PhaseBridgeContentDiverges) {
			t.Fatalf("unexpected bridge_content_diverges for small gap: %+v", ev)
		}
	}
}

func eventPhases(evs []runlog.RunEvent) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = ev.Phase
	}
	return out
}