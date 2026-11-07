package pipeline

import (
	"log"
	"strings"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
)

// afterSuccessfulTurn clears the per-session failure state after a successful
// run. Durable context is owned by ai-memory; only execution state remains.
func (s *Service) afterSuccessfulTurn(chatID int64, threadID int, userText string, finalText string, runID string, userID int64, isPrivateChat bool, owners ...runOwnership) {
	if len(owners) > 0 && !s.activeRunStillOwned(chatID, threadID, userID, firstRunOwnership(owners).owner) {
		return
	}
	if !s.withRunOwnership(chatID, threadID, userID, owners, func() {
		if s.sessions != nil {
			s.sessions.ClearFailureState(chatID, threadID, userID)
		}
	}) {
		return
	}
}

// getRunID returns the current runID from runLogStates, or empty string.
// Must be called before completeRunLog, which deletes the state.
func (s *Service) getRunID(chatID int64, threadID int, userID int64, owners ...runOwnership) string {
	var runID string
	s.withRunOwnership(chatID, threadID, userID, owners, func() {
		state, ok := s.runLogStateFor(chatID, threadID, userID, firstRunOwnership(owners))
		if ok && state != nil {
			runID = state.runID
		}
	})
	return runID
}

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
	return "failed", runlog.RunFailed, "provider_error"
}

func (s *Service) handleErrorEvent(chatID int64, threadID int, messageID int, ev bridge.Event, userID int64, isPrivateChat bool, owners ...runOwnership) Outcome {
	ownership := firstRunOwnership(owners)
	if ownership.finalizer == nil {
		var claimed bool
		ownership, claimed = s.claimRunFinalization(chatID, threadID, userID, owners...)
		if !claimed {
			return OutcomeCanceled
		}
	}
	// Terminal bridge event: counts as surface-updating feedback.
	s.trackRunFeedback(chatID, threadID, userID, bridgeEventTime(ev), ownership)
	errMsg := ev.Message
	if errMsg == "" {
		errMsg = ev.Content
	}
	if errMsg == "" {
		errMsg = "Erro desconhecido no processador."
	}
	redacted := sanitizeForPersistence(errMsg, maxRunlogErrorRunes)
	log.Printf("Bridge error: %s", redacted)
	status, runStatus, reason := classifyBridgeErrorOutcome(redacted)
	durableReason := reason
	if runStatus == runlog.RunFailed {
		durableReason = "provider_error"
	}
	// Capture the run ID BEFORE completeRunLog removes the runlog state so
	// the run_failed event emitted afterwards is correlated to the same run.
	runID := s.getRunID(chatID, threadID, userID, ownership)
	s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent(runID,
		observability.PhaseRunFailed, durableReason), ownership)
	s.completeRunLog(chatID, threadID, userID, runStatus, "", durableReason, ownership)

	// Mark failure for timeout/provider errors so lifecycle manager marks session suspect.
	// Skip billing errors — they're provider issues, not session failures.
	if s.sessions != nil && status == "timed_out" && !isBillingError(redacted) {
		if !s.withCurrentRunOwnership(chatID, threadID, userID, []runOwnership{ownership}, func() {
			s.sessions.MarkFailure(chatID, threadID, userID, reason)
		}) {
			return OutcomeCanceled
		}
	}

	if s.activeRunStillOwned(chatID, threadID, userID, ownership.owner) {
		if err := s.output.SendError(chatID, threadID, redacted); err != nil {
			log.Printf("Failed to send error to chat %d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
	}
	s.output.ConfirmMessage(chatID, messageID)
	return OutcomeLLMError
}
