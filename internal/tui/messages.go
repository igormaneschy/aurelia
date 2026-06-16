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

// contextWithTimeout is a test-accessible timeout helper.
var contextWithTimeout = func(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
