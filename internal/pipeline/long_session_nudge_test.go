package pipeline

import (
	"testing"

	"github.com/igormaneschy/aurelia/internal/session"
)

// TestLongSessionAttentionPct pins the attention threshold: the configured
// context-usage percentage of the model window (0 = disabled).
func TestLongSessionAttentionPct(t *testing.T) {
	cases := []struct {
		name   string
		policy session.LifecyclePolicy
		want   int
	}{
		{
			name:   "default warn percentage",
			policy: session.LifecyclePolicy{WarnContextPct: 70},
			want:   70,
		},
		{
			name:   "custom percentage",
			policy: session.LifecyclePolicy{WarnContextPct: 55},
			want:   55,
		},
		{
			name:   "disabled",
			policy: session.LifecyclePolicy{},
			want:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := longSessionAttentionPct(tc.policy); got != tc.want {
				t.Fatalf("longSessionAttentionPct() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestMaybeNudgeLongSession_OneShotPerSession covers A8: the nudge fires once
// per session, is not repeated on later turns, and is re-armed only when the
// session file changes (new session).
func TestMaybeNudgeLongSession_OneShotPerSession(t *testing.T) {
	output := &fakeOutput{}
	sessions := session.NewStore()
	sessions.SetSession(1, 0, 100, "/tmp/session-a.jsonl")
	s := &Service{output: output, sessions: sessions}

	s.maybeNudgeLongSession(1, 0, 100, 90_000, 1_000_000, 72)
	if output.sentTextCount() != 1 {
		t.Fatalf("first nudge sentTextCount = %d, want 1", output.sentTextCount())
	}

	// Later turns above the threshold must not repeat the nudge.
	s.maybeNudgeLongSession(1, 0, 100, 95_000, 1_000_000, 75)
	s.maybeNudgeLongSession(1, 0, 100, 120_000, 1_000_000, 80)
	if output.sentTextCount() != 1 {
		t.Fatalf("repeated nudge sentTextCount = %d, want 1", output.sentTextCount())
	}

	// A new session file re-arms the one-shot nudge.
	sessions.SetSession(1, 0, 100, "/tmp/session-b.jsonl")
	s.maybeNudgeLongSession(1, 0, 100, 91_000, 1_000_000, 71)
	if output.sentTextCount() != 2 {
		t.Fatalf("nudge after new session sentTextCount = %d, want 2", output.sentTextCount())
	}
}

// TestMaybeNudgeLongSession_FailsClosedWithoutSession covers the degraded
// paths: no session store, no output, or an unknown conversation never panics
// and never claims the nudge.
func TestMaybeNudgeLongSession_FailsClosedWithoutSession(t *testing.T) {
	// Unknown conversation (store has no entry for this user).
	output := &fakeOutput{}
	sessions := session.NewStore()
	s := &Service{output: output, sessions: sessions}
	s.maybeNudgeLongSession(1, 0, 999, 90_000, 1_000_000, 72)
	if output.sentTextCount() != 0 {
		t.Fatalf("nudge without a stored session sentTextCount = %d, want 0", output.sentTextCount())
	}

	// Nil dependencies must be safe.
	(&Service{}).maybeNudgeLongSession(1, 0, 100, 90_000, 1_000_000, 72)
	(&Service{output: output}).maybeNudgeLongSession(1, 0, 100, 90_000, 1_000_000, 72)
}
