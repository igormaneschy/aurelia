// Package observability provides a local-first observability layer for Aurelia.
//
// It defines the correlation model (RunContext), structured logging setup,
// phase event constants, and the Recorder interface for persisting run
// metadata and event timelines.
//
// Stable field names (lowercase snake_case) shared across logs, SQLite,
// and JSON output:
//
//	run_id, parent_run_id, request_id, entrypoint,
//	chat_id, thread_id, user_id, message_id,
//	cwd, agent, provider, model, capability_profile,
//	phase, status, error_class, timeout_origin,
//	duration_ms, input_tokens, output_tokens, cost_usd,
//	used_fallback, session_file
package observability

import "time"

// EntryPoint enumerates the known run origins.
const (
	EntryPointTelegram      = "telegram"
	EntryPointCron          = "cron"
	EntryPointOrchestration = "orchestration"
	EntryPointNudge         = "nudge"
	EntryPointCLI           = "cli"
)

// Standard phase names for the run_events timeline.
// Each phase records a single point-in-time event during a run lifecycle.
const (
	PhaseTelegramReceived     = "telegram_received"
	PhaseUserGateOK           = "user_gate_ok"
	PhaseAgentRouted          = "agent_routed"
	PhasePromptBuilt          = "prompt_built"
	PhaseBridgeRequestStarted = "bridge_request_started"
	PhaseBridgeSystem         = "bridge_system"
	PhaseBridgeToolUse        = "bridge_tool_use"
	PhaseBridgeToolResult     = "bridge_tool_result"
	PhaseBridgeResult         = "bridge_result"
	PhaseReplyStarted         = "reply_started"
	PhaseReplySent            = "reply_sent"
	PhaseContinuityPatched    = "continuity_patched"
	PhaseSessionLifecycle     = "session_lifecycle"
	PhaseDreamNudgeScheduled  = "dream_nudge_scheduled"
	PhaseRunCompleted         = "run_completed"

	// Error / edge phases
	PhaseRunFailed          = "run_failed"
	PhaseRunCanceled        = "run_canceled"
	PhaseRunTimedOut        = "run_timed_out"
	PhaseBridgeExecuteError = "bridge_execute_error"
	PhaseBridgeProcessDeath = "bridge_process_death"
	PhaseRetryStarted       = "retry_started"
	PhaseRetryFailed        = "retry_failed"
	PhaseFallbackStarted    = "fallback_started"
	PhaseFallbackResult     = "fallback_result"
	PhaseSecurityBlock      = "security_block"

	// Cron phases
	PhaseCronDue         = "cron_due"
	PhaseCronPromptBuilt = "cron_prompt_built"
	PhaseCronBridgeStart = "cron_bridge_started"
	PhaseCronCompleted   = "cron_completed"
	PhaseCronFailed      = "cron_failed"

	// Orchestration phases
	PhaseOrchPreflightStarted = "orchestration_preflight_started"
	PhaseOrchPreflightFailed  = "orchestration_preflight_failed"
	PhaseWorkerStarted        = "worker_started"
	PhaseWorkerToolUse        = "worker_tool_use"
	PhaseWorkerValFailed      = "worker_validation_failed"
	PhaseWorkerApproved       = "worker_approved"
	PhaseWorkerMerged         = "worker_merged"
	PhaseOrchCommitted        = "orchestration_committed"
	PhaseOrchCompleted        = "orchestration_completed"
)

// RunContext carries the correlation context for a single run.
// RunID is created once per user turn, before Bridge execution.
// All fields are populated as early as possible in the pipeline.
type RunContext struct {
	RunID      string
	EntryPoint string
	RequestID  string
	ChatID     int64
	ThreadID   int
	UserID     int64
	MessageID  int
	CWD        string
	AgentName  string
	Provider   string
	Model      string
	Profile    string // capability_profile
	StartedAt  time.Time
}
