package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

func TestIsBareHelpAndStatusCommands(t *testing.T) {
	if !isBareHelpCommand("/help") || isBareHelpCommand("/help foo") {
		t.Fatal("help bare check failed")
	}
	if !isBareStatusCommand("/status") || isBareStatusCommand("/status foo") {
		t.Fatal("status bare check failed")
	}
}

func TestOpenFormForCommand_HelpOpensOverlay(t *testing.T) {
	m := testChatModel()
	next, cmd, handled := m.openFormForCommand("/help")
	if !handled || cmd != nil || !next.helpVisible() {
		t.Fatalf("handled=%v cmd=%v helpVisible=%v", handled, cmd, next.helpVisible())
	}
}

func TestOpenFormForCommand_StatusOpensProjectPanel(t *testing.T) {
	m := testChatModel()
	next, cmd, handled := m.openFormForCommand("/status")
	if !handled || cmd == nil || !next.projectPanelOpen {
		t.Fatalf("handled=%v cmd=%v projectPanelOpen=%v", handled, cmd, next.projectPanelOpen)
	}
}

func TestBareHelpSubmitOpensOverlayNotIPC(t *testing.T) {
	m := testChatModel()
	m.textarea.SetValue("/help")
	next, cmd := m.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := next.(Model)
	if !m2.helpVisible() || cmd != nil {
		t.Fatalf("helpVisible=%v cmd=%v", m2.helpVisible(), cmd)
	}
}

func TestBareStatusSubmitOpensProjectPanel(t *testing.T) {
	m := testChatModel()
	m.textarea.SetValue("/status")
	next, cmd := m.handleKeyMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := next.(Model)
	if !m2.projectPanelOpen {
		t.Fatal("expected project panel open")
	}
	if cmd == nil {
		t.Fatal("expected fetch command")
	}
}

func TestModalKey_BlocksTextareaWhileHelpOpen(t *testing.T) {
	m := testChatModel()
	m.helpModel.ShowAll = true
	m.textarea.SetValue("")

	updated, _ := m.handleKeyMsg(keyText("a"))
	m2 := updated.(Model)
	if m2.textarea.Value() != "" {
		t.Fatalf("expected textarea unchanged, got %q", m2.textarea.Value())
	}
	if !m2.helpVisible() {
		t.Fatal("expected help to stay open")
	}
}

func TestModalKey_BlocksTextareaWhileProjectPanelOpen(t *testing.T) {
	m := testChatModel()
	m.projectPanelOpen = true

	updated, _ := m.handleKeyMsg(keyText("hello"))
	m2 := updated.(Model)
	if m2.textarea.Value() != "" {
		t.Fatalf("expected textarea unchanged, got %q", m2.textarea.Value())
	}
	if !m2.projectPanelOpen {
		t.Fatal("expected project panel to stay open")
	}
}

func TestRenderModalOverlay_HasBorderedPanel(t *testing.T) {
	m := testChatModel()
	m.width = 80
	m.height = 24
	m.helpModel.ShowAll = true

	view := stripANSIForTest(m.View().Content)
	if !strings.Contains(view, "Keyboard Shortcuts") {
		t.Fatal("expected help content in modal view")
	}
	// Rounded border uses corner runes in the rendered box.
	if !strings.Contains(m.View().Content, "╭") && !strings.Contains(m.View().Content, "┌") {
		t.Fatal("expected bordered modal panel")
	}
}

func TestRenderModalOverlay_FormUsesModal(t *testing.T) {
	m := testChatModel()
	m.width = 80
	m.height = 24
	m.formOpen = true
	m.activeForm = newModelProviderForm(modelCatalog{}, "auto")

	view := m.View().Content
	if view == "" {
		t.Fatal("expected non-empty view")
	}
	if !strings.Contains(view, "╭") && !strings.Contains(view, "┌") {
		t.Fatal("expected bordered form modal")
	}
}

// TestOverlayPanel_UsesThemedBorder pins the modal border to the theme token:
// dark renders with the dark accent (205), light with the light accent (125).
func TestOverlayPanel_UsesThemedBorder(t *testing.T) {
	dark := testChatModel()
	dark.width = 80
	dark.height = 24
	darkView := dark.overlayPanel("bg", "panel")
	if !strings.Contains(darkView, "38;5;205") {
		t.Fatal("expected dark modal border color 205 in overlay output")
	}

	light := testChatModel()
	light.theme = ThemeLight
	light.styles = newStylesForTheme(ThemeLight)
	light.width = 80
	light.height = 24
	lightView := light.overlayPanel("bg", "panel")
	if !strings.Contains(lightView, "38;5;125") {
		t.Fatal("expected light modal border color 125 in overlay output")
	}
}

func TestToggleProjectPanel_FetchesState(t *testing.T) {
	m := testChatModel()
	m.activeSession = ipc.ReservedTUIChatID
	next, cmd := m.toggleProjectPanel()
	if !next.projectPanelOpen || cmd == nil {
		t.Fatalf("open: panel=%v cmd=%v", next.projectPanelOpen, cmd)
	}
	next2, cmd2 := next.toggleProjectPanel()
	if next2.projectPanelOpen || cmd2 != nil {
		t.Fatalf("close: panel=%v cmd=%v", next2.projectPanelOpen, cmd2)
	}
}
