package bridge

import (
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

func TestRequestStream_EvictOneForTerminalPreservesOrder(t *testing.T) {
	s := newRequestStream(1)
	_ = s.deliver(Event{Type: "assistant", Text: "keep"})
	_ = s.deliver(Event{Type: "assistant", Text: "overflow"})

	if _, ok := s.evictOneForTerminal(); !ok {
		t.Fatal("expected eviction from full channel")
	}
	ev, ok := s.dequeueOverflow()
	if !ok || ev.Text != "overflow" {
		t.Fatalf("first overflow = %+v, want overflow", ev)
	}
	ev, ok = s.dequeueOverflow()
	if !ok || ev.Text != "keep" {
		t.Fatalf("evicted channel head = %+v, want keep", ev)
	}
}