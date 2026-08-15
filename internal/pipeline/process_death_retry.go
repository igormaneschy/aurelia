package pipeline

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
)

var retryRequestCounter atomic.Uint64

// retryRequestID returns a fresh protocol-safe ID for the second bridge
// request. It must not reuse the dead request ID because the bridge rejects
// duplicate active IDs and cancellation targets the live request.
func retryRequestID(original string) string {
	seq := retryRequestCounter.Add(1)
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", original, seq)))
	return fmt.Sprintf("retry-%x", digest[:12])
}

// executeAsync runs the full bridge request lifecycle in a goroutine: typing
// indicator, progress, timeout safety nets, the bridge execute query and the
// bridge event stream. The runlog was started by processRunWithCancel BEFORE
// lifecycle evaluation so proactive compaction telemetry shares the run; this
// method only records the bridge request start when that state exists
// (runLogStarted) and never starts a second run. On process death the runlog
// stays open through retryAfterProcessDeath so every retry path completes
// exactly one runlog.
func (s *Service) executeAsync(parentCtx context.Context, chatID int64, threadID int, messageID int, req bridge.Request, userText string, userID int64, isPrivateChat bool, warningThreshold int, criticalThreshold int, runLogStarted bool, ownership runOwnership) {
	stopTyping := s.output.StartTyping(chatID, threadID)
	defer stopTyping()

	progress := s.output.NewProgress(chatID, threadID)
	defer progress.Delete()

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	timeoutTracker := newRunTimeoutTracker()

	// steerDuringExecution sends a steer command to the active bridge session.
	// This injects a message into the model's context without canceling execution.
	steerDuringExecution := func(msg string) {
		steerCtx, steerCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer steerCancel()
		_, err := s.bridge.ExecuteSync(steerCtx, bridge.Request{
			Command: "steer",
			Prompt:  msg,
			Options: bridge.RequestOptions{
				ChatID:   chatID,
				ThreadID: threadID,
				UserID:   userID,
			},
		})
		if err != nil {
			log.Printf("pipeline: steer failed during execution chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
	}
	toolTracker := newToolCallTracker(chatID, threadID, s.output, steerDuringExecution, warningThreshold, criticalThreshold)
	loopDetect := newLoopDetector(chatID, threadID, s.output, steerDuringExecution)
	s.SetActiveToolState(chatID, threadID, userID, toolTracker, loopDetect)
	defer s.ClearActiveToolState(chatID, threadID, userID)

	// Max timeout goroutine — safety net after 30min
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline: panic in maxTimeout goroutine: %v", r)
			}
		}()
		timer := time.NewTimer(bridgeExecutionTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			timeoutTracker.mark(timeoutOriginMaxExecution)
			log.Printf("pipeline: max execution timeout (%s) reached chat=%d thread=%d user=%d",
				bridgeExecutionTimeout, chatID, threadID, userID)
			cancel()
		case <-ctx.Done():
		}
	}()

	// Timeout warning goroutine — warns user 5min before hard timeout.
	// Does NOT cancel — only notifies. The max timeout goroutine above
	// handles the actual 30-min cancellation.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline: panic in timeoutWarning goroutine: %v", r)
			}
		}()
		timer := time.NewTimer(bridgeExecutionTimeout - timeoutWarningLead)
		defer timer.Stop()
		select {
		case <-timer.C:
			// Send Telegram warning to user
			userMsg := fmt.Sprintf(
				"⏰ Aproximando do limite de tempo de %s. "+
					"Vou concluir o que tenho e apresentar um resumo parcial.",
				bridgeExecutionTimeout)
			if _, err := s.output.SendText(chatID, threadID, userMsg); err != nil {
				log.Printf("pipeline: SendText(timeout warning) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
			}
			// Steer the model to wrap up — this injects into the active model context
			steerDuringExecution(fmt.Sprintf(
				"Você está próximo do limite de tempo de %s. "+
					"Conclua imediatamente o que está fazendo e apresente um resumo parcial "+
					"do que conseguiu até agora.",
				bridgeExecutionTimeout))
		case <-ctx.Done():
		}
	}()

	cancelDone := s.cancelBridgeOnContextDone(ctx, req.RequestID)
	defer cancelDone()

	// The runlog was started by processRunWithCancel BEFORE lifecycle
	// evaluation so proactive compaction telemetry shares the run; this
	// method only records the bridge request start when that state exists
	// (runLogStarted). No second run is ever started here.
	if runLogStarted {
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewEvent("",
			observability.PhaseBridgeRequestStarted,
			fmt.Sprintf("request_id=%s provider=%s model=%s", req.RequestID, req.Options.Provider, req.Options.Model)), ownership)
	}

	var ch <-chan bridge.Event
	ch, usedFallback, err := s.executeQuery(ctx, req, func(msg string) {
		if _, sendErr := s.output.SendText(chatID, threadID, msg); sendErr != nil {
			log.Printf("pipeline: SendText(fallback status) failed for chat=%d: %s", chatID, sanitizeForPersistence(sendErr.Error(), maxRunlogErrorRunes))
		}
	})
	if usedFallback && runLogStarted {
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewWarnEvent("",
			observability.PhaseFallbackResult,
			fmt.Sprintf("provider=%s model=%s", req.Options.Provider, req.Options.Model)), ownership)
	}

	if ch != nil {
		ch = idleTimeoutWrapper(ctx, ch, s.getIdleTimeout(), cancel, func() {
			timeoutTracker.mark(timeoutOriginIdleBridge)
		})
	}

	var outcome Outcome
	if err != nil {
		if errors.Is(err, errProcessDeath) {
			// Record process death event; recovery below will handle it.
			if runLogStarted {
				s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent("",
					observability.PhaseBridgeProcessDeath, "bridge process exited during Execute"), ownership)
			}
		} else if errors.Is(err, context.Canceled) {
			if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID, userID, isPrivateChat, timeoutTracker, ownership); handled {
				s.output.ConfirmMessage(chatID, messageID)
				return
			}
			log.Printf("pipeline: run canceled by user chat=%d thread=%d user=%d", chatID, threadID, userID)
			if runLogStarted {
				const reason = "user_cancel"
				s.patchContinuityFailure(chatID, threadID, "canceled", reason, userID, isPrivateChat, ownership)
				runID := s.getRunID(chatID, threadID, userID, ownership)
				s.recordPipelineEvent(chatID, threadID, userID, observability.NewEvent(runID, observability.PhaseRunCanceled, reason), ownership)
				s.completeRunLog(chatID, threadID, userID, runlog.RunCanceled, "", reason, ownership)
			}
			return
		} else {
			log.Printf("Bridge execute error: %s", sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
			if runLogStarted {
				s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent("",
					observability.PhaseBridgeExecuteError, "provider_error"), ownership)
				s.patchContinuityFailure(chatID, threadID, "failed", "provider_error", userID, isPrivateChat, ownership)
				runID := s.getRunID(chatID, threadID, userID, ownership)
				s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent(runID, observability.PhaseRunFailed, "provider_error"), ownership)
				s.completeRunLog(chatID, threadID, userID, runlog.RunFailed, "", "provider_error", ownership)
			}
			s.output.ConfirmMessage(chatID, messageID)
			return
		}
	} else {
		toolUseSignal := make(chan struct{}, 16)
		go heartbeatMonitor(ctx.Done(), toolUseSignal, toolTracker, progress)
		outcome = s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText, toolUseSignal, userID, isPrivateChat, toolTracker, loopDetect, ownership)
		if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID, userID, isPrivateChat, timeoutTracker, ownership); handled {
			s.output.ConfirmMessage(chatID, messageID)
			return
		}
		if outcome == OutcomeSuccess {
			s.bridgeFailures.reset()
			return
		}
		if outcome != OutcomeProcessDeath {
			return
		}
	}

	s.bridgeFailures.record()
	log.Printf("bridge: process died mid-request, retrying for chat=%d thread=%d", chatID, threadID)

	// The runlog stays open through the retry: bridge_process_death, retry
	// telemetry and retry feedback all land in the same run, and the terminal
	// row reflects the retry outcome instead of a premature failure.
	s.retryAfterProcessDeath(parentCtx, ctx, cancel, chatID, threadID, messageID, req, userText, userID, isPrivateChat, progress, runLogStarted, toolTracker, loopDetect, timeoutTracker, s.bridge.Execute, ownership)
}

// retryAfterProcessDeath re-drives the request after the bridge process died.
// The runlog entry is NOT completed before the retry: every terminal retry
// path (success, Execute error, cooldown, cancel/timeout, new process death)
// completes exactly one runlog, and events are never dropped for lack of a
// RunID while the state is alive.
func (s *Service) retryAfterProcessDeath(
	parentCtx context.Context,
	ctx context.Context,
	cancel context.CancelFunc,
	chatID int64,
	threadID int,
	messageID int,
	req bridge.Request,
	userText string,
	userID int64,
	isPrivateChat bool,
	progress ProgressReporter,
	runLogStarted bool,
	toolTracker *toolCallTracker,
	loopDetect *loopDetector,
	timeoutTracker *runTimeoutTracker,
	execute func(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error),
	owners ...runOwnership,
) {
	ownership := firstRunOwnership(owners)
	if len(owners) > 0 && !s.runOwnershipActive(chatID, threadID, userID, owners) {
		if runLogStarted {
			s.cancelStartedRunLog(pipelineInput{chatID: chatID, threadID: threadID, userID: userID}, ownership)
		}
		return
	}
	// Mark process death in session store for lifecycle evaluation
	if s.sessions != nil {
		if !s.withRunOwnership(chatID, threadID, userID, owners, func() {
			s.sessions.MarkProcessDeath(chatID, threadID, userID)
		}) {
			if runLogStarted {
				s.cancelStartedRunLog(pipelineInput{chatID: chatID, threadID: threadID, userID: userID}, ownership)
			}
			return
		}
	}

	retryRunRequestID := ""
	if runLogStarted {
		const retryReason = "process_death"
		s.patchContinuityFailure(chatID, threadID, "failed", retryReason, userID, isPrivateChat, ownership)
		retryRunRequestID = retryRequestID(req.RequestID)
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewWarnEvent("",
			observability.PhaseRetryStarted, fmt.Sprintf("origin=process_death request_id=%s", retryRunRequestID)), ownership)
	}

	// Cancellation/timeout is terminal even when the bridge failure would put
	// the failure tracker into cooldown. Check this before any retry gate.
	if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID, userID, isPrivateChat, timeoutTracker, ownership); handled {
		s.output.ConfirmMessage(chatID, messageID)
		return
	}

	if s.bridgeFailures.inCooldown() {
		remaining := s.bridgeFailures.cooldownRemaining()
		log.Printf("bridge: in cooldown, skipping retry for chat=%d", chatID)
		if runLogStarted {
			runID := s.getRunID(chatID, threadID, userID, ownership)
			s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent(runID,
				observability.PhaseRetryFailed, "process_death"), ownership)
			s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent(runID,
				observability.PhaseRunFailed, "process_death"), ownership)
			s.completeRunLog(chatID, threadID, userID, runlog.RunFailed, "", "process_death", ownership)
		}
		if err := s.output.SendError(chatID, threadID, bridgeCooldownMessage(remaining)); err != nil {
			log.Printf("pipeline: SendError(cooldown) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		s.output.ConfirmMessage(chatID, messageID)
		return
	}

	reconnectMsg, sErr := s.output.SendText(chatID, threadID, "⚡ Reconectando...")
	if sErr != nil {
		log.Printf("pipeline: SendText(reconnect) failed for chat=%d: %s", chatID, sanitizeForPersistence(sErr.Error(), maxRunlogErrorRunes))
	}

	retryReq := req
	retryReq.Options.Continue = false
	if retryRunRequestID == "" {
		retryRunRequestID = retryRequestID(req.RequestID)
	}
	retryReq.RequestID = retryRunRequestID
	if s.sessions != nil {
		if sid := s.sessions.GetSession(chatID, threadID, userID); sid != "" {
			retryReq.Options.Resume = sid
			log.Printf("bridge: retry with resume file=%s", filepath.Base(sid))
		}
	}

	// The cold-resume retry relies on the PI session file surviving the crash,
	// but a death mid-compaction/write can corrupt it. Inject the same
	// recovered-context snapshot the fallback path uses so the retry still has
	// the interrupted task's context when the session file is unusable.
	if chatID != 0 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("process_death_retry: panic in continuitySnapshot: %v", r)
				}
			}()
			snapshot := s.continuitySnapshot(ctx, chatID, threadID, userID)
			if snapshot != "" {
				snapshotBlock := "\n\n## Previous Session Context (recovered)\n\n" +
					"The following is recovered context from the previous session that was interrupted. " +
					"Use it to continue the task.\n\n" +
					"<recovered_context_untrusted>\n" + snapshot + "\n</recovered_context_untrusted>"

				if retryReq.Options.SystemPrompt != "" {
					retryReq.Options.SystemPrompt += snapshotBlock
				} else {
					retryReq.Options.SystemPrompt = snapshotBlock
				}
				log.Printf("process_death_retry: injected continuity snapshot into retry prompt (chat=%d thread=%d)", chatID, threadID)
			}
		}()
	}

	// Intentionally uses s.bridge.Execute (not s.executeQuery) here because
	// process-death recovery has its own retry/fallback discipline — the bridge
	// process was restarted by s.bridge's readLoop death callback, so a single
	// retry suffices. Using executeQuery's retry+fallback would add latency and
	// risk confusing the user with duplicate "reconnecting" messages.
	retryCancelDone := s.cancelBridgeOnContextDone(ctx, retryReq.RequestID)
	defer retryCancelDone()
	ch, err := execute(ctx, retryReq)
	s.output.DeleteMessage(reconnectMsg)
	if err != nil {
		log.Printf("bridge: retry failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		// Preserve cancel/timeout terminality: a user cancel or run timeout
		// observed during the retry Execute must stay RunCanceled/RunTimedOut
		// (with timeout_origin/checkpoint preserved), never degrade to
		// RunFailed.
		if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID, userID, isPrivateChat, timeoutTracker, ownership); handled {
			s.output.ConfirmMessage(chatID, messageID)
			return
		}
		s.patchContinuitySessionCold(chatID, threadID, "process_death", userID, isPrivateChat, ownership)
		if runLogStarted {
			runID := s.getRunID(chatID, threadID, userID, ownership)
			s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent("",
				observability.PhaseRetryFailed, "process_death"), ownership)
			s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent(runID,
				observability.PhaseRunFailed, "process_death"), ownership)
			s.completeRunLog(chatID, threadID, userID, runlog.RunFailed, "", "process_death", ownership)
		}
		if err := s.output.SendError(chatID, threadID, bridgeRetryFailedMessage); err != nil {
			log.Printf("pipeline: SendError(retry failed) for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		s.output.ConfirmMessage(chatID, messageID)
		return
	}

	if ch != nil {
		ch = idleTimeoutWrapper(ctx, ch, s.getIdleTimeout(), cancel, func() {
			timeoutTracker.mark(timeoutOriginIdleBridge)
		})
	}

	toolUseSignal := make(chan struct{}, 16)
	go heartbeatMonitor(ctx.Done(), toolUseSignal, toolTracker, progress)
	outcome := s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText, toolUseSignal, userID, isPrivateChat, toolTracker, loopDetect, ownership)
	if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID, userID, isPrivateChat, timeoutTracker, ownership); handled {
		s.output.ConfirmMessage(chatID, messageID)
		return
	}
	s.handleRetryOutcome(chatID, threadID, messageID, outcome, userID, isPrivateChat, ownership)
}

// timeoutDetails returns the timeout origin and elapsed time from the first
// tracker that has a snapshot; without any tracker it reports unknown.
func timeoutDetails(trackers ...*runTimeoutTracker) (string, time.Duration) {
	if len(trackers) == 0 {
		return timeoutOriginUnknown, 0
	}
	return trackers[0].snapshot()
}

// handleRetryOutcome completes the runlog after a retry event stream ends.
// The success path was already completed by handleResultEvent on the retry
// stream (only the failure tracker resets here); a second process death is
// terminal and completes exactly one runlog as RunFailed.
func (s *Service) handleRetryOutcome(chatID int64, threadID int, messageID int, outcome Outcome, userID int64, isPrivateChat bool, owners ...runOwnership) {
	ownership := firstRunOwnership(owners)
	if len(owners) > 0 && !s.runOwnershipActive(chatID, threadID, userID, owners) {
		s.cancelStartedRunLog(pipelineInput{chatID: chatID, threadID: threadID, userID: userID}, ownership)
		return
	}
	switch outcome {
	case OutcomeSuccess:
		// The run was already completed by handleResultEvent on the retry
		// stream; only the failure tracker resets here.
		s.bridgeFailures.reset()
	case OutcomeProcessDeath:
		s.bridgeFailures.record()
		if s.sessions != nil {
			if !s.withRunOwnership(chatID, threadID, userID, owners, func() {
				s.sessions.MarkProcessDeath(chatID, threadID, userID)
			}) {
				s.cancelStartedRunLog(pipelineInput{chatID: chatID, threadID: threadID, userID: userID}, ownership)
				return
			}
		}
		runID := s.getRunID(chatID, threadID, userID, ownership)
		s.patchContinuitySessionCold(chatID, threadID, "process_death", userID, isPrivateChat, ownership)
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent("",
			observability.PhaseRetryFailed, "process_death"), ownership)
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent(runID,
			observability.PhaseRunFailed, "process_death"), ownership)
		// Terminal retry path: complete exactly one runlog (RunFailed).
		s.completeRunLog(chatID, threadID, userID, runlog.RunFailed, "", "process_death", ownership)
		if err := s.output.SendError(chatID, threadID, bridgeRetryFailedMessage); err != nil {
			log.Printf("pipeline: SendError(retry outcome) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		s.output.ConfirmMessage(chatID, messageID)
	}
}
