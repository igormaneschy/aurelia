package pipeline

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/runlog"
)

// processRunWithCancel drives one user request end-to-end inside the reserved
// context: profile resolution, project preflight, system prompt, runlog
// start, lifecycle evaluation and the async bridge execution.
//
// Compaction lifecycle: the runlog is started BEFORE lifecycle evaluation so
// proactive compaction (and its delta/duration telemetry) lands in the same
// run/request as the prompt that follows — compactSession records its events
// through this state. executeAsync reuses the same state instead of starting
// a second run. If Start fails the run stays fail-open (compaction still
// runs, but its events are not persisted).
func (s *Service) processRunWithCancel(input pipelineInput, run *activeRun, reservedCtx context.Context, reservedCancel context.CancelFunc) {
	key := sessionKey(input.chatID, input.threadID, input.userID)
	defer func() {
		activeRunSlotMu.Lock()
		s.activeSessions.CompareAndDelete(key, run)
		markRunSuperseded(run)
		activeRunSlotMu.Unlock()
	}()
	defer reservedCancel()
	_ = reservedCtx

	// Resolve effective Prompt Profile: @name > active mode > general.
	activeMode := s.userActiveMode(input.userID)
	profile, userText, resolveErr := s.resolveEffectiveProfile(input.text, activeMode, input.userID, s.isOwnerUser(input.userID))
	if resolveErr != nil {
		if pfErr, ok := resolveErr.(*profiles.ErrProfileNotFound); ok {
			log.Printf("pipeline: unknown @profile chat=%d name=%s", input.chatID, pfErr.Name)
			if err := s.output.SendError(input.chatID, input.threadID,
				fmt.Sprintf("Perfil @%s não encontrado. Use /agents para ver os perfis disponíveis.", pfErr.Name)); err != nil {
				log.Printf("pipeline: SendError(unknown profile) failed for chat=%d: %v", input.chatID, redactSecrets(err.Error()))
			}
		} else if deniedErr, ok := resolveErr.(*profiles.ErrProfileNotAllowed); ok {
			log.Printf("pipeline: forbidden @profile chat=%d name=%s user=%d", input.chatID, deniedErr.Name, input.userID)
			if err := s.output.SendError(input.chatID, input.threadID,
				fmt.Sprintf("Perfil @%s não encontrado ou indisponível. Use /agents para ver os perfis disponíveis.", deniedErr.Name)); err != nil {
				log.Printf("pipeline: SendError(forbidden profile) failed for chat=%d: %v", input.chatID, redactSecrets(err.Error()))
			}
		} else {
			log.Printf("pipeline: profile resolution error chat=%d: %v", input.chatID, resolveErr)
		}
		s.output.ConfirmMessage(input.chatID, input.messageID)
		return
	}

	if s.checkProjectPreflight(input, profile, userText) {
		return
	}

	systemPrompt, err := s.buildSystemPrompt(userText, profile, input.chatID, input.messageID, input.threadID, input.userID, input.isPrivateChat)
	if err != nil {
		log.Printf("Failed to build system prompt: %s", redactSecrets(err.Error()))
		if err := s.output.SendError(input.chatID, input.threadID, "Falha ao montar o prompt de sistema."); err != nil {
			log.Printf("pipeline: SendError(system prompt) failed for chat=%d: %v", input.chatID, redactSecrets(err.Error()))
		}
		s.output.ConfirmMessage(input.chatID, input.messageID)
		return
	}

	req := s.buildBridgeRequest(userText, systemPrompt, profile, input.chatID, input.threadID, input.userID, input.isPrivateChat)
	req.RequestID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	req.Options.Images = input.images
	s.applyVisionFallback(reservedCtx, &req, input.images)

	// Start the runlog BEFORE lifecycle evaluation so proactive compaction
	// (and its delta/duration telemetry) lands in the same run/request as
	// the prompt that follows — compactSession records its events through
	// this state. executeAsync reuses the same state instead of starting a
	// second run. If Start fails the run stays fail-open (compaction still
	// runs, but its events are not persisted).
	runLogStarted := s.startRunLog(startRunLogParams{
		ChatID:     input.chatID,
		ThreadID:   input.threadID,
		RequestID:  req.RequestID,
		MessageID:  input.messageID,
		CWD:        s.effectiveCwd(nil, input.chatID, input.threadID),
		Prompt:     userText,
		UserID:     input.userID,
		AgentName:  requestAgentName(req),
		Provider:   req.Options.Provider,
		Model:      req.Options.Model,
		Profile:    requestProfile(req),
		EntryPoint: s.entryPoint,
		Owner:      run,
	})

	ownership := runOwnership{owner: run}
	if !s.runOwnershipActive(input.chatID, input.threadID, input.userID, []runOwnership{ownership}) {
		if runLogStarted {
			s.cancelStartedRunLog(input, ownership)
		}
		return
	}
	if !s.withRunOwnership(input.chatID, input.threadID, input.userID, []runOwnership{ownership}, func() {
		if state, ok := s.runLogStateFor(input.chatID, input.threadID, input.userID, ownership); ok {
			ownership.runID = state.runID
		}
	}) {
		if runLogStarted {
			s.cancelStartedRunLog(input, ownership)
		}
		return
	}
	if lcResult := s.applyLifecycle(reservedCtx, &req, input.chatID, input.threadID, input.userID, ownership); lcResult.SkipExecution {
		log.Printf("lifecycle: execution skipped for chat=%d: %s", input.chatID, lcResult.ErrorMessage)
		if runLogStarted {
			s.recordPipelineEvent(input.chatID, input.threadID, input.userID,
				observability.NewWarnEvent("", observability.PhaseSessionLifecycle, "execution skipped by lifecycle"), ownership)
			s.completeRunLog(input.chatID, input.threadID, input.userID, runlog.RunFailed, "", "execution skipped by lifecycle", ownership)
		}
		if lcResult.ErrorMessage != "" {
			if err := s.output.SendError(input.chatID, input.threadID, lcResult.ErrorMessage); err != nil {
				log.Printf("pipeline: SendError(lifecycle) failed for chat=%d: %v", input.chatID, redactSecrets(err.Error()))
			}
		}
		s.output.ConfirmMessage(input.chatID, input.messageID)
		return
	}

	warnThreshold := toolCallWarningThreshold
	critThreshold := toolCallCriticalThreshold
	if profile != nil && profile.ToolBudget > 0 {
		warnThreshold = profile.ToolBudget
		critThreshold = profile.ToolBudget * 5 / 2
	}

	s.executeAsync(reservedCtx, input.chatID, input.threadID, input.messageID, req, userText, input.userID, input.isPrivateChat, warnThreshold, critThreshold, runLogStarted, ownership)
}

// cancelStartedRunLog closes a run whose owner was lost after Start. It uses
// the detached state capability, never the replacement in the session slot.
func (s *Service) cancelStartedRunLog(input pipelineInput, ownership runOwnership) {
	if ownership.owner != nil && ownership.owner.runLogState != nil {
		ownership.runID = ownership.owner.runLogState.runID
	}
	claimed, ok := s.claimRunFinalization(input.chatID, input.threadID, input.userID, ownership)
	if !ok {
		return
	}
	s.cancelClaimedRunLog(input, claimed)
}

func (s *Service) cancelClaimedRunLog(input pipelineInput, ownership runOwnership) {
	runID := s.getRunID(input.chatID, input.threadID, input.userID, ownership)
	if runID == "" {
		runID = ownership.runID
	}
	s.recordPipelineEvent(input.chatID, input.threadID, input.userID,
		observability.NewEvent(runID, observability.PhaseRunCanceled, "owner_lost_after_start"), ownership)
	s.completeRunLog(input.chatID, input.threadID, input.userID, runlog.RunCanceled, "", "owner_lost_after_start", ownership)
}

// requestAgentName returns the security agent name from a bridge request.
func requestAgentName(req bridge.Request) string {
	if req.Options.Security == nil {
		return ""
	}
	return req.Options.Security.AgentName
}

// requestProfile returns the security profile from a bridge request.
func requestProfile(req bridge.Request) string {
	if req.Options.Security == nil {
		return ""
	}
	return req.Options.Security.Profile
}
