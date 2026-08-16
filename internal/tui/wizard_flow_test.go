package tui

import (
	"encoding/json"
	"testing"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

func TestModelWizardFlow_LlamacppProviderShowsModel(t *testing.T) {
	body := `[{"provider":"opencode-go","id":"alpha"},{"provider":"llamacpp-tailscale","id":"Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"}]`
	catalog := catalogFromIPCModels([]ipc.IPCEvent{{Type: ipc.EventTypeModels, Body: body}})
	if len(catalog.byProvider["llamacpp-tailscale"]) != 1 {
		t.Fatalf("catalog = %#v", catalog.byProvider)
	}

	m := testChatModel()
	m.activeModel = "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"
	m.formOpen = true
	m.activeForm = newModelProviderForm(catalog, m.activeModel)
	m.activeForm.selected = "llamacpp-tailscale"

	next, _ := m.advanceModelWizard(m.activeForm)
	if next.activeForm == nil || next.activeForm.kind != formKindModelName {
		t.Fatalf("expected model step, got %#v", next.activeForm)
	}
	if next.activeForm.chosenModel() != "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf" {
		t.Fatalf("selected = %q", next.activeForm.chosenModel())
	}
	if len(next.activeForm.catalog.modelOptions("llamacpp-tailscale")) != 1 {
		t.Fatalf("options = %#v", next.activeForm.catalog.modelOptions("llamacpp-tailscale"))
	}
}

func TestModelWizardReload_OnModelStepRefreshesLlamacpp(t *testing.T) {
	initial := catalogFromIPCModels([]ipc.IPCEvent{{
		Type: ipc.EventTypeModels,
		Body: mustJSON([]ipc.TUIModelEntry{{Provider: "llamacpp-tailscale", ID: "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"}}),
	}})
	m := testChatModel()
	m.formOpen = true
	m.activeForm = newModelNameForm(initial, "llamacpp-tailscale", "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf")

	refreshed := catalogFromIPCModels([]ipc.IPCEvent{{
		Type: ipc.EventTypeModels,
		Body: mustJSON([]ipc.TUIModelEntry{
			{Provider: "opencode-go", ID: "new-model"},
			{Provider: "llamacpp-tailscale", ID: "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"},
		}),
	}})
	next := m.applyWizardCatalog(tuiModelsMsg{catalog: refreshed, reloaded: true})

	if next.activeForm.kind != formKindModelName {
		t.Fatalf("expected model step after reload, got %v", next.activeForm.kind)
	}
	if next.activeForm.chosenModel() != "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf" {
		t.Fatalf("selected = %q", next.activeForm.chosenModel())
	}
}

func TestCatalogProviderKey_ResolvesLlamacppShorthand(t *testing.T) {
	catalog := catalogFromIPCModels([]ipc.IPCEvent{{
		Type: ipc.EventTypeModels,
		Body: mustJSON([]ipc.TUIModelEntry{{Provider: "llamacpp-tailscale", ID: "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"}}),
	}})
	if got := catalog.catalogProviderKey("llamacpp"); got != "llamacpp-tailscale" {
		t.Fatalf("catalogProviderKey(llamacpp) = %q", got)
	}
	if len(catalog.modelsForProvider("llamacpp", "")) != 1 {
		t.Fatalf("modelsForProvider = %#v", catalog.modelsForProvider("llamacpp", ""))
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
