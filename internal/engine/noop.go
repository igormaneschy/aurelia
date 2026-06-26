package engine

import (
	"context"
	"sync"
)

// NoopEngine implements Engine with empty responses. Useful as a default in tests.
type NoopEngine struct{}

func (NoopEngine) Query(_ context.Context, _ Request) (<-chan Event, error) {
	ch := make(chan Event)
	close(ch)
	return ch, nil
}

func (NoopEngine) Command(_ context.Context, _ Command) (Event, error) {
	return Event{Type: EventTypeDone, RawType: "result"}, nil
}

func (NoopEngine) Stats(_ context.Context, _ string, _ StatsOptions) (Stats, error) {
	return Stats{}, nil
}

// RecordedCall captures a single Engine invocation for test assertions.
type RecordedCall struct {
	Method  string
	Request *Request
	Command *Command
}

// MockEngine is a configurable test double for Engine.
type MockEngine struct {
	mu sync.Mutex

	QueryResponses  []Event
	QueryErr        error
	CommandResponse Event
	CommandErr      error
	StatsResponse   Stats
	StatsErr        error

	Calls []RecordedCall
}

func (m *MockEngine) Query(_ context.Context, req Request) (<-chan Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := req
	m.Calls = append(m.Calls, RecordedCall{Method: "Query", Request: &r})
	if m.QueryErr != nil {
		return nil, m.QueryErr
	}
	ch := make(chan Event, len(m.QueryResponses))
	for _, ev := range m.QueryResponses {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (m *MockEngine) Command(_ context.Context, cmd Command) (Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := cmd
	m.Calls = append(m.Calls, RecordedCall{Method: "Command", Command: &c})
	if m.CommandErr != nil {
		return Event{}, m.CommandErr
	}
	return m.CommandResponse, nil
}

func (m *MockEngine) Stats(_ context.Context, _ string, _ StatsOptions) (Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, RecordedCall{Method: "Stats"})
	if m.StatsErr != nil {
		return Stats{}, m.StatsErr
	}
	return m.StatsResponse, nil
}