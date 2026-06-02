package session

import "sync"

const maxNudgeBufferedMessages = 40

// NudgeMessage holds one side of a conversation turn.
type NudgeMessage struct {
	Role    string // "user" or "assistant"
	Content string
	// ToolSummary, when non-empty, describes tool calls that happened during
	// the assistant turn. Populated by AddToolEvent; never set by AddTurn.
	ToolSummary string
}

// NudgeBuffer accumulates conversation turns per chat thread for periodic nudge review.
// Snapshot returns a version token; Commit only acts when the token matches the current
// buffer version, preventing stale commits from overwriting newer data.
type NudgeBuffer struct {
	mu       sync.Mutex
	messages map[SessionKey][]NudgeMessage
	turns    map[SessionKey]int
	versions map[SessionKey]uint64 // incremented on every mutation
}

// NewNudgeBuffer creates a new buffer.
func NewNudgeBuffer() *NudgeBuffer {
	return &NudgeBuffer{
		messages: make(map[SessionKey][]NudgeMessage),
		turns:    make(map[SessionKey]int),
		versions: make(map[SessionKey]uint64),
	}
}

// bumpVersion increments the mutation counter for a key.
func (b *NudgeBuffer) bumpVersion(key SessionKey) {
	b.versions[key]++
}

// AddTurn records a user+assistant exchange for the given chat and user.
// When the buffer exceeds maxNudgeBufferedMessages, the oldest messages are
// dropped to stay within the limit.
func (b *NudgeBuffer) AddTurn(chatID int64, threadID int, userID int64, userMsg, assistantMsg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, userID)
	b.messages[key] = append(b.messages[key],
		NudgeMessage{Role: "user", Content: userMsg},
		NudgeMessage{Role: "assistant", Content: assistantMsg},
	)
	b.turns[key]++

	// Enforce max buffer size — drop oldest messages when exceeded
	if len(b.messages[key]) > maxNudgeBufferedMessages {
		excess := len(b.messages[key]) - maxNudgeBufferedMessages
		// Round to even to keep message pairs intact
		if excess%2 != 0 {
			excess--
		}
		b.messages[key] = b.messages[key][excess:]
		// Recalculate turns: each turn is 2 messages
		b.turns[key] = len(b.messages[key]) / 2
	}
	b.bumpVersion(key)
}

// AddToolEvent appends tool call context to the most recent assistant message
// in the buffer. This enriches the transcript with tool_use/tool_result summaries
// without altering the AddTurn signature or message-pair structure.
// If the buffer is empty or the last message is not from the assistant, the
// event is silently dropped.
func (b *NudgeBuffer) AddToolEvent(chatID int64, threadID int, userID int64, toolSummary string) {
	if toolSummary == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, userID)
	msgs := b.messages[key]
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != "assistant" {
		return
	}
	last := &msgs[len(msgs)-1]
	if last.ToolSummary != "" {
		last.ToolSummary += "\n" + toolSummary
	} else {
		last.ToolSummary = toolSummary
	}
	b.bumpVersion(key)
}

// TurnCount returns how many turns have been buffered for the chat and user.
func (b *NudgeBuffer) TurnCount(chatID int64, threadID int, userID int64) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.turns[SessionKeyFor(chatID, threadID, userID)]
}

// Snapshot returns a copy of all buffered messages for the chat thread and user
// without clearing the buffer, plus a version token. Use Commit(token, count)
// to remove processed messages only if the buffer has not been modified since
// the snapshot was taken.
func (b *NudgeBuffer) Snapshot(chatID int64, threadID int, userID int64) ([]NudgeMessage, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, userID)
	msgs := b.messages[key]
	if len(msgs) == 0 {
		return nil, b.versions[key]
	}
	result := make([]NudgeMessage, len(msgs))
	copy(result, msgs)
	return result, b.versions[key]
}

// Commit removes the first count messages from the buffer, but only if the
// buffer version token matches the one returned by a prior Snapshot. If the
// buffer has been modified since the snapshot (e.g. by AddTurn or cap eviction),
// the commit is silently skipped — the caller must snapshot again.
// Count should be even (user+assistant pairs). If count is odd, the last message
// of the partial pair is silently dropped.
func (b *NudgeBuffer) Commit(chatID int64, threadID int, userID int64, version uint64, count int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, userID)
	if b.versions[key] != version {
		// Buffer was modified since snapshot — do not commit stale data
		return
	}
	msgs := b.messages[key]
	if count <= 0 || len(msgs) == 0 {
		return
	}
	if count >= len(msgs) {
		delete(b.messages, key)
		delete(b.turns, key)
		b.bumpVersion(key)
		return
	}
	// Guard against odd count: drop the last message of the partial pair
	if count%2 != 0 {
		count--
	}
	b.messages[key] = msgs[count:]
	b.turns[key] = len(b.messages[key]) / 2
	b.bumpVersion(key)
}

// GetAndReset returns all buffered messages for the chat and user and resets the buffer.
// Prefer Snapshot + Commit for retry-safe semantics.
func (b *NudgeBuffer) GetAndReset(chatID int64, threadID int, userID int64) []NudgeMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, userID)
	msgs := b.messages[key]
	delete(b.messages, key)
	delete(b.turns, key)
	b.bumpVersion(key)
	return msgs
}

// ClearUser removes all buffered messages for a user across all chats and
// threads. Called when a user invokes /forget-me to erase their data.
func (b *NudgeBuffer) ClearUser(userID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key := range b.messages {
		if key.UserID == userID {
			delete(b.messages, key)
			delete(b.turns, key)
			b.bumpVersion(key)
		}
	}
}

// TotalChars returns the total character count of all buffered messages.
func (b *NudgeBuffer) TotalChars(chatID int64, threadID int, userID int64) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, userID)
	total := 0
	for _, m := range b.messages[key] {
		total += len(m.Content)
		total += len(m.ToolSummary)
	}
	return total
}
