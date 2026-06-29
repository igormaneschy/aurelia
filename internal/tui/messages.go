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
	err      error
	streamID int64
}

// tuiStatusMsg carries daemon state used by chrome/sidebar rendering.
type tuiStatusMsg struct {
	cwd   string
	model string
	err   error
}

// tuiModelsMsg carries models grouped by provider from a daemon /model response.
type tuiModelsMsg struct {
	catalog  modelCatalog
	err      error
	reloaded bool // true when triggered by /model refresh inside the wizard
}

// tuiHistoryMsg carries recent user/assistant transcript messages loaded from
// the daemon's PI session history. Errors are non-fatal at startup.
type tuiHistoryMsg struct {
	messages []chatMessage
	err      error
}

// streamReaderMsg wraps an IPC response reader for the update loop.
type streamReaderMsg struct {
	reader   *ipc.ResponseReader
	streamID int64
}

// streamEventMsg wraps a single IPC event from the stream.
type streamEventMsg struct {
	event    ipc.IPCEvent
	streamID int64
}

// streamDoneMsg signals that the stream has ended.
type streamDoneMsg struct {
	streamID int64
}

// streamErrMsg signals a stream error.
type streamErrMsg struct {
	err      error
	streamID int64
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

// tuiSessionsMsg carries the list of TUI local sessions from the daemon.
type tuiSessionsMsg struct {
	sessions []tuiSessionInfo
	err      error
}

// tuiSessionCreatedMsg carries a newly created session.
type tuiSessionCreatedMsg struct {
	session tuiSessionInfo
	err     error
}

// tuiSessionOpenedMsg carries the session that was opened/switched to.
type tuiSessionOpenedMsg struct {
	session tuiSessionInfo
	err     error
}

// tuiSessionDeletedMsg confirms a session was deleted.
type tuiSessionDeletedMsg struct {
	chatID int64
	err    error
}

// tuiSessionRenamedMsg confirms a session was renamed.
type tuiSessionRenamedMsg struct {
	chatID int64
	name   string
	err    error
}

// tuiSessionInfo is the TUI-side representation of a session.
type tuiSessionInfo struct {
	ChatID       int64
	Name         string
	MessageCount int `json:"message_count"`
}

// tuiProjectStateMsg carries the project state panel data from the daemon.
type tuiProjectStateMsg struct {
	state *ipc.ProjectStatePayload
	err   error
}

// projectStatePollTickMsg triggers a periodic poll of project state while the
// panel is open.
type projectStatePollTickMsg struct{}

// toolActivityTickMsg re-renders tool activity after the post-done dwell time.
type toolActivityTickMsg struct{}

// contextWithTimeout is a test-accessible timeout helper.
var contextWithTimeout = func(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
