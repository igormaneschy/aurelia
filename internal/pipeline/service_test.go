package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/igormaneschy/aurelia/internal/runlog"
)

func TestParseSessionKey_Valid(t *testing.T) {
	tests := []struct {
		key             string
		wantChatID      int64
		wantThreadID    int
		wantUserID      int64
	}{
		{"42:7:100", 42, 7, 100},
		{"0:0:0", 0, 0, 0},
		{"-1:3:999", -1, 3, 999},
		{"12345:999:88888", 12345, 999, 88888},
	}
	for _, tt := range tests {
		chatID, threadID, userID, ok := parseSessionKey(tt.key)
		if !ok {
			t.Errorf("parseSessionKey(%q) = (_, _, _, false), want true", tt.key)
			continue
		}
		if chatID != tt.wantChatID {
			t.Errorf("parseSessionKey(%q) chatID = %d, want %d", tt.key, chatID, tt.wantChatID)
		}
		if threadID != tt.wantThreadID {
			t.Errorf("parseSessionKey(%q) threadID = %d, want %d", tt.key, threadID, tt.wantThreadID)
		}
		if userID != tt.wantUserID {
			t.Errorf("parseSessionKey(%q) userID = %d, want %d", tt.key, userID, tt.wantUserID)
		}
	}
}

func TestParseSessionKey_Malformed(t *testing.T) {
	tests := []string{
		"",
		"abc:def:ghi",
		"42:7",              // missing userID
		"42:7:100:extra",    // extra trailing field
		"not-a-key",
		"42::100",           // empty threadID
		":7:100",            // empty chatID
		"42:7:",             // empty userID
		"42:7:100x",         // trailing non-digit in userID field
		"42:7:100 ",         // trailing whitespace
		" 42:7:100",         // leading whitespace
	}
	for _, key := range tests {
		_, _, _, ok := parseSessionKey(key)
		if ok {
			t.Errorf("parseSessionKey(%q) = (_, _, _, true), want false", key)
		}
	}
}

// addActiveSession is a test helper to populate the activeSessions map.
// Returns the context so callers can check ctx.Done() for cancellation.
func addActiveSession(s *Service, chatID int64, threadID int, userID int64) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	s.activeSessions.Store(sessionKey(chatID, threadID, userID), cancel)
	return ctx
}

func TestScopedAbortRequest_SetsCommandAndOptions(t *testing.T) {
	req := scopedAbortRequest(42, 7, 100)

	if req.Command != "abort" {
		t.Errorf("Command = %q, want %q", req.Command, "abort")
	}
	if req.Options.ChatID != 42 {
		t.Errorf("Options.ChatID = %d, want %d", req.Options.ChatID, 42)
	}
	if req.Options.ThreadID != 7 {
		t.Errorf("Options.ThreadID = %d, want %d", req.Options.ThreadID, 7)
	}
	if req.Options.UserID != 100 {
		t.Errorf("Options.UserID = %d, want %d", req.Options.UserID, 100)
	}
}

func TestScopedAbortRequest_ZeroOptions(t *testing.T) {
	req := scopedAbortRequest(0, 0, 0)

	if req.Command != "abort" {
		t.Errorf("Command = %q, want %q", req.Command, "abort")
	}
	if req.Options.ChatID != 0 {
		t.Errorf("Options.ChatID = %d, want 0", req.Options.ChatID)
	}
	if req.Options.ThreadID != 0 {
		t.Errorf("Options.ThreadID = %d, want 0", req.Options.ThreadID)
	}
	if req.Options.UserID != 0 {
		t.Errorf("Options.UserID = %d, want 0", req.Options.UserID)
	}
}

func TestCancelAllForUser_NilService(t *testing.T) {
	if (*Service)(nil).CancelAllForUser(42) {
		t.Error("CancelAllForUser on nil service returned true, want false")
	}
}

func TestCancelAllForUser_NoMatch(t *testing.T) {
	s := &Service{}
	addActiveSession(s, 1, 2, 100)

	if s.CancelAllForUser(200) {
		t.Error("CancelAllForUser(200) returned true when only user 100 has sessions")
	}

	// Verify user 100's session is still intact
	_, ok := s.activeSessions.Load(sessionKey(1, 2, 100))
	if !ok {
		t.Error("user 100 session was removed after CancelAllForUser for user 200")
	}
}

func TestCancelAllForUser_TwoUsersSameChatSameThread(t *testing.T) {
	s := &Service{}

	chatID := int64(42)
	threadID := 7
	userA := int64(100)
	userB := int64(200)

	ctxA := addActiveSession(s, chatID, threadID, userA)
	addActiveSession(s, chatID, threadID, userB)

	// Verify both sessions exist before cancel
	if _, ok := s.activeSessions.Load(sessionKey(chatID, threadID, userA)); !ok {
		t.Fatal("userA session not found before cancel")
	}
	if _, ok := s.activeSessions.Load(sessionKey(chatID, threadID, userB)); !ok {
		t.Fatal("userB session not found before cancel")
	}

	// Cancel only user A
	if !s.CancelAllForUser(userA) {
		t.Error("CancelAllForUser(userA) returned false, want true")
	}

	// User A's session should be cancelled (context done) and removed
	if _, ok := s.activeSessions.Load(sessionKey(chatID, threadID, userA)); ok {
		t.Error("userA session still in activeSessions after CancelAllForUser")
	}
	select {
	case <-ctxA.Done():
		// expected — context was cancelled
	default:
		t.Error("userA cancel func was not called")
	}

	// User B's session should remain active and not cancelled
	if _, ok := s.activeSessions.Load(sessionKey(chatID, threadID, userB)); !ok {
		t.Error("userB session was removed after CancelAllForUser for userA")
	}
}

func TestCancelAllForUser_MultipleSessionsSameUser(t *testing.T) {
	s := &Service{}

	userID := int64(42)

	// User has sessions across different chats/threads
	sessions := []struct {
		chatID   int64
		threadID int
	}{
		{1, 0},
		{1, 1},
		{2, 0},
		{3, 5},
	}

	ctxs := make([]context.Context, len(sessions))
	for i, sess := range sessions {
		ctxs[i] = addActiveSession(s, sess.chatID, sess.threadID, userID)
	}

	// Also add a session for another user to verify it's not affected
	addActiveSession(s, 1, 0, 999)

	if !s.CancelAllForUser(userID) {
		t.Error("CancelAllForUser(userID) returned false, want true")
	}

	// All user's sessions should be removed and cancelled
	for i, sess := range sessions {
		key := sessionKey(sess.chatID, sess.threadID, userID)
		if _, ok := s.activeSessions.Load(key); ok {
			t.Errorf("user session key=%q still in map", key)
		}
		select {
		case <-ctxs[i].Done():
			// expected
		default:
			t.Errorf("user session %d cancel func was not called", i)
		}
	}

	// Other user's session remains
	if _, ok := s.activeSessions.Load(sessionKey(1, 0, 999)); !ok {
		t.Error("other user's session was removed")
	}
}

func TestCancelAllForUser_UserIDZero(t *testing.T) {
	s := &Service{}

	// UserID 0 sessions exist
	ctxZero := addActiveSession(s, 1, 0, 0)
	// Another user with ID 0 should also match and be cancelled
	addActiveSession(s, 2, 0, 0)
	// A non-zero user should be unaffected
	addActiveSession(s, 1, 0, 100)

	if !s.CancelAllForUser(0) {
		t.Error("CancelAllForUser(0) returned false, want true")
	}

	select {
	case <-ctxZero.Done():
		// expected
	default:
		t.Error("userID 0 cancel func was not called")
	}

	// Non-zero user remains
	if _, ok := s.activeSessions.Load(sessionKey(1, 0, 100)); !ok {
		t.Error("non-zero user session was incorrectly removed")
	}
}

func TestCancelAllForUser_NonCancelValueInMap(t *testing.T) {
	// Should not panic if stored value isn't a context.CancelFunc
	s := &Service{}
	s.activeSessions.Store(sessionKey(1, 0, 100), "not-a-cancel-func")

	// Should not panic
	if s.CancelAllForUser(100) {
		t.Error("CancelAllForUser returned true when value is not a CancelFunc")
	}

	// The key should still be deleted from the map
	if _, ok := s.activeSessions.Load(sessionKey(1, 0, 100)); ok {
		t.Error("key not deleted from activeSessions even though value was wrong type")
	}
}

func TestCancelAllForUser_MalformedKeyInMap(t *testing.T) {
	s := &Service{}

	// Store a malformed key directly
	s.activeSessions.Store("not-a-valid-key", context.CancelFunc(func() {}))

	// Store valid keys for both users
	addActiveSession(s, 1, 0, 100)
	addActiveSession(s, 1, 0, 200)

	// Should not panic
	if !s.CancelAllForUser(100) {
		t.Error("CancelAllForUser(100) returned false, want true")
	}

	// User 200 session still exists
	if _, ok := s.activeSessions.Load(sessionKey(1, 0, 200)); !ok {
		t.Error("user 200 session was removed")
	}

	// Malformed key should still be in the map (we skip, don't delete malformed keys)
	if _, ok := s.activeSessions.Load("not-a-valid-key"); !ok {
		t.Error("malformed key was deleted — we should skip, not delete")
	}
}

func TestCancelAllForUser_EmptyMap(t *testing.T) {
	s := &Service{}
	if s.CancelAllForUser(42) {
		t.Error("CancelAllForUser on empty map returned true, want false")
	}
}

func TestCancel_NilService(t *testing.T) {
	if (*Service)(nil).Cancel(1, 0) {
		t.Error("Cancel on nil service returned true, want false")
	}
}

func TestCancel_NilBridge(t *testing.T) {
	s := &Service{}
	if s.Cancel(1, 0) {
		t.Error("Cancel with nil bridge returned true, want false")
	}
}

func TestWorkStatus_NilService(t *testing.T) {
	text, turns := (*Service)(nil).WorkStatus(1, 0)
	if text != "" {
		t.Errorf("WorkStatus on nil service returned text=%q, want empty", text)
	}
	if turns != 0 {
		t.Errorf("WorkStatus on nil service returned turns=%d, want 0", turns)
	}
}

func TestWorkStatus_NilBridge(t *testing.T) {
	s := &Service{}
	text, turns := s.WorkStatus(1, 0)
	if text != "" {
		t.Errorf("WorkStatus with nil bridge returned text=%q, want empty", text)
	}
	if turns != 0 {
		t.Errorf("WorkStatus with nil bridge returned turns=%d, want 0", turns)
	}
}

func TestRecordToolUse_NoDeadlockWithCompleteRunLog(t *testing.T) {
	// This test ensures the WaitGroup added to runLogState prevents
	// race conditions between recordToolUse async DB update and
	// completeRunLog cleanup, without causing deadlocks.

	// Create a minimal Service with a mock runLog
	s := &Service{
		runLog:       &fakeRunLogStore{},
		runLogStates: make(map[string]*runLogState),
		runLogMu:     sync.Mutex{},
	}

	// Simulate startRunLog by creating a minimal state
	runID := uuid.NewString()
	key := runLogKey(42, 7)
	s.runLogMu.Lock()
	s.runLogStates[key] = &runLogState{
		runID: runID,
	}
	s.runLogMu.Unlock()

	// Verify initial state
	s.runLogMu.Lock()
	state := s.runLogStates[key]
	s.runLogMu.Unlock()
	if state == nil {
		t.Fatal("expected runLogState to exist")
	}

	// Simulate what recordToolUse does
	state.mu.Lock()
	state.summaryCount = 5 // Force an update
	state.summary.WriteString("Read")
	state.mu.Unlock()

	// Now completeRunLog should work without hanging
	done := make(chan struct{})
	go func() {
		s.completeRunLog(42, 7, runlog.RunCompleted, "", "")
		close(done)
	}()

	select {
	case <-done:
		// success — no deadlock
	case <-time.After(3 * time.Second):
		t.Fatal("completeRunLog deadlocked or hung")
	}
}
