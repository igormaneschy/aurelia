package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// forceTransportCleanup terminates the bridge process after a cancellation
// handshake deadline. It deliberately does not detach the SDK operation: the
// process that owns that operation is killed, pending streams are closed, and
// the reader performs its bounded reap path.
func (b *Bridge) forceTransportCleanup(requestID string) {
	b.mu.Lock()
	stdin := b.stdin
	cmd := b.cmd
	done := b.done
	b.stopping = true
	b.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil && (cmd.ProcessState == nil || !cmd.ProcessState.Exited()) {
		if err := cmd.Process.Kill(); err != nil {
			slog.Warn("bridge: forced compact-session process cleanup failed", "request_id", requestID,
				"error", boundedEventText(err.Error(), 256))
		}
	}
	b.pendingMu.Lock()
	for rid, stream := range b.pending {
		stream.close()
		delete(b.pending, rid)
	}
	b.pendingMu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			slog.Warn("bridge: forced compact-session cleanup reader still stopping", "request_id", requestID)
		}
	}
}

// CompactSessionResult holds the result of a compact-session command.
type CompactSessionResult struct {
	Success      bool   `json:"success"`
	TokensBefore int    `json:"tokens_before"`
	Summary      string `json:"summary,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	SessionFile  string `json:"session_file,omitempty"`
}

// CompactSessionEvent is a bounded intermediate compaction event streamed by
// CompactSessionWithEvents. Only normalized enum/static values are exposed —
// the raw SDK reason/error text never crosses this boundary. Type is
// "compaction_start" or "compaction_end"; the remaining fields are only
// populated for compaction_end.
type CompactSessionEvent struct {
	Type             string
	RequestID        string // validated Bridge request correlation identity
	Reason           string // manual | automatic | unknown
	Success          bool
	ErrorClass       string // static: "compaction_error" | ""
	TokensBefore     int
	TokensAfter      *int
	DeltaTokens      *int
	DurationMeasured bool // authoritative presence marker, true even for 0ms
	DurationMs       int64
	Timestamp        string // raw ISO-8601 correlation metadata
}

// CompactSessionWithEvents requests proactive compaction of a PI session and
// streams bounded intermediate events (compaction_start/compaction_end) to
// onEvent as they arrive. onEvent may be nil. The terminal event is handled
// exactly once: it returns the compaction result (or an error) and the raw
// SDK error message is never exposed. Behavior matches CompactSession.
func (b *Bridge) CompactSessionWithEvents(ctx context.Context, opts RequestOptions, onEvent func(CompactSessionEvent)) (*CompactSessionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Keep the transport request alive after the caller cancels long enough to
	// send the protocol cancel command and observe the compact-session terminal
	// event. Execute(ctx) would only stop the Go proxy and would leave the
	// TypeScript SDK operation running.
	requestID := fmt.Sprintf("compact-%d", b.reqCounter.Add(1))
	bridgeCtx, bridgeCancel := context.WithCancel(context.Background())
	defer bridgeCancel()
	ch, err := b.Execute(bridgeCtx, Request{
		Command:   "compact-session",
		RequestID: requestID,
		Options:   opts,
	})
	if err != nil {
		return nil, fmt.Errorf("bridge: compact-session: %w", err)
	}

	const compactionCancelGrace = 2 * time.Second
	cancelWatchStop := make(chan struct{})
	cancelWatchDone := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), compactionCancelGrace)
			defer cancel()
			cancelWatchDone <- b.CancelRequest(cancelCtx, requestID)
		case <-cancelWatchStop:
			cancelWatchDone <- nil
		}
	}()
	stopCancelWatch := func() {
		select {
		case <-cancelWatchStop:
		default:
			close(cancelWatchStop)
		}
		select {
		case <-cancelWatchDone:
		case <-time.After(compactionCancelGrace):
			slog.Warn("bridge: compact-session cancellation watcher did not stop", "request_id", requestID)
		}
	}
	defer stopCancelWatch()

	var result *CompactSessionResult
	compactionStarted := false
	compactionEnded := false
	lastReason := "unknown"
	emitCompaction := func(ev CompactSessionEvent) {
		// The transport routes the stream by request ID, but do not trust an
		// SDK/event payload to repeat that identity correctly. Every accepted
		// compaction event belongs to this command, never to a shared/foreign
		// correlation key.
		ev.RequestID = requestID
		switch ev.Type {
		case "compaction_start":
			if compactionStarted {
				return
			}
			compactionStarted = true
			lastReason = normalizeCompactionReasonForBridge(ev.Reason)
		case "compaction_end":
			if compactionEnded {
				return
			}
			compactionEnded = true
			if ev.Reason == "" {
				ev.Reason = lastReason
			}
		}
		if onEvent != nil {
			onEvent(ev)
		}
	}
	ensureEnd := func(success bool) {
		if !compactionStarted || compactionEnded {
			return
		}
		end := CompactSessionEvent{
			Type:      "compaction_end",
			RequestID: requestID,
			Reason:    lastReason,
			Success:   success,
		}
		if !success {
			end.ErrorClass = "compaction_error"
		}
		emitCompaction(end)
	}

	ctxDone := ctx.Done()
	cancelRequested := false
	cancelDeadline := (<-chan time.Time)(nil)
	var cancelTimer *time.Timer
	stopCancelTimer := func() {
		if cancelTimer == nil {
			return
		}
		if !cancelTimer.Stop() {
			select {
			case <-cancelTimer.C:
			default:
			}
		}
		cancelTimer = nil
		cancelDeadline = nil
	}
	defer stopCancelTimer()
	for {
		var cancelEvents <-chan error
		if cancelRequested {
			cancelEvents = cancelWatchDone
		}
		select {
		case ev, ok := <-ch:
			if !ok {
				ensureEnd(false)
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, errors.New("bridge: compact-session ended without result")
			}
			switch ev.Type {
			case "compaction_start", "compaction_end":
				emitCompaction(CompactSessionEvent{
					Type:             ev.Type,
					RequestID:        ev.RequestID,
					Reason:           ev.Reason,
					Success:          ev.Success,
					ErrorClass:       ev.ErrorClass,
					TokensBefore:     ev.TokensBefore,
					TokensAfter:      ev.TokensAfter,
					DeltaTokens:      ev.DeltaTokens,
					DurationMeasured: ev.DurationMeasured,
					DurationMs:       ev.DurationMs,
					Timestamp:        ev.Timestamp,
				})
			case "error":
				if ctx.Err() != nil {
					ensureEnd(false)
					return nil, ctx.Err()
				}
				ensureEnd(false)
				// Keep the public error class stable; provider/SDK detail is not
				// required for durable lifecycle decisions and must not escape here.
				return nil, errors.New("bridge: compact-session error: compaction failed")
			case "result":
				if ctx.Err() != nil {
					ensureEnd(false)
					return nil, ctx.Err()
				}
				if ev.Content == "" {
					ensureEnd(false)
					return nil, nil
				}
				var res CompactSessionResult
				if err := json.Unmarshal([]byte(ev.Content), &res); err != nil {
					ensureEnd(false)
					return nil, fmt.Errorf("bridge: compact-session parse: %w", err)
				}
				result = &res
			}
		case <-ctxDone:
			if !cancelRequested {
				cancelRequested = true
				ctxDone = nil
				cancelTimer = time.NewTimer(compactionCancelGrace)
				cancelDeadline = cancelTimer.C
			}
		case cancelErr := <-cancelEvents:
			cancelEvents = nil
			if cancelErr != nil {
				slog.Warn("bridge: compact-session cancellation request failed", "request_id", requestID,
					"error", boundedEventText(cancelErr.Error(), 256))
				b.forceTransportCleanup(requestID)
				ensureEnd(false)
				return nil, ctx.Err()
			}
			// The TypeScript cancel handler acknowledges only after the exact
			// SDK compaction finally path has emitted its terminal event.
			ctxDone = nil
			stopCancelTimer()
		case <-cancelDeadline:
			// The protocol cancel handshake did not complete. Kill the bridge
			// process so the SDK operation cannot continue detached after the
			// caller returns; this is a cleanup failure, not cancellation success.
			b.forceTransportCleanup(requestID)
			ensureEnd(false)
			return nil, ctx.Err()
		}
		if result != nil {
			// A provider/SDK may return a terminal result without forwarding its
			// compaction_end callback. Preserve the exactly-once terminal event
			// contract without misclassifying a successful result as an error.
			ensureEnd(result.Success)
			return result, nil
		}
	}
}

func normalizeCompactionReasonForBridge(reason string) string {
	switch reason {
	case "manual", "automatic", "unknown":
		return reason
	default:
		return "unknown"
	}
}

// CompactSession requests proactive compaction of a PI session.
// Returns the compaction result with tokens before/after.
func (b *Bridge) CompactSession(ctx context.Context, opts RequestOptions) (*CompactSessionResult, error) {
	return b.CompactSessionWithEvents(ctx, opts, nil)
}
