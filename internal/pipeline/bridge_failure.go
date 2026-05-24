package pipeline

import (
	"sync"
	"time"
)

// Outcome represents the result of processing bridge events.
type Outcome int

const (
	OutcomeSuccess      Outcome = iota // terminal "result" event
	OutcomeLLMError                    // terminal "error" event
	OutcomeProcessDeath                // channel closed without terminal event
	OutcomeCanceled                    // user canceled the active run
	OutcomeTimeout                     // run exceeded its deadline
)

const (
	bridgeEmptyResultMessage = "Nao consegui gerar uma resposta util desta vez. Tente reenviar ou reformular."
)

const (
	failureWindowMax = 3                // max failures before cooldown
	failureWindowDur = 1 * time.Minute  // window to count failures
	cooldownDuration = 30 * time.Second // cooldown period after max failures
)

// FailureTracker tracks consecutive bridge failures to implement cooldown.
type FailureTracker struct {
	mu       sync.Mutex
	failures []time.Time // timestamps of recent failures
}

// record adds a failure timestamp and returns true if in cooldown.
func (t *FailureTracker) record() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.failures = append(t.failures, now)

	// Trim failures outside the window
	cutoff := now.Add(-failureWindowDur)
	start := 0
	for start < len(t.failures) && t.failures[start].Before(cutoff) {
		start++
	}
	t.failures = append([]time.Time(nil), t.failures[start:]...)

	return len(t.failures) >= failureWindowMax
}

// inCooldown returns true if we're in cooldown (recent failures >= max).
func (t *FailureTracker) inCooldown() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.failures) < failureWindowMax {
		return false
	}

	// In cooldown if last failure was within cooldown duration
	last := t.failures[len(t.failures)-1]
	return time.Since(last) < cooldownDuration
}

func (t *FailureTracker) cooldownRemaining() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.failures) < failureWindowMax {
		return 0
	}
	remaining := cooldownDuration - time.Since(t.failures[len(t.failures)-1])
	if remaining < 0 {
		return 0
	}
	return remaining
}

// reset clears the failure history after a successful execution.
func (t *FailureTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures = t.failures[:0]
}
