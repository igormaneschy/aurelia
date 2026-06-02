package pipeline

import (
	"strings"
	"sync"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
)

// fakeOutput is a test double for the Output interface.
type fakeOutput struct {
	lastError     string
	lastReply     string
	planExecuted  bool
	planThreadID  int
	planCwd       string
	planUserID    int64
	confirmCalled bool
	planDone      chan struct{}
	planDoneOnce  sync.Once
}

func (f *fakeOutput) StartTyping(_ int64, _ int) func() {
	return func() {}
}

func (f *fakeOutput) NewProgress(_ int64, _ int) ProgressReporter {
	return &fakeProgress{}
}

func (f *fakeOutput) SendError(_ int64, _ int, text string) error {
	f.lastError = text
	return nil
}

func (f *fakeOutput) SendReply(_ int64, _ int, text string) (int64, error) {
	f.lastReply = text
	return 0, nil
}

func (f *fakeOutput) SendText(_ int64, _ int, _ string) (any, error) {
	return nil, nil
}

func (f *fakeOutput) DeleteMessage(_ any) {}

func (f *fakeOutput) ConfirmMessage(_ int64, _ int) {
	f.confirmCalled = true
}

func (f *fakeOutput) ExecuteApprovedPlan(_ int64, threadID int, _ int, cwd string, userID int64, _ *orchestrator.Plan) {
	f.planExecuted = true
	f.planThreadID = threadID
	f.planCwd = cwd
	f.planUserID = userID
	if f.planDone != nil {
		f.planDoneOnce.Do(func() { close(f.planDone) })
	}
}

type fakeProgress struct{}

func (fakeProgress) ReportTool(_ string)       {}
func (fakeProgress) ReportToolResult(_ string) {}
func (fakeProgress) ReportText(_ string)       {}
func (fakeProgress) Delete()                   {}

func newTestService(output Output) *Service {
	return &Service{
		output:       output,
		sessions:     nil,
		nudgeBuffer:  nil,
		dreamer:      nil,
		orchestrator: nil,
		config:       nil,
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

func TestHandleResultEvent_StripsPlanBlockFromNormalReply(t *testing.T) {
	fo := &fakeOutput{}
	s := newTestService(fo)

	ev := bridge.Event{Type: "result", Content: "Vou executar.\n\n```aurelia-plan\n{\"tasks\":[{\"id\":\"T1\",\"description\":\"secret\",\"prompt\":\"internal prompt\",\"needs_worktree\":false}]}\n```"}
	var assistantText strings.Builder

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "hello", 100, false)

	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}
	if strings.Contains(fo.lastReply, "internal prompt") || strings.Contains(fo.lastReply, "aurelia-plan") {
		t.Fatalf("reply leaked plan internals: %q", fo.lastReply)
	}
	if !strings.Contains(fo.lastReply, "[plano de execução interno omitido]") {
		t.Fatalf("expected omission marker, got %q", fo.lastReply)
	}
}

func TestHandleResultEvent_InvalidPlanMarkerIsNotSentRaw(t *testing.T) {
	fo := &fakeOutput{}
	s := newTestService(fo)
	s.orchestrator = orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{})

	ev := bridge.Event{Type: "result", Content: "Now emit plan.\n\n```aurelia-plan\n{not valid json with prompt: secret}\n```"}
	var assistantText strings.Builder

	outcome := s.handleResultEvent(1, 0, 100, ev, &assistantText, "pode iniciar", 100, false)

	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}
	if fo.lastError == "" {
		t.Fatal("expected safe parse error message")
	}
	if strings.Contains(fo.lastError, "secret") || strings.Contains(fo.lastError, "aurelia-plan") {
		t.Fatalf("error leaked plan internals: %q", fo.lastError)
	}
	if fo.lastReply != "" {
		t.Fatalf("did not expect raw reply, got %q", fo.lastReply)
	}
}
