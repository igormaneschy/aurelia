package tui

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/stopwatch"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

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
		return m, tea.Batch(
			fetchTUIStatus(m.ipcClient, m.activeSession),
			fetchTUIHistory(m.ipcClient, m.activeSession),
			fetchTUISessions(m.ipcClient),
			scheduleHealthCheck(),
		)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+o" {
		return m.toggleMouseCapture()
	}

	if m.formOpen {
		switch msg.(type) {
		case tea.KeyMsg, tea.WindowSizeMsg, formInternalMsg:
			return m.updateActiveForm(msg)
		}
	}

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
			m.viewport.SetWidth(contentWidth)
			m.viewport.SetHeight(viewportHeightForTerminal(msg.Height))
			m.updateViewport()
		}
		m.resizeSidebarTable()
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.PasteMsg:
		text := strings.TrimSpace(msg.Content)
		if isImagePath(text) {
			errMsg := m.attachImageFromPath(text)
			if errMsg != "" {
				m.messages = append(m.messages, chatMessage{Sender: "⚠️", Text: errMsg})
				m.updateViewport()
			} else {
				m.messages = append(m.messages, chatMessage{
					Sender: "📎",
					Text:   fmt.Sprintf("Image attached: %s", filepath.Base(text)),
				})
				m.updateViewport()
			}
			return m, nil
		}
		if looksLikeFilePath(text) {
			normalized := normalizeImagePath(text)
			errMsg := m.attachDocumentFromPath(normalized)
			if errMsg != "" {
				m.messages = append(m.messages, chatMessage{Sender: "⚠️", Text: errMsg})
			} else {
				name := filepath.Base(normalized)
				if len(m.pendingAttachments) > 0 {
					name = m.pendingAttachments[len(m.pendingAttachments)-1].name
				}
				m.messages = append(m.messages, chatMessage{
					Sender: "📎",
					Text:   fmt.Sprintf("Document attached: %s", name),
				})
			}
			m.updateViewport()
			return m, nil
		}
		if msg.Content != "" {
			m.textarea.InsertString(msg.Content)
			m.clearAutocomplete()
		}
		return m, nil

	case tea.MouseMsg:
		if !m.mouseEnabled { return m, nil }
		if m.shouldShowSidebar() {
			if handled, model, cmd := m.handleSidebarMouse(msg); handled { return model, cmd }
		}
		return m.handleViewportMsg(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case stopwatch.TickMsg, stopwatch.StartStopMsg, stopwatch.ResetMsg:
		return m.updateStreamProgressMsgs(msg)

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
			if m.daemonLabel == "offline" {
				// Daemon recovered — notify user.
				m.messages = append(m.messages, chatMessage{
					Sender: "🔗",
					Text:   "Daemon reconnected.",
				})
				m.updateViewport()
			}
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
		if msg.err == nil {
			if msg.cwd != "" {
				m.cwdPath = msg.cwd
			} else {
				// No cwd marker in status response — session has no project.
				m.cwdPath = "not set"
			}
			m.activeModel = msg.model
			m.syncSidebarRows()
		}
		return m, nil

	case tuiModelsMsg:
		if m.formOpen && m.activeForm != nil {
			catalog := msg.catalog
			if catalog.providerCount() == 0 || msg.err != nil {
				catalog = catalogFromModels(modelFallbackList(m.activeModel))
			}
			// Do not reset the wizard if the user already advanced to model pick.
			if m.activeForm.kind == formKindModelName {
				m.activeForm.catalog = catalog
				if len(catalog.byProvider[m.activeForm.provider]) > 0 {
					m.activeForm = newModelNameForm(catalog, m.activeForm.provider, m.activeForm.chosenModel())
					return m, m.activeForm.init()
				}
				return m, nil
			}
			m = m.refreshModelSelectForm(tuiModelsMsg{catalog: catalog})
			return m, m.activeForm.init()
		}
		return m, nil

	case tuiHistoryMsg:
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{
				Sender: "⚠️",
				Text:   fmt.Sprintf("Warning: failed to load chat history: %s", safeSessionLabel(msg.err.Error())),
			})
			m.updateViewport()
			m.switchingSession = false
			return m, nil
		}
		if m.switchingSession || m.canApplyStartupHistory() {
			m.messages = msg.messages
			m.updateViewport()
		}
		m.switchingSession = false
		return m, nil

	case daemonErrorMsg:
		if msg.streamID != 0 && msg.streamID != m.streamID {
			return m, nil
		}
		m.daemonLabel = "error"
		m.waiting = false
		m.resetStreamProgress()
		m.streamBuf = ""
		m.cleanupSubmittedTempImages()
		if m.reader != nil {
			_ = m.reader.Close()
			m.reader = nil
		}
		m.err = msg.err
		m.messages = append(m.messages, chatMessage{
			Sender:    "⚠️",
			Text:      fmt.Sprintf("Error: %s", msg.err),
			Timestamp: time.Now(),
		})
		m.updateViewport()
		return m.continueWithNextQueuedMessage()

	case *streamReaderMsg:
		if msg.streamID != m.streamID {
			_ = msg.reader.Close()
			return m, nil
		}
		m.reader = msg.reader
		return m, tea.Batch(m.readNextStreamEvent(), spinnerTickCmd())

	case streamEventMsg:
		if msg.streamID != m.streamID {
			return m, nil
		}
		return m.handleStreamEvent(msg.event)

	case streamDoneMsg:
		if msg.streamID != m.streamID {
			return m, nil
		}
		// Stream ended (EOF) without explicit terminal event.
		m.waiting = false
		m.resetStreamProgress()
		m.cleanupSubmittedTempImages()
		if m.reader != nil {
			_ = m.reader.Close()
			m.reader = nil
		}
		m.streamBuf = ""
		m.messages = append(m.messages, chatMessage{
			Sender:    "⚠️",
			Text:      "Connection closed unexpectedly.",
			Timestamp: time.Now(),
		})
		m.updateViewport()
		return m.continueWithNextQueuedMessage()

	case streamErrMsg:
		if msg.streamID != m.streamID {
			return m, nil
		}
		// Stream error.
		m.waiting = false
		m.resetStreamProgress()
		m.cleanupSubmittedTempImages()
		if m.reader != nil {
			_ = m.reader.Close()
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
		return m.continueWithNextQueuedMessage()

	case tuiSessionsMsg:
		if msg.err == nil {
			m.sessions = msg.sessions
			m.syncSidebarRows()
			// Ensure the active session is in the list (the default DM
			// always exists implicitly even if not in the store).
			m.ensureDefaultSessionInList()
			// Clamp sidebar cursor to valid range.
			if m.sidebarCursor >= len(m.sessions) {
				m.sidebarCursor = maxInt(0, len(m.sessions)-1)
			}
		}
		return m, nil

	case tuiSessionCreatedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{
				Sender:    "⚠️",
				Text:      fmt.Sprintf("Failed to create session: %s", msg.err),
				Timestamp: time.Now(),
			})
			m.updateViewport()
			return m, nil
		}
		// Clean up any queued images from the previous session.
		m.cleanupQueuedTempImages()
		// Switch to the newly created session.
		m.activeSession = msg.session.ChatID
		m.messages = []chatMessage{}
		m.viewportSet = false
		m.switchingSession = true
		m.syncSidebarRows()
		m.updateViewport()
		// Reload sessions list + history for the new session.
		return m, tea.Batch(
			fetchTUISessions(m.ipcClient),
			fetchTUIHistory(m.ipcClient, m.activeSession),
			fetchTUIStatus(m.ipcClient, m.activeSession),
		)

	case tuiSessionOpenedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{
				Sender:    "⚠️",
				Text:      fmt.Sprintf("Failed to open session: %s", msg.err),
				Timestamp: time.Now(),
			})
			m.updateViewport()
			return m, nil
		}
		// Clean up any queued images from the previous session.
		m.cleanupQueuedTempImages()
		// Switch to the opened session.
		m.activeSession = msg.session.ChatID
		m.messages = []chatMessage{}
		m.viewportSet = false
		m.switchingSession = true
		m.syncSidebarRows()
		m.updateViewport()
		m.sidebarFocused = false
		m.sidebarTable.Blur()
		// Reload history + status for the new session.
		return m, tea.Batch(
			fetchTUIHistory(m.ipcClient, m.activeSession),
			fetchTUIStatus(m.ipcClient, m.activeSession),
		)

	case tuiSessionDeletedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{
				Sender:    "⚠️",
				Text:      fmt.Sprintf("Failed to delete session: %s", msg.err),
				Timestamp: time.Now(),
			})
			m.updateViewport()
			return m, nil
		}
		// If the active session was deleted, fall back to the default DM.
		if m.activeSession == msg.chatID {
			m.activeSession = ipc.ReservedTUIChatID
			m.messages = []chatMessage{}
			m.viewportSet = false
			m.updateViewport()
			return m, tea.Batch(
				fetchTUISessions(m.ipcClient),
				fetchTUIHistory(m.ipcClient, m.activeSession),
				fetchTUIStatus(m.ipcClient, m.activeSession),
			)
		}
		return m, fetchTUISessions(m.ipcClient)

	case tuiSessionRenamedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{
				Sender:    "⚠️",
				Text:      fmt.Sprintf("Failed to rename session: %s", msg.err),
				Timestamp: time.Now(),
			})
			m.updateViewport()
			return m, nil
		}
		// Update the session name in the local list.
		for i := range m.sessions {
			if m.sessions[i].ChatID == msg.chatID {
				m.sessions[i].Name = msg.name
				break
			}
		}
		m.updateViewport()
		return m, fetchTUISessions(m.ipcClient)

	case tuiProjectStateMsg:
		if msg.err == nil && msg.state != nil {
			m.projectState = msg.state
		}
		// Schedule next poll if panel is still open.
		if m.projectPanelOpen {
			return m, scheduleProjectStatePoll()
		}
		return m, nil

	case projectStatePollTickMsg:
		if m.projectPanelOpen && !m.waiting {
			return m, fetchTUIProjectState(m.ipcClient, m.activeSession)
		}
		return m, nil

	case clipboardPasteMsg:
		if msg.err != nil {
			errText := msg.err.Error()
			if errText == "" {
				errText = "unknown error"
			}
			m.messages = append(m.messages, chatMessage{
				Sender: "⚠️",
				Text:   fmt.Sprintf("Clipboard paste failed: %s. Use /img <path> instead.", errText),
			})
			m.updateViewport()
			return m, nil
		}
		// Add the clipboard image to pending images, tracking it as a temp
		// file that Aurelia owns and must clean up.
		errMsg := m.attachTempImage(msg.path)
		if errMsg != "" {
			// attachTempImage already removed the temp file on error.
			m.messages = append(m.messages, chatMessage{
				Sender: "⚠️",
				Text:   errMsg,
			})
			m.updateViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{
			Sender: "📎",
			Text:   "Image attached from clipboard",
		})
		m.updateViewport()
		return m, nil

	case clipboardCopyMsg:
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{
				Sender: "⚠️",
				Text:   fmt.Sprintf("Copy %s failed: %s", msg.label, msg.err),
			})
		} else {
			m.messages = append(m.messages, chatMessage{
				Sender: "📋",
				Text:   fmt.Sprintf("Copied %s to clipboard", msg.label),
			})
		}
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

// ensureDefaultSessionInList adds the default DM session to the list if
// it's not already present. The DM always exists implicitly.
func (m *Model) ensureDefaultSessionInList() {
	for _, s := range m.sessions {
		if s.ChatID == ipc.ReservedTUIChatID {
			return
		}
	}
	// Prepend the default DM so it's always first.
	m.sessions = append([]tuiSessionInfo{
		{ChatID: ipc.ReservedTUIChatID, Name: "dm"},
	}, m.sessions...)
	m.syncSidebarRows()
}

// handleKeyMsg processes keyboard input.
// enter submits when not waiting. alt+enter inserts a newline in the textarea.
// ctrl+j is also accepted as a portable newline fallback.
// When the sidebar is focused, ↑↓ navigate sessions, enter opens, n creates.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+o" {
		return m.toggleMouseCapture()
	}

	// Sidebar-focused mode: intercept navigation keys.
	if m.sidebarFocused && m.state == stateChat {
		return m.handleSidebarKey(msg)
	}

	switch {
	case msg.String() == "ctrl+c":
		m.cleanupTempImages()
		m.cleanupSubmittedTempImages()
		m.cleanupQueuedTempImages()
		return m, tea.Quit

	case msg.String() == "ctrl+l":
		m.messages = nil
		m.updateViewport()
		return m, nil

	case msg.String() == "ctrl+x":
		// Clear pending images and attachments.
		cleared := false
		if len(m.pendingImages) > 0 {
			m.clearPendingImages()
			cleared = true
		}
		if len(m.pendingAttachments) > 0 {
			m.clearPendingAttachments()
			cleared = true
		}
		if cleared {
			m.messages = append(m.messages, chatMessage{
				Sender: "📎",
				Text:   "Cleared pending images and documents",
			})
			m.updateViewport()
		}
		return m, nil

	case msg.String() == "ctrl+v":
		// Paste image from clipboard.
		return m, pasteFromClipboardCmd()

	case msg.String() == "ctrl+y":
		return m, copyChatToClipboardCmd(m.messages)

	case msg.String() == "ctrl+r":
		return m, copyLastAureliaToClipboardCmd(m.messages)

	case isAutocompleteKey(msg) && m.inputCommandPrefix() != "":
		if m.hasAutocomplete() {
			return m.cycleAutocomplete(), nil
		}
		return m.refreshAutocomplete(), nil

	// Help overlay: ? toggles it. esc, enter, or ? closes it.
	case key.Matches(msg, m.fullKeyMap().Help) && m.helpVisible():
		m.helpModel.ShowAll = false
		return m, nil
	case key.Matches(msg, m.fullKeyMap().Help) && m.textarea.Value() == "":
		m.helpModel.ShowAll = true
		return m, nil
	case (key.Matches(msg, m.fullKeyMap().Cancel) || key.Matches(msg, m.fullKeyMap().Submit)) && m.helpVisible():
		m.helpModel.ShowAll = false
		// If a stream is active, also cancel it so the user doesn't
		// need to press Esc twice.
		if m.waiting {
			return m.cancelStreaming()
		}
		return m, nil

	case isProjectPanelToggleKey(msg):
		m.projectPanelOpen = !m.projectPanelOpen
		if m.projectPanelOpen {
			return m, tea.Batch(
				fetchTUIProjectState(m.ipcClient, m.activeSession),
				scheduleProjectStatePoll(),
			)
		}
		return m, nil

	case isSidebarToggleKey(msg):
		m.showSidebar = !m.showSidebar
		m.updateViewport()
		return m, nil

	case isSidebarFocusKey(msg):
		if m.showSidebar && len(m.sessions) > 0 {
			m.sidebarFocused = true
			m.sidebarTable.Focus()
			m.syncSidebarRows()
			m.sidebarTable.SetCursor(m.sidebarCursor)
			// Set cursor to the active session if found.
			for i, s := range m.sessions {
				if s.ChatID == m.activeSession {
					m.sidebarCursor = i
					break
				}
			}
		}
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
		if m.renameTargetChatID != 0 {
			m.renameTargetChatID = 0
			m.textarea.Reset()
			m.textarea.Placeholder = "Type a message…"
			return m, nil
		}
		if m.waiting {
			return m.cancelStreaming()
		}
		return m, nil

	case msg.String() == "enter":
		// In rename mode, send the rename command.
		if m.renameTargetChatID != 0 {
			name := strings.TrimSpace(m.textarea.Value())
			if name != "" {
				cmd := renameTUISession(m.ipcClient, m.renameTargetChatID, name)
				m.renameTargetChatID = 0
				m.textarea.Reset()
				m.textarea.Placeholder = "Type a message…"
				return m, cmd
			}
			// Empty name — cancel rename.
			m.renameTargetChatID = 0
			m.textarea.Reset()
			m.textarea.Placeholder = "Type a message…"
			return m, nil
		}

		if m.hasAutocomplete() {
			return m.applyAutocomplete(), nil
		}

		// Submit.
		text := strings.TrimSpace(m.textarea.Value())
		if text == "" && len(m.pendingImages) == 0 && len(m.pendingAttachments) == 0 {
			return m, nil
		}

		// Handle /img command — attach image, don't send message.
		if strings.HasPrefix(text, "/img ") {
			path := strings.TrimPrefix(text, "/img ")
			errMsg := m.attachImageFromPath(path)
			m.textarea.Reset()
			if errMsg != "" {
				m.messages = append(m.messages, chatMessage{
					Sender: "⚠️",
					Text:   errMsg,
				})
				m.updateViewport()
			}
			return m, nil
		}

		// Handle bare /img (no path).
		if text == "/img" {
			m.textarea.Reset()
			m.messages = append(m.messages, chatMessage{
				Sender: "⚠️",
				Text:   "Usage: /img <path-to-image>",
			})
			m.updateViewport()
			return m, nil
		}

		// Handle /attach command — attach document, don't send message.
		if strings.HasPrefix(text, "/attach ") {
			path := strings.TrimPrefix(text, "/attach ")
			errMsg := m.attachDocumentFromPath(path)
			m.textarea.Reset()
			if errMsg != "" {
				m.messages = append(m.messages, chatMessage{
					Sender: "⚠️",
					Text:   errMsg,
				})
				m.updateViewport()
			}
			return m, nil
		}

		// Handle bare /attach (no path).
		if text == "/attach" {
			m.textarea.Reset()
			m.messages = append(m.messages, chatMessage{
				Sender: "⚠️",
				Text:   "Usage: /attach <path>",
			})
			m.updateViewport()
			return m, nil
		}

		// Free-form image paths are message input, not command arguments. Keep
		// slash commands untouched, except when the input itself starts with a
		// local path such as /Users/me/screenshot.png. We use a syntactic check
		// (file existence not required) so a missing/invalid file still routes
		// through image error handling instead of being treated as a command.
		if !strings.HasPrefix(text, "/") || startsWithSyntacticImagePath(text) {
			cleanedText, attachedCount, errMsg := m.attachImagePathsFromText(text)
			if errMsg != "" {
				m.messages = append(m.messages, chatMessage{
					Sender: "⚠️",
					Text:   errMsg,
				})
				m.updateViewport()
				return m, nil
			}
			if attachedCount > 0 {
				text = cleanedText
				if text == "" && len(m.pendingImages) == 0 && len(m.pendingAttachments) == 0 {
					return m, nil
				}
			}
		}

		if isBareModelCommand(text) {
			m.textarea.Reset()
			return m.openModelSelect()
		}

		if m.waiting && len(m.pendingQueue) >= maxPendingQueue {
			m.messages = append(m.messages, chatMessage{
				Sender:    "⚠️",
				Text:      fmt.Sprintf("Queue full: %d pending messages", maxPendingQueue),
				Timestamp: time.Now(),
			})
			m.updateViewport()
			return m, nil
		}

		m.rememberInput(text)
		m.textarea.Reset()

		// Build display text with image and attachment badges.
		displayText := text
		var badgeLines []string
		if badges := m.pendingImageBadges(); badges != "" {
			badgeLines = append(badgeLines, badges)
		}
		if badges := m.pendingAttachmentBadges(); badges != "" {
			badgeLines = append(badgeLines, badges)
		}
		if len(badgeLines) > 0 {
			combined := strings.Join(badgeLines, "\n")
			if displayText != "" {
				displayText = combined + "\n" + displayText
			} else {
				displayText = combined
			}
		}

		m.messages = append(m.messages, chatMessage{
			Sender:    "Igor",
			Text:      displayText,
			Timestamp: time.Now(),
		})

		isCommand := strings.HasPrefix(text, "/")
		queued := queuedMessage{
			chatID:         m.activeSession,
			text:           text,
			displayText:    displayText,
			images:         m.toIPCImages(),
			attachments:    m.toIPCAttachments(),
			tempImagePaths: m.tempImagePaths(),
			isCommand:      isCommand,
		}
		if m.waiting || m.pendingCount() > 0 {
			if err := m.enqueueMessage(queued); err != nil {
				m.messages = append(m.messages, chatMessage{
					Sender:    "⚠️",
					Text:      err.Error(),
					Timestamp: time.Now(),
				})
			}
			m.pendingImages = nil
			m.pendingAttachments = nil
			m.updateViewport()
			if !m.waiting && m.pendingCount() > 0 {
				return m.startQueuedMessage()
			}
			return m, nil
		}

		m.waiting = true
		m.streamID++
		progressCmd := (&m).initStreamProgress()
		m.updateViewport()

		if isCommand {
			return m, tea.Batch(m.sendCommand(text), spinnerTickCmd(), progressCmd)
		}

		// Capture pending images before clearing. Clipboard temp files must stay
		// on disk until the daemon has consumed the IPC image paths, so clean
		// them up only after the submitted stream reaches a terminal state.
		tempPaths := m.tempImagePaths()
		cmd := m.submitMessage(text)
		m.submittedTempImagePaths = append(m.submittedTempImagePaths, tempPaths...)
		m.pendingImages = nil
		m.pendingAttachments = nil
		return m, tea.Batch(cmd, spinnerTickCmd(), progressCmd)

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

func (m Model) toggleMouseCapture() (tea.Model, tea.Cmd) {
	m.mouseEnabled = !m.mouseEnabled
	return m, nil
}


func (m Model) openSidebarSessionAt(row int) (tea.Model, tea.Cmd) {
	if row < 0 || row >= len(m.sessions) { return m, nil }
	m.sidebarCursor = row
	m.syncSidebarRows()
	target := m.sessions[row]
	if target.ChatID == m.activeSession {
		if m.sidebarFocused { m.sidebarFocused = false; m.sidebarTable.Blur() }
		return m, nil
	}
	if m.warnSessionChangeWhileStreaming() { return m, nil }
	return m, openTUISession(m.ipcClient, target.ChatID)
}

func (m Model) handleSidebarMouse(msg tea.MouseMsg) (handled bool, model tea.Model, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || !sidebarMouseHitX(msg.X) { return false, m, nil }
		row := m.sidebarRowAt(msg.Y)
		if row < 0 { return true, m, nil }
		model, cmd := m.openSidebarSessionAt(row)
		return true, model, cmd
	case tea.MouseMotionMsg:
		if sidebarMouseHitX(msg.X) {
			row := m.sidebarRowAt(msg.Y)
			if row != m.sidebarHoverRow { m.sidebarHoverRow = row; m.syncSidebarRows() }
			return true, m, nil
		}
		if m.sidebarHoverRow != -1 { m.sidebarHoverRow = -1; m.syncSidebarRows() }
		return false, m, nil
	default:
		return false, m, nil
	}
}

// handleSidebarKey processes keys when the sidebar is focused.
func (m Model) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var tableCmd tea.Cmd
	switch msg.String() {
	case "up", "k":
		if m.sidebarCursor > 0 {
			m.sidebarCursor--
		}
	case "down", "j":
		if m.sidebarCursor < len(m.sessions)-1 {
			m.sidebarCursor++
		}
	default:
		updatedTable, cmd := m.sidebarTable.Update(msg)
		m.sidebarTable = updatedTable
		tableCmd = cmd
		m.sidebarCursor = m.sidebarTable.Cursor()
	}
	m.syncSidebarRows()

	switch msg.String() {
	case "ctrl+c":
		m.cleanupTempImages()
		m.cleanupSubmittedTempImages()
		m.cleanupQueuedTempImages()
		return m, tea.Quit

	case "esc", "tab", "ctrl+i", "\t":
		// Exit sidebar focus — return to input.
		m.sidebarFocused = false
		m.sidebarTable.Blur()
		if msg.String() == "tab" || msg.String() == "ctrl+i" || msg.String() == "\t" {
			// Tab also toggles sidebar visibility.
			m.showSidebar = !m.showSidebar
			m.updateViewport()
		}
		return m, nil

	case "enter":
		return m.openSidebarSessionAt(m.sidebarCursor)

	case "n":
		if m.warnSessionChangeWhileStreaming() {
			return m, nil
		}
		// Create a new session — use a default name based on count.
		name := fmt.Sprintf("session-%d", len(m.sessions))
		return m, createTUISession(m.ipcClient, name)

	case "d":
		// Delete the selected session (not the default DM).
		if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sessions) {
			return m, nil
		}
		if m.warnSessionChangeWhileStreaming() {
			return m, nil
		}
		target := m.sessions[m.sidebarCursor]
		if target.ChatID == ipc.ReservedTUIChatID {
			// Can't delete the default DM — show a message.
			m.messages = append(m.messages, chatMessage{
				Sender:    "⚠️",
				Text:      "Cannot delete the default DM session.",
				Timestamp: time.Now(),
			})
			m.updateViewport()
			return m, nil
		}
		return m, deleteTUISession(m.ipcClient, target.ChatID)

	case "r":
		// Rename the selected session (not the default DM).
		if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sessions) {
			return m, nil
		}
		target := m.sessions[m.sidebarCursor]
		if target.ChatID == ipc.ReservedTUIChatID {
			m.messages = append(m.messages, chatMessage{
				Sender:    "⚠️",
				Text:      "Cannot rename the default DM session.",
				Timestamp: time.Now(),
			})
			m.updateViewport()
			return m, nil
		}
		// Enter rename mode: show the current name as placeholder, clear textarea.
		m.renameTargetChatID = target.ChatID
		m.sidebarFocused = false
		m.sidebarTable.Blur()
		m.textarea.Reset()
		m.textarea.Placeholder = fmt.Sprintf("Rename '%s' to...", target.Name)
		m.textarea.Focus()
		return m, nil

	default:
		return m, tableCmd
	}
}

func (m Model) delegateKeyToTextarea(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if isTerminalColorReportResidue(msg) {
		return m, nil
	}
	if !shouldDelegateKeyToTextarea(msg, m.textarea.KeyMap) {
		return m, nil
	}

	// Check if this is a paste event with an image path (drag-and-drop).
	// In v2, paste is handled via tea.PasteMsg, but we check for printable
	// multi-character text from KeyPressMsg as fallback.
	if k := msg.Key(); len(k.Text) > 1 {
		text := strings.TrimSpace(k.Text)
		if isImagePath(text) {
			// Treat as image attachment, not text input.
			errMsg := m.attachImageFromPath(text)
			if errMsg != "" {
				m.messages = append(m.messages, chatMessage{
					Sender: "⚠️",
					Text:   errMsg,
				})
				m.updateViewport()
			} else {
				m.messages = append(m.messages, chatMessage{
					Sender: "📎",
					Text:   fmt.Sprintf("Image attached: %s", filepath.Base(text)),
				})
				m.updateViewport()
			}
			return m, nil
		}

		// Check if this is a paste event with a document path (drag-and-drop).
		if looksLikeFilePath(text) {
			// Normalize the text before passing to attachDocumentFromPath
			// (strips quotes, file:// prefix, unescapes spaces).
			normalized := normalizeImagePath(strings.TrimSpace(text))
			errMsg := m.attachDocumentFromPath(normalized)
			if errMsg != "" {
				m.messages = append(m.messages, chatMessage{
					Sender: "⚠️",
					Text:   errMsg,
				})
			} else {
				// Use the actual filename from the pending attachment.
				name := filepath.Base(normalized)
				if len(m.pendingAttachments) > 0 {
					name = m.pendingAttachments[len(m.pendingAttachments)-1].name
				}
				m.messages = append(m.messages, chatMessage{
					Sender: "📎",
					Text:   fmt.Sprintf("Document attached: %s", name),
				})
			}
			m.updateViewport()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.clearAutocomplete()
	return m, cmd
}

func isAutocompleteKey(msg tea.KeyMsg) bool {
	if msg.String() == "?" {
		return true
	}
	return isSidebarToggleKey(msg)
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
// Closing the reader closes the IPC connection, which cancels the daemon-side
// handler context, which aborts the pipeline.
func (m Model) cancelStreaming() (tea.Model, tea.Cmd) {
	m.waiting = false
	m.streamID++
	m.cleanupSubmittedTempImages()
	if m.reader != nil {
		_ = m.reader.Close()
		m.reader = nil
	}
	m.streamBuf = ""
	m.messages = append(m.messages, chatMessage{
		Sender:    "⚠️",
		Text:      "(cancelled — pipeline aborting)",
		Timestamp: time.Now(),
	})
	m.updateViewport()
	return m.continueWithNextQueuedMessage()
}

func isViewportScrollKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "pgup", "pgdown":
		return true
	}
	return false
}

func isTerminalColorReportResidue(msg tea.KeyMsg) bool {
	k := msg.Key()
	return k.Text != "" && terminalColorReportPattern.MatchString(k.Text)
}

func shouldDelegateKeyToTextarea(msg tea.KeyMsg, keyMap textarea.KeyMap) bool {
	k := msg.Key()
	if k.Mod&tea.ModAlt == 0 || k.Text == "" {
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
	if msg.Key().Code == tea.KeyTab {
		return true
	}
	s := msg.String()
	return s == "tab" || s == "ctrl+i" || s == "\t"
}

// isSidebarFocusKey returns true for ctrl+s or f2, which focus the
// sidebar for session navigation. ctrl+s is the primary; f2 is the
// fallback for terminals that intercept ctrl+s as XOFF flow control.
func isSidebarFocusKey(msg tea.KeyMsg) bool {
	s := msg.String()
	return s == "ctrl+s" || s == "f2"
}

// isProjectPanelToggleKey returns true for ctrl+p, which toggles the
// project state overlay panel.
func isProjectPanelToggleKey(msg tea.KeyMsg) bool {
	return msg.String() == "ctrl+p"
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
		m.updateStreamProgress(len(event.Body))
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
		m.resetStreamProgress()
		m.cleanupSubmittedTempImages()
		if m.reader != nil {
			_ = m.reader.Close()
			m.reader = nil
		}
		m.streamBuf = ""
		m.updateViewport()
		return m.continueWithNextQueuedMessage()

	case "error":
		// Terminal event — error.
		m.waiting = false
		m.resetStreamProgress()
		m.cleanupSubmittedTempImages()
		if m.reader != nil {
			_ = m.reader.Close()
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
		return m.continueWithNextQueuedMessage()
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
	streamID := m.streamID
	return func() tea.Msg {
		event, err := m.reader.Read()
		if err == io.EOF {
			_ = m.reader.Close()
			return streamDoneMsg{streamID: streamID}
		}
		if err != nil {
			_ = m.reader.Close()
			return streamErrMsg{err: err, streamID: streamID}
		}
		return streamEventMsg{event: event, streamID: streamID}
	}
}

// warnSessionChangeWhileStreaming appends a warning when session mutations
// are attempted during an active stream.
func (m *Model) warnSessionChangeWhileStreaming() bool {
	if !m.waiting {
		return false
	}
	m.messages = append(m.messages, chatMessage{
		Sender:    "⚠️",
		Text:      "Wait for the current response to finish before changing sessions.",
		Timestamp: time.Now(),
	})
	m.updateViewport()
	return true
}
