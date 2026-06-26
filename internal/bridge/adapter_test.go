package bridge

import (
	"testing"

	"github.com/igormaneschy/aurelia/internal/engine"
)

func TestToEngineEvent_Assistant(t *testing.T) {
	ev := toEngineEvent(Event{Type: "assistant", Text: "hello"})
	if ev.Type != engine.EventTypeText || ev.RawType != "assistant" {
		t.Fatalf("got type=%v raw=%q", ev.Type, ev.RawType)
	}
	if ev.ContentText() != "hello" {
		t.Fatalf("content = %q", ev.ContentText())
	}
}

func TestToEngineEvent_ToolUse(t *testing.T) {
	ev := toEngineEvent(Event{Type: "tool_use", Name: "Read", Input: map[string]any{"path": "x.go"}})
	if ev.Type != engine.EventTypeToolUse || ev.Name != "Read" {
		t.Fatalf("unexpected tool event: %+v", ev)
	}
	if ev.Input == "" {
		t.Fatal("expected JSON input")
	}
}

func TestToEngineEvent_Result(t *testing.T) {
	ev := toEngineEvent(Event{
		Type:         "result",
		InputTokens:  10,
		OutputTokens: 20,
		CostUSD:      0.01,
		NumTurns:     2,
	})
	if ev.Type != engine.EventTypeDone {
		t.Fatalf("type = %v", ev.Type)
	}
	if !ev.IsTerminal() {
		t.Fatal("expected terminal")
	}
}

func TestToEngineEvent_Error(t *testing.T) {
	ev := toEngineEvent(Event{Type: "error", Message: "boom"})
	if ev.Type != engine.EventTypeError || ev.Err == nil {
		t.Fatalf("expected error event, got %+v", ev)
	}
}

func TestToEngineEvent_System(t *testing.T) {
	ev := toEngineEvent(Event{
		Type:        "system",
		SessionID:   "sid",
		SessionFile: "/tmp/sess.json",
		Model:       "gpt-4",
		Tools:       []string{"Bash"},
	})
	if ev.Type != engine.EventTypeSystem {
		t.Fatalf("type = %v", ev.Type)
	}
	if ev.SessionFile != "/tmp/sess.json" || ev.Model != "gpt-4" {
		t.Fatalf("system meta lost: %+v", ev)
	}
}

func TestToBridgeRequest_SecurityAndImages(t *testing.T) {
	persist := true
	req := engine.Request{
		Prompt:       "hi",
		SessionKey:   "/tmp/s.json",
		Provider:     "openai",
		Model:        "gpt-4",
		Cwd:          "/work",
		Continue:     true,
		PersistSession: &persist,
		Images: []engine.Image{{Data: "abc", MediaType: "image/png"}},
		Security: &engine.SecurityPolicy{
			Enabled: true,
			Profile: "execute_safe",
			ChatID:  1,
		},
	}
	br := toBridgeRequest(req)
	if br.Prompt != "hi" || br.Options.Resume != "/tmp/s.json" {
		t.Fatalf("basic mapping failed: %+v", br)
	}
	if br.Options.Security == nil || br.Options.Security.Profile != "execute_safe" {
		t.Fatal("security not mapped")
	}
	if len(br.Options.Images) != 1 || br.Options.Images[0].Data != "abc" {
		t.Fatal("images not mapped")
	}
}

func TestToBridgeCommand_Steer(t *testing.T) {
	br := toBridgeCommand(engine.Command{Name: "steer", Payload: "stop", ChatID: 9})
	if br.Command != "steer" || br.Prompt != "stop" || br.Options.ChatID != 9 {
		t.Fatalf("steer mapping failed: %+v", br)
	}
}