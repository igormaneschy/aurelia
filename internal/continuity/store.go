package continuity

import "context"

// Store persists ConversationState for durable recovery across restarts,
// resets, timeouts, and cold sessions.
type Store interface {
	// Get retrieves the current state for a chat/thread/user, or nil if absent.
	// userID=0 matches legacy rows created before the user_id migration.
	Get(ctx context.Context, chatID int64, threadID int, userID int64) (*ConversationState, error)

	// Upsert fully replaces the state for a chat/thread/user.
	Upsert(ctx context.Context, state ConversationState) error

	// Patch applies partial updates without overwriting unset fields.
	Patch(ctx context.Context, key ConversationKey, patch StatePatch) error

	// MarkColdForSessions sets session_cold=1 and reset_reason for all rows
	// with a non-empty session_id. Used when the bridge process dies.
	MarkColdForSessions(ctx context.Context, reason string) error

	// GetProjectWork retrieves project work state for a (userID, projectSlug)
	// pair, or nil if absent.
	GetProjectWork(ctx context.Context, userID int64, projectSlug string) (*ProjectWorkState, error)

	// PatchProjectWork applies partial updates without overwriting nil fields.
	// Creates a new row if one doesn't exist. Returns an error if projectSlug
	// is empty.
	PatchProjectWork(ctx context.Context, key ProjectWorkKey, patch ProjectWorkPatch) error

	// Close releases the store's resources.
	Close() error
}
