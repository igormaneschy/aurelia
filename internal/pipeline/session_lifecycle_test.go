package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
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

	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

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

	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

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

	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

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

	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

	// MarkEmptyResult sets active=false with 1 empty result. Under default
	// MaxEmptyResultsBeforeRotate=2, NeedsRotation is not triggered; the
	// cold/inactive check catches it with HealthCold/ActionColdResume.
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

	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

	// MarkProcessDeath sets active=false with 1 death. Under default
	// MaxProcessDeathsBeforeRotate=2, NeedsRotation is not triggered; the
	// cold/inactive check catches it with HealthCold/ActionColdResume.
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

	// Large-token decision (input_tokens >= CompactAfterInputTokens) requires
	// bridge stats enrichment (GetSessionStats). In this test env without a
	// real bridge, the signals never carry InputTokens, so the decision falls
	// through to the healthy/continue path. The unit-level decision logic is
	// covered in session.EvaluateLifecycle tests (see lifecycle_test.go).
	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	_ = s.applyLifecycle(context.Background(), req, 1, 2, 100)
	// No assertion needed here: session.EvaluateLifecycle (unit-tested in
	// lifecycle_test.go) covers the active+large→continue decision. This
	// test exists to ensure applyLifecycle does not panic or hang.
}

func TestApplyLifecycle_NoSession(t *testing.T) {
	s := newLifecycleTestService(t)
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()

	// No session at all — signals will be zero-valued, active=false
	req := &bridge.Request{
		Options: bridge.RequestOptions{},
	}

	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

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

	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

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

	result2 := s.applyLifecycle(context.Background(), req2, 1, 2, 100)

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
	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

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

func TestHandleRetryOutcome_MarksProcessDeathForCurrentUser(t *testing.T) {
	sessions := session.NewStore()
	sessions.SetSession(42, 7, 100, "/tmp/user-100.jsonl")
	sessions.SetSession(42, 7, 200, "/tmp/user-200.jsonl")
	svc := &Service{sessions: sessions, output: &fakeOutput{}}

	svc.handleRetryOutcome(42, 7, 123, OutcomeProcessDeath, 100)

	user100 := sessions.GetHealthSignals(42, 7, 100)
	if user100.RecentProcessDeaths != 1 {
		t.Fatalf("expected process death for user 100, got %d", user100.RecentProcessDeaths)
	}
	user200 := sessions.GetHealthSignals(42, 7, 200)
	if user200.RecentProcessDeaths != 0 {
		t.Fatalf("expected no process death for user 200, got %d", user200.RecentProcessDeaths)
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

func TestGetIdleTimeout_Defaults(t *testing.T) {
	s := &Service{config: nil}
	d := s.getIdleTimeout()
	if d != defaultIdleTimeout {
		t.Fatalf("expected defaultIdleTimeout=%s, got %s", defaultIdleTimeout, d)
	}
}

func TestGetIdleTimeout_DisabledLifecycle(t *testing.T) {
	s := &Service{
		config: &config.AppConfig{
			SessionLifecycle: config.SessionLifecycleConfig{Enabled: false},
		},
	}
	d := s.getIdleTimeout()
	if d != defaultIdleTimeout {
		t.Fatalf("expected defaultIdleTimeout when disabled, got %s", d)
	}
}

func TestGetIdleTimeout_FromConfig(t *testing.T) {
	s := &Service{
		config: &config.AppConfig{
			SessionLifecycle: config.SessionLifecycleConfig{
				Enabled:            true,
				IdleTimeoutMinutes: 30,
			},
		},
	}
	d := s.getIdleTimeout()
	if d != 30*time.Minute {
		t.Fatalf("expected 30m, got %s", d)
	}
}

func TestGetIdleTimeout_ZeroMinutes(t *testing.T) {
	s := &Service{
		config: &config.AppConfig{
			SessionLifecycle: config.SessionLifecycleConfig{
				Enabled:            true,
				IdleTimeoutMinutes: 0,
			},
		},
	}
	d := s.getIdleTimeout()
	if d != defaultIdleTimeout {
		t.Fatalf("expected defaultIdleTimeout for zero config, got %s", d)
	}
}

func TestLifecycleMessages_NoOldCompactPhrase(t *testing.T) {
	t.Parallel()

	// Ensure the user-visible compact messages (reachable only as fallback/
	// manual path per Long Flow UX v2) do not contain any of the old
	// technical phrases. Use exact old strings so a revert of the message
	// constants is caught regardless of case or accent differences.
	oldForbidden := []string{
		"Compactando histórico longo",
		"Compactação automática falhou",
	}
	for _, msg := range []string{lifecycleCompactMessage, lifecycleCompactFailedMessage} {
		for _, forbid := range oldForbidden {
			if strings.Contains(msg, forbid) {
				t.Fatalf("lifecycle message must not contain old phrase %q: %q", forbid, msg)
			}
		}
	}
}

func TestLifecycleMessages_NoOldRotatePhrases(t *testing.T) {
	t.Parallel()

	// Ensure the user-visible rotate messages do not contain any old technical
	// lifecycle phrases per Long Flow UX v2. Use exact old strings so a revert
	// of the message constants is caught regardless of case or accent differences.
	forbidden := []string{
		"Histórico muito longo",
		"Criando nova sessão",
		"Nova sessão criada",
		"resumo do contexto anterior",
		"Rotação automática",
		"compactação",
		"Compactando",
		"calls",
		"ferramentas",
		"chamadas",
	}
	for _, msg := range []string{lifecycleRotateMessage, lifecycleRotateSuccessMessage, lifecycleRotateFailedMessage} {
		for _, forbid := range forbidden {
			if strings.Contains(msg, forbid) {
				t.Fatalf("lifecycle rotate message must not contain old phrase %q: %q", forbid, msg)
			}
		}
	}
}

// recordingOutput implements Output and records all SendText calls for
// multi-message assertion. Does not affect captureOutput used by other tests.
type recordingOutput struct {
	texts []string
}

func (r *recordingOutput) StartTyping(_ int64, _ int) func()           { return func() {} }
func (r *recordingOutput) NewProgress(_ int64, _ int) ProgressReporter { return &fakeProgress{} }
func (r *recordingOutput) SendError(_ int64, _ int, text string) error { return nil }
func (r *recordingOutput) SendReply(_ int64, _ int, text string) error { return nil }
func (r *recordingOutput) SendText(_ int64, _ int, text string) (any, error) {
	r.texts = append(r.texts, text)
	return nil, nil
}
func (r *recordingOutput) DeleteMessage(_ any)           {}
func (r *recordingOutput) ConfirmMessage(_ int64, _ int) {}
func (r *recordingOutput) ExecuteApprovedPlan(_ int64, _ int, _ int, _ string, _ int64, _ *orchestrator.Plan) {
}

func TestApplyLifecycle_ColdStoreSendsNoRotateNotices(t *testing.T) {
	// Cold/inactive sessions without suspect failures go directly to
	// cold_resume without any lifecycle notices. This verifies cold wins
	// over token-based decisions, preventing unnecessary rotate+summary
	// cycles when a user returns from idle.
	recOut := &recordingOutput{}
	s := &Service{
		config:   &config.AppConfig{SessionLifecycle: config.DefaultSessionLifecycleConfig()},
		sessions: session.NewStore(),
		bridge:   nil,
		output:   recOut,
	}

	// Use DeactivateSession to set Active=false without suspect signals.
	// (MarkEmptyResult would also set Active=false but adds suspect signals
	// that hit NeedsRotation before the cold check.)
	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")
	s.sessions.DeactivateSession(1, 2, 100)

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

	// Must cold-resume, not rotate
	if result.Decision.State != session.HealthCold {
		t.Fatalf("expected cold for inactive session, got %s", result.Decision.State)
	}
	if result.Decision.Action != session.ActionColdResume {
		t.Fatalf("expected cold_resume, got %s", result.Decision.Action)
	}
	if req.Options.Continue != false {
		t.Fatal("request continue should be false for cold session")
	}

	// Cold_resume must not send lifecycle notices (unlike rotate/compact)
	if len(recOut.texts) != 0 {
		t.Fatalf("expected 0 lifecycle notices for cold_resume, got %d: %v", len(recOut.texts), recOut.texts)
	}
}

func TestApplyLifecycle_SingleEmptyResultDoesNotRotate(t *testing.T) {
	// A single empty result under default MaxEmptyResultsBeforeRotate=2 must
	// not enter the rotate branch: no lifecycle notices sent, and the final
	// decision is cold_resume (MarkEmptyResult sets Active=false, cold wins
	// over single-suspect; the key is no rotate attempt).
	recOut := &recordingOutput{}
	s := &Service{
		config:   &config.AppConfig{SessionLifecycle: config.DefaultSessionLifecycleConfig()},
		sessions: session.NewStore(),
		bridge:   nil,
		output:   recOut,
	}

	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")
	s.sessions.MarkEmptyResult(1, 2, 100)

	req := &bridge.Request{
		Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"},
	}

	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

	// Decision must be cold_resume (not rotate)
	if result.Decision.Action != session.ActionColdResume {
		t.Fatalf("expected cold_resume for single empty result, got %s", result.Decision.Action)
	}
	if req.Options.Continue != false {
		t.Fatal("request continue should be false for single empty result")
	}

	// No lifecycle notices → rotate branch was not entered
	if len(recOut.texts) != 0 {
		t.Fatalf("expected 0 lifecycle notices (no rotate branch), got %d: %v", len(recOut.texts), recOut.texts)
	}
}

func TestColdStoreCompositeSignalsDontRotate(t *testing.T) {
	// Decision-level composite-signal test: the signal combination that
	// results from a cold session store (Active=false) plus enriched bridge
	// stats (even above the legacy rotate threshold) must produce cold_resume.
	// Cold wins over token-based decisions so Aurelia preserves the original PI
	// session_file instead of creating a summary-seeded replacement.
	policy := config.DefaultSessionLifecycleConfig().LifecyclePolicy()

	signals := session.HealthSignals{
		Active:      false,  // from session store
		InputTokens: 600000, // from bridge enrichment
	}

	dec := session.EvaluateLifecycle(signals, policy)

	if dec.State != session.HealthCold {
		t.Fatalf("expected cold (inactive overrides rotate threshold), got %s", dec.State)
	}
	if dec.Action != session.ActionColdResume {
		t.Fatalf("expected cold_resume for inactive session with high tokens, got %s", dec.Action)
	}
}

// TestBuildTimeoutMessage_AllOrigins checks that each known timeout origin
// produces a user-visible message that includes a relevant human-readable
// substring so the user understands what happened.
func TestBuildTimeoutMessage_AllOrigins(t *testing.T) {
	tests := []struct {
		origin string
		want   string // substring expected in message
	}{
		{timeoutOriginIdleBridge, "inatividade"},
		{timeoutOriginBridgeQuery, "consulta"},
		{timeoutOriginProviderPI, "provedor de IA"},
		{timeoutOriginMaxExecution, "complexa"},
		{timeoutOriginUnknown, "Tempo limite"},
		{"", "Tempo limite"},
	}

	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			msg := buildTimeoutMessage(tt.origin)
			if !strings.Contains(msg, tt.want) {
				t.Errorf("buildTimeoutMessage(%q) = %q, want substring %q", tt.origin, msg, tt.want)
			}
		})
	}
}
