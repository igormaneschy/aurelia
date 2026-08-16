package bridge

import (
	"strings"
	"testing"
)

func TestRequestStream_DeliverUsesOverflowBeforeDrop(t *testing.T) {
	s := newRequestStream(2)

	if !s.deliver(Event{Type: "assistant", Text: "a"}) {
		t.Fatal("first deliver failed")
	}
	if !s.deliver(Event{Type: "assistant", Text: "b"}) {
		t.Fatal("second deliver failed")
	}
	if !s.deliver(Event{Type: "assistant", Text: "c"}) {
		t.Fatal("third deliver should spill to overflow")
	}

	ev, ok := s.dequeueOverflow()
	if !ok || ev.Text != "c" {
		t.Fatalf("overflow = %+v, ok=%v, want text c", ev, ok)
	}
}

func TestRequestStream_DeliverDropsWhenOverflowFull(t *testing.T) {
	s := newRequestStream(1)
	if !s.deliver(Event{Type: "assistant", Text: "ch"}) {
		t.Fatal("channel deliver failed")
	}
	for i := 0; i < eventOverflowBuffer; i++ {
		if !s.deliver(Event{Type: "assistant", Text: "ov"}) {
			t.Fatalf("overflow deliver %d failed unexpectedly", i)
		}
	}
	if s.deliver(Event{Type: "assistant", Text: "drop"}) {
		t.Fatal("expected drop when channel and overflow are full")
	}
}

func TestAggregateStreamBudget_ReservesTerminalCapacity(t *testing.T) {
	budget := newAggregateStreamBudget()
	if !budget.acquire() {
		t.Fatal("first stream should acquire aggregate budget")
	}
	s := newRequestStream(eventChannelBuffer, budget)

	// Fill the stream with bounded-but-large non-terminal events until the
	// per-stream budget rejects one. The terminal must still evict history and
	// fit through the reserved path.
	large := Event{Type: "assistant", Text: strings.Repeat("x", 100)}
	for s.deliver(large) {
	}
	dropped, ok := s.deliverTerminal(Event{Type: "result", Content: "done"})
	if !ok {
		t.Fatal("terminal event was not preserved after non-terminal budget exhaustion")
	}
	if dropped == 0 {
		t.Fatal("terminal delivery should evict at least one queued non-terminal")
	}

	seenTerminal := false
drainLoop:
	for {
		select {
		case ev, open := <-s.ch:
			if !open {
				break
			}
			if ev.Type == "result" {
				seenTerminal = true
			}
		default:
			if ev, ok := s.dequeueOverflow(); ok {
				if ev.Type == "result" {
					seenTerminal = true
				}
				continue
			}
			break drainLoop
		}
	}

	if !seenTerminal {
		t.Fatal("terminal result was lost from the bounded stream")
	}
	s.close()
}

func TestRequestStream_DiscardQueuedReleasesAggregateBytes(t *testing.T) {
	budget := newAggregateStreamBudget()
	if !budget.acquire() {
		t.Fatal("stream should acquire aggregate budget")
	}
	s := newRequestStream(1, budget)
	if !s.deliver(Event{Type: "assistant", Text: "queued"}) {
		t.Fatal("event should be queued")
	}
	if budget.queuedBytes == 0 {
		t.Fatal("expected queued bytes before discard")
	}

	s.close()
	s.discardQueued()
	if budget.queuedBytes != 0 {
		t.Fatalf("queued bytes = %d, want 0 after discard", budget.queuedBytes)
	}
}

func TestAggregateStreamBudget_BoundsActiveStreams(t *testing.T) {
	budget := newAggregateStreamBudget()
	for i := 0; i < maxActiveRequestStreams; i++ {
		if !budget.acquire() {
			t.Fatalf("stream %d failed before active-stream cap", i)
		}
	}
	if budget.acquire() {
		t.Fatal("active stream cap was not enforced")
	}
	for i := 0; i < maxActiveRequestStreams; i++ {
		budget.releaseStream()
	}
}

func TestAggregateStreamBudget_ControlSlotSurvivesOrdinaryCap(t *testing.T) {
	budget := newAggregateStreamBudget()
	for i := 0; i < maxActiveRequestStreams; i++ {
		if !budget.acquire() {
			t.Fatalf("ordinary stream %d failed before active-stream cap", i)
		}
	}
	if !budget.acquireControl() {
		t.Fatal("control stream should remain available at ordinary stream cap")
	}
	if budget.acquireControl() {
		t.Fatal("second control stream should be rejected")
	}
	budget.releaseControlStream()
	for i := 0; i < maxActiveRequestStreams; i++ {
		budget.releaseStream()
	}
}
