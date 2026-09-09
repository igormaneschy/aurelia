package pipeline

import (
	"testing"

	"github.com/igormaneschy/aurelia/internal/session"
)

// TestLongSessionAttentionThreshold pins the attention threshold: 60% of the
// compaction threshold, lowered to the warn threshold when that is smaller, and
// disabled (0) when compaction is not configured.
func TestLongSessionAttentionThreshold(t *testing.T) {
	cases := []struct {
		name   string
		policy session.LifecyclePolicy
		want   int
	}{
		{
			name:   "default: 60% of compact_after",
			policy: session.LifecyclePolicy{CompactAfterInputTokens: 200_000, RotateAfterInputTokens: 500_000},
			want:   120_000,
		},
		{
			name:   "warn threshold below 60% wins",
			policy: session.LifecyclePolicy{CompactAfterInputTokens: 1_000_000, RotateAfterInputTokens: 2_000_000},
			want:   500_000,
		},
		{
			name:   "disabled without compact threshold",
			policy: session.LifecyclePolicy{},
			want:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := longSessionAttentionThreshold(tc.policy); got != tc.want {
				t.Fatalf("longSessionAttentionThreshold() = %d, want %d", got, tc.want)
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

	s.maybeNudgeLongSession(1, 0, 100, 121_000)
	if output.sentTextCount() != 1 {
		t.Fatalf("first nudge sentTextCount = %d, want 1", output.sentTextCount())
	}

	// Later turns above the threshold must not repeat the nudge.
	s.maybeNudgeLongSession(1, 0, 100, 130_000)
	s.maybeNudgeLongSession(1, 0, 100, 180_000)
	if output.sentTextCount() != 1 {
		t.Fatalf("repeated nudge sentTextCount = %d, want 1", output.sentTextCount())
	}

	// A new session file re-arms the one-shot nudge.
	sessions.SetSession(1, 0, 100, "/tmp/session-b.jsonl")
	s.maybeNudgeLongSession(1, 0, 100, 125_000)
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
	s.maybeNudgeLongSession(1, 0, 999, 200_000)
	if output.sentTextCount() != 0 {
		t.Fatalf("nudge without a stored session sentTextCount = %d, want 0", output.sentTextCount())
	}

	// Nil dependencies must be safe.
	(&Service{}).maybeNudgeLongSession(1, 0, 100, 200_000)
	(&Service{output: output}).maybeNudgeLongSession(1, 0, 100, 200_000)
}
