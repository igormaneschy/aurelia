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
// Rotate threshold is raised to 500K — rotation is a last-resort safety net
// for corrupt sessions, not a routine token-management tool. The PI SDK's
// auto-compaction keeps effective context within model limits automatically.
func DefaultLifecyclePolicy() LifecyclePolicy {
	return LifecyclePolicy{
		Enabled:                      true,
		CompactAfterInputTokens:      200000, // raised from 120000 — PI SDK manages normal compaction
		RotateAfterInputTokens:       500000, // raised from 250000 — only rotate on truly huge sessions
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
//  1. Repeated suspect failures → rotate. Suspect counters can be set while
//     the session is inactive (e.g. MarkEmptyResult sets Active=false), so
//     NeedsRotation must run before the cold/inactive check to preserve the
//     documented priority "repeated suspect failures → rotate".
//  2. Cold/inactive session → cold resume. Inactive sessions without suspect
//     failures resume cold, regardless of enriched token stats. This prevents
//     stale large sessions (e.g. user returned after a day) from triggering
//     a rotate + summary cycle on the first message.
//  3. Single suspect failures → cold resume / safe recovery
//  4. Dangerous token threshold → rotate (active sessions only)
//  5. Large but healthy → continue (PI SDK manages compaction)
//  6. Healthy → continue
//
// ActionCompact is reserved for explicit/manual/fallback use only, not the
// default healthy path. The PI SDK owns normal session compaction.
func EvaluateLifecycle(signals HealthSignals, policy LifecyclePolicy) Decision {
	// 1. Repeated suspect signals are dangerous enough to rotate.
	// Checked before cold/inactive because store failure markers (MarkEmptyResult,
	// MarkProcessDeath) set Active=false as a side effect. Moving cold before
	// NeedsRotation would silently demote repeated suspect failures to cold_resume.
	if policy.NeedsRotation(signals) {
		return Decision{
			State:  HealthDangerous,
			Action: ActionRotate,
			Reason: fmt.Sprintf("suspect threshold reached: empty_results=%d process_deaths=%d", signals.RecentEmptyResults, signals.RecentProcessDeaths),
		}
	}

	// 2. Cold/inactive: session is not active (e.g. restored from disk after
	// restart or user returned after a long idle). Cold wins over token-based
	// decisions to avoid unnecessary rotate/compact on the first user message
	// from an inactive session without suspect failures.
	if !signals.Active {
		return Decision{
			State:  HealthCold,
			Action: ActionColdResume,
			Reason: "session is inactive (cold)",
		}
	}

	// 3. Suspect: empty results or process deaths signal potential corruption.
	if signals.RecentEmptyResults > 0 {
		return Decision{
			State:  HealthSuspect,
			Action: ActionColdResume,
			Reason: fmt.Sprintf("recent_empty_results=%d", signals.RecentEmptyResults),
		}
	}
	if signals.RecentProcessDeaths > 0 {
		return Decision{
			State:  HealthSuspect,
			Action: ActionColdResume,
			Reason: fmt.Sprintf("recent_process_deaths=%d", signals.RecentProcessDeaths),
		}
	}

	// 4. Dangerous: active session with input tokens exceeding rotate threshold.
	if signals.InputTokens >= policy.RotateAfterInputTokens {
		return Decision{
			State:  HealthDangerous,
			Action: ActionRotate,
			Reason: fmt.Sprintf("input_tokens=%d >= rotate_after=%d", signals.InputTokens, policy.RotateAfterInputTokens),
		}
	}

	// 5. Large but healthy: input tokens above the old compact threshold.
	//    PI SDK manages normal compaction internally; Go continues the session
	//    without proactive compaction.
	if signals.InputTokens >= policy.CompactAfterInputTokens {
		return Decision{
			State:  HealthLarge,
			Action: ActionContinue,
			Reason: fmt.Sprintf("input_tokens=%d >= compact_after=%d (PI SDK manages compaction)", signals.InputTokens, policy.CompactAfterInputTokens),
		}
	}

	// 6. Healthy: normal continue.
	return Decision{
		State:  HealthHealthy,
		Action: ActionContinue,
		Reason: "session is healthy",
	}
}

// NeedsRotation returns true if the suspect signals exceed the rotate threshold.
func (p LifecyclePolicy) NeedsRotation(signals HealthSignals) bool {
	if signals.RecentEmptyResults >= p.MaxEmptyResultsBeforeRotate {
		return true
	}
	if signals.RecentProcessDeaths >= p.MaxProcessDeathsBeforeRotate {
		return true
	}
	return false
}
