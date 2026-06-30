package bridge

import (
	"sync"
)

const (
	// eventOverflowBuffer is the per-request spillover capacity when the fast
	// channel buffer is full. Events beyond channel+overflow are dropped.
	eventOverflowBuffer = 512
)

// requestStream routes bridge events to a consumer with a fast channel buffer
// plus a bounded in-memory overflow queue for slow consumers.
type requestStream struct {
	ch       chan Event
	overflow []Event
	mu       sync.Mutex
}

func newRequestStream(chanBuf int) *requestStream {
	return &requestStream{
		ch: make(chan Event, chanBuf),
	}
}

// deliver attempts to enqueue ev without blocking the readLoop.
// Returns false only when both the channel and overflow queue are full.
func (s *requestStream) deliver(ev Event) bool {
	select {
	case s.ch <- ev:
		return true
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.overflow) >= eventOverflowBuffer {
		return false
	}
	s.overflow = append(s.overflow, ev)
	return true
}

// evictOneForTerminal frees one slot in the fast channel, preserving ordering
// by moving the evicted event into overflow when space allows. When overflow
// is full, returns the evicted event for explicit drop accounting.
func (s *requestStream) evictOneForTerminal() (dropped Event, didEvict bool) {
	select {
	case evicted := <-s.ch:
		didEvict = true
		s.mu.Lock()
		if len(s.overflow) < eventOverflowBuffer {
			s.overflow = append(s.overflow, evicted)
			s.mu.Unlock()
			return Event{}, true
		}
		s.mu.Unlock()
		return evicted, true
	default:
		return Event{}, false
	}
}

func (s *requestStream) dequeueOverflow() (Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.overflow) == 0 {
		return Event{}, false
	}
	ev := s.overflow[0]
	s.overflow = s.overflow[1:]
	return ev, true
}

func (s *requestStream) close() {
	safeClose(s.ch)
}