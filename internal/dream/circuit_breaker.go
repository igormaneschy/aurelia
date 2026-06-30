package dream

import (
	"log"
	"strings"
	"time"
)

const defaultBackgroundCircuitCooldown = 30 * time.Minute

// shouldTripBackgroundCircuit reports whether a bridge/API error should pause
// dream and nudge background calls (401 auth/billing, 402 payment, 429 rate limit).
func shouldTripBackgroundCircuit(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" {
		return false
	}

	if strings.Contains(lower, "429") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "too many requests") {
		return true
	}

	if strings.Contains(lower, "402") ||
		strings.Contains(lower, "payment required") ||
		strings.Contains(lower, "insufficient balance") ||
		strings.Contains(lower, "insufficient credits") {
		return true
	}

	if strings.Contains(lower, "401") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "authentication") ||
		strings.Contains(lower, "invalid api key") ||
		strings.Contains(lower, "api key") {
		return true
	}

	return false
}

func (d *Dreamer) backgroundCircuitCooldown() time.Duration {
	if d.config.BackgroundCircuitCooldown > 0 {
		return d.config.BackgroundCircuitCooldown
	}
	return defaultBackgroundCircuitCooldown
}

// backgroundCircuitSkip returns true when dream/nudge should not call the bridge.
func (d *Dreamer) backgroundCircuitSkip(component string) bool {
	d.bgCircuitMu.Lock()
	open := !d.bgCircuitOpenUntil.IsZero() && time.Now().Before(d.bgCircuitOpenUntil)
	reason := d.bgCircuitReason
	until := d.bgCircuitOpenUntil
	d.bgCircuitMu.Unlock()

	if !open {
		return false
	}
	log.Printf("[%s] circuit_open: skipping background API call (reason=%q until=%s)",
		component, reason, until.UTC().Format(time.RFC3339))
	return true
}

func (d *Dreamer) backgroundCircuitTrip(component, msg string) {
	if !shouldTripBackgroundCircuit(msg) {
		return
	}
	cooldown := d.backgroundCircuitCooldown()
	reason := truncateCircuitReason(msg)
	until := time.Now().Add(cooldown)

	d.bgCircuitMu.Lock()
	d.bgCircuitOpenUntil = until
	d.bgCircuitReason = reason
	d.bgCircuitMu.Unlock()

	log.Printf("[%s] circuit_open: tripped for %s (reason=%q)", component, cooldown.Round(time.Second), reason)
}

func truncateCircuitReason(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) <= 200 {
		return msg
	}
	return msg[:200] + "..."
}