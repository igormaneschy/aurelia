package pipeline

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/session"
)

// lifecycleDecisionResult captures the outcome of a lifecycle evaluation
// and any modifications to apply to the bridge request.
type lifecycleDecisionResult struct {
	Decision      session.Decision
	ModifiedReq   *bridge.Request // non-nil when the request was modified
	SkipExecution bool            // true when the query should not proceed (critical failure)
	ErrorMessage  string          // user-facing message when SkipExecution
}

// applyLifecycle evaluates session health and applies the lifecycle decision
// to the bridge request. It modifies req in-place when action is taken.
//
// Flow:
//  1. Gather signals from session store
//  2. Evaluate lifecycle against policy
//  3. Apply action:
//     - continue:   keep Continue=true (no change)
//     - cold_resume: force Continue=false
//     - compact:     call compact-session, then update session file
//     - rotate:      for now, force cold resume (rotation T10)
//  4. Record lifecycle decision as runlog event
//
// Returns a result with the modified request or skip signal.
func (s *Service) applyLifecycle(ctx context.Context, req *bridge.Request, chatID int64, threadID int, userID int64) lifecycleDecisionResult {
	if s.config == nil {
		return lifecycleDecisionResult{
			Decision: session.Decision{
				State:  session.HealthHealthy,
				Action: session.ActionContinue,
				Reason: "lifecycle unavailable: missing config",
			},
		}
	}

	// Get lifecycle policy from config
	lc := s.config.SessionLifecycle
	if !lc.Enabled {
		return lifecycleDecisionResult{
			Decision: session.Decision{
				State:  session.HealthHealthy,
				Action: session.ActionContinue,
				Reason: "lifecycle disabled",
			},
		}
	}

	policy := lc.LifecyclePolicy()

	// Gather health signals from session store (nil-safe for tests), then enrich
	// with PI stats when the bridge can inspect the persisted session file.
	var signals session.HealthSignals
	if s.sessions != nil {
		signals = s.sessions.GetHealthSignals(chatID, threadID, userID)
	}
	signals = s.enrichLifecycleSignals(ctx, req, signals)

	// Evaluate
	dec := session.EvaluateLifecycle(signals, policy)

	log.Printf("lifecycle: chat=%d thread=%d user=%d state=%s action=%s reason=%q",
		chatID, threadID, userID, dec.State, dec.Action, dec.Reason)

	// Record lifecycle decision as runlog event
	s.recordLifecycleDecision(chatID, threadID, dec)

	// Apply action
	switch dec.Action {
	case session.ActionContinue:
		// No change — buildBridgeRequest already sets Continue=true for active sessions
		return lifecycleDecisionResult{Decision: dec}

	case session.ActionColdResume:
		// Force Continue=false — session will be resumed without continuing
		req.Options.Continue = false
		return lifecycleDecisionResult{
			Decision:    dec,
			ModifiedReq: req,
		}

	case session.ActionCompact:
		// Notify user about proactive compaction
		if s.output != nil {
			s.output.SendText(chatID, threadID, "🧠 Compactando histórico longo para continuar com segurança...")
		}

		result, err := s.compactSession(ctx, chatID, threadID, userID, req.Options)
		if err != nil {
			log.Printf("lifecycle: compaction failed for chat=%d: %v — falling back to cold resume", chatID, err)
			if s.output != nil {
				s.output.SendText(chatID, threadID, "⚠️ Compactação automática falhou. Retomando sessão com segurança.")
			}
			if s.sessions != nil {
				s.sessions.MarkFailure(chatID, threadID, userID, "compaction failed")
			}
			req.Options.Continue = false
			return lifecycleDecisionResult{
				Decision: session.Decision{
					State:  session.HealthSuspect,
					Action: session.ActionColdResume,
					Reason: fmt.Sprintf("compaction failed: %v", err),
				},
				ModifiedReq: req,
			}
		}

		if result == nil || !result.Success {
			log.Printf("lifecycle: compaction returned unsuccessful result for chat=%d", chatID)
			if s.sessions != nil {
				s.sessions.MarkFailure(chatID, threadID, userID, "compaction returned unsuccessful result")
			}
			req.Options.Continue = false
			return lifecycleDecisionResult{
				Decision: session.Decision{
					State:  session.HealthSuspect,
					Action: session.ActionColdResume,
					Reason: "compaction returned unsuccessful result",
				},
				ModifiedReq: req,
			}
		}

		log.Printf("lifecycle: compaction succeeded for chat=%d tokens_before=%d", chatID, result.TokensBefore)

		// After compaction, the session is healthy enough to continue
		if result.SessionFile != "" {
			req.Options.Resume = result.SessionFile
		}
		req.Options.Continue = true
		return lifecycleDecisionResult{
			Decision:    dec,
			ModifiedReq: req,
		}

	case session.ActionRotate:
		// Notify user about session rotation
		if s.output != nil {
			s.output.SendText(chatID, threadID, "🔄 Histórico muito longo. Criando nova sessão com resumo do contexto anterior...")
		}

		result, err := s.rotateSession(ctx, chatID, threadID, userID, req.Options)
		if err != nil {
			log.Printf("lifecycle: rotation failed for chat=%d: %v — falling back to cold resume", chatID, err)
			if s.output != nil {
				s.output.SendText(chatID, threadID, "⚠️ Rotação automática falhou. Retomando sessão anterior com segurança.")
			}
			if s.sessions != nil {
				s.sessions.MarkFailure(chatID, threadID, userID, "rotation failed")
			}
			req.Options.Continue = false
			return lifecycleDecisionResult{
				Decision: session.Decision{
					State:  session.HealthSuspect,
					Action: session.ActionColdResume,
					Reason: fmt.Sprintf("rotation failed: %v", err),
				},
				ModifiedReq: req,
			}
		}

		if result == nil || !result.Success || result.NewSessionFile == "" {
			log.Printf("lifecycle: rotation returned invalid result for chat=%d", chatID)
			if s.sessions != nil {
				s.sessions.MarkFailure(chatID, threadID, userID, "rotation returned invalid result")
			}
			req.Options.Continue = false
			return lifecycleDecisionResult{
				Decision: session.Decision{
					State:  session.HealthSuspect,
					Action: session.ActionColdResume,
					Reason: "rotation returned invalid result",
				},
				ModifiedReq: req,
			}
		}

		log.Printf("lifecycle: rotation succeeded for chat=%d old=%s new=%s",
			chatID, filepath.Base(result.OldSessionFile), filepath.Base(result.NewSessionFile))

		if s.output != nil {
			s.output.SendText(chatID, threadID, "✅ Nova sessão criada com resumo do contexto anterior. Continuando com segurança.")
		}

		// Update session store with new session file
		if s.sessions != nil {
			s.sessions.SetSession(chatID, threadID, userID, result.NewSessionFile)
			s.sessions.ClearFailureState(chatID, threadID, userID)
		}

		// Resume the new session without continuing (cold start)
		req.Options.Resume = result.NewSessionFile
		req.Options.Continue = false
		return lifecycleDecisionResult{
			Decision:    dec,
			ModifiedReq: req,
		}
	}

	return lifecycleDecisionResult{Decision: dec}
}

// enrichLifecycleSignals reads PI session stats when possible and merges them
// into store-derived signals. Stats failures are non-fatal: lifecycle still
// protects known cold/suspect sessions from the store metadata.
func (s *Service) enrichLifecycleSignals(ctx context.Context, req *bridge.Request, signals session.HealthSignals) session.HealthSignals {
	if s == nil || s.bridge == nil || req == nil || req.Options.Resume == "" {
		return signals
	}

	statsCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	stats, err := s.bridge.GetSessionStats(statsCtx, req.Options)
	if err != nil {
		log.Printf("lifecycle: get-session-stats failed for session=%s: %s", filepath.Base(req.Options.Resume), redactSecrets(err.Error()))
		return signals
	}
	if stats == nil {
		return signals
	}

	signals.InputTokens = stats.InputTokens
	signals.OutputTokens = stats.OutputTokens
	signals.TotalMessages = stats.TotalMessages
	signals.AssistantMessages = stats.AssistantMessages
	signals.ToolResults = stats.ToolResults
	return signals
}

// rotateSession runs the rotate-session bridge command with a timeout.
func (s *Service) rotateSession(ctx context.Context, chatID int64, threadID int, userID int64, opts bridge.RequestOptions) (*bridge.RotateSessionResult, error) {
	if s.bridge == nil {
		return nil, fmt.Errorf("bridge not available")
	}

	rotateCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Ensure resume path is set
	if opts.Resume == "" && s.sessions != nil {
		opts.Resume = s.sessions.GetSession(chatID, threadID, userID)
	}
	opts.ChatID = chatID
	opts.ThreadID = threadID
	opts.UserID = userID

	return s.bridge.RotateSession(rotateCtx, opts)
}

// compactSession runs the compact-session bridge command with a timeout.
func (s *Service) compactSession(ctx context.Context, chatID int64, threadID int, userID int64, opts bridge.RequestOptions) (*bridge.CompactSessionResult, error) {
	if s.bridge == nil {
		return nil, fmt.Errorf("bridge not available")
	}

	compactCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Ensure resume path is set
	if opts.Resume == "" && s.sessions != nil {
		opts.Resume = s.sessions.GetSession(chatID, threadID, userID)
	}
	opts.ChatID = chatID
	opts.ThreadID = threadID
	opts.UserID = userID

	return s.bridge.CompactSession(compactCtx, opts)
}

// recordLifecycleDecision records the lifecycle decision as a runlog event.
func (s *Service) recordLifecycleDecision(chatID int64, threadID int, dec session.Decision) {
	if s.runLog == nil {
		return
	}
	s.recordPipelineEvent(chatID, threadID, observability.NewEvent("",
		observability.PhaseSessionLifecycle,
		fmt.Sprintf("state=%s action=%s reason=%s", dec.State, dec.Action, redactSecrets(dec.Reason))))
}

// getLifecyclePolicy returns the active lifecycle policy from config.
func (s *Service) getLifecyclePolicy() session.LifecyclePolicy {
	if s.config == nil {
		return session.DefaultLifecyclePolicy()
	}
	return s.config.SessionLifecycle.LifecyclePolicy()
}

// getIdleTimeout returns the configured idle timeout, falling back to defaultIdleTimeout
// when config is not available or lifecycle is disabled.
func (s *Service) getIdleTimeout() time.Duration {
	if s.config == nil || !s.config.SessionLifecycle.Enabled {
		return defaultIdleTimeout
	}
	min := s.config.SessionLifecycle.IdleTimeoutMinutes
	if min <= 0 {
		return defaultIdleTimeout
	}
	return time.Duration(min) * time.Minute
}
