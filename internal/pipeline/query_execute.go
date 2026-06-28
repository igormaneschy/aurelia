package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
)

// errProcessDeath is a sentinel returned by validateChannel when the bridge
// process exits without producing a terminal event. The pipeline's existing
// process-death recovery must handle it, not the retry/fallback path.
var errProcessDeath = errors.New("bridge process exited without producing a terminal event")

const (
	defaultQueryMaxRetries   = 3
	defaultQueryRetryBackoff = 2 * time.Second
	defaultFallbackProvider  = "openrouter"
	defaultFallbackModel     = "openrouter/free"
)

// executeQuery runs a bridge query with retry and optional OpenRouter fallback.
// onNotify is called with user-facing messages (e.g. fallback activated).
func (s *Service) executeQuery(
	ctx context.Context,
	req bridge.Request,
	onNotify func(msg string),
) (ch <-chan bridge.Event, usedFallback bool, err error) {
	if s == nil || s.bridge == nil {
		return nil, false, fmt.Errorf("bridge not available")
	}

	provider := req.Options.Provider
	model := req.Options.Model

	chatID, threadID, userID := extractChatThreadUser(req)

	result := s.executeWithRetry(ctx, req, chatID, threadID, userID)
	if result.err == nil {
		return result.events, false, nil
	}

	if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
		return nil, false, result.err
	}

	if errors.Is(result.err, errProcessDeath) {
		return nil, false, result.err
	}

	cat := ClassifyError(result.err.Error())
	if cat.IsRetryable() {
		log.Printf("resilience: primary %s failed after retries (%v), attempting fallback", provider, redactSecrets(result.err.Error()))
	} else {
		te := TranslateError(provider, model, result.err.Error())
		safeNotify(onNotify, te.Message)
		if cat == ErrAuth {
			return nil, false, result.err
		}
	}

	return s.tryQueryFallback(ctx, req, onNotify, chatID, threadID, userID)
}

type queryResult struct {
	events <-chan bridge.Event
	err    error
}

func (s *Service) executeWithRetry(ctx context.Context, req bridge.Request, chatID int64, threadID int, userID int64) queryResult {
	var lastErr error

	for attempt := 0; attempt <= s.queryMaxRetries; attempt++ {
		if attempt > 0 {
			s.fireQueryEvent(chatID, threadID, userID, observability.PhaseRetryStarted, "warn",
				fmt.Sprintf("attempt=%d/%d provider=%s model=%s", attempt, s.queryMaxRetries, req.Options.Provider, req.Options.Model))
			delay := s.queryRetryBackoff * (1 << (attempt - 1))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return queryResult{err: ctx.Err()}
			}
		}

		evCh, err := s.bridgeQuery(ctx, req)
		if err == nil {
			validated, valErr := s.validateChannel(ctx, evCh)
			if valErr == nil {
				return queryResult{events: validated}
			}
			lastErr = valErr
			if !ClassifyError(valErr.Error()).IsRetryable() {
				return queryResult{err: valErr}
			}
			continue
		}

		lastErr = err
		if !ClassifyError(err.Error()).IsRetryable() {
			return queryResult{err: err}
		}
	}

	return queryResult{err: fmt.Errorf("max retries exceeded: %w", lastErr)}
}

func (s *Service) bridgeQuery(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error) {
	if s.testBridgeQuery != nil {
		return s.testBridgeQuery(ctx, req)
	}
	return s.bridge.Execute(ctx, req)
}

func (s *Service) validateChannel(ctx context.Context, src <-chan bridge.Event) (<-chan bridge.Event, error) {
	var prefix []bridge.Event
	for {
		select {
		case ev, ok := <-src:
			if !ok {
				if len(prefix) == 0 {
					return nil, errProcessDeath
				}
				return replayBuffer(prefix), nil
			}
			if ev.IsTerminal() {
				if ev.Type == "error" {
					msg := ev.Message
					if msg == "" {
						msg = ev.Content
					}
					if msg == "" {
						msg = "unknown bridge error"
					}
					return nil, fmt.Errorf("%s", msg)
				}
				prefix = append(prefix, ev)
				return replayBuffer(prefix), nil
			}
			prefix = append(prefix, ev)
			return proxyChannel(prefix, src), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func replayBuffer(buf []bridge.Event) <-chan bridge.Event {
	out := make(chan bridge.Event, len(buf))
	for _, ev := range buf {
		out <- ev
	}
	close(out)
	return out
}

func proxyChannel(prefix []bridge.Event, src <-chan bridge.Event) <-chan bridge.Event {
	out := make(chan bridge.Event, len(prefix)+16)
	for _, ev := range prefix {
		out <- ev
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("query_execute: panic in proxyChannel", "error", r)
			}
		}()
		defer close(out)
		for ev := range src {
			out <- ev
		}
	}()
	return out
}

func (s *Service) tryQueryFallback(ctx context.Context, req bridge.Request, onNotify func(msg string), chatID int64, threadID int, userID int64) (<-chan bridge.Event, bool, error) {
	s.fireQueryEvent(chatID, threadID, userID, observability.PhaseFallbackStarted, "warn",
		fmt.Sprintf("provider=%s model=%s fallback_provider=%s",
			req.Options.Provider, req.Options.Model, s.fallbackProvider))

	if s.openRouterAPIKey == "" {
		safeNotify(onNotify, OpenRouterNotConfiguredMessage())
		return nil, false, fmt.Errorf("fallback unavailable: OpenRouter not configured")
	}

	fallbackReq := req
	fallbackReq.Options.Provider = s.fallbackProvider
	fallbackReq.Options.Model = s.fallbackModel
	fallbackReq.Options.Resume = ""
	fallbackReq.Options.Continue = false

	safeNotify(onNotify, FallbackMessage(req.Options.Provider))

	log.Printf("resilience: falling back to %s/%s", s.fallbackProvider, s.fallbackModel)

	if chatID != 0 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("query_execute: panic in continuitySnapshot: %v", r)
				}
			}()
			snapshot := s.continuitySnapshot(ctx, chatID, threadID, userID)
			if snapshot != "" {
				snapshotBlock := "\n\n## Previous Session Context (recovered)\n\n" +
					"The following is recovered context from the previous session that was interrupted. " +
					"Use it to continue the task.\n\n" +
					"<fallback_context_untrusted>\n" + snapshot + "\n</fallback_context_untrusted>"

				if fallbackReq.Options.SystemPrompt != "" {
					fallbackReq.Options.SystemPrompt += snapshotBlock
				} else {
					fallbackReq.Options.SystemPrompt = snapshotBlock
				}
				log.Printf("resilience: injected continuity snapshot into fallback prompt (chat=%d thread=%d)", chatID, threadID)
			}
		}()
	}

	evCh, err := s.bridgeQuery(ctx, fallbackReq)
	if err != nil {
		safeNotify(onNotify, FinalErrorMessage())
		return nil, false, fmt.Errorf("fallback failed: %w", err)
	}

	validated, valErr := s.validateChannel(ctx, evCh)
	if valErr != nil {
		safeNotify(onNotify, FinalErrorMessage())
		return nil, false, fmt.Errorf("fallback failed: %w", valErr)
	}

	return validated, true, nil
}

func extractChatThreadUser(req bridge.Request) (chatID int64, threadID int, userID int64) {
	if req.Options.Security != nil {
		return req.Options.Security.ChatID, req.Options.Security.ThreadID, req.Options.Security.UserID
	}
	return req.Options.ChatID, req.Options.ThreadID, req.Options.UserID
}

func safeNotify(onNotify func(string), msg string) {
	if onNotify == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("query_execute: panic in onNotify: %v", r)
		}
	}()
	onNotify(msg)
}

func (s *Service) fireQueryEvent(chatID int64, threadID int, userID int64, phase, level, message string) {
	if s.OnEvent == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("query_execute: panic in OnEvent: %v", r)
		}
	}()
	s.OnEvent(chatID, threadID, userID, phase, level, message)
}