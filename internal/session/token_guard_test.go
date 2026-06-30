package session

import "testing"

func TestTokenGuard_NoEscalationBelowCompact(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	dec, escalated := g.Evaluate(key, 150_000, policy)
	if escalated {
		t.Fatalf("unexpected escalation: %v", dec)
	}
}

func TestTokenGuard_ImmediateRotateAtCeiling(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	dec, escalated := g.Evaluate(key, 550_000, policy)
	if !escalated {
		t.Fatal("expected rotate escalation at rotate_after")
	}
	if dec.Action != ActionRotate {
		t.Fatalf("expected rotate, got %s", dec.Action)
	}
	if dec.State != HealthDangerous {
		t.Fatalf("expected dangerous, got %s", dec.State)
	}
}

func TestTokenGuard_CompactAfterStallTurns(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	tokens := []int{250_000, 300_000, 350_000}
	for i, tok := range tokens {
		dec, escalated := g.Evaluate(key, tok, policy)
		if i < len(tokens)-1 {
			if escalated {
				t.Fatalf("turn %d: unexpected escalation at %d tokens", i+1, tok)
			}
			continue
		}
		if !escalated {
			t.Fatal("expected compact after 3 stall turns")
		}
		if dec.Action != ActionCompact {
			t.Fatalf("expected compact, got %s", dec.Action)
		}
	}
}

func TestTokenGuard_ResetOnMeaningfulReduction(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	g.Evaluate(key, 250_000, policy)
	g.Evaluate(key, 300_000, policy)
	// 20% drop — PI compacted successfully.
	dec, escalated := g.Evaluate(key, 240_000, policy)
	if escalated {
		t.Fatalf("unexpected escalation after reduction: %v", dec)
	}

	// Need 3 fresh stall turns after reduction.
	for _, tok := range []int{260_000, 280_000} {
		if _, escalated := g.Evaluate(key, tok, policy); escalated {
			t.Fatalf("unexpected escalation at %d", tok)
		}
	}
	dec, escalated = g.Evaluate(key, 300_000, policy)
	if !escalated || dec.Action != ActionCompact {
		t.Fatalf("expected compact after new stall cycle, got escalated=%v dec=%v", escalated, dec)
	}
}

func TestTokenGuard_ResetClearsState(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	g.Evaluate(key, 250_000, policy)
	g.Evaluate(key, 300_000, policy)
	g.Reset(key)

	dec, escalated := g.Evaluate(key, 350_000, policy)
	if escalated {
		t.Fatalf("expected no escalation after reset, got %v", dec)
	}
}

func TestTokenGuard_DropsBelowCompactResets(t *testing.T) {
	g := NewTokenGuard()
	key := SessionKey{ChatID: 1, ThreadID: 2, UserID: 100}
	policy := DefaultLifecyclePolicy()

	g.Evaluate(key, 250_000, policy)
	g.Evaluate(key, 300_000, policy)
	if _, escalated := g.Evaluate(key, 180_000, policy); escalated {
		t.Fatal("unexpected escalation when below compact threshold")
	}
}

func TestTokensReduced(t *testing.T) {
	if !tokensReduced(300_000, 250_000) {
		t.Fatal("expected 16% drop to count as reduced")
	}
	if tokensReduced(300_000, 290_000) {
		t.Fatal("3% drop should not count as reduced")
	}
	if tokensReduced(300_000, 300_000) {
		t.Fatal("flat tokens should not count as reduced")
	}
}

func TestWarnInputTokensThreshold(t *testing.T) {
	policy := DefaultLifecyclePolicy()
	if WarnInputTokensThreshold(policy) != DefaultWarnInputTokens {
		t.Fatalf("expected default warn %d", DefaultWarnInputTokens)
	}
}