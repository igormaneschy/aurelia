package tui

import (
	"context"
	"time"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// Message types for Bubble Tea.

// daemonReachableMsg is sent when the daemon is reachable.
type daemonReachableMsg struct {
	latency time.Duration
}

// daemonUnreachableMsg is sent when the daemon is not reachable.
type daemonUnreachableMsg struct {
	err error
}

// daemonErrorMsg is sent when the daemon returns an error.
type daemonErrorMsg struct {
	err error
}

// tuiStatusMsg carries daemon state used by chrome/sidebar rendering.
type tuiStatusMsg struct {
	cwd string
	err error
}

// streamReaderMsg wraps an IPC response reader for the update loop.
type streamReaderMsg struct {
	reader *ipc.ResponseReader
}

// streamEventMsg wraps a single IPC event from the stream.
type streamEventMsg struct {
	event ipc.IPCEvent
}

// streamDoneMsg signals that the stream has ended.
type streamDoneMsg struct{}

// streamErrMsg signals a stream error.
type streamErrMsg struct {
	err error
}

// healthCheckTickMsg triggers a periodic daemon reachability check.
type healthCheckTickMsg struct{}

// healthCheckResultMsg carries the result of a periodic health check.
// Unlike daemonReachableMsg/daemonUnreachableMsg, this never transitions
// to stateError — it only updates the daemon label for non-fatal checks.
type healthCheckResultMsg struct {
	err     error
	latency time.Duration
}

// contextWithTimeout is a test-accessible timeout helper.
var contextWithTimeout = func(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
