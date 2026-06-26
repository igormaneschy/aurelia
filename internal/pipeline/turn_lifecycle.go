package pipeline

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/engine"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
)

// summaryCounter tracks the number of successful turns since the last
// LLM-generated summary for a conversation. Stored in-memory only;
// on daemon restart it resets to 0, triggering a fresh summary on the
// next turn from existing continuity state.
type summaryCounter struct {
	mu     sync.Mutex
	counts map[continuity.ConversationKey]int
}

// increment increments the turn counter and returns the new count and whether
// we should generate a summary (turns >= interval).
func (c *summaryCounter) increment(key continuity.ConversationKey, interval int) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[key]++
	turns := c.counts[key]
	return turns, turns >= interval
}

// reset resets the turn counter for a conversation after summarization.
func (c *summaryCounter) reset(key continuity.ConversationKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.counts, key)
}

// runeCap returns the first n runes of s, preserving valid UTF-8.
func runeCap(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	var count int
	for i := range s {
		if count >= n {
			return s[:i]
		}
		count++
	}
	return s
}

// generateProgressiveSummary calls the LLM to merge the previous summary
// with the latest exchange. Returns an updated summary string, or empty
// string if summarization failed (caller falls back to raw text).
func (s *Service) generateProgressiveSummary(ctx context.Context, previousSummary, userText, assistantText string) string {
	if s.bridge == nil || s.config == nil {
		return ""
	}

	// Cap inputs to keep prompt tokens manageable
	cappedPrev := runeCap(previousSummary, 2000)
	cappedUser := runeCap(userText, 2000)
	cappedAssistant := runeCap(assistantText, 2000)

	prompt := fmt.Sprintf(`Merge the previous summary with the latest user message and assistant response into ONE updated summary that captures all important context, decisions, and open items.

Previous summary: %s
Latest user message: %s
Latest assistant response: %s

Updated summary (max 900 chars, no preamble):`,
		cappedPrev, cappedUser, cappedAssistant)

	sumCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req := engine.Request{
		Prompt:       prompt,
		SystemPrompt: "You are a conversation summarizer. Output ONLY the requested summary, no preamble, no explanation. Maximum 900 characters, in the same language as the conversation (Portuguese).",
	}
	s.applyConfiguredModelOptions(&req)

	ch, err := s.engine.Query(sumCtx, req)
	if err != nil {
		log.Printf("summary: failed to generate progressive summary: %v", err)
		return ""
	}

	var content string
	for ev := range ch {
		if ev.RawType == "result" || ev.Type == engine.EventTypeDone {
			content = ev.ContentText()
			break
		}
		if ev.RawType == "error" || ev.Type == engine.EventTypeError {
			log.Printf("summary: engine error: %v", ev.Err)
			return ""
		}
	}
	if content == "" {
		log.Printf("summary: no result from engine")
		return ""
	}

	// Redact BEFORE truncation (per redaction-before-truncation.md) so
	// secrets straddling the boundary aren't sliced in half.
	summary := redactSecrets(strings.TrimSpace(content))
	return runeCap(summary, continuity.MaxAssistantSummary)
}

func (s *Service) afterSuccessfulTurn(chatID int64, threadID int, userText string, finalText string, runID string, userID int64, isPrivateChat bool) {
	// Clear failure/suspect state after successful completion
	if s.sessions != nil {
		s.sessions.ClearFailureState(chatID, threadID, userID)
	}

	key := continuity.ConversationKeyFor(chatID, threadID, userID)

	// Progressive summarization: on non-summary turns, re-read the existing
	// summary from continuity so LastAssistantSummary accumulates across
	// intervals instead of being overwritten by the latest raw text.
	finalSummary := finalText
	if s.summaryInterval > 0 {
		turns, shouldSummarize := s.summaryCounter.increment(key, s.summaryInterval)
		if shouldSummarize {
			log.Printf("summary: generating progressive summary for chat=%d thread=%d after %d turns", chatID, threadID, turns)

			if s.continuity != nil {
				readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer readCancel()

				state, err := s.continuity.Get(readCtx, chatID, threadID, userID)
				if err == nil && state != nil && state.LastAssistantSummary != "" {
					// Use background context (not readCtx) so generateProgressiveSummary
					// can derive its own 10s timeout — a 5s parent would truncate it.
					merged := s.generateProgressiveSummary(context.Background(), state.LastAssistantSummary, userText, finalText)
					if merged != "" {
						finalSummary = merged
						s.summaryCounter.reset(key)
						log.Printf("summary: progressive summary generated (%d chars) for chat=%d thread=%d", len(merged), chatID, threadID)
					}
				}
			}
		} else if s.continuity != nil {
			// Preserve accumulated summary across non-summary turns
			// so subsequent intervals build on the previous summary.
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			state, err := s.continuity.Get(ctx, chatID, threadID, userID)
			if err == nil && state != nil && state.LastAssistantSummary != "" {
				finalSummary = state.LastAssistantSummary
			}
		}
	}

	// Patch continuity with the (potentially summarized) assistant text
	s.patchContinuityAfterSuccess(chatID, threadID, userText, finalSummary, runID, userID)

	if s.dreamer == nil {
		return
	}
	s.dreamer.AfterTurn(userID)
	cwd := s.effectiveCwdForContext(nil, chatID, threadID, userID, isPrivateChat)
	sessionFile := ""
	if s.sessions != nil {
		sessionFile = s.sessions.GetSession(chatID, threadID, userID)
	}
	s.nudgeBuffer.AddTurn(chatID, threadID, userID, userText, finalText)
	if toolSummary := s.getRunToolSummary(chatID, threadID, userID); toolSummary != "" {
		s.nudgeBuffer.AddToolEvent(chatID, threadID, userID, toolSummary)
	}
	s.dreamer.AfterTurnNudge(chatID, threadID, userID, cwd, sessionFile, s.nudgeBuffer)
	s.InvalidateMemoryDirs(chatID, threadID, userID, cwd)
}

// --- Continuity lifecycle helpers ---

// getRunID returns the current runID from runLogStates, or empty string.
// Must be called before completeRunLog, which deletes the state.
func (s *Service) getRunID(chatID int64, threadID int, userID int64) string {
	key := runLogKey(chatID, threadID, userID)
	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()
	if ok && state != nil {
		return state.runID
	}
	return ""
}

// patchContinuityAfterSuccess writes successful turn state into the continuity store.
// runID must be captured before completeRunLog (which cleans up runLogStates).
func (s *Service) patchContinuityAfterSuccess(chatID int64, threadID int, userText string, assistantText string, runID string, userID int64) {
	if s.continuity == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cwd := s.effectiveCwd(nil, chatID, threadID)
	now := time.Now()
	runStatus := "completed"
	sessionCold := false

	sessionID := ""
	if s.sessions != nil {
		sessionID = s.sessions.GetSession(chatID, threadID, userID)
	}

	err := s.continuity.Patch(ctx, continuity.ConversationKeyFor(chatID, threadID, userID), continuity.StatePatch{
		CWD:                  &cwd,
		LastUserIntent:       &userText,
		LastAssistantSummary: &assistantText,
		LastRunID:            &runID,
		LastRunStatus:        &runStatus,
		SessionID:            &sessionID,
		SessionCold:          &sessionCold,
		UpdatedAt:            now,
	})
	if err != nil {
		log.Printf("continuity: failed to patch after success chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// patchContinuityFailure writes failure/timeout/error state into the continuity store.
// Must be called BEFORE completeRunLog, since that cleans up the run log state.
func (s *Service) patchContinuityFailure(chatID int64, threadID int, status string, errMsg string, userID int64) {
	if s.continuity == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	now := time.Now()
	sessionCold := true

	// Capture latest checkpoint, tools, and partial assistant text from runLogState
	checkpoint := ""
	tools := ""
	runID := ""
	assistantText := ""
	key := runLogKey(chatID, threadID, userID)
	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	if ok && state != nil {
		runID = state.runID
		state.mu.Lock()
		tools = state.summary.String()
		assistantText = state.partialAssistant
		state.mu.Unlock()
	}
	s.runLogMu.Unlock()

	if tools != "" {
		tools = redactSecrets(tools)
	}
	if assistantText != "" {
		assistantText = redactSecrets(assistantText)
	}

	// Build checkpoint from available info, including partial assistant text
	cp := buildCheckpoint(runlog.RunStatus(status), "", tools, errMsg, assistantText)
	checkpoint = redactSecrets(cp)

	cwd := s.effectiveCwd(nil, chatID, threadID)

	sid := ""
	if s.sessions != nil {
		sid = s.sessions.GetSession(chatID, threadID, userID)
	}

	err := s.continuity.Patch(ctx, continuity.ConversationKeyFor(chatID, threadID, userID), continuity.StatePatch{
		CWD:            &cwd,
		LastRunID:      &runID,
		LastRunStatus:  &status,
		LastCheckpoint: &checkpoint,
		LastTools:      &tools,
		SessionID:      &sid,
		SessionCold:    &sessionCold,
		ResetReason:    &errMsg,
		UpdatedAt:      now,
	})
	if err != nil {
		log.Printf("continuity: failed to patch failure chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// patchContinuitySessionCold marks the session as cold with a reset reason.
func (s *Service) patchContinuitySessionCold(chatID int64, threadID int, reason string, userID int64) {
	if s.continuity == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cold := true
	err := s.continuity.Patch(ctx, continuity.ConversationKeyFor(chatID, threadID, userID), continuity.StatePatch{
		SessionCold: &cold,
		ResetReason: &reason,
		UpdatedAt:   time.Now(),
	})
	if err != nil {
		log.Printf("continuity: failed to patch session cold chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// patchContinuitySessionID updates the session ID in continuity state.
func (s *Service) patchContinuitySessionID(chatID int64, threadID int, sessionID string, userID int64) {
	if s.continuity == nil || sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := s.continuity.Patch(ctx, continuity.ConversationKeyFor(chatID, threadID, userID), continuity.StatePatch{
		SessionID: &sessionID,
		UpdatedAt: time.Now(),
	})
	if err != nil {
		log.Printf("continuity: failed to patch session ID chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// continuitySnapshot captures the current continuity state for fallback recovery.
// Returns a compact, redacted summary string, or empty if unavailable.
// All field values are redacted for defense-in-depth, escaped to prevent
// delimiter injection, and the total is capped at MaxContinuityBlockChars.
func (s *Service) continuitySnapshot(ctx context.Context, chatID int64, threadID int, userID int64) string {
	if s.continuity == nil {
		return ""
	}
	getCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	state, err := s.continuity.Get(getCtx, chatID, threadID, userID)
	if err != nil || state == nil {
		return ""
	}

	var parts []string
	if state.LastUserIntent != "" {
		parts = append(parts, "Last user intent: "+redactSecrets(state.LastUserIntent))
	}
	if state.LastAssistantSummary != "" {
		parts = append(parts, "Last assistant summary: "+redactSecrets(state.LastAssistantSummary))
	}
	if state.LastCheckpoint != "" {
		parts = append(parts, "Last checkpoint: "+redactSecrets(state.LastCheckpoint))
	}
	if state.LastRunStatus != "" {
		parts = append(parts, "Last run status: "+redactSecrets(state.LastRunStatus))
	}
	if state.LastTools != "" {
		parts = append(parts, "Tools used: "+redactSecrets(state.LastTools))
	}
	if state.CWD != "" {
		parts = append(parts, "Working directory: "+redactSecrets(state.CWD))
	}

	if len(parts) == 0 {
		return ""
	}

	body := strings.Join(parts, "\n")

	// Cap the total block size (rune-aware to avoid splitting multi-byte chars).
	if utf8.RuneCountInString(body) > continuity.MaxContinuityBlockChars {
		for utf8.RuneCountInString(body) > continuity.MaxContinuityBlockChars {
			body = body[:len(body)-1]
		}
		// Walk back to valid rune boundary.
		for len(body) > 0 && body[len(body)-1]&0xC0 == 0x80 {
			body = body[:len(body)-1]
		}
	}

	// Escape delimiter-sensitive characters to prevent injection of
	// closing </fallback_context_untrusted> tags.
	body = continuity.EscapeUntrusted(body)

	return body
}

// isBillingError detects PI SDK billing/credit errors so we can:
// - surface clear user-facing messages (bridge)
// - avoid contaminating session suspect counters (pipeline)
// - suppress misleading lifecycle UX messages (session_lifecycle)
func isBillingError(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "insufficient balance") ||
		strings.Contains(lower, "insufficient credits") ||
		(strings.Contains(lower, "401") && strings.Contains(lower, "billing"))
}

func classifyBridgeErrorOutcome(message string) (string, runlog.RunStatus, string) {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "query timeout") {
		return "timed_out", runlog.RunTimedOut, timeoutOriginBridgeQuery
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") || strings.Contains(lower, "deadline exceeded") {
		return "timed_out", runlog.RunTimedOut, timeoutOriginProviderPI
	}
	return "failed", runlog.RunFailed, message
}

func (s *Service) handleErrorEvent(chatID int64, threadID int, messageID int, ev engine.Event, userID int64) Outcome {
	errMsg := ev.Message
	if errMsg == "" {
		errMsg = ev.Content
	}
	if errMsg == "" {
		errMsg = "Erro desconhecido no processador."
	}
	redacted := redactSecrets(errMsg)
	log.Printf("Bridge error: %s", redacted)
	status, runStatus, reason := classifyBridgeErrorOutcome(redacted)
	s.patchContinuityFailure(chatID, threadID, status, reason, userID)
	s.completeRunLog(chatID, threadID, userID, runStatus, "", reason)
	s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent("",
		observability.PhaseRunFailed, reason))

	// Mark failure for timeout/provider errors so lifecycle manager marks session suspect.
	// Skip billing errors — they're provider issues, not session failures.
	if s.sessions != nil && status == "timed_out" && !isBillingError(redacted) {
		s.sessions.MarkFailure(chatID, threadID, userID, reason)
	}

	if err := s.output.SendError(chatID, threadID, redacted); err != nil {
		log.Printf("Failed to send error to chat %d: %v", chatID, redactSecrets(err.Error()))
	}
	s.output.ConfirmMessage(chatID, messageID)
	return OutcomeLLMError
}
