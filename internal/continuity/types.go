package continuity

import "time"

// ConversationKey uniquely identifies a conversation state by chat, thread, and user.
// For private chats and non-forum groups, UserID provides per-user isolation so that
// in multi-user groups, each user's continuity context is stored separately.
// During transition, UserID may be 0 — callers should prefer the 3-arg constructor.
type ConversationKey struct {
	ChatID   int64
	ThreadID int
	UserID   int64
}

// ConversationState is the durable recovery context for a chat/thread/user triple.
// All text fields SHALL be redacted and length-capped before persistence.
type ConversationState struct {
	ChatID   int64
	ThreadID int
	UserID   int64
	CWD      string

	ActiveGoal           string
	LastUserIntent       string
	LastAssistantSummary string
	LastCheckpoint       string

	LastRunID     string
	LastRunStatus string
	LastTools     string

	SessionID   string
	SessionCold bool
	ResetReason string

	UpdatedAt time.Time
}

// StatePatch carries optional fields for partial updates. Nil pointer means
// "do not update this field", avoiding accidental zero-value overwrites.
type StatePatch struct {
	CWD                  *string
	ActiveGoal           *string
	LastUserIntent       *string
	LastAssistantSummary *string
	LastCheckpoint       *string
	LastRunID            *string
	LastRunStatus        *string
	LastTools            *string
	SessionID            *string
	SessionCold          *bool
	ResetReason          *string
	UpdatedAt            time.Time
}

// Data caps — all truncation must be rune-safe.
const (
	MaxActiveGoal           = 300
	MaxUserIntent           = 500
	MaxAssistantSummary     = 900
	MaxCheckpoint           = 1200
	MaxTools                = 700
	MaxContinuityBlockChars = 2000

	// LegacyConversationKeyUserID is used when upgrading from the old 2-column
	// primary key (chat_id, thread_id) to the new 3-column (chat_id, thread_id, user_id).
	// Rows with user_id=0 were created before the migration.
	LegacyConversationKeyUserID int64 = 0
)

// RetentionThreshold is the maximum age of a ConversationState we consider
// fresh enough for automatic prompt injection (7 days).
const RetentionThreshold = 7 * 24 * time.Hour

// ConversationKeyFor returns a ConversationKey with all three fields.
// Use this constructor to avoid omitting UserID.
func ConversationKeyFor(chatID int64, threadID int, userID int64) ConversationKey {
	return ConversationKey{ChatID: chatID, ThreadID: threadID, UserID: userID}
}

// FreshThreshold is the boundary for "hot" state — the session is likely still
// warm and continuity can be skipped to save tokens.
const FreshThreshold = 5 * time.Minute

// StaleThreshold is the boundary for "stale" state — continuity recovery
// context is unlikely to be useful beyond this point.
const StaleThreshold = 6 * time.Hour
