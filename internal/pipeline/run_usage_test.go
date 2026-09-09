package pipeline

import (
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/pkg/idgen"
)

// TestCompleteRunLog_PersistsUsageAndToolCount covers A5/A7 at the pipeline
// boundary: usage captured from the bridge result and the tool count from the
// run's tool stream are persisted in the single terminal completion.
func TestCompleteRunLog_PersistsUsageAndToolCount(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
	}
	runID := idgen.New()
	key := runLogKey(1, 0, 100)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	s.recordRunUsage(1, 0, 100, bridge.Event{InputTokens: 399368, OutputTokens: 28731, CostUSD: 0.0561})
	s.recordToolUse(1, 0, 100, "bash")
	s.recordToolUse(1, 0, 100, "read")
	s.completeRunLog(1, 0, 100, runlog.RunCompleted, "done", "")

	completions := spy.recordedCompletions()
	if len(completions) != 1 {
		t.Fatalf("completions = %d, want exactly 1", len(completions))
	}
	agg := completions[0].agg
	if agg.InputTokens != 399368 || agg.OutputTokens != 28731 || agg.CostUSD != 0.0561 || agg.ToolCount != 2 {
		t.Fatalf("usage aggregates = %+v, want tokens 399368/28731, cost 0.0561, 2 tools", agg)
	}
}

// TestRecordRunUsage_IgnoresZeroAndMissingState proves the capture is
// best-effort: a zero-valued result never overwrites a previous capture, and a
// missing run state is a safe no-op.
func TestRecordRunUsage_IgnoresZeroAndMissingState(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
	}

	// Missing state: must not panic or create state.
	s.recordRunUsage(1, 0, 100, bridge.Event{InputTokens: 10})
	if _, ok := s.runLogStates[runLogKey(1, 0, 100)]; ok {
		t.Fatal("recordRunUsage created run state for an unknown run")
	}

	key := runLogKey(1, 0, 100)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: idgen.New(), startedAt: time.Now()}
	s.runLogMu.Unlock()

	s.recordRunUsage(1, 0, 100, bridge.Event{InputTokens: 100, OutputTokens: 20, CostUSD: 0.01})
	s.recordRunUsage(1, 0, 100, bridge.Event{}) // zeros must not clobber

	state, _ := s.runLogStateFor(1, 0, 100, runOwnership{})
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.inputTokens != 100 || state.outputTokens != 20 || state.costUSD != 0.01 {
		t.Fatalf("usage = %d/%d/%f, want 100/20/0.01", state.inputTokens, state.outputTokens, state.costUSD)
	}
}
