package runlog

import "time"

// RunStatus represents the lifecycle state of a pipeline run.
type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunTimedOut  RunStatus = "timed_out"
	RunCanceled  RunStatus = "canceled"
	RunFailed    RunStatus = "failed"
)

// RunRecord is the full representation of a run journal entry.
type RunRecord struct {
	RunID      string
	ChatID     int64
	ThreadID   int
	RequestID  string
	SessionID  string
	CWD        string
	Prompt     string
	Status     RunStatus
	Checkpoint string
	ToolSummary string
	Error      string
	StartedAt  time.Time
	UpdatedAt  time.Time
	CompletedAt time.Time

	// Extended observability fields (backfilled; zero/empty for old rows).
	UserID           int64
	EntryPoint       string // telegram | cron | orchestration | nudge | cli
	AgentName        string
	Provider         string
	Model            string
	CapabilityProfile string
	DurationMs       int64
	InputTokens      int64
	OutputTokens     int64
	CostUSD          float64
	ToolCount        int
	ErrorClass       string
	TimeoutOrigin    string
	UsedFallback     bool
	SessionFile      string
	ParentRunID      string

	// Pi session ↔ Telegram message bridge.
	InboundMessageID  int64 // Telegram message_id that triggered this run (0 if N/A)
	OutboundMessageID int64 // Telegram message_id of the final response; always 0 at Start, set via Update after sending
}

// RunRecordRx is the full read schema for scanning rows from run_journal.
// It includes the extended fields with nullable types for backward
// compatibility with rows that predate the migration.
type RunRecordRx struct {
	RunRecord
	// DurationMsNullable, etc. can be added here if scan-time defaulting
	// is needed. For now zero values from missing columns are acceptable.
}

// RunUpdate carries optional fields to update on an existing run.
type RunUpdate struct {
	RunID       string
	SessionID   *string
	Status      *RunStatus
	Checkpoint  *string
	ToolSummary *string
	Error       *string
	CompletedAt *time.Time

	// Extended observability fields (optional pointer semantics).
	UserID           *int64
	EntryPoint       *string
	AgentName        *string
	Provider         *string
	Model            *string
	CapabilityProfile *string
	DurationMs       *int64
	InputTokens      *int64
	OutputTokens     *int64
	CostUSD          *float64
	ToolCount        *int
	ErrorClass       *string
	TimeoutOrigin    *string
	UsedFallback     *bool
	SessionFile      *string
	ParentRunID      *string

	// Pi session ↔ Telegram message bridge.
	InboundMessageID  *int64
	OutboundMessageID *int64
}

// RunEvent represents a single point-in-time timeline event.
type RunEvent struct {
	ID           int64  // auto-increment primary key
	RunID        string // correlation id
	Timestamp    int64  // unix timestamp (seconds)
	Phase        string // e.g. "telegram_received", "bridge_request_started"
	Level        string // "info", "warn", "error"
	Message      string
	MetadataJSON string // small redacted JSON blob
}

// RunResult carries completion fields computed from a bridge result event.
type RunResult struct {
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	ToolCount    int
	DurationMs   int64
	Status       RunStatus
	ErrorClass   string
	TimeoutOrigin string
	UsedFallback  bool
	SessionFile  string
}
