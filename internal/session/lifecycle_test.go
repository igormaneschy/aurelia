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
		Active:          true,
		InputTokens:     350000, // cumulative billing — ignored for context
		ContextUsagePct: 75,     // current context above warn (70%)
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

func TestEvaluateLifecycle_VeryLargeInputTokensContinue(t *testing.T) {
	signals := HealthSignals{
		Active:          true,
		InputTokens:     550000, // cumulative billing — ignored for context
		ContextUsagePct: 90,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthLarge {
		t.Fatalf("expected large, got %s", dec.State)
	}
	if dec.Action != ActionContinue {
		t.Fatalf("expected continue because PI SDK owns continuity, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_CumulativeTokensDoNotDriveContext(t *testing.T) {
	// Regression for the 2026-09-09 incident: a 1M-window model was rotated at
	// input_tokens=506751 (cumulative billing) while the CURRENT context was
	// ~10% of the window. Cumulative tokens must never mark a session large or
	// rotate it; only context_usage_pct does.
	signals := HealthSignals{
		Active:          true,
		InputTokens:     506751, // cumulative billing
		ContextUsagePct: 10,     // current context: 10% of a 1M window
		ContextTokens:   100000,
		ContextWindow:   1048576,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthHealthy {
		t.Fatalf("expected healthy (low context), got %s (%s)", dec.State, dec.Reason)
	}
	if dec.Action != ActionContinue {
		t.Fatalf("expected continue, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_IncidentTokenCountDoesNotRotate(t *testing.T) {
	// Regression for 2026-06-01: an active topic session with ~371k cumulative
	// input tokens and an older 250k rotate threshold must continue the original
	// PI session_file. Aurelia must not create a summary-seeded replacement.
	signals := HealthSignals{
		Active:          true,
		InputTokens:     371682, // cumulative billing — ignored for context
		ContextUsagePct: 40,     // current context well within the window
	}
	policy := DefaultLifecyclePolicy()
	policy.CompactAfterInputTokens = 120000
	policy.RotateAfterInputTokens = 250000

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthHealthy {
		t.Fatalf("expected healthy (cumulative tokens ignored), got %s", dec.State)
	}
	if dec.Action != ActionContinue {
		t.Fatalf("expected continue for incident token count, got %s", dec.Action)
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

func TestEvaluateLifecycle_RepeatedSuspectColdResumes(t *testing.T) {
	// Repeated suspect signals are dangerous, but automatic rotation is not
	// allowed because PI owns continuity. Aurelia cold-resumes the original
	// session_file first.
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
		t.Fatalf("expected dangerous signal, got %s", dec.State)
	}
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume for repeated suspect signals, got %s", dec.Action)
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
	// Exactly at the warn percentage continues (PI SDK manages compaction).
	signals := HealthSignals{
		Active:          true,
		ContextUsagePct: 70,
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
	// Just below the warn percentage remains healthy.
	signals := HealthSignals{
		Active:          true,
		ContextUsagePct: 69,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthHealthy {
		t.Fatalf("expected healthy below threshold, got %s", dec.State)
	}
}

func TestEvaluateLifecycle_UnknownContextIsHealthy(t *testing.T) {
	// When the SDK cannot estimate the context (pct <= 0), the session is not
	// marked large even with high cumulative tokens.
	signals := HealthSignals{
		Active:      true,
		InputTokens: 900000,
	}
	policy := DefaultLifecyclePolicy()

	if dec := EvaluateLifecycle(signals, policy); dec.State != HealthHealthy {
		t.Fatalf("expected healthy for unknown context, got %s", dec.State)
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

func TestEvaluateLifecycle_ActiveDangerousTokensStillContinue(t *testing.T) {
	// Active sessions with high CURRENT context still continue. The PI SDK owns
	// compaction and continuity for large sessions.
	signals := HealthSignals{
		Active:          true,
		ContextUsagePct: 90,
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthLarge {
		t.Fatalf("expected large for active session with high context, got %s", dec.State)
	}
	if dec.Action != ActionContinue {
		t.Fatalf("expected continue for active session with high context, got %s", dec.Action)
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

func TestEvaluateLifecycle_TwoEmptyResultsColdResume(t *testing.T) {
	// Two empty results under default policy are dangerous, but Aurelia must not
	// auto-rotate; it cold-resumes the original PI session first.
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
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume for 2 empty results, got %s", dec.Action)
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

func TestEvaluateLifecycle_TwoProcessDeathsColdResume(t *testing.T) {
	// Two process deaths under default policy are dangerous, but Aurelia must not
	// auto-rotate; it cold-resumes the original PI session first.
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
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume for 2 process deaths, got %s", dec.Action)
	}
}

func TestEvaluateLifecycle_InactiveWithSuspectFailuresColdResumes(t *testing.T) {
	// Inactive sessions always cold-resume the original PI session. This avoids
	// replacing topic context with a summary-generated session during recovery.
	signals := HealthSignals{
		Active:             false,
		RecentEmptyResults: 2, // >= MaxEmptyResultsBeforeRotate (default=2)
	}
	policy := DefaultLifecyclePolicy()

	dec := EvaluateLifecycle(signals, policy)

	if dec.State != HealthCold {
		t.Fatalf("expected cold for inactive session with suspect failures, got %s", dec.State)
	}
	if dec.Action != ActionColdResume {
		t.Fatalf("expected cold_resume for inactive session with suspect failures, got %s", dec.Action)
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
