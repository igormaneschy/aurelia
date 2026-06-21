// Package tuisessions persists metadata for TUI local sessions.
//
// Each TUI session is identified by a reserved ChatID in the range
// [ipc.ReservedTUIChatIDFloor, ipc.ReservedTUIChatID]. The store holds
// the human-readable name and timestamps so the TUI sidebar can list
// and reopen sessions across restarts.
package tuisessions

import (
	"context"
	"fmt"
	"time"
)

// Session is the persisted metadata for one TUI local session.
type Session struct {
	ChatID     int64     // reserved TUI ChatID
	Name       string    // user-facing label (e.g. "dm", "work")
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// Store persists TUI session metadata.
type Store interface {
	List(ctx context.Context) ([]Session, error)
	Get(ctx context.Context, chatID int64) (*Session, error)
	Create(ctx context.Context, chatID int64, name string) (*Session, error)
	Rename(ctx context.Context, chatID int64, name string) error
	Touch(ctx context.Context, chatID int64) error
	Delete(ctx context.Context, chatID int64) error
	// NextChatID returns the next available ChatID for a new session.
	// It queries MIN(chat_id)-1 atomically so concurrent callers
	// always see the latest state.
	NextChatID(ctx context.Context) (int64, error)
	Close() error
}

// ErrSessionExists is returned when Create is called with a ChatID that
// already has a session row.
var ErrSessionExists = fmt.Errorf("tui session already exists")

// ErrSessionNotFound is returned when Get or Touch is called for a ChatID
// that has no session row.
var ErrSessionNotFound = fmt.Errorf("tui session not found")
