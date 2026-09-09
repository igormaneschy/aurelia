package session

import (
	"testing"
	"time"
)

func TestStore_SetGetClear(t *testing.T) {
	s := NewStore()
	if id := s.Get(1, 0); id != "" {
		t.Fatalf("expected empty, got %q", id)
	}
	s.Set(1, 0, "sess-abc")
	if id := s.Get(1, 0); id != "sess-abc" {
		t.Fatalf("expected sess-abc, got %q", id)
	}
	sid, active := s.GetWithState(1, 0)
	if sid != "sess-abc" || !active {
		t.Fatalf("expected sess-abc/active, got %q/%v", sid, active)
	}
	s.Clear(1, 0)
	if id := s.Get(1, 0); id != "" {
		t.Fatalf("expected empty after clear, got %q", id)
	}
}

func TestStore_ThreadIsolation(t *testing.T) {
	s := NewStore()
	s.Set(1, 0, "sess-main")
	s.Set(1, 42, "sess-topic-42")
	s.Set(1, 99, "sess-topic-99")

	// Each thread has its own session
	if id := s.Get(1, 0); id != "sess-main" {
		t.Fatalf("thread 0 = %q, want sess-main", id)
	}
	if id := s.Get(1, 42); id != "sess-topic-42" {
		t.Fatalf("thread 42 = %q, want sess-topic-42", id)
	}
	if id := s.Get(1, 99); id != "sess-topic-99" {
		t.Fatalf("thread 99 = %q, want sess-topic-99", id)
	}

	// Clear only one thread
	s.Clear(1, 42)
	if id := s.Get(1, 42); id != "" {
		t.Fatalf("thread 42 should be empty after clear, got %q", id)
	}
	if id := s.Get(1, 0); id != "sess-main" {
		t.Fatalf("thread 0 should be preserved, got %q", id)
	}

	// ClearAll removes all threads
	s.ClearAll(1)
	if id := s.Get(1, 0); id != "" {
		t.Fatalf("thread 0 should be empty after ClearAll, got %q", id)
	}
	if id := s.Get(1, 99); id != "" {
		t.Fatalf("thread 99 should be empty after ClearAll, got %q", id)
	}
}

func TestStore_DeactivateAll(t *testing.T) {
	s := NewStore()
	s.Set(1, 0, "sess-a")
	s.Set(2, 0, "sess-b")
	s.Set(3, 0, "sess-c")

	// All active before deactivation
	for _, chatID := range []int64{1, 2, 3} {
		if _, active := s.GetWithState(chatID, 0); !active {
			t.Fatalf("chat %d should be active before DeactivateAll", chatID)
		}
	}

	s.DeactivateAll()

	// All inactive after deactivation, but IDs preserved
	for _, chatID := range []int64{1, 2, 3} {
		sid, active := s.GetWithState(chatID, 0)
		if active {
			t.Fatalf("chat %d should be inactive after DeactivateAll", chatID)
		}
		if sid == "" {
			t.Fatalf("chat %d session ID should be preserved after DeactivateAll", chatID)
		}
	}

	// Get still returns the session ID
	if id := s.Get(1, 0); id != "sess-a" {
		t.Fatalf("Get(1, 0) = %q, want %q", id, "sess-a")
	}
}

func TestStore_DeactivateAll_Empty(t *testing.T) {
	s := NewStore()
	s.DeactivateAll() // should not panic
}

func TestStore_Deactivate(t *testing.T) {
	s := NewStore()
	s.Set(1, 0, "sess-active")
	s.Set(1, 42, "sess-topic")

	// Deactivate only the specific session
	s.Deactivate(1, 0)

	// Verify chat 1, thread 0 is now inactive
	sid, active := s.GetWithState(1, 0)
	if sid != "sess-active" {
		t.Fatalf("session ID should be preserved, got %q", sid)
	}
	if active {
		t.Fatal("session should be inactive after Deactivate")
	}

	// Verify other sessions are unaffected
	sid, active = s.GetWithState(1, 42)
	if sid != "sess-topic" {
		t.Fatalf("session ID for thread 42 should be preserved, got %q", sid)
	}
	if !active {
		t.Fatal("other session should remain active")
	}
}

func TestStore_Deactivate_NonExistent(t *testing.T) {
	s := NewStore()
	s.Deactivate(999, 0) // should not panic
}

func TestStore_Cwd(t *testing.T) {
	s := NewStore()
	s.SetCwd(1, 0, "/home/user")
	if cwd := s.GetCwd(1, 0); cwd != "/home/user" {
		t.Fatalf("expected /home/user, got %q", cwd)
	}
	s.Clear(1, 0)
	if cwd := s.GetCwd(1, 0); cwd != "" {
		t.Fatalf("expected empty after clear, got %q", cwd)
	}
}

func TestStore_ClearSession_PreservesCwd(t *testing.T) {
	s := NewStore()
	s.Set(1, 42, "sess-topic")
	s.SetCwd(1, 42, "/repo")

	s.ClearSession(1, 42)

	if id := s.Get(1, 42); id != "" {
		t.Fatalf("expected session to be cleared, got %q", id)
	}
	if cwd := s.GetCwd(1, 42); cwd != "/repo" {
		t.Fatalf("expected cwd to be preserved, got %q", cwd)
	}
}

func TestStore_GC_RemovesOldEntries(t *testing.T) {
	s := NewStore()
	s.Set(1, 0, "sess-old")
	s.Set(2, 0, "sess-new")

	// Run GC with zero maxAge — everything should be removed
	s.GC(0)

	if id := s.Get(1, 0); id != "" {
		t.Fatalf("expected empty after GC(0), got %q", id)
	}
	if id := s.Get(2, 0); id != "" {
		t.Fatalf("expected empty after GC(0), got %q", id)
	}
}

func TestStore_GC_KeepsRecentEntries(t *testing.T) {
	s := NewStore()
	s.Set(1, 0, "sess-recent")

	// GC with 1 hour maxAge — recent entries should survive
	s.GC(1 * time.Hour)

	if id := s.Get(1, 0); id != "sess-recent" {
		t.Fatalf("expected sess-recent after GC, got %q", id)
	}
}

func TestStore_GC_AlsoClearsCwd(t *testing.T) {
	s := NewStore()
	s.Set(1, 0, "sess-1")
	s.SetCwd(1, 0, "/home/project")

	s.GC(0)

	if cwd := s.GetCwd(1, 0); cwd != "" {
		t.Fatalf("expected empty cwd after GC, got %q", cwd)
	}
}

func TestStore_GC_ClearsCwdWithoutSession(t *testing.T) {
	s := NewStore()
	s.SetCwd(1, 0, "/home/project")

	s.GC(0)

	if cwd := s.GetCwd(1, 0); cwd != "" {
		t.Fatalf("expected empty cwd-only entry after GC, got %q", cwd)
	}
}

func TestStore_GC_Empty(t *testing.T) {
	s := NewStore()
	s.GC(1 * time.Hour) // should not panic
}

func TestStore_Cwd_TopicFallback(t *testing.T) {
	s := NewStore()
	// Set cwd on general topic (thread=0)
	s.SetCwd(1, 0, "/home/project")

	// Topic should inherit general topic's cwd
	if cwd := s.GetCwd(1, 2); cwd != "/home/project" {
		t.Fatalf("topic should inherit general cwd, got %q", cwd)
	}

	// Set topic-specific cwd — should override general
	s.SetCwd(1, 2, "/home/project/sub")
	if cwd := s.GetCwd(1, 2); cwd != "/home/project/sub" {
		t.Fatalf("expected topic-specific cwd, got %q", cwd)
	}

	// Other topic without specific cwd should still inherit general
	if cwd := s.GetCwd(1, 3); cwd != "/home/project" {
		t.Fatalf("other topic should inherit general cwd, got %q", cwd)
	}

	// Clear topic-specific cwd — should fall back to general
	s.Clear(1, 2)
	if cwd := s.GetCwd(1, 2); cwd != "/home/project" {
		t.Fatalf("after clear, topic should fall back to general cwd, got %q", cwd)
	}

	// Clear general — nothing left
	s.Clear(1, 0)
	if cwd := s.GetCwd(1, 2); cwd != "" {
		t.Fatalf("after general clear, topic should return empty, got %q", cwd)
	}
}

func TestStore_UserSessionIsolation(t *testing.T) {
	s := NewStore()
	s.SetSession(1, 2, 100, "sess-user-100")
	s.SetSession(1, 2, 200, "sess-user-200")

	if got := s.GetSession(1, 2, 100); got != "sess-user-100" {
		t.Fatalf("user 100 session = %q, want sess-user-100", got)
	}
	if got := s.GetSession(1, 2, 200); got != "sess-user-200" {
		t.Fatalf("user 200 session = %q, want sess-user-200", got)
	}
	if got := s.GetSession(1, 2, 300); got != "" {
		t.Fatalf("unrelated user should not see session, got %q", got)
	}

	s.DeactivateSession(1, 2, 100)
	_, active100 := s.GetSessionWithState(1, 2, 100)
	_, active200 := s.GetSessionWithState(1, 2, 200)
	if active100 {
		t.Fatal("user 100 session should be inactive")
	}
	if !active200 {
		t.Fatal("user 200 session should remain active")
	}

	s.ClearSessionForUser(1, 2, 100)
	if got := s.GetSession(1, 2, 100); got != "" {
		t.Fatalf("user 100 session should be cleared, got %q", got)
	}
	if got := s.GetSession(1, 2, 200); got != "sess-user-200" {
		t.Fatalf("user 200 session should be preserved, got %q", got)
	}
}

// TestMarkLongSessionNudged_OneShotAndResetOnNewSession covers A8 exactly-once
// semantics: the first claim wins, later claims are rejected, re-setting the
// same session file does not re-arm, a new file does, and an unknown
// conversation fails closed.
func TestMarkLongSessionNudged_OneShotAndResetOnNewSession(t *testing.T) {
	s := NewStore()
	s.SetSession(1, 0, 100, "/tmp/a.jsonl")

	if !s.MarkLongSessionNudged(1, 0, 100) {
		t.Fatal("first claim should win")
	}
	if s.MarkLongSessionNudged(1, 0, 100) {
		t.Fatal("second claim must be rejected")
	}

	// Re-setting the SAME session file must not re-arm the nudge.
	s.SetSession(1, 0, 100, "/tmp/a.jsonl")
	if s.MarkLongSessionNudged(1, 0, 100) {
		t.Fatal("re-setting the same session file must not re-arm")
	}

	// A different session file means a new conversation: re-arm.
	s.SetSession(1, 0, 100, "/tmp/b.jsonl")
	if !s.MarkLongSessionNudged(1, 0, 100) {
		t.Fatal("new session file must re-arm the nudge")
	}

	// Unknown conversation fails closed (no unbounded tracking).
	if s.MarkLongSessionNudged(2, 0, 200) {
		t.Fatal("unknown conversation must fail closed")
	}
}
