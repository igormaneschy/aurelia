package pipeline

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/pkg/idgen"
)

// TestApplyLifecycle_CompactRecordsCompactionEventsInRun covers correction 3
// / A1: when the runlog is started BEFORE applyLifecycle (as processRunWithCancel
// now does), proactive compaction records bounded compaction_start/end events
// into the SAME run_id as the prompt that follows.
func TestApplyLifecycle_CompactRecordsCompactionEventsInRun(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		config:       &config.AppConfig{SessionLifecycle: config.DefaultSessionLifecycleConfig()},
		sessions:     session.NewStore(),
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
		tokenGuard:   session.NewTokenGuard(),
		testSessionStats: func(_ context.Context, _ bridge.RequestOptions) (*bridge.SessionStats, error) {
			return &bridge.SessionStats{InputTokens: 350_000}, nil
		},
		testCompactSession: func(_ context.Context, _ int64, _ int, _ int64, _ bridge.RequestOptions) (*bridge.CompactSessionResult, error) {
			return &bridge.CompactSessionResult{Success: true, SessionFile: "/tmp/test.jsonl", TokensBefore: 350_000}, nil
		},
	}

	// The runlog state was started by processRunWithCancel before lifecycle.
	runID := idgen.New()
	key := runLogKey(1, 2, 100)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")
	req := &bridge.Request{Command: "query", Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"}}

	// Stall the token guard 3 turns so the third one escalates to compact.
	for i := 0; i < 2; i++ {
		result := s.applyLifecycle(context.Background(), req, 1, 2, 100)
		if result.Decision.Action != session.ActionContinue {
			t.Fatalf("turn %d: expected continue, got %s", i+1, result.Decision.Action)
		}
	}
	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)
	if result.Decision.Action != session.ActionCompact {
		t.Fatalf("expected compact on 3rd turn, got %s (%s)", result.Decision.Action, result.Decision.Reason)
	}

	// Production flushes pending events at the terminal boundary; complete
	// the run the way the pipeline does and then assert on the timeline.
	s.completeRunLog(1, 2, 100, runlog.RunCompleted, "", "")

	// The compaction events were recorded into the pre-started run.
	evs := spy.recordedEvents()
	var compStart, compEnd *runlog.RunEvent
	for i := range evs {
		ev := evs[i]
		if ev.Phase != string(observability.PhaseBridgeCompaction) {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ev.MetadataJSON), &m); err != nil {
			t.Fatalf("compaction metadata not JSON: %v", err)
		}
		switch m["event"] {
		case "compaction_start":
			compStart = &ev
		case "compaction_end":
			compEnd = &ev
		}
	}
	if compStart == nil || compEnd == nil {
		t.Fatalf("missing compaction events: start=%v end=%v", compStart != nil, compEnd != nil)
	}
	// Same run as the prompt that follows (A1: same run_id/request_id).
	if compStart.RunID != runID || compEnd.RunID != runID {
		t.Fatalf("compaction RunIDs = %q/%q, want %q (same run)", compStart.RunID, compEnd.RunID, runID)
	}

	var endMeta map[string]any
	if err := json.Unmarshal([]byte(compEnd.MetadataJSON), &endMeta); err != nil {
		t.Fatalf("compaction_end metadata not JSON: %v", err)
	}
	if endMeta["success"] != true || endMeta["tokens_before"] != float64(350_000) {
		t.Fatalf("compaction_end metadata = %v, want success + tokens_before", endMeta)
	}
}
