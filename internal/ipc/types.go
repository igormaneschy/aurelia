// Package ipc implements the IPC layer between the Aurelia daemon (aureliad)
// and the TUI client (aurelia-tui). Communication is over a Unix socket
// using newline-delimited JSON (JSON lines) for streaming responses.
package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
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
	// MsgTypeSessions requests the list of TUI local sessions.
	MsgTypeSessions = "sessions"
	// MsgTypeSessionCreate creates a new TUI local session with a name.
	// The daemon assigns a ChatID from the reserved range.
	MsgTypeSessionCreate = "session_create"
	// MsgTypeSessionOpen opens/switches to an existing TUI local session.
	// The ChatID field selects the session to activate.
	MsgTypeSessionOpen = "session_open"
	// MsgTypeSessionDelete removes a TUI local session.
	MsgTypeSessionDelete = "session_delete"
	// MsgTypeProjectState requests a full project state snapshot for the panel.
	MsgTypeProjectState = "project_state"
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
	// EventTypeSessions returns JSON-encoded session list in Body.
	EventTypeSessions = "sessions"
	// EventTypeSessionCreated returns JSON-encoded created session in Body.
	EventTypeSessionCreated = "session_created"
	// EventTypeSessionOpened returns JSON-encoded opened session in Body.
	EventTypeSessionOpened = "session_opened"
	// EventTypeSessionDeleted confirms a session was deleted.
	EventTypeSessionDeleted = "session_deleted"
	// EventTypeProjectState returns the project state snapshot JSON in Body.
	EventTypeProjectState = "project_state"
)

// MaxImageCount is the maximum number of images per message.
const MaxImageCount = 10

// MaxTotalImageBytes is the maximum total size of all images (15 MB).
const MaxTotalImageBytes = 15 * 1024 * 1024

// MaxAttachmentCount is the maximum number of document attachments per message.
const MaxAttachmentCount = 10

// MaxAttachmentBytes is the maximum file size for a single attachment (25 MB).
const MaxAttachmentBytes = 25 * 1024 * 1024

// MaxTotalAttachmentBytes is the maximum total size of all attachments (100 MB).
const MaxTotalAttachmentBytes = 100 * 1024 * 1024

// IPCImage represents an image sent from the TUI to the daemon.
// The TUI sends file paths (not base64) because the daemon reads files
// from the local filesystem and base64-encodes them for the bridge.
type IPCImage struct {
	// Path is the filesystem path to the image file (preferred for local IPC).
	Path string `json:"path,omitempty"`
	// Data is base64-encoded image data (fallback; not used in MVP).
	Data string `json:"data,omitempty"`
	// MediaType is the MIME type (e.g. "image/png", "image/jpeg").
	MediaType string `json:"media_type"`
}

// IPCAttachment represents a document file attached to a TUI message.
// The TUI sends a filesystem path; the daemon copies the file into
// <cwd>/uploads/ before forwarding the message to the pipeline.
type IPCAttachment struct {
	// Path is the absolute filesystem path to the document file.
	Path string `json:"path"`
	// Name is an optional display name (defaults to basename of Path).
	Name string `json:"name,omitempty"`
}

// IPCMessage is sent from the TUI client to the daemon.
type IPCMessage struct {
	Type     string `json:"type"`
	ChatID   int64  `json:"chat_id,omitempty"`
	ThreadID int64  `json:"thread_id,omitempty"`
	UserID   int64  `json:"user_id,omitempty"`
	Text     string `json:"text,omitempty"`
	// Images are optional image attachments for the message.
	Images []IPCImage `json:"images,omitempty"`
	// Attachments are optional document attachments for the message.
	Attachments []IPCAttachment `json:"attachments,omitempty"`
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

// ProjectStatePayload is the JSON payload for EventTypeProjectState.
type ProjectStatePayload struct {
	CWD             string                    `json:"cwd"`
	BindingSource   string                    `json:"binding_source"`
	BindingFrom     string                    `json:"binding_from,omitempty"`
	ActiveAgent     string                    `json:"active_agent"`
	Model           string                    `json:"model"`
	BridgeStatus    string                    `json:"bridge_status"`
	MemoryLayers    []ProjectStateMemoryLayer `json:"memory_layers"`
	CheckpointLayer string                    `json:"checkpoint_layer"`
	LatestRun       *ProjectStateRun          `json:"latest_run,omitempty"`
}

// ProjectStateMemoryLayer describes one memory layer in the project state.
type ProjectStateMemoryLayer struct {
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	Exists    bool   `json:"exists"`
	FileCount int    `json:"file_count"`
}

// ProjectStateRun describes the latest run in the project state.
type ProjectStateRun struct {
	Status     string    `json:"status"`
	Checkpoint string    `json:"checkpoint,omitempty"`
	AgentName  string    `json:"agent_name,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms,omitempty"`
}
