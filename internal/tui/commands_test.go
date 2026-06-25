package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestIsBareModelCommand(t *testing.T) {
	if !isBareModelCommand("/model") || isBareModelCommand("/model gpt-4") {
		t.Fatal("bad")
	}
}

func TestCatalogFromDaemonText_GroupsByProvider(t *testing.T) {
	body := "Current model: **PI default**\n\n**Available models:**\n\nanthropic:\n  `claude-sonnet-4-6` 📷\nopenai:\n  `gpt-5.1`\n"
	catalog := catalogFromDaemonText(body)
	if catalog.providerCount() != 2 {
		t.Fatalf("expected 2 providers, got %d", catalog.providerCount())
	}
	if !containsString(catalog.byProvider["anthropic"], "claude-sonnet-4-6") {
		t.Fatalf("missing anthropic model: %#v", catalog.byProvider)
	}
	if !containsString(catalog.byProvider["openai"], "gpt-5.1") {
		t.Fatalf("missing openai model: %#v", catalog.byProvider)
	}
}

func TestOpenModelSelect_StartsWithProviderStep(t *testing.T) {
	m := testChatModel()
	next, cmd := m.openModelSelect()
	if !next.formOpen || next.activeForm == nil || cmd == nil {
		t.Fatal("bad")
	}
	if next.activeForm.kind != formKindModelProvider {
		t.Fatalf("expected provider step, got %v", next.activeForm.kind)
	}
}

func TestAdvanceModelWizard_ProviderAutoSubmits(t *testing.T) {
	m := testChatModel()
	hf := &huhForm{kind: formKindModelProvider, selected: "auto"}
	next, cmd := m.advanceModelWizard(hf)
	if next.formOpen {
		t.Fatal("expected form closed after auto")
	}
	if cmd == nil {
		t.Fatal("expected submit command")
	}
}

func TestAdvanceModelWizard_ProviderOpensModelStep(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	catalog := catalogFromDaemonText("openai:\n  `gpt-5.1`\nanthropic:\n  `claude-sonnet-4-6`\n")
	hf := &huhForm{kind: formKindModelProvider, selected: "openai", catalog: catalog}
	next, cmd := m.advanceModelWizard(hf)
	if !next.formOpen || next.activeForm == nil {
		t.Fatal("expected model step form")
	}
	if next.activeForm.kind != formKindModelName {
		t.Fatalf("expected model step, got %v", next.activeForm.kind)
	}
	if next.activeForm.provider != "openai" {
		t.Fatalf("provider = %q", next.activeForm.provider)
	}
	if cmd == nil {
		t.Fatal("expected init command")
	}
}

func TestCloseForm_ClearsState(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newModelProviderForm(modelCatalog{}, "auto")
	c := m.closeForm()
	if c.formOpen || c.activeForm != nil {
		t.Fatal("bad")
	}
}

func TestBareModelSubmitOpensFormNotIPC(t *testing.T) {
	m := testChatModel()
	m.textarea.SetValue("/model")
	next, cmd := m.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !next.(Model).formOpen || cmd == nil {
		t.Fatal("bad")
	}
}

func TestCtrlOTogglesMouseWhileFormOpen(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newModelProviderForm(modelCatalog{}, "auto")
	m.mouseEnabled = true

	updated, _ := m.updateChat(keyCtrl('o'))
	m2 := updated.(Model)
	if m2.mouseEnabled {
		t.Fatal("expected ctrl+o to disable mouse while form is open")
	}
	if !m2.formOpen {
		t.Fatal("expected form to stay open")
	}
}

func TestRenderFormOverlay_UsesPanel(t *testing.T) {
	m := testChatModel()
	m.width = 60
	m.height = 10
	m.formOpen = true
	m.activeForm = newModelProviderForm(modelCatalog{}, "auto")
	if m.renderFormOverlay(strings.Repeat("L", 60)) == strings.Repeat("L", 60) {
		t.Fatal("bad")
	}
}