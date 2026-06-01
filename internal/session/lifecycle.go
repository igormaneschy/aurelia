package session

import (
	"fmt"
	"time"
)

// HealthState represents the assessed health of a PI session.
type HealthState string

const (
	HealthHealthy   HealthState = "healthy"
	HealthLarge     HealthState = "large"
	HealthCold      HealthState = "cold"
	HealthSuspect   HealthState = "suspect"
	HealthDangerous HealthState = "dangerous"
)

// LifecycleAction is the action the pipeline should take based on health assessment.
type LifecycleAction string

const (
	ActionContinue   LifecycleAction = "continue"
	ActionColdResume LifecycleAction = "cold_resume"
	ActionCompact    LifecycleAction = "compact"
	ActionRotate     LifecycleAction = "rotate"
)

// HealthSignals carries the metrics used to assess session health.
type HealthSignals struct {
	Active              bool
	InputTokens         int
	OutputTokens        int
	TotalMessages       int
	AssistantMessages   int
	ToolResults         int
	RecentTimeouts      int
	RecentEmptyResults  int
	RecentProcessDeaths int
	LastError           string
	LastSeen            time.Time
}

// Decision is the output of lifecycle evaluation.
type Decision struct {
	State  HealthState
	Action LifecycleAction
	Reason string
}

func (d Decision) String() string {
	return fmt.Sprintf("%s/%s: %s", d.State, d.Action, d.Reason)
}

// LifecyclePolicy configures thresholds for lifecycle decisions.
type LifecyclePolicy struct {
	Enabled                      bool
	CompactAfterInputTokens      int
	RotateAfterInputTokens       int
	MaxEmptyResultsBeforeRotate  int
	MaxProcessDeathsBeforeRotate int
	IdleTimeoutMinutes           int
	KeepRecentTokens             int
	ReserveTokens                int
}

// DefaultLifecyclePolicy returns safe defaults for session lifecycle policy.
// PI SDK handles auto-compaction internally (contextWindow - reserveTokens).
// Aurelia must not rotate large sessions just because token counters are high:
// rotation creates a new session seeded by a generated summary and can degrade
// topic continuity. Token thresholds are now observability signals only.
func DefaultLifecyclePolicy() LifecyclePolicy {
	return LifecyclePolicy{
		Enabled:                      true,
		CompactAfterInputTokens:      200000, // informational — PI SDK manages normal compaction
		RotateAfterInputTokens:       500000, // informational — automatic token rotation is disabled
		MaxEmptyResultsBeforeRotate:  2,
		MaxProcessDeathsBeforeRotate: 2,
		IdleTimeoutMinutes:           20,
		KeepRecentTokens:             8000,
		ReserveTokens:                32768,
	}
}

// EvaluateLifecycle assesses session health signals against policy and returns
// a decision. This function is pure: no I/O, no side effects.
//
// Priority order:
//  1. Cold/inactive session → cold resume. PI restores the persisted
//     session_file; Aurelia must not replace it with a summary-seeded session.
//  2. Suspect failures → cold resume / safe recovery. Repeated failures are
//     logged in the reason, but still resume the original PI session first.
//  3. Large or very large token counts → continue. PI SDK owns normal
//     context compaction and pruning.
//  4. Healthy → continue.
//
// ActionCompact and ActionRotate are reserved for explicit/manual/emergency
// flows only, not automatic token management. The PI SDK owns continuity.
func EvaluateLifecycle(signals HealthSignals, policy LifecyclePolicy) Decision {
	// 1. Cold/inactive: session is not active (e.g. restored from disk after
	// restart or user returned after a long idle). Cold wins over every
	// token-based decision to avoid unnecessary rotate/summary cycles.
	if !signals.Active {
		return Decision{
			State:  HealthCold,
			Action: ActionColdResume,
			Reason: "session is inactive (cold)",
		}
	}

	// 2. Suspect: empty results or process deaths signal potential corruption.
	// Cold resume preserves the original PI session_file and lets PI recover
	// before Aurelia considers any explicit manual intervention.
	if signals.RecentEmptyResults > 0 || signals.RecentProcessDeaths > 0 {
		state := HealthSuspect
		reason := fmt.Sprintf("recent_empty_results=%d recent_process_deaths=%d", signals.RecentEmptyResults, signals.RecentProcessDeaths)
		if policy.NeedsRotation(signals) {
			state = HealthDangerous
			reason = fmt.Sprintf("suspect threshold reached; cold-resuming original PI session: empty_results=%d process_deaths=%d", signals.RecentEmptyResults, signals.RecentProcessDeaths)
		}
		return Decision{
			State:  state,
			Action: ActionColdResume,
			Reason: reason,
		}
	}

	// 3. Large but healthy: PI SDK manages normal compaction internally; Go
	// continues the original session without proactive compaction or rotation.
	if signals.InputTokens >= policy.CompactAfterInputTokens {
		return Decision{
			State:  HealthLarge,
			Action: ActionContinue,
			Reason: fmt.Sprintf("input_tokens=%d >= compact_after=%d (PI SDK owns compaction/continuity)", signals.InputTokens, policy.CompactAfterInputTokens),
		}
	}

	// 4. Healthy: normal continue.
	return Decision{
		State:  HealthHealthy,
		Action: ActionContinue,
		Reason: "session is healthy",
	}
}

// NeedsRotation reports whether suspect signals crossed the legacy rotation
// threshold. EvaluateLifecycle uses this as severity metadata only; automatic
// rotation is disabled so PI can preserve continuity through the original
// session_file.
func (p LifecyclePolicy) NeedsRotation(signals HealthSignals) bool {
	if signals.RecentEmptyResults >= p.MaxEmptyResultsBeforeRotate {
		return true
	}
	if signals.RecentProcessDeaths >= p.MaxProcessDeathsBeforeRotate {
		return true
	}
	return false
}
