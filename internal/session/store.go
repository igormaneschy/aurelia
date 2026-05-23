package session

import (
	"fmt"
	"sync"
	"time"
)

// SessionKey uniquely identifies a session within a chat thread and user.
// For private chats and non-forum groups, ThreadID is 0.
// During transition, UserID is 0 for all existing call sites.
type SessionKey struct {
	ChatID   int64
	ThreadID int // 0 for non-forum chats
	UserID   int64
}

func (k SessionKey) String() string {
	return fmt.Sprintf("%d:%d:%d", k.ChatID, k.ThreadID, k.UserID)
}

// ConversationKey identifies a conversation (chat + thread) without user scope.
type ConversationKey struct {
	ChatID   int64
	ThreadID int
}

func (k ConversationKey) String() string {
	return fmt.Sprintf("%d:%d", k.ChatID, k.ThreadID)
}

// Store manages session IDs and working directories per chat thread.
type Store struct {
	mu          sync.RWMutex
	sessions    map[SessionKey]*entry
	cwds        map[ConversationKey]string
	cwdSeen     map[ConversationKey]time.Time
	persistPath string
}

type entry struct {
	sessionFile string
	active      bool
	lastSeen    time.Time
}

// Info is a read-only view of a stored PI session.
type Info struct {
	ChatID      int64
	ThreadID    int
	UserID      int64
	SessionFile string
	Active      bool
	LastSeen    time.Time
}

// NewStore creates a new session store.
func NewStore() *Store {
	return &Store{
		sessions: make(map[SessionKey]*entry),
		cwds:     make(map[ConversationKey]string),
		cwdSeen:  make(map[ConversationKey]time.Time),
	}
}

// SessionKeyFor returns a SessionKey from chatID, threadID, and userID.
// During transition, pass 0 for userID.
func SessionKeyFor(chatID int64, threadID int, userID int64) SessionKey {
	return SessionKey{ChatID: chatID, ThreadID: threadID, UserID: userID}
}

func (s *Store) Get(chatID int64, threadID int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, 0)
	e := s.sessions[key]
	if e == nil {
		return ""
	}
	e.lastSeen = time.Now()
	return e.sessionFile
}

func (s *Store) GetWithState(chatID int64, threadID int) (sessionID string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, 0)
	e := s.sessions[key]
	if e == nil {
		return "", false
	}
	e.lastSeen = time.Now()
	return e.sessionFile, e.active
}

func (s *Store) Set(chatID int64, threadID int, sessionFile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, 0)
	s.sessions[key] = &entry{sessionFile: sessionFile, active: true, lastSeen: time.Now()}
	s.persistLocked()
}

// Clear removes session and cwd for a specific chat thread.
func (s *Store) Clear(chatID int64, threadID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk := SessionKeyFor(chatID, threadID, 0)
	ck := ConversationKey{ChatID: chatID, ThreadID: threadID}
	delete(s.sessions, sk)
	delete(s.cwds, ck)
	delete(s.cwdSeen, ck)
	s.persistLocked()
}

// ClearSession removes only the session ID for a specific chat thread.
func (s *Store) ClearSession(chatID int64, threadID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, 0)
	delete(s.sessions, key)
	s.persistLocked()
}

// ClearAll removes all sessions and cwds for a chat (all threads, all users).
// During transition (UserID=0) this matches existing behavior. When user isolation
// is active, this clears ALL users' sessions for the given chat.
func (s *Store) ClearAll(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.sessions {
		if key.ChatID == chatID {
			delete(s.sessions, key)
		}
	}
	for key := range s.cwds {
		if key.ChatID == chatID {
			delete(s.cwds, key)
			delete(s.cwdSeen, key)
		}
	}
	s.persistLocked()
}

// DeactivateAll marks all sessions as inactive (cold). Used when the bridge
// process dies — sessions keep their IDs for resume, but Continue must not be
// used since the process that held them is gone.
func (s *Store) DeactivateAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.sessions {
		e.active = false
	}
	s.persistLocked()
}

// Deactivate marks a single session as inactive (cold). Used when a run times
// out — the session ID is kept for cold resume, but Continue must not be used
// since the session state may be inconsistent.
func (s *Store) Deactivate(chatID int64, threadID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKeyFor(chatID, threadID, 0)
	if e, ok := s.sessions[key]; ok {
		e.active = false
		s.persistLocked()
	}
}

// GC removes sessions and cwds that have not been seen since maxAge ago.
func (s *Store) GC(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for key, e := range s.sessions {
		if e.lastSeen.Before(cutoff) {
			ck := ConversationKey{ChatID: key.ChatID, ThreadID: key.ThreadID}
			delete(s.sessions, key)
			delete(s.cwds, ck)
			delete(s.cwdSeen, ck)
		}
	}
	for key, seen := range s.cwdSeen {
		if seen.Before(cutoff) {
			delete(s.cwds, key)
			delete(s.cwdSeen, key)
		}
	}
	for key := range s.cwds {
		if _, ok := s.cwdSeen[key]; !ok {
			delete(s.cwds, key)
		}
	}
	s.persistLocked()
}

func (s *Store) GetCwd(chatID int64, threadID int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Try thread-specific cwd first
	key := ConversationKey{ChatID: chatID, ThreadID: threadID}
	if cwd, ok := s.cwds[key]; ok {
		s.cwdSeen[key] = time.Now()
		return cwd
	}
	// Fall back to general topic (thread=0) cwd
	if threadID != 0 {
		generalKey := ConversationKey{ChatID: chatID, ThreadID: 0}
		if cwd, ok := s.cwds[generalKey]; ok {
			s.cwdSeen[generalKey] = time.Now()
			return cwd
		}
	}
	return ""
}

func (s *Store) SetCwd(chatID int64, threadID int, cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ConversationKey{ChatID: chatID, ThreadID: threadID}
	s.cwds[key] = cwd
	s.cwdSeen[key] = time.Now()
	s.persistLocked()
}

func (s *Store) ClearCwd(chatID int64, threadID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ConversationKey{ChatID: chatID, ThreadID: threadID}
	delete(s.cwds, key)
	delete(s.cwdSeen, key)
	s.persistLocked()
}

// GetSession returns the session ID for a specific chat, thread, and user.
func (s *Store) GetSession(chatID int64, threadID int, userID int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKey{ChatID: chatID, ThreadID: threadID, UserID: userID}
	e := s.sessions[key]
	if e == nil {
		return ""
	}
	e.lastSeen = time.Now()
	return e.sessionFile
}

// GetSessionWithState returns the session ID and active state for a specific chat, thread, and user.
func (s *Store) GetSessionWithState(chatID int64, threadID int, userID int64) (sessionID string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKey{ChatID: chatID, ThreadID: threadID, UserID: userID}
	e := s.sessions[key]
	if e == nil {
		return "", false
	}
	e.lastSeen = time.Now()
	return e.sessionFile, e.active
}

// RecentColdSessions returns inactive sessions seen within maxAge.
func (s *Store) RecentColdSessions(maxAge time.Duration) []Info {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cutoff := time.Now().Add(-maxAge)
	infos := make([]Info, 0)
	for key, e := range s.sessions {
		if e == nil || e.active || e.sessionFile == "" || e.lastSeen.Before(cutoff) {
			continue
		}
		infos = append(infos, Info{
			ChatID:      key.ChatID,
			ThreadID:    key.ThreadID,
			UserID:      key.UserID,
			SessionFile: e.sessionFile,
			Active:      e.active,
			LastSeen:    e.lastSeen,
		})
	}
	return infos
}

// SetSession creates or updates a session for a specific chat, thread, and user.
func (s *Store) SetSession(chatID int64, threadID int, userID int64, sessionFile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKey{ChatID: chatID, ThreadID: threadID, UserID: userID}
	s.sessions[key] = &entry{sessionFile: sessionFile, active: true, lastSeen: time.Now()}
	s.persistLocked()
}

// ClearSessionForUser removes the session for a specific chat, thread, and user.
func (s *Store) ClearSessionForUser(chatID int64, threadID int, userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKey{ChatID: chatID, ThreadID: threadID, UserID: userID}
	delete(s.sessions, key)
	s.persistLocked()
}

// DeactivateSession marks a session as inactive for a specific chat, thread, and user.
func (s *Store) DeactivateSession(chatID int64, threadID int, userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SessionKey{ChatID: chatID, ThreadID: threadID, UserID: userID}
	if e, ok := s.sessions[key]; ok {
		e.active = false
		s.persistLocked()
	}
}
