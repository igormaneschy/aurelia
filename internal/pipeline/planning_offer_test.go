package pipeline

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/igormaneschy/aurelia/internal/planning"
	"github.com/igormaneschy/aurelia/internal/session"
)

// setupOfferTestStore creates an in-memory SQLite store with migrations applied.
func setupOfferTestStore(t *testing.T) *planning.SQLiteStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := planning.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	return planning.NewSQLiteStore(db)
}

func TestMaybeOfferPlanning_NoIntent(t *testing.T) {
	store := setupOfferTestStore(t)
	s := &Service{
		planningStore: store,
	}
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}

	offered, msg := s.maybeOfferPlanning(t.Context(), "bom dia", key)
	if offered {
		t.Fatal("expected no offer for non-planning text")
	}
	if msg != "" {
		t.Fatalf("expected empty message for non-planning text, got %q", msg)
	}
}

func TestMaybeOfferPlanning_WithIntent(t *testing.T) {
	store := setupOfferTestStore(t)
	s := &Service{
		planningStore: store,
	}
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}

	offered, msg := s.maybeOfferPlanning(t.Context(), "implementa uma feature", key)
	if !offered {
		t.Fatal("expected offer for planning text")
	}
	if msg == "" {
		t.Fatal("expected non-empty offer message")
	}
	if msg != planningOfferMessage {
		t.Fatalf("expected offer message %q, got %q", planningOfferMessage, msg)
	}
}

func TestMaybeOfferPlanning_Throttled(t *testing.T) {
	store := setupOfferTestStore(t)
	s := &Service{
		planningStore: store,
	}
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}

	// First offer should succeed.
	offered, _ := s.maybeOfferPlanning(t.Context(), "crie uma task", key)
	if !offered {
		t.Fatal("expected first offer to be accepted")
	}

	// Second offer with different text but within TTL — should be throttled.
	offered, _ = s.maybeOfferPlanning(t.Context(), "implementa outra coisa", key)
	if offered {
		t.Fatal("expected second offer to be throttled (within TTL)")
	}
}

func TestMaybeOfferPlanning_ActiveState(t *testing.T) {
	store := setupOfferTestStore(t)
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}
	localKey := sessionKey(key.ChatID, key.ThreadID, key.UserID)

	s := &Service{
		planningStore: store,
	}
	// Populate planningStates with an active state.
	s.planningStates.Store(localKey, &planning.State{
		Status: planning.StatusActive,
	})

	offered, msg := s.maybeOfferPlanning(t.Context(), "implementa uma feature", key)
	if offered {
		t.Fatal("expected no offer when active planning state exists")
	}
	if msg != "" {
		t.Fatalf("expected empty message when active state exists, got %q", msg)
	}
}

func TestMaybeOfferPlanning_ApprovalTerms(t *testing.T) {
	store := setupOfferTestStore(t)
	s := &Service{
		planningStore: store,
	}
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}

	tests := []struct {
		name string
		text string
	}{
		{"aprovado", "aprovado"},
		{"pode fazer", "pode fazer isso agora"},
		{"execute", "execute agora"},
		{"approved", "approved"},
		{"bora", "bora"},
		{"manda ver", "manda ver"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			offered, msg := s.maybeOfferPlanning(t.Context(), tc.text, key)
			if offered {
				t.Fatalf("expected no offer for approval term %q, got offered=true msg=%q", tc.text, msg)
			}
			if msg != "" {
				t.Fatalf("expected empty message for approval term %q, got %q", tc.text, msg)
			}
		})
	}
}

func TestMaybeOfferPlanning_NilStore(t *testing.T) {
	s := &Service{}
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}

	offered, msg := s.maybeOfferPlanning(t.Context(), "implementa algo", key)
	if offered {
		t.Fatal("expected no offer when planningStore is nil")
	}
	if msg != "" {
		t.Fatalf("expected empty message when planningStore is nil, got %q", msg)
	}
}

func TestMaybeOfferPlanning_NonOfferStore(t *testing.T) {
	// A store that implements planning.Store but NOT planning.OfferStore
	// should not cause a panic or offer.
	type storeOnly struct {
		planning.Store
	}
	s := &Service{
		planningStore: &storeOnly{},
	}
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}

	offered, msg := s.maybeOfferPlanning(t.Context(), "implementa algo", key)
	if offered {
		t.Fatal("expected no offer when store does not implement OfferStore")
	}
	if msg != "" {
		t.Fatalf("expected empty message, got %q", msg)
	}
}
