package dream

import (
	"testing"
	"time"
)

func TestShouldTripBackgroundCircuit(t *testing.T) {
	trips := []string{
		"429 Too Many Requests",
		"rate limit exceeded",
		"402 Payment Required",
		"insufficient balance",
		"401 Unauthorized billing",
		"authentication failed",
	}
	for _, msg := range trips {
		if !shouldTripBackgroundCircuit(msg) {
			t.Errorf("expected trip for %q", msg)
		}
	}

	noTrip := []string{
		"",
		"model not found",
		"context length exceeded",
		"random failure",
		"timeout",
	}
	for _, msg := range noTrip {
		if shouldTripBackgroundCircuit(msg) {
			t.Errorf("expected no trip for %q", msg)
		}
	}
}

func TestBackgroundCircuit_SkipWhileOpen(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{BackgroundCircuitCooldown: time.Minute})
	d.backgroundCircuitTrip("test", "429 Too Many Requests")

	if !d.backgroundCircuitSkip("dream") {
		t.Fatal("expected circuit to block dream while open")
	}
	if !d.backgroundCircuitSkip("nudge") {
		t.Fatal("expected circuit to block nudge while open")
	}
}

func TestBackgroundCircuit_Expires(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{BackgroundCircuitCooldown: 5 * time.Millisecond})
	d.backgroundCircuitTrip("test", "402 Payment Required")

	time.Sleep(10 * time.Millisecond)
	if d.backgroundCircuitSkip("dream") {
		t.Fatal("expected circuit to close after cooldown")
	}
}

func TestBackgroundCircuit_NoTripOnBenignError(t *testing.T) {
	d := New(nil, nil, nil, DreamConfig{BackgroundCircuitCooldown: time.Minute})
	d.backgroundCircuitTrip("dream", "model not found")
	if d.backgroundCircuitSkip("dream") {
		t.Fatal("benign errors must not open the circuit")
	}
}

