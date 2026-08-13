package pipeline

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
)

// runLogState is the per-run runlog accumulator. It tracks the run identity,
// pending events, tool summaries and the long-session feedback/silence
// aggregates so the terminal write is a single, bounded operation.
type runLogState struct {
	mu               sync.Mutex
	runID            string
	requestID        string
	owner            *activeRun // compare-and-delete/update ownership token
	summary          strings.Builder
	summaryCount     int
	wg               sync.WaitGroup // tracks in-flight DB updates
	finalizing       bool           // terminal finalization has been claimed
	finalized        bool           // terminal persistence path has completed
	finalizer        *terminalFinalization
	partialAssistant string // last partial assistant text, for checkpoint on timeout
	writeToolsUsed   bool   // Write/Edit tools may have changed memory files
	pendingEvents    []runlog.RunEvent
	pendingBytes     int
	pendingDropped   int

	// Long-session feedback/silence tracking. Only surface-updating events
	// (assistant, tool_use, tool_result, result, error) count as feedback;
	// lifecycle/telemetry events (stall, steer, compaction, retries) never do.
	startedAt       time.Time
	firstFeedbackAt time.Time
	lastFeedbackAt  time.Time
	maxSilenceMs    int64
	stallCount      int
	steerCount      int

	// Telemetry budget (stall/steer/compaction). Counted before events are
	// accumulated in pendingEvents; overflow is dropped explicitly and
	// countable (telemetryDropped) instead of growing without limit. Terminal
	// events are never subject to this budget.
	telemetryEvents     int
	telemetryBytes      int
	telemetryDropped    int
	telemetryDropLogged bool
}

// Telemetry budget for bridge_health events (stall/steer/compaction) per run.
// These are diagnostic-only; once the budget is exhausted further telemetry
// is dropped (counted, logged once) without ever blocking the pipeline.
const (
	maxTelemetryEventsPerRun   = 64
	maxTelemetryBytesPerRun    = 16 * 1024
	maxPendingEventsPerRun     = 256
	maxPendingBytesPerRun      = 64 * 1024
	maxRunlogEventMessageBytes = 2048
	maxRunlogErrorRunes        = 1024
	maxAssistantBytesPerRun    = 128 * 1024
	maxTelemetryMetricValue    = 100_000_000
)

// sanitizeForPersistence applies every transformation in the safe order:
// redact secrets first, then repair UTF-8/control characters, then truncate.
// It is used for continuity, checkpoints, errors and runlog text, not for the
// user-facing assistant reply itself.
func sanitizeForPersistence(content string, maxRunes int) string {
	content = redactSecrets(strings.ToValidUTF8(content, "\uFFFD"))
	content = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, content)
	return truncateRunesExact(content, maxRunes)
}

// sanitizeForPersistenceBytes is the byte-budget companion used for timeline
// messages. The cut is always made after redaction and at a UTF-8 boundary.
func sanitizeForPersistenceBytes(content string, maxBytes int) string {
	content = redactSecrets(strings.ToValidUTF8(content, "\uFFFD"))
	content = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, content)
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}
	cut := content[:maxBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	for len(cut) > 0 && cut[len(cut)-1]&0xc0 == 0x80 {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func truncateRunesExact(content string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes])
}

// runOwnership is carried by a live pipeline goroutine. Both the durable run
// ID and the in-memory active-run token must still match before a stale run
// can mutate or delete runLogStates.
type runOwnership struct {
	runID string
	owner *activeRun
	// finalizer is set after a terminal path claims the run. It prevents a
	// second callback from mutating continuity or the runlog while the first
	// terminal path is still assembling its bounded checkpoint.
	finalizer *terminalFinalization
}

type terminalFinalization struct{}

func firstRunOwnership(values []runOwnership) runOwnership {
	if len(values) == 0 {
		return runOwnership{}
	}
	return values[0]
}

func (s *Service) runLogStateFor(chatID int64, threadID int, userID int64, ownership runOwnership) (*runLogState, bool) {
	key := runLogKey(chatID, threadID, userID)
	s.runLogMu.Lock()
	defer s.runLogMu.Unlock()
	state, ok := s.runLogStates[key]
	if ownership.owner != nil && ownership.owner.runLogState != nil &&
		(ownership.finalizer != nil || state == nil || state.owner != ownership.owner) {
		state = ownership.owner.runLogState
		ok = true
	}
	if !ok || state == nil {
		return nil, false
	}
	if ownership.runID != "" && state.runID != ownership.runID {
		return nil, false
	}
	if ownership.owner != nil && state.owner != ownership.owner {
		return nil, false
	}
	return state, true
}

// runOwnershipActive reports whether a caller still owns the live run state.
// Legacy callers omit owners and retain the historical fail-open behavior;
// lifecycle goroutines pass an owner token so a superseded run cannot mutate
// continuity state after a newer run has replaced it.
func (s *Service) runOwnershipActive(chatID int64, threadID int, userID int64, owners []runOwnership) bool {
	if len(owners) == 0 {
		return true
	}
	ownership := firstRunOwnership(owners)
	if ownership.runID == "" && ownership.owner == nil {
		return true
	}
	activeRunSlotMu.Lock()
	defer activeRunSlotMu.Unlock()
	if ownership.owner != nil {
		ownership.owner.mu.RLock()
		defer ownership.owner.mu.RUnlock()
	}
	return s.runOwnershipActiveLocked(chatID, threadID, userID, ownership)
}

func (s *Service) activeRunStillOwned(chatID int64, threadID int, userID int64, owner *activeRun) bool {
	if owner == nil {
		return true
	}
	if s == nil {
		return false
	}
	activeRunSlotMu.Lock()
	defer activeRunSlotMu.Unlock()
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	return s.activeRunStillOwnedLocked(chatID, threadID, userID, owner)
}

func (s *Service) activeRunStillOwnedLocked(chatID int64, threadID int, userID int64, owner *activeRun) bool {
	if owner == nil {
		return true
	}
	if owner.superseded || s == nil {
		return false
	}
	current, ok := s.activeSessions.Load(sessionKey(chatID, threadID, userID))
	return !ok || current == owner
}

// runOwnershipActiveLocked is called while activeRunSlotMu (and, when
// present, the owner's read/write lock) is held. It validates both the live
// session token and the runlog state before a caller reaches a durable write.
// A missing runlog state remains fail-open for the historical optional-runlog
// path, but a present state must match the caller's owner and run ID.
func (s *Service) runOwnershipActiveLocked(chatID int64, threadID int, userID int64, ownership runOwnership) bool {
	if ownership.owner != nil && ownership.owner.superseded && ownership.finalizer == nil {
		return false
	}
	if ownership.owner != nil && ownership.owner.finalized && ownership.owner.finalizer != ownership.finalizer {
		return false
	}

	current, hasCurrent := s.activeSessions.Load(sessionKey(chatID, threadID, userID))
	if ownership.owner != nil && hasCurrent && current != ownership.owner {
		if ownership.finalizer == nil {
			return false
		}
	}

	state, hasState := s.runLogStateFor(chatID, threadID, userID, ownership)
	if hasState {
		state.mu.Lock()
		finalizing := state.finalizing && state.finalizer != ownership.finalizer
		state.mu.Unlock()
		if finalizing {
			return false
		}
		return state != nil
	}

	// When runlog persistence is disabled or Start failed, the active owner is
	// the only available authority. If cancellation detached the token before
	// a replacement was installed, the missing state is still fail-open for the
	// caller that owns that detached run; a newer active token is rejected above.
	return true
}

// withRunOwnership performs the ownership check and the mutation while the
// same token/slot locks are held. This is the write-boundary guarantee: a
// superseding run cannot be installed between validation and continuity,
// project-state, or runlog persistence.
func (s *Service) withRunOwnership(chatID int64, threadID int, userID int64, owners []runOwnership, fn func()) bool {
	if len(owners) == 0 {
		fn()
		return true
	}
	ownership := firstRunOwnership(owners)
	activeRunSlotMu.Lock()
	defer activeRunSlotMu.Unlock()
	if ownership.owner != nil {
		ownership.owner.mu.Lock()
		defer ownership.owner.mu.Unlock()
	}
	terminalOwnerClaimed := ownership.owner != nil && ownership.owner.superseded &&
		ownership.owner.finalizer == ownership.finalizer && ownership.finalizer != nil
	if !terminalOwnerClaimed && !s.runOwnershipActiveLocked(chatID, threadID, userID, ownership) {
		return false
	}
	fn()
	return true
}

// withCurrentRunOwnership gates shared side effects. A detached finalizer may
// still persist its original runlog, but cannot mutate a replacement run.
func (s *Service) withCurrentRunOwnership(chatID int64, threadID int, userID int64, owners []runOwnership, fn func()) bool {
	if len(owners) == 0 {
		fn()
		return true
	}
	ownership := firstRunOwnership(owners)
	activeRunSlotMu.Lock()
	defer activeRunSlotMu.Unlock()
	if ownership.owner != nil {
		ownership.owner.mu.RLock()
		defer ownership.owner.mu.RUnlock()
	}
	// Shared state never accepts the detached-finalizer exception used by
	// runlog persistence: the owner must still be the current, non-superseded
	// slot while this callback holds both locks.
	if ownership.owner != nil && !s.activeRunStillOwnedLocked(chatID, threadID, userID, ownership.owner) {
		return false
	}
	fn()
	return true
}

// claimRunFinalization reserves the one terminalization slot for a run before
// continuity or timeline side effects are performed. Without this reservation,
// an error callback and a timeout callback could both pass their ownership
// checks, patch continuity, and race to overwrite the same terminal row.
func (s *Service) claimRunFinalization(chatID int64, threadID int, userID int64, owners ...runOwnership) (runOwnership, bool) {
	ownership := firstRunOwnership(owners)
	if ownership.finalizer != nil {
		return ownership, false
	}
	claim := &terminalFinalization{}
	claimed := false
	claimFn := func() {
		if ownership.owner != nil && ownership.owner.finalized && ownership.owner.finalizer != ownership.finalizer {
			return
		}
		state, ok := s.runLogStateFor(chatID, threadID, userID, ownership)
		if !ok || state == nil {
			// The runlog is optional; ownership still protects the live path.
			if ownership.owner != nil {
				ownership.owner.finalized = true
				ownership.owner.finalizer = claim
				ownership.finalizer = claim
			}
			claimed = true
			return
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.finalizing || state.finalized {
			return
		}
		state.finalizing = true
		state.finalizer = claim
		if ownership.runID == "" {
			ownership.runID = state.runID
		}
		ownership.finalizer = claim
		if ownership.owner != nil {
			ownership.owner.finalized = true
			ownership.owner.finalizer = claim
		}
		claimed = true
	}
	if len(owners) == 0 {
		activeRunSlotMu.Lock()
		defer activeRunSlotMu.Unlock()
		claimFn()
		return ownership, claimed
	}
	// Validate the live owner before installing the finalizer. The finalizer
	// itself is then carried as the ownership token for the remainder of the
	// terminal path, while the active-run slot remains protected.
	if ownership.owner != nil {
		// Cancellation/supersession removes the owner from the active slot, but
		// its detached run state is still an authority for terminal persistence.
		activeRunSlotMu.Lock()
		ownership.owner.mu.Lock()
		if ownership.owner.superseded && ownership.owner.runLogState != nil {
			claimFn()
		} else if s.runOwnershipActiveLocked(chatID, threadID, userID, ownership) {
			claimFn()
		}
		ownership.owner.mu.Unlock()
		activeRunSlotMu.Unlock()
		return ownership, claimed
	}
	if !s.withRunOwnership(chatID, threadID, userID, owners, claimFn) {
		return ownership, false
	}
	return ownership, claimed
}

func runEventIsTerminal(phase string) bool {
	switch phase {
	case observability.PhaseBridgeResult, observability.PhaseRunCompleted,
		observability.PhaseRunFailed, observability.PhaseRunCanceled,
		observability.PhaseRunTimedOut, observability.PhaseRetryFailed:
		return true
	default:
		return false
	}
}

func runEventMemoryBytes(ev runlog.RunEvent) int {
	return len(ev.Message) + len(ev.MetadataJSON) + 64
}

func sanitizeRunEvent(ev observability.RunEvent) runlog.RunEvent {
	timestamp := int64(0)
	if !ev.Timestamp.IsZero() {
		timestamp = ev.Timestamp.Unix()
	}
	return runlog.RunEvent{
		RunID:        ev.RunID,
		Timestamp:    timestamp,
		Phase:        sanitizeForPersistence(ev.Phase, 128),
		Level:        sanitizeForPersistence(ev.Level, 32),
		Message:      sanitizeForPersistenceBytes(ev.Message, maxRunlogEventMessageBytes),
		MetadataJSON: telemetryMetadataOrEmpty(ev.MetadataJSON),
	}
}

func telemetryMetadataOrEmpty(metadata string) string {
	if metadata == "" {
		return "{}"
	}
	// The SQLite sink performs the final parse/remarshal. Keep the in-memory
	// representation bounded as well; malformed JSON is replaced rather than
	// retaining arbitrary provider text in pendingEvents.
	if len(metadata) > observability.MaxEventMetadataBytes {
		return `{"metadata_truncated":true}`
	}
	var value any
	if err := json.Unmarshal([]byte(metadata), &value); err != nil {
		return "{}"
	}
	b, err := json.Marshal(value)
	if err != nil || len(b) > observability.MaxEventMetadataBytes {
		return `{"metadata_truncated":true}`
	}
	return string(b)
}

// maxToolSummaryBytes caps the in-memory tool summary builder (tool names +
// summarized results) so a long tool stream can never grow the run state
// without limit. The summary is persisted in tool_summary and embedded in
// checkpoints, so the cap keeps both bounded.
const maxToolSummaryBytes = 8 * 1024

// truncateBytesTo cuts s to at most n bytes, stopping at a rune boundary so
// the result is always valid UTF-8.
func truncateBytesTo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && cut[len(cut)-1]&0xC0 == 0x80 {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// recordPipelineEvent persists a single pipeline event into the active run
// state. Telemetry phases (stall/steer/compaction and tool use/result) are
// subject to the per-run telemetry budget: overflow is dropped explicitly and
// countable; terminal and lifecycle events are never limited. When no state
// exists (post-completion events) the event is written immediately.
func (s *Service) recordPipelineEvent(chatID int64, threadID int, userID int64, ev observability.RunEvent, owners ...runOwnership) {
	ownership := firstRunOwnership(owners)
	s.withRunOwnership(chatID, threadID, userID, owners, func() {
		s.recordPipelineEventOwned(chatID, threadID, userID, ev, ownership)
	})
}

func (s *Service) recordPipelineEventOwned(chatID int64, threadID int, userID int64, ev observability.RunEvent, ownership runOwnership) {
	if s.runLog == nil {
		return
	}
	key := runLogKey(chatID, threadID, userID)
	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	if ownership.finalizer != nil && ownership.owner != nil && ownership.owner.runLogState != nil &&
		(!ok || state == nil || state.owner != ownership.owner) {
		state = ownership.owner.runLogState
		ok = true
	}
	hadState := state != nil
	if ok && state != nil {
		if ownership.runID != "" && state.runID != ownership.runID {
			ok = false
		}
		if ownership.owner != nil && state.owner != ownership.owner {
			ok = false
		}
		if ok && ev.RunID != "" && ev.RunID != state.runID {
			ok = false
		}
		if ok {
			ev.RunID = state.runID
			// Acquire the state lock while holding the map lock. Completion uses
			// the same order, so an event cannot append after completion copied
			// and detached the pending queue.
			state.mu.Lock()
		}
	}
	s.runLogMu.Unlock()
	if ok && state != nil {
		defer state.mu.Unlock()
		evLog := sanitizeRunEvent(ev)
		evBytes := runEventMemoryBytes(evLog)
		if isTelemetryPhase(ev.Phase) {
			// Enforce the per-run telemetry budget before accumulating. The
			// budget covers stall/steer/compaction AND tool use/result
			// telemetry, counting message bytes + metadata bytes together
			// (tool summaries and messages can dwarf metadata). A dropped
			// telemetry event is counted and logged once; terminal and
			// lifecycle events are never subject to this budget.
			if state.telemetryEvents >= maxTelemetryEventsPerRun ||
				state.telemetryBytes+evBytes > maxTelemetryBytesPerRun {
				state.telemetryDropped++
				if !state.telemetryDropLogged {
					state.telemetryDropLogged = true
					log.Printf("runlog: telemetry budget exceeded for run %s (phase=%s), dropping telemetry", state.runID, ev.Phase)
				}
				return
			}
		}

		if len(state.pendingEvents) >= maxPendingEventsPerRun ||
			state.pendingBytes+evBytes > maxPendingBytesPerRun {
			if !runEventIsTerminal(ev.Phase) {
				state.pendingDropped++
				if isTelemetryPhase(ev.Phase) {
					state.telemetryDropped++
				}
				return
			}
			// Keep the terminal event by evicting oldest bounded history. The
			// event itself is already capped and is never silently discarded.
			for len(state.pendingEvents) > 0 &&
				(len(state.pendingEvents) >= maxPendingEventsPerRun || state.pendingBytes+evBytes > maxPendingBytesPerRun) {
				state.pendingBytes -= runEventMemoryBytes(state.pendingEvents[0])
				state.pendingEvents = state.pendingEvents[1:]
				state.pendingDropped++
			}
		}
		if isTelemetryPhase(ev.Phase) {
			state.telemetryEvents++
			state.telemetryBytes += evBytes
		}
		state.pendingEvents = append(state.pendingEvents, evLog)
		state.pendingBytes += evBytes
		return
	}
	if hadState && !ok {
		return
	}
	if ownership.owner != nil && !s.activeRunStillOwnedLocked(chatID, threadID, userID, ownership.owner) {
		return
	}

	// Post-completion events (state already cleaned up) write immediately.
	runID := ev.RunID
	if runID == "" {
		runID = ownership.runID
	}
	if runID == "" {
		return
	}
	ev.RunID = runID
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := s.runLog.RecordEvent(ctx, sanitizeRunEvent(ev)); err != nil {
		slog.Warn("observability: event dropped", "run_id", runID, "phase", ev.Phase,
			"error", sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
	}
}

// bridgeTimestampSkew bounds how far a Bridge-stamped ISO timestamp may
// deviate from the local clock before it is treated as unreliable. The ISO
// timestamp remains correlation metadata; feedback/silence aggregation uses
// local time for timestamps outside this window.
const bridgeTimestampSkew = 5 * time.Minute

// bridgeEventTime returns the Bridge ISO-8601 timestamp when it is valid and
// within the explicit skew window of the local clock (sub-second correlation),
// falling back to the local wall clock otherwise. Timestamps from a skewed or
// reset Bridge clock (very old or far-future) therefore cannot produce
// negative or unbounded feedback/silence aggregates.
func bridgeEventTime(ev bridge.Event) time.Time {
	now := time.Now()
	if ev.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
			if !t.Before(now.Add(-bridgeTimestampSkew)) && !t.After(now.Add(bridgeTimestampSkew)) {
				return t
			}
		}
	}
	return now
}

func safeTelemetryTimestamp(raw string) string {
	if raw == "" {
		return ""
	}
	if _, err := time.Parse(time.RFC3339Nano, raw); err != nil {
		return ""
	}
	// Preserve valid source precision for correlation/debugging while keeping
	// the value bounded and sanitized before persistence.
	return sanitizeForPersistence(raw, 64)
}

func safeTelemetryID(raw string) string {
	clean := sanitizeForPersistence(raw, 128)
	if clean == "" {
		return ""
	}
	for _, r := range clean {
		switch {
		case r == '-' || r == '_' || r == '.' || r == ':':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return ""
		}
	}
	return clean
}

func appendAssistantText(builder *strings.Builder, delta string) {
	if builder.Len() >= maxAssistantBytesPerRun || delta == "" {
		return
	}
	builder.WriteString(truncateBytesTo(delta, maxAssistantBytesPerRun-builder.Len()))
}

// trackRunFeedback updates the per-run feedback/silence aggregation.
// Only surface-updating events (assistant, tool_use, tool_result, result,
// error) call this; lifecycle/telemetry events never do.
// `at` is clamped into [startedAt, now] so out-of-order or skewed event
// times can never produce negative gaps or inflate trailing silence; the
// aggregation therefore stays bounded by the local run time.
func (s *Service) trackRunFeedback(chatID int64, threadID int, userID int64, at time.Time, owners ...runOwnership) {
	ownership := firstRunOwnership(owners)
	s.withRunOwnership(chatID, threadID, userID, owners, func() {
		state, ok := s.runLogStateFor(chatID, threadID, userID, ownership)
		if !ok || state == nil {
			return
		}

		state.mu.Lock()
		defer state.mu.Unlock()
		now := time.Now()
		if state.startedAt.IsZero() {
			state.startedAt = now
		}
		if at.IsZero() {
			at = now
		}
		if at.Before(state.startedAt) {
			at = state.startedAt // no negative first-feedback gap
		}
		if at.After(now) {
			at = now // no future feedback (clamped to local run time)
		}
		if state.firstFeedbackAt.IsZero() {
			state.firstFeedbackAt = at
			gap := at.Sub(state.startedAt).Milliseconds()
			if gap > state.maxSilenceMs {
				state.maxSilenceMs = gap
			}
		} else if at.After(state.lastFeedbackAt) {
			gap := at.Sub(state.lastFeedbackAt).Milliseconds()
			if gap > state.maxSilenceMs {
				state.maxSilenceMs = gap
			}
		}
		if at.After(state.lastFeedbackAt) {
			state.lastFeedbackAt = at
		}
	})
}

// countBridgeTelemetry increments the per-run stall/steer counters. Telemetry
// is not productive feedback; it only feeds the diagnostic aggregates. The
// counters saturate at maxTelemetryEventsPerRun so a runaway bridge telemetry
// stream can never grow the counters without limit.
func (s *Service) countBridgeTelemetry(chatID int64, threadID int, userID int64, eventType string, owners ...runOwnership) {
	ownership := firstRunOwnership(owners)
	s.withRunOwnership(chatID, threadID, userID, owners, func() {
		state, ok := s.runLogStateFor(chatID, threadID, userID, ownership)
		if !ok || state == nil {
			return
		}

		state.mu.Lock()
		defer state.mu.Unlock()
		switch eventType {
		case "stall":
			if state.stallCount < maxTelemetryEventsPerRun {
				state.stallCount++
			}
		case "steer":
			if state.steerCount < maxTelemetryEventsPerRun {
				state.steerCount++
			}
		}
	})
}

// normalizeSeverity bounds bridge_health severity to the telemetry enum
// (warning|urgent|unknown); anything else degrades to unknown so arbitrary
// severity text can never be persisted.
func normalizeSeverity(s string) string {
	switch s {
	case "warning", "urgent", "unknown":
		return s
	default:
		return "unknown"
	}
}

// normalizeToolLabel reduces untrusted SDK tool names to the approved durable
// label set. Unknown extension/provider names are represented by "tool" rather
// than becoming arbitrary runlog text. The same safe label is also used by the
// live progress and loop-monitoring paths so raw provider names do not cross
// the telemetry boundary through an adjacent field.
func normalizeToolLabel(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read":
		return "Read"
	case "write":
		return "Write"
	case "edit":
		return "Edit"
	case "bash":
		return "Bash"
	case "grep":
		return "Grep"
	case "find", "glob":
		return "Find"
	case "ls", "list":
		return "LS"
	case "web_search":
		return "WebSearch"
	case "web_search_premium":
		return "WebSearchPremium"
	case "mcp":
		return "mcp"
	case "code_search":
		return "code_search"
	case "fetch_content":
		return "fetch_content"
	case "get_search_content":
		return "get_search_content"
	default:
		return "tool"
	}
}

// normalizeCompactionReason bounds the compaction reason to the explicit enum
// (manual|automatic|unknown); raw SDK reason text degrades to unknown and is
// never persisted.
func normalizeCompactionReason(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "manual", "user":
		return "manual"
	case "automatic", "auto", "system", "context", "context_window", "threshold", "overflow":
		return "automatic"
	case "unknown":
		return "unknown"
	default:
		return "unknown"
	}
}

// compactionEffectiveness classifies the compaction outcome from the token
// delta (tokens_after - tokens_before): a negative delta means the context
// was REDUCED — effective; a zero delta is neutral; a positive delta means
// the context grew — regressive. An unmeasured delta is classified as
// unknown by the caller.
func compactionEffectiveness(deltaTokens int) string {
	switch {
	case deltaTokens < 0:
		return "effective"
	case deltaTokens == 0:
		return "neutral"
	default:
		return "regressive"
	}
}

// normalizeErrorClass bounds the compaction error_class to the static
// allowlist (compaction_error only). Anything else degrades to "unknown" so
// arbitrary text — including CR/LF or other control characters — can never
// be persisted.
func normalizeErrorClass(s string) string {
	if s == "compaction_error" {
		return s
	}
	return "unknown"
}

func boundedTelemetryMetric(value int, allowNegative bool) int {
	if !allowNegative && value < 0 {
		return 0
	}
	if value > maxTelemetryMetricValue {
		return maxTelemetryMetricValue
	}
	if value < -maxTelemetryMetricValue {
		return -maxTelemetryMetricValue
	}
	return value
}

// telemetryMetadata marshals a small telemetry metadata map into redacted
// JSON. The map is built by callers with contract-bounded keys only; values
// that exceed observability.MaxEventMetadataBytes degrade to a valid
// fallback object so the timeline never stores broken JSON and the pipeline
// never blocks on metadata.
func telemetryMetadata(meta map[string]any) string {
	if len(meta) == 0 {
		return "{}"
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return `{"metadata_truncated":true}`
	}
	if len(b) > observability.MaxEventMetadataBytes {
		return `{"metadata_truncated":true}`
	}
	return string(b)
}

// isTelemetryPhase reports whether a phase belongs to the bounded
// bridge_health telemetry stream (stall/steer/compaction and tool
// use/result), which is subject to the per-run telemetry budget.
func isTelemetryPhase(phase string) bool {
	switch phase {
	case observability.PhaseBridgeStall, observability.PhaseBridgeSteer,
		observability.PhaseBridgeCompaction, observability.PhaseBridgeToolUse,
		observability.PhaseBridgeToolResult:
		return true
	default:
		return false
	}
}

// updateRunLogOutboundMessage updates the outbound Telegram message_id on a
// completed runlog entry. Called after the final reply has been sent.
func (s *Service) updateRunLogOutboundMessage(chatID int64, threadID int, userID int64, runID string, outboundMessageID int64, owners ...runOwnership) {
	if s.runLog == nil || runID == "" || outboundMessageID == 0 {
		return
	}
	ownership := firstRunOwnership(owners)
	s.withRunOwnership(chatID, threadID, userID, owners, func() {
		// The terminal row has already removed its in-memory state. Validate the
		// still-current token and captured run ID against the active owner before
		// updating outbound terminal metadata.
		if ownership.owner != nil {
			if ownership.owner.superseded || (ownership.runID != "" && ownership.runID != runID) {
				return
			}
		}
		updateCtx, updateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer updateCancel()
		if err := s.runLog.Update(updateCtx, runlog.RunUpdate{
			RunID:             runID,
			OutboundMessageID: &outboundMessageID,
		}); err != nil {
			log.Printf("runlog: failed to update outbound_message_id for %s: %s", runID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
	})
}

// recordToolUse appends a tool name to the in-memory tool summary for a run.
// The summary is capped at maxToolSummaryBytes before growing so a runaway
// tool stream can never grow the run state without limit.
func (s *Service) recordToolUse(chatID int64, threadID int, userID int64, toolName string, owners ...runOwnership) {
	if s.runLog == nil || toolName == "" {
		return
	}
	toolName = normalizeToolLabel(toolName)
	if toolName == "" {
		return
	}
	ownership := firstRunOwnership(owners)
	s.withRunOwnership(chatID, threadID, userID, owners, func() {
		state, ok := s.runLogStateFor(chatID, threadID, userID, ownership)
		if !ok || state == nil {
			return
		}

		state.mu.Lock()
		defer state.mu.Unlock()
		if toolName == "Write" || toolName == "Edit" {
			state.writeToolsUsed = true
		}
		snippet := toolName
		if state.summary.Len() > 0 {
			snippet = ", " + snippet
		}
		avail := maxToolSummaryBytes - state.summary.Len()
		if avail <= 0 {
			return
		}
		state.summary.WriteString(truncateBytesTo(snippet, avail))
		state.summaryCount++
	})
}

// savePartialAssistant stores the current partial assistant response text
// into the run log state. Used for checkpoint on timeout so the resume has
// context of what the model was saying.
func (s *Service) savePartialAssistant(chatID int64, threadID int, userID int64, text string, owners ...runOwnership) {
	ownership := firstRunOwnership(owners)
	s.withRunOwnership(chatID, threadID, userID, owners, func() {
		state, ok := s.runLogStateFor(chatID, threadID, userID, ownership)
		if !ok || state == nil {
			return
		}
		state.mu.Lock()
		state.partialAssistant = redactAndTruncate(text, maxCheckpointRunes)
		state.mu.Unlock()
	})
}

// recordToolResult appends a summarized tool result to the tool summary.
// The summary is capped at maxToolSummaryBytes before growing so a long
// tool stream can never grow the run state without limit.
func (s *Service) recordToolResult(chatID int64, threadID int, userID int64, summary string, owners ...runOwnership) {
	if s.runLog == nil {
		return
	}
	ownership := firstRunOwnership(owners)
	s.withRunOwnership(chatID, threadID, userID, owners, func() {
		state, ok := s.runLogStateFor(chatID, threadID, userID, ownership)
		if !ok || state == nil {
			return
		}

		state.mu.Lock()
		defer state.mu.Unlock()
		// Tool result excerpts are live-only data. Durable runlog state stores a
		// static class so provider output, file contents, and command output can
		// never become long-lived telemetry.
		snippet := " → [tool_result]"
		avail := maxToolSummaryBytes - state.summary.Len()
		if avail <= 0 {
			return
		}
		state.summary.WriteString(truncateBytesTo(snippet, avail))
	})
}

// completeRunLog marks the runlog entry with a terminal status and checkpoint.
// All persisted data is redacted before storage to prevent credential leakage.
func (s *Service) completeRunLog(chatID int64, threadID int, userID int64, status runlog.RunStatus, checkpoint, errMsg string, owners ...runOwnership) {
	ownership := firstRunOwnership(owners)
	claimed := ownership.finalizer != nil
	if !claimed {
		ownership, claimed = s.claimRunFinalization(chatID, threadID, userID, owners...)
	}
	if !claimed {
		return
	}
	s.completeRunLogOwned(chatID, threadID, userID, status, checkpoint, errMsg, ownership)
}

func (s *Service) completeRunLogOwned(chatID int64, threadID int, userID int64, status runlog.RunStatus, checkpoint, errMsg string, ownership runOwnership) {
	key := runLogKey(chatID, threadID, userID)

	// Validate and detach the state under the same map -> state ownership
	// boundary used by event writers. Do not retain active-run locks while the
	// external runlog store performs its transaction: finalization has already
	// claimed the owner, and the detached state prevents late callbacks from
	// reaching this row.
	activeRunSlotMu.Lock()
	if ownership.owner != nil {
		ownership.owner.mu.Lock()
	}
	if !s.runOwnershipActiveLocked(chatID, threadID, userID, ownership) {
		if ownership.owner != nil {
			ownership.owner.mu.Unlock()
		}
		activeRunSlotMu.Unlock()
		return
	}
	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	if ok && state != nil {
		if ownership.runID != "" && state.runID != ownership.runID {
			ok = false
		}
		if ownership.owner != nil && state.owner != ownership.owner {
			ok = false
		}
	}
	if ok {
		delete(s.runLogStates, key)
		// Match recordPipelineEvent's map -> state lock order. This closes the
		// completion race where an event captured the pointer just before the
		// map entry was removed.
		state.mu.Lock()
	}
	if (!ok || state == nil) && ownership.owner != nil && ownership.owner.runLogState != nil {
		// The session-key slot now belongs to a replacement. Terminalization
		// must use the original owner's detached capability and must not delete
		// or lock the replacement state.
		state = ownership.owner.runLogState
		ok = state.runID == ownership.runID || ownership.runID == ""
		if ok {
			state.mu.Lock()
		}
	}
	s.runLogMu.Unlock()
	if ownership.owner != nil {
		ownership.owner.mu.Unlock()
	}
	activeRunSlotMu.Unlock()

	if !ok || state == nil || s.runLog == nil {
		return
	}

	// Capture final tool summary, pending events, and partial assistant text.
	summary := state.summary.String()
	partialAssistant := state.partialAssistant
	pendingEvents := append([]runlog.RunEvent(nil), state.pendingEvents...)
	state.pendingEvents = nil
	state.pendingBytes = 0
	telemetryDropped := state.telemetryDropped
	pendingDropped := state.pendingDropped

	// Long-session aggregates, computed at the terminal boundary:
	// - first_feedback_ms: run start -> first surface-updating event (0 if none)
	// - max_silence_ms: largest gap between feedback events, including the
	//   trailing gap when the run ends without a terminal bridge event
	//   (cancel/timeout/process death) — the silent-window fixture case.
	// All gaps are clamped to the local run time (non-negative, bounded).
	firstFeedbackMs := int64(0)
	maxSilenceMs := state.maxSilenceMs
	now := time.Now()
	if !state.startedAt.IsZero() && !state.firstFeedbackAt.IsZero() {
		if gap := state.firstFeedbackAt.Sub(state.startedAt).Milliseconds(); gap > 0 {
			firstFeedbackMs = gap
		}
	}
	if !state.startedAt.IsZero() {
		if state.lastFeedbackAt.IsZero() {
			// No productive event at all: the silence spans the whole run.
			if gap := now.Sub(state.startedAt).Milliseconds(); gap > maxSilenceMs {
				maxSilenceMs = gap
			}
		} else if !state.lastFeedbackAt.After(now) {
			if gap := now.Sub(state.lastFeedbackAt).Milliseconds(); gap > maxSilenceMs {
				maxSilenceMs = gap
			}
		}
	}
	stallCount := state.stallCount
	steerCount := state.steerCount
	state.finalized = true
	state.mu.Unlock()

	if telemetryDropped > 0 {
		log.Printf("runlog: telemetry budget for run %s: %d telemetry event(s) dropped", state.runID, telemetryDropped)
	}
	if pendingDropped > 0 {
		log.Printf("runlog: pending event budget for run %s: %d event(s) dropped", state.runID, pendingDropped)
	}

	// Defensive redaction: assistant output may contain credentials.
	summary = sanitizeForPersistence(summary, maxToolSummaryBytes)
	checkpoint = sanitizeForPersistence(checkpoint, maxRunlogErrorRunes*2)
	errMsg = sanitizeForPersistence(errMsg, maxRunlogErrorRunes)
	partialAssistant = sanitizeForPersistence(partialAssistant, maxCheckpointRunes)

	// Build checkpoint with partial assistant text if available
	if checkpoint == "" {
		checkpoint = buildCheckpoint(status, "", summary, errMsg, partialAssistant)
	} else {
		checkpoint = buildCheckpoint(status, checkpoint, summary, errMsg, partialAssistant)
	}

	state.wg.Wait()

	completeCtx, completeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer completeCancel()

	// Persist the terminal status and the long-session aggregates in the same
	// operation when the store supports it (SQLite); other stores fall back to
	// Complete + Update. A failure degrades diagnostics, never the run result.
	agg := runlog.CompletionAggregates{
		FirstFeedbackMs: firstFeedbackMs,
		MaxSilenceMs:    maxSilenceMs,
		StallCount:      stallCount,
		SteerCount:      steerCount,
	}
	if ca, ok := s.runLog.(runlog.AtomicCompletionStore); ok {
		if err := ca.CompleteWithEvents(completeCtx, state.runID, status, checkpoint, errMsg, summary, agg, pendingEvents); err != nil {
			log.Printf("runlog: failed to complete %s (status=%s): %s", state.runID, status, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		return
	}
	if ca, ok := s.runLog.(runlog.AggregateCompletionStore); ok {
		// This capability commits terminal aggregates, but not the pending event
		// batch. Keep the split explicit for generic test/non-SQLite stores.
		if len(pendingEvents) > 0 {
			if err := s.runLog.RecordEvents(completeCtx, pendingEvents); err != nil {
				log.Printf("runlog: non-atomic event flush failed for %s (%d events): %s", state.runID, len(pendingEvents), sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
				return
			}
		}
		if err := ca.CompleteWithAggregates(completeCtx, state.runID, status, checkpoint, errMsg, summary, agg); err != nil {
			log.Printf("runlog: non-atomic terminal completion failed for %s (status=%s): %s", state.runID, status, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		return
	}
	// Generic test stores do not expose a transaction. Preserve their legacy
	// best-effort behavior explicitly; a failure in either half is reported but
	// never presented as an atomic SQLite commit.
	if len(pendingEvents) > 0 {
		if err := s.runLog.RecordEvents(completeCtx, pendingEvents); err != nil {
			log.Printf("runlog: non-atomic event flush failed for %s (%d events): %s", state.runID, len(pendingEvents), sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
	}
	if err := s.runLog.Complete(completeCtx, state.runID, status, checkpoint, errMsg, summary); err != nil {
		log.Printf("runlog: non-atomic terminal completion failed for %s (status=%s): %s", state.runID, status, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
	}
	if err := s.runLog.Update(completeCtx, runlog.RunUpdate{
		RunID:           state.runID,
		FirstFeedbackMs: &firstFeedbackMs,
		MaxSilenceMs:    &maxSilenceMs,
		StallCount:      &stallCount,
		SteerCount:      &steerCount,
	}); err != nil {
		log.Printf("runlog: failed to persist aggregates for %s (status=%s): %s", state.runID, status, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
	}
}
