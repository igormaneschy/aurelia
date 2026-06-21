package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCommandSuggestionsPrefixMatch(t *testing.T) {
	got := commandSuggestions("/mo")
	want := []string{"/model"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestions = %#v, want %#v", got, want)
	}
}

func TestCommandSuggestionsRejectNonCommandPrefix(t *testing.T) {
	if got := commandSuggestions("hello"); len(got) != 0 {
		t.Fatalf("expected no suggestions for non-command, got %#v", got)
	}
}

func TestModel_TabShowsAutocompleteForSlashInput(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.textarea.SetValue("/")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for autocomplete")
	}
	if got := len(m2.autocompleteOptions); got == 0 {
		t.Fatal("expected autocomplete options")
	}
	if m2.showSidebar != m.showSidebar {
		t.Fatal("expected tab autocomplete not to toggle sidebar")
	}
}

func TestModel_TabCyclesAutocomplete(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.textarea.SetValue("/")
	m = m.refreshAutocomplete()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := updated.(Model)

	if m2.autocompleteIndex != 1 {
		t.Fatalf("autocompleteIndex = %d, want 1", m2.autocompleteIndex)
	}
}

func TestModel_EnterAppliesAutocomplete(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.textarea.SetValue("/mo")
	m = m.refreshAutocomplete()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command while applying autocomplete")
	}
	if got := m2.textarea.Value(); got != "/model" {
		t.Fatalf("textarea = %q, want /model", got)
	}
	if m2.hasAutocomplete() {
		t.Fatal("expected autocomplete cleared after apply")
	}
}

func TestModel_QuestionMarkShowsCommandSuggestions(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.textarea.SetValue("/c")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m2 := updated.(Model)

	if got := len(m2.autocompleteOptions); got != 1 || m2.autocompleteOptions[0] != "/cwd" {
		t.Fatalf("autocompleteOptions = %#v, want /cwd", m2.autocompleteOptions)
	}
}

func TestModel_QuestionMarkInCommandArgumentsDelegatesToTextarea(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.textarea.SetValue("/cwd /tmp/project")
	m.textarea.CursorEnd()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m2 := updated.(Model)

	if m2.hasAutocomplete() {
		t.Fatal("expected no autocomplete while editing command arguments")
	}
	if got := m2.textarea.Value(); got != "/cwd /tmp/project?" {
		t.Fatalf("textarea = %q, want question mark appended", got)
	}
}

func TestModel_TabTogglesSidebarForNonCommandInput(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.showSidebar = true
	m.textarea.SetValue("hello")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := updated.(Model)

	if m2.showSidebar {
		t.Fatal("expected tab to toggle sidebar for non-command input")
	}
}

func TestModel_RenderAutocompleteHighlightsSelectedOption(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.autocompleteOptions = []string{"/help", "/status"}
	m.autocompleteIndex = 1

	view := stripANSIForTest(m.renderAutocomplete())

	if !strings.Contains(view, "▶ /status") {
		t.Fatalf("expected selected autocomplete marker, got %q", view)
	}
}
