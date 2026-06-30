package tui

import "strings"

var tuiCommandSuggestions = []string{
	"/help",
	"/status",
	"/cwd",
	"/mode",
	"/agents",
	"/model",
	"/img",
	"/attach",
}

func commandSuggestions(prefix string) []string {
	prefix = strings.TrimSpace(prefix)
	if !strings.HasPrefix(prefix, "/") {
		return nil
	}
	var matches []string
	for _, cmd := range tuiCommandSuggestions {
		if strings.HasPrefix(cmd, prefix) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

func (m Model) inputCommandPrefix() string {
	value := strings.TrimSpace(m.textarea.Value())
	if !strings.HasPrefix(value, "/") {
		return ""
	}
	if idx := strings.IndexAny(value, " \t\n\r"); idx >= 0 {
		return ""
	}
	return value
}

func (m Model) hasAutocomplete() bool {
	return len(m.autocompleteOptions) > 0
}

func (m Model) refreshAutocomplete() Model {
	m.autocompleteOptions = commandSuggestions(m.inputCommandPrefix())
	m.autocompleteIndex = 0
	(&m).syncViewportDimensions()
	return m
}

func (m Model) cycleAutocomplete() Model {
	if len(m.autocompleteOptions) == 0 {
		return m.refreshAutocomplete()
	}
	m.autocompleteIndex = (m.autocompleteIndex + 1) % len(m.autocompleteOptions)
	return m
}

func (m Model) applyAutocomplete() Model {
	if len(m.autocompleteOptions) == 0 {
		return m
	}
	if m.autocompleteIndex < 0 || m.autocompleteIndex >= len(m.autocompleteOptions) {
		m.autocompleteIndex = 0
	}
	selected := m.autocompleteOptions[m.autocompleteIndex]
	current := m.textarea.Value()
	prefix := m.inputCommandPrefix()
	if prefix == "" {
		return m
	}
	m.textarea.SetValue(strings.Replace(current, prefix, selected, 1))
	m.textarea.CursorEnd()
	m.clearAutocomplete()
	return m
}

func (m *Model) clearAutocomplete() {
	if len(m.autocompleteOptions) == 0 {
		return
	}
	m.autocompleteOptions = nil
	m.autocompleteIndex = 0
	m.syncViewportDimensions()
}
