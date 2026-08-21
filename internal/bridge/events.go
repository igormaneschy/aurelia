package bridge

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maxEventPayloadBytes bounds one normalized event crossing the Go
	// boundary. The request stream has a separate aggregate byte budget.
	// The TS bridge never emits a serialized event above its own 64KB
	// MAX_OUT_EVENT_BYTES, so this backstop sits above that with headroom
	// and only triggers on hostile/buggy bridge output.
	maxEventPayloadBytes = 80 * 1024
	// maxEventTextBytes bounds free text (streaming deltas, messages, log
	// content). Structured JSON result payloads have their own bound below.
	maxEventTextBytes = 12 * 1024
	// maxEventResultContentBytes mirrors the TS bridge's
	// MAX_RESULT_CONTENT_RUNES for structured result payloads (list-models,
	// get-session-history): they are JSON the Go callers must parse whole,
	// so cutting them at the text bound produces invalid JSON and empty
	// catalogs/histories downstream.
	maxEventResultContentBytes = 48 * 1024
	maxEventIDBytes            = 128
	maxEventInputBytes         = 4 * 1024
	maxEventListEntries        = 64
	maxEventMetric             = 100_000_000
	maxEventDurationMs         = int64(24 * time.Hour / time.Millisecond)

	// maxRequestStreamBytes is enforced by requestStream across its fast
	// channel and overflow queue. Terminal delivery may evict non-terminals
	// but never bypasses the cap with an unbounded payload.
	maxRequestStreamBytes = 512 * 1024
)

var (
	bridgeAPIKeyRE  = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9]{20,}|pk-[A-Za-z0-9]{20,}|sk_live_[A-Za-z0-9]+|sk_test_[A-Za-z0-9]+|AKIA[A-Z0-9]{16}|AIza[0-9A-Za-z_-]{35}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[0-9A-Za-z_-]+|xai-[A-Za-z0-9]{20,}|glpat-[A-Za-z0-9_-]{20,}|hf_[A-Za-z0-9]{20,}|npm_[A-Za-z0-9]{20,})`)
	bridgeJWTRE     = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+`)
	bridgePrivateRE = regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----`)
	bridgeAuthRE    = regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|Basic)\s+)\S+`)
	bridgeSecretRE  = regexp.MustCompile(`(?i)(?:^|[\s"{,])(?:password|secret|api[_-]?key|client[_-]?secret|access[_-]?token|refresh[_-]?token|token)\s*[=:]\s*\S+`)
)

func redactBridgeText(s string) string {
	s = bridgePrivateRE.ReplaceAllString(s, "[PRIVATE_KEY_BLOCK_REDACTED]")
	s = bridgeAuthRE.ReplaceAllString(s, "$1[REDACTED]")
	s = bridgeAPIKeyRE.ReplaceAllString(s, "[CREDENTIAL_REDACTED]")
	s = bridgeJWTRE.ReplaceAllString(s, "[JWT_REDACTED]")
	return bridgeSecretRE.ReplaceAllString(s, "[CREDENTIAL_REDACTED]")
}

// boundedEventText always redacts before changing the size/shape of the
// untrusted string. It also guarantees valid UTF-8 and removes line/control
// injection characters before logs or persistence can observe the value.
func boundedEventText(s string, maxBytes int) string {
	s = redactBridgeText(strings.ToValidUTF8(s, "\uFFFD"))
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
	if maxBytes <= 0 || len(clean) <= maxBytes {
		return clean
	}
	cut := clean[:maxBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	for len(cut) > 0 && cut[len(cut)-1]&0xc0 == 0x80 {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func boundedEventMetric(v int, allowNegative bool) int {
	if v < 0 && !allowNegative {
		return 0
	}
	if v > maxEventMetric {
		return maxEventMetric
	}
	if v < -maxEventMetric {
		return -maxEventMetric
	}
	return v
}

func boundedEventDuration(v int64) (int64, bool) {
	if v < 0 || v > maxEventDurationMs {
		return 0, false
	}
	return v, true
}

func safeEventID(raw string) string {
	if raw == "" {
		return ""
	}
	if len(raw) <= maxEventIDBytes && isSafeEventID(raw) {
		return raw
	}
	// Keep malformed/hostile IDs request-local in representation: only a
	// stable digest crosses the boundary, never the raw SDK value.
	digest := sha256.Sum256([]byte(raw))
	return "id-" + stringHex(digest[:10])
}

func isSafeEventID(s string) bool {
	for _, r := range s {
		switch {
		case r == '-' || r == '_' || r == '.' || r == ':':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func validRequestID(s string) bool {
	return s != "" && len(s) <= maxEventIDBytes && isSafeEventID(s)
}

// validateBridgeRequest rejects malformed command schemas before the payload
// reaches stdin/TypeScript/SDK work. Keep this allowlist identical to the
// TypeScript production handler; get-env is intentionally unsupported.
func validateBridgeRequest(req Request) error {
	switch req.Command {
	case "query", "steer", "follow-up":
		if req.Prompt == "" {
			return fmt.Errorf("bridge: command %q requires a prompt", req.Command)
		}
	case "ping", "cancel", "list-models", "abort", "get-state", "get-session-stats",
		"get-session-history", "compact-session", "rotate-session":
		// Valid command schemas with optional prompt/options.
	default:
		return fmt.Errorf("bridge: invalid command")
	}
	if req.Command == "cancel" && !validRequestID(req.TargetRequestID) {
		return fmt.Errorf("bridge: cancel requires a valid target_request_id")
	}
	if req.Options.Security != nil && req.Options.Security.RequestID != "" &&
		!validRequestID(req.Options.Security.RequestID) {
		return fmt.Errorf("bridge: invalid security request_id")
	}
	return nil
}

func stringHex(b []byte) string {
	const hex = "0123456789abcdef"
	var out strings.Builder
	out.Grow(len(b) * 2)
	for _, v := range b {
		out.WriteByte(hex[v>>4])
		out.WriteByte(hex[v&0x0f])
	}
	return out.String()
}

func normalizeEventType(s string) string {
	switch s {
	case "system", "tool_use", "tool_result", "assistant", "result", "error",
		"pong", "compaction_start", "compaction_end", "stall", "steer",
		"turn_start", "turn_end", "agent_start", "agent_end", "auto_retry_start", "auto_retry_end":
		return s
	default:
		return "unknown"
	}
}

func normalizeEventTimestamp(s string) string {
	if s == "" {
		return ""
	}
	if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
		return ""
	}
	// Preserve the valid source representation (including millisecond
	// precision) because it is correlation metadata, not an arithmetic value.
	return boundedEventText(s, 64)
}

func normalizeEventReason(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "manual", "user":
		return "manual"
	case "automatic", "auto", "system", "context", "context_window", "threshold", "overflow":
		return "automatic"
	case "unknown":
		return "unknown"
	default:
		return "unknown"
	}
}

func normalizeEventInput(input any) any {
	if input == nil {
		return nil
	}
	b, err := json.Marshal(input)
	if err != nil || len(b) > maxEventInputBytes {
		return map[string]any{"input_truncated": true}
	}
	clean := boundedEventText(string(b), maxEventInputBytes)
	var out any
	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return map[string]any{"input_truncated": true}
	}
	return out
}

func normalizeEvent(ev Event) Event {
	ev.Type = normalizeEventType(ev.Type)
	ev.RequestID = safeEventID(ev.RequestID)
	ev.Timestamp = normalizeEventTimestamp(ev.Timestamp)
	ev.SessionID = safeEventID(ev.SessionID)
	ev.SessionFile = boundedEventText(ev.SessionFile, maxEventTextBytes)
	ev.Model = boundedEventText(ev.Model, maxEventIDBytes)
	if len(ev.Tools) > maxEventListEntries {
		ev.Tools = ev.Tools[:maxEventListEntries]
	}
	for i := range ev.Tools {
		ev.Tools[i] = boundedEventText(ev.Tools[i], maxEventIDBytes)
	}
	ev.Name = boundedEventText(ev.Name, maxEventIDBytes)
	ev.ToolCallID = safeEventID(ev.ToolCallID)
	if ev.ToolCallID == "" {
		ev.ToolCallID = safeEventID(ev.ID)
	}
	ev.ID = ev.ToolCallID
	contentBound := maxEventTextBytes
	if ev.Type == "result" {
		contentBound = maxEventResultContentBytes
	}
	ev.Content = boundedEventText(ev.Content, contentBound)
	ev.Text = boundedEventText(ev.Text, maxEventTextBytes)
	ev.Message = boundedEventText(ev.Message, maxEventTextBytes)
	ev.Input = normalizeEventInput(ev.Input)
	ev.Reason = normalizeEventReason(ev.Reason)
	switch ev.ErrorClass {
	case "", "compaction_error":
		// Keep the static enum or the absent value.
	default:
		ev.ErrorClass = "unknown"
	}
	switch ev.Severity {
	case "", "warning", "urgent":
		// Keep the bounded severity enum or the absent value.
	default:
		ev.Severity = "unknown"
	}
	if ev.Source != "bridge_health" {
		ev.Source = ""
	}
	ev.TokensBefore = boundedEventMetric(ev.TokensBefore, false)
	ev.PendingCount = boundedEventMetric(ev.PendingCount, false)
	ev.NumTurns = boundedEventMetric(ev.NumTurns, false)
	ev.InputTokens = boundedEventMetric(ev.InputTokens, false)
	ev.OutputTokens = boundedEventMetric(ev.OutputTokens, false)
	ev.TokensAfter = normalizeEventIntPtr(ev.TokensAfter, false)
	ev.DeltaTokens = normalizeEventIntPtr(ev.DeltaTokens, true)
	if duration, ok := boundedEventDuration(ev.DurationMs); ok {
		ev.DurationMs = duration
	} else {
		ev.DurationMs = 0
		ev.DurationMeasured = false
	}
	if ev.SilentMs < 0 || ev.SilentMs > maxEventDurationMs {
		ev.SilentMs = 0
	}
	if ev.CostUSD < 0 || ev.CostUSD > 1_000_000_000 {
		ev.CostUSD = 0
	}

	if encoded, err := json.Marshal(ev); err != nil || len(encoded) > maxEventPayloadBytes {
		// Preserve the event envelope and terminal class while dropping the
		// largest non-essential payloads. The terminal event itself remains
		// routable and is never converted into a dropped stream item.
		ev.Input = nil
		ev.Tools = nil
		switch ev.Type {
		case "result":
			ev.Content = boundedEventText(ev.Content, maxEventPayloadBytes/4)
			ev.Text = ""
			ev.Message = ""
		case "error":
			ev.Content = ""
			ev.Text = ""
			ev.Message = boundedEventText(ev.Message, maxEventPayloadBytes/4)
		default:
			ev.Content = boundedEventText(ev.Content, maxEventPayloadBytes/4)
			ev.Text = ""
			ev.Message = ""
		}
	}
	return ev
}

func normalizeEventIntPtr(v *int, allowNegative bool) *int {
	if v == nil {
		return nil
	}
	n := boundedEventMetric(*v, allowNegative)
	return &n
}

func eventMemoryBytes(ev Event) int {
	b, err := json.Marshal(ev)
	if err != nil {
		return maxEventPayloadBytes
	}
	return len(b)
}

// Event represents a single NDJSON event emitted by the Bridge on stdout.
// Not all fields are populated for every event type — only the fields relevant
// to the event's Type are set.
type Event struct {
	Type      string `json:"event"`
	RequestID string `json:"request_id,omitempty"`

	// Timestamp is the raw ISO-8601 timestamp stamped by the Bridge on every
	// NDJSON event. It is correlation metadata only: sub-second correlation
	// for telemetry (stall/steer/tool/compaction); never parsed as the run
	// journal clock. Empty when absent.
	Timestamp string `json:"timestamp,omitempty"`

	// system event
	SessionID   string   `json:"session_id,omitempty"`
	SessionFile string   `json:"session_file,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Model       string   `json:"model,omitempty"`

	// tool_use event
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`

	// tool_result / assistant / result / error
	Content string `json:"content,omitempty"`
	Text    string `json:"text,omitempty"`
	Message string `json:"message,omitempty"`

	// tool_result telemetry: tool_call_id is an opaque, non-sensitive
	// correlation id; DurationMeasured is an explicit marker that a
	// start/end pair was observed (present even when duration_ms is 0).
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ID is retained for compatibility with older Bridge tool_use payloads;
	// normalizeEvent maps it into ToolCallID before routing.
	ID               string `json:"id,omitempty"`
	DurationMeasured bool   `json:"duration_measured,omitempty"`

	// result event
	CostUSD      float64 `json:"cost_usd,omitempty"`
	DurationMs   int64   `json:"duration_ms,omitempty"`
	NumTurns     int     `json:"num_turns,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`

	// get_state response
	IsStreaming  bool `json:"is_streaming,omitempty"`
	PendingCount int  `json:"pending_count,omitempty"`

	// compaction event fields
	Reason       string `json:"reason,omitempty"`
	TokensBefore int    `json:"tokens_before,omitempty"`
	Success      bool   `json:"success,omitempty"`

	// ErrorClass carries the static enum value (e.g. "compaction_error")
	// when a compaction failed. The raw SDK error message is never emitted
	// or persisted — only this bounded classification.
	ErrorClass string `json:"error_class,omitempty"`

	// compaction_end additions: tokens_after/delta_tokens use pointer
	// semantics — nil means the Bridge did not measure them; an explicit
	// value (including 0 for a neutral compaction or a negative delta for a
	// regressive one) is always observable.
	TokensAfter *int `json:"tokens_after,omitempty"`
	DeltaTokens *int `json:"delta_tokens,omitempty"`

	// stall / steer telemetry (source=bridge_health). Telemetry only — never
	// user-facing text, never productive activity.
	Severity string `json:"severity,omitempty"`
	SilentMs int64  `json:"silent_ms,omitempty"`
	Source   string `json:"source,omitempty"`
}

// IsTerminal returns true if the event signals the end of a request stream.
func (e Event) IsTerminal() bool {
	return e.Type == "result" || e.Type == "error" || e.Type == "pong"
}

// ContentText returns the primary text payload, preferring Text over Content.
func (e Event) ContentText() string {
	if e.Text != "" {
		return e.Text
	}
	return e.Content
}
