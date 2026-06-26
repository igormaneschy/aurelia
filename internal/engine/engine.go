// Package engine defines the contract between the Aurelia pipeline and any
// reasoning engine (PI SDK today, others in the future). It has no internal
// dependencies — adapters in internal/bridge translate to concrete transports.
package engine

import "context"

// EventType is a normalized event category. RawType preserves the original
// engine-specific type string for pipeline handlers that branch on PI events.
type EventType string

const (
	EventTypeText       EventType = "text"
	EventTypeToolUse    EventType = "tool_use"
	EventTypeToolResult EventType = "tool_result"
	EventTypeDone       EventType = "done"
	EventTypeError      EventType = "error"
	EventTypeSystem     EventType = "system"
	EventTypeOther      EventType = "other"
)

// Engine is the minimal interface the pipeline needs from a reasoning backend.
type Engine interface {
	Query(ctx context.Context, req Request) (<-chan Event, error)
	Command(ctx context.Context, cmd Command) (Event, error)
	Stats(ctx context.Context, sessionKey string, opts StatsOptions) (Stats, error)
}

// Event is a single streaming or synchronous response from the engine.
// RawType mirrors the underlying transport event name (e.g. "assistant",
// "compaction_start") so existing pipeline switches keep working during migration.
type Event struct {
	Type    EventType
	RawType string

	RequestID string
	Content   string
	Text      string
	Message   string

	// Tool events
	Name      string
	Input     string // JSON-encoded tool input
	InputRaw  any    // optional decoded input for loop detection during migration

	// System session metadata
	SessionID   string
	SessionFile string
	Model       string
	Tools       []string

	// Result / usage
	CostUSD      float64
	DurationMs   int64
	NumTurns     int
	InputTokens  int
	OutputTokens int

	// Streaming state (get-state)
	IsStreaming  bool
	PendingCount int

	Err error
}

// IsTerminal reports whether the event ends a request stream.
func (e Event) IsTerminal() bool {
	if e.Type == EventTypeDone || e.Type == EventTypeError {
		return true
	}
	return e.RawType == "result" || e.RawType == "error" || e.RawType == "pong"
}

// ContentText returns the primary text payload, preferring Text over Content.
func (e Event) ContentText() string {
	if e.Text != "" {
		return e.Text
	}
	return e.Content
}

// Request is a streaming query to the engine.
type Request struct {
	Prompt       string
	SystemPrompt string
	SessionKey   string // opaque resume key (PI session file path)
	Provider     string
	Model        string
	Cwd          string
	AllowedTools []string
	DisallowedTools []string
	Continue     bool
	NoUserSettings  bool
	PersistSession  *bool
	StreamingBehavior string
	RequestID    string
	Images       []Image
	Security     *SecurityPolicy
	ChatID       int64
	ThreadID     int
	UserID       int64
}

// Command is a synchronous engine operation (abort, steer, get-state, etc.).
type Command struct {
	Name            string
	Payload         string
	TargetRequestID string
	Refresh         bool
	ChatID          int64
	ThreadID        int
	UserID          int64
	SessionKey      string
}

// Image is an image attachment for vision queries.
type Image struct {
	Data      string
	MediaType string
	Path      string
}

// SecurityPolicy carries capability profile and guard-rail context.
type SecurityPolicy struct {
	Enabled           bool
	Profile           string
	Mode              string
	Cwd               string
	SensitivePaths    []string
	AllowedOutsideCWD []string
	ChatID            int64
	ThreadID          int
	UserID            int64
	AgentName         string
	RequestID         string
}

// StatsOptions identifies the session for statistics queries.
type StatsOptions struct {
	ChatID   int64
	ThreadID int
	UserID   int64
}

// Stats holds session usage statistics.
type Stats struct {
	SessionFile     string
	SessionID       string
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	ToolCalls       int
	UserMessages    int
	Turns           int
	Cost            float64
	ContextUsagePct float64
}