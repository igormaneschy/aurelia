package engine

import (
	"context"
	"testing"
)

func TestMockEngine_QueryAndCommand(t *testing.T) {
	m := &MockEngine{
		QueryResponses: []Event{{Type: EventTypeText, RawType: "assistant", Text: "ok"}},
		CommandResponse: Event{Type: EventTypeDone, RawType: "result"},
	}
	ch, err := m.Query(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	ev := <-ch
	if ev.Text != "ok" {
		t.Fatalf("text = %q", ev.Text)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed")
	}
	resp, err := m.Command(context.Background(), Command{Name: "abort"})
	if err != nil || resp.Type != EventTypeDone {
		t.Fatalf("command = %+v err=%v", resp, err)
	}
	if len(m.Calls) != 2 {
		t.Fatalf("calls = %d", len(m.Calls))
	}
}