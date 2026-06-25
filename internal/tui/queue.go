package tui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

const maxPendingQueue = 10

type queuedMessage struct {
	chatID         int64
	text           string
	displayText    string
	images         []ipc.IPCImage
	attachments    []ipc.IPCAttachment
	tempImagePaths []string
	isCommand      bool
}

func (m *Model) enqueueMessage(q queuedMessage) error {
	if strings.TrimSpace(q.text) == "" && len(q.images) == 0 && len(q.attachments) == 0 {
		return nil
	}
	if len(m.pendingQueue) >= maxPendingQueue {
		return fmt.Errorf("queue full: %d pending messages", maxPendingQueue)
	}
	m.pendingQueue = append(m.pendingQueue, q)
	return nil
}

func (m *Model) dequeueMessage() (queuedMessage, bool) {
	if len(m.pendingQueue) == 0 {
		return queuedMessage{}, false
	}
	q := m.pendingQueue[0]
	m.pendingQueue = m.pendingQueue[1:]
	return q, true
}

func (m Model) pendingCount() int {
	return len(m.pendingQueue)
}

func (m Model) startQueuedMessage() (Model, tea.Cmd) {
	q, ok := (&m).dequeueMessage()
	if !ok {
		return m, nil
	}
	m.waiting = true
	m.streamID++
	fileCount := len(q.images) + len(q.attachments)
	if fileCount > 0 {
		(&m).initAttachProgress(fileCount)
	}
	m.submittedTempImagePaths = append(m.submittedTempImagePaths, q.tempImagePaths...)
	if q.isCommand {
		return m, tea.Batch(m.sendCommandToSession(q.chatID, q.text, m.streamID), spinnerTickCmd())
	}
	return m, tea.Batch(m.submitMessageWithPayload(q.chatID, q.text, q.images, q.attachments, m.streamID), spinnerTickCmd())
}

func (m Model) continueWithNextQueuedMessage() (Model, tea.Cmd) {
	if m.pendingCount() == 0 {
		return m, nil
	}
	return m.startQueuedMessage()
}

func (m *Model) cleanupQueuedTempImages() {
	for _, q := range m.pendingQueue {
		for _, path := range q.tempImagePaths {
			_ = os.Remove(path)
		}
	}
	m.pendingQueue = nil
}
