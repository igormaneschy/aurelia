package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// TestHandleProgressEvent_ToolUpdatesIndicatorWithoutTranscript covers T2:
// a working event with a tool name feeds the visual indicator (activeTools)
// and must never touch the transcript buffer.
func TestHandleProgressEvent_ToolUpdatesIndicatorWithoutTranscript(t *testing.T) {
	m := testChatModel()
	m.streamBuf = "existing text"

	updated, _ := m.handleProgressEvent(ipc.IPCEvent{
		Type: ipc.EventTypeProgress,
		Body: mustProgressBody(t, ipc.ProgressPayload{State: "working", ToolName: "Bash", Detail: "ls -la"}),
	})
	m2 := updated.(Model)

	if len(m2.activeTools) != 1 {
		t.Fatalf("activeTools = %+v, want 1 entry", m2.activeTools)
	}
	if m2.activeTools[0].Name != "Bash" || m2.activeTools[0].Detail != "ls -la" || m2.activeTools[0].Done {
		t.Fatalf("activeTools[0] = %+v, want Bash/ls -la pending", m2.activeTools[0])
	}
	if m2.streamBuf != "existing text" {
		t.Fatalf("streamBuf was polluted by progress event: %q", m2.streamBuf)
	}
	if len(m2.messages) != 0 {
		t.Fatalf("progress event must not create chat messages, got %d", len(m2.messages))
	}
}

// TestHandleProgressEvent_ToolDoneMarksLastTool covers the completion marker.
func TestHandleProgressEvent_ToolDoneMarksLastTool(t *testing.T) {
	m := testChatModel()
	m.activeTools = []toolInfo{{Name: "Read", Detail: "x.go"}}

	updated, _ := m.handleProgressEvent(ipc.IPCEvent{
		Type: ipc.EventTypeProgress,
		Body: mustProgressBody(t, ipc.ProgressPayload{State: "working", ToolDone: true}),
	})
	m2 := updated.(Model)

	if len(m2.activeTools) != 1 || !m2.activeTools[0].Done {
		t.Fatalf("activeTools = %+v, want last tool marked done", m2.activeTools)
	}
	if m2.streamBuf != "" {
		t.Fatalf("streamBuf polluted: %q", m2.streamBuf)
	}
}

// TestHandleProgressEvent_StallStatesSetIndicatorLine covers the human stall
// mapping (warning/urgent) and the clear on working/waiting.
func TestHandleProgressEvent_StallStatesSetIndicatorLine(t *testing.T) {
	cases := []struct {
		name    string
		state   string
		wantSub string
	}{
		{"warning", "stall_warning", "⚠️"},
		{"urgent", "stall_urgent", "🚨"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := testChatModel()
			updated, _ := m.handleProgressEvent(ipc.IPCEvent{
				Type: ipc.EventTypeProgress,
				Body: mustProgressBody(t, ipc.ProgressPayload{State: tc.state}),
			})
			m2 := updated.(Model)
			if !strings.Contains(m2.stallLine, tc.wantSub) {
				t.Fatalf("stallLine = %q, want %q", m2.stallLine, tc.wantSub)
			}
			if m2.streamBuf != "" {
				t.Fatalf("stall event polluted streamBuf: %q", m2.streamBuf)
			}
		})
	}

	// Working without a tool clears the warning line.
	m := testChatModel()
	m.stallLine = "⚠️ whatever"
	updated, _ := m.handleProgressEvent(ipc.IPCEvent{
		Type: ipc.EventTypeProgress,
		Body: mustProgressBody(t, ipc.ProgressPayload{State: "working"}),
	})
	m2 := updated.(Model)
	if m2.stallLine != "" {
		t.Fatalf("working must clear stallLine, got %q", m2.stallLine)
	}

	// Waiting (heartbeat) also clears it.
	m = testChatModel()
	m.stallLine = "⚠️ whatever"
	updated, _ = m.handleProgressEvent(ipc.IPCEvent{
		Type: ipc.EventTypeProgress,
		Body: mustProgressBody(t, ipc.ProgressPayload{State: "waiting", Detail: "Ainda estou processando (15s)."}),
	})
	m2 = updated.(Model)
	if m2.stallLine != "" {
		t.Fatalf("waiting must clear stallLine, got %q", m2.stallLine)
	}
}

// TestHandleProgressEvent_TerminalStatesNoOp covers done/canceled/failed:
// they must not mutate the indicator (stream_end/error reset the chrome).
func TestHandleProgressEvent_TerminalStatesNoOp(t *testing.T) {
	for _, state := range []string{"done", "canceled", "failed"} {
		m := testChatModel()
		m.activeTools = []toolInfo{{Name: "Bash"}}
		updated, _ := m.handleProgressEvent(ipc.IPCEvent{
			Type: ipc.EventTypeProgress,
			Body: mustProgressBody(t, ipc.ProgressPayload{State: state}),
		})
		m2 := updated.(Model)
		if len(m2.activeTools) != 1 {
			t.Fatalf("%s must not clear activeTools (stream_end owns cleanup), got %+v", state, m2.activeTools)
		}
	}
}

// TestHandleProgressEvent_MalformedBodyIgnored covers forward compatibility:
// a bad payload must not panic or drop the stream.
func TestHandleProgressEvent_MalformedBodyIgnored(t *testing.T) {
	m := testChatModel()
	updated, _ := m.handleProgressEvent(ipc.IPCEvent{Type: ipc.EventTypeProgress, Body: "{not json"})
	m2 := updated.(Model)
	if len(m2.activeTools) != 0 || m2.stallLine != "" {
		t.Fatalf("malformed progress event mutated state: tools=%v stall=%q", m2.activeTools, m2.stallLine)
	}
}

func mustProgressBody(t *testing.T, payload ipc.ProgressPayload) string {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return string(body)
}
