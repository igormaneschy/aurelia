package session

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type snapshot struct {
	Sessions []sessionSnapshot `json:"sessions"`
	Cwds     []cwdSnapshot     `json:"cwds"`
}

type sessionSnapshot struct {
	ChatID              int64     `json:"chat_id"`
	ThreadID            int       `json:"thread_id"`
	UserID              int64     `json:"user_id"`
	SessionFile         string    `json:"session_file"`
	LastSeen            time.Time `json:"last_seen"`
	LastFailure         string    `json:"last_failure,omitempty"`
	LastFailureAt       time.Time `json:"last_failure_at,omitempty"`
	SuspectCount        int       `json:"suspect_count,omitempty"`
	EmptyResults        int       `json:"empty_results,omitempty"`
	ProcessDeaths       int       `json:"process_deaths,omitempty"`
	LastLifecycleAction string    `json:"last_lifecycle_action,omitempty"`
}

type cwdSnapshot struct {
	ChatID   int64     `json:"chat_id"`
	ThreadID int       `json:"thread_id"`
	Cwd      string    `json:"cwd"`
	LastSeen time.Time `json:"last_seen"`
}

// NewPersistentStore creates a Store backed by a JSON snapshot file.
// Sessions restored from disk are intentionally cold: the PI bridge process
// that held live in-memory state was restarted, so the next turn must use
// resume without continue.
func NewPersistentStore(path string) (*Store, error) {
	store := NewStore()
	store.persistPath = path
	if err := store.loadSnapshot(path); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) loadSnapshot(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load session snapshot %q: %w", path, err)
	}
	if len(data) == 0 {
		return nil
	}

	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("parse session snapshot %q: %w", path, err)
	}

	for _, item := range snap.Sessions {
		if item.SessionFile == "" {
			continue
		}
		key := SessionKey{ChatID: item.ChatID, ThreadID: item.ThreadID, UserID: item.UserID}
		s.sessions[key] = &entry{
			sessionFile:         item.SessionFile,
			active:              false,
			lastSeen:            item.LastSeen,
			lastFailure:         item.LastFailure,
			lastFailureAt:       item.LastFailureAt,
			suspectCount:        item.SuspectCount,
			emptyResults:        item.EmptyResults,
			processDeaths:       item.ProcessDeaths,
			lastLifecycleAction: item.LastLifecycleAction,
		}
	}
	for _, item := range snap.Cwds {
		if item.Cwd == "" {
			continue
		}
		key := ConversationKey{ChatID: item.ChatID, ThreadID: item.ThreadID}
		s.cwds[key] = item.Cwd
		s.cwdSeen[key] = item.LastSeen
	}
	return nil
}

func (s *Store) persistLocked() {
	if s.persistPath == "" {
		return
	}
	// Capture a generation for this snapshot while holding s.mu (the only writer).
	// The atomic read later under persistMu is race-free.
	gen := s.persistGen.Add(1)
	data, err := s.serializeLocked()
	if err != nil {
		log.Printf("Warning: failed to serialize session snapshot: %v", err)
		return
	}
	// Release main lock during disk I/O to avoid blocking other session operations.
	s.mu.Unlock()
	// Serialise via persistMu so only one write at a time reaches the filesystem.
	// The generation guard prevents an older snapshot from overwriting a newer one.
	s.persistMu.Lock()
	if s.persistGen.Load() != gen {
		// A newer persistLocked already committed; skip this stale write.
		s.persistMu.Unlock()
		s.mu.Lock()
		return
	}
	err = s.writeSnapshot(data)
	s.persistMu.Unlock()
	s.mu.Lock()
	if err != nil {
		log.Printf("Warning: failed to persist session snapshot: %v", err)
	}
}

func (s *Store) serializeLocked() ([]byte, error) {
	snap := snapshot{}
	for key, item := range s.sessions {
		if item == nil || item.sessionFile == "" {
			continue
		}
		snap.Sessions = append(snap.Sessions, sessionSnapshot{
			ChatID:              key.ChatID,
			ThreadID:            key.ThreadID,
			UserID:              key.UserID,
			SessionFile:         item.sessionFile,
			LastSeen:            item.lastSeen,
			LastFailure:         item.lastFailure,
			LastFailureAt:       item.lastFailureAt,
			SuspectCount:        item.suspectCount,
			EmptyResults:        item.emptyResults,
			ProcessDeaths:       item.processDeaths,
			LastLifecycleAction: item.lastLifecycleAction,
		})
	}
	for key, cwd := range s.cwds {
		if cwd == "" {
			continue
		}
		snap.Cwds = append(snap.Cwds, cwdSnapshot{
			ChatID:   key.ChatID,
			ThreadID: key.ThreadID,
			Cwd:      cwd,
			LastSeen: s.cwdSeen[key],
		})
	}
	return json.MarshalIndent(snap, "", "  ")
}

func (s *Store) writeSnapshot(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(s.persistPath), 0o700); err != nil {
		return fmt.Errorf("create session snapshot dir: %w", err)
	}
	// Use os.CreateTemp for unique temp files so concurrent
	// persistLocked calls do not race on the same fixed temp path.
	tmpFile, err := os.CreateTemp(filepath.Dir(s.persistPath), "session-*.tmp")
	if err != nil {
		return fmt.Errorf("create session snapshot temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write session snapshot temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close session snapshot temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.persistPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace session snapshot: %w", err)
	}
	return nil
}
