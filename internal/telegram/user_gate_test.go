package telegram

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/users"
	_ "modernc.org/sqlite"
)

func newTestUserGate(t *testing.T) (*UserGate, *users.Store, *users.OnboardingStore) {
	t.Helper()
	root := t.TempDir()
	r := users.NewResolver(root)
	store := users.NewStore(r)

	dbPath := filepath.Join(t.TempDir(), "ob.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	obStore := users.NewOnboardingStore(db)
	if err := obStore.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	gate := NewUserGate(store, obStore, 0)
	return gate, store, obStore
}

func TestUserGate_NeedsOnboarding(t *testing.T) {
	gate, _, _ := newTestUserGate(t)

	state := gate.Check(1)
	if state != UserGateNeedsOnboarding {
		t.Errorf("Check() = %d, want %d (UserGateNeedsOnboarding)", state, UserGateNeedsOnboarding)
	}
}

func TestUserGate_OnboardingInProgress(t *testing.T) {
	gate, _, obStore := newTestUserGate(t)

	// Create onboarding state manually
	state := &users.OnboardingState{
		UserID: 1, ChatID: 100, ThreadID: 0,
		Step: "name", Language: "pt", FirstMsg: "olá",
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := obStore.Begin(state); err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	got := gate.Check(1)
	if got != UserGateOnboarding {
		t.Errorf("Check() = %d, want %d (UserGateOnboarding)", got, UserGateOnboarding)
	}
}

func TestUserGate_OK(t *testing.T) {
	gate, store, _ := newTestUserGate(t)

	// Create a profile
	profile := &users.Profile{
		UserID: 1, Name: "Alice", Language: "pt",
		OnboardedAt: time.Now(), LastSeenAt: time.Now(),
	}
	if err := store.Save(profile); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got := gate.Check(1)
	if got != UserGateOK {
		t.Errorf("Check() = %d, want %d (UserGateOK)", got, UserGateOK)
	}
}

func TestUserGate_BeginAndStep(t *testing.T) {
	gate, store, _ := newTestUserGate(t)

	// Begin
	greeting, err := gate.Begin(1, 100, 0, "olá")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if greeting == "" {
		t.Fatal("Begin() returned empty greeting")
	}

	// Check state
	if gate.Check(1) != UserGateOnboarding {
		t.Fatal("expected UserGateOnboarding after Begin")
	}

	// Name step
	reply, done, err := gate.Step(1, "Alice")
	if err != nil {
		t.Fatalf("Step(name) error = %v", err)
	}
	if reply == "" {
		t.Fatal("Step(name) returned empty reply")
	}
	if done {
		t.Fatal("Step(name) should not be done")
	}

	// Bio step
	_, done, err = gate.Step(1, "Sou dev")
	if err != nil {
		t.Fatalf("Step(bio) error = %v", err)
	}
	if !done {
		t.Fatal("Step(bio) should mark onboarding as done")
	}

	// Profile should exist
	if !store.Exists(1) {
		t.Fatal("profile should exist after onboarding completes")
	}

	// Check state is now OK
	if gate.Check(1) != UserGateOK {
		t.Fatal("expected UserGateOK after onboarding completes")
	}
}

func TestUserGate_FirstMsgPreserved(t *testing.T) {
	gate, _, _ := newTestUserGate(t)

	firstMsg := "olá mundo"
	_, err := gate.Begin(1, 100, 0, firstMsg)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	// Check FirstMsg returns the original message
	got := gate.FirstMsg(1)
	if got != firstMsg {
		t.Errorf("FirstMsg() = %q, want %q", got, firstMsg)
	}

	// Step through onboarding
	gate.Step(1, "Alice")
	gate.Step(1, "skip")

	// After Complete, the state is deleted
	gate.Complete(1)
	got = gate.FirstMsg(1)
	if got != "" {
		t.Errorf("FirstMsg() after Complete should be empty, got %q", got)
	}
}

func TestUserGate_Complete(t *testing.T) {
	gate, _, obStore := newTestUserGate(t)

	_, err := gate.Begin(1, 100, 0, "hello")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	if err := gate.Complete(1); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	// State should be gone
	state, err := obStore.Get(1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if state != nil {
		t.Fatal("state should be nil after Complete")
	}
}

func TestUserGate_LanguageDetection(t *testing.T) {
	gate, _, _ := newTestUserGate(t)

	// PT greeting
	greeting, err := gate.Begin(1, 100, 0, "olá tudo bem")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	expectedPt := "Olá! Eu sou a Aurelia. Como devo te chamar?"
	if greeting != expectedPt {
		t.Errorf("PT greeting = %q, want %q", greeting, expectedPt)
	}

	// EN greeting
	greeting, err = gate.Begin(2, 100, 0, "hello how are you")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	expectedEn := "Hello! I'm Aurelia. What should I call you?"
	if greeting != expectedEn {
		t.Errorf("EN greeting = %q, want %q", greeting, expectedEn)
	}
}

// TestUserGate_Check_ZeroID ensures Check handles userID=0 gracefully.
func TestUserGate_Check_ZeroID(t *testing.T) {
	gate, _, _ := newTestUserGate(t)

	// User 0 doesn't exist, no onboarding state → NeedsOnboarding
	state := gate.Check(0)
	if state != UserGateNeedsOnboarding {
		t.Errorf("Check(0) = %d, want %d (UserGateNeedsOnboarding)", state, UserGateNeedsOnboarding)
	}
}

// TestUserGate_Step_NoState tests that Step returns an error when no state exists.
func TestUserGate_Step_NoState(t *testing.T) {
	gate, _, _ := newTestUserGate(t)

	_, _, err := gate.Step(999, "hello")
	if err == nil {
		t.Fatal("expected error for non-existent onboarding state")
	}
}
