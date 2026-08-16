package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestIsBareModelCommand(t *testing.T) {
	if !isBareModelCommand("/model") || isBareModelCommand("/model gpt-4") {
		t.Fatal("bad")
	}
}

func TestCatalogFromIPCModels_IncludesLateLocalProvider(t *testing.T) {
	events := []ipc.IPCEvent{{
		Type: ipc.EventTypeModels,
		Body: `[{"provider":"openai","id":"gpt-5.1"},{"provider":"llamacpp-tailscale","id":"qwen3"}]`,
	}}
	catalog := catalogFromIPCModels(events)
	if !containsString(catalog.byProvider["llamacpp-tailscale"], "qwen3") {
		t.Fatalf("missing llamacpp-tailscale model: %#v", catalog.byProvider)
	}
}

func TestCatalogFromDaemonText_HeaderWithoutModelsOmitsProvider(t *testing.T) {
	body := "openai:\n  `gpt-5.1`\nllamacpp-tailscale:\n"
	catalog := catalogFromDaemonText(body)
	if containsString(catalog.providers, "llamacpp-tailscale") {
		t.Fatalf("provider header without model lines must not appear in catalog: %#v", catalog.byProvider)
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

func TestModelProviderForm_BindsSelectionPointer(t *testing.T) {
	catalog := catalogFromDaemonText("openai:\n  `gpt-5.1`\nanthropic:\n  `claude-sonnet-4-6`\n")
	hf := newModelProviderForm(catalog, "auto")
	hf.selected = "anthropic"
	if hf.chosenModel() != "anthropic" {
		t.Fatalf("expected bound selection anthropic, got %q", hf.chosenModel())
	}
}

func TestAdvanceModelWizard_ProviderOpensModelStep(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	catalog := catalogFromDaemonText("openai:\n  `gpt-5.1`\nanthropic:\n  `claude-sonnet-4-6`\n")
	hf := newModelProviderForm(catalog, "auto")
	hf.selected = "openai"
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

func TestHandleModelWizardKey_EscClosesForm(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newModelProviderForm(modelCatalog{}, "auto")
	next, cmd, handled := m.handleModelWizardKey(keyPress(tea.KeyEsc))
	if !handled || cmd != nil {
		t.Fatalf("handled=%v cmd=%v", handled, cmd)
	}
	if next.formOpen || next.activeForm != nil {
		t.Fatal("expected form closed")
	}
}

func TestHandleModelWizardKey_BackReturnsToProvider(t *testing.T) {
	m := testChatModel()
	catalog := catalogFromDaemonText("openai:\n  `gpt-5.1`\nanthropic:\n  `claude-sonnet-4-6`\n")
	m.formOpen = true
	m.activeForm = newModelNameForm(catalog, "openai", "gpt-5.1")
	next, cmd, handled := m.handleModelWizardKey(keyText("b"))
	if !handled || cmd == nil {
		t.Fatalf("handled=%v cmd=%v", handled, cmd)
	}
	if !next.formOpen || next.activeForm == nil {
		t.Fatal("expected form still open")
	}
	if next.activeForm.kind != formKindModelProvider {
		t.Fatalf("expected provider step, got %v", next.activeForm.kind)
	}
	if next.activeForm.selected != "openai" {
		t.Fatalf("provider selection = %q", next.activeForm.selected)
	}
}

func TestRefreshModelSelectForm_PreservesProviderSelection(t *testing.T) {
	initial := catalogFromDaemonText("openai:\n  `gpt-5.1`\n")
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newModelProviderForm(initial, "auto")
	m.activeForm.selected = "openai"

	updated := catalogFromDaemonText("openai:\n  `gpt-5.1`\nanthropic:\n  `claude-sonnet-4-6`\n")
	next := m.refreshModelSelectForm(updated)

	if next.activeForm.selected != "openai" {
		t.Fatalf("selected = %q, want openai", next.activeForm.selected)
	}
	if next.activeForm.catalog.providerCount() != 2 {
		t.Fatalf("expected 2 providers after refresh, got %d", next.activeForm.catalog.providerCount())
	}
}

func TestUpdateChat_TuiModelsMsgWhileFormOpen_UpdatesCatalog(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newModelProviderForm(catalogFromModels(modelFallbackList("Qwen3.6-35B")), "Qwen3.6-35B")
	if m.activeForm.catalog.providerCount() != 1 {
		t.Fatalf("fallback providers=%d, want 1 (other)", m.activeForm.catalog.providerCount())
	}

	full := catalogFromDaemonText("openai:\n  `gpt-5.1`\nanthropic:\n  `claude-sonnet-4-6`\n")
	next, cmd := m.updateChat(tuiModelsMsg{catalog: full})
	nm := next.(Model)
	if !nm.formOpen || nm.activeForm == nil {
		t.Fatal("model wizard should stay open")
	}
	if nm.activeForm.catalog.providerCount() < 2 {
		t.Fatalf("providers=%d after updateChat, want full catalog", nm.activeForm.catalog.providerCount())
	}
	if cmd == nil {
		t.Fatal("expected initActiveForm after catalog update")
	}
}

func TestApplyWizardCatalog_FailedReloadKeepsExistingCatalog(t *testing.T) {
	catalog := catalogFromDaemonText("openai:\n  `gpt-5.1`\nanthropic:\n  `claude-sonnet-4-6`\n")
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newModelProviderForm(catalog, "auto")
	m.activeForm.selected = "anthropic"

	next := m.applyWizardCatalog(tuiModelsMsg{reloaded: true, err: assertErr("timeout")})

	if next.activeForm.catalog.providerCount() != 2 {
		t.Fatalf("expected existing catalog preserved, got %d providers", next.activeForm.catalog.providerCount())
	}
	if next.activeForm.selected != "anthropic" {
		t.Fatalf("selected = %q, want anthropic", next.activeForm.selected)
	}
}

func TestRefreshModelSelectForm_ModelStepKeepsSelection(t *testing.T) {
	catalog := catalogFromDaemonText("openai:\n  `gpt-5.1`\nanthropic:\n  `claude-sonnet-4-6`\n")
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newModelNameForm(catalog, "openai", "gpt-5.1")

	updated := catalogFromDaemonText("openai:\n  `gpt-5.1`\n  `gpt-5.2`\nanthropic:\n  `claude-sonnet-4-6`\n")
	next := m.refreshModelSelectForm(updated)

	if next.activeForm.kind != formKindModelName {
		t.Fatalf("expected model step, got %v", next.activeForm.kind)
	}
	if next.activeForm.chosenModel() != "gpt-5.1" {
		t.Fatalf("selected model = %q, want gpt-5.1", next.activeForm.chosenModel())
	}
	if !containsString(next.activeForm.catalog.byProvider["openai"], "gpt-5.2") {
		t.Fatalf("expected refreshed openai models, got %#v", next.activeForm.catalog.byProvider["openai"])
	}
}

func TestHandleModelWizardKey_ReloadFetchesModels(t *testing.T) {
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newModelProviderForm(modelCatalog{}, "auto")
	next, cmd, handled := m.handleModelWizardKey(keyText("r"))
	if !handled || cmd == nil {
		t.Fatalf("handled=%v cmd=%v", handled, cmd)
	}
	if !next.formOpen || next.activeForm == nil {
		t.Fatal("expected form still open during reload")
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
