package bridge

import (
	"sync"
)

const (
	// eventOverflowBuffer is the per-request spillover capacity when the fast
	// channel buffer is full. Events beyond channel+overflow are dropped.
	eventOverflowBuffer = 512
	maxActiveRequestStreams = 32
	// maxAggregateStreamBytes bounds total queued stdout-derived bytes. It
	// must stay comfortably above the worst-case terminal reserve
	// (maxActiveRequestStreams × maxEventPayloadBytes) so non-terminal
	// delivery keeps working while every ordinary stream slot is occupied.
	maxAggregateStreamBytes = 16 * 1024 * 1024
)

// aggregateStreamBudget bounds the total queued stdout-derived event bytes
// across all active requests. Each active stream reserves one normalized
// terminal-event allowance; non-terminal delivery cannot consume that reserve.
// Terminal delivery may use the reserve after evicting older non-terminals.
type aggregateStreamBudget struct {
	mu            sync.Mutex
	active        int
	controlActive int
	queuedBytes   int
	reservedBytes int
}

func newAggregateStreamBudget() *aggregateStreamBudget {
	return &aggregateStreamBudget{}
}

func (b *aggregateStreamBudget) acquire() bool {
	return b.acquireKind(false)
}

// acquireControl reserves the dedicated control-stream slot used by cancel
// requests. Cancellation must remain possible even when all ordinary request
// stream slots are occupied.
func (b *aggregateStreamBudget) acquireControl() bool {
	return b.acquireKind(true)
}

func (b *aggregateStreamBudget) acquireKind(control bool) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if control {
		if b.controlActive >= 1 {
			return false
		}
		b.controlActive++
	} else {
		if b.active >= maxActiveRequestStreams {
			return false
		}
		b.active++
	}
	b.reservedBytes += maxEventPayloadBytes
	return true
}

func (b *aggregateStreamBudget) releaseStream() {
	b.releaseStreamKind(false)
}

func (b *aggregateStreamBudget) releaseControlStream() {
	b.releaseStreamKind(true)
}

func (b *aggregateStreamBudget) releaseStreamKind(control bool) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if control {
		if b.controlActive > 0 {
			b.controlActive--
		}
	} else if b.active > 0 {
		b.active--
	}
	if b.reservedBytes >= maxEventPayloadBytes {
		b.reservedBytes -= maxEventPayloadBytes
	} else {
		b.reservedBytes = 0
	}
}

func (b *aggregateStreamBudget) tryReserve(size int, terminal bool) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	limit := maxAggregateStreamBytes
	if !terminal {
		limit -= b.reservedBytes
	}
	if size < 0 || b.queuedBytes+size > limit {
		return false
	}
	b.queuedBytes += size
	return true
}

func (b *aggregateStreamBudget) releaseBytes(size int) {
	if b == nil || size <= 0 {
		return
	}
	b.mu.Lock()
	b.queuedBytes -= size
	if b.queuedBytes < 0 {
		b.queuedBytes = 0
	}
	b.mu.Unlock()
}

// requestStream routes bridge events to a consumer with a fast channel buffer
// plus a bounded in-memory overflow queue for slow consumers.
type requestStream struct {
	ch             chan Event
	overflow       []Event
	queuedBytes    int
	closed         bool
	budget         *aggregateStreamBudget
	controlStream  bool
	budgetReleased bool
	mu             sync.Mutex
}

func newRequestStream(chanBuf int, budgets ...*aggregateStreamBudget) *requestStream {
	var budget *aggregateStreamBudget
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	return newRequestStreamKind(chanBuf, budget, false)
}

func newControlRequestStream(chanBuf int, budget *aggregateStreamBudget) *requestStream {
	return newRequestStreamKind(chanBuf, budget, true)
}

func newRequestStreamKind(chanBuf int, budget *aggregateStreamBudget, control bool) *requestStream {
	return &requestStream{
		ch:            make(chan Event, chanBuf),
		budget:        budget,
		controlStream: control,
	}
}

// deliver attempts to enqueue ev without blocking the readLoop.
// Returns false only when both the channel and overflow queue are full.
func (s *requestStream) deliver(ev Event) bool {
	size := eventMemoryBytes(ev)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || size > maxRequestStreamBytes || s.queuedBytes+size > maxRequestStreamBytes {
		return false
	}
	if !s.budget.tryReserve(size, false) {
		return false
	}
	reserved := true
	defer func() {
		if reserved {
			s.budget.releaseBytes(size)
		}
	}()
	// Once spillover exists, keep appending there until it drains. Sending a
	// newer event into a newly available fast-channel slot would let it pass
	// older overflow entries and reorder the stream for slow consumers.
	if len(s.overflow) > 0 {
		if len(s.overflow) >= eventOverflowBuffer {
			return false
		}
		s.overflow = append(s.overflow, ev)
		s.queuedBytes += size
		reserved = false
		return true
	}
	select {
	case s.ch <- ev:
		s.queuedBytes += size
		reserved = false
		return true
	default:
	}

	if len(s.overflow) >= eventOverflowBuffer {
		return false
	}
	s.overflow = append(s.overflow, ev)
	s.queuedBytes += size
	reserved = false
	return true
}

// deliverTerminal preserves a bounded terminal result/error by evicting
// queued non-terminal events until both a channel slot and byte budget exist.
// It returns the number of non-terminal events evicted and whether delivery
// succeeded. The normalized terminal event is guaranteed to fit the byte cap.
func (s *requestStream) deliverTerminal(ev Event) (int, bool) {
	size := eventMemoryBytes(ev)
	if size > maxRequestStreamBytes {
		return 0, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, false
	}
	dropped := 0
	for {
		reserved := false
		if s.queuedBytes+size <= maxRequestStreamBytes {
			reserved = s.budget.tryReserve(size, true)
		}
		if reserved {
			if len(s.overflow) > 0 {
				if len(s.overflow) < eventOverflowBuffer {
					s.overflow = append(s.overflow, ev)
					s.queuedBytes += size
					return dropped, true
				}
			} else if len(s.ch) < cap(s.ch) {
				s.ch <- ev
				s.queuedBytes += size
				return dropped, true
			}
			// The stream queue is full even though the aggregate budget had
			// room. Release this attempt before evicting an older item and
			// retrying the reservation.
			s.budget.releaseBytes(size)
		}

		// Drop the oldest queued item, preferring overflow when it exists so
		// the terminal event remains after all retained FIFO history.
		if len(s.overflow) > 0 {
			old := s.overflow[0]
			s.overflow = s.overflow[1:]
			s.queuedBytes -= eventMemoryBytes(old)
			s.budget.releaseBytes(eventMemoryBytes(old))
			dropped++
			continue
		}
		select {
		case old := <-s.ch:
			s.queuedBytes -= eventMemoryBytes(old)
			s.budget.releaseBytes(eventMemoryBytes(old))
			dropped++
		default:
			return dropped, false
		}
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
	s.queuedBytes -= eventMemoryBytes(ev)
	s.budget.releaseBytes(eventMemoryBytes(ev))
	return ev, true
}

// consumeFast accounts for a consumer receiving from the fast channel.
func (s *requestStream) consumeFast(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queuedBytes -= eventMemoryBytes(ev)
	s.budget.releaseBytes(eventMemoryBytes(ev))
	if s.queuedBytes < 0 {
		s.queuedBytes = 0
	}
}

// discardQueued releases aggregate-budget bytes for events that a consumer
// abandoned after the stream was closed or its context expired. A consumer
// that already received an event owns that event's accounting and will still
// release it through consumeFast; only items still present in the queues are
// removed here.
func (s *requestStream) discardQueued() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		select {
		case ev, open := <-s.ch:
			if !open {
				for _, ev := range s.overflow {
					s.queuedBytes -= eventMemoryBytes(ev)
					s.budget.releaseBytes(eventMemoryBytes(ev))
				}
				s.overflow = nil
				if s.queuedBytes < 0 {
					s.queuedBytes = 0
				}
				return
			}
			s.queuedBytes -= eventMemoryBytes(ev)
			s.budget.releaseBytes(eventMemoryBytes(ev))
		default:
			for _, ev := range s.overflow {
				s.queuedBytes -= eventMemoryBytes(ev)
				s.budget.releaseBytes(eventMemoryBytes(ev))
			}
			s.overflow = nil
			if s.queuedBytes < 0 {
				s.queuedBytes = 0
			}
			return
		}
	}
}

func (s *requestStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if !s.budgetReleased {
		s.budgetReleased = true
		if s.controlStream {
			s.budget.releaseControlStream()
		} else {
			s.budget.releaseStream()
		}
	}
	close(s.ch)
}
