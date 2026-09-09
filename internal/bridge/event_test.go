package bridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestEventContent(t *testing.T) {
	tests := []struct {
		name string
		ev   Event
		want string
	}{
		{name: "both empty", ev: Event{}, want: ""},
		{name: "content only", ev: Event{Content: "c"}, want: "c"},
		{name: "text only", ev: Event{Text: "t"}, want: "t"},
		{name: "text preferred over content", ev: Event{Text: "text", Content: "content"}, want: "text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EventContent(tc.ev)
			if got != tc.want {
				t.Fatalf("EventContent(%+v) = %q, want %q", tc.ev, got, tc.want)
			}
		})
	}
}

// TestEventParseLongSessionFields verifies the NDJSON contract additions
// (ISO timestamp, stall/steer telemetry, tool duration, compaction delta)
// survive Bridge -> Go parsing with explicit units.
func TestEventParseLongSessionFields(t *testing.T) {
	line := `{"event":"stall","request_id":"req-1","timestamp":"2026-08-11T10:00:00.123Z",` +
		`"severity":"warning","silent_ms":120000,"source":"bridge_health"}`
	var ev Event
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Fatalf("unmarshal stall: %v", err)
	}
	if ev.Type != "stall" || ev.RequestID != "req-1" {
		t.Fatalf("unexpected stall base fields: %+v", ev)
	}
	if ev.Timestamp != "2026-08-11T10:00:00.123Z" {
		t.Fatalf("timestamp = %q, want raw ISO preserved", ev.Timestamp)
	}
	if ev.Severity != "warning" || ev.SilentMs != 120000 || ev.Source != "bridge_health" {
		t.Fatalf("unexpected stall telemetry fields: %+v", ev)
	}

	// tool_result carries duration_ms only when the Bridge observed a pair;
	// tool_call_id is the opaque non-sensitive correlation id and
	// duration_measured=true is present even for a measured 0ms pair.
	var tr Event
	if err := json.Unmarshal([]byte(
		`{"event":"tool_result","request_id":"req-1","content":"summary","tool_call_id":"tc-1","duration_measured":true,"duration_ms":0}`), &tr); err != nil {
		t.Fatalf("unmarshal tool_result: %v", err)
	}
	if tr.ToolCallID != "tc-1" || !tr.DurationMeasured || tr.DurationMs != 0 {
		t.Fatalf("unexpected tool_result fields: %+v", tr)
	}
	var tr2 Event
	if err := json.Unmarshal([]byte(
		`{"event":"tool_result","request_id":"req-1","content":"summary","tool_call_id":"tc-2","duration_measured":true,"duration_ms":1234}`), &tr2); err != nil {
		t.Fatalf("unmarshal tool_result: %v", err)
	}
	if tr2.ToolCallID != "tc-2" || !tr2.DurationMeasured || tr2.DurationMs != 1234 {
		t.Fatalf("unexpected tool_result fields: %+v", tr2)
	}

	// compaction_end carries tokens_after/delta_tokens; a negative delta
	// (effective reduction) stays observable, and a measured zero
	// (neutral compaction) is explicit, not "unmeasured". error_class is a
	// static enum — the raw error message is never part of the contract.
	var ce Event
	if err := json.Unmarshal([]byte(
		`{"event":"compaction_end","request_id":"req-1","tokens_before":1000,`+
			`"tokens_after":1200,"delta_tokens":200,"success":true,"duration_ms":5000}`), &ce); err != nil {
		t.Fatalf("unmarshal compaction_end: %v", err)
	}
	if ce.TokensBefore != 1000 || ce.TokensAfter == nil || *ce.TokensAfter != 1200 ||
		ce.DeltaTokens == nil || *ce.DeltaTokens != 200 || !ce.Success {
		t.Fatalf("unexpected compaction fields: %+v", ce)
	}
	var errComp Event
	if err := json.Unmarshal([]byte(
		`{"event":"compaction_end","success":false,"error_class":"compaction_error"}`), &errComp); err != nil {
		t.Fatalf("unmarshal compaction error: %v", err)
	}
	if errComp.ErrorClass != "compaction_error" || errComp.Success {
		t.Fatalf("unexpected compaction error fields: %+v", errComp)
	}
	var neg Event
	if err := json.Unmarshal([]byte(
		`{"event":"compaction_end","tokens_before":1000,"tokens_after":800,"delta_tokens":-200}`), &neg); err != nil {
		t.Fatalf("unmarshal negative delta: %v", err)
	}
	if neg.DeltaTokens == nil || *neg.DeltaTokens != -200 {
		t.Fatalf("delta_tokens = %v, want -200 (negative reduction observable)", neg.DeltaTokens)
	}
	var neutral Event
	if err := json.Unmarshal([]byte(
		`{"event":"compaction_end","tokens_before":1000,"tokens_after":1000,"delta_tokens":0}`), &neutral); err != nil {
		t.Fatalf("unmarshal neutral delta: %v", err)
	}
	if neutral.DeltaTokens == nil || *neutral.DeltaTokens != 0 {
		t.Fatalf("delta_tokens = %v, want explicit 0 (neutral observable)", neutral.DeltaTokens)
	}

	// Absent optional telemetry fields remain nil/zero (duration_ms=0 means
	// no reliable pair was observed).
	var bare Event
	if err := json.Unmarshal([]byte(`{"event":"tool_result","content":"x"}`), &bare); err != nil {
		t.Fatalf("unmarshal bare tool_result: %v", err)
	}
	if bare.DurationMs != 0 || bare.SilentMs != 0 || bare.Timestamp != "" ||
		bare.TokensAfter != nil || bare.DeltaTokens != nil || bare.ToolCallID != "" ||
		bare.DurationMeasured || bare.ErrorClass != "" {
		t.Fatalf("expected nil/zero-valued optional fields, got %+v", bare)
	}
}

func TestNormalizeEventPreservesBoundedTelemetryEnums(t *testing.T) {
	got := normalizeEvent(Event{
		Type:       "stall",
		Severity:   "warning",
		Source:     "bridge_health",
		ErrorClass: "compaction_error",
		Reason:     "threshold",
	})
	if got.Severity != "warning" || got.Source != "bridge_health" {
		t.Fatalf("bounded bridge-health fields were lost: %+v", got)
	}
	if got.ErrorClass != "compaction_error" || got.Reason != "automatic" {
		t.Fatalf("bounded compaction fields were lost: %+v", got)
	}

	unsafe := normalizeEvent(Event{Severity: "provider supplied text", ErrorClass: "provider supplied text", Source: "attacker"})
	if unsafe.Severity != "unknown" || unsafe.ErrorClass != "unknown" || unsafe.Source != "" {
		t.Fatalf("unsafe enum values were not bounded: %+v", unsafe)
	}
}

func TestNormalizeEventCapsSerializedPayload(t *testing.T) {
	got := normalizeEvent(Event{
		Type:    "result",
		Content: strings.Repeat("x", maxEventTextBytes),
		Text:    strings.Repeat("y", maxEventTextBytes),
		Message: strings.Repeat("z", maxEventTextBytes),
		Input:   strings.Repeat("input", maxEventTextBytes),
	})
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal normalized event: %v", err)
	}
	if len(encoded) > maxEventPayloadBytes {
		t.Fatalf("normalized payload = %d bytes, want <= %d", len(encoded), maxEventPayloadBytes)
	}
	if got.Content == "" {
		t.Fatal("terminal result content was discarded while bounding payload")
	}
}

// TestNormalizeEvent_ResultContentKeepsStructuredJSON pins the list-models /
// get-session-history contract: structured result payloads are JSON the Go
// callers must parse whole. A catalog larger than every historical bound
// (12K text, then 48K first-attempt) must survive normalization without
// being cut mid-JSON — the Go-side truncation regression behind empty model
// pickers. Real daemon catalogs with cloud keys reach tens of KB.
func TestNormalizeEvent_ResultContentKeepsStructuredJSON(t *testing.T) {
	models := make([]map[string]any, 0, 640)
	for i := range 640 {
		models = append(models, map[string]any{
			"provider": fmt.Sprintf("provider-%02d", i%40), "id": fmt.Sprintf("model-%03d", i),
			"name": fmt.Sprintf("Model %03d long display name for catalog fixture", i), "supportsImages": false,
		})
	}
	content, err := json.Marshal(models)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if len(content) <= maxEventTextBytes {
		t.Fatalf("fixture too small (%d bytes); must exceed the %d text bound", len(content), maxEventTextBytes)
	}

	got := normalizeEvent(Event{Type: "result", Content: string(content)})
	if got.Content != string(content) {
		t.Fatalf("result content was altered by normalization: got %d bytes, want %d", len(got.Content), len(content))
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(got.Content), &parsed); err != nil {
		t.Fatalf("normalized result content no longer parses: %v", err)
	}
}

// TestNormalizeEvent_NonResultTextStillBounded guards the other direction:
// streaming text, messages, and non-result content keep the tight 12K bound.
func TestNormalizeEvent_NonResultTextStillBounded(t *testing.T) {
	big := strings.Repeat("x", maxEventTextBytes+2048)

	got := normalizeEvent(Event{Type: "assistant", Text: big})
	if len(got.Text) != maxEventTextBytes {
		t.Fatalf("assistant text = %d bytes, want %d", len(got.Text), maxEventTextBytes)
	}

	got = normalizeEvent(Event{Type: "log", Content: big})
	if len(got.Content) != maxEventTextBytes {
		t.Fatalf("log content = %d bytes, want %d", len(got.Content), maxEventTextBytes)
	}
}

// TestNormalizeEvent_KeepsToolRunningTelemetry pins the tool-aware health
// contract: tool_running/tool_slow survive the type allowlist (unknown types
// degrade to "unknown" and are dropped downstream), the bounded label and
// elapsed are preserved, and an out-of-range elapsed degrades to 0.
func TestNormalizeEvent_KeepsToolRunningTelemetry(t *testing.T) {
	got := normalizeEvent(Event{
		Type:       "tool_running",
		Name:       "Bash",
		ToolCallID: "tool-abc",
		ElapsedMs:  195_000,
		Source:     "bridge_health",
	})
	if got.Type != "tool_running" {
		t.Fatalf("type = %q, want tool_running", got.Type)
	}
	if got.ElapsedMs != 195_000 || got.Name != "Bash" || got.Source != "bridge_health" {
		t.Fatalf("tool_running telemetry lost: %+v", got)
	}

	slow := normalizeEvent(Event{Type: "tool_slow", Name: "Bash", ElapsedMs: 700_000})
	if slow.Type != "tool_slow" {
		t.Fatalf("type = %q, want tool_slow", slow.Type)
	}

	bogus := normalizeEvent(Event{Type: "tool_running", Name: "Bash", ElapsedMs: -5})
	if bogus.ElapsedMs != 0 {
		t.Fatalf("elapsed_ms = %d, want 0 for out-of-range input", bogus.ElapsedMs)
	}
}

// TestNormalizeEvent_PreservesNewlinesInContent pins the long-reply regression
// (introduced by 0a99422, fixed here): control-char sanitization must keep the
// whitespace controls that make a reply readable — stripping LF/CR/TAB collapsed
// a long assistant reply into an unformatted run-on block in Telegram.
func TestNormalizeEvent_PreservesNewlinesInContent(t *testing.T) {
	got := normalizeEvent(Event{
		Type:    "result",
		Content: "linha1\nlinha2\r\n\n\tcode\n**bold**\n",
	})
	want := "linha1\nlinha2\r\n\n\tcode\n**bold**\n"
	if got.Content != want {
		t.Fatalf("content newlines/whitespace dropped:\n got=%q\nwant=%q", got.Content, want)
	}
	// Non-whitespace control chars are still stripped (injection safety),
	// while the newline is preserved.
	got = normalizeEvent(Event{Type: "assistant", Text: "a\x00b\x1f\nkeep"})
	if got.Text != "ab\nkeep" {
		t.Fatalf("assistant text = %q, want %q (newline kept, \x00/\x1f stripped)", got.Text, "ab\nkeep")
	}
}
