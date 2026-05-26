package pipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/runlog"
)

// captureOutput implements Output and captures the last SendText text for assertions.
type captureOutput struct {
	lastText      string
	confirmCalled bool
}

func (c *captureOutput) StartTyping(_ int64, _ int) func() { return func() {} }
func (c *captureOutput) NewProgress(_ int64, _ int) ProgressReporter { return &fakeProgress{} }
func (c *captureOutput) SendError(_ int64, _ int, text string) error { return nil }
func (c *captureOutput) SendReply(_ int64, _ int, text string) error { return nil }
func (c *captureOutput) SendText(_ int64, _ int, text string) (any, error) {
	c.lastText = text
	return nil, nil
}
func (c *captureOutput) DeleteMessage(_ any)                              {}
func (c *captureOutput) ConfirmMessage(_ int64, _ int)                    { c.confirmCalled = true }
func (c *captureOutput) ExecuteApprovedPlan(_ int64, _ int, _ int, _ string, _ int64, _ *orchestrator.Plan) {}

func TestRunTimeoutTrackerKeepsFirstOrigin(t *testing.T) {
	t.Parallel()

	origin, elapsed := timeoutDetails()
	if origin != timeoutOriginUnknown || elapsed != 0 {
		t.Fatalf("empty timeoutDetails = %q/%s", origin, elapsed)
	}

	tracker := newRunTimeoutTracker()
	tracker.mark(timeoutOriginIdleBridge)
	tracker.mark(timeoutOriginMaxExecution)
	origin, elapsed = timeoutDetails(tracker)
	if origin != timeoutOriginIdleBridge {
		t.Fatalf("origin = %q, want %q", origin, timeoutOriginIdleBridge)
	}
	if elapsed < 0 {
		t.Fatalf("elapsed should not be negative: %s", elapsed)
	}
}

func TestSessionUserIDFallback(t *testing.T) {
	t.Parallel()

	if got := sessionUserID(); got != 0 {
		t.Fatalf("sessionUserID() = %d, want 0", got)
	}
	if got := sessionUserID(123); got != 123 {
		t.Fatalf("sessionUserID(123) = %d, want 123", got)
	}
}

func TestClassifyBridgeErrorOutcomeTimeoutOrigins(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		message    string
		wantStatus string
		wantRun    runlog.RunStatus
		wantReason string
	}{
		{"bridge query", "query timeout: no result after 30 minutes", "timed_out", runlog.RunTimedOut, timeoutOriginBridgeQuery},
		{"provider", "upstream timed out waiting for model", "timed_out", runlog.RunTimedOut, timeoutOriginProviderPI},
		{"normal", "rate limit exceeded", "failed", runlog.RunFailed, "rate limit exceeded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, runStatus, reason := classifyBridgeErrorOutcome(tc.message)
			if status != tc.wantStatus || runStatus != tc.wantRun || reason != tc.wantReason {
				t.Fatalf("got %q/%s/%q, want %q/%s/%q", status, runStatus, reason, tc.wantStatus, tc.wantRun, tc.wantReason)
			}
		})
	}
}

// ── Tool tracker UX tests ──────────────────────────────────────────────

func TestToolCallTracker_WarningMessageOmitsTechnicalTerms(t *testing.T) {
	capOut := &captureOutput{}
	var steers []string
	tracker := newToolCallTracker(1, 2, capOut, func(s string) { steers = append(steers, s) })

	// Trigger warning threshold
	for i := 0; i < toolCallWarningThreshold; i++ {
		tracker.increment("Read")
	}

	if capOut.lastText == "" {
		t.Fatal("expected a user-facing warning message")
	}

	// Must not contain technical terms
	for _, forbid := range []string{"calls", "ferramentas", "chamadas", "Read", "20"} {
		if strings.Contains(capOut.lastText, forbid) {
			t.Fatalf("user message must not contain %q: %q", forbid, capOut.lastText)
		}
	}

	// Must contain human progress language
	if !strings.Contains(capOut.lastText, "consolidar") {
		t.Fatalf("user message should mention consolidation, got: %q", capOut.lastText)
	}

	// Steer must still contain technical details
	if len(steers) == 0 {
		t.Fatal("expected at least one steer command")
	}
	if !strings.Contains(steers[0], "ferramentas") {
		t.Fatalf("steer should contain technical terms, got: %q", steers[0])
	}
}

func TestToolCallTracker_CriticalMessageOmitsTechnicalTerms(t *testing.T) {
	capOut := &captureOutput{}
	var steers []string
	tracker := newToolCallTracker(1, 2, capOut, func(s string) { steers = append(steers, s) })

	// Trigger critical threshold
	for i := 0; i < toolCallCriticalThreshold; i++ {
		tracker.increment("Read")
	}

	if capOut.lastText == "" {
		t.Fatal("expected a user-facing critical message")
	}

	// Must not contain technical terms
	for _, forbid := range []string{"calls", "ferramentas", "chamadas", "Read", "50"} {
		if strings.Contains(capOut.lastText, forbid) {
			t.Fatalf("user message must not contain %q: %q", forbid, capOut.lastText)
		}
	}

	// Must contain human progress language
	if !strings.Contains(capOut.lastText, "consolidar") {
		t.Fatalf("user message should mention consolidation, got: %q", capOut.lastText)
	}
}

func TestToolCallTracker_EveryNMessageOmitsTechnicalTerms(t *testing.T) {
	capOut := &captureOutput{}
	tracker := newToolCallTracker(1, 2, capOut, func(s string) {})

	// Trigger twice the critical threshold to get the every-N message
	for i := 0; i < toolCallCriticalThreshold*2; i++ {
		tracker.increment("Read")
	}

	if capOut.lastText == "" {
		t.Fatal("expected a user-facing every-N message")
	}

	for _, forbid := range []string{"calls", "ferramentas", "chamadas", "Read", "100"} {
		if strings.Contains(capOut.lastText, forbid) {
			t.Fatalf("user message must not contain %q: %q", forbid, capOut.lastText)
		}
	}
}

// ── Loop detector UX tests ─────────────────────────────────────────────

func TestLoopDetector_UserMessageIsNeutral(t *testing.T) {
	capOut := &captureOutput{}
	var steers []string
	detector := newLoopDetector(1, 2, capOut, func(s string) { steers = append(steers, s) })

	// Simulate a repetitive consecutive-repeats pattern (need 4+ fills
	// before detectLocked checks patterns, and 3 consecutive repeats).
	for i := 0; i < loopRepeatThreshold+1; i++ {
		detector.record("Read", map[string]any{"path": "/foo/bar"})
	}

	if capOut.lastText == "" {
		t.Fatal("expected a loop detection warning")
	}

	// Must not contain imperative steering language
	for _, forbid := range []string{"Pare", "pare", "PARE"} {
		if strings.Contains(capOut.lastText, forbid) {
			t.Fatalf("user message must not contain %q: %q", forbid, capOut.lastText)
		}
	}

	// Must contain neutral wording
	if !strings.Contains(capOut.lastText, "consolidar") {
		t.Fatalf("user message should mention consolidation, got: %q", capOut.lastText)
	}

	// Steer must still contain imperative stop instruction
	if len(steers) == 0 {
		t.Fatal("expected at least one steer command")
	}
	if !strings.Contains(steers[0], "Pare") {
		t.Fatalf("steer should contain imperative 'Pare', got: %q", steers[0])
	}
}

// ── Heartbeat UX tests ─────────────────────────────────────────────────

func TestHeartbeatMessage_OmitsChamadasDeFerramenta(t *testing.T) {
	// Use the production helper buildHeartbeatMessage so the test exercises
	// real code rather than duplicating format strings.
	msg := buildHeartbeatMessage(30*time.Second, heartbeatToolThreshold, 35)

	if strings.Contains(msg, "chamadas de ferramenta") {
		t.Fatalf("heartbeat message must not contain 'chamadas de ferramenta': %q", msg)
	}
	if strings.Contains(msg, "calls") {
		t.Fatalf("heartbeat message must not contain 'calls': %q", msg)
	}
	if !strings.Contains(msg, "consolidar") {
		t.Fatalf("heartbeat message should mention consolidation: %q", msg)
	}
}

func TestHeartbeatMessage_NormalModeOmitsToolCounts(t *testing.T) {
	// Normal heartbeat without hitting the every-N threshold
	msg := buildHeartbeatMessage(30*time.Second, 1, 35)

	if strings.Contains(msg, "chamadas de ferramenta") {
		t.Fatalf("normal heartbeat must not contain 'chamadas de ferramenta': %q", msg)
	}
	if strings.Contains(msg, "ferramenta") {
		t.Fatalf("normal heartbeat must not contain 'ferramenta': %q", msg)
	}
	if strings.Contains(msg, "35") {
		t.Fatalf("normal heartbeat must not contain raw count: %q", msg)
	}
}

func TestBuildHeartbeatMessage_EveryNConsolidationVariant(t *testing.T) {
	// Beat count multiple of threshold with tool count > 0 triggers the
	// consolidation variant. Other combinations produce the simple variant.
	cases := []struct {
		name      string
		beatCount int
		toolCount int
		wantFull  bool // true = consolidation variant, false = simple variant
	}{
		{"every-N with tools", heartbeatToolThreshold, 5, true},
		{"normal beat with tools", 1, 35, false},
		{"normal beat no tools", 1, 0, false},
		{"every-N but no tools (simple)", heartbeatToolThreshold, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := buildHeartbeatMessage(30*time.Second, tc.beatCount, tc.toolCount)
			if tc.wantFull && !strings.Contains(msg, "consolidar") {
				t.Errorf("expected consolidation variant, got: %q", msg)
			}
			if !tc.wantFull && strings.Contains(msg, "consolidar") {
				t.Errorf("expected simple variant, got: %q", msg)
			}
			if strings.Contains(msg, "chamadas de ferramenta") {
				t.Errorf("must not contain 'chamadas de ferramenta': %q", msg)
			}
		})
	}
}

func TestBridgeErrorMessagesIncludeActionableHints(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  string
		want string
	}{
		{name: "connect", msg: bridgeConnectErrorMessage, want: "/new"},
		{name: "retry", msg: bridgeRetryFailedMessage, want: "Dica"},
		{name: "timeout", msg: buildTimeoutMessage(timeoutOriginMaxExecution), want: "dividir em partes"},
		{name: "cooldown", msg: bridgeCooldownMessage(12 * time.Second), want: "~12 segundos"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(tc.msg, tc.want) {
				t.Fatalf("message %q missing %q", tc.msg, tc.want)
			}
		})
	}
}
