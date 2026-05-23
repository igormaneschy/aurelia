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
	ActionContinue    LifecycleAction = "continue"
	ActionColdResume  LifecycleAction = "cold_resume"
	ActionCompact     LifecycleAction = "compact"
	ActionRotate      LifecycleAction = "rotate"
)

// HealthSignals carries the metrics used to assess session health.
type HealthSignals struct {
	Active               bool
	InputTokens          int
	OutputTokens         int
	TotalMessages        int
	AssistantMessages    int
	ToolResults          int
	RecentTimeouts       int
	RecentEmptyResults   int
	RecentProcessDeaths  int
	LastError            string
	LastSeen             time.Time
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
func DefaultLifecyclePolicy() LifecyclePolicy {
	return LifecyclePolicy{
		Enabled:                      true,
		CompactAfterInputTokens:      120000,
		RotateAfterInputTokens:       250000,
		MaxEmptyResultsBeforeRotate:  1,
		MaxProcessDeathsBeforeRotate: 1,
		IdleTimeoutMinutes:           20,
		KeepRecentTokens:             8000,
		ReserveTokens:                32768,
	}
}

// EvaluateLifecycle assesses session health signals against policy and returns
// a decision. This function is pure: no I/O, no side effects.
func EvaluateLifecycle(signals HealthSignals, policy LifecyclePolicy) Decision {
	// Priority order: check most consequential states first.

	// 1. Cold: session is not active (e.g. restored from disk after restart).
	// Check before dangerous/large because an inactive session's metrics are stale.
	if !signals.Active {
		return Decision{
			State:  HealthCold,
			Action: ActionColdResume,
			Reason: "session is inactive (cold)",
		}
	}

	// 2. Dangerous: input tokens exceed rotate threshold.
	if signals.InputTokens >= policy.RotateAfterInputTokens {
		return Decision{
			State:  HealthDangerous,
			Action: ActionRotate,
			Reason: fmt.Sprintf("input_tokens=%d >= rotate_after=%d", signals.InputTokens, policy.RotateAfterInputTokens),
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

	// 4. Large: input tokens above compact threshold.
	if signals.InputTokens >= policy.CompactAfterInputTokens {
		return Decision{
			State:  HealthLarge,
			Action: ActionCompact,
			Reason: fmt.Sprintf("input_tokens=%d >= compact_after=%d", signals.InputTokens, policy.CompactAfterInputTokens),
		}
	}

	// 5. Healthy: normal continue.
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
