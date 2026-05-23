package pipeline

import (
	"context"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/session"
)

// lifecycleTestHelper creates a minimal Service with a real session store
// and config for testing lifecycle decisions.
func newLifecycleTestService(t *testing.T) *Service {
	t.Helper()
	s := &Service{
		config:   &config.AppConfig{},
		sessions: session.NewStore(),
		runLog:   nil, // disabled for test
	}
	return s
}

func TestApplyLifecycle_Disabled(t *testing.T) {
	s := newLifecycleTestService(t)
	s.config.SessionLifecycle = config.SessionLifecycleConfig{Enabled: false}

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	result := s.applyLifecycle(req, 1, 2, 100)

	if result.Decision.Action != session.ActionContinue {
		t.Fatalf("expected continue when disabled, got %s", result.Decision.Action)
	}
	if req.Options.Continue != true {
		t.Fatal("request continue should remain true when disabled")
	}
}

func TestApplyLifecycle_HealthyContinue(t *testing.T) {
	s := newLifecycleTestService(t)
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()
	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	result := s.applyLifecycle(req, 1, 2, 100)

	if result.Decision.Action != session.ActionContinue {
		t.Fatalf("expected continue for healthy session, got %s (state=%s)", result.Decision.Action, result.Decision.State)
	}
	if req.Options.Continue != true {
		t.Fatal("request continue should remain true for healthy session")
	}
}

func TestApplyLifecycle_ColdResume(t *testing.T) {
	s := newLifecycleTestService(t)
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()

	// Session exists but is inactive (cold)
	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")
	s.sessions.DeactivateSession(1, 2, 100)

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	result := s.applyLifecycle(req, 1, 2, 100)

	if result.Decision.Action != session.ActionColdResume {
		t.Fatalf("expected cold_resume for inactive session, got %s (state=%s)", result.Decision.Action, result.Decision.State)
	}
	if req.Options.Continue != false {
		t.Fatal("request continue should be false for cold resume")
	}
}

func TestApplyLifecycle_SuspectDueToEmptyResult(t *testing.T) {
	s := newLifecycleTestService(t)
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()

	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")
	s.sessions.MarkEmptyResult(1, 2, 100)

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	result := s.applyLifecycle(req, 1, 2, 100)

	// MarkEmptyResult sets active=false, so the cold priority wins.
	// The action (cold_resume) is the same for both cold and suspect.
	if result.Decision.Action != session.ActionColdResume {
		t.Fatalf("expected cold_resume after empty result, got %s (state=%s)", result.Decision.Action, result.Decision.State)
	}
	if req.Options.Continue != false {
		t.Fatal("request continue should be false after empty result")
	}
}

func TestApplyLifecycle_SuspectDueToProcessDeath(t *testing.T) {
	s := newLifecycleTestService(t)
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()

	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")
	s.sessions.MarkProcessDeath(1, 2, 100)

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	result := s.applyLifecycle(req, 1, 2, 100)

	// MarkProcessDeath sets active=false, so cold priority wins.
	// The action (cold_resume) is the same for both.
	if result.Decision.Action != session.ActionColdResume {
		t.Fatalf("expected cold_resume after process death, got %s (state=%s)", result.Decision.Action, result.Decision.State)
	}
	if req.Options.Continue != false {
		t.Fatal("request continue should be false")
	}
}

func TestApplyLifecycle_LargeTokens(t *testing.T) {
	s := newLifecycleTestService(t)
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()

	// For large state, we need compact — but compact requires a bridge.
	// Without bridge, the fallback should be cold resume.
	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")
	// Session is active, but we can't inject InputTokens into health signals
	// since they're read from the store and we can't set them directly.
	// We test the compact fallback via the bridge-less code path.

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	_ = s.applyLifecycle(req, 1, 2, 100)
	// Without a real bridge, large tokens decision can't be reached via store signals
	// (GetHealthSignals doesn't include InputTokens). This test is a placeholder
	// for when bridge stats are fully integrated.
}

func TestApplyLifecycle_NoSession(t *testing.T) {
	s := newLifecycleTestService(t)
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()

	// No session at all — signals will be zero-valued, active=false
	req := &bridge.Request{
		Options: bridge.RequestOptions{},
	}

	result := s.applyLifecycle(req, 1, 2, 100)

	if result.Decision.Action != session.ActionColdResume {
		t.Fatalf("expected cold_resume for no session, got %s (state=%s)", result.Decision.Action, result.Decision.State)
	}
}

func TestApplyLifecycle_ColdResumeThenClearFailure(t *testing.T) {
	s := newLifecycleTestService(t)
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()

	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")
	s.sessions.MarkFailure(1, 2, 100, "timeout")

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	result := s.applyLifecycle(req, 1, 2, 100)

	if result.Decision.Action != session.ActionColdResume {
		t.Fatalf("expected cold_resume after failure, got %s", result.Decision.Action)
	}
	if req.Options.Continue != false {
		t.Fatal("request continue should be false after failure")
	}

	// Now clear failure state (simulating a successful run)
	s.sessions.ClearFailureState(1, 2, 100)
	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl") // re-activate

	req2 := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	result2 := s.applyLifecycle(req2, 1, 2, 100)

	if result2.Decision.Action != session.ActionContinue {
		t.Fatalf("expected continue after clearing failure, got %s", result2.Decision.Action)
	}
}

func TestApplyLifecycle_NoSessionStore(t *testing.T) {
	s := &Service{
		config:   &config.AppConfig{SessionLifecycle: config.DefaultSessionLifecycleConfig()},
		sessions: nil,
	}

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true},
	}

	// Should not panic when sessions store is nil
	result := s.applyLifecycle(req, 1, 2, 100)

	// Without session store, signals are zero-valued with Active=false → cold
	if result.Decision.State != session.HealthCold {
		t.Fatalf("expected cold with nil store (no active signals), got %s", result.Decision.State)
	}
	if result.Decision.Action != session.ActionColdResume {
		t.Fatalf("expected cold_resume with nil store, got %s", result.Decision.Action)
	}
	// Continue should be forced to false
	if req.Options.Continue != false {
		t.Fatal("request continue should be false when lifecycle is cold")
	}
}

func TestCompactSession_NilBridge(t *testing.T) {
	s := &Service{
		bridge: nil,
	}

	_, err := s.compactSession(context.Background(), 1, 2, 100, bridge.RequestOptions{})
	if err == nil {
		t.Fatal("expected error with nil bridge")
	}
}

func TestGetLifecyclePolicy_Default(t *testing.T) {
	s := &Service{config: nil}
	policy := s.getLifecyclePolicy()
	if !policy.Enabled {
		t.Fatal("default policy should be enabled")
	}
}

func TestGetLifecyclePolicy_FromConfig(t *testing.T) {
	s := &Service{
		config: &config.AppConfig{
			SessionLifecycle: config.SessionLifecycleConfig{
				Enabled:                 true,
				CompactAfterInputTokens: 50000,
			},
		},
	}
	policy := s.getLifecyclePolicy()
	if policy.CompactAfterInputTokens != 50000 {
		t.Fatalf("expected compact_after=50000, got %d", policy.CompactAfterInputTokens)
	}
}
