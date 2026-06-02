package session

import (
	"strings"
	"testing"
)

func TestNudgeBuffer_AddAndCount(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, 0, 100, "hello", "hi there")
	b.AddTurn(1, 0, 100, "how are you", "good")
	b.AddTurn(2, 0, 200, "other chat", "response")

	if got := b.TurnCount(1, 0, 100); got != 2 {
		t.Errorf("TurnCount(1) = %d, want 2", got)
	}
	if got := b.TurnCount(2, 0, 200); got != 1 {
		t.Errorf("TurnCount(2) = %d, want 1", got)
	}
	if got := b.TurnCount(999, 0, 0); got != 0 {
		t.Errorf("TurnCount(999) = %d, want 0", got)
	}
}

func TestNudgeBuffer_GetAndReset(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, 0, 100, "msg1", "resp1")
	b.AddTurn(1, 0, 100, "msg2", "resp2")

	msgs := b.GetAndReset(1, 0, 100)
	if len(msgs) != 4 { // 2 turns × 2 messages each
		t.Fatalf("GetAndReset returned %d messages, want 4", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "msg1" {
		t.Errorf("first message = %+v, want user/msg1", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "resp1" {
		t.Errorf("second message = %+v, want assistant/resp1", msgs[1])
	}

	// Buffer should be empty after reset
	if got := b.TurnCount(1, 0, 100); got != 0 {
		t.Errorf("TurnCount after reset = %d, want 0", got)
	}
	if msgs := b.GetAndReset(1, 0, 100); msgs != nil {
		t.Errorf("GetAndReset after reset = %v, want nil", msgs)
	}
}

func TestNudgeBuffer_IsolatedChats(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, 0, 100, "chat1", "resp1")
	b.AddTurn(2, 0, 200, "chat2", "resp2")

	// Reset chat 1, chat 2 should be unaffected
	b.GetAndReset(1, 0, 100)

	if got := b.TurnCount(2, 0, 200); got != 1 {
		t.Errorf("TurnCount(2) after reset(1) = %d, want 1", got)
	}
}

func TestNudgeBuffer_IsolatedThreads(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, 10, 100, "topic10", "resp10")
	b.AddTurn(1, 20, 100, "topic20", "resp20")
	b.GetAndReset(1, 10, 100)

	if got := b.TurnCount(1, 20, 100); got != 1 {
		t.Errorf("TurnCount(1,20) after reset(1,10) = %d, want 1", got)
	}
	if got := b.TurnCount(1, 10, 100); got != 0 {
		t.Errorf("TurnCount(1,10) after reset = %d, want 0", got)
	}
}

func TestNudgeBuffer_SnapshotPreservesBuffer(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, 100, "hello", "hi")
	b.AddTurn(1, 0, 100, "how are you", "good")

	snap, ver := b.Snapshot(1, 0, 100)
	if len(snap) != 4 {
		t.Fatalf("Snapshot returned %d messages, want 4", len(snap))
	}
	if ver == 0 {
		t.Fatal("expected non-zero version")
	}

	// Buffer should still be intact
	if got := b.TurnCount(1, 0, 100); got != 2 {
		t.Errorf("TurnCount after Snapshot = %d, want 2", got)
	}
	if got := len(b.GetAndReset(1, 0, 100)); got != 4 {
		t.Errorf("buffer after Snapshot = %d messages, want 4", got)
	}
}

func TestNudgeBuffer_CommitPartial(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, 100, "q1", "a1")
	b.AddTurn(1, 0, 100, "q2", "a2")
	b.AddTurn(1, 0, 100, "q3", "a3")

	msgs, ver := b.Snapshot(1, 0, 100)
	// Commit 2 messages (1 turn) — should leave 2 turns (4 messages)
	b.Commit(1, 0, 100, ver, 2)

	if got := b.TurnCount(1, 0, 100); got != 2 {
		t.Errorf("TurnCount after partial commit = %d, want 2", got)
	}
	msgs2, _ := b.Snapshot(1, 0, 100)
	if len(msgs2) != 4 {
		t.Fatalf("messages after partial commit = %d, want 4", len(msgs2))
	}
	if msgs2[0].Content != "q2" || msgs2[2].Content != "q3" {
		t.Fatalf("remaining messages out of order: %+v", msgs2)
	}
	_ = msgs
}

func TestNudgeBuffer_CommitAll(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, 100, "q1", "a1")
	b.AddTurn(1, 0, 100, "q2", "a2")

	_, ver := b.Snapshot(1, 0, 100)
	b.Commit(1, 0, 100, ver, 10) // more than available

	if got := b.TurnCount(1, 0, 100); got != 0 {
		t.Errorf("TurnCount after full commit = %d, want 0", got)
	}
	if msgs, _ := b.Snapshot(1, 0, 100); msgs != nil {
		t.Errorf("expected nil after full commit, got %d messages", len(msgs))
	}
}

func TestNudgeBuffer_CommitNoop(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, 100, "q1", "a1")

	_, ver := b.Snapshot(1, 0, 100)
	b.Commit(1, 0, 100, ver, 0) // count=0 should be no-op

	if got := b.TurnCount(1, 0, 100); got != 1 {
		t.Errorf("TurnCount after noop commit = %d, want 1", got)
	}
}

func TestNudgeBuffer_SnapshotCommitPreservesOtherThreads(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 10, 100, "t10", "r10")
	b.AddTurn(1, 20, 100, "t20", "r20")

	snap, ver := b.Snapshot(1, 10, 100)
	b.Commit(1, 10, 100, ver, 2)

	// Thread 20 should be unaffected
	if got := b.TurnCount(1, 20, 100); got != 1 {
		t.Errorf("TurnCount(1,20) = %d, want 1", got)
	}
	_ = snap
}

func TestNudgeBuffer_StaleCommitSkipped(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, 100, "q1", "a1")

	_, ver := b.Snapshot(1, 0, 100)

	// Buffer modified before commit — version changed
	b.AddTurn(1, 0, 100, "q2", "a2")

	// Attempt commit with stale version — should be silently skipped
	b.Commit(1, 0, 100, ver, 2)

	// Buffer should still have both turns
	if got := b.TurnCount(1, 0, 100); got != 2 {
		t.Errorf("TurnCount after stale commit = %d, want 2", got)
	}
}

func TestNudgeBuffer_CapDropsOldest(t *testing.T) {
	b := NewNudgeBuffer()
	// Fill to just under cap, then add one more pair to exceed
	for i := 0; i < 19; i++ {
		b.AddTurn(1, 0, 100, "pre", "pre-resp")
	}
	if got := b.TurnCount(1, 0, 100); got != 19 {
		t.Fatalf("expected 19 turns, got %d", got)
	}

	// Snapshot at cap boundary
	snapAtCap, verAtCap := b.Snapshot(1, 0, 100)
	if len(snapAtCap) != 38 {
		t.Fatalf("expected 38 messages before final add, got %d", len(snapAtCap))
	}

	// Now add one more — should trigger cap. 20 turns = 40 messages, at cap.
	b.AddTurn(1, 0, 100, "final", "final-answer")
	if got := b.TurnCount(1, 0, 100); got != 20 {
		t.Fatalf("expected 20 turns (at cap), got %d", got)
	}

	// Add beyond cap — oldest should be dropped
	b.AddTurn(1, 0, 100, "extra", "extra-answer")
	if got := b.TurnCount(1, 0, 100); got != 20 {
		t.Fatalf("expected 20 turns (capped), got %d", got)
	}

	msgs, ver := b.Snapshot(1, 0, 100)
	if len(msgs) != 40 {
		t.Fatalf("expected 40 messages (capped), got %d", len(msgs))
	}
	// Last assistant should be extra-answer
	if msgs[39].Content != "extra-answer" {
		t.Fatalf("expected last message 'extra-answer', got %q", msgs[39].Content)
	}

	// Stale commit from prior snapshot should be skipped
	b.Commit(1, 0, 100, verAtCap, len(snapAtCap))
	if got := b.TurnCount(1, 0, 100); got != 20 {
		t.Errorf("stale commit should be skipped, got %d turns", got)
	}

	// Fresh commit from current snapshot should work
	b.Commit(1, 0, 100, ver, len(msgs))
	if got := b.TurnCount(1, 0, 100); got != 0 {
		t.Errorf("fresh commit should clear buffer, got %d turns", got)
	}
}

func TestNudgeBuffer_PerUserIsolation(t *testing.T) {
	b := NewNudgeBuffer()

	// User A in chat 1
	b.AddTurn(1, 0, 100, "a_msg1", "a_resp1")
	b.AddTurn(1, 0, 100, "a_msg2", "a_resp2")

	// User B in chat 1
	b.AddTurn(1, 0, 200, "b_msg1", "b_resp1")

	// User A should have 2 turns
	if got := b.TurnCount(1, 0, 100); got != 2 {
		t.Errorf("TurnCount(user A) = %d, want 2", got)
	}
	// User B should have 1 turn
	if got := b.TurnCount(1, 0, 200); got != 1 {
		t.Errorf("TurnCount(user B) = %d, want 1", got)
	}

	// Reset user A, user B should be unaffected
	b.GetAndReset(1, 0, 100)

	if got := b.TurnCount(1, 0, 100); got != 0 {
		t.Errorf("TurnCount(user A) after reset = %d, want 0", got)
	}
	if got := b.TurnCount(1, 0, 200); got != 1 {
		t.Errorf("TurnCount(user B) after reset = %d, want 1", got)
	}
}

func TestNudgeBuffer_AddToolEvent(t *testing.T) {
	b := NewNudgeBuffer()

	// Empty buffer — AddToolEvent should be silently dropped
	b.AddToolEvent(1, 0, 100, "tool: read file")
	msgs, _ := b.Snapshot(1, 0, 100)
	if len(msgs) != 0 {
		t.Fatal("AddToolEvent on empty buffer should be a no-op")
	}

	// Add a turn, then attach tool event to the assistant message
	b.AddTurn(1, 0, 100, "read this", "ok processing")
	b.AddToolEvent(1, 0, 100, "tool: Read(file=main.go)")
	b.AddToolEvent(1, 0, 100, "tool: Grep(pattern=func)")

	msgs, _ = b.Snapshot(1, 0, 100)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	// User message should NOT have ToolSummary
	if msgs[0].ToolSummary != "" {
		t.Error("user message should not have ToolSummary")
	}
	// Assistant message should have accumulated ToolSummary
	if !strings.Contains(msgs[1].ToolSummary, "Read(file=main.go)") {
		t.Error("assistant message should have Read tool in ToolSummary")
	}
	if !strings.Contains(msgs[1].ToolSummary, "Grep(pattern=func)") {
		t.Error("assistant message should have Grep tool in ToolSummary")
	}
}

func TestNudgeBuffer_AddToolEvent_EmptyStringNoOp(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, 100, "hi", "hello")
	b.AddToolEvent(1, 0, 100, "") // empty — should not change anything
	msgs, _ := b.Snapshot(1, 0, 100)
	if msgs[1].ToolSummary != "" {
		t.Error("empty tool summary should not be stored")
	}
}

func TestNudgeBuffer_ClearUser(t *testing.T) {
	b := NewNudgeBuffer()

	// User A in multiple chats
	b.AddTurn(1, 0, 100, "a1", "ar1")
	b.AddTurn(2, 5, 100, "a2", "ar2")
	// User B in same chat
	b.AddTurn(1, 0, 200, "b1", "br1")

	if b.TurnCount(1, 0, 100) != 1 || b.TurnCount(2, 5, 100) != 1 || b.TurnCount(1, 0, 200) != 1 {
		t.Fatal("expected all buffers to have data before ClearUser")
	}

	b.ClearUser(100)

	// User A should be gone
	if b.TurnCount(1, 0, 100) != 0 {
		t.Error("user A chat 1 should be cleared")
	}
	if b.TurnCount(2, 5, 100) != 0 {
		t.Error("user A chat 2 should be cleared")
	}
	// User B should still have data
	if b.TurnCount(1, 0, 200) != 1 {
		t.Error("user B should NOT be cleared")
	}
}

func TestNudgeBuffer_TotalChars(t *testing.T) {
	b := NewNudgeBuffer()

	if got := b.TotalChars(1, 0, 100); got != 0 {
		t.Fatalf("TotalChars empty = %d, want 0", got)
	}

	b.AddTurn(1, 0, 100, "hello", "world")
	// "hello" + "world" = 10 chars
	if got := b.TotalChars(1, 0, 100); got != 10 {
		t.Fatalf("TotalChars after AddTurn = %d, want 10", got)
	}

	b.AddToolEvent(1, 0, 100, "tool: Read(file=main.go)")
	// 10 + 24 = 34
	if got := b.TotalChars(1, 0, 100); got != 34 {
		t.Fatalf("TotalChars after AddToolEvent = %d, want 34", got)
	}
}
