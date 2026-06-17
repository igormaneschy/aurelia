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
// The daemon assigns the next available ChatID from the reserved range.
func handleTUISessionCreate(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	if a.tuiSessions == nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "tui sessions store not available", RequestID: msg.RequestID})
	}

	name := strings.TrimSpace(msg.Text)
	if name == "" {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "session name is required", RequestID: msg.RequestID})
	}

	chatID, err := allocateTUISessionChatID(ctx, a)
	if err != nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: err.Error(), RequestID: msg.RequestID})
	}

	sess, err := a.tuiSessions.Create(ctx, chatID, name)
	if err != nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("create session: %s", err), RequestID: msg.RequestID})
	}

	body, _ := json.Marshal(sessionJSON{ChatID: sess.ChatID, Name: sess.Name})
	if err := emit(ipc.IPCEvent{Type: ipc.EventTypeSessionCreated, Body: string(body), RequestID: msg.RequestID}); err != nil {
		return err
	}
	return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
}

// handleTUISessionOpen opens/switches to an existing TUI local session.
// The ChatID in the message selects which session to activate.
func handleTUISessionOpen(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	if a.tuiSessions == nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "tui sessions store not available", RequestID: msg.RequestID})
	}

	chatID := msg.ChatID
	if !ipc.IsReservedTUIID(chatID) {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "invalid session chat_id", RequestID: msg.RequestID})
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

// allocateTUISessionChatID finds the next available ChatID in the reserved
// TUI range [-9000009, -9000002]. The default DM (-9000001) is skipped — it
// always exists implicitly. Returns an error if all slots are taken.
func allocateTUISessionChatID(ctx context.Context, a *app) (int64, error) {
	existing, err := a.tuiSessions.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("list sessions for allocation: %w", err)
	}
	taken := make(map[int64]bool, len(existing))
	for _, s := range existing {
		taken[s.ChatID] = true
	}

	// Scan -9000002 down to -9000009 for the first free slot.
	for chatID := ipc.ReservedTUIChatID - 1; chatID >= ipc.ReservedTUIChatIDFloor; chatID-- {
		if !taken[chatID] {
			return chatID, nil
		}
	}
	return 0, fmt.Errorf("no free TUI session slots (all %d slots in use)", int(ipc.ReservedTUIChatID-ipc.ReservedTUIChatIDFloor))
}
