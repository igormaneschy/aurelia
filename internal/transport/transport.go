// Package transport defines a generic interface for chat transport surfaces
// (Telegram, TUI, etc.). It sits below the pipeline Output layer and provides
// the common message send/receive operations that all transports support.
package transport

import "context"

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

	// Send sends an outgoing message. Returns error on failure.
	Send(ctx context.Context, msg OutgoingMessage) error

	// SendError sends an error-formatted message to the chat.
	SendError(ctx context.Context, chatID int64, threadID int, text string) error

	// StartTyping starts a typing indicator. Returns a stop function.
	StartTyping(chatID int64, threadID int) func()

	// Receive returns a channel of incoming messages.
	// For push-based transports (Telegram), returns a closed channel.
	// For pull-based transports (TUI), the channel delivers messages as they arrive.
	Receive() <-chan IncomingMessage
}
