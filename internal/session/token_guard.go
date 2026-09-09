package session

import (
	"fmt"
	"sync"
)

const (
	// DefaultLargeTurnsBeforeCompact triggers compaction after this many
	// consecutive context readings without meaningful reduction.
	DefaultLargeTurnsBeforeCompact = 3
	// tokenReductionMinPercent is the minimum drop from the previous context
	// reading that counts as successful PI compaction (resets the stall
	// counter). It is expressed in percent of the previous reading.
	tokenReductionMinPercent = 5
)

// TokenGuard tracks per-session CONTEXT usage (percentage of the model's
// context window) and escalates to compact or rotate when the PI SDK fails to
// bound the context in time. It deliberately does not use cumulative billing
// input tokens: that metric only grows, so a reduction could never be observed.
type TokenGuard struct {
	mu      sync.Mutex
	entries map[SessionKey]tokenGuardEntry
}

type tokenGuardEntry struct {
	LastContextPct float64
	StallTurns     int
}

// NewTokenGuard creates an empty token guard tracker.
func NewTokenGuard() *TokenGuard {
	return &TokenGuard{entries: make(map[SessionKey]tokenGuardEntry, 16)}
}

// Evaluate checks whether an active session should be escalated beyond
// ActionContinue based on its current context usage. Returns (decision,
// escalated). Call only when base EvaluateLifecycle returned continue.
//
// contextPct is the current context as a percentage of the model window
// (<= 0 means the SDK could not estimate it). Unknown context never escalates:
// the PI SDK and the provider own the window limit.
func (g *TokenGuard) Evaluate(key SessionKey, contextPct float64, policy LifecyclePolicy) (Decision, bool) {
	if g == nil || contextPct <= 0 || !policy.Enabled {
		return Decision{}, false
	}

	// Emergency ceiling: rotate before the model window is exhausted. Normal
	// compaction is owned by the PI SDK (contextWindow - reserveTokens).
	if policy.EmergencyRotateContextPct > 0 && contextPct >= float64(policy.EmergencyRotateContextPct) {
		g.Reset(key)
		return Decision{
			State:  HealthDangerous,
			Action: ActionRotate,
			Reason: fmt.Sprintf(
				"token guard: context_usage=%.1f%% >= emergency_rotate_context_pct=%d",
				contextPct, policy.EmergencyRotateContextPct,
			),
		}, true
	}

	// Below the attention threshold: PI compaction is keeping the context
	// bounded, so there is nothing to guard.
	if policy.WarnContextPct > 0 && contextPct < float64(policy.WarnContextPct) {
		g.Reset(key)
		return Decision{}, false
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	entry := g.entries[key]
	if entry.LastContextPct > 0 && contextReduced(entry.LastContextPct, contextPct) {
		entry.StallTurns = 0
	} else {
		entry.StallTurns++
	}
	entry.LastContextPct = contextPct
	g.entries[key] = entry

	stallLimit := largeTurnsBeforeCompact(policy)
	if entry.StallTurns < stallLimit {
		return Decision{}, false
	}

	// Escalate to compact; reset stall so a failed compact can accumulate again.
	entry.StallTurns = 0
	g.entries[key] = entry

	return Decision{
		State:  HealthLarge,
		Action: ActionCompact,
		Reason: fmt.Sprintf(
			"token guard: %d consecutive readings without >=%d%% context reduction (context_usage=%.1f%% >= warn_context_pct=%d)",
			stallLimit, tokenReductionMinPercent, contextPct, policy.WarnContextPct,
		),
	}, true
}

// Reset clears tracking for a session (after rotate, cold resume, or /new).
func (g *TokenGuard) Reset(key SessionKey) {
	if g == nil {
		return
	}
	g.mu.Lock()
	delete(g.entries, key)
	g.mu.Unlock()
}

func largeTurnsBeforeCompact(policy LifecyclePolicy) int {
	_ = policy
	return DefaultLargeTurnsBeforeCompact
}

// contextReduced reports whether current is at least tokenReductionMinPercent
// below previous (a successful compaction shrank the context).
func contextReduced(previous, current float64) bool {
	if previous <= 0 || current >= previous {
		return false
	}
	drop := previous - current
	minDrop := previous * float64(tokenReductionMinPercent) / 100
	if minDrop < 0.01 {
		minDrop = 0.01
	}
	return drop >= minDrop
}
