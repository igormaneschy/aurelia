package pipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/runlog"
)

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
