package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/pkg/idgen"
)

// TestProcessBridgeEvents_TelemetryNotFeedback drives the real event loop:
// stall/steer must be persisted as redacted telemetry (with bounded severity,
// silent_ms, source and the raw ISO timestamp as correlation metadata), tool
// duration must survive in tool_result metadata, and none of the telemetry
// may be classified as productive feedback (asserted via MaxSilenceMs staying
// tiny despite a stall carrying a fixed 2026 ISO timestamp that would blow the
// trailing-gap computation if it were counted as feedback).
func TestProcessBridgeEvents_TelemetryNotFeedback(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(1, 0, 100)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	ch := make(chan bridge.Event, 8)
	ch <- bridge.Event{Type: "tool_use", Name: "Read", Input: map[string]any{"path": "/tmp/x"}}
	ch <- bridge.Event{Type: "tool_result", Content: "redacted summary", ToolCallID: "tc-123", DurationMeasured: true, DurationMs: 2500}
	ch <- bridge.Event{Type: "stall", RequestID: "req-1",
		Timestamp: "2026-08-11T10:00:01.000Z", Severity: "warning",
		SilentMs: 60000, Source: "bridge_health"}
	ch <- bridge.Event{Type: "steer", RequestID: "req-1",
		Timestamp: "2026-08-11T10:00:01.100Z", Severity: "urgent",
		SilentMs: 120000, Source: "bridge_health"}
	ch <- bridge.Event{Type: "assistant", Text: "working on it"}
	ch <- bridge.Event{Type: "result", Content: "final answer", RequestID: "req-1"}
	close(ch)

	outcome := s.ProcessBridgeEvents(1, 0, 100, ch, &fakeProgress{}, "hello", nil, 100, false, nil, nil)
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want OutcomeSuccess", outcome)
	}

	// ── Telemetry events persisted with redacted metadata ──
	events := spy.recordedEvents()
	var stallEv, steerEv, toolResultEv *runlog.RunEvent
	for i := range events {
		ev := events[i]
		switch ev.Phase {
		case string(observability.PhaseBridgeStall):
			stallEv = &ev
		case string(observability.PhaseBridgeSteer):
			steerEv = &ev
		case string(observability.PhaseBridgeToolResult):
			toolResultEv = &ev
		}
	}
	if stallEv == nil || steerEv == nil {
		t.Fatalf("missing stall/steer events: stall=%v steer=%v", stallEv != nil, steerEv != nil)
	}
	if strings.Contains(stallEv.Message, "Continue please") || strings.Contains(steerEv.Message, "Continue please") {
		t.Fatalf("steer text leaked into telemetry: %q / %q", stallEv.Message, steerEv.Message)
	}
	var stallMeta map[string]any
	if err := json.Unmarshal([]byte(stallEv.MetadataJSON), &stallMeta); err != nil {
		t.Fatalf("stall metadata not JSON: %v", err)
	}
	if stallMeta["severity"] != "warning" || stallMeta["silent_ms"] != float64(60000) ||
		stallMeta["source"] != "bridge_health" || stallMeta["ts_iso"] != "2026-08-11T10:00:01.000Z" {
		t.Fatalf("stall metadata = %v, want severity/silent_ms/source/ts_iso", stallMeta)
	}
	var steerMeta map[string]any
	if err := json.Unmarshal([]byte(steerEv.MetadataJSON), &steerMeta); err != nil {
		t.Fatalf("steer metadata not JSON: %v", err)
	}
	if steerMeta["severity"] != "urgent" {
		t.Fatalf("steer metadata = %v, want severity=urgent", steerMeta)
	}
	var toolMeta map[string]any
	if toolResultEv == nil {
		t.Fatal("missing tool_result telemetry event")
	}
	if err := json.Unmarshal([]byte(toolResultEv.MetadataJSON), &toolMeta); err != nil {
		t.Fatalf("tool_result metadata not JSON: %v", err)
	}
	if toolMeta["tool_call_id"] != "tc-123" || toolMeta["duration_measured"] != true ||
		toolMeta["duration_ms"] != float64(2500) {
		t.Fatalf("tool_result metadata = %v, want tool_call_id/duration_measured/duration_ms", toolMeta)
	}

	// ── Aggregates: telemetry counted, feedback not polluted ──
	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1", len(comps))
	}
	agg := comps[0].agg
	if agg.StallCount != 1 || agg.SteerCount != 1 {
		t.Fatalf("stall/steer counts = %d/%d, want 1/1", agg.StallCount, agg.SteerCount)
	}
	if agg.FirstFeedbackMs < 0 {
		t.Fatalf("first_feedback_ms = %d, want >= 0 (tool_use was the first feedback)", agg.FirstFeedbackMs)
	}
	// If stall (fixed 2026 ISO timestamp) were feedback, the trailing gap to
	// completion would be enormous; it must stay a tiny real-time value.
	if agg.MaxSilenceMs >= 60_000 {
		t.Fatalf("max_silence_ms = %d, want < 60000 (telemetry must not count as feedback)", agg.MaxSilenceMs)
	}
}

func TestProcessBridgeEvents_StaleOwnerCannotMutateOrReply(t *testing.T) {
	s := &Service{
		output: &fakeOutput{},
	}
	current := newActiveRun()
	stale := newActiveRun()
	s.activeSessions.Store(sessionKey(10, 0, 1000), current)

	ch := make(chan bridge.Event, 1)
	ch <- bridge.Event{Type: "result", Content: "stale result"}
	close(ch)

	outcome := s.ProcessBridgeEvents(10, 0, 99, ch, &fakeProgress{}, "hello", nil, 1000, false, nil, nil,
		runOwnership{owner: stale})
	if outcome != OutcomeCanceled {
		t.Fatalf("outcome = %v, want OutcomeCanceled for superseded owner", outcome)
	}
}

// TestTrackRunFeedback_ExactAggregation verifies the exact aggregation math:
// first_feedback_ms from run start, max_silence_ms as the largest gap between
// feedback events, and stall/steer counters independent of feedback. Feedback
// times are past-relative (within the bridge skew window) so the local-clock
// clamps never fire and the math is deterministic.
func TestTrackRunFeedback_ExactAggregation(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(2, 1, 200)
	started := time.Now().Add(-30 * time.Second)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: started}
	s.runLogMu.Unlock()

	// First feedback 5s after run start; stall/steer telemetry interleaved.
	s.trackRunFeedback(2, 1, 200, started.Add(5*time.Second))
	s.countBridgeTelemetry(2, 1, 200, "stall")
	s.countBridgeTelemetry(2, 1, 200, "stall")
	s.countBridgeTelemetry(2, 1, 200, "steer")
	// Second feedback 25s after run start → largest gap is 20s.
	s.trackRunFeedback(2, 1, 200, started.Add(25*time.Second))

	s.completeRunLog(2, 1, 200, runlog.RunCompleted, "", "")

	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1", len(comps))
	}
	agg := comps[0].agg
	if agg.FirstFeedbackMs != 5000 {
		t.Fatalf("first_feedback_ms = %d, want 5000", agg.FirstFeedbackMs)
	}
	if agg.MaxSilenceMs != 20000 {
		t.Fatalf("max_silence_ms = %d, want 20000", agg.MaxSilenceMs)
	}
	if agg.StallCount != 2 || agg.SteerCount != 1 {
		t.Fatalf("stall/steer = %d/%d, want 2/1", agg.StallCount, agg.SteerCount)
	}
}

// TestCompleteRunLog_SilentRun_TrailingSilence covers the silent-stream
// fixture: a run with no feedback at all persists max_silence spanning the
// whole run, and telemetry counters survive without any feedback.
func TestCompleteRunLog_SilentRun_TrailingSilence(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(3, 0, 300)
	started := time.Now().Add(-10 * time.Minute)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: started}
	s.runLogMu.Unlock()

	s.countBridgeTelemetry(3, 0, 300, "stall")
	s.completeRunLog(3, 0, 300, runlog.RunTimedOut, "", "idle_bridge_timeout")

	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1", len(comps))
	}
	agg := comps[0].agg
	if agg.FirstFeedbackMs != 0 {
		t.Fatalf("first_feedback_ms = %d, want 0 (no feedback ever)", agg.FirstFeedbackMs)
	}
	if agg.MaxSilenceMs < 600_000 {
		t.Fatalf("max_silence_ms = %d, want >= 600000 (whole silent run)", agg.MaxSilenceMs)
	}
	if agg.StallCount != 1 || agg.SteerCount != 0 {
		t.Fatalf("stall/steer = %d/%d, want 1/0", agg.StallCount, agg.SteerCount)
	}
}

// TestBridgeEventTime_SkewWindow ensures Bridge ISO timestamps are accepted
// only within the explicit skew window of the local clock; far-past and
// far-future timestamps fall back to the local clock so they can never
// produce negative or unbounded aggregates.
func TestBridgeEventTime_SkewWindow(t *testing.T) {
	now := time.Now()

	// Within the window: preserved as correlation/sub-second time.
	within := now.Add(-30 * time.Second)
	parsed := bridgeEventTime(bridge.Event{Timestamp: within.Format(time.RFC3339Nano)})
	if !parsed.Equal(within) {
		t.Fatalf("within-skew timestamp = %v, want %v", parsed, within)
	}

	// Far past (2020): falls back to local clock, never the ancient value.
	old := bridgeEventTime(bridge.Event{Timestamp: "2020-01-01T00:00:00Z"})
	if old.Before(now.Add(-time.Minute)) || old.After(time.Now().Add(time.Minute)) {
		t.Fatalf("far-past timestamp fell back to %v, want local now", old)
	}

	// Far future (2030): falls back to local clock, never a future value.
	future := bridgeEventTime(bridge.Event{Timestamp: "2030-01-01T00:00:00Z"})
	if future.After(time.Now().Add(time.Minute)) {
		t.Fatalf("far-future timestamp fell back to %v, want local now", future)
	}

	// Invalid / empty: local clock.
	invalid := bridgeEventTime(bridge.Event{Timestamp: "not-a-timestamp"})
	if invalid.After(time.Now()) {
		t.Fatal("invalid timestamp fallback must not be in the future")
	}
	if invalid.Before(time.Now().Add(-time.Minute)) {
		t.Fatal("invalid timestamp fallback must be near now")
	}
}

// TestTrackRunFeedback_ClockSkewAndOutOfOrder drives the deterministic skew
// fixtures: a very old Bridge timestamp, a far-future one, and out-of-order
// feedback. The aggregates must stay non-negative and bounded by the local
// run time; out-of-order events must never inflate trailing silence.
func TestTrackRunFeedback_ClockSkewAndOutOfOrder(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(4, 0, 400)
	started := time.Now().Add(-60 * time.Second)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: started}
	s.runLogMu.Unlock()

	// Very old Bridge timestamp (2020): rejected by the skew window, treated
	// as local arrival — cannot create a negative first-feedback gap.
	s.trackRunFeedback(4, 0, 400, bridgeEventTime(bridge.Event{Timestamp: "2020-01-01T00:00:00Z"}))
	// Far-future Bridge timestamp (2030): rejected, clamped to local time.
	s.trackRunFeedback(4, 0, 400, bridgeEventTime(bridge.Event{Timestamp: "2030-01-01T00:00:00Z"}))
	// Out-of-order: newer then older — the older event must not move the
	// clock back nor inflate the silence.
	s.trackRunFeedback(4, 0, 400, started.Add(40*time.Second))
	s.trackRunFeedback(4, 0, 400, started.Add(10*time.Second))

	s.completeRunLog(4, 0, 400, runlog.RunCompleted, "", "")

	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1", len(comps))
	}
	agg := comps[0].agg
	if agg.FirstFeedbackMs < 0 {
		t.Fatalf("first_feedback_ms = %d, want >= 0", agg.FirstFeedbackMs)
	}
	if agg.MaxSilenceMs < 0 {
		t.Fatalf("max_silence_ms = %d, want >= 0", agg.MaxSilenceMs)
	}
	// Bounded by the local run time (60s), not by skewed timestamps.
	if agg.MaxSilenceMs > 60_000 {
		t.Fatalf("max_silence_ms = %d, want <= 60000 (bounded by local run time)", agg.MaxSilenceMs)
	}
	if agg.FirstFeedbackMs > 60_000 {
		t.Fatalf("first_feedback_ms = %d, want <= 60000 (bounded by local run time)", agg.FirstFeedbackMs)
	}
}

// TestRecordPipelineEvent_TelemetryBudget enforces the per-run telemetry
// budget: overflowing stall/steer/compaction events are dropped explicitly
// (counted), while terminal events are never limited.
func TestRecordPipelineEvent_TelemetryBudget(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(5, 0, 500)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	// Pump far more telemetry events than the budget allows.
	for i := 0; i < maxTelemetryEventsPerRun+10; i++ {
		s.recordPipelineEvent(5, 0, 500, observability.NewWarnEvent("",
			observability.PhaseBridgeStall, "event=stall severity=unknown silent_ms=0"))
	}
	// Terminal events are never subject to the telemetry budget.
	s.recordPipelineEvent(5, 0, 500, observability.NewEvent(runID,
		observability.PhaseRunCompleted, "status=completed"))

	// The drop is explicit and countable: capture the counter while the
	// state is still alive (completeRunLog removes it).
	s.runLogMu.Lock()
	state := s.runLogStates[key]
	s.runLogMu.Unlock()
	state.mu.Lock()
	dropped := state.telemetryDropped
	state.mu.Unlock()

	// Flush the pending events the way completeRunLog does.
	s.completeRunLog(5, 0, 500, runlog.RunCompleted, "", "")

	evs := spy.recordedEvents()
	telemetry := 0
	terminal := 0
	for _, ev := range evs {
		switch ev.Phase {
		case string(observability.PhaseBridgeStall):
			telemetry++
		case string(observability.PhaseRunCompleted):
			terminal++
		}
	}
	if telemetry > maxTelemetryEventsPerRun {
		t.Fatalf("telemetry events recorded = %d, want <= %d", telemetry, maxTelemetryEventsPerRun)
	}
	if telemetry == maxTelemetryEventsPerRun+10 {
		t.Fatalf("telemetry budget not enforced: all %d events recorded", telemetry)
	}
	if terminal != 1 {
		t.Fatalf("terminal events = %d, want 1 (terminal must never be dropped)", terminal)
	}
	if dropped != 10 {
		t.Fatalf("telemetryDropped = %d, want 10 (explicitly countable drops)", dropped)
	}
}

// TestProcessBridgeEvents_CompactionTelemetry covers the bounded compaction
// metadata contract: enum reason (never raw SDK text), static error_class
// (never raw error messages), explicit delta presence with effectiveness
// classification (effective|neutral|regressive|unknown), duration presence,
// and source=bridge_health.
func TestProcessBridgeEvents_CompactionTelemetry(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(7, 0, 700)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	ptr := func(v int) *int { return &v }

	ch := make(chan bridge.Event, 16)
	// start with the enum value (the bridge normalizes raw SDK text before
	// emitting; Go re-validates and rejects anything outside the enum).
	ch <- bridge.Event{Type: "compaction_start", RequestID: "compact-42", Reason: "automatic"}
	// regressive: POSITIVE delta (context grew) with measured duration.
	ch <- bridge.Event{Type: "compaction_end", RequestID: "compact-42", Reason: "auto", Success: true,
		TokensBefore: 1000, TokensAfter: ptr(1200), DeltaTokens: ptr(200),
		DurationMeasured: true, DurationMs: 5000, Timestamp: "2026-08-11T10:00:02.000Z"}
	// effective: NEGATIVE delta means the context was reduced — must never be
	// classified as regressive even when the operational flag is false.
	ch <- bridge.Event{Type: "compaction_end", Reason: "", Success: false,
		TokensBefore: 1000, TokensAfter: ptr(800), DeltaTokens: ptr(-200),
		ErrorClass: "compaction_error"}
	// neutral: zero delta.
	ch <- bridge.Event{Type: "compaction_end", Reason: "manual",
		TokensBefore: 500, TokensAfter: ptr(500), DeltaTokens: ptr(0), Success: true}
	// unknown: delta absent.
	ch <- bridge.Event{Type: "compaction_end", Reason: "raw free text", Success: true}
	// terminal so the run completes.
	ch <- bridge.Event{Type: "result", Content: "done"}
	close(ch)

	outcome := s.ProcessBridgeEvents(7, 0, 700, ch, &fakeProgress{}, "hi", nil, 700, false, nil, nil)
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want OutcomeSuccess", outcome)
	}

	events := spy.recordedEvents()
	var compactions []runlog.RunEvent
	for _, ev := range events {
		if ev.Phase == string(observability.PhaseBridgeCompaction) {
			compactions = append(compactions, ev)
		}
	}
	if len(compactions) != 5 {
		t.Fatalf("compaction events = %d, want 5", len(compactions))
	}

	decode := func(ev runlog.RunEvent) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal([]byte(ev.MetadataJSON), &m); err != nil {
			t.Fatalf("compaction metadata not JSON: %v", err)
		}
		if m["source"] != "bridge_health" {
			t.Fatalf("compaction source = %v, want bridge_health", m["source"])
		}
		return m
	}

	if m := decode(compactions[0]); m["reason"] != "automatic" {
		t.Fatalf("compaction_start reason = %v, want automatic (normalized)", m["reason"])
	}
	if m := decode(compactions[0]); m["request_id"] != "compact-42" {
		t.Fatalf("compaction request_id = %v, want compact-42", m["request_id"])
	}
	// Positive delta = context GREW = regressive, even with success=true.
	if m := decode(compactions[1]); m["effectiveness"] != "regressive" || m["success"] != true ||
		m["duration_measured"] != true || m["duration_ms"] != float64(5000) ||
		m["delta_tokens"] != float64(200) || m["ts_iso"] != "2026-08-11T10:00:02.000Z" {
		t.Fatalf("regressive compaction metadata = %v", m)
	}
	// Negative delta = context REDUCED = effective.
	if m := decode(compactions[2]); m["effectiveness"] != "effective" || m["success"] != false ||
		m["error_class"] != "compaction_error" {
		t.Fatalf("effective compaction metadata = %v", m)
	}
	if m := decode(compactions[3]); m["effectiveness"] != "neutral" || m["reason"] != "manual" {
		t.Fatalf("neutral compaction metadata = %v", m)
	}
	if m := decode(compactions[4]); m["effectiveness"] != "unknown" || m["reason"] != "unknown" {
		t.Fatalf("unknown compaction metadata = %v", m)
	}
}

// TestProcessBridgeEvents_SeverityBounded ensures arbitrary severity text is
// normalized to the warning|urgent|unknown enum and never persisted raw.
func TestProcessBridgeEvents_SeverityBounded(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(8, 0, 800)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	ch := make(chan bridge.Event, 4)
	ch <- bridge.Event{Type: "stall", Severity: "arbitrary user-controlled text", SilentMs: 60000}
	ch <- bridge.Event{Type: "steer", Severity: "", SilentMs: 120000}
	ch <- bridge.Event{Type: "result", Content: "done"}
	close(ch)

	outcome := s.ProcessBridgeEvents(8, 0, 800, ch, &fakeProgress{}, "hi", nil, 800, false, nil, nil)
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want OutcomeSuccess", outcome)
	}

	events := spy.recordedEvents()
	var stall, steer *runlog.RunEvent
	for i := range events {
		switch events[i].Phase {
		case string(observability.PhaseBridgeStall):
			stall = &events[i]
		case string(observability.PhaseBridgeSteer):
			steer = &events[i]
		}
	}
	if stall == nil || steer == nil {
		t.Fatalf("missing stall/steer: stall=%v steer=%v", stall != nil, steer != nil)
	}
	if strings.Contains(stall.Message, "arbitrary user-controlled text") {
		t.Fatalf("raw severity leaked: %q", stall.Message)
	}
	for _, ev := range []*runlog.RunEvent{stall, steer} {
		var m map[string]any
		if err := json.Unmarshal([]byte(ev.MetadataJSON), &m); err != nil {
			t.Fatalf("metadata not JSON: %v", err)
		}
		if m["severity"] != "unknown" {
			t.Fatalf("severity = %v, want unknown (normalized)", m["severity"])
		}
	}
}

// TestRecordPipelineEvent_TelemetryByteBudget enforces the per-run byte
// budget: large telemetry metadata cannot grow pendingEvents without limit.
func TestRecordPipelineEvent_TelemetryByteBudget(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(6, 0, 600)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	// ~300B metadata per telemetry event: the byte budget (16KB) binds well
	// before the count budget (64).
	big := telemetryMetadata(map[string]any{"ts_iso": strings.Repeat("x", 280)})
	for i := 0; i < maxTelemetryEventsPerRun; i++ {
		ev := observability.NewWarnEvent("", observability.PhaseBridgeCompaction, "event=compaction_end")
		ev.MetadataJSON = big
		s.recordPipelineEvent(6, 0, 600, ev)
	}

	// Flush the pending events the way completeRunLog does.
	s.completeRunLog(6, 0, 600, runlog.RunCompleted, "", "")

	evs := spy.recordedEvents()
	telemetry := 0
	for _, ev := range evs {
		if ev.Phase == string(observability.PhaseBridgeCompaction) {
			telemetry++
		}
	}
	if telemetry >= maxTelemetryEventsPerRun {
		t.Fatalf("byte budget not enforced: %d events recorded, want < %d", telemetry, maxTelemetryEventsPerRun)
	}
	if telemetry == 0 {
		t.Fatal("byte budget too aggressive: no telemetry recorded at all")
	}
}

// TestProcessBridgeEvents_ErrorClassAllowlist covers A3: error_class is an
// allowlist in the consumer — arbitrary text (including CR/LF and other
// control characters) degrades to "unknown" and the raw value is never
// persisted; only the static compaction_error enum survives.
func TestProcessBridgeEvents_ErrorClassAllowlist(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(12, 0, 1200)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	ch := make(chan bridge.Event, 6)
	// Static enum survives.
	ch <- bridge.Event{Type: "compaction_end", Success: false, ErrorClass: "compaction_error"}
	// Arbitrary attacker-ish text with CR/LF + control chars degrades.
	ch <- bridge.Event{Type: "compaction_end", Success: false, ErrorClass: "INJECTED\r\n\x00\x1f<script>"}
	// Empty stays omitted.
	ch <- bridge.Event{Type: "compaction_end", Success: true}
	// Terminal so the run completes.
	ch <- bridge.Event{Type: "result", Content: "done"}
	close(ch)

	outcome := s.ProcessBridgeEvents(12, 0, 1200, ch, &fakeProgress{}, "hi", nil, 1200, false, nil, nil)
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want OutcomeSuccess", outcome)
	}

	events := spy.recordedEvents()
	var compactions []runlog.RunEvent
	for _, ev := range events {
		if ev.Phase == string(observability.PhaseBridgeCompaction) {
			compactions = append(compactions, ev)
		}
	}
	if len(compactions) != 3 {
		t.Fatalf("compaction events = %d, want 3", len(compactions))
	}

	decode := func(ev runlog.RunEvent) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal([]byte(ev.MetadataJSON), &m); err != nil {
			t.Fatalf("compaction metadata not JSON: %v", err)
		}
		return m
	}

	if m := decode(compactions[0]); m["error_class"] != "compaction_error" {
		t.Fatalf("static enum error_class = %v, want compaction_error", m["error_class"])
	}
	if m := decode(compactions[1]); m["error_class"] != "unknown" {
		t.Fatalf("arbitrary error_class = %v, want unknown (allowlist)", m["error_class"])
	}
	if raw := decode(compactions[1]); strings.Contains(fmt.Sprint(raw), "INJECTED") {
		t.Fatalf("raw error_class text leaked into metadata: %v", raw)
	}
	if m := decode(compactions[2]); m["error_class"] != nil {
		t.Fatalf("empty error_class = %v, want omitted", m["error_class"])
	}
}

// TestCountBridgeTelemetry_SaturatesAtBudget covers A3: stall/steer counters
// saturate at the telemetry budget bound so a runaway bridge telemetry stream
// can never grow the counters without limit.
func TestCountBridgeTelemetry_SaturatesAtBudget(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(13, 0, 1300)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	for i := 0; i < maxTelemetryEventsPerRun+500; i++ {
		s.countBridgeTelemetry(13, 0, 1300, "stall")
	}
	s.countBridgeTelemetry(13, 0, 1300, "steer")

	s.completeRunLog(13, 0, 1300, runlog.RunCompleted, "", "")

	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want 1", len(comps))
	}
	if comps[0].agg.StallCount != maxTelemetryEventsPerRun {
		t.Fatalf("stall_count = %d, want saturated at %d", comps[0].agg.StallCount, maxTelemetryEventsPerRun)
	}
	if comps[0].agg.SteerCount != 1 {
		t.Fatalf("steer_count = %d, want 1", comps[0].agg.SteerCount)
	}
}

// TestRecordPipelineEvent_BudgetCountsMessageAndToolEvents covers A3: the
// per-run telemetry budget counts message bytes + metadata bytes together and
// also covers tool-use/tool-result events, not only stall/steer/compaction.
func TestRecordPipelineEvent_BudgetCountsMessageAndToolEvents(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(14, 0, 1400)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	// Big messages (not just metadata) must consume the byte budget: each
	// tool_result event carries a ~3KB message, so the 16KB byte budget binds
	// long before the 64-event count budget.
	big := strings.Repeat("x", 3000)
	for i := 0; i < maxTelemetryEventsPerRun; i++ {
		s.recordPipelineEvent(14, 0, 1400, observability.NewEvent("",
			observability.PhaseBridgeToolResult, big))
	}
	// Tool events also count against the event budget: after the byte budget
	// is exhausted, further tool telemetry is dropped (counted), while a
	// terminal event is never dropped.
	s.recordPipelineEvent(14, 0, 1400, observability.NewEvent(runID,
		observability.PhaseRunCompleted, "status=completed"))

	s.completeRunLog(14, 0, 1400, runlog.RunCompleted, "", "")

	evs := spy.recordedEvents()
	toolEvents := 0
	terminal := 0
	for _, ev := range evs {
		switch ev.Phase {
		case string(observability.PhaseBridgeToolResult):
			toolEvents++
		case string(observability.PhaseRunCompleted):
			terminal++
		}
	}
	if toolEvents >= maxTelemetryEventsPerRun {
		t.Fatalf("byte budget not enforced for tool messages: %d events recorded, want < %d", toolEvents, maxTelemetryEventsPerRun)
	}
	if toolEvents == 0 {
		t.Fatal("byte budget too aggressive: no tool events recorded at all")
	}
	if terminal != 1 {
		t.Fatalf("terminal events = %d, want 1 (terminal must never be dropped)", terminal)
	}
}

// TestRecordToolSummary_CappedAtMaxBytes covers A3: the in-memory tool
// summary (names + summarized results) is capped before growing, so a long
// tool stream can never grow the run state without limit.
func TestRecordToolSummary_CappedAtMaxBytes(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(15, 0, 1500)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	// Pump far more tool use + result summaries than the cap allows.
	for i := 0; i < 500; i++ {
		s.recordToolUse(15, 0, 1500, "Read")
		s.recordToolResult(15, 0, 1500, strings.Repeat("r", 200))
	}

	s.runLogMu.Lock()
	state := s.runLogStates[key]
	s.runLogMu.Unlock()
	state.mu.Lock()
	size := state.summary.Len()
	state.mu.Unlock()
	if size > maxToolSummaryBytes {
		t.Fatalf("tool summary = %d bytes, want <= %d (capped)", size, maxToolSummaryBytes)
	}
	if size == 0 {
		t.Fatal("tool summary empty despite tool activity")
	}
}

// TestRecordToolSummary_UsesStaticDurableLabels ensures untrusted provider
// tool names and result excerpts cannot enter the durable run summary. The
// live progress path may display a bounded summary, but the runlog checkpoint
// must retain only the static tool/result classes.
func TestRecordToolSummary_UsesStaticDurableLabels(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
	}
	runID := idgen.New()
	key := runLogKey(18, 0, 1800)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	secretTool := "provider-tool-name-with-sensitive-details"
	secretResult := "Authorization: Bearer sk-proj-abc123def456ghi789jkl012"
	s.recordToolUse(18, 0, 1800, secretTool)
	s.recordToolResult(18, 0, 1800, secretResult)

	s.runLogMu.Lock()
	state := s.runLogStates[key]
	s.runLogMu.Unlock()
	state.mu.Lock()
	summary := state.summary.String()
	state.mu.Unlock()

	if strings.Contains(summary, secretTool) || strings.Contains(summary, secretResult) || strings.Contains(summary, "sk-proj-") {
		t.Fatalf("untrusted tool data entered durable summary: %q", summary)
	}
	if summary != "tool → [tool_result]" {
		t.Fatalf("durable summary = %q, want static tool/result labels", summary)
	}
}

// TestHandleErrorEvent_RunFailedCarriesRunID covers A2/correction 5: the
// run_failed event emitted AFTER completeRunLog (state already removed) must
// carry the run ID captured before the removal.
func TestHandleErrorEvent_RunFailedCarriesRunID(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}
	runID := idgen.New()
	key := runLogKey(16, 0, 1600)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, startedAt: time.Now()}
	s.runLogMu.Unlock()

	outcome := s.handleErrorEvent(16, 0, 1, bridge.Event{Type: "error", Message: "provider exploded"}, 1600, false)
	if outcome != OutcomeLLMError {
		t.Fatalf("outcome = %v, want OutcomeLLMError", outcome)
	}

	// Exactly one terminal completion (RunFailed) with the same run ID.
	comps := spy.recordedCompletions()
	if len(comps) != 1 {
		t.Fatalf("completions = %d, want exactly 1", len(comps))
	}
	if comps[0].status != runlog.RunFailed || comps[0].runID != runID {
		t.Fatalf("completion = %+v, want RunFailed with runID %q", comps[0], runID)
	}

	// The run_failed event lands AFTER state cleanup and must still carry the
	// captured run ID (no event is dropped for a missing RunID).
	evs := spy.recordedEvents()
	var failed *runlog.RunEvent
	for i := range evs {
		if evs[i].Phase == string(observability.PhaseRunFailed) {
			failed = &evs[i]
		}
	}
	if failed == nil {
		t.Fatal("run_failed event missing")
	}
	if failed.RunID != runID {
		t.Fatalf("run_failed RunID = %q, want %q (captured before state removal)", failed.RunID, runID)
	}
}

func TestTerminalFinalization_SecondErrorCannotOverwriteFirst(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		output:       &fakeOutput{},
		runLog:       spy,
		runLogStates: make(map[string]*runLogState),
	}
	runID := idgen.New()
	key := runLogKey(17, 0, 1700)
	owner := newActiveRun()
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, owner: owner, startedAt: time.Now()}
	s.runLogMu.Unlock()
	s.activeSessions.Store(sessionKey(17, 0, 1700), owner)
	ownership := runOwnership{runID: runID, owner: owner}

	if got := s.handleErrorEvent(17, 0, 1, bridge.Event{Type: "error", Message: "first failure"}, 1700, false, ownership); got != OutcomeLLMError {
		t.Fatalf("first outcome = %v, want LLM error", got)
	}
	if got := s.handleErrorEvent(17, 0, 2, bridge.Event{Type: "error", Message: "second failure"}, 1700, false, ownership); got != OutcomeCanceled {
		t.Fatalf("second outcome = %v, want canceled", got)
	}
	completions := spy.recordedCompletions()
	if len(completions) != 1 || completions[0].runID != runID {
		t.Fatalf("completions = %+v, want exactly one first terminal completion", completions)
	}
}

func TestTerminalFinalization_ErrorThenGenericOutcomeCompletesOnce(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{output: &fakeOutput{}, runLog: spy, runLogStates: make(map[string]*runLogState)}
	owner := newActiveRun()
	runID := idgen.New()
	key := runLogKey(19, 0, 1900)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{runID: runID, owner: owner, startedAt: time.Now()}
	s.runLogMu.Unlock()
	s.activeSessions.Store(sessionKey(19, 0, 1900), owner)
	ownership := runOwnership{runID: runID, owner: owner}

	if got := s.handleErrorEvent(19, 0, 1, bridge.Event{Type: "error", Message: "first"}, 1900, false, ownership); got != OutcomeLLMError {
		t.Fatalf("error outcome = %v, want LLM error", got)
	}
	s.handleRetryOutcome(19, 0, 1, OutcomeProcessDeath, 1900, false, ownership)
	if got := spy.recordedCompletions(); len(got) != 1 {
		t.Fatalf("completions = %d, want exactly one after first terminal claim", len(got))
	}
}
