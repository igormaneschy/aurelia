package telegram

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gopkg.in/telebot.v3"

	pipelinepkg "github.com/igormaneschy/aurelia/internal/pipeline"
)

// TestProgressReporter_ReportState_NoBotUpdatesStatusLine covers the
// state-machine core of the receipt with no bot wired (ausência de bot):
// stall/waiting set the status line, working clears it, and terminal states
// are no-ops because the caller deletes the receipt afterwards.
func TestProgressReporter_ReportState_NoBotUpdatesStatusLine(t *testing.T) {
	p := &progressReporter{}

	p.ReportState(pipelinepkg.ProgressStateStallWarning, "")
	p.mu.Lock()
	if !strings.Contains(p.statusLine, "⚠️") {
		t.Fatalf("stall_warning must set a warning line, got %q", p.statusLine)
	}
	p.mu.Unlock()

	p.ReportState(pipelinepkg.ProgressStateStallUrgent, "")
	p.mu.Lock()
	if !strings.Contains(p.statusLine, "🚨") {
		t.Fatalf("stall_urgent must set an urgent line, got %q", p.statusLine)
	}
	p.mu.Unlock()

	p.ReportState(pipelinepkg.ProgressStateWaiting, "")
	p.mu.Lock()
	if !strings.Contains(p.statusLine, "⏳") {
		t.Fatalf("waiting must set a processing line, got %q", p.statusLine)
	}
	p.mu.Unlock()

	// Working resumes productive activity — the status line must clear.
	p.ReportState(pipelinepkg.ProgressStateWorking, "")
	p.mu.Lock()
	if p.statusLine != "" {
		t.Fatalf("working must clear the status line, got %q", p.statusLine)
	}
	p.mu.Unlock()
}

// TestProgressReporter_ReportState_TerminalStatesAreNoOps covers término and
// cancelamento: done/canceled/failed must not mutate the receipt state — the
// run caller deletes the receipt once the final reply/error is out.
func TestProgressReporter_ReportState_TerminalStatesAreNoOps(t *testing.T) {
	p := &progressReporter{}
	p.ReportState(pipelinepkg.ProgressStateStallWarning, "")
	p.mu.Lock()
	before := p.statusLine
	p.mu.Unlock()

	for _, state := range []pipelinepkg.ProgressState{
		pipelinepkg.ProgressStateDone,
		pipelinepkg.ProgressStateCanceled,
		pipelinepkg.ProgressStateFailed,
	} {
		p.ReportState(state, "")
		p.mu.Lock()
		if p.statusLine != before {
			t.Fatalf("terminal state %s must not change the receipt, got %q (want %q)",
				state, p.statusLine, before)
		}
		p.mu.Unlock()
	}
}

// TestProgressReporter_ReportState_ThrottledWithoutBotEdit covers the edit
// boundary: when the receipt exists but edits are throttled, ReportState
// must keep the receipt unchanged and never panic (erro de edição).
func TestProgressReporter_ReportState_ThrottledWithoutBotEdit(t *testing.T) {
	p := &progressReporter{
		msg:      nil, // no message yet → would Send, but no bot → silent
		lastText: "",
	}
	// No bot: state changes stay internal; no panic, no send.
	p.ReportState(pipelinepkg.ProgressStateWaiting, "")
	p.mu.Lock()
	if p.statusLine == "" {
		t.Fatal("waiting must set the status line even without a bot")
	}
	p.mu.Unlock()
}

// TestProgressReporter_BuildDisplay_IncludesStatusLine verifies the status
// line renders inside the single receipt display.
func TestProgressReporter_BuildDisplay_IncludesStatusLine(t *testing.T) {
	p := &progressReporter{startTime: time.Now()}
	p.ReportTool("Bash", "ls -la")
	p.ReportState(pipelinepkg.ProgressStateStallUrgent, "")
	p.mu.Lock()
	display := p.buildDisplay()
	p.mu.Unlock()

	if !strings.Contains(display, "🚨") {
		t.Fatalf("display must include the stall urgent line, got %q", display)
	}
	if !strings.Contains(display, "bash") {
		t.Fatalf("display must keep the tool line, got %q", display)
	}
}

// fakeDeliverer records Send/Edit calls so tests can verify the single
// receipt contract without a network bot.
type fakeDeliverer struct {
	sendCount int
	editCount int
	sent      *telebot.Message
	sendErr   error
	editErr   error
}

func (f *fakeDeliverer) Send(_ *telebot.Chat, _ string, _ int) (*telebot.Message, error) {
	f.sendCount++
	f.sent = &telebot.Message{ID: 1}
	return f.sent, f.sendErr
}

func (f *fakeDeliverer) Edit(_ *telebot.Message, _ string) error {
	f.editCount++
	return f.editErr
}

// TestProgressReporter_SingleReceiptContract is the headline T2 contract:
// the first update SENDS one receipt and every later update EDITS the same
// message — never a second send. The 1.5s throttle keeps edits rate-safe.
func TestProgressReporter_SingleReceiptContract(t *testing.T) {
	fake := &fakeDeliverer{}
	p := &progressReporter{chat: &telebot.Chat{ID: 1}, deliver: fake, startTime: time.Now()}

	p.ReportTool("Bash", "ls")
	if fake.sendCount != 1 || fake.editCount != 0 {
		t.Fatalf("first update: send=%d edit=%d, want send=1 edit=0", fake.sendCount, fake.editCount)
	}
	if p.msg == nil {
		t.Fatal("receipt message must be tracked after first send")
	}

	// Open the throttle window, then update again — must EDIT the same receipt.
	p.mu.Lock()
	p.lastEdit = time.Now().Add(-2 * time.Second)
	p.mu.Unlock()
	p.ReportToolResult("done")
	if fake.sendCount != 1 || fake.editCount != 1 {
		t.Fatalf("second update: send=%d edit=%d, want send=1 edit=1", fake.sendCount, fake.editCount)
	}

	// Immediately after an edit the throttle blocks delivery entirely.
	before := fake.editCount
	p.ReportText("thought")
	if fake.editCount != before {
		t.Fatalf("throttled update must not deliver, edit=%d want %d", fake.editCount, before)
	}

	// A stall state edits the same receipt again once the throttle opens.
	p.mu.Lock()
	p.lastEdit = time.Now().Add(-2 * time.Second)
	p.mu.Unlock()
	p.ReportState(pipelinepkg.ProgressStateStallUrgent, "")
	if fake.sendCount != 1 || fake.editCount != before+1 {
		t.Fatalf("state update: send=%d edit=%d, want send=1 edit=%d", fake.sendCount, fake.editCount, before+1)
	}
}

// TestProgressReporter_EditErrorKeepsReceipt covers erro de edição: a failed
// edit is logged, the receipt survives, and the next update retries.
func TestProgressReporter_EditErrorKeepsReceipt(t *testing.T) {
	fake := &fakeDeliverer{editErr: errors.New("edit failed")}
	p := &progressReporter{chat: &telebot.Chat{ID: 1}, deliver: fake, startTime: time.Now()}

	p.ReportTool("Read", "x.go")
	p.mu.Lock()
	p.lastEdit = time.Now().Add(-2 * time.Second)
	p.mu.Unlock()
	p.ReportToolResult("ok")
	if fake.editCount != 1 {
		t.Fatalf("editCount = %d, want 1 (edit attempted)", fake.editCount)
	}
	p.mu.Lock()
	if p.msg == nil {
		p.mu.Unlock()
		t.Fatal("receipt must survive an edit error")
	}
	p.mu.Unlock()

	// Next update retries the edit on the same receipt.
	p.mu.Lock()
	p.lastEdit = time.Now().Add(-2 * time.Second)
	p.mu.Unlock()
	p.ReportText("next")
	if fake.editCount != 2 {
		t.Fatalf("editCount = %d, want 2 (continue after error)", fake.editCount)
	}
	if fake.sendCount != 1 {
		t.Fatalf("sendCount = %d, want 1 (never re-send)", fake.sendCount)
	}
}

// TestProgressReporter_SendErrorNoReceipt covers a failed first send: no
// receipt is tracked, so the next update tries to send again.
func TestProgressReporter_SendErrorNoReceipt(t *testing.T) {
	fake := &fakeDeliverer{sendErr: errors.New("send failed")}
	p := &progressReporter{chat: &telebot.Chat{ID: 1}, deliver: fake, startTime: time.Now()}

	p.ReportTool("Bash", "ls")
	if fake.sendCount != 1 {
		t.Fatalf("sendCount = %d, want 1", fake.sendCount)
	}
	p.mu.Lock()
	if p.msg != nil {
		p.mu.Unlock()
		t.Fatal("msg must stay nil after a failed send")
	}
	p.mu.Unlock()

	p.ReportText("thought")
	if fake.sendCount != 2 {
		t.Fatalf("sendCount = %d, want 2 (retry send)", fake.sendCount)
	}
}
