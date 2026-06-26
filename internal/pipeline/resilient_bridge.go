package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/igormaneschy/aurelia/internal/engine"
	"github.com/igormaneschy/aurelia/internal/observability"
)

// ResilientBridge wraps an engine with retry, circuit breaker, and fallback.
type ResilientBridge struct {
	bridge   engine.Engine
	breakers *circuitBreakerRegistry
	config   ResilientConfig

	// ContinuitySnapshot is called before fallback to capture current context.
	// Returns a compact summary string to inject into the fallback prompt.
	// The summary is already redacted, escaped, and capped. May be empty string.
	ContinuitySnapshot func(ctx context.Context, chatID int64, threadID int, userID int64) string

	// OnEvent is called when the resilient bridge emits an observable event
	// (retry, fallback, circuit breaker). The callback must be fast and
	// fail-open. May be nil.
	OnEvent func(chatID int64, threadID int, userID int64, phase, level, message string)
}

// ResilientConfig configures retry and fallback behavior.
type ResilientConfig struct {
	MaxRetries       int
	RetryBackoffBase time.Duration
	FallbackProvider string
	FallbackModel    string
	OpenRouterAPIKey string // empty = fallback disabled
}

// DefaultResilientConfig returns sensible defaults.
func DefaultResilientConfig() ResilientConfig {
	return ResilientConfig{
		MaxRetries:       3,
		RetryBackoffBase: 2 * time.Second,
		FallbackProvider: "openrouter",
		FallbackModel:    "openrouter/free",
	}
}

// NewResilientBridge wraps the given bridge with resilience features.
func NewResilientBridge(b engine.Engine, cfg ResilientConfig) *ResilientBridge {
	return &ResilientBridge{
		bridge:   b,
		breakers: newCircuitBreakerRegistry(),
		config:   cfg,
	}
}

// ExecuteResult holds the outcome of a resilient execution.
type ExecuteResult struct {
	Events       <-chan engine.Event
	UsedFallback bool
	Err          error
}

// errProcessDeath is a sentinel returned by validateChannel when the bridge
// process exits without producing a terminal event. The pipeline's existing
// process-death recovery must handle it, not the resilient retry/fallback path.
var errProcessDeath = errors.New("bridge process exited without producing a terminal event")

// Execute runs a request with retry, circuit breaker, and fallback.
// onNotify is called with user-facing messages (e.g. fallback activated).
func (rb *ResilientBridge) Execute(
	ctx context.Context,
	req engine.Request,
	onNotify func(msg string),
) ExecuteResult {
	provider := req.Provider
	model := req.Model

	// Extract chat/thread from security context for fallback snapshot injection.
	// Must be captured once per Execute call and passed explicitly to tryFallback
	// to avoid data races (ResilientBridge is shared across goroutines).
	chatID, threadID, userID := extractChatThreadUser(req)

	// 1. Circuit breaker open → skip directly to fallback.
	if rb.breakers.ShouldSkip(provider) {
		if msg := rb.breakers.NotifyMessage(provider); msg != "" {
			safeNotify(onNotify, msg)
		}
		return rb.tryFallback(ctx, req, onNotify, chatID, threadID, userID)
	}

	// 2. Try primary provider with retries.
	result := rb.executeWithRetry(ctx, req, chatID, threadID, userID)
	if result.Err == nil {
		rb.breakers.RecordResult(provider, true)
		return result
	}

	// Don't record failure or attempt fallback if the user cancelled.
	if errors.Is(result.Err, context.Canceled) || errors.Is(result.Err, context.DeadlineExceeded) {
		return result
	}

	// Process death is not a provider failure — let the pipeline handle it.
	if errors.Is(result.Err, errProcessDeath) {
		return result
	}

	// 3. Primary failed → record failure and attempt fallback.
	rb.breakers.RecordResult(provider, false)

	cat := ClassifyError(result.Err.Error())
	if cat.IsRetryable() {
		// User already saw retry messages if onNotify was used inside executeWithRetry.
		log.Printf("resilience: primary %s failed after retries (%v), attempting fallback", provider, redactSecrets(result.Err.Error()))
	} else {
		te := TranslateError(provider, model, result.Err.Error())
		if onNotify != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("resilient_bridge: panic in onNotify: %v", r)
					}
				}()
				onNotify(te.Message)
			}()
		}
		// Non-retryable errors still get fallback for better UX (except auth).
		if cat == ErrAuth {
			return result
		}
	}

	return rb.tryFallback(ctx, req, onNotify, chatID, threadID, userID)
}

// executeWithRetry attempts the request up to MaxRetries with exponential backoff.
// Only transient errors trigger retry; others fail fast.
func (rb *ResilientBridge) executeWithRetry(ctx context.Context, req engine.Request, chatID int64, threadID int, userID int64) ExecuteResult {
	var lastErr error

	for attempt := 0; attempt <= rb.config.MaxRetries; attempt++ {
		if attempt > 0 {
			rb.fireEvent(chatID, threadID, userID, observability.PhaseRetryStarted, "warn",
				fmt.Sprintf("attempt=%d/%d provider=%s model=%s", attempt, rb.config.MaxRetries, req.Provider, req.Model))
			delay := rb.config.RetryBackoffBase * (1 << (attempt - 1))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ExecuteResult{Err: ctx.Err()}
			}
		}

		evCh, err := rb.bridge.Query(ctx, req)
		if err == nil {
			// Validate the first terminal event — if it's an error, classify it.
			validated, valErr := rb.validateChannel(ctx, evCh)
			if valErr == nil {
				return ExecuteResult{Events: validated}
			}
			lastErr = valErr
			cat := ClassifyError(valErr.Error())
			if !cat.IsRetryable() {
				return ExecuteResult{Err: valErr}
			}
			// Transient → continue to next retry attempt.
			continue
		}

		lastErr = err
		cat := ClassifyError(err.Error())
		if !cat.IsRetryable() {
			return ExecuteResult{Err: err}
		}
	}

	return ExecuteResult{Err: fmt.Errorf("max retries exceeded: %w", lastErr)}
}

// validateChannel reads at most until the first non-terminal event arrives
// (proving the stream is live) OR a terminal event arrives. Terminal errors
// surface as errors; otherwise a wrapper channel is returned that prepends
// the events already consumed and proxies the rest live so downstream
// consumers (e.g. ProgressReporter) see tool_use events as they happen
// instead of after the full response completes.
func (rb *ResilientBridge) validateChannel(ctx context.Context, src <-chan engine.Event) (<-chan engine.Event, error) {
	var prefix []engine.Event
	for {
		select {
		case ev, ok := <-src:
			if !ok {
				if len(prefix) == 0 {
					return nil, errProcessDeath
				}
				// Stream closed before terminal but we had partial events —
				// replay them so the caller sees what arrived; downstream
				// handles missing terminal as process death.
				return replayBuffer(prefix), nil
			}
			if ev.IsTerminal() {
				if ev.RawType == "error" || ev.Type == engine.EventTypeError {
					msg := ev.Message
					if msg == "" {
						msg = ev.Content
					}
					if msg == "" && ev.Err != nil {
						msg = ev.Err.Error()
					}
					if msg == "" {
						msg = "unknown bridge error"
					}
					return nil, fmt.Errorf("%s", msg)
				}
				prefix = append(prefix, ev)
				return replayBuffer(prefix), nil
			}
			// First non-terminal event proves the stream is live — start
			// proxying remaining events without buffering.
			prefix = append(prefix, ev)
			return proxyChannel(prefix, src), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// replayBuffer returns a closed channel preloaded with the buffered events.
func replayBuffer(buf []engine.Event) <-chan engine.Event {
	out := make(chan engine.Event, len(buf))
	for _, ev := range buf {
		out <- ev
	}
	close(out)
	return out
}

// proxyChannel returns a channel that emits the prefix first, then forwards
// every event from src as it arrives. Closes when src closes.
func proxyChannel(prefix []engine.Event, src <-chan engine.Event) <-chan engine.Event {
	out := make(chan engine.Event, len(prefix)+16)
	for _, ev := range prefix {
		out <- ev
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("resilient_bridge: panic in proxyChannel", "error", r)
			}
		}()
		defer close(out)
		for ev := range src {
			out <- ev
		}
	}()
	return out
}

// tryFallback attempts the request with the fallback provider.
// chatID, threadID, and userID are extracted from the request security context by the
// caller (Execute) and passed explicitly instead of stored on the struct to
// avoid data races — ResilientBridge is shared across goroutines.
func (rb *ResilientBridge) tryFallback(ctx context.Context, req engine.Request, onNotify func(string), chatID int64, threadID int, userID int64) ExecuteResult {
	rb.fireEvent(chatID, threadID, userID, observability.PhaseFallbackStarted, "warn",
		fmt.Sprintf("provider=%s model=%s fallback_provider=%s",
			req.Provider, req.Model, rb.config.FallbackProvider))

	if rb.config.OpenRouterAPIKey == "" {
		safeNotify(onNotify, OpenRouterNotConfiguredMessage())
		return ExecuteResult{Err: fmt.Errorf("fallback unavailable: OpenRouter not configured")}
	}

	fallbackReq := req
	fallbackReq.Provider = rb.config.FallbackProvider
	fallbackReq.Model = rb.config.FallbackModel
	// Do NOT carry over resume/continue across providers (PI SDK limitation).
	fallbackReq.SessionKey = ""
	fallbackReq.Continue = false

	safeNotify(onNotify, FallbackMessage(req.Provider))

	log.Printf("resilience: falling back to %s/%s", rb.config.FallbackProvider, rb.config.FallbackModel)

	// Inject continuity snapshot into fallback system prompt before executing.
	// Protected with defer/recover so a panic in the callback does not abort
	// the fallback attempt.
	if rb.ContinuitySnapshot != nil && chatID != 0 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("resilient_bridge: panic in ContinuitySnapshot: %v", r)
				}
			}()
			snapshot := rb.ContinuitySnapshot(ctx, chatID, threadID, userID)
			if snapshot != "" {
				snapshotBlock := "\n\n## Previous Session Context (recovered)\n\n" +
					"The following is recovered context from the previous session that was interrupted. " +
					"Use it to continue the task.\n\n" +
					"<fallback_context_untrusted>\n" + snapshot + "\n</fallback_context_untrusted>"

				if fallbackReq.SystemPrompt != "" {
					fallbackReq.SystemPrompt += snapshotBlock
				} else {
					fallbackReq.SystemPrompt = snapshotBlock
				}
				log.Printf("resilience: injected continuity snapshot into fallback prompt (chat=%d thread=%d)", chatID, threadID)
			}
		}()
	}

	evCh, err := rb.bridge.Query(ctx, fallbackReq)
	if err != nil {
		safeNotify(onNotify, FinalErrorMessage())
		return ExecuteResult{Err: fmt.Errorf("fallback failed: %w", err)}
	}

	// Validate the fallback channel.
	validated, valErr := rb.validateChannel(ctx, evCh)
	if valErr != nil {
		safeNotify(onNotify, FinalErrorMessage())
		return ExecuteResult{Err: fmt.Errorf("fallback failed: %w", valErr)}
	}

	return ExecuteResult{Events: validated, UsedFallback: true}
}

// BreakerState returns the circuit breaker state for a provider (for status/debug).
func (rb *ResilientBridge) BreakerState(provider string) CircuitState {
	return rb.breakers.get(provider).State()
}

// safeNotify calls onNotify with panic recovery. Used for all user-facing
// notifications that are not critical to the execution flow.
func safeNotify(onNotify func(string), msg string) {
	if onNotify == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("resilient_bridge: panic in onNotify: %v", r)
		}
	}()
	onNotify(msg)
}

// fireEvent calls OnEvent if set, with panic recovery.
func (rb *ResilientBridge) fireEvent(chatID int64, threadID int, userID int64, phase, level, message string) {
	if rb.OnEvent == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("resilient_bridge: panic in OnEvent: %v", r)
		}
	}()
	rb.OnEvent(chatID, threadID, userID, phase, level, message)
}

// extractChatThreadUser reads ChatID, ThreadID, and UserID from the request's security
// context, or returns zero values when no security context is present.
// Must be called once per Execute invocation and the results passed explicitly
// to tryFallback to avoid data races on ResilientBridge shared state.
func extractChatThreadUser(req engine.Request) (chatID int64, threadID int, userID int64) {
	if req.Security != nil {
		return req.Security.ChatID, req.Security.ThreadID, req.Security.UserID
	}
	return req.ChatID, req.ThreadID, req.UserID
}
