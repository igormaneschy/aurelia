package pipeline

import (
	"context"
	"fmt"
	"log"
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
func (s *Service) applyLifecycle(req *bridge.Request, chatID int64, threadID int, userID int64) lifecycleDecisionResult {
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

	// Gather health signals from session store (nil-safe for tests)
	var signals session.HealthSignals
	if s.sessions != nil {
		signals = s.sessions.GetHealthSignals(chatID, threadID, userID)
	}

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
		// Proactive compaction before query
		result, err := s.compactSession(context.Background(), chatID, threadID, userID, req.Options)
		if err != nil {
			log.Printf("lifecycle: compaction failed for chat=%d: %v — falling back to cold resume", chatID, err)
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

		log.Printf("lifecycle: compaction succeeded for chat=%d tokens_before=%d", chatID, result.TokensBefore)

		// After compaction, the session is healthy enough to continue
		// The session file might have changed; update request options
		if result.SessionFile != "" {
			req.Options.Resume = result.SessionFile
		}
		req.Options.Continue = true
		return lifecycleDecisionResult{
			Decision:    dec,
			ModifiedReq: req,
		}

	case session.ActionRotate:
		// For now, force cold resume. T10 will implement full rotation.
		log.Printf("lifecycle: rotate needed for chat=%d — falling back to cold resume until rotation is implemented", chatID)
		req.Options.Continue = false
		return lifecycleDecisionResult{
			Decision: session.Decision{
				State:  session.HealthCold,
				Action: session.ActionColdResume,
				Reason: "rotate not yet implemented, using cold resume",
			},
			ModifiedReq: req,
		}
	}

	return lifecycleDecisionResult{Decision: dec}
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
