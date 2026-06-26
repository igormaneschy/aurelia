package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/engine"
)

// fakeEngine is a test double for engine.Engine.
type fakeEngine struct {
	calls     []engine.Request
	responses map[string][]engine.Event
	defaultEv engine.Event
	queryFn   func(ctx context.Context, req engine.Request) (<-chan engine.Event, error)
}

func newFakeEngine() *fakeEngine {
	f := &fakeEngine{responses: make(map[string][]engine.Event)}
	f.queryFn = f.defaultQuery
	return f
}

func (f *fakeEngine) addResponse(prompt string, events []engine.Event) {
	f.responses[prompt] = events
}

func (f *fakeEngine) defaultQuery(ctx context.Context, req engine.Request) (<-chan engine.Event, error) {
	f.calls = append(f.calls, req)
	ch := make(chan engine.Event, 16)
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

func (f *fakeEngine) Query(ctx context.Context, req engine.Request) (<-chan engine.Event, error) {
	return f.queryFn(ctx, req)
}

func (f *fakeEngine) Command(_ context.Context, _ engine.Command) (engine.Event, error) {
	return engine.Event{}, nil
}

func (f *fakeEngine) Stats(_ context.Context, _ string, _ engine.StatsOptions) (engine.Stats, error) {
	return engine.Stats{}, nil
}

func resultEvent(content string) engine.Event {
	return engine.Event{Type: engine.EventTypeDone, RawType: "result", Content: content}
}

func errorEvent(message string) engine.Event {
	return engine.Event{Type: engine.EventTypeError, RawType: "error", Message: message}
}

func TestResilientBridge_Success(t *testing.T) {
	fb := newFakeEngine()
	fb.addResponse("hello", []engine.Event{resultEvent("world")})

	rb := NewResilientBridge(fb, fastConfig())
	req := engine.Request{Prompt: "hello", Provider: "kimi", Model: "kimi-k2"}

	var notify string
	res := rb.Execute(context.Background(), req, func(msg string) { notify = msg })

	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.UsedFallback {
		t.Error("should not use fallback on success")
	}
	if notify != "" {
		t.Error("should not notify on success")
	}
}

func TestResilientBridge_TransientRetryThenSuccess(t *testing.T) {
	fb := newFakeEngine()
	attempt := 0
	fb.defaultEv = errorEvent("rate limit exceeded")

	rb := NewResilientBridge(fb, ResilientConfig{
		MaxRetries:       3,
		RetryBackoffBase: 50 * time.Millisecond,
	})

	req := engine.Request{Prompt: "retry-test", Provider: "kimi", Model: "kimi-k2"}

	original := fb.queryFn
	fb.queryFn = func(ctx context.Context, r engine.Request) (<-chan engine.Event, error) {
		attempt++
		if attempt >= 3 {
			ch := make(chan engine.Event, 2)
			ch <- resultEvent("success after retry")
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	res := rb.Execute(context.Background(), req, nil)
	if res.Err != nil {
		t.Fatalf("expected success after retry, got: %v", res.Err)
	}
	if attempt < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", attempt)
	}
}

func TestResilientBridge_CircuitBreakerOpens(t *testing.T) {
	fb := newFakeEngine()
	fb.defaultEv = errorEvent("rate limit exceeded")

	cfg := fastConfig()
	cfg.OpenRouterAPIKey = "sk-test"
	rb := NewResilientBridge(fb, cfg)

	req := engine.Request{Prompt: "fail", Provider: "kimi", Model: "kimi-k2"}

	original := fb.queryFn
	fb.queryFn = func(ctx context.Context, r engine.Request) (<-chan engine.Event, error) {
		if r.Provider == "openrouter" {
			ch := make(chan engine.Event, 2)
			ch <- resultEvent("fallback success")
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	for i := 0; i < circuitFailureThreshold; i++ {
		rb.Execute(context.Background(), req, nil)
	}

	if rb.BreakerState("kimi") != CircuitOpen {
		t.Fatal("circuit should be open")
	}

	var notifyMsg string
	res := rb.Execute(context.Background(), req, func(msg string) { notifyMsg = msg })

	if !res.UsedFallback {
		t.Error("should use fallback when circuit is open")
	}
	if notifyMsg == "" {
		t.Error("should notify user about fallback")
	}
}

func fastConfig() ResilientConfig {
	cfg := DefaultResilientConfig()
	cfg.RetryBackoffBase = 10 * time.Millisecond
	return cfg
}

func TestResilientBridge_FallbackWithoutOpenRouterKey(t *testing.T) {
	fb := newFakeEngine()
	fb.defaultEv = errorEvent("rate limit exceeded")

	cfg := fastConfig()
	cfg.OpenRouterAPIKey = ""
	rb := NewResilientBridge(fb, cfg)

	req := engine.Request{Prompt: "fail", Provider: "kimi", Model: "kimi-k2"}

	var notifyMsg string
	res := rb.Execute(context.Background(), req, func(msg string) { notifyMsg = msg })

	if res.Err == nil {
		t.Fatal("expected error when fallback is unavailable")
	}
	if !strings.Contains(notifyMsg, "OpenRouter") {
		t.Error("should notify about missing OpenRouter config")
	}
}

func TestResilientBridge_NonRetryableError(t *testing.T) {
	fb := newFakeEngine()
	fb.addResponse("auth-fail", []engine.Event{errorEvent("Invalid API key")})

	rb := NewResilientBridge(fb, fastConfig())
	req := engine.Request{Prompt: "auth-fail", Provider: "kimi", Model: "kimi-k2"}

	var notifyMsg string
	res := rb.Execute(context.Background(), req, func(msg string) { notifyMsg = msg })

	if res.Err == nil {
		t.Fatal("expected error for auth failure")
	}
	if !strings.Contains(notifyMsg, "autenticação") {
		t.Error("should notify about auth error")
	}
	if res.UsedFallback {
		t.Error("should NOT fallback on auth error")
	}
}

func TestResilientBridge_FallbackSuccess(t *testing.T) {
	fb := newFakeEngine()
	fb.addResponse("primary-fail", []engine.Event{errorEvent("rate limit exceeded")})

	cfg := fastConfig()
	cfg.OpenRouterAPIKey = "sk-test"
	rb := NewResilientBridge(fb, cfg)

	req := engine.Request{Prompt: "primary-fail", Provider: "kimi", Model: "kimi-k2"}

	original := fb.queryFn
	fb.queryFn = func(ctx context.Context, r engine.Request) (<-chan engine.Event, error) {
		if r.Provider == "openrouter" {
			ch := make(chan engine.Event, 2)
			ch <- resultEvent("fallback result")
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	res := rb.Execute(context.Background(), req, nil)
	if res.Err != nil {
		t.Fatalf("expected fallback success: %v", res.Err)
	}
	if !res.UsedFallback {
		t.Error("should mark fallback as used")
	}
}

func TestResilientBridge_AllRetriesFail(t *testing.T) {
	fb := newFakeEngine()
	fb.addResponse("always-fail", []engine.Event{errorEvent("rate limit exceeded")})

	cfg := fastConfig()
	cfg.OpenRouterAPIKey = "sk-test"
	rb := NewResilientBridge(fb, cfg)

	req := engine.Request{Prompt: "always-fail", Provider: "kimi", Model: "kimi-k2"}

	fb.queryFn = func(ctx context.Context, r engine.Request) (<-chan engine.Event, error) {
		ch := make(chan engine.Event, 2)
		ch <- errorEvent("fallback also down")
		close(ch)
		return ch, nil
	}

	res := rb.Execute(context.Background(), req, nil)
	if res.Err == nil {
		t.Fatal("expected error when all fail")
	}
	if !strings.Contains(res.Err.Error(), "fallback failed") {
		t.Errorf("expected fallback to be attempted and fail, got: %v", res.Err)
	}
}

func TestResilientBridge_CancelDuringRetry(t *testing.T) {
	fb := newFakeEngine()
	fb.addResponse("slow", []engine.Event{errorEvent("rate limit exceeded")})

	rb := NewResilientBridge(fb, ResilientConfig{
		MaxRetries:       3,
		RetryBackoffBase: 10 * time.Second,
	})

	req := engine.Request{Prompt: "slow", Provider: "kimi", Model: "kimi-k2"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	res := rb.Execute(ctx, req, nil)
	if res.Err != context.Canceled {
		t.Fatalf("expected context canceled, got: %v", res.Err)
	}
}

func TestResilientBridge_ProcessDeathOutcome(t *testing.T) {
	fb := newFakeEngine()
	fb.queryFn = func(ctx context.Context, r engine.Request) (<-chan engine.Event, error) {
		ch := make(chan engine.Event)
		close(ch)
		return ch, nil
	}

	rb := NewResilientBridge(fb, fastConfig())
	req := engine.Request{Prompt: "death", Provider: "kimi", Model: "kimi-k2"}

	res := rb.Execute(context.Background(), req, nil)
	if res.Err == nil {
		t.Fatal("expected error for process death")
	}
}

func TestResilientBridge_FallbackResetsSession(t *testing.T) {
	fb := newFakeEngine()
	fb.addResponse("session-test", []engine.Event{errorEvent("rate limit exceeded")})

	cfg := fastConfig()
	cfg.OpenRouterAPIKey = "sk-test"
	rb := NewResilientBridge(fb, cfg)

	req := engine.Request{
		Prompt:     "session-test",
		Provider:   "kimi",
		Model:      "kimi-k2",
		SessionKey: "sess-123",
		Continue:   true,
	}

	var captured engine.Request
	original := fb.queryFn
	fb.queryFn = func(ctx context.Context, r engine.Request) (<-chan engine.Event, error) {
		if r.Provider == "openrouter" {
			captured = r
			ch := make(chan engine.Event, 2)
			ch <- resultEvent("fallback")
			close(ch)
			return ch, nil
		}
		return original(ctx, r)
	}

	rb.Execute(context.Background(), req, nil)

	if captured.SessionKey != "" {
		t.Error("fallback request should clear session key")
	}
	if captured.Continue {
		t.Error("fallback request should clear continue")
	}
}