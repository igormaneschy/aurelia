package telegram

import (
	"testing"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/session"
)

// newTestBotController creates a minimal BotController for testing bridge event
// processing. The bot field is nil — only use for tests that don't trigger Send.
func newTestBotController() *BotController {
	return &BotController{
		config: &config.AppConfig{
			MaxSessionTokens: 100000,
			Providers:        map[string]config.ProviderConfig{},
		},
		sessions: session.NewStore(),
	}
}

// noopProgress returns a progressReporter that does nothing (no bot required).
func noopProgress() *progressReporter {
	return &progressReporter{}
}

func TestProcessBridgeEventsAsync_ProcessDeath_NoTerminalEvent(t *testing.T) {
	bc := newTestBotController()
	chat := &telebot.Chat{ID: 1}

	ch := make(chan bridge.Event, 2)
	ch <- bridge.Event{Type: "system", SessionID: "sess-1", SessionFile: "/tmp/test-session.jsonl"}
	ch <- bridge.Event{Type: "assistant", Text: "partial response"}
	close(ch) // simulate process death — no terminal event

	outcome := bc.processBridgeEventsAsync(chat, ch, noopProgress(), "test", 1)
	if outcome != outcomeProcessDeath {
		t.Fatalf("expected outcomeProcessDeath, got %d", outcome)
	}

	// Session should still have been set from the system event
	sid := bc.sessions.Get(1, 0)
	if sid != "/tmp/test-session.jsonl" {
		t.Fatalf("expected session file /tmp/test-session.jsonl, got %q", sid)
	}
}

func TestProcessBridgeEventsAsync_EmptyChannelIsDeath(t *testing.T) {
	bc := newTestBotController()
	chat := &telebot.Chat{ID: 1}

	ch := make(chan bridge.Event)
	close(ch) // immediate close — no events at all

	outcome := bc.processBridgeEventsAsync(chat, ch, noopProgress(), "test", 1)
	if outcome != outcomeProcessDeath {
		t.Fatalf("expected outcomeProcessDeath, got %d", outcome)
	}
}

func TestProcessBridgeEventsAsync_SessionSetFromSystemEvent(t *testing.T) {
	bc := newTestBotController()
	chat := &telebot.Chat{ID: 42}

	ch := make(chan bridge.Event, 1)
	ch <- bridge.Event{Type: "system", SessionID: "sess-xyz", SessionFile: "/tmp/test-session.jsonl"}
	close(ch) // death after system event

	bc.processBridgeEventsAsync(chat, ch, noopProgress(), "test", 1)

	sid, active := bc.sessions.GetWithState(42, 0)
	if sid != "/tmp/test-session.jsonl" {
		t.Fatalf("expected session sess-xyz, got %q", sid)
	}
	if !active {
		t.Fatal("session should be active after Set")
	}
}

func TestBridgeRecovery_DeactivateAllPreservesIDs(t *testing.T) {
	sessions := session.NewStore()
	sessions.Set(1, 0, "/tmp/sess-a.jsonl")
	sessions.Set(2, 0, "/tmp/sess-b.jsonl")

	sessions.DeactivateAll()

	// Sessions should be cold but IDs preserved
	sid, active := sessions.GetWithState(1, 0)
	if active {
		t.Fatal("session 1 should be inactive")
	}
	if sid != "/tmp/sess-a.jsonl" {
		t.Fatalf("session 1 file should be preserved, got %q", sid)
	}

	sid, active = sessions.GetWithState(2, 0)
	if active {
		t.Fatal("session 2 should be inactive")
	}
	if sid != "/tmp/sess-b.jsonl" {
		t.Fatalf("session 2 file should be preserved, got %q", sid)
	}

	// Get still works
	if id := sessions.Get(1, 0); id != "/tmp/sess-a.jsonl" {
		t.Fatalf("Get(1) = %q, want sess-a", id)
	}
}

// --- P3: Backoff tests ---

func TestBridgeFailureTracker_RecordAndCooldown(t *testing.T) {
	var tracker bridgeFailureTracker

	// First two failures: not in cooldown
	tracker.record()
	if tracker.inCooldown() {
		t.Fatal("should not be in cooldown after 1 failure")
	}
	tracker.record()
	if tracker.inCooldown() {
		t.Fatal("should not be in cooldown after 2 failures")
	}

	// Third failure: enters cooldown
	inCooldown := tracker.record()
	if !inCooldown {
		t.Fatal("record should return true after 3 failures")
	}
	if !tracker.inCooldown() {
		t.Fatal("should be in cooldown after 3 failures")
	}
}

func TestBridgeFailureTracker_ResetClearsCooldown(t *testing.T) {
	var tracker bridgeFailureTracker

	tracker.record()
	tracker.record()
	tracker.record()

	if !tracker.inCooldown() {
		t.Fatal("should be in cooldown after 3 failures")
	}

	tracker.reset()

	if tracker.inCooldown() {
		t.Fatal("should not be in cooldown after reset")
	}
}

func TestBridgeFailureTracker_OldFailuresExpire(t *testing.T) {
	var tracker bridgeFailureTracker

	// Manually add old failures outside the window
	tracker.mu.Lock()
	old := time.Now().Add(-2 * time.Minute)
	tracker.failures = []time.Time{old, old}
	tracker.mu.Unlock()

	// New failure + old ones outside window = only 1 recent failure
	tracker.record()

	if tracker.inCooldown() {
		t.Fatal("should not be in cooldown — old failures should have expired")
	}
}

func TestBridgeFailureTracker_EmptyNotInCooldown(t *testing.T) {
	var tracker bridgeFailureTracker
	if tracker.inCooldown() {
		t.Fatal("empty tracker should not be in cooldown")
	}
}