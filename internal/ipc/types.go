// Package ipc implements the IPC layer between the Aurelia daemon (aureliad)
// and the TUI client (aurelia-tui). Communication is over a Unix socket
// using newline-delimited JSON (JSON lines) for streaming responses.
package ipc

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultSocketName is the default basename for the Unix socket file.
const DefaultSocketName = "aurelia.sock"

// MaxMessageTextLength is the maximum allowed length for IPCMessage.Text.
const MaxMessageTextLength = 4000

// MaxRequestIDLength is the maximum allowed length for IPCMessage.RequestID.
const MaxRequestIDLength = 64

// ReservedTUIChatID is the reserved chat ID for the default TUI local DM
// conversation. This negative ID is in a namespace separate from Telegram's
// positive chat IDs.
const ReservedTUIChatID int64 = -9000001

// ReservedTUIChatIDFloor is the most negative chat ID reserved for TUI local
// sessions. Together with ReservedTUIChatID, this defines the TUI local
// namespace: [-9000009, -9000001] — 9 slots for named local sessions.
// ReservedTUIChatID (-9000001) is the default DM; -9000002..-9000009 are
// available for user-named sessions (e.g. "tui:work", "tui:research").
const ReservedTUIChatIDFloor int64 = -9000009

// IsReservedTUIID returns true if the chat ID is in the reserved TUI local
// namespace [ReservedTUIChatIDFloor, ReservedTUIChatID].
func IsReservedTUIID(chatID int64) bool {
	return chatID <= ReservedTUIChatID && chatID >= ReservedTUIChatIDFloor
}

// IsDefaultTUISession returns true if the chat ID is the default TUI DM.
func IsDefaultTUISession(chatID int64) bool {
	return chatID == ReservedTUIChatID
}

// DefaultSocketPath returns the default socket path under the Aurelia data
// directory (~/.aurelia).
func DefaultSocketPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".aurelia", DefaultSocketName), nil
}

// IPC message types sent from client to server.
const (
	// MsgTypeSend sends a chat message to the daemon for processing.
	MsgTypeSend = "send"
	// MsgTypeSubscribe subscribes to events for a given chat/thread.
	MsgTypeSubscribe = "subscribe"
	// MsgTypeCommand sends a command (e.g. "/cwd", "/status") to the daemon.
	MsgTypeCommand = "command"
	// MsgTypeHistory requests recent UI-safe transcript messages for the TUI.
	MsgTypeHistory = "history"
)

// IPC event types sent from server to client.
const (
	// EventTypeMessage is a full (non-streamed) response message.
	EventTypeMessage = "message"
	// EventTypeStreamChunk is a single chunk of a streaming response.
	EventTypeStreamChunk = "stream_chunk"
	// EventTypeStreamEnd signals the end of a streaming response.
	EventTypeStreamEnd = "stream_end"
	// EventTypeError signals an error from the daemon.
	EventTypeError = "error"
	// EventTypeAck is an acknowledgement that a message was received.
	EventTypeAck = "ack"
	// EventTypeHistory returns JSON-encoded history messages in Body.
	EventTypeHistory = "history"
)

// IPCMessage is sent from the TUI client to the daemon.
type IPCMessage struct {
	Type     string `json:"type"`
	ChatID   int64  `json:"chat_id,omitempty"`
	ThreadID int64  `json:"thread_id,omitempty"`
	UserID   int64  `json:"user_id,omitempty"`
	Text     string `json:"text,omitempty"`
	// RequestID is an optional correlation ID for matching responses.
	RequestID string `json:"request_id,omitempty"`
}

// IPCEvent is sent from the daemon to the TUI client in response to a message.
type IPCEvent struct {
	Type      string `json:"type"`
	Body      string `json:"body,omitempty"`
	Done      bool   `json:"done,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	// Error message, present when Type is EventTypeError.
	Error string `json:"error,omitempty"`
}
