package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

func newQueryTestService(queryFn func(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error)) *Service {
	s := &Service{
		bridge:            &bridge.Bridge{},
		queryMaxRetries:   defaultQueryMaxRetries,
		queryRetryBackoff: 10 * time.Millisecond,
		fallbackProvider:  defaultFallbackProvider,
		fallbackModel:     defaultFallbackModel,
	}
	s.testBridgeQuery = queryFn
	return s
}

func newFakeQueryEngine() *fakeQueryEngine {
	return &fakeQueryEngine{responses: make(map[string][]bridge.Event)}
}

type fakeQueryEngine struct {
	calls     []bridge.Request
	responses map[string][]bridge.Event
	defaultEv bridge.Event
	queryFn   func(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error)
}

func (f *fakeQueryEngine) addResponse(prompt string, events []bridge.Event) {
	f.responses[prompt] = events
}

func (f *fakeQueryEngine) defaultQuery(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error) {
	f.calls = append(f.calls, req)
	ch := make(chan bridge.Event, 16)
	evts, ok := f.responses[req.Prompt]
	if !ok {
		ch <- f.defaultEv
		close(ch)
		return ch, nil
	}
	for _, ev := range evts {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (f *fakeQueryEngine) Query(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, req)
	}
	return f.defaultQuery(ctx, req)
}

func (f *fakeQueryEngine) testService() *Service {
	return newQueryTestService(f.Query)
}

func resultEvent(content string) bridge.Event {
	return bridge.Event{Type: "result", Content: content}
}

func errorEvent(message string) bridge.Event {
	return bridge.Event{Type: "error", Message: message}
}

func TestExecuteQuery_Success(t *testing.T) {
	fb := newFakeQueryEngine()
	fb.addResponse("hello", []bridge.Event{resultEvent("world")})

	s := fb.testService()
	req := bridge.Request{Command: "query", Prompt: "hello", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	var notify string
	ch, usedFallback, err := s.executeQuery(context.Background(), req, func(msg string) { notify = msg })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usedFallback {
		t.Error("should not use fallback on success")
	}
	if notify != "" {
		t.Error("should not notify on success")
	}
	if ch == nil {
		t.Fatal("expected event channel")
	}
	for range ch {
	}
}

func TestExecuteQuery_TransientRetryThenSuccess(t *testing.T) {
	fb := newFakeQueryEngine()
	fb.defaultEv = errorEvent("rate limit exceeded")

	s := newQueryTestService(nil)
	s.queryMaxRetries = 3
	s.queryRetryBackoff = 50 * time.Millisecond

	req := bridge.Request{Command: "query", Prompt: "retry-test", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	attempt := 0
	original := fb.defaultQuery
	s.testBridgeQuery = func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		attempt++
		if attempt >= 3 {
			ch := make(chan bridge.Event, 2)
			ch <- resultEvent("success after retry")
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	_, _, err := s.executeQuery(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if attempt < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", attempt)
	}
}

func TestExecuteQuery_FallbackWithoutOpenRouterKey(t *testing.T) {
	fb := newFakeQueryEngine()
	fb.defaultEv = errorEvent("rate limit exceeded")

	s := fb.testService()
	s.openRouterAPIKey = ""

	req := bridge.Request{Command: "query", Prompt: "fail", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	var notifyMsg string
	_, _, err := s.executeQuery(context.Background(), req, func(msg string) { notifyMsg = msg })

	if err == nil {
		t.Fatal("expected error when fallback is unavailable")
	}
	if !strings.Contains(notifyMsg, "OpenRouter") {
		t.Error("should notify about missing OpenRouter config")
	}
}

func TestExecuteQuery_NonRetryableError(t *testing.T) {
	fb := newFakeQueryEngine()
	fb.addResponse("auth-fail", []bridge.Event{errorEvent("Invalid API key")})

	s := fb.testService()
	req := bridge.Request{Command: "query", Prompt: "auth-fail", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	var notifyMsg string
	_, usedFallback, err := s.executeQuery(context.Background(), req, func(msg string) { notifyMsg = msg })

	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	if !strings.Contains(notifyMsg, "autenticação") {
		t.Error("should notify about auth error")
	}
	if usedFallback {
		t.Error("should NOT fallback on auth error")
	}
}

func TestExecuteQuery_FallbackSuccess(t *testing.T) {
	fb := newFakeQueryEngine()
	fb.addResponse("primary-fail", []bridge.Event{errorEvent("rate limit exceeded")})

	s := fb.testService()
	s.openRouterAPIKey = "sk-test"

	req := bridge.Request{Command: "query", Prompt: "primary-fail", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	original := fb.defaultQuery
	s.testBridgeQuery = func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		if r.Options.Provider == "openrouter" {
			ch := make(chan bridge.Event, 2)
			ch <- resultEvent("fallback result")
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	_, usedFallback, err := s.executeQuery(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("expected fallback success: %v", err)
	}
	if !usedFallback {
		t.Error("should mark fallback as used")
	}
}

func TestExecuteQuery_AllRetriesFail(t *testing.T) {
	fb := newFakeQueryEngine()
	fb.addResponse("always-fail", []bridge.Event{errorEvent("rate limit exceeded")})

	s := fb.testService()
	s.openRouterAPIKey = "sk-test"

	req := bridge.Request{Command: "query", Prompt: "always-fail", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	s.testBridgeQuery = func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		ch := make(chan bridge.Event, 2)
		ch <- errorEvent("fallback also down")
		close(ch)
		return ch, nil
	}

	_, _, err := s.executeQuery(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected error when all fail")
	}
	if !strings.Contains(err.Error(), "fallback failed") {
		t.Errorf("expected fallback to be attempted and fail, got: %v", err)
	}
}

func TestExecuteQuery_CancelDuringRetry(t *testing.T) {
	fb := newFakeQueryEngine()
	fb.addResponse("slow", []bridge.Event{errorEvent("rate limit exceeded")})

	s := newQueryTestService(fb.Query)
	s.queryMaxRetries = 3
	s.queryRetryBackoff = 10 * time.Second

	req := bridge.Request{Command: "query", Prompt: "slow", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, _, err := s.executeQuery(ctx, req, nil)
	if err != context.Canceled {
		t.Fatalf("expected context canceled, got: %v", err)
	}
}

func TestExecuteQuery_ProcessDeathOutcome(t *testing.T) {
	s := newQueryTestService(func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		ch := make(chan bridge.Event)
		close(ch)
		return ch, nil
	})

	req := bridge.Request{Command: "query", Prompt: "death", Options: bridge.RequestOptions{Provider: "kimi", Model: "kimi-k2"}}

	_, _, err := s.executeQuery(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected error for process death")
	}
}

func TestExecuteQuery_FallbackResetsSession(t *testing.T) {
	fb := newFakeQueryEngine()
	fb.addResponse("session-test", []bridge.Event{errorEvent("rate limit exceeded")})

	s := fb.testService()
	s.openRouterAPIKey = "sk-test"

	req := bridge.Request{
		Command: "query",
		Prompt:  "session-test",
		Options: bridge.RequestOptions{
			Provider: "kimi",
			Model:    "kimi-k2",
			Resume:   "sess-123",
			Continue: true,
		},
	}

	var captured bridge.Request
	original := fb.defaultQuery
	s.testBridgeQuery = func(ctx context.Context, r bridge.Request) (<-chan bridge.Event, error) {
		if r.Options.Provider == "openrouter" {
			captured = r
			ch := make(chan bridge.Event, 2)
			ch <- resultEvent("fallback")
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	s.executeQuery(context.Background(), req, nil)

	if captured.Options.Resume != "" {
		t.Error("fallback request should clear session key")
	}
	if captured.Options.Continue {
		t.Error("fallback request should clear continue")
	}
}