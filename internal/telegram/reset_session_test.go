package telegram

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/session"
)

// A reset must clear the continuity context of the conversation, not only the
// PI session. The continuity state (last intent / last summary) is injected
// into the next prompt, so leaving it behind made the message after a reset
// answer from the previous conversation.
func TestResetSession_ClearsContinuityContext(t *testing.T) {
	t.Parallel()

	contDir := t.TempDir()
	contStore, err := continuity.NewSQLiteStore(filepath.Join(contDir, "cont.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer contStore.Close()

	ctx := t.Context()
	if err := contStore.Upsert(ctx, continuity.ConversationState{
		ChatID:               42,
		ThreadID:             0,
		UserID:               100,
		LastUserIntent:       "analisar o modulo de auth",
		LastAssistantSummary: "o modulo de auth usa JWT",
		LastCheckpoint:       "checkpoint antigo",
		UpdatedAt:            time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	sessions := session.NewStore()
	sessions.SetSession(42, 0, 100, "/tmp/old-session.jsonl")

	bc := &BotController{sessions: sessions, continuity: contStore}
	bc.ResetSession(42, 0, 100, true, true)

	if sid := sessions.GetSession(42, 0, 100); sid != "" {
		t.Fatalf("session should be cleared, got %q", sid)
	}

	state, err := contStore.Get(ctx, 42, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("continuity row should be kept with the reset reason")
	}
	if state.LastUserIntent != "" || state.LastAssistantSummary != "" {
		t.Fatalf("reset did not clear carried context: %+v", state)
	}
	if state.ResetReason != continuity.UserResetReason {
		t.Fatalf("reset reason = %q, want %q", state.ResetReason, continuity.UserResetReason)
	}
}

// A reset must not touch the continuity of other conversations.
func TestResetSession_LeavesOtherConversationsIntact(t *testing.T) {
	t.Parallel()

	contDir := t.TempDir()
	contStore, err := continuity.NewSQLiteStore(filepath.Join(contDir, "cont.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer contStore.Close()

	ctx := t.Context()
	for _, chatID := range []int64{42, 43} {
		if err := contStore.Upsert(ctx, continuity.ConversationState{
			ChatID:         chatID,
			ThreadID:       0,
			UserID:         100,
			LastUserIntent: "intent da conversa",
			UpdatedAt:      time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	bc := &BotController{sessions: session.NewStore(), continuity: contStore}
	bc.ResetSession(42, 0, 100, true, true)

	other, err := contStore.Get(ctx, 43, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if other == nil || other.LastUserIntent != "intent da conversa" {
		t.Fatalf("other conversation was affected: %+v", other)
	}
}
