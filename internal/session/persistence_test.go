package session

import (
	"path/filepath"
	"testing"
)

func TestPersistentStoreRestoresSessionsCold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")

	store, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("create persistent store: %v", err)
	}
	store.SetSession(42, 99, 100, "/tmp/pi-session.jsonl")
	store.SetCwd(42, 99, "/repo")

	restored, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("restore persistent store: %v", err)
	}

	sessionFile, active := restored.GetSessionWithState(42, 99, 100)
	if sessionFile != "/tmp/pi-session.jsonl" {
		t.Fatalf("expected restored session file, got %q", sessionFile)
	}
	if active {
		t.Fatal("restored session must be cold after daemon restart")
	}
	if cwd := restored.GetCwd(42, 99); cwd != "/repo" {
		t.Fatalf("expected restored cwd, got %q", cwd)
	}
}

func TestPersistentStoreRemovesClearedSessionFromSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")

	store, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("create persistent store: %v", err)
	}
	store.SetSession(42, 0, 100, "/tmp/pi-session.jsonl")
	store.ClearSessionForUser(42, 0, 100)

	restored, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("restore persistent store: %v", err)
	}
	if got := restored.GetSession(42, 0, 100); got != "" {
		t.Fatalf("expected cleared session to stay cleared, got %q", got)
	}
}
