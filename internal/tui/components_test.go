package tui

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
)

func TestModel_ComponentEmbeddingPromotesFields(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("hello")

	m := testChatModelWithTextarea(ta)
	m.messages = []chatMessage{{Sender: "Igor", Text: "hi"}}
	m.width = 100
	m.showSidebar = false

	if len(m.transcriptModel.messages) != 1 {
		t.Fatalf("expected transcript messages on embedded struct, got %d", len(m.transcriptModel.messages))
	}
	if m.inputModel.textarea.Value() != "hello" {
		t.Fatalf("expected input textarea on embedded struct, got %q", m.inputModel.textarea.Value())
	}
	if m.chromeModel.width != 100 {
		t.Fatalf("expected chrome width on embedded struct, got %d", m.chromeModel.width)
	}
}