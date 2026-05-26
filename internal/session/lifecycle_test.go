package session

import (
	"testing"
	"time"
)

func TestEvaluateLifecycle_HealthyActiveSession(t *testing.T) {
	signals := HealthSignals{
		Active:            true,
		InputTokens:       1000,
		OutputTokens:      500,
		TotalMessages:     5,
		AssistantMessages: 3,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthHealthy {
		t.Fatalf("expected healthy, got %s", dec.State)
	}
	if dec.Action != ActionContinue {
		t.Fatalf("expected continue, got %s", dec.Action)
	}
	if dec.Reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestEvaluateLifecycle_InactiveSession(t *testing.T) {
	signals := HealthSignals{
		Active:      false,
		InputTokens: 0,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthCold {
		t.Fatalf("expected cold, got %s", dec.State)
	}
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_LargeInputTokens(t *testing.T) {
	signals := HealthSignals{
		Active:      true,
		InputTokens: 150000, // above compact (120k), below rotate (250k)
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthLarge {
		t.Fatalf("expected large, got %s", dec.State)
	}
	if dec.Action != ActionContinue {
		t.Fatalf("expected continue (PI SDK manages compaction), got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_DangerousInputTokens(t *testing.T) {
	signals := HealthSignals{
		Active:      true,
		InputTokens: 300000, // above rotate (250k)
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthDangerous {
		t.Fatalf("expected dangerous, got %s", dec.State)
	}
	if dec.Action != ActionRotate {
		t.Fatalf("expected rotate, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_SuspectDueToEmptyResults(t *testing.T) {
	signals := HealthSignals{
		Active:             true,
		InputTokens:        1000,
		RecentEmptyResults: 1,
	}
	policy := DefaultLifecyclePolicy()
	policy.MaxEmptyResultsBeforeRotate = 2

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthSuspect {
		t.Fatalf("expected suspect, got %s", dec.State)
	}
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_SuspectDueToProcessDeaths(t *testing.T) {
	signals := HealthSignals{
		Active:              true,
		InputTokens:         1000,
		RecentProcessDeaths: 1,
	}
	policy := DefaultLifecyclePolicy()
	policy.MaxProcessDeathsBeforeRotate = 2

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthSuspect {
		t.Fatalf("expected suspect, got %s", dec.State)
	}
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_PriorityDangerousOverSuspect(t *testing.T) {
	// For active sessions, dangerous (suspect + rotate threshold) takes
	// priority over single-suspect cold-resume. NeedsRotation catches
	// this before the single-suspect step.
	signals := HealthSignals{
		Active:              true,
		InputTokens:         300000,
		RecentEmptyResults:  1,
		RecentProcessDeaths: 1,
	}
	policy := DefaultLifecyclePolicy()
	policy.MaxEmptyResultsBeforeRotate = 1
	policy.MaxProcessDeathsBeforeRotate = 1

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthDangerous {
		t.Fatalf("expected dangerous (higher priority), got %s", dec.State)
	}
}

func TestEvaluateLifecycle_PrioritySuspectOverHealthy(t *testing.T) {
	// Suspect should take priority even with large tokens in healthy range.
	signals := HealthSignals{
		Active:             true,
		InputTokens:        1000,
		RecentEmptyResults: 1,
	}
	policy := DefaultLifecyclePolicy()
	policy.MaxEmptyResultsBeforeRotate = 2

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthSuspect {
		t.Fatalf("expected suspect, got %s", dec.State)
	}
}

func TestEvaluateLifecycle_InputTokenBoundary(t *testing.T) {
	// Exactly at compact threshold should continue (PI SDK manages compaction).
	signals := HealthSignals{
		Active:      true,
		InputTokens: 120000,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthLarge {
		t.Fatalf("expected large at boundary, got %s", dec.State)
	}
	if dec.Action != ActionContinue {
		t.Fatalf("expected continue at boundary, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_InputTokenBelowCompact(t *testing.T) {
	// Just below compact threshold should remain healthy.
	signals := HealthSignals{
		Active:      true,
		InputTokens: 119999,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthHealthy {
		t.Fatalf("expected healthy below threshold, got %s", dec.State)
	}
}

func TestNeedsRotation_EmptyResultExceedsThreshold(t *testing.T) {
	policy := DefaultLifecyclePolicy()
	signals := HealthSignals{
		RecentEmptyResults: 2, // default MaxEmptyResultsBeforeRotate=2
	}
	if !policy.NeedsRotation(signals) {
		t.Fatal("expected needs rotation for empty results >= 2")
	}
}

func TestNeedsRotation_ProcessDeathExceedsThreshold(t *testing.T) {
	policy := DefaultLifecyclePolicy()
	signals := HealthSignals{
		RecentProcessDeaths: 2, // default MaxProcessDeathsBeforeRotate=2
	}
	if !policy.NeedsRotation(signals) {
		t.Fatal("expected needs rotation for process deaths >= 2")
	}
}

func TestNeedsRotation_NoFailures(t *testing.T) {
	policy := DefaultLifecyclePolicy()
	signals := HealthSignals{}
	if policy.NeedsRotation(signals) {
		t.Fatal("expected no rotation for clean session")
	}
}

func TestDefaultLifecyclePolicy(t *testing.T) {
	p := DefaultLifecyclePolicy()
	if !p.Enabled {
		t.Fatal("default policy should be enabled")
	}
	if p.CompactAfterInputTokens <= 0 {
		t.Fatalf("expected positive compact threshold, got %d", p.CompactAfterInputTokens)
	}
	if p.RotateAfterInputTokens <= p.CompactAfterInputTokens {
		t.Fatalf("rotate threshold (%d) must be > compact threshold (%d)", p.RotateAfterInputTokens, p.CompactAfterInputTokens)
	}
	if p.MaxEmptyResultsBeforeRotate <= 0 {
		t.Fatalf("expected positive empty result threshold, got %d", p.MaxEmptyResultsBeforeRotate)
	}
}

func TestDecision_String(t *testing.T) {
	d := Decision{State: HealthLarge, Action: ActionCompact, Reason: "input_tokens=150000"}
	s := d.String()
	if s != "large/compact: input_tokens=150000" {
		t.Fatalf("unexpected string: %q", s)
	}
}

func TestEvaluateLifecycle_InactiveAboveCompactThreshold(t *testing.T) {
	// Inactive session with tokens above compact but below rotate should
	// cold-resume, not compact. Cold takes priority over large.
	signals := HealthSignals{
		Active:      false,
		InputTokens: 150000, // above compact (120k), below rotate (250k)
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthCold {
		t.Fatalf("expected cold, got %s", dec.State)
	}
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_ColdOverridesDangerousTokens(t *testing.T) {
	// Inactive/cold sessions must resume cold regardless of token counts.
	// This prevents a stale large session from triggering a rotate + summary
	// cycle on the first user message after returning from idle.
	signals := HealthSignals{
		Active:      false,
		InputTokens: 300000,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthCold {
		t.Fatalf("expected cold (inactive wins over rotate threshold), got %s", dec.State)
	}
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume for inactive session, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_ActiveDangerousStillRotates(t *testing.T) {
	// Active sessions with tokens above rotate threshold still rotate.
	// This ensures the dangerous token policy still applies to active sessions.
	signals := HealthSignals{
		Active:      true,
		InputTokens: 300000,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthDangerous {
		t.Fatalf("expected dangerous for active session with high tokens, got %s", dec.State)
	}
	if dec.Action != ActionRotate {
		t.Fatalf("expected rotate for active session with high tokens, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_SingleEmptyResultSuspects(t *testing.T) {
	// A single empty result under default policy should produce
	// suspect/cold_resume, not rotate.
	signals := HealthSignals{
		Active:             true,
		InputTokens:        1000,
		RecentEmptyResults: 1, // below MaxEmptyResultsBeforeRotate (default=2)
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthSuspect {
		t.Fatalf("expected suspect for 1 empty result, got %s", dec.State)
	}
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume for 1 empty result, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_TwoEmptyResultsRotate(t *testing.T) {
	// Two empty results under default policy should hit NeedsRotation
	// and produce dangerous/rotate.
	signals := HealthSignals{
		Active:             true,
		InputTokens:        1000,
		RecentEmptyResults: 2, // >= MaxEmptyResultsBeforeRotate (default=2)
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthDangerous {
		t.Fatalf("expected dangerous for 2 empty results, got %s", dec.State)
	}
	if dec.Action != ActionRotate {
		t.Fatalf("expected rotate for 2 empty results, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_SingleProcessDeathSuspects(t *testing.T) {
	// A single process death under default policy should produce
	// suspect/cold_resume, not rotate.
	signals := HealthSignals{
		Active:              true,
		InputTokens:         1000,
		RecentProcessDeaths: 1, // below MaxProcessDeathsBeforeRotate (default=2)
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthSuspect {
		t.Fatalf("expected suspect for 1 process death, got %s", dec.State)
	}
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume for 1 process death, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_TwoProcessDeathsRotate(t *testing.T) {
	// Two process deaths under default policy should hit NeedsRotation
	// and produce dangerous/rotate.
	signals := HealthSignals{
		Active:              true,
		InputTokens:         1000,
		RecentProcessDeaths: 2, // >= MaxProcessDeathsBeforeRotate (default=2)
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthDangerous {
		t.Fatalf("expected dangerous for 2 process deaths, got %s", dec.State)
	}
	if dec.Action != ActionRotate {
		t.Fatalf("expected rotate for 2 process deaths, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_InactiveWithSuspectFailuresRotates(t *testing.T) {
	// Inactive sessions with suspect failures reaching the rotation threshold
	// must still rotate. NeedsRotation runs before the cold/inactive check
	// because store failure markers (MarkEmptyResult, MarkProcessDeath) set
	// Active=false as a side effect.
	signals := HealthSignals{
		Active:             false,
		RecentEmptyResults: 2, // >= MaxEmptyResultsBeforeRotate (default=2)
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthDangerous {
		t.Fatalf("expected dangerous for inactive session with suspect failures, got %s", dec.State)
	}
	if dec.Action != ActionRotate {
		t.Fatalf("expected rotate for inactive session with suspect failures, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_EmptySignals(t *testing.T) {
	// Zero-value signals should produce healthy for active session.
	signals := HealthSignals{Active: true}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthHealthy {
		t.Fatalf("expected healthy with empty signals, got %s", dec.State)
	}
}

func TestEvaluateLifecycle_LastSeenNotAffectingDecision(t *testing.T) {
	// LastSeen is informational, not a decision input in current logic.
	old := time.Now().Add(-72 * time.Hour)
	signals := HealthSignals{
		Active:      true,
		InputTokens: 1000,
		LastSeen:    old,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthHealthy {
		t.Fatalf("expected healthy regardless of LastSeen, got %s", dec.State)
	}
}
