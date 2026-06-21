// Package tui implements the terminal UI for the Aurelia TUI client using
// Bubble Tea. It provides a chat-like interface for interacting with the
// Aurelia daemon over a Unix socket IPC connection.
package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// spinnerTickCmd returns a command that fires a spinner.TickMsg, used to
// keep the spinner animating during streaming responses.
func spinnerTickCmd() tea.Cmd {
	return func() tea.Msg {
		return spinner.TickMsg{}
	}
}

// TUI states.
type tuiState int

const (
	stateLoading tuiState = iota
	stateChat
	stateError
)

// chatMessage represents a single message in the chat viewport.
type chatMessage struct {
	Sender    string // "Igor" or "Aurelia"
	Text      string
	Timestamp time.Time
}

// Model is the main Bubble Tea model for the TUI.
type Model struct {
	// State machine
	state tuiState

	// IPC connection
	socketPath string
	ipcClient  *ipc.Client

	// Active session — ChatID of the currently open TUI local session.
	// Default is ReservedTUIChatID (the DM).
	activeSession int64

	// Session list for sidebar
	sessions       []tuiSessionInfo
	sidebarCursor  int // index into sessions, 0-based
	sidebarFocused bool

	// Chat history (active session only — reloaded on session switch)
	messages []chatMessage

	// Viewport for scrolling chat
	viewport    viewport.Model
	viewportSet bool

	// Spinner for loading state
	spinner spinner.Model

	// Textarea for multiline input
	textarea textarea.Model

	// Command autocomplete state.
	autocompleteOptions []string
	autocompleteIndex   int

	// In-memory prompt/command history for ↑/↓ navigation.
	inputHistory      []string
	inputHistoryIndex int
	historyPath       string

	// Pending request tracking
	requestID string
	waiting   bool
	streamID  int64

	// Messages submitted while a stream is active. The daemon still processes
	// one turn at a time; the TUI client sends the next item after stream_end.
	pendingQueue []queuedMessage

	// switchingSession is true while waiting for history after a session
	// switch. It tells tuiHistoryMsg to replace messages even when the
	// "Connected to Aurelia daemon" startup message is not present.
	switchingSession bool

	// Current stream reader (held between events during streaming)
	reader *ipc.ResponseReader

	// Accumulated streaming text
	streamBuf string

	// UI state
	width          int
	height         int
	showSidebar    bool
	mouseEnabled   bool
	err            error
	ready          bool
	daemonLabel    string
	cwdPath        string
	connectLatency time.Duration

	// Cached glamour renderer (recreated when width changes)
	glamourRenderer *glamour.TermRenderer
	rendererWidth   int

	// Pending image attachments (cleared after send)
	pendingImages           []pendingImage
	submittedTempImagePaths []string

	// Pending document attachments (cleared after send)
	pendingAttachments []pendingAttachment

	// Project state panel
	projectPanelOpen bool
	projectState     *ipc.ProjectStatePayload

	// Help overlay — toggled with ?.
	helpOverlayOpen bool

	// renameTargetChatID is non-zero when the user is renaming a session.
	// The textarea shows the current name and Enter sends the rename.
	renameTargetChatID int64

	// Style palette — owned by theme.go. Default is dark; T5.2.1 will
	// select light vs dark based on terminal hints and --theme flag.
	styles themeStyles

	// theme is the requested theme (auto, light, dark). The effective palette
	// in styles is resolved from this value at startup.
	theme Theme

	// activeModel is the display name of the currently active AI model
	// (e.g. "gpt-5.5", "deepseek-v4-pro"). Populated from daemon status.
	activeModel string

	// turnStart marks when the current turn began. Reset when no turn is
	// active. Used by the status bar to show elapsed time.
	turnStart time.Time
}

// NewModel creates a new TUI model with the given socket path and theme.
func NewModel(socketPath string, theme Theme) Model {
	return newModel(socketPath, defaultInputHistoryPath(), theme)
}

func newModel(socketPath, historyPath string, theme Theme) Model {
	s := spinner.New()
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	ta := textarea.New()
	ta.Prompt = ""
	ta.Placeholder = "Type a message…"
	ta.Focus()
	ta.CharLimit = 4000
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false) // Enter will be intercepted for submit

	ta.SetHeight(2)

	inputHistory := loadInputHistory(historyPath)

	return Model{
		state:             stateLoading,
		socketPath:        socketPath,
		ipcClient:         ipc.NewClient(socketPath),
		spinner:           s,
		textarea:          ta,
		showSidebar:       true,
		daemonLabel:       "connecting",
		cwdPath:           "not set",
		messages:          make([]chatMessage, 0),
		inputHistory:      inputHistory,
		inputHistoryIndex: len(inputHistory),
		historyPath:       historyPath,
		activeSession:     ipc.ReservedTUIChatID,
		theme:             theme,
		styles:            newStylesForTheme(theme),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		checkDaemon(m.ipcClient),
	)
}

// isChatMode returns true when no project cwd is set, meaning file system
// tools (Read, Write, Edit, Bash, Glob, Grep, LS, List) are disabled.
// The session works as a conversational assistant only.
func (m Model) isChatMode() bool {
	return m.cwdPath == "" || m.cwdPath == "not set"
}

// checkDaemon returns a command that pings the daemon.
func checkDaemon(client *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		ctx, cancel := contextWithTimeout(1200 * time.Millisecond)
		defer cancel()
		if err := client.Ping(ctx); err != nil {
			return daemonUnreachableMsg{err: err}
		}
		return daemonReachableMsg{latency: time.Since(started)}
	}
}

// healthCheckInterval is the delay between periodic daemon health checks.
const healthCheckInterval = 30 * time.Second

// scheduleHealthCheck returns a command that fires a healthCheckTickMsg
// after the configured interval.
func scheduleHealthCheck() tea.Cmd {
	return tea.Tick(healthCheckInterval, func(time.Time) tea.Msg {
		return healthCheckTickMsg{}
	})
}

// runHealthCheck pings the daemon and returns a healthCheckResultMsg.
// Unlike checkDaemon, a failure here does not transition to stateError.
func runHealthCheck(client *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		ctx, cancel := contextWithTimeout(1500 * time.Millisecond)
		defer cancel()
		if err := client.Ping(ctx); err != nil {
			return healthCheckResultMsg{err: err}
		}
		return healthCheckResultMsg{latency: time.Since(started)}
	}
}

// projectStatePollInterval is the delay between automatic project state polls
// while the panel is open.
const projectStatePollInterval = 30 * time.Second

// scheduleProjectStatePoll returns a command that fires a poll tick after
// the configured interval.
func scheduleProjectStatePoll() tea.Cmd {
	return tea.Tick(projectStatePollInterval, func(time.Time) tea.Msg {
		return projectStatePollTickMsg{}
	})
}

// fetchTUIProjectState returns a command that requests the full project state
// snapshot from the daemon for the project panel (ctrl+p).
func fetchTUIProjectState(client *ipc.Client, chatID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(3 * time.Second)
		defer cancel()
		events, err := client.SendAndWait(ctx, ipc.IPCMessage{
			Type:      ipc.MsgTypeProjectState,
			ChatID:    chatID,
			ThreadID:  0,
			UserID:    int64(os.Getuid()),
			RequestID: fmt.Sprintf("tui-project-state-%d", time.Now().UnixNano()),
		})
		if err != nil {
			return tuiProjectStateMsg{err: err}
		}
		return projectStateFromEvents(events)
	}
}

// projectStateFromEvents parses the project state payload from IPC events.
func projectStateFromEvents(events []ipc.IPCEvent) tuiProjectStateMsg {
	for _, ev := range events {
		if ev.Type != ipc.EventTypeProjectState || ev.Body == "" {
			continue
		}
		var payload ipc.ProjectStatePayload
		if err := json.Unmarshal([]byte(ev.Body), &payload); err != nil {
			return tuiProjectStateMsg{err: fmt.Errorf("parse project state: %w", err)}
		}
		return tuiProjectStateMsg{state: &payload}
	}
	return tuiProjectStateMsg{err: fmt.Errorf("no project state event in response")}
}

// fetchTUIStatus returns a command that asks the daemon for lightweight
// session status used by the chrome/sidebar. It intentionally does not write
// into the chat history.
func fetchTUIStatus(client *ipc.Client, chatID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(1500 * time.Millisecond)
		defer cancel()
		events, err := client.SendAndWait(ctx, ipc.IPCMessage{
			Type:      ipc.MsgTypeCommand,
			ChatID:    chatID,
			ThreadID:  0,
			UserID:    int64(os.Getuid()),
			Text:      "/status",
			RequestID: fmt.Sprintf("tui-status-%d", time.Now().UnixNano()),
		})
		if err != nil {
			return tuiStatusMsg{err: err}
		}
		return statusFromEvents(events)
	}
}

// statusFromEvents extracts cwd and model from the daemon's /status response.
func statusFromEvents(events []ipc.IPCEvent) tuiStatusMsg {
	for _, ev := range events {
		if ev.Type != ipc.EventTypeMessage || ev.Body == "" {
			continue
		}
		cwd := cwdFromText(ev.Body)
		model := modelFromText(ev.Body)
		return tuiStatusMsg{cwd: cwd, model: model}
	}
	return tuiStatusMsg{}
}

type tuiHistoryPayload struct {
	Sender    string `json:"sender"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}

// fetchTUIHistory asks the daemon for recent PI session transcript messages.
// Failure is intentionally non-fatal so startup never blocks on history.
func fetchTUIHistory(client *ipc.Client, chatID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(3 * time.Second)
		defer cancel()
		events, err := client.SendAndWait(ctx, ipc.IPCMessage{
			Type:      ipc.MsgTypeHistory,
			ChatID:    chatID,
			ThreadID:  0,
			UserID:    int64(os.Getuid()),
			RequestID: fmt.Sprintf("tui-history-%d", time.Now().UnixNano()),
		})
		if err != nil {
			return tuiHistoryMsg{err: err}
		}
		return historyFromEvents(events)
	}
}

func historyFromEvents(events []ipc.IPCEvent) tuiHistoryMsg {
	for _, ev := range events {
		if ev.Type != ipc.EventTypeHistory {
			continue
		}
		var payload []tuiHistoryPayload
		if err := json.Unmarshal([]byte(ev.Body), &payload); err != nil {
			return tuiHistoryMsg{err: fmt.Errorf("parse history: %w", err)}
		}
		messages := make([]chatMessage, 0, len(payload))
		for _, item := range payload {
			if item.Text == "" || item.Sender == "" {
				continue
			}
			ts := time.Time{}
			if item.Timestamp != "" {
				if parsed, err := time.Parse(time.RFC3339Nano, item.Timestamp); err == nil {
					ts = parsed
				}
			}
			messages = append(messages, chatMessage{
				Sender:    item.Sender,
				Text:      item.Text,
				Timestamp: ts,
			})
		}
		return tuiHistoryMsg{messages: messages}
	}
	return tuiHistoryMsg{}
}

// submitMessage sends a message to the daemon and returns a command.
func (m Model) submitMessage(text string) tea.Cmd {
	return m.submitMessageWithPayload(m.activeSession, text, m.toIPCImages(), m.toIPCAttachments(), m.streamID)
}

func (m Model) submitMessageWithPayload(chatID int64, text string, images []ipc.IPCImage, attachments []ipc.IPCAttachment, streamID int64) tea.Cmd {
	m.requestID = fmt.Sprintf("tui-%d", time.Now().UnixNano())
	m.waiting = true
	m.turnStart = time.Now()

	userID := int64(os.Getuid())
	msg := ipc.IPCMessage{
		Type:        ipc.MsgTypeSend,
		ChatID:      chatID,
		ThreadID:    0,
		UserID:      userID,
		Text:        text,
		Images:      images,
		Attachments: attachments,
		RequestID:   m.requestID,
	}

	// Note: pendingImages cleanup happens in update.go after submitMessage
	// returns, because Model is a value type and mutations here are discarded.

	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(30 * time.Minute)
		defer cancel()
		reader, err := m.ipcClient.Send(ctx, msg)
		if err != nil {
			return daemonErrorMsg{err: err, streamID: streamID}
		}
		return &streamReaderMsg{reader: reader, streamID: streamID}
	}
}

// sendCommand sends a command to the daemon.
func (m Model) sendCommand(text string) tea.Cmd {
	return m.sendCommandToSession(m.activeSession, text, m.streamID)
}

func (m Model) sendCommandToSession(chatID int64, text string, streamID int64) tea.Cmd {
	m.requestID = fmt.Sprintf("tui-%d", time.Now().UnixNano())
	m.waiting = true

	userID := int64(os.Getuid())
	msg := ipc.IPCMessage{
		Type:      ipc.MsgTypeCommand,
		ChatID:    chatID,
		ThreadID:  0,
		UserID:    userID,
		Text:      text,
		RequestID: m.requestID,
	}

	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(30 * time.Second)
		defer cancel()
		reader, err := m.ipcClient.Send(ctx, msg)
		if err != nil {
			return daemonErrorMsg{err: err, streamID: streamID}
		}
		return &streamReaderMsg{reader: reader, streamID: streamID}
	}
}

// fetchTUISessions asks the daemon for the list of TUI local sessions.
func fetchTUISessions(client *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(3 * time.Second)
		defer cancel()
		events, err := client.SendAndWait(ctx, ipc.IPCMessage{
			Type:      ipc.MsgTypeSessions,
			RequestID: fmt.Sprintf("tui-sessions-%d", time.Now().UnixNano()),
		})
		if err != nil {
			return tuiSessionsMsg{err: err}
		}
		return sessionsFromEvents(events)
	}
}

// createTUISession asks the daemon to create a new named session.
func createTUISession(client *ipc.Client, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(3 * time.Second)
		defer cancel()
		events, err := client.SendAndWait(ctx, ipc.IPCMessage{
			Type:      ipc.MsgTypeSessionCreate,
			Text:      name,
			RequestID: fmt.Sprintf("tui-session-create-%d", time.Now().UnixNano()),
		})
		if err != nil {
			return tuiSessionCreatedMsg{err: err}
		}
		return sessionCreatedFromEvents(events)
	}
}

// openTUISession asks the daemon to open/switch to an existing session.
func openTUISession(client *ipc.Client, chatID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(3 * time.Second)
		defer cancel()
		events, err := client.SendAndWait(ctx, ipc.IPCMessage{
			Type:      ipc.MsgTypeSessionOpen,
			ChatID:    chatID,
			RequestID: fmt.Sprintf("tui-session-open-%d", time.Now().UnixNano()),
		})
		if err != nil {
			return tuiSessionOpenedMsg{err: err}
		}
		return sessionOpenedFromEvents(events)
	}
}

// deleteTUISession asks the daemon to delete a session.
func deleteTUISession(client *ipc.Client, chatID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(3 * time.Second)
		defer cancel()
		_, err := client.SendAndWait(ctx, ipc.IPCMessage{
			Type:      ipc.MsgTypeSessionDelete,
			ChatID:    chatID,
			RequestID: fmt.Sprintf("tui-session-delete-%d", time.Now().UnixNano()),
		})
		if err != nil {
			return tuiSessionDeletedMsg{chatID: chatID, err: err}
		}
		return tuiSessionDeletedMsg{chatID: chatID}
	}
}

// renameTUISession asks the daemon to rename a session.
func renameTUISession(client *ipc.Client, chatID int64, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(3 * time.Second)
		defer cancel()
		events, err := client.SendAndWait(ctx, ipc.IPCMessage{
			Type:      ipc.MsgTypeSessionRename,
			ChatID:    chatID,
			Text:      name,
			RequestID: fmt.Sprintf("tui-session-rename-%d", time.Now().UnixNano()),
		})
		if err != nil {
			return tuiSessionRenamedMsg{chatID: chatID, err: err}
		}
		return sessionRenamedFromEvents(events)
	}
}

// sessionRenamedFromEvents parses a renamed session from IPC events.
func sessionRenamedFromEvents(events []ipc.IPCEvent) tuiSessionRenamedMsg {
	type sessionPayload struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}
	for _, ev := range events {
		if ev.Type != ipc.EventTypeSessionRenamed || ev.Body == "" {
			continue
		}
		var s sessionPayload
		if err := json.Unmarshal([]byte(ev.Body), &s); err != nil {
			return tuiSessionRenamedMsg{err: fmt.Errorf("parse renamed session: %w", err)}
		}
		return tuiSessionRenamedMsg{chatID: s.ChatID, name: s.Name}
	}
	return tuiSessionRenamedMsg{}
}

// sessionsFromEvents parses the sessions list from IPC events.
func sessionsFromEvents(events []ipc.IPCEvent) tuiSessionsMsg {
	type sessionPayload struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}
	for _, ev := range events {
		if ev.Type != ipc.EventTypeSessions || ev.Body == "" {
			continue
		}
		var payload []sessionPayload
		if err := json.Unmarshal([]byte(ev.Body), &payload); err != nil {
			return tuiSessionsMsg{err: fmt.Errorf("parse sessions: %w", err)}
		}
		sessions := make([]tuiSessionInfo, 0, len(payload))
		for _, s := range payload {
			sessions = append(sessions, tuiSessionInfo(s))
		}
		return tuiSessionsMsg{sessions: sessions}
	}
	return tuiSessionsMsg{}
}

// sessionCreatedFromEvents parses a created session from IPC events.
func sessionCreatedFromEvents(events []ipc.IPCEvent) tuiSessionCreatedMsg {
	type sessionPayload struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}
	for _, ev := range events {
		if ev.Type != ipc.EventTypeSessionCreated || ev.Body == "" {
			continue
		}
		var s sessionPayload
		if err := json.Unmarshal([]byte(ev.Body), &s); err != nil {
			return tuiSessionCreatedMsg{err: fmt.Errorf("parse created session: %w", err)}
		}
		return tuiSessionCreatedMsg{session: tuiSessionInfo(s)}
	}
	return tuiSessionCreatedMsg{}
}

// sessionOpenedFromEvents parses an opened session from IPC events.
func sessionOpenedFromEvents(events []ipc.IPCEvent) tuiSessionOpenedMsg {
	type sessionPayload struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}
	for _, ev := range events {
		if ev.Type != ipc.EventTypeSessionOpened || ev.Body == "" {
			continue
		}
		var s sessionPayload
		if err := json.Unmarshal([]byte(ev.Body), &s); err != nil {
			return tuiSessionOpenedMsg{err: fmt.Errorf("parse opened session: %w", err)}
		}
		return tuiSessionOpenedMsg{session: tuiSessionInfo(s)}
	}
	return tuiSessionOpenedMsg{}
}
