package observability

import "time"

// RunEvent represents a single point-in-time event in a run's timeline.
// Phases are defined as constants in this package.
type RunEvent struct {
	ID           int64     // database auto-increment id (0 for new events)
	RunID        string    // correlation id
	Timestamp    time.Time // when the event occurred
	Phase        string    // one of the Phase* constants
	Level        string    // "info", "warn", "error"
	Message      string    // human-readable description
	MetadataJSON string    // small, redacted JSON blob (max 4096 bytes)
}

// EventLevel constants.
const (
	EventLevelInfo  = "info"
	EventLevelWarn  = "warn"
	EventLevelError = "error"
)

// --- Helper to build events ---

// NewEvent creates a RunEvent with the current timestamp, info level,
// and empty metadata. Fields that are empty at construction time are set
// by the caller before calling RecordEvent if needed.
func NewEvent(runID, phase, message string) RunEvent {
	return RunEvent{
		RunID:     runID,
		Timestamp: time.Now(),
		Phase:     phase,
		Level:     EventLevelInfo,
		Message:   message,
	}
}

// NewErrorEvent creates an error-level RunEvent.
func NewErrorEvent(runID, phase, message string) RunEvent {
	ev := NewEvent(runID, phase, message)
	ev.Level = EventLevelError
	return ev
}

// NewWarnEvent creates a warn-level RunEvent.
func NewWarnEvent(runID, phase, message string) RunEvent {
	ev := NewEvent(runID, phase, message)
	ev.Level = EventLevelWarn
	return ev
}

// MaxEventMetadataBytes is the maximum allowed size for MetadataJSON.
// Values above this are truncated before storage.
const MaxEventMetadataBytes = 4096
