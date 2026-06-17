package tui

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// newlineFallbackKey is displayed in the status bar as the multiline key.
// alt+enter is the primary; ctrl+j is the portable fallback.
const newlineFallbackKey = "alt+enter"

var terminalColorReportPattern = regexp.MustCompile(`^\d{1,2};rgb:[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}$`)

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateLoading:
		return m.updateLoading(msg)
	case stateChat:
		return m.updateChat(msg)
	case stateError:
		return m.updateError(msg)
	}
	return m, nil
}

func (m Model) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textarea.SetWidth(inputTextareaWidth(msg.Width))
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}

	case daemonUnreachableMsg:
		m.daemonLabel = "offline"
		m.state = stateError
		m.err = fmt.Errorf("daemon not reachable: %w", msg.err)
		return m, nil

	case daemonReachableMsg:
		m.state = stateChat
		m.ready = true
		m.daemonLabel = "ready"
		m.connectLatency = msg.latency
		m.messages = append(m.messages, chatMessage{
			Sender:    "Aurelia",
			Text:      "Connected to Aurelia daemon. Type a message or /help.",
			Timestamp: time.Now(),
		})
		m.ensureViewport()
		return m, tea.Batch(fetchTUIStatus(m.ipcClient), fetchTUIHistory(m.ipcClient))

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textarea.SetWidth(inputTextareaWidth(msg.Width))
		contentWidth := m.contentWidth()
		if !m.viewportSet {
			m.viewport = viewportForSize(contentWidth, msg.Height)
			m.viewportSet = true
			m.viewport.SetContent(m.renderMessages(m.messages, contentWidth))
			m.viewport.GotoBottom()
		} else {
			m.viewport.Width = contentWidth
			m.viewport.Height = viewportHeightForTerminal(msg.Height)
			m.updateViewport()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.MouseMsg:
		return m.handleViewportMsg(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case daemonReachableMsg:
		m.daemonLabel = "ready"
		m.connectLatency = msg.latency
		return m, scheduleHealthCheck()

	case healthCheckTickMsg:
		return m, runHealthCheck(m.ipcClient)

	case healthCheckResultMsg:
		if msg.err != nil {
			m.daemonLabel = "offline"
		} else {
			m.daemonLabel = "ready"
			m.connectLatency = msg.latency
		}
		return m, scheduleHealthCheck()

	case daemonUnreachableMsg:
		m.daemonLabel = "offline"
		m.state = stateError
		m.err = fmt.Errorf("daemon disconnected: %w", msg.err)
		return m, nil

	case tuiStatusMsg:
		if msg.err == nil && msg.cwd != "" {
			m.cwdPath = msg.cwd
		}
		return m, nil

	case tuiHistoryMsg:
		if msg.err == nil && len(msg.messages) > 0 && m.canApplyStartupHistory() {
			m.messages = msg.messages
			m.updateViewport()
		}
		return m, nil

	case daemonErrorMsg:
		m.daemonLabel = "error"
		m.waiting = false
		m.streamBuf = ""
		if m.reader != nil {
			m.reader.Close()
			m.reader = nil
		}
		m.err = msg.err
		m.messages = append(m.messages, chatMessage{
			Sender:    "⚠️",
			Text:      fmt.Sprintf("Error: %s", msg.err),
			Timestamp: time.Now(),
		})
		m.updateViewport()
		return m, nil

	case *streamReaderMsg:
		m.reader = msg.reader
		return m, tea.Batch(m.readNextStreamEvent(), spinnerTickCmd())

	case streamEventMsg:
		return m.handleStreamEvent(msg.event)

	case streamDoneMsg:
		// Stream ended (EOF) without explicit terminal event.
		m.waiting = false
		if m.reader != nil {
			m.reader.Close()
			m.reader = nil
		}
		m.streamBuf = ""
		m.messages = append(m.messages, chatMessage{
			Sender:    "⚠️",
			Text:      "Connection closed unexpectedly.",
			Timestamp: time.Now(),
		})
		m.updateViewport()
		return m, nil

	case streamErrMsg:
		// Stream error.
		m.waiting = false
		if m.reader != nil {
			m.reader.Close()
			m.reader = nil
		}
		m.streamBuf = ""
		errText := msg.err.Error()
		if errText == "" {
			errText = "unknown stream error"
		}
		m.messages = append(m.messages, chatMessage{
			Sender:    "⚠️",
			Text:      fmt.Sprintf("Stream error: %s", errText),
			Timestamp: time.Now(),
		})
		m.updateViewport()
		return m, nil
	}

	return m, nil
}

func (m Model) updateError(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			m.state = stateLoading
			m.err = nil
			m.daemonLabel = "connecting"
			return m, tea.Batch(
				m.spinner.Tick,
				checkDaemon(m.ipcClient),
			)
		}
	}

	return m, nil
}

// handleKeyMsg processes keyboard input.
// enter submits when not waiting. alt+enter inserts a newline in the textarea.
// ctrl+j is also accepted as a portable newline fallback.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit

	case msg.String() == "ctrl+l":
		m.messages = nil
		m.updateViewport()
		return m, nil

	case isSidebarToggleKey(msg):
		m.showSidebar = !m.showSidebar
		m.updateViewport()
		return m, nil

	case isViewportScrollKey(msg):
		return m.handleViewportMsg(msg)

	case msg.String() == "up":
		if m.canNavigateInputHistory(-1) {
			return m.navigateInputHistory(-1), nil
		}
		return m.delegateKeyToTextarea(msg)

	case msg.String() == "down":
		if m.canNavigateInputHistory(1) {
			return m.navigateInputHistory(1), nil
		}
		return m.delegateKeyToTextarea(msg)

	case msg.String() == "esc":
		if m.waiting {
			return m.cancelStreaming()
		}
		return m, nil

	case msg.String() == "enter":
		// Submit.
		if m.waiting {
			return m, nil
		}
		text := strings.TrimSpace(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		m.rememberInput(text)
		m.textarea.Reset()
		m.messages = append(m.messages, chatMessage{
			Sender:    "Igor",
			Text:      text,
			Timestamp: time.Now(),
		})
		m.waiting = true
		m.updateViewport()

		if strings.HasPrefix(text, "/") {
			return m, tea.Batch(m.sendCommand(text), spinnerTickCmd())
		}
		return m, tea.Batch(m.submitMessage(text), spinnerTickCmd())

	case msg.String() == "alt+enter":
		// alt+enter: insert newline.
		m.textarea.InsertString("\n")
		return m, nil

	case msg.String() == "ctrl+j":
		// ctrl+j: portable newline fallback.
		m.textarea.InsertString("\n")
		return m, nil

	default:
		return m.delegateKeyToTextarea(msg)
	}
}

func (m Model) delegateKeyToTextarea(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if isTerminalColorReportResidue(msg) {
		return m, nil
	}
	if !shouldDelegateKeyToTextarea(msg, m.textarea.KeyMap) {
		return m, nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

const maxInputHistory = 100

func (m *Model) rememberInput(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	last := len(m.inputHistory) - 1
	if last < 0 || m.inputHistory[last] != text {
		m.inputHistory = append(m.inputHistory, text)
		if len(m.inputHistory) > maxInputHistory {
			m.inputHistory = m.inputHistory[len(m.inputHistory)-maxInputHistory:]
		}
	}
	m.inputHistoryIndex = len(m.inputHistory)
}

func (m Model) canNavigateInputHistory(direction int) bool {
	if m.waiting || len(m.inputHistory) == 0 {
		return false
	}
	if m.isViewingInputHistory() {
		return true
	}
	if direction < 0 {
		return strings.TrimSpace(m.textarea.Value()) == ""
	}
	return false
}

func (m Model) isViewingInputHistory() bool {
	if m.inputHistoryIndex < 0 || m.inputHistoryIndex >= len(m.inputHistory) {
		return false
	}
	return m.textarea.Value() == m.inputHistory[m.inputHistoryIndex]
}

func (m Model) navigateInputHistory(direction int) Model {
	if len(m.inputHistory) == 0 {
		return m
	}
	if m.inputHistoryIndex < 0 || m.inputHistoryIndex > len(m.inputHistory) {
		m.inputHistoryIndex = len(m.inputHistory)
	}

	if direction < 0 && m.inputHistoryIndex > 0 {
		m.inputHistoryIndex--
		m.textarea.SetValue(m.inputHistory[m.inputHistoryIndex])
		m.textarea.CursorEnd()
		return m
	}
	if direction > 0 && m.inputHistoryIndex < len(m.inputHistory) {
		m.inputHistoryIndex++
		if m.inputHistoryIndex == len(m.inputHistory) {
			m.textarea.Reset()
			return m
		}
		m.textarea.SetValue(m.inputHistory[m.inputHistoryIndex])
		m.textarea.CursorEnd()
	}
	return m
}

func (m Model) canApplyStartupHistory() bool {
	if m.waiting || m.reader != nil || len(m.messages) != 1 {
		return false
	}
	return m.messages[0].Sender == "Aurelia" && strings.HasPrefix(m.messages[0].Text, "Connected to Aurelia daemon")
}

func (m Model) handleViewportMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !m.viewportSet {
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// cancelStreaming aborts the current streaming response, closing the reader
// and returning the model to the ready state. Triggered by Esc during waiting.
func (m Model) cancelStreaming() (tea.Model, tea.Cmd) {
	m.waiting = false
	if m.reader != nil {
		m.reader.Close()
		m.reader = nil
	}
	m.streamBuf = ""
	m.messages = append(m.messages, chatMessage{
		Sender:    "⚠️",
		Text:      "(cancelled)",
		Timestamp: time.Now(),
	})
	m.updateViewport()
	return m, nil
}

func isViewportScrollKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "pgup", "pgdown":
		return true
	}
	return false
}

func isTerminalColorReportResidue(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes && !msg.Paste && terminalColorReportPattern.MatchString(string(msg.Runes))
}

func shouldDelegateKeyToTextarea(msg tea.KeyMsg, keyMap textarea.KeyMap) bool {
	if !msg.Alt || msg.Type != tea.KeyRunes || msg.Paste {
		return true
	}
	return key.Matches(msg,
		keyMap.WordForward,
		keyMap.WordBackward,
		keyMap.DeleteWordForward,
		keyMap.InputBegin,
		keyMap.InputEnd,
		keyMap.LowercaseWordForward,
		keyMap.UppercaseWordForward,
		keyMap.CapitalizeWordForward,
	)
}

func isSidebarToggleKey(msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyTab {
		return true
	}
	s := msg.String()
	return s == "tab" || s == "ctrl+i" || s == "\t"
}

// handleStreamEvent processes a single IPC event.
// For "message" events (final text), the event Body replaces accumulated
// streamBuf to avoid duplicating chunk deltas.
func (m Model) handleStreamEvent(event ipc.IPCEvent) (tea.Model, tea.Cmd) {
	switch event.Type {
	case "ack":
		// No content to display; continue reading.
		return m, tea.Batch(m.readNextStreamEvent(), spinnerTickCmd())

	case "stream_chunk":
		m.streamBuf += event.Body
		m.appendOrUpdateAureliaMessage(m.streamBuf)
		m.updateViewport()
		return m, tea.Batch(m.readNextStreamEvent(), spinnerTickCmd())

	case "message":
		// Replace accumulated text with the final message to avoid duplication.
		m.streamBuf = event.Body
		if cwd := cwdFromText(event.Body); cwd != "" {
			m.cwdPath = cwd
		}
		m.appendOrUpdateAureliaMessage(m.streamBuf)
		m.updateViewport()
		return m, tea.Batch(m.readNextStreamEvent(), spinnerTickCmd())

	case "stream_end":
		// Terminal event — stream complete.
		m.waiting = false
		if m.reader != nil {
			m.reader.Close()
			m.reader = nil
		}
		m.streamBuf = ""
		m.updateViewport()
		return m, nil

	case "error":
		// Terminal event — error.
		m.waiting = false
		if m.reader != nil {
			m.reader.Close()
			m.reader = nil
		}
		errText := event.Error
		if errText == "" {
			errText = "unknown error"
		}
		m.streamBuf = ""
		m.messages = append(m.messages, chatMessage{
			Sender:    "⚠️",
			Text:      errText,
			Timestamp: time.Now(),
		})
		m.updateViewport()
		return m, nil
	}

	// Unknown event type — keep reading.
	return m, tea.Batch(m.readNextStreamEvent(), spinnerTickCmd())
}

// readNextStreamEvent returns a command that reads the next event from the
// current stream reader. Returns nil if no reader is active.
func (m Model) readNextStreamEvent() tea.Cmd {
	if m.reader == nil {
		return nil
	}
	return func() tea.Msg {
		event, err := m.reader.Read()
		if err == io.EOF {
			m.reader.Close()
			return streamDoneMsg{}
		}
		if err != nil {
			m.reader.Close()
			return streamErrMsg{err: err}
		}
		return streamEventMsg{event: event}
	}
}

// appendOrUpdateAureliaMessage appends a new Aurelia message or updates the
// last one if it's already an Aurelia message (for streaming).
func (m *Model) appendOrUpdateAureliaMessage(text string) {
	now := time.Now()
	if len(m.messages) > 0 {
		last := &m.messages[len(m.messages)-1]
		if last.Sender == "Aurelia" {
			last.Text = text
			last.Timestamp = now
			return
		}
	}
	m.messages = append(m.messages, chatMessage{
		Sender:    "Aurelia",
		Text:      text,
		Timestamp: now,
	})
}

// ensureViewport lazily initializes the viewport if dimensions were stored
// during loading but the viewport hasn't been created yet.
func (m *Model) ensureViewport() {
	if m.width > 0 && m.height > 0 && !m.viewportSet {
		contentWidth := m.contentWidth()
		m.viewport = viewportForSize(contentWidth, m.height)
		m.viewportSet = true
		m.viewport.SetContent(m.renderMessages(m.messages, contentWidth))
		m.viewport.GotoBottom()
	}
}

// updateViewport refreshes the viewport content if initialized.
func (m *Model) updateViewport() {
	m.ensureViewport()
	if m.viewportSet && m.viewport.Height > 0 {
		contentWidth := m.contentWidth()
		m.viewport.Width = contentWidth
		m.viewport.SetContent(m.renderMessages(m.messages, contentWidth))
		m.viewport.GotoBottom()
	}
}
