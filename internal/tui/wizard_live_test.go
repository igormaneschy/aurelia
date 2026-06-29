package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/igormaneschy/aurelia/internal/ipc"
)

func runBatchCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			if c == nil {
				continue
			}
			out = append(out, c())
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestOpenModelSelect_LiveCatalogUpdatesProviders(t *testing.T) {
	path, err := ipc.DefaultSocketPath()
	if err != nil {
		t.Skip(err)
	}
	m := testChatModel()
	m.ipcClient = ipc.NewClient(path)
	m.activeSession = ipc.ReservedTUIChatID
	m.width = 120
	m.height = 30

	next, cmd := m.openModelSelect()
	if !next.formOpen || next.activeForm == nil {
		t.Fatal("form not open")
	}
	var modelMsg tuiModelsMsg
	for _, msg := range runBatchCmd(cmd) {
		if m, ok := msg.(tuiModelsMsg); ok {
			modelMsg = m
		}
	}
	if modelMsg.err != nil {
		t.Skipf("live test requires running daemon: %v", modelMsg.err)
	}
	if modelMsg.catalog.providerCount() < 2 {
		t.Fatalf("providers=%d", modelMsg.catalog.providerCount())
	}

	updated := next.applyWizardCatalog(modelMsg)
	updated.width = 120
	updated.height = 30
	for _, msg := range runBatchCmd(updated.initActiveForm()) {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			updated.width = ws.Width
			updated.height = ws.Height
			if updated.activeForm != nil {
				_ = updated.activeForm.update(ws)
			}
		}
	}
	if updated.activeForm.catalog.providerCount() < 2 {
		t.Fatalf("form providers=%d", updated.activeForm.catalog.providerCount())
	}
	view := updated.activeForm.view()
	found := false
	for _, p := range []string{"openai", "anthropic", "groq", "deepseek"} {
		if strings.Contains(view, p) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected provider in view, count=%d view:\n%s", updated.activeForm.catalog.providerCount(), view)
	}
}
