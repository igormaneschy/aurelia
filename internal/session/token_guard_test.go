package session

import "testing"

func TestTokenGuard_NoEscalationBelowWarnContext(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	dec, escalated := g.Evaluate(key, 50, policy) // 50% < warn 70%
	if escalated {
		t.Fatalf("unexpected escalation: %v", dec)
	}
}

func TestTokenGuard_UnknownContextNeverEscalates(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	for _, pct := range []float64{0, -1} {
		if dec, escalated := g.Evaluate(key, pct, policy); escalated {
			t.Fatalf("pct=%v: unexpected escalation: %v", pct, dec)
		}
	}
}

func TestTokenGuard_ImmediateRotateAtEmergencyCeiling(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	dec, escalated := g.Evaluate(key, 96, policy) // >= 95%
	if !escalated {
		t.Fatal("expected rotate escalation at emergency_rotate_context_pct")
	}
	if dec.Action != ActionRotate {
		t.Fatalf("expected rotate, got %s", dec.Action)
	}
	if dec.State != HealthDangerous {
		t.Fatalf("expected dangerous, got %s", dec.State)
	}
}

func TestTokenGuard_CompactAfterStallReadings(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	pcts := []float64{75, 80, 85}
	for i, pct := range pcts {
		dec, escalated := g.Evaluate(key, pct, policy)
		if i < len(pcts)-1 {
			if escalated {
				t.Fatalf("reading %d: unexpected escalation at %.0f%%", i+1, pct)
			}
			continue
		}
		if !escalated {
			t.Fatal("expected compact after 3 stall readings")
		}
		if dec.Action != ActionCompact {
			t.Fatalf("expected compact, got %s", dec.Action)
		}
	}
}

func TestTokenGuard_ResetOnMeaningfulContextReduction(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	g.Evaluate(key, 75, policy)
	g.Evaluate(key, 80, policy)
	// 25% drop — PI compacted successfully.
	if dec, escalated := g.Evaluate(key, 60, policy); escalated {
		t.Fatalf("unexpected escalation after reduction: %v", dec)
	}

	// Need 3 fresh stall readings after the reduction.
	for _, pct := range []float64{72, 74} {
		if _, escalated := g.Evaluate(key, pct, policy); escalated {
			t.Fatalf("unexpected escalation at %.0f%%", pct)
		}
	}
	dec, escalated := g.Evaluate(key, 76, policy)
	if !escalated || dec.Action != ActionCompact {
		t.Fatalf("expected compact after new stall cycle, got escalated=%v dec=%v", escalated, dec)
	}
}

func TestTokenGuard_ResetClearsState(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	g.Evaluate(key, 75, policy)
	g.Evaluate(key, 80, policy)
	g.Reset(key)

	if dec, escalated := g.Evaluate(key, 85, policy); escalated {
		t.Fatalf("expected no escalation after reset, got %v", dec)
	}
}

func TestTokenGuard_DropsBelowWarnResets(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	g.Evaluate(key, 75, policy)
	g.Evaluate(key, 80, policy)
	if _, escalated := g.Evaluate(key, 50, policy); escalated {
		t.Fatal("unexpected escalation when below warn threshold")
	}
}

func TestContextReduced(t *testing.T) {
	if !contextReduced(80, 60) {
		t.Fatal("expected 25% drop to count as reduced")
	}
	if contextReduced(80, 78) {
		t.Fatal("2.5% drop should not count as reduced")
	}
	if contextReduced(80, 80) {
		t.Fatal("flat context should not count as reduced")
	}
	if contextReduced(0, 50) {
		t.Fatal("zero previous should not count as reduced")
	}
}
