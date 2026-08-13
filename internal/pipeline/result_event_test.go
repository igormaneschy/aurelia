package pipeline

import (
	"strings"
	"sync"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/transport"
	"github.com/igormaneschy/aurelia/pkg/idgen"
)

// fakeOutput is a test double for the Output interface.
type fakeOutput struct {
	mu            sync.Mutex
	lastError     string
	lastReply     string
	confirmCalled bool
}

func (f *fakeOutput) StartTyping(_ int64, _ int) func() {
	return func() {}
}

func (f *fakeOutput) NewProgress(_ int64, _ int) ProgressReporter {
	return &fakeProgress{}
}

func (f *fakeOutput) SendError(_ int64, _ int, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastError = text
	return nil
}

func (f *fakeOutput) SendReply(_ int64, _ int, text string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastReply = text
	return 0, nil
}

func newOwnedResultService(t *testing.T, sessionFile string) (*Service, *spyRunLogStore, runOwnership, *activeRun) {
	t.Helper()
	spy := &spyRunLogStore{}
	output := &fakeOutput{}
	s := &Service{output: output, runLog: spy, runLogStates: make(map[string]*runLogState), sessions: session.NewStore()}
	owner := newActiveRun()
	runID := idgen.New()
	key := runLogKey(1, 0, 100)
	state := &runLogState{runID: runID, owner: owner}
	owner.runLogState = state
	s.runLogStates[key] = state
	s.activeSessions.Store(sessionKey(1, 0, 100), owner)
	if sessionFile != "" {
		s.sessions.SetSession(1, 0, 100, sessionFile)
	}
	return s, spy, runOwnership{runID: runID, owner: owner}, owner
}

func TestHandleResultEvent_LiveOwnerCompletesAndReplies(t *testing.T) {
	s, spy, ownership, _ := newOwnedResultService(t, "")
	var assistant strings.Builder
	outcome := s.handleResultEvent(1, 0, 7, bridge.Event{Type: "result", Content: "live answer"}, &assistant, "hello", 100, false, ownership)
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want success", outcome)
	}
	if got := s.output.(*fakeOutput).lastReply; got != "live answer" {
		t.Fatalf("reply = %q, want live answer", got)
	}
	comps := spy.recordedCompletions()
	if len(comps) != 1 || comps[0].runID != ownership.runID || comps[0].status != runlog.RunCompleted {
		t.Fatalf("completions = %+v, want one completed live run", comps)
	}
	terminalEvents := 0
	for _, ev := range spy.recordedEvents() {
		if ev.Phase != string(observability.PhaseRunCompleted) {
			continue
		}
		terminalEvents++
		if ev.RunID != ownership.runID {
			t.Fatalf("terminal event runID = %q, want %q", ev.RunID, ownership.runID)
		}
	}
	if terminalEvents != 1 {
		t.Fatalf("terminal events = %d, want exactly one", terminalEvents)
	}
}

func TestHandleResultEvent_LiveOwnerSessionFileCompletes(t *testing.T) {
	s, spy, ownership, _ := newOwnedResultService(t, "")
	const sessionFile = "/tmp/live-session.jsonl"
	var assistant strings.Builder
	outcome := s.handleResultEvent(1, 0, 7, bridge.Event{Type: "result", Content: "answer", SessionFile: sessionFile}, &assistant, "hello", 100, false, ownership)
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want success", outcome)
	}
	if got := s.sessions.GetSession(1, 0, 100); got != sessionFile {
		t.Fatalf("session file = %q, want %q", got, sessionFile)
	}
	updates := spy.recordedUpdates()
	if len(updates) != 1 || updates[0].RunID != ownership.runID || updates[0].SessionFile == nil || *updates[0].SessionFile != sessionFile {
		t.Fatalf("updates = %+v, want one SessionFile update for run %q", updates, ownership.runID)
	}
	comps := spy.recordedCompletions()
	if len(comps) != 1 || comps[0].runID != ownership.runID {
		t.Fatalf("completions = %+v, want one completion for %q", comps, ownership.runID)
	}
}

func (f *fakeOutput) SendText(_ int64, _ int, _ string) (transport.MessageHandle, error) {
	return nil, nil
}

func (f *fakeOutput) DeleteMessage(_ transport.MessageHandle) {}

func (f *fakeOutput) ConfirmMessage(_ int64, _ int) {
	f.confirmCalled = true
}

type fakeProgress struct{}

func (fakeProgress) ReportTool(_, _ string)           {}
func (fakeProgress) ReportToolResult(_ string)        {}
func (fakeProgress) ReportText(_ string)              {}
func (fakeProgress) ReportState(ProgressState, string) {}
func (fakeProgress) Delete()                          {}

func newTestService(output Output) *Service {
	return &Service{
		output:      output,
		sessions:    nil,
		nudgeBuffer: nil,
		dreamer:     nil,
		config:      nil,
	}
}

func TestHandleResultEvent_EmptyContent_ReturnsLLMError(t *testing.T) {
	fo := &fakeOutput{}
	s := newTestService(fo)

	ev := bridge.Event{Type: "result", Content: ""}
	var assistantText strings.Builder

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "hello", 100, false)

	if outcome != OutcomeLLMError {
		t.Fatalf("expected OutcomeLLMError, got %v", outcome)
	}
	if fo.lastError != bridgeEmptyResultMessage {
		t.Fatalf("expected error %q, got %q", bridgeEmptyResultMessage, fo.lastError)
	}
	if !fo.confirmCalled {
		t.Fatal("expected ConfirmMessage to be called")
	}
}

func TestHandleResultEvent_AssistantText_EmptyResult_ReturnsSuccess(t *testing.T) {
	fo := &fakeOutput{}
	s := newTestService(fo)

	ev := bridge.Event{Type: "result", Content: ""}
	var assistantText strings.Builder
	assistantText.WriteString("Resposta acumulada.")

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "hello", 100, false)

	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}
	if fo.lastReply != "Resposta acumulada." {
		t.Fatalf("expected reply %q, got %q", "Resposta acumulada.", fo.lastReply)
	}
	if !fo.confirmCalled {
		t.Fatal("expected ConfirmMessage to be called")
	}
}

func TestHandleResultEvent_ResultContent_ReturnsSuccess(t *testing.T) {
	fo := &fakeOutput{}
	s := newTestService(fo)

	ev := bridge.Event{Type: "result", Content: "Resposta direta do modelo."}
	var assistantText strings.Builder

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "hello", 100, false)

	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}
	if fo.lastReply != "Resposta direta do modelo." {
		t.Fatalf("expected reply %q, got %q", "Resposta direta do modelo.", fo.lastReply)
	}
	if !fo.confirmCalled {
		t.Fatal("expected ConfirmMessage to be called")
	}
}

func TestEventContent_PrefersTextOverContent(t *testing.T) {
	tests := []struct {
		name string
		ev   bridge.Event
		want string
	}{
		{name: "both empty", ev: bridge.Event{}, want: ""},
		{name: "content only", ev: bridge.Event{Content: "c"}, want: "c"},
		{name: "text only", ev: bridge.Event{Text: "t"}, want: "t"},
		{name: "text preferred", ev: bridge.Event{Text: "text", Content: "content"}, want: "text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := eventContent(tc.ev)
			if got != tc.want {
				t.Fatalf("eventContent(%+v) = %q, want %q", tc.ev, got, tc.want)
			}
		})
	}
}

func TestHandleResultEvent_TextContent_ReturnsSuccess(t *testing.T) {
	fo := &fakeOutput{}
	s := newTestService(fo)

	// eventContent prefers ev.Text over ev.Content
	ev := bridge.Event{Type: "result", Text: "Resposta via campo Text."}
	var assistantText strings.Builder

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "hello", 100, false)

	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}
	if fo.lastReply != "Resposta via campo Text." {
		t.Fatalf("expected reply %q, got %q", "Resposta via campo Text.", fo.lastReply)
	}
}

func TestHandleResultEvent_DeliversResultTextAsIs(t *testing.T) {
	fo := &fakeOutput{}
	s := newTestService(fo)

	content := "Vou executar.\n\n```aurelia-plan\n{\"tasks\":[{\"id\":\"T1\",\"prompt\":\"internal prompt\"}]}\n```"
	ev := bridge.Event{Type: "result", Content: content}
	var assistantText strings.Builder

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "hello", 100, false)

	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}
	if fo.lastReply != content {
		t.Fatalf("expected passthrough reply, got %q", fo.lastReply)
	}
	if !fo.confirmCalled {
		t.Fatal("expected ConfirmMessage to be called")
	}
}
