package bridge

// Request sent to Bridge process via stdin as JSON.
type Request struct {
	Command         string         `json:"command"`
	Prompt          string         `json:"prompt,omitempty"`
	RequestID       string         `json:"request_id,omitempty"`
	TargetRequestID string         `json:"target_request_id,omitempty"`
	Refresh         bool           `json:"refresh,omitempty"`
	Options         RequestOptions `json:"options,omitempty"`
}

// ImageAttachment represents a base64-encoded image to send alongside the prompt.
type ImageAttachment struct {
	Path      string `json:"path,omitempty"`
	Data      string `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

// RequestOptions configures how the Bridge executes a query or session command.
//
// The PI SDK does not expose hooks for MaxTurns, PermissionMode, MCP servers,
// sub-agent registries, or per-tool disablement that the legacy Claude SDK
// supported. Those fields were dropped during the migration; revisit if PI
// adds equivalents in a future release.
//
// ChatID, ThreadID, and UserID identify the chat session for bridge-side session indexing.
// StreamingBehavior controls how the bridge queues the prompt on an active session:
// "steer" interrupts the current turn, "followUp" queues for after completion.
type RequestOptions struct {
	Provider          string            `json:"provider,omitempty"`
	Model             string            `json:"model,omitempty"`
	Cwd               string            `json:"cwd,omitempty"`
	SystemPrompt      string            `json:"system_prompt,omitempty"`
	Resume            string            `json:"resume,omitempty"`
	AllowedTools      []string          `json:"allowed_tools,omitempty"`
	DisallowedTools   []string          `json:"disallowed_tools,omitempty"`
	Continue          bool              `json:"continue,omitempty"`
	NoUserSettings    bool              `json:"no_user_settings,omitempty"`
	PersistSession    *bool             `json:"persist_session,omitempty"`
	Images            []ImageAttachment `json:"images,omitempty"`
	Security          *SecurityContext  `json:"security,omitempty"`
	ChatID            int64             `json:"chat_id,omitempty"`
	ThreadID          int               `json:"thread_id,omitempty"`
	UserID            int64             `json:"user_id,omitempty"`
	StreamingBehavior string            `json:"streaming_behavior,omitempty"`
}

// SessionStats holds PI session statistics returned by get-session-stats.
// Mirrors the PI SDK's SessionStats interface for multi-session lifecycle
// management on the Go side.
type SessionStats struct {
	SessionFile       string  `json:"session_file,omitempty"`
	SessionID         string  `json:"session_id"`
	UserMessages      int     `json:"user_messages,omitempty"`
	AssistantMessages int     `json:"assistant_messages,omitempty"`
	ToolCalls         int     `json:"tool_calls,omitempty"`
	ToolResults       int     `json:"tool_results,omitempty"`
	TotalMessages     int     `json:"total_messages,omitempty"`
	InputTokens       int     `json:"input_tokens,omitempty"`
	OutputTokens      int     `json:"output_tokens,omitempty"`
	CacheReadTokens   int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens  int     `json:"cache_write_tokens,omitempty"`
	TotalTokens       int     `json:"total_tokens,omitempty"`
	Cost              float64 `json:"cost,omitempty"`
	ContextUsagePct   float64 `json:"context_usage_pct,omitempty"`
}

// SessionHistoryMessage is a UI-safe transcript message derived from the PI
// session file. Tool/system/internal messages are filtered out by the bridge.
type SessionHistoryMessage struct {
	Sender    string `json:"sender"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}

// SecurityContext carries capability profile and policy configuration to the
// Bridge so the PI tool_call hook can evaluate and govern individual tool
// calls before they execute.
type SecurityContext struct {
	Enabled           bool     `json:"enabled"`
	Profile           string   `json:"profile"`
	Mode              string   `json:"mode"`
	Cwd               string   `json:"cwd"`
	SensitivePaths    []string `json:"sensitive_paths,omitempty"`
	AllowedOutsideCWD []string `json:"allowed_outside_cwd,omitempty"`
	ChatID            int64    `json:"chat_id,omitempty"`
	ThreadID          int      `json:"thread_id,omitempty"`
	UserID            int64    `json:"user_id,omitempty"`
	AgentName         string   `json:"agent_name,omitempty"`
	RequestID         string   `json:"request_id,omitempty"`
}
