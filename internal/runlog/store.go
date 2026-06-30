package runlog

import "context"

// Store persists run lifecycle events for observability and checkpointing.
type Store interface {
	// Start creates a new run journal entry with status=running.
	Start(ctx context.Context, record RunRecord) error

	// Update applies partial updates to an existing run by RunID.
	Update(ctx context.Context, update RunUpdate) error

	// Complete transitions a run to a terminal status, persisting checkpoint,
	// error, and optional tool_summary in a single write.
	Complete(ctx context.Context, runID string, status RunStatus, checkpoint, errMsg, toolSummary string) error

	// RecordEvents persists multiple timeline events in one transaction.
	// Best-effort: errors are logged by callers, never block the pipeline.
	RecordEvents(ctx context.Context, events []RunEvent) error

	// Prune deletes terminal runs older than opts.OlderThan and their events.
	// Running runs are preserved regardless of age.
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)

	// Latest returns the most recent run for a given chat/thread, or nil if none.
	Latest(ctx context.Context, chatID int64, threadID int) (*RunRecord, error)

	// RecordEvent persists a single run event to the timeline.
	// Best-effort: errors are logged, never block the caller.
	RecordEvent(ctx context.Context, ev RunEvent) error

	// ListEvents returns all events for a run, ordered by timestamp ascending.
	ListEvents(ctx context.Context, runID string) ([]RunEvent, error)

	// GetRun returns a single run by RunID, or nil if not found.
	GetRun(ctx context.Context, runID string) (*RunRecord, error)

	// ListRuns returns recent runs matching optional filters.
	// Limit caps the result set (default 20). When chatID is non-zero,
	// results are scoped to that chat. Results are ordered by started_at DESC.
	ListRuns(ctx context.Context, chatID int64, limit int) ([]RunRecord, error)

	// Metrics returns aggregate operational metrics over a time window.
	Metrics(ctx context.Context, filter MetricsFilter) (*MetricsResult, error)

	// GetLastOutboundMessage returns the chat_id, thread_id, and outbound_message_id
	// of the most recent run that has a non-zero outbound_message_id for the given
	// session_file. Returns chatID=0, threadID=0, messageID=0 if not found.
	GetLastOutboundMessage(ctx context.Context, sessionFile string) (chatID int64, threadID int, messageID int64, err error)

	// Close releases the store's resources.
	Close() error
}
