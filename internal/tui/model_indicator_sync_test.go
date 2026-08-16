package tui

import (
	"errors"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// ── Classifier ──

func TestIsModelChangeCommand(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"/model", false},          // bare: opens the wizard
		{"/model refresh", false},  // catalog reload, not a change
		{"/model auto", true},      // automatic selection
		{"/model model-b", true},   // textual change
		{"/model   model-b", true}, // extra spaces tolerated
		{"/cwd /tmp", false},       // unrelated command
		{"hello", false},           // normal message
	}
	for _, tc := range cases {
		if got := isModelChangeCommand(tc.text); got != tc.want {
			t.Errorf("isModelChangeCommand(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

// ── Flag setting on every selection path (A2) ──

// TestModelChangeRefresh_DirectCommandSetsFlag covers the textual path:
// /model <name> marks a post-command refresh; /model refresh and normal
// messages never do.
func TestModelChangeRefresh_DirectCommandSetsFlag(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/model model-b")
	m := testChatModelWithTextarea(ta)
	updated, _ := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)
	if !m2.refreshStatusOnStreamEnd {
		t.Fatal("expected refreshStatusOnStreamEnd=true after /model model-b")
	}

	ta.SetValue("/model refresh")
	m = testChatModelWithTextarea(ta)
	updated, _ = m.Update(keyPress(tea.KeyEnter))
	m2 = updated.(Model)
	if m2.refreshStatusOnStreamEnd {
		t.Fatal("expected refreshStatusOnStreamEnd=false after /model refresh")
	}

	ta.SetValue("normal message")
	m = testChatModelWithTextarea(ta)
	updated, _ = m.Update(keyPress(tea.KeyEnter))
	m2 = updated.(Model)
	if m2.refreshStatusOnStreamEnd {
		t.Fatal("expected refreshStatusOnStreamEnd=false for a normal message")
	}
}

// TestModelChangeRefresh_WizardSetsFlag covers selection via the wizard form.
func TestModelChangeRefresh_WizardSetsFlag(t *testing.T) {
	m := testChatModel()
	next, cmd := m.submitModelSelection("model-b")
	if cmd == nil {
		t.Fatal("expected command from submitModelSelection")
	}
	if !next.refreshStatusOnStreamEnd {
		t.Fatal("expected refreshStatusOnStreamEnd=true after wizard selection")
	}
}

// TestModelChangeRefresh_QueueSetsFlag covers a queued /model command: the
// flag is set when the queued item starts, and only for model commands.
func TestModelChangeRefresh_QueueSetsFlag(t *testing.T) {
	m := testChatModel()
	if err := m.enqueueMessage(queuedMessage{chatID: 1, text: "/model model-b", isCommand: true}); err != nil {
		t.Fatal(err)
	}
	next, cmd := m.startQueuedMessage()
	if cmd == nil {
		t.Fatal("expected command from startQueuedMessage")
	}
	if !next.refreshStatusOnStreamEnd {
		t.Fatal("expected refreshStatusOnStreamEnd=true for queued /model command")
	}

	m = testChatModel()
	if err := m.enqueueMessage(queuedMessage{chatID: 1, text: "/new", isCommand: true}); err != nil {
		t.Fatal(err)
	}
	next, _ = m.startQueuedMessage()
	if next.refreshStatusOnStreamEnd {
		t.Fatal("expected refreshStatusOnStreamEnd=false for queued non-model command")
	}
}

// TestModelChangeRefresh_PendingSessionModelSetsFlag covers the model pending
// on a freshly created session.
func TestModelChangeRefresh_PendingSessionModelSetsFlag(t *testing.T) {
	m := testChatModel()
	m.pendingSessionModel = "model-b"
	updated, _ := m.Update(tuiSessionCreatedMsg{session: tuiSessionInfo{ChatID: 42}})
	m2 := updated.(Model)
	if !m2.refreshStatusOnStreamEnd {
		t.Fatal("expected refreshStatusOnStreamEnd=true for pending session model")
	}
	if m2.pendingSessionModel != "" {
		t.Fatalf("pendingSessionModel = %q, want cleared", m2.pendingSessionModel)
	}
}

// ── Flag consumption on terminal events ──

// TestModelChangeRefresh_StreamEndConsumesFlag covers the canonical path:
// stream_end clears the marker and schedules a status refresh.
func TestModelChangeRefresh_StreamEndConsumesFlag(t *testing.T) {
	m := testChatModel()
	m.streamID = 5
	m.refreshStatusOnStreamEnd = true

	updated, cmd := m.Update(streamEventMsg{streamID: 5, event: ipc.IPCEvent{Type: "stream_end"}})
	m2 := updated.(Model)
	if m2.refreshStatusOnStreamEnd {
		t.Fatal("expected refreshStatusOnStreamEnd cleared after stream_end")
	}
	if cmd == nil {
		t.Fatal("expected a command batch (refresh) after stream_end")
	}
}

// TestModelChangeRefresh_ClearedOnTerminalFailures covers A3: error event,
// unexpected EOF and stream errors all clear the marker without refreshing.
func TestModelChangeRefresh_ClearedOnTerminalFailures(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.Msg
	}{
		{"error event", streamEventMsg{streamID: 1, event: ipc.IPCEvent{Type: "error", Error: "boom"}}},
		{"streamDone EOF", streamDoneMsg{streamID: 1}},
		{"streamErr", streamErrMsg{streamID: 1, err: errors.New("conn lost")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := testChatModel()
			m.streamID = 1
			m.refreshStatusOnStreamEnd = true
			updated, _ := m.Update(tc.msg)
			m2 := updated.(Model)
			if m2.refreshStatusOnStreamEnd {
				t.Fatal("expected refreshStatusOnStreamEnd cleared on failure")
			}
		})
	}
}

// ── Status application rules (A1/A3) ──

// TestStatusMsg_StaleSeqDropped covers the seq guard: an older intermediate
// status response arriving late must not overwrite the post-command refresh.
func TestStatusMsg_StaleSeqDropped(t *testing.T) {
	m := testChatModel()
	updated, _ := m.Update(tuiStatusMsg{model: "model-b", seq: 2})
	m2 := updated.(Model)
	if m2.activeModel != "model-b" {
		t.Fatalf("activeModel = %q, want model-b", m2.activeModel)
	}
	// Stale response (seq 1 < 2) arrives late: dropped.
	updated, _ = m2.Update(tuiStatusMsg{model: "model-a", seq: 1})
	m3 := updated.(Model)
	if m3.activeModel != "model-b" {
		t.Fatalf("activeModel = %q, want model-b (stale dropped)", m3.activeModel)
	}
}

// TestStatusMsg_EmptyModelKeepsLastConfirmed covers A3: a status response
// without a model must never clear the last confirmed model.
func TestStatusMsg_EmptyModelKeepsLastConfirmed(t *testing.T) {
	m := testChatModel()
	m.activeModel = "model-a"
	updated, _ := m.Update(tuiStatusMsg{model: "", seq: 3})
	m2 := updated.(Model)
	if m2.activeModel != "model-a" {
		t.Fatalf("activeModel = %q, want model-a (empty model never replaces)", m2.activeModel)
	}
}

// TestStatusMsg_ErrorKeepsState covers A3: a failed status refresh leaves the
// last confirmed model untouched.
func TestStatusMsg_ErrorKeepsState(t *testing.T) {
	m := testChatModel()
	m.activeModel = "model-a"
	updated, _ := m.Update(tuiStatusMsg{err: errors.New("daemon offline"), seq: 4})
	m2 := updated.(Model)
	if m2.activeModel != "model-a" {
		t.Fatalf("activeModel = %q, want model-a after failed refresh", m2.activeModel)
	}
}

// TestModelChangeRefresh_EndToEnd covers A1 end-to-end at the model level:
// textual change → stream_end refresh → tuiStatusMsg updates the indicator.
func TestModelChangeRefresh_EndToEnd(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("/model model-b")
	m := testChatModelWithTextarea(ta)
	updated, _ := m.Update(keyPress(tea.KeyEnter))
	m2 := updated.(Model)
	if !m2.refreshStatusOnStreamEnd {
		t.Fatal("expected refresh flag after /model model-b")
	}

	// Command stream ends: flag consumed, refresh scheduled.
	m2.streamID = 1
	updated, _ = m2.Update(streamEventMsg{streamID: 1, event: ipc.IPCEvent{Type: "stream_end"}})
	m3 := updated.(Model)

	// Canonical status arrives and header/sidebar sources update.
	updated, _ = m3.Update(tuiStatusMsg{model: "model-b", seq: 1})
	m4 := updated.(Model)
	if m4.activeModel != "model-b" {
		t.Fatalf("activeModel = %q, want model-b after end-to-end flow", m4.activeModel)
	}
}
