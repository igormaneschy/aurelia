package pipeline

import (
	"context"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/session"
)

// TestApplyLifecycle_SharedTokenGuardAcrossPipelineInstances verifies that two
// per-send pipeline instances (TUI pattern) retain stall-turn state when they
// share one TokenGuard injected via Config.
func TestApplyLifecycle_SharedTokenGuardAcrossPipelineInstances(t *testing.T) {
	sharedGuard := session.NewTokenGuard()
	lc := config.DefaultSessionLifecycleConfig()

	newSendPipeline := func() *Service {
		s := newLifecycleTestService(t)
		s.config.SessionLifecycle = lc
		s.tokenGuard = sharedGuard
		s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")
		s.testCompactSession = func(_ context.Context, _ int64, _ int, _ int64, _ bridge.RequestOptions) (*bridge.CompactSessionResult, error) {
			return &bridge.CompactSessionResult{Success: true, SessionFile: "/tmp/test.jsonl", TokensBefore: 350_000}, nil
		}
		return s
	}

	inputs := []int{250_000, 300_000, 350_000}
	call := 0
	statsHook := func(_ context.Context, _ bridge.RequestOptions) (*bridge.SessionStats, error) {
		stats := &bridge.SessionStats{InputTokens: inputs[call]}
		if call < len(inputs)-1 {
			call++
		}
		return stats, nil
	}

	req := &bridge.Request{Command: "query", Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"}}

	for i := 0; i < len(inputs)-1; i++ {
		s := newSendPipeline()
		s.testSessionStats = statsHook
		result := s.applyLifecycle(context.Background(), req, 1, 2, 100)
		if result.Decision.Action != session.ActionContinue {
			t.Fatalf("turn %d: expected continue, got %s (%s)", i+1, result.Decision.Action, result.Decision.Reason)
		}
	}

	final := newSendPipeline()
	final.testSessionStats = statsHook
	result := final.applyLifecycle(context.Background(), req, 1, 2, 100)
	if result.Decision.Action != session.ActionCompact {
		t.Fatalf("expected compact on 3rd stall turn across shared guard, got %s (%s)", result.Decision.Action, result.Decision.Reason)
	}
}
