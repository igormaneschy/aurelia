package planning

import (
	"context"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/session"
)

func TestSQLiteStore_Roundtrip(t *testing.T) {
	db := NewTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	handoffAt := now.Add(-5 * time.Minute)

	key := session.SessionKey{ChatID: 1, ThreadID: 2, UserID: 3}
	state := &State{
		Key:     key,
		Version: 0,
		Status:  StatusActive,
		Phase:   PhaseDesign,
		CWD:     "/tmp/project",
		ProjectCtx: &ProjectContext{
			HasGit:    true,
			HasReadme: false,
			Layouts:   []string{"tlc"},
			Stacks:    []string{"go"},
			DiscoveredAt: now,
		},
		Materialized: []Artifact{
			{Path: "/tmp/plan.md", Phase: PhaseDesign, Tool: "Write", CreatedAt: now},
		},
		LastHandoffError: "previous error",
		HandoffStartedAt: &handoffAt,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	// Save.
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Get and verify.
	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil, expected state")
	}

	if got.Key != key {
		t.Errorf("Key = %+v, want %+v", got.Key, key)
	}
	if got.Status != StatusActive {
		t.Errorf("Status = %q, want %q", got.Status, StatusActive)
	}
	if got.Phase != PhaseDesign {
		t.Errorf("Phase = %q, want %q", got.Phase, PhaseDesign)
	}
	if got.CWD != "/tmp/project" {
		t.Errorf("CWD = %q, want %q", got.CWD, "/tmp/project")
	}
	if got.ProjectCtx == nil || !got.ProjectCtx.HasGit {
		t.Error("ProjectCtx.HasGit = false, want true")
	}
	if len(got.Materialized) != 1 || got.Materialized[0].Path != "/tmp/plan.md" {
		t.Errorf("Materialized = %+v, want [plan.md]", got.Materialized)
	}
	if got.LastHandoffError != "previous error" {
		t.Errorf("LastHandoffError = %q, want %q", got.LastHandoffError, "previous error")
	}
	if got.HandoffStartedAt == nil || !got.HandoffStartedAt.Equal(handoffAt) {
		t.Errorf("HandoffStartedAt = %v, want %v", got.HandoffStartedAt, handoffAt)
	}

	// Verify version was incremented on save.
	if state.Version != 1 {
		t.Errorf("Version after save = %d, want 1", state.Version)
	}
	if state.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set after save")
	}
	if state.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set after save")
	}
}

func TestSQLiteStore_GetNotFound(t *testing.T) {
	db := NewTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	got, err := store.Get(ctx, session.SessionKey{ChatID: 999, ThreadID: 0, UserID: 0})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatal("Get returned non-nil for non-existent key")
	}
}

func TestSQLiteStore_Conflict(t *testing.T) {
	db := NewTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 0}
	state := &State{
		Key:    key,
		Status: StatusActive,
		Phase:  PhaseSpecify,
	}

	// First save — success.
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	// Simulate concurrent modification: save a copy of the original version.
	state2 := &State{
		Key:     key,
		Version: 0, // stale version
		Status:  StatusCompleted,
		Phase:   PhaseReview,
	}
	err := store.Save(ctx, state2)
	if err == nil {
		t.Fatal("expected ErrConflict, got nil")
	}
	if err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestSQLiteStore_Delete(t *testing.T) {
	db := NewTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	key := session.SessionKey{ChatID: 1, ThreadID: 2, UserID: 3}
	state := &State{Key: key, Status: StatusActive, Phase: PhaseSpecify}

	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after Delete: %v", err)
	}
	if got != nil {
		t.Fatal("Get returned non-nil after Delete")
	}

	// Idempotent: delete again should not error.
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete (idempotent): %v", err)
	}
}

func TestSQLiteStore_ListByUser(t *testing.T) {
	db := NewTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	// Create states for user 42 with different chat/thread combos.
	keys := []session.SessionKey{
		{ChatID: 1, ThreadID: 0, UserID: 42},
		{ChatID: 2, ThreadID: 0, UserID: 42},
		{ChatID: 3, ThreadID: 5, UserID: 42},
	}
	for _, k := range keys {
		if err := store.Save(ctx, &State{Key: k, Status: StatusActive, Phase: PhaseSpecify}); err != nil {
			t.Fatalf("Save %+v: %v", k, err)
		}
	}

	// Create a state for a different user (should not appear).
	otherKey := session.SessionKey{ChatID: 99, ThreadID: 0, UserID: 7}
	if err := store.Save(ctx, &State{Key: otherKey, Status: StatusActive, Phase: PhaseDesign}); err != nil {
		t.Fatalf("Save other user: %v", err)
	}

	states, err := store.ListByUser(ctx, 42)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(states) != 3 {
		t.Fatalf("ListByUser returned %d states, want 3", len(states))
	}

	found := make(map[int64]bool) // chat_id -> seen
	for _, st := range states {
		found[st.Key.ChatID] = true
	}
	for _, k := range keys {
		if !found[k.ChatID] {
			t.Errorf("missing state for chat_id=%d", k.ChatID)
		}
	}

	// Empty list for user with no states.
	empty, err := store.ListByUser(ctx, 999)
	if err != nil {
		t.Fatalf("ListByUser (empty): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty list, got %d states", len(empty))
	}
}

func TestSQLiteStore_GC(t *testing.T) {
	db := NewTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	oldKey := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 1}
	newKey := session.SessionKey{ChatID: 2, ThreadID: 0, UserID: 1}

	// Create a state with an old updated_at by saving, then backdating it.
	if err := store.Save(ctx, &State{Key: oldKey, Status: StatusActive, Phase: PhaseSpecify}); err != nil {
		t.Fatalf("Save old: %v", err)
	}
	_, err := db.Exec(`UPDATE planning_state SET updated_at = ? WHERE chat_id=? AND thread_id=? AND user_id=?`,
		100, oldKey.ChatID, oldKey.ThreadID, oldKey.UserID)
	if err != nil {
		t.Fatalf("backdate old state: %v", err)
	}

	// Create a recent state.
	if err := store.Save(ctx, &State{Key: newKey, Status: StatusActive, Phase: PhaseDesign}); err != nil {
		t.Fatalf("Save new: %v", err)
	}

	// GC with maxAge that should clean the old state but keep the new one.
	if err := store.GC(ctx, 1*time.Hour); err != nil {
		t.Fatalf("GC: %v", err)
	}

	// Old should be gone.
	got, err := store.Get(ctx, oldKey)
	if err != nil {
		t.Fatalf("Get old: %v", err)
	}
	if got != nil {
		t.Error("old state should have been removed by GC")
	}

	// New should remain.
	got, err = store.Get(ctx, newKey)
	if err != nil {
		t.Fatalf("Get new: %v", err)
	}
	if got == nil {
		t.Error("new state should still exist after GC")
	}
}

func TestSQLiteStore_OfferThrottle(t *testing.T) {
	db := NewTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 1}
	intentHash := "abc123"

	// First offer — should be accepted (new).
	accepted, err := store.RecordOffer(ctx, key, intentHash, 5*time.Minute)
	if err != nil {
		t.Fatalf("first RecordOffer: %v", err)
	}
	if !accepted {
		t.Fatal("first offer should be accepted (new)")
	}

	// Second offer within TTL — should be rejected (throttled).
	accepted, err = store.RecordOffer(ctx, key, intentHash, 5*time.Minute)
	if err != nil {
		t.Fatalf("second RecordOffer: %v", err)
	}
	if accepted {
		t.Fatal("second offer should be rejected (within TTL)")
	}

	// HasRecentOffer should return true within TTL.
	recent, err := store.HasRecentOffer(ctx, key, intentHash)
	if err != nil {
		t.Fatalf("HasRecentOffer: %v", err)
	}
	if !recent {
		t.Fatal("HasRecentOffer should be true (unexpired offer exists)")
	}

	// HasRecentOffer for non-existent hash should return false.
	recent, err = store.HasRecentOffer(ctx, key, "nonexistent")
	if err != nil {
		t.Fatalf("HasRecentOffer (non-existent): %v", err)
	}
	if recent {
		t.Fatal("HasRecentOffer should be false for non-existent hash")
	}
}

func TestSQLiteStore_OfferGC(t *testing.T) {
	db := NewTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 1}

	// Insert an expired offer directly.
	_, err := db.Exec(`
		INSERT INTO planning_offer (chat_id, thread_id, user_id, intent_hash, offered_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		key.ChatID, key.ThreadID, key.UserID, "expired", 100, 100)
	if err != nil {
		t.Fatalf("insert expired offer: %v", err)
	}

	// Insert a non-expired offer.
	_, err = db.Exec(`
		INSERT INTO planning_offer (chat_id, thread_id, user_id, intent_hash, offered_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		key.ChatID, key.ThreadID, key.UserID, "live", 100, time.Now().Add(1*time.Hour).Unix())
	if err != nil {
		t.Fatalf("insert live offer: %v", err)
	}

	// Run GCOffers.
	if err := store.GCOffers(ctx); err != nil {
		t.Fatalf("GCOffers: %v", err)
	}

	// Expired offer should be gone.
	recent, err := store.HasRecentOffer(ctx, key, "expired")
	if err != nil {
		t.Fatalf("HasRecentOffer expired: %v", err)
	}
	if recent {
		t.Error("expired offer should have been removed by GCOffers")
	}

	// Live offer should remain.
	recent, err = store.HasRecentOffer(ctx, key, "live")
	if err != nil {
		t.Fatalf("HasRecentOffer live: %v", err)
	}
	if !recent {
		t.Error("live offer should remain after GCOffers")
	}
}
