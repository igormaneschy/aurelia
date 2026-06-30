package runlog

import "time"

// PruneOptions controls runlog retention pruning.
type PruneOptions struct {
	// OlderThan deletes completed terminal runs with started_at strictly before
	// this instant. Running runs are never deleted.
	OlderThan time.Time
	// DryRun reports counts without deleting.
	DryRun bool
}

// PruneResult reports what a prune operation would delete or deleted.
type PruneResult struct {
	RunsDeleted   int64
	EventsDeleted int64
}

// DefaultRetentionDays is the runlog retention window when observability.retention_days
// is unset in config. Set retention_days to 0 to disable automatic pruning.
const DefaultRetentionDays = 30