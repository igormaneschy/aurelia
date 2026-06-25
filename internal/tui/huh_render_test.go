package tui

import (
	"strings"
	"testing"

	tea1 "github.com/charmbracelet/bubbletea"
	"github.com/igormaneschy/aurelia/internal/ipc"
)

func initModelFormView(hf *huhForm) string {
	if cmd := hf.init(); cmd != nil {
		cmd()
	}
	_, _ = hf.form.Update(tea1.WindowSizeMsg{Width: 60, Height: 20})
	return hf.view()
}

func TestNewModelNameForm_ViewAfterInitShowsLlamacppModel(t *testing.T) {
	catalog := catalogFromIPCModels([]ipc.IPCEvent{{
		Type: ipc.EventTypeModels,
		Body: `[{"provider":"llamacpp-tailscale","id":"Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"}]`,
	}})
	hf := newModelNameForm(catalog, "llamacpp-tailscale", "")
	view := initModelFormView(hf)
	if !strings.Contains(view, "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf") {
		t.Fatalf("expected model in view after init, got:\n%s", view)
	}
}

func TestNewModelNameForm_EmptyCatalogAfterInitShowsAuto(t *testing.T) {
	hf := newModelNameForm(modelCatalog{}, "llamacpp-tailscale", "")
	view := initModelFormView(hf)
	if !strings.Contains(view, "auto") {
		t.Fatalf("expected auto in view, got:\n%s", view)
	}
}

func TestNewModelNameForm_WithoutWindowSizeHidesOptions(t *testing.T) {
	catalog := catalogFromIPCModels([]ipc.IPCEvent{{
		Type: ipc.EventTypeModels,
		Body: `[{"provider":"llamacpp-tailscale","id":"Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"}]`,
	}})
	hf := newModelNameForm(catalog, "llamacpp-tailscale", "")
	if cmd := hf.init(); cmd != nil {
		cmd()
	}
	view := hf.view()
	if strings.Contains(view, "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf") {
		t.Fatal("expected model hidden before window size")
	}
}

func TestInitActiveForm_ReturnsCommandWhenSized(t *testing.T) {
	m := testChatModel()
	m.width = 80
	m.height = 24
	m.formOpen = true
	m.activeForm = newModelNameForm(modelCatalog{}, "llamacpp-tailscale", "")
	if m.initActiveForm() == nil {
		t.Fatal("expected initActiveForm command")
	}
}

func TestNewModelProviderForm_ViewAfterInitShowsProviders(t *testing.T) {
	catalog := catalogFromIPCModels([]ipc.IPCEvent{{
		Type: ipc.EventTypeModels,
		Body: `[{"provider":"llamacpp-tailscale","id":"Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"},{"provider":"openai","id":"gpt-5.1"}]`,
	}})
	hf := newModelProviderForm(catalog, "auto")
	view := initModelFormView(hf)
	if !strings.Contains(view, "llamacpp-tailscale") {
		t.Fatalf("expected provider in view, got:\n%s", view)
	}
}