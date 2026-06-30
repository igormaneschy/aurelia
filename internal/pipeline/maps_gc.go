package pipeline

import (
	"context"
	"log/slog"
	"time"

	"github.com/igormaneschy/aurelia/internal/runlog"
)

const (
	defaultMapsGCInterval       = 1 * time.Hour
	defaultMemoryCacheGCMaxAge  = 24 * time.Hour
	orphanRunLogStateMaxAge     = 45 * time.Minute // bridge hard timeout is 30m
)

// MapsGCResult counts entries removed by a maps GC pass.
type MapsGCResult struct {
	PendingPlansRemoved     int
	SummaryCountersRemoved  int
	RunLogStatesRemoved     int
	ActiveToolStatesRemoved int
	MemoryCacheRemoved      int
	TokenGuardRemoved       int
}

// GCMaps reclaims stale in-process map entries. sessionMaxAge should align with
// session.Store GC (typically session_ttl_hours). Safe to call concurrently.
func (s *Service) GCMaps(sessionMaxAge time.Duration) MapsGCResult {
	if s == nil {
		return MapsGCResult{}
	}
	var result MapsGCResult
	result.PendingPlansRemoved = s.gcPendingPlans()
	if s.summaryCounter != nil {
		result.SummaryCountersRemoved = s.summaryCounter.gc(sessionMaxAge)
	}
	result.RunLogStatesRemoved = s.gcOrphanRunLogStates(orphanRunLogStateMaxAge)
	result.ActiveToolStatesRemoved = s.gcStaleActiveToolStates()
	if s.memoryCache != nil {
		result.MemoryCacheRemoved = s.memoryCache.gc(defaultMemoryCacheGCMaxAge)
	}
	if s.tokenGuard != nil {
		result.TokenGuardRemoved = s.tokenGuard.GC(sessionMaxAge)
	}
	if total := result.Total(); total > 0 {
		slog.Info("pipeline: maps GC",
			"pending_plans", result.PendingPlansRemoved,
			"summary_counters", result.SummaryCountersRemoved,
			"runlog_states", result.RunLogStatesRemoved,
			"active_tool_states", result.ActiveToolStatesRemoved,
			"memory_cache", result.MemoryCacheRemoved,
			"token_guard", result.TokenGuardRemoved,
		)
	}
	return result
}

// Total returns the sum of all removed entries.
func (r MapsGCResult) Total() int {
	return r.PendingPlansRemoved + r.SummaryCountersRemoved + r.RunLogStatesRemoved +
		r.ActiveToolStatesRemoved + r.MemoryCacheRemoved + r.TokenGuardRemoved
}

// StartMapsGC runs GCMaps on a ticker until ctx is canceled.
func (s *Service) StartMapsGC(ctx context.Context, interval, sessionMaxAge time.Duration) {
	if s == nil || ctx == nil {
		return
	}
	if interval <= 0 {
		interval = defaultMapsGCInterval
	}
	if sessionMaxAge <= 0 {
		sessionMaxAge = 7 * 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.GCMaps(sessionMaxAge)
		}
	}
}

func (s *Service) gcPendingPlans() int {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	removed := 0
	now := time.Now()
	for key, pp := range s.pendingPlans {
		if pp == nil || now.Sub(pp.createdAt) > pendingPlanExpiry {
			delete(s.pendingPlans, key)
			removed++
		}
	}
	return removed
}

type orphanRunLogTarget struct {
	chatID   int64
	threadID int
	userID   int64
}

func (s *Service) gcOrphanRunLogStates(maxAge time.Duration) int {
	if maxAge <= 0 || s.runLog == nil {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	var targets []orphanRunLogTarget

	s.runLogMu.Lock()
	for key, state := range s.runLogStates {
		if state == nil {
			continue
		}
		started := state.startedAt
		if started.IsZero() || !started.Before(cutoff) {
			continue
		}
		chatID, threadID, userID, ok := parseSessionKey(key)
		if !ok {
			continue
		}
		if s.isSessionActive(chatID, threadID, userID) {
			continue
		}
		targets = append(targets, orphanRunLogTarget{chatID: chatID, threadID: threadID, userID: userID})
	}
	s.runLogMu.Unlock()

	removed := 0
	for _, t := range targets {
		s.completeRunLog(t.chatID, t.threadID, t.userID, runlog.RunTimedOut, "",
			"orphan run state reclaimed by maps GC")
		removed++
	}
	return removed
}

func (s *Service) gcStaleActiveToolStates() int {
	s.toolStateMu.Lock()
	defer s.toolStateMu.Unlock()
	if len(s.activeToolStates) == 0 {
		return 0
	}
	removed := 0
	for key := range s.activeToolStates {
		chatID, threadID, userID, ok := parseSessionKey(key)
		if !ok || !s.isSessionActive(chatID, threadID, userID) {
			delete(s.activeToolStates, key)
			removed++
		}
	}
	return removed
}

func (s *Service) isSessionActive(chatID int64, threadID int, userID int64) bool {
	_, ok := s.activeSessions.Load(sessionKey(chatID, threadID, userID))
	return ok
}