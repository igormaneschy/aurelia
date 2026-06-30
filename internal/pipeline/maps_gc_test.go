package pipeline

import (
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/session"
)

func TestGCPendingPlans_RemovesExpired(t *testing.T) {
	s := &Service{
		pendingPlans: map[string]*pendingPlan{
			sessionKey(1, 0, 100): {
				plan:      &orchestrator.Plan{},
				createdAt: time.Now().Add(-(pendingPlanExpiry + time.Minute)),
			},
			sessionKey(2, 0, 100): {
				plan:      &orchestrator.Plan{},
				createdAt: time.Now(),
			},
		},
	}

	removed := s.gcPendingPlans()
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if len(s.pendingPlans) != 1 {
		t.Fatalf("pendingPlans len = %d, want 1", len(s.pendingPlans))
	}
	if _, ok := s.pendingPlans[sessionKey(2, 0, 100)]; !ok {
		t.Fatal("expected recent pending plan to remain")
	}
}

func TestSummaryCounterGC_RemovesStale(t *testing.T) {
	key := continuity.ConversationKeyFor(1, 0, 100)
	sc := &summaryCounter{
		counts: map[continuity.ConversationKey]int{key: 3},
		lastSeen: map[continuity.ConversationKey]time.Time{
			key: time.Now().Add(-2 * time.Hour),
		},
	}
	removed := sc.gc(1 * time.Hour)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if len(sc.counts) != 0 {
		t.Fatalf("counts len = %d, want 0", len(sc.counts))
	}
}

func TestSummaryCounterGC_KeepsRecent(t *testing.T) {
	key := continuity.ConversationKeyFor(1, 0, 100)
	sc := &summaryCounter{
		counts: map[continuity.ConversationKey]int{key: 2},
		lastSeen: map[continuity.ConversationKey]time.Time{
			key: time.Now(),
		},
	}
	removed := sc.gc(1 * time.Hour)
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
}

func TestGCOrphanRunLogStates_ReclaimsStaleInactive(t *testing.T) {
	spy := &spyRunLogStore{}
	runID := "run-orphan"
	key := runLogKey(9, 0, 100)
	s := &Service{
		runLog:       spy,
		runLogStates: map[string]*runLogState{key: {runID: runID, startedAt: time.Now().Add(-2 * time.Hour)}},
		runLogMu:     sync.Mutex{},
	}

	removed := s.gcOrphanRunLogStates(45 * time.Minute)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := s.runLogStates[key]; ok {
		t.Fatal("expected orphan runLogState to be removed")
	}
}

func TestGCOrphanRunLogStates_SkipsActiveSession(t *testing.T) {
	key := runLogKey(9, 0, 100)
	s := &Service{
		runLog:       &spyRunLogStore{},
		runLogStates: map[string]*runLogState{key: {runID: "run-active", startedAt: time.Now().Add(-2 * time.Hour)}},
		runLogMu:     sync.Mutex{},
	}
	s.activeSessions.Store(sessionKey(9, 0, 100), &activeRun{})

	removed := s.gcOrphanRunLogStates(45 * time.Minute)
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 while session active", removed)
	}
	if _, ok := s.runLogStates[key]; !ok {
		t.Fatal("expected active runLogState to remain")
	}
}

func TestGCStaleActiveToolStates_RemovesInactive(t *testing.T) {
	s := &Service{
		activeToolStates: map[string]activeToolState{
			sessionKey(1, 0, 100): {},
			sessionKey(2, 0, 100): {},
		},
	}
	s.activeSessions.Store(sessionKey(2, 0, 100), &activeRun{})

	removed := s.gcStaleActiveToolStates()
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if len(s.activeToolStates) != 1 {
		t.Fatalf("activeToolStates len = %d, want 1", len(s.activeToolStates))
	}
}

func TestGCMaps_AggregatesCounts(t *testing.T) {
	key := continuity.ConversationKeyFor(3, 0, 100)
	s := &Service{
		pendingPlans: map[string]*pendingPlan{
			sessionKey(1, 0, 100): {createdAt: time.Now().Add(-(pendingPlanExpiry + time.Minute))},
		},
		summaryCounter: &summaryCounter{
			counts: map[continuity.ConversationKey]int{key: 1},
			lastSeen: map[continuity.ConversationKey]time.Time{
				key: time.Now().Add(-48 * time.Hour),
			},
		},
		memoryCache: NewMemoryCache(),
		tokenGuard:  session.NewTokenGuard(),
	}
	s.memoryCache.put("/tmp/stale", "content", nil)
	s.memoryCache.mu.Lock()
	s.memoryCache.entries["/tmp/stale"] = memoryCacheEntry{
		content:       "content",
		lastValidated: time.Now().Add(-48 * time.Hour),
	}
	s.memoryCache.mu.Unlock()

	result := s.GCMaps(24 * time.Hour)
	if result.PendingPlansRemoved != 1 {
		t.Fatalf("PendingPlansRemoved = %d, want 1", result.PendingPlansRemoved)
	}
	if result.SummaryCountersRemoved != 1 {
		t.Fatalf("SummaryCountersRemoved = %d, want 1", result.SummaryCountersRemoved)
	}
	if result.MemoryCacheRemoved != 1 {
		t.Fatalf("MemoryCacheRemoved = %d, want 1", result.MemoryCacheRemoved)
	}
	if result.Total() < 3 {
		t.Fatalf("Total = %d, want at least 3", result.Total())
	}
}

func TestMemoryCacheGC_RemovesStaleEntries(t *testing.T) {
	cache := NewMemoryCache()
	now := time.Now()
	cache.mu.Lock()
	cache.entries["/tmp/stale"] = memoryCacheEntry{
		content:       "stale",
		lastValidated: now.Add(-48 * time.Hour),
	}
	cache.entries["/tmp/fresh"] = memoryCacheEntry{
		content:       "fresh",
		lastValidated: now,
	}
	cache.mu.Unlock()

	removed := cache.gc(24 * time.Hour)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := cache.entries["/tmp/stale"]; ok {
		t.Fatal("expected stale entry removed")
	}
	if _, ok := cache.entries["/tmp/fresh"]; !ok {
		t.Fatal("expected fresh entry to remain")
	}
}

func TestGCOrphanRunLogStates_NoRunLog(t *testing.T) {
	s := &Service{
		runLogStates: map[string]*runLogState{
			runLogKey(1, 0, 100): {runID: "x", startedAt: time.Now().Add(-2 * time.Hour)},
		},
		runLogMu: sync.Mutex{},
	}
	if removed := s.gcOrphanRunLogStates(45 * time.Minute); removed != 0 {
		t.Fatalf("removed = %d, want 0 without runlog store", removed)
	}
}

func TestGCOrphanRunLogStates_CompletesWithTimedOut(t *testing.T) {
	spy := &spyRunLogStore{}
	s := &Service{
		runLog: spy,
		runLogStates: map[string]*runLogState{
			runLogKey(5, 0, 100): {runID: "run-timeout", startedAt: time.Now().Add(-time.Hour)},
		},
		runLogMu: sync.Mutex{},
	}
	s.gcOrphanRunLogStates(45 * time.Minute)
	// completeRunLog is best-effort; ensure state map entry is gone.
	if _, ok := s.runLogStates[runLogKey(5, 0, 100)]; ok {
		t.Fatal("expected state removed after orphan reclaim")
	}
}