package tui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

const maxStartupSessionNameLength = 64

// ParseStartupSessionFlag normalizes the --session CLI value.
// Accepts optional "tui:" prefix (e.g. "tui:work" → "work").
// Empty string and "dm" mean the default DM session.
func ParseStartupSessionFlag(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(s), "tui:") {
		s = strings.TrimSpace(s[4:])
	}
	if strings.EqualFold(s, "dm") {
		return ""
	}
	return s
}

// ValidateStartupSessionName rejects unsafe session names before the TUI starts.
// Mirrors daemon-side sanitizeSessionName rules for path/control characters.
func ValidateStartupSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("session name is required")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("session name %q is not allowed", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("session name cannot contain path separators")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7F || r == 0xFFFD || (r >= 0x80 && r <= 0x9F) {
			return fmt.Errorf("session name contains invalid characters")
		}
		if unicode.IsControl(r) {
			return fmt.Errorf("session name contains control characters")
		}
	}
	runes := []rune(name)
	if len(runes) > maxStartupSessionNameLength {
		return fmt.Errorf("session name exceeds %d characters", maxStartupSessionNameLength)
	}
	return nil
}

// findSessionByName returns the chat ID for a session with the given name.
func findSessionByName(sessions []tuiSessionInfo, name string) (int64, bool) {
	for _, s := range sessions {
		if s.Name == name {
			return s.ChatID, true
		}
	}
	return 0, false
}

// startupSessionCmd opens an existing session or creates it when --session was set.
func (m Model) startupSessionCmd() tea.Cmd {
	if !m.startupSessionPending || m.startupSession == "" {
		return nil
	}
	name := m.startupSession
	if chatID, ok := findSessionByName(m.sessions, name); ok {
		return openTUISession(m.ipcClient, chatID)
	}
	return createTUISession(m.ipcClient, name)
}
