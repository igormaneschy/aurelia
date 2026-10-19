package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/tuisessions"
)

// maxSessionNameLength is the maximum length of a sanitized session name.
const maxSessionNameLength = 64

// emitSessionError emits an error event followed by the terminal event.
//
// The IPC protocol requires every request to end with a terminal event
// (stream_end). A handler that returned after only the error event left the
// client waiting forever: the desktop client blocked on "A carregar
// histórico…" when opening a session that no longer existed, because the
// error event alone never closed the request.
func emitSessionError(emit func(ipc.IPCEvent) error, requestID string, message string) error {
	if err := emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: message, RequestID: requestID}); err != nil {
		return err
	}
	return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: requestID})
}

// sanitizeSessionName removes terminal-control characters and limits length.
// Strips ANSI escape sequences (ESC), OSC sequences, and all C0 control
// characters (NUL..US, DEL). The result is trimmed and truncated to
// maxSessionNameLength runes so it renders safely in the sidebar.
func sanitizeSessionName(name string) string {
	// Remove ANSI escape sequences: ESC [ ... <letter>
	// Remove OSC sequences: ESC ] ... ST (BEL or ESC \)
	cleaned := name
	for {
		before := cleaned
		cleaned = stripAnsiOrOsc(cleaned)
		if cleaned == before {
			break
		}
	}
	// Remove C0 (0x00-0x1F), DEL (0x7F), C1 (U+0080-U+009F), and
	// any U+FFFD replacement characters (invalid UTF-8 bytes become
	// replacement chars when converted to runes in Go).
	var b strings.Builder
	for _, r := range cleaned {
		if r < 0x20 || r == 0x7F || r == 0xFFFD || (r >= 0x80 && r <= 0x9F) {
			continue
		}
		b.WriteRune(r)
	}
	cleaned = strings.TrimSpace(b.String())
	// Limit to maxSessionNameLength runes.
	runes := []rune(cleaned)
	if len(runes) > maxSessionNameLength {
		runes = runes[:maxSessionNameLength]
		// Re-trim to avoid ending on a partial character or space.
		cleaned = strings.TrimSpace(string(runes))
	}
	return cleaned
}

// stripAnsiOrOsc removes a single ANSI escape or OSC sequence from the start
// of a string, or returns the string unchanged if no sequence is found.
// ANSI: ESC [ (parameter bytes) (intermediate bytes) (final byte)
// OSC: ESC ] (payload) (ST = ESC \ or BEL)
func stripAnsiOrOsc(s string) string {
	idx := strings.IndexRune(s, '\x1b')
	if idx < 0 {
		return s
	}
	if idx > 0 {
		return s[:idx] + stripAnsiOrOsc(s[idx:])
	}
	// s starts with ESC
	if len(s) < 2 {
		return s
	}
	switch s[1] {
	case '[':
		// ANSI CSI sequence: ESC [ parameters... intermediate... final
		i := 2
		// Parameter bytes: 0x30-0x3F
		for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3F {
			i++
		}
		// Intermediate bytes: 0x20-0x2F
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2F {
			i++
		}
		// Final byte: 0x40-0x7E
		if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7E {
			i++
		}
		if i >= len(s) {
			return ""
		}
		return s[i:]
	case ']':
		// OSC sequence: ESC ] ... ST (BEL 0x07 or ESC \)
		for i := 2; i < len(s); i++ {
			if s[i] == '\x07' {
				return s[i+1:]
			}
			if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
				return s[i+2:]
			}
		}
		// No terminator found — strip from ESC onward.
		return ""
	default:
		// Other ESC sequences: remove this ESC and continue.
		return s[1:]
	}
}

// sessionJSON is the wire format for a TUI session in IPC events.
type sessionJSON struct {
	ChatID       int64  `json:"chat_id"`
	Name         string `json:"name"`
	MessageCount int    `json:"message_count"`
}

// handleTUISessions lists all TUI local sessions.
func handleTUISessions(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	if a.tuiSessions == nil {
		return emitSessionError(emit, msg.RequestID, "tui sessions store not available")
	}

	sessions, err := a.tuiSessions.List(ctx)
	if err != nil {
		return emitSessionError(emit, msg.RequestID, fmt.Sprintf("list sessions: %s", err))
	}

	userID := int64(os.Getuid())
	payload := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		count := 0
		if hist, err := loadTUIHistory(ctx, a, s.ChatID, 0, userID); err == nil {
			count = len(hist)
		}
		payload = append(payload, sessionJSON{
			ChatID:       s.ChatID,
			Name:         s.Name,
			MessageCount: count,
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return emitSessionError(emit, msg.RequestID, fmt.Sprintf("marshal sessions: %s", err))
	}

	if err := emit(ipc.IPCEvent{Type: ipc.EventTypeSessions, Body: string(body), RequestID: msg.RequestID}); err != nil {
		return err
	}
	return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
}

// handleTUISessionCreate creates a new TUI local session with the given name.
// Allocates the next unused ChatID below the current minimum (most negative)
// so that deleted session IDs are never reused.
func handleTUISessionCreate(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	if a.tuiSessions == nil {
		return emitSessionError(emit, msg.RequestID, "tui sessions store not available")
	}

	rawName := strings.TrimSpace(msg.Text)
	if rawName == "" {
		return emitSessionError(emit, msg.RequestID, "session name is required")
	}

	name := sanitizeSessionName(rawName)
	if name == "" {
		return emitSessionError(emit, msg.RequestID, "session name is empty after sanitization")
	}

	// Query the current minimum ChatID from the database so we allocate
	// below the most negative used ID. This avoids reusing deleted slots.
	// The Create attempt is retried if a concurrent create races us for
	// the same nextID (ErrSessionExists → try the next one below).
	nextID, err := a.tuiSessions.NextChatID(ctx)
	if err != nil {
		return emitSessionError(emit, msg.RequestID, fmt.Sprintf("next chat id: %s", err))
	}
	if nextID == -1 {
		nextID = ipc.ReservedTUIChatID - 1 // first free slot below DM
	}

	for {
		if nextID < ipc.ReservedTUIChatIDFloor {
			return emitSessionError(emit, msg.RequestID, "no free TUI session slots")
		}

		sess, err := a.tuiSessions.Create(ctx, nextID, name)
		if err == nil {
			body, _ := json.Marshal(sessionJSON{ChatID: sess.ChatID, Name: sess.Name})
			if emitErr := emit(ipc.IPCEvent{Type: ipc.EventTypeSessionCreated, Body: string(body), RequestID: msg.RequestID}); emitErr != nil {
				return emitErr
			}
			return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
		}
		if err == tuisessions.ErrSessionExists {
			// Race with a concurrent create — try the next slot.
			nextID--
			continue
		}
		return emitSessionError(emit, msg.RequestID, fmt.Sprintf("create session: %s", err))
	}
}

// handleTUISessionOpen opens/switches to an existing TUI local session.
// The ChatID in the message selects which session to activate.
// The default DM (ReservedTUIChatID) always exists implicitly — it is
// never created via session_create and may not have a row in the store.
func handleTUISessionOpen(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	if a.tuiSessions == nil {
		return emitSessionError(emit, msg.RequestID, "tui sessions store not available")
	}

	chatID := msg.ChatID
	if !ipc.IsReservedTUIID(chatID) {
		return emitSessionError(emit, msg.RequestID, "invalid session chat_id")
	}

	// The default DM exists implicitly — don't require a store row.
	if ipc.IsDefaultTUISession(chatID) {
		if err := emit(ipc.IPCEvent{
			Type:      ipc.EventTypeSessionOpened,
			Body:      fmt.Sprintf(`{"chat_id":%d,"name":"dm"}`, chatID),
			RequestID: msg.RequestID,
		}); err != nil {
			return err
		}
		return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
	}

	sess, err := a.tuiSessions.Get(ctx, chatID)
	if err == tuisessions.ErrSessionNotFound {
		return emitSessionError(emit, msg.RequestID, "session not found")
	}
	if err != nil {
		return emitSessionError(emit, msg.RequestID, fmt.Sprintf("get session: %s", err))
	}

	if err := a.tuiSessions.Touch(ctx, chatID); err != nil {
		return emitSessionError(emit, msg.RequestID, fmt.Sprintf("touch session: %s", err))
	}

	body, _ := json.Marshal(sessionJSON{ChatID: sess.ChatID, Name: sess.Name})
	if err := emit(ipc.IPCEvent{Type: ipc.EventTypeSessionOpened, Body: string(body), RequestID: msg.RequestID}); err != nil {
		return err
	}
	return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
}

// handleTUISessionRename renames an existing TUI local session.
func handleTUISessionRename(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	if a.tuiSessions == nil {
		return emitSessionError(emit, msg.RequestID, "tui sessions store not available")
	}

	chatID := msg.ChatID
	if !ipc.IsReservedTUIID(chatID) {
		return emitSessionError(emit, msg.RequestID, "invalid session chat_id")
	}
	if ipc.IsDefaultTUISession(chatID) {
		return emitSessionError(emit, msg.RequestID, "cannot rename the default DM session")
	}

	name := sanitizeSessionName(strings.TrimSpace(msg.Text))
	if name == "" {
		return emitSessionError(emit, msg.RequestID, "session name is required")
	}

	if err := a.tuiSessions.Rename(ctx, chatID, name); err != nil {
		if err == tuisessions.ErrSessionNotFound {
			return emitSessionError(emit, msg.RequestID, "session not found")
		}
		return emitSessionError(emit, msg.RequestID, fmt.Sprintf("rename session: %s", err))
	}

	body, _ := json.Marshal(sessionJSON{ChatID: chatID, Name: name})
	if err := emit(ipc.IPCEvent{Type: ipc.EventTypeSessionRenamed, Body: string(body), RequestID: msg.RequestID}); err != nil {
		return err
	}
	return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
}

// handleTUISessionDelete removes a TUI local session.
func handleTUISessionDelete(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	if a.tuiSessions == nil {
		return emitSessionError(emit, msg.RequestID, "tui sessions store not available")
	}

	chatID := msg.ChatID
	if !ipc.IsReservedTUIID(chatID) {
		return emitSessionError(emit, msg.RequestID, "invalid session chat_id")
	}

	// Prevent deleting the default DM session.
	if ipc.IsDefaultTUISession(chatID) {
		return emitSessionError(emit, msg.RequestID, "cannot delete the default DM session")
	}

	if err := a.tuiSessions.Delete(ctx, chatID); err != nil {
		return emitSessionError(emit, msg.RequestID, fmt.Sprintf("delete session: %s", err))
	}

	// Also clean up session state and project binding for this ChatID.
	if a.sessions != nil {
		a.sessions.Clear(chatID, 0)
	}
	if a.bindings != nil {
		_ = a.bindings.Delete(ctx, projectbinding.ConversationKey{ChatID: chatID, ThreadID: 0})
	}

	if err := emit(ipc.IPCEvent{Type: ipc.EventTypeSessionDeleted, RequestID: msg.RequestID}); err != nil {
		return err
	}
	return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
}
