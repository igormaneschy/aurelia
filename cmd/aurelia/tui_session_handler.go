package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/tuisessions"
)

// maxSessionNameLength is the maximum length of a sanitized session name.
const maxSessionNameLength = 64

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
	ChatID int64  `json:"chat_id"`
	Name   string `json:"name"`
}

// handleTUISessions lists all TUI local sessions.
func handleTUISessions(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	if a.tuiSessions == nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "tui sessions store not available", RequestID: msg.RequestID})
	}

	sessions, err := a.tuiSessions.List(ctx)
	if err != nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("list sessions: %s", err), RequestID: msg.RequestID})
	}

	payload := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		payload = append(payload, sessionJSON{ChatID: s.ChatID, Name: s.Name})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("marshal sessions: %s", err), RequestID: msg.RequestID})
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
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "tui sessions store not available", RequestID: msg.RequestID})
	}

	rawName := strings.TrimSpace(msg.Text)
	if rawName == "" {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "session name is required", RequestID: msg.RequestID})
	}

	name := sanitizeSessionName(rawName)
	if name == "" {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "session name is empty after sanitization", RequestID: msg.RequestID})
	}

	// Find the lowest (most negative) used ChatID and allocate the next one.
	// Uses an atomic retry loop: if the calculated nextID is taken (race with
	// a concurrent create), we try the next one below until we find a free slot
	// or hit the floor.
	sessions, err := a.tuiSessions.List(ctx)
	if err != nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("list sessions: %s", err), RequestID: msg.RequestID})
	}

	nextID := ipc.ReservedTUIChatID - 1
	if len(sessions) > 0 {
		minID := sessions[0].ChatID
		for _, s := range sessions {
			if s.ChatID < minID {
				minID = s.ChatID
			}
		}
		nextID = minID - 1
	}

	for {
		if nextID < ipc.ReservedTUIChatIDFloor {
			return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "no free TUI session slots", RequestID: msg.RequestID})
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
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("create session: %s", err), RequestID: msg.RequestID})
	}
}

// handleTUISessionOpen opens/switches to an existing TUI local session.
// The ChatID in the message selects which session to activate.
// The default DM (ReservedTUIChatID) always exists implicitly — it is
// never created via session_create and may not have a row in the store.
func handleTUISessionOpen(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	if a.tuiSessions == nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "tui sessions store not available", RequestID: msg.RequestID})
	}

	chatID := msg.ChatID
	if !ipc.IsReservedTUIID(chatID) {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "invalid session chat_id", RequestID: msg.RequestID})
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
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "session not found", RequestID: msg.RequestID})
	}
	if err != nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("get session: %s", err), RequestID: msg.RequestID})
	}

	if err := a.tuiSessions.Touch(ctx, chatID); err != nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("touch session: %s", err), RequestID: msg.RequestID})
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
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "tui sessions store not available", RequestID: msg.RequestID})
	}

	chatID := msg.ChatID
	if !ipc.IsReservedTUIID(chatID) {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "invalid session chat_id", RequestID: msg.RequestID})
	}
	if ipc.IsDefaultTUISession(chatID) {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "cannot rename the default DM session", RequestID: msg.RequestID})
	}

	name := sanitizeSessionName(strings.TrimSpace(msg.Text))
	if name == "" {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "session name is required", RequestID: msg.RequestID})
	}

	if err := a.tuiSessions.Rename(ctx, chatID, name); err != nil {
		if err == tuisessions.ErrSessionNotFound {
			return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "session not found", RequestID: msg.RequestID})
		}
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("rename session: %s", err), RequestID: msg.RequestID})
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
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "tui sessions store not available", RequestID: msg.RequestID})
	}

	chatID := msg.ChatID
	if !ipc.IsReservedTUIID(chatID) {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "invalid session chat_id", RequestID: msg.RequestID})
	}

	// Prevent deleting the default DM session.
	if ipc.IsDefaultTUISession(chatID) {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "cannot delete the default DM session", RequestID: msg.RequestID})
	}

	if err := a.tuiSessions.Delete(ctx, chatID); err != nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("delete session: %s", err), RequestID: msg.RequestID})
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
