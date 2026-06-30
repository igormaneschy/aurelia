package session

import (
	"fmt"
	"sync"
)

const (
	// DefaultWarnInputTokens logs a warning when input tokens reach this count.
	DefaultWarnInputTokens = 500_000
	// DefaultLargeTurnsBeforeCompact triggers compaction after this many
	// consecutive large turns without meaningful token reduction.
	DefaultLargeTurnsBeforeCompact = 3
	// tokenReductionMinPercent is the minimum drop from the previous reading
	// that counts as successful PI compaction (resets the stall counter).
	tokenReductionMinPercent = 5
)

// TokenGuard tracks per-session input token growth and escalates to compact or
// rotate when the PI SDK fails to bound context size in time.
type TokenGuard struct {
	mu      sync.Mutex
	entries map[SessionKey]tokenGuardEntry
}

type tokenGuardEntry struct {
	LastInputTokens int
	StallTurns      int
}

// NewTokenGuard creates an empty token guard tracker.
func NewTokenGuard() *TokenGuard {
	return &TokenGuard{entries: make(map[SessionKey]tokenGuardEntry, 16)}
}

// Evaluate checks whether an active session with high input tokens should be
// escalated beyond ActionContinue. Returns (decision, escalated).
// Call only when base EvaluateLifecycle returned continue for a large session.
func (g *TokenGuard) Evaluate(key SessionKey, inputTokens int, policy LifecyclePolicy) (Decision, bool) {
	if g == nil || inputTokens <= 0 || !policy.Enabled {
		return Decision{}, false
	}
	if policy.CompactAfterInputTokens <= 0 {
		return Decision{}, false
	}

	// Hard ceiling: rotate immediately before provider context limits are hit.
	if policy.RotateAfterInputTokens > 0 && inputTokens >= policy.RotateAfterInputTokens {
		g.Reset(key)
		return Decision{
			State:  HealthDangerous,
			Action: ActionRotate,
			Reason: fmt.Sprintf(
				"token guard: input_tokens=%d >= rotate_after=%d",
				inputTokens, policy.RotateAfterInputTokens,
			),
		}, true
	}

	// Session returned below compact threshold — PI compaction worked or session shrank.
	if inputTokens < policy.CompactAfterInputTokens {
		g.Reset(key)
		return Decision{}, false
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	entry := g.entries[key]
	if entry.LastInputTokens > 0 && tokensReduced(entry.LastInputTokens, inputTokens) {
		entry.StallTurns = 0
	} else {
		entry.StallTurns++
	}
	entry.LastInputTokens = inputTokens
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
			"token guard: %d consecutive large turns without >=%d%% token reduction (input_tokens=%d >= compact_after=%d)",
			stallLimit, tokenReductionMinPercent, inputTokens, policy.CompactAfterInputTokens,
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

// WarnInputTokensThreshold returns the input token count that triggers a warning log.
func WarnInputTokensThreshold(policy LifecyclePolicy) int {
	if policy.RotateAfterInputTokens > 0 && policy.RotateAfterInputTokens < DefaultWarnInputTokens {
		// Warn slightly before rotate when rotate threshold is below default warn.
		warn := policy.RotateAfterInputTokens - 50_000
		if warn < policy.CompactAfterInputTokens {
			return policy.CompactAfterInputTokens
		}
		return warn
	}
	return DefaultWarnInputTokens
}

func largeTurnsBeforeCompact(policy LifecyclePolicy) int {
	_ = policy
	return DefaultLargeTurnsBeforeCompact
}

func tokensReduced(previous, current int) bool {
	if previous <= 0 || current >= previous {
		return false
	}
	drop := previous - current
	minDrop := previous * tokenReductionMinPercent / 100
	if minDrop < 1 {
		minDrop = 1
	}
	return drop >= minDrop
}