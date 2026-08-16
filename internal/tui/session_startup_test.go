package tui

import (
	"strings"
	"testing"
)

func TestParseStartupSessionFlag(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"dm", ""},
		{"DM", ""},
		{"work", "work"},
		{"tui:work", "work"},
		{"TUI:work", "work"},
		{"  tui:infra  ", "infra"},
	}
	for _, tt := range tests {
		if got := ParseStartupSessionFlag(tt.in); got != tt.want {
			t.Errorf("ParseStartupSessionFlag(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestValidateStartupSessionName(t *testing.T) {
	valid := []string{"work", "aurelia-dev", "sprint_e"}
	for _, name := range valid {
		if err := ValidateStartupSessionName(name); err != nil {
			t.Errorf("ValidateStartupSessionName(%q) unexpected error: %v", name, err)
		}
	}

	invalid := []struct {
		name string
		frag string
	}{
		{"", "required"},
		{".", "not allowed"},
		{"..", "not allowed"},
		{"foo/bar", "path separators"},
		{"foo\\bar", "path separators"},
		{"bad\x1bname", "invalid"},
	}
	for _, tt := range invalid {
		err := ValidateStartupSessionName(tt.name)
		if err == nil {
			t.Errorf("ValidateStartupSessionName(%q) expected error", tt.name)
			continue
		}
		if !strings.Contains(err.Error(), tt.frag) {
			t.Errorf("ValidateStartupSessionName(%q) = %v, want fragment %q", tt.name, err, tt.frag)
		}
	}
}

func TestFindSessionByName(t *testing.T) {
	sessions := []tuiSessionInfo{
		{ChatID: -9000001, Name: "dm"},
		{ChatID: -9000002, Name: "work"},
	}
	if id, ok := findSessionByName(sessions, "work"); !ok || id != -9000002 {
		t.Errorf("findSessionByName(work) = (%d, %v), want (-9000002, true)", id, ok)
	}
	if _, ok := findSessionByName(sessions, "missing"); ok {
		t.Error("expected missing session to return false")
	}
}

func TestStartupSessionCmd_OpenExisting(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.startupSession = "work"
	m.startupSessionPending = true
	m.sessions = []tuiSessionInfo{{ChatID: -9000002, Name: "work"}}
	if cmd := m.startupSessionCmd(); cmd == nil {
		t.Fatal("expected open command for existing session")
	}
}

func TestStartupSessionCmd_CreateNew(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.startupSession = "newproj"
	m.startupSessionPending = true
	m.sessions = []tuiSessionInfo{{ChatID: -9000001, Name: "dm"}}
	if cmd := m.startupSessionCmd(); cmd == nil {
		t.Fatal("expected create command for new session")
	}
}

func TestStartupSessionCmd_SkipsWhenNotPending(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.startupSession = "work"
	if cmd := m.startupSessionCmd(); cmd != nil {
		t.Fatal("expected nil when startup session not pending")
	}
}
