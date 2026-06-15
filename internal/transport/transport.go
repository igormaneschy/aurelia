// Package transport defines a generic interface for chat transport surfaces
// (Telegram, TUI, etc.). It sits below the pipeline Output layer and provides
// the common message send/receive operations that all transports support.
package transport

import "context"

// MessageHandle is an opaque, transport-specific token returned by
// Transport.Send. It can be passed to optional interfaces
// (DeletableTransport, ReactableTransport) for post-send operations
// such as delete and react.
//
// Callers MUST NOT inspect, interpret, or type-assert the handle's
// concrete value. The handle MUST NOT contain secrets, and MUST NOT be
// logged — it is an implementation detail of the transport layer.
//
// Examples: *telebot.Message for Telegram, an internal numeric ID for a TUI.
type MessageHandle any

// ImageAttachment represents an image sent alongside a message.
// The shape is compatible with bridge.ImageAttachment so callers can convert
// between the two as needed.
type ImageAttachment struct {
	Path      string `json:"path,omitempty"`
	Data      string `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

// IncomingMessage represents a message received from a chat transport.
type IncomingMessage struct {
	ChatID   int64
	ThreadID int
	UserID   int64
	Text     string
	Source   string // transport identifier, e.g. "telegram"
	Images   []ImageAttachment
}

// OutgoingMessage represents a message to send through a chat transport.
type OutgoingMessage struct {
	ChatID   int64
	ThreadID int
	Text     string
	Markdown bool // true if Text contains markdown to be rendered
}

// Transport defines the interface for sending and receiving messages across
// different chat surfaces. Each transport surface (Telegram, TUI, etc.)
// provides its own implementation.
type Transport interface {
	// Name returns the transport identifier (e.g. "telegram", "tui").
	Name() string

	// Send sends an outgoing message and returns an opaque handle.
	// The handle can be used with optional interfaces (DeletableTransport,
	// ReactableTransport) for transport-specific post-send operations.
	Send(ctx context.Context, msg OutgoingMessage) (MessageHandle, error)

	// SendError sends an error-formatted message to the chat.
	SendError(ctx context.Context, chatID int64, threadID int, text string) error

	// StartTyping starts a typing indicator. Returns a stop function.
	StartTyping(chatID int64, threadID int) func()

	// Receive returns a channel of incoming messages.
	// For push-based transports (Telegram), returns a closed channel.
	// For pull-based transports (TUI), the channel delivers messages as they arrive.
	Receive() <-chan IncomingMessage
}

// DeletableTransport is an optional interface for transports that can
// delete previously-sent messages.
type DeletableTransport interface {
	// Delete removes the message identified by the handle. Safe to call
	// with nil handle (no-op).
	Delete(ctx context.Context, handle MessageHandle) error
}

// ReactableTransport is an optional interface for transports that can
// add reactions (e.g. emoji) to messages.
type ReactableTransport interface {
	// React adds a reaction to the message identified by chatID and messageID.
	// Safe to call with messageID == 0 (no-op).
	React(ctx context.Context, chatID int64, messageID int) error
}
