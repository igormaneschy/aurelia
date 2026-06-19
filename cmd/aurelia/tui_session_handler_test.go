package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

func TestTUIHandler_SessionsListEmpty(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	if err := handler(ctx, ipc.IPCMessage{Type: ipc.MsgTypeSessions}, te.emit); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Expect: ack + sessions event + stream_end
	if te.count() < 3 {
		t.Fatalf("expected at least 3 events, got %d", te.count())
	}

	var sessionsEvent *ipc.IPCEvent
	for i, ev := range te.events {
		if ev.Type == ipc.EventTypeSessions {
			sessionsEvent = &te.events[i]
			break
		}
	}
	if sessionsEvent == nil {
		t.Fatal("no sessions event emitted")
	}

	var sessions []sessionJSON
	if err := json.Unmarshal([]byte(sessionsEvent.Body), &sessions); err != nil {
		t.Fatalf("unmarshal sessions body: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected empty session list, got %d", len(sessions))
	}
}

func TestTUIHandler_SessionCreateAndList(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	// Create a session named "work".
	if err := handler(ctx, ipc.IPCMessage{
		Type: ipc.MsgTypeSessionCreate,
		Text: "work",
	}, te.emit); err != nil {
		t.Fatalf("create handler error: %v", err)
	}

	var createdEvent *ipc.IPCEvent
	for i, ev := range te.events {
		if ev.Type == ipc.EventTypeSessionCreated {
			createdEvent = &te.events[i]
			break
		}
	}
	if createdEvent == nil {
		t.Fatal("no session_created event emitted")
	}

	var created sessionJSON
	if err := json.Unmarshal([]byte(createdEvent.Body), &created); err != nil {
		t.Fatalf("unmarshal created session: %v", err)
	}
	if created.Name != "work" {
		t.Errorf("created.Name = %q, want %q", created.Name, "work")
	}
	if !ipc.IsReservedTUIID(created.ChatID) {
		t.Errorf("created.ChatID = %d, not in reserved TUI range", created.ChatID)
	}
	if created.ChatID == ipc.ReservedTUIChatID {
		t.Errorf("created.ChatID should not be the default DM")
	}

	// List sessions — should contain the created one.
	te2 := &testEmit{}
	if err := handler(ctx, ipc.IPCMessage{Type: ipc.MsgTypeSessions}, te2.emit); err != nil {
		t.Fatalf("list handler error: %v", err)
	}

	var sessionsEvent *ipc.IPCEvent
	for i, ev := range te2.events {
		if ev.Type == ipc.EventTypeSessions {
			sessionsEvent = &te2.events[i]
			break
		}
	}
	if sessionsEvent == nil {
		t.Fatal("no sessions event in list response")
	}

	var sessions []sessionJSON
	if err := json.Unmarshal([]byte(sessionsEvent.Body), &sessions); err != nil {
		t.Fatalf("unmarshal sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Name != "work" {
		t.Errorf("sessions[0].Name = %q, want %q", sessions[0].Name, "work")
	}
}

func TestTUIHandler_SessionCreateEmptyName(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	if err := handler(ctx, ipc.IPCMessage{
		Type: ipc.MsgTypeSessionCreate,
		Text: "   ",
	}, te.emit); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	foundError := false
	for _, ev := range te.events {
		if ev.Type == ipc.EventTypeError && ev.Error == "session name is required" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected error for empty session name")
	}
}

func TestTUIHandler_SessionOpen(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)

	// Create a session first.
	teCreate := &testEmit{}
	_ = handler(ctx, ipc.IPCMessage{Type: ipc.MsgTypeSessionCreate, Text: "research"}, teCreate.emit)

	var created sessionJSON
	for _, ev := range teCreate.events {
		if ev.Type == ipc.EventTypeSessionCreated {
			_ = json.Unmarshal([]byte(ev.Body), &created)
			break
		}
	}
	if created.ChatID == 0 {
		t.Fatal("failed to create session for open test")
	}

	// Open the session.
	teOpen := &testEmit{}
	if err := handler(ctx, ipc.IPCMessage{
		Type:   ipc.MsgTypeSessionOpen,
		ChatID: created.ChatID,
	}, teOpen.emit); err != nil {
		t.Fatalf("open handler error: %v", err)
	}

	var openedEvent *ipc.IPCEvent
	for i, ev := range teOpen.events {
		if ev.Type == ipc.EventTypeSessionOpened {
			openedEvent = &teOpen.events[i]
			break
		}
	}
	if openedEvent == nil {
		t.Fatal("no session_opened event emitted")
	}

	var opened sessionJSON
	if err := json.Unmarshal([]byte(openedEvent.Body), &opened); err != nil {
		t.Fatalf("unmarshal opened session: %v", err)
	}
	if opened.ChatID != created.ChatID {
		t.Errorf("opened.ChatID = %d, want %d", opened.ChatID, created.ChatID)
	}
	if opened.Name != "research" {
		t.Errorf("opened.Name = %q, want %q", opened.Name, "research")
	}
}

func TestTUIHandler_SessionOpenInvalidChatID(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	// ChatID outside reserved range should be rejected.
	if err := handler(ctx, ipc.IPCMessage{
		Type:   ipc.MsgTypeSessionOpen,
		ChatID: 12345,
	}, te.emit); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	foundError := false
	for _, ev := range te.events {
		if ev.Type == ipc.EventTypeError && ev.Error == "invalid session chat_id" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected error for invalid session chat_id")
	}
}

func TestTUIHandler_SessionOpenNotFound(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	// -9000005 is in range but doesn't exist.
	if err := handler(ctx, ipc.IPCMessage{
		Type:   ipc.MsgTypeSessionOpen,
		ChatID: -9000005,
	}, te.emit); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	foundError := false
	for _, ev := range te.events {
		if ev.Type == ipc.EventTypeError && ev.Error == "session not found" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected 'session not found' error")
	}
}

func TestTUIHandler_SessionOpenDefaultDM(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	// The default DM (-9000001) exists implicitly — it's never created
	// via session_create, so it has no row in the store. Opening it
	// should succeed without "session not found".
	if err := handler(ctx, ipc.IPCMessage{
		Type:   ipc.MsgTypeSessionOpen,
		ChatID: ipc.ReservedTUIChatID,
	}, te.emit); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	foundOpened := false
	for _, ev := range te.events {
		if ev.Type == ipc.EventTypeSessionOpened {
			foundOpened = true
			var s sessionJSON
			if err := json.Unmarshal([]byte(ev.Body), &s); err != nil {
				t.Fatalf("unmarshal opened session: %v", err)
			}
			if s.ChatID != ipc.ReservedTUIChatID {
				t.Errorf("opened.ChatID = %d, want ReservedTUIChatID", s.ChatID)
			}
			if s.Name != "dm" {
				t.Errorf("opened.Name = %q, want %q", s.Name, "dm")
			}
			break
		}
	}
	if !foundOpened {
		t.Error("expected session_opened event for default DM")
	}

	// Verify no error was emitted.
	for _, ev := range te.events {
		if ev.Type == ipc.EventTypeError {
			t.Errorf("unexpected error for default DM: %s", ev.Error)
		}
	}
}

func TestTUIHandler_SessionDelete(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)

	// Create a session.
	teCreate := &testEmit{}
	_ = handler(ctx, ipc.IPCMessage{Type: ipc.MsgTypeSessionCreate, Text: "temp"}, teCreate.emit)

	var created sessionJSON
	for _, ev := range teCreate.events {
		if ev.Type == ipc.EventTypeSessionCreated {
			_ = json.Unmarshal([]byte(ev.Body), &created)
			break
		}
	}
	if created.ChatID == 0 {
		t.Fatal("failed to create session for delete test")
	}

	// Delete it.
	teDelete := &testEmit{}
	if err := handler(ctx, ipc.IPCMessage{
		Type:   ipc.MsgTypeSessionDelete,
		ChatID: created.ChatID,
	}, teDelete.emit); err != nil {
		t.Fatalf("delete handler error: %v", err)
	}

	foundDeleted := false
	for _, ev := range teDelete.events {
		if ev.Type == ipc.EventTypeSessionDeleted {
			foundDeleted = true
			break
		}
	}
	if !foundDeleted {
		t.Error("expected session_deleted event")
	}

	// Verify it's gone from the list.
	teList := &testEmit{}
	_ = handler(ctx, ipc.IPCMessage{Type: ipc.MsgTypeSessions}, teList.emit)

	for _, ev := range teList.events {
		if ev.Type == ipc.EventTypeSessions {
			var sessions []sessionJSON
			_ = json.Unmarshal([]byte(ev.Body), &sessions)
			for _, s := range sessions {
				if s.ChatID == created.ChatID {
					t.Error("deleted session still in list")
				}
			}
		}
	}
}

func TestTUIHandler_SessionDeleteDefaultDMRejected(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)
	te := &testEmit{}

	if err := handler(ctx, ipc.IPCMessage{
		Type:   ipc.MsgTypeSessionDelete,
		ChatID: ipc.ReservedTUIChatID,
	}, te.emit); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	foundError := false
	for _, ev := range te.events {
		if ev.Type == ipc.EventTypeError && ev.Error == "cannot delete the default DM session" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected error when deleting default DM session")
	}
}

func TestSanitizeSessionName_StripsControlChars(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: "normal name", expected: "normal name"},
		{input: "  trimmed  ", expected: "trimmed"},
		{input: "name\nwith\tnewline\r", expected: "namewithnewline"},
		{input: "name\x00with\x1fNUL", expected: "namewithNUL"},
		{input: "\x1b[31mred\x1b[0m", expected: "red"},
		{input: "\x1b]0;title\x07name", expected: "name"},
		{input: "\x1b]0;title\x1b\\name", expected: "name"},
		{input: "\x1b[1;34m\x1b[Kbold blue\x1b[m", expected: "bold blue"},
		{input: "na\x7fme", expected: "name"},
		{input: "name\x80\x9fC1", expected: "nameC1"},
		{input: "CSI:\x9b1m", expected: "CSI:1m"},
	}
	for _, tt := range tests {
		got := sanitizeSessionName(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeSessionName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSanitizeSessionName_LimitsLength(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := sanitizeSessionName(long)
	if len([]rune(got)) > maxSessionNameLength {
		t.Errorf("sanitized name too long: %d runes, max %d", len([]rune(got)), maxSessionNameLength)
	}
	if got != strings.Repeat("a", maxSessionNameLength) {
		t.Errorf("expected %d a's, got %q (len=%d)", maxSessionNameLength, got, len(got))
	}
}

func TestSanitizeSessionName_AllControlOnly(t *testing.T) {
	got := sanitizeSessionName("\x1b[3m \x1b[0m  ")
	if got != "" {
		t.Errorf("expected empty for control-only input, got %q", got)
	}
}

func TestSanitizeSessionName_PreservesUnicode(t *testing.T) {
	input := "café résumé 日本語"
	got := sanitizeSessionName(input)
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestTUIHandler_SessionCreateAllocatesSequentialSlots(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)

	// Create 3 sessions and verify they get distinct, sequential ChatIDs.
	chatIDs := make(map[int64]bool)
	for _, name := range []string{"a", "b", "c"} {
		te := &testEmit{}
		_ = handler(ctx, ipc.IPCMessage{Type: ipc.MsgTypeSessionCreate, Text: name}, te.emit)

		for _, ev := range te.events {
			if ev.Type == ipc.EventTypeSessionCreated {
				var s sessionJSON
				_ = json.Unmarshal([]byte(ev.Body), &s)
				if chatIDs[s.ChatID] {
					t.Errorf("duplicate ChatID %d allocated", s.ChatID)
				}
				chatIDs[s.ChatID] = true
			}
		}
	}

	if len(chatIDs) != 3 {
		t.Errorf("expected 3 distinct ChatIDs, got %d", len(chatIDs))
	}

	// All should be in the reserved range, not the default DM.
	for cid := range chatIDs {
		if !ipc.IsReservedTUIID(cid) {
			t.Errorf("ChatID %d not in reserved range", cid)
		}
		if cid == ipc.ReservedTUIChatID {
			t.Errorf("ChatID should not be the default DM")
		}
	}
}

func TestTUIHandler_SessionCreateConcurrentAllocation(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)

	// With 8 reserved slots (-9000002..-9000009) and 6 concurrent creates,
	// every request must succeed with a distinct ChatID. No ErrSessionExists
	// is acceptable because the atomic slot iteration guarantees a free slot
	// is found as long as one remains.
	const numSessions = 6
	type result struct {
		err    error
		chatID int64
	}
	results := make(chan result, numSessions)

	for i := 0; i < numSessions; i++ {
		go func(n int) {
			te := &testEmit{}
			err := handler(ctx, ipc.IPCMessage{
				Type: ipc.MsgTypeSessionCreate,
				Text: fmt.Sprintf("session-%d", n),
			}, te.emit)
			if err != nil {
				results <- result{err: err}
				return
			}
			for _, ev := range te.events {
				if ev.Type == ipc.EventTypeSessionCreated {
					var s sessionJSON
					_ = json.Unmarshal([]byte(ev.Body), &s)
					results <- result{chatID: s.ChatID}
					return
				}
			}
			for _, ev := range te.events {
				if ev.Type == ipc.EventTypeError {
					results <- result{err: fmt.Errorf("%s", ev.Error)}
					return
				}
			}
			results <- result{err: fmt.Errorf("no session_created or error event")}
		}(i)
	}

	chatIDs := make(map[int64]bool, numSessions)
	for i := 0; i < numSessions; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("concurrent create failed: %v (no ErrSessionExists should occur while free slots remain)", r.err)
		}
		if chatIDs[r.chatID] {
			t.Errorf("duplicate ChatID %d allocated concurrently", r.chatID)
		}
		chatIDs[r.chatID] = true
	}

	if len(chatIDs) != numSessions {
		t.Fatalf("expected %d unique ChatIDs, got %d", numSessions, len(chatIDs))
	}

	for cid := range chatIDs {
		if !ipc.IsReservedTUIID(cid) {
			t.Errorf("ChatID %d not in reserved range", cid)
		}
		if cid == ipc.ReservedTUIChatID {
			t.Errorf("ChatID should not be the default DM")
		}
	}
}
