package session

import (
	"os"
	"testing"
)

func TestStore_MarkFailure(t *testing.T) {
	s := NewStore()
	s.SetSession(1, 2, 100, "sess-abc")

	if !s.MarkFailure(1, 2, 100, "timeout") {
		t.Fatal("expected MarkFailure to return true for existing session")
	}

	// Session should be deactivated
	_, active := s.GetSessionWithState(1, 2, 100)
	if active {
		t.Fatal("session should be inactive after MarkFailure")
	}

	signals := s.GetHealthSignals(1, 2, 100)
	if signals.LastError != "timeout" {
		t.Fatalf("expected last_error=timeout, got %q", signals.LastError)
	}
}

func TestStore_MarkFailure_NonExistent(t *testing.T) {
	s := NewStore()
	if s.MarkFailure(999, 0, 0, "fail") {
		t.Fatal("expected MarkFailure to return false for non-existent session")
	}
}

func TestStore_MarkProcessDeath(t *testing.T) {
	s := NewStore()
	s.SetSession(1, 2, 100, "sess-abc")

	s.MarkProcessDeath(1, 2, 100)
	s.MarkProcessDeath(1, 2, 100)

	signals := s.GetHealthSignals(1, 2, 100)
	if signals.RecentProcessDeaths != 2 {
		t.Fatalf("expected 2 process deaths, got %d", signals.RecentProcessDeaths)
	}
	if signals.Active {
		t.Fatal("session should be inactive after process death")
	}
}

func TestStore_MarkProcessDeath_NonExistent(t *testing.T) {
	s := NewStore()
	s.MarkProcessDeath(999, 0, 0) // should not panic
}

func TestStore_MarkEmptyResult(t *testing.T) {
	s := NewStore()
	s.SetSession(1, 2, 100, "sess-abc")

	s.MarkEmptyResult(1, 2, 100)

	signals := s.GetHealthSignals(1, 2, 100)
	if signals.RecentEmptyResults != 1 {
		t.Fatalf("expected 1 empty result, got %d", signals.RecentEmptyResults)
	}
	if signals.LastError != "empty result after work" {
		t.Fatalf("expected last_error, got %q", signals.LastError)
	}
}

func TestStore_MarkEmptyResult_NonExistent(t *testing.T) {
	s := NewStore()
	s.MarkEmptyResult(999, 0, 0) // should not panic
}

func TestStore_ClearFailureState(t *testing.T) {
	s := NewStore()
	s.SetSession(1, 2, 100, "sess-abc")

	s.MarkFailure(1, 2, 100, "timeout")
	s.MarkEmptyResult(1, 2, 100)
	s.MarkProcessDeath(1, 2, 100)

	s.ClearFailureState(1, 2, 100)

	signals := s.GetHealthSignals(1, 2, 100)
	if signals.LastError != "" {
		t.Fatalf("expected empty last_error after clear, got %q", signals.LastError)
	}
	if signals.RecentEmptyResults != 0 {
		t.Fatalf("expected 0 empty results after clear, got %d", signals.RecentEmptyResults)
	}
	if signals.RecentProcessDeaths != 0 {
		t.Fatalf("expected 0 process deaths after clear, got %d", signals.RecentProcessDeaths)
	}
}

func TestStore_ClearFailureState_NonExistent(t *testing.T) {
	s := NewStore()
	s.ClearFailureState(999, 0, 0) // should not panic
}

func TestStore_GetSuspectCount(t *testing.T) {
	s := NewStore()
	s.SetSession(1, 2, 100, "sess-abc")

	s.MarkFailure(1, 2, 100, "timeout")
	s.MarkFailure(1, 2, 100, "timeout again")

	if count := s.GetSuspectCount(1, 2, 100); count != 2 {
		t.Fatalf("expected suspect count 2, got %d", count)
	}

	s.ClearFailureState(1, 2, 100)
	if count := s.GetSuspectCount(1, 2, 100); count != 0 {
		t.Fatalf("expected suspect count 0 after clear, got %d", count)
	}
}

func TestStore_GetSuspectCount_NonExistent(t *testing.T) {
	s := NewStore()
	if count := s.GetSuspectCount(999, 0, 0); count != 0 {
		t.Fatalf("expected 0 for non-existent session, got %d", count)
	}
}

func TestStore_GetHealthSignals_NonExistent(t *testing.T) {
	s := NewStore()
	signals := s.GetHealthSignals(999, 0, 0)
	if signals.Active {
		t.Fatal("expected inactive for non-existent session")
	}
}

func TestStore_GetHealthSignals_ActiveSession(t *testing.T) {
	s := NewStore()
	s.SetSession(1, 2, 100, "sess-abc")

	signals := s.GetHealthSignals(1, 2, 100)
	if !signals.Active {
		t.Fatal("expected active session")
	}
	if signals.RecentEmptyResults != 0 {
		t.Fatalf("expected 0 empty results, got %d", signals.RecentEmptyResults)
	}
}

func TestStore_MarkFailure_UserIsolation(t *testing.T) {
	s := NewStore()
	s.SetSession(1, 2, 100, "sess-user-100")
	s.SetSession(1, 2, 200, "sess-user-200")

	s.MarkFailure(1, 2, 100, "timeout")

	// User 100 should be affected
	signals100 := s.GetHealthSignals(1, 2, 100)
	if signals100.Active {
		t.Fatal("user 100 should be inactive")
	}
	if signals100.LastError != "timeout" {
		t.Fatalf("expected last_error=timeout for user 100, got %q", signals100.LastError)
	}

	// User 200 should be unaffected
	signals200 := s.GetHealthSignals(1, 2, 200)
	if !signals200.Active {
		t.Fatal("user 200 should remain active")
	}
	if signals200.LastError != "" {
		t.Fatalf("expected no error for user 200, got %q", signals200.LastError)
	}
}

func TestStore_FailureMetadataSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/sessions.json"
	store, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("create persistent store: %v", err)
	}

	store.SetSession(42, 99, 100, "/tmp/pi-session.jsonl")
	store.MarkFailure(42, 99, 100, "bridge timeout")

	restored, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	signals := restored.GetHealthSignals(42, 99, 100)
	if signals.LastError != "bridge timeout" {
		t.Fatalf("expected last_error to survive restart, got %q", signals.LastError)
	}
	if signals.RecentEmptyResults != 0 {
		t.Fatalf("expected empty results to survive as 0, got %d", signals.RecentEmptyResults)
	}
}

func TestStore_OldSnapshotWithoutMetadata(t *testing.T) {
	// Simulate an old snapshot without the new metadata fields
	oldJSON := `{
		"sessions": [
			{
				"chat_id": 42,
				"thread_id": 99,
				"user_id": 100,
				"session_file": "/tmp/pi-session.jsonl",
				"last_seen": "2026-05-20T10:00:00Z"
			}
		]
	}`

	path := t.TempDir() + "/sessions.json"
	if err := os.WriteFile(path, []byte(oldJSON), 0o600); err != nil {
		t.Fatalf("write old snapshot: %v", err)
	}

	store, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("load old snapshot: %v", err)
	}

	sessionFile, active := store.GetSessionWithState(42, 99, 100)
	if sessionFile != "/tmp/pi-session.jsonl" {
		t.Fatalf("expected session file to load, got %q", sessionFile)
	}
	if active {
		t.Fatal("restored session must be cold")
	}

	// Metadata should be zero-values (backward compatible)
	signals := store.GetHealthSignals(42, 99, 100)
	if signals.LastError != "" {
		t.Fatalf("expected empty last_error for old snapshot, got %q", signals.LastError)
	}
	if signals.RecentEmptyResults != 0 {
		t.Fatalf("expected 0 empty results for old snapshot, got %d", signals.RecentEmptyResults)
	}
	if signals.RecentProcessDeaths != 0 {
		t.Fatalf("expected 0 process deaths for old snapshot, got %d", signals.RecentProcessDeaths)
	}
}

func TestStore_MarkFailureDoesNotCreateNewEntry(t *testing.T) {
	s := NewStore()
	s.MarkFailure(1, 2, 100, "fail") // no session exists

	if s.GetSession(1, 2, 100) != "" {
		t.Fatal("MarkFailure should not create a session entry")
	}
}

func TestStore_MarkProcessDeathDoesNotCreateNewEntry(t *testing.T) {
	s := NewStore()
	s.MarkProcessDeath(1, 2, 100) // no session exists

	if s.GetSession(1, 2, 100) != "" {
		t.Fatal("MarkProcessDeath should not create a session entry")
	}
}

func TestStore_MarkEmptyResultDoesNotCreateNewEntry(t *testing.T) {
	s := NewStore()
	s.MarkEmptyResult(1, 2, 100) // no session exists

	if s.GetSession(1, 2, 100) != "" {
		t.Fatal("MarkEmptyResult should not create a session entry")
	}
}
