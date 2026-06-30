package bridge

import (
	"context"
	"sync"
	"time"
)

// Priority classifies bridge workload for dispatch ordering.
// Interactive (Telegram/TUI/lifecycle) runs ahead of cron and background work.
type Priority int

const (
	PriorityInteractive Priority = iota
	PriorityCron
	PriorityBackground
)

const priorityPollInterval = 25 * time.Millisecond

// requestSlotTracker gates stdin writes so lower-priority work waits while
// higher-priority queries are in flight.
type requestSlotTracker struct {
	mu sync.Mutex

	activeInteractive int
	activeCron        int
	activeBackground  int
}

func newRequestSlotTracker() *requestSlotTracker {
	return &requestSlotTracker{}
}

func commandBypassesPriorityQueue(cmd string) bool {
	switch cmd {
	case "ping", "cancel", "abort", "get-env", "list-models":
		return true
	default:
		return false
	}
}

// effectivePriority returns the dispatch priority for a request. Callers may
// set Request.Priority explicitly; otherwise nudge/dream agent names map to
// background and everything else defaults to interactive.
func effectivePriority(req Request) Priority {
	if req.Options.Security != nil {
		switch req.Options.Security.AgentName {
		case "nudge", "dream":
			return PriorityBackground
		}
	}
	switch req.Priority {
	case PriorityCron, PriorityBackground:
		return req.Priority
	default:
		return PriorityInteractive
	}
}

func (t *requestSlotTracker) canAcquire(p Priority) bool {
	switch p {
	case PriorityInteractive:
		return true
	case PriorityCron:
		return t.activeInteractive == 0
	case PriorityBackground:
		return t.activeInteractive == 0 && t.activeCron == 0
	default:
		return true
	}
}

func (t *requestSlotTracker) incActive(p Priority) {
	switch p {
	case PriorityCron:
		t.activeCron++
	case PriorityBackground:
		t.activeBackground++
	default:
		t.activeInteractive++
	}
}

func (t *requestSlotTracker) decActive(p Priority) {
	switch p {
	case PriorityCron:
		if t.activeCron > 0 {
			t.activeCron--
		}
	case PriorityBackground:
		if t.activeBackground > 0 {
			t.activeBackground--
		}
	default:
		if t.activeInteractive > 0 {
			t.activeInteractive--
		}
	}
}

func (t *requestSlotTracker) acquire(ctx context.Context, p Priority) error {
	for {
		t.mu.Lock()
		if t.canAcquire(p) {
			t.incActive(p)
			t.mu.Unlock()
			return nil
		}
		t.mu.Unlock()

		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(priorityPollInterval):
		}
	}
}

func (t *requestSlotTracker) release(p Priority) {
	t.mu.Lock()
	t.decActive(p)
	t.mu.Unlock()
}