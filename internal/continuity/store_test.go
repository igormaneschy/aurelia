package continuity

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStore_UpsertGetRoundtrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	state := ConversationState{
		ChatID:               42,
		ThreadID:             1,
		UserID:               100,
		CWD:                  "/repo/project",
		ActiveGoal:           "Implement feature X",
		LastUserIntent:       "Please implement X",
		LastAssistantSummary: "Implemented X, tests pass",
		LastCheckpoint:       "Status: completed",
		LastRunID:            "run-123",
		LastRunStatus:        "completed",
		LastTools:            "Read, Write, Edit",
		SessionID:            "sid-abc",
		SessionCold:          false,
		ResetReason:          "",
		UpdatedAt:            now,
	}

	if err := store.Upsert(ctx, state); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get(ctx, 42, 1, 100)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil, expected state")
	}

	if got.CWD != "/repo/project" {
		t.Fatalf("CWD = %q, want %q", got.CWD, "/repo/project")
	}
	if got.LastRunStatus != "completed" {
		t.Fatalf("LastRunStatus = %q, want %q", got.LastRunStatus, "completed")
	}
	if got.SessionCold {
		t.Fatal("SessionCold = true, want false")
	}
	if !got.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %v, want %v", got.UpdatedAt, now)
	}
}

func TestStore_GetMissingState_ReturnsNil(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	got, err := store.Get(ctx, 999, 0, 0)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing state")
	}
}

func TestStore_UpsertOverwritesExisting(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	_ = store.Upsert(ctx, ConversationState{
		ChatID:   42,
		ThreadID: 0,
		UserID:               1,
		CWD:      "/old/path",
		UpdatedAt: now,
	})

	_ = store.Upsert(ctx, ConversationState{
		ChatID:   42,
		ThreadID: 0,
		UserID:               1,
		CWD:      "/new/path",
		UpdatedAt: now.Add(time.Hour),
	})

	got, _ := store.Get(ctx, 42, 0, 1)
	if got == nil {
		t.Fatal("expected state after second upsert")
	}
	if got.CWD != "/new/path" {
		t.Fatalf("CWD = %q, want %q", got.CWD, "/new/path")
	}
}

func TestStore_PatchOnlyUpdatesProvidedFields(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	_ = store.Upsert(ctx, ConversationState{
		ChatID:   42,
		ThreadID: 0,
		UserID:               1,
		CWD:      "/repo",
		ActiveGoal: "Old goal",
		UpdatedAt: now,
	})

	cold := true
	reason := "auto-reset"
	err := store.Patch(ctx, ConversationKeyFor(42, 0, 1), StatePatch{
		SessionCold: &cold,
		ResetReason: &reason,
		UpdatedAt:   now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}

	got, _ := store.Get(ctx, 42, 0, 1)
	if got == nil {
		t.Fatal("expected state after patch")
	}
	// CWD and ActiveGoal should be preserved (not zeroed)
	if got.CWD != "/repo" {
		t.Fatalf("CWD = %q, want preserved %q", got.CWD, "/repo")
	}
	if got.ActiveGoal != "Old goal" {
		t.Fatalf("ActiveGoal = %q, want preserved %q", got.ActiveGoal, "Old goal")
	}
	// Patch fields should be set
	if !got.SessionCold {
		t.Fatal("SessionCold = false, want true")
	}
	if got.ResetReason != "auto-reset" {
		t.Fatalf("ResetReason = %q, want %q", got.ResetReason, "auto-reset")
	}
}

func TestStore_PatchCreatesNewRowWhenMissing(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	cold := true
	reason := "fresh-cold"

	err := store.Patch(ctx, ConversationKeyFor(1, 2, 0), StatePatch{
		CWD:         strPtr("/workspace"),
		SessionCold: &cold,
		ResetReason: &reason,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("Patch on missing row: %v", err)
	}

	got, _ := store.Get(ctx, 1, 2, 0)
	if got == nil {
		t.Fatal("expected state after patch on missing row")
	}
	if got.CWD != "/workspace" {
		t.Fatalf("CWD = %q, want %q", got.CWD, "/workspace")
	}
	if !got.SessionCold {
		t.Fatal("SessionCold should be true")
	}
}

func TestStore_ReopenPreservesState(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "continuity.db")

	store1, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	_ = store1.Upsert(ctx, ConversationState{
		ChatID:   42,
		ThreadID: 0,
		UserID:               1,
		CWD:      "/persisted",
		UpdatedAt: now,
	})
	store1.Close()

	store2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore reopen: %v", err)
	}
	defer store2.Close()

	got, _ := store2.Get(ctx, 42, 0, 1)
	if got == nil {
		t.Fatal("expected state after reopen")
	}
	if got.CWD != "/persisted" {
		t.Fatalf("CWD = %q, want %q", got.CWD, "/persisted")
	}
}

func TestStore_GetDifferentThread(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	_ = store.Upsert(ctx, ConversationState{
		ChatID:   42,
		ThreadID: 1,
		UserID:               1,
		CWD:      "/thread-1",
		UpdatedAt: now,
	})
	_ = store.Upsert(ctx, ConversationState{
		ChatID:   42,
		ThreadID: 2,
		UserID:               1,
		CWD:      "/thread-2",
		UpdatedAt: now,
	})

	got, _ := store.Get(ctx, 42, 1, 1)
	if got == nil {
		t.Fatal("expected thread 1 state, got nil")
	}
	if got.CWD != "/thread-1" {
		t.Fatalf("thread 1 CWD = %q, want %q", got.CWD, "/thread-1")
	}

	got, _ = store.Get(ctx, 42, 2, 1)
	if got == nil {
		t.Fatal("expected thread 2 state, got nil")
	}
	if got.CWD != "/thread-2" {
		t.Fatalf("thread 2 CWD = %q, want %q", got.CWD, "/thread-2")
	}

	got, _ = store.Get(ctx, 42, 3, 1)
	if got != nil {
		t.Fatal("expected nil for non-existent thread")
	}
}

func TestStore_MigrationFromLegacySchema(t *testing.T) {
	// Create a database with the old 2-column PK schema (pre-UserID migration),
	// then open with NewSQLiteStore which should detect and migrate it.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy_continuity.db")

	// Simulate old schema: 2-column PK, no user_id column.
	f, err := os.OpenFile(dbPath, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// Create old schema.
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS conversation_state (
		chat_id INTEGER NOT NULL,
		thread_id INTEGER NOT NULL,
		cwd TEXT DEFAULT '',
		active_goal TEXT DEFAULT '',
		last_user_intent TEXT DEFAULT '',
		last_assistant_summary TEXT DEFAULT '',
		last_checkpoint TEXT DEFAULT '',
		last_run_id TEXT DEFAULT '',
		last_run_status TEXT DEFAULT '',
		last_tools TEXT DEFAULT '',
		session_id TEXT DEFAULT '',
		session_cold INTEGER NOT NULL DEFAULT 0,
		reset_reason TEXT DEFAULT '',
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (chat_id, thread_id)
	)`)
	if err != nil {
		t.Fatalf("create old schema: %v", err)
	}

	// Insert a legacy row.
	_, err = db.Exec(`
	INSERT INTO conversation_state (chat_id, thread_id, cwd, updated_at)
	VALUES (1, 0, '/legacy/path', 1000000)`)
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	db.Close()

	// Now open with NewSQLiteStore — migration should run automatically.
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore after legacy schema: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Legacy row should be accessible with userID=0.
	state, err := store.Get(ctx, 1, 0, 0)
	if err != nil {
		t.Fatalf("Get legacy row: %v", err)
	}
	if state == nil {
		t.Fatal("legacy row not found after migration")
	}
	if state.CWD != "/legacy/path" {
		t.Fatalf("CWD = %q, want %q", state.CWD, "/legacy/path")
	}

	// Write a new row with explicit UserID — should work with new PK.
	now := time.Now().Truncate(time.Second)
	err = store.Upsert(ctx, ConversationState{
		ChatID:    2,
		ThreadID:  0,
		UserID:    42,
		CWD:       "/new/path",
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("Upsert after migration: %v", err)
	}

	// Read back with matching UserID.
	state, err = store.Get(ctx, 2, 0, 42)
	if err != nil {
		t.Fatalf("Get new row: %v", err)
	}
	if state == nil || state.CWD != "/new/path" {
		t.Fatal("new row with UserID not found")
	}

	// Reopen — migration should be a no-op (user_id already in PK).
	store.Close()
	store2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore reopen: %v", err)
	}
	defer store2.Close()

	state, err = store2.Get(ctx, 1, 0, 0)
	if err != nil || state == nil || state.CWD != "/legacy/path" {
		t.Fatal("legacy row lost after reopen")
	}
}

func TestEscapeUntrusted_EscapesAmpersandBeforeAngleBrackets(t *testing.T) {
	// &amp; must come before &lt; to prevent double-escaping
	input := "& < > &lt;"
	got := escapeUntrusted(input)
	want := "&amp; &lt; &gt; &amp;lt;"
	if got != want {
		t.Fatalf("escapeUntrusted(%q) = %q, want %q", input, got, want)
	}
}

func TestEscapeUntrusted_DoubleEscapesEntities(t *testing.T) {
	// escapeUntrusted is NOT idempotent — & is replaced first, so already-escaped
	// entities get their & escaped again. This is correct for delimiter-injection
	// defense: every & → &amp; before < or > are processed.
	input := "&amp; &lt; &gt;"
	got := escapeUntrusted(input)
	if got != "&amp;amp; &amp;lt; &amp;gt;" {
		t.Fatalf("escapeUntrusted(%q) = %q, want double-escaped", input, got)
	}
}

func TestEscapeUntrusted_HandlesEmptyString(t *testing.T) {
	if got := escapeUntrusted(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestEscapeUntrusted_NoSpecialChars(t *testing.T) {
	input := "hello world 123"
	got := escapeUntrusted(input)
	if got != input {
		t.Fatalf("escapeUntrusted(%q) = %q, want unchanged", input, got)
	}
}

// helpers

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "continuity_test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return store
}

func strPtr(s string) *string { return &s }
