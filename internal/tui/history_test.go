package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInputHistorySaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".aurelia", "tui_history.json")
	want := []string{"/cwd /tmp/project", "hello"}

	if err := saveInputHistory(path, want); err != nil {
		t.Fatalf("saveInputHistory error: %v", err)
	}
	got := loadInputHistory(path)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %#v, want %#v", got, want)
	}
}

func TestInputHistorySaveUses0600Permissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_history.json")
	if err := os.WriteFile(path, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := saveInputHistory(path, []string{"secret-ish prompt"}); err != nil {
		t.Fatalf("saveInputHistory error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != inputHistoryFileMode {
		t.Fatalf("mode = %o, want %o", got, inputHistoryFileMode)
	}
}

func TestInputHistorySaveReplacesExistingFileAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_history.json")
	if err := os.WriteFile(path, []byte(`["old"]`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := saveInputHistory(path, []string{"new"}); err != nil {
		t.Fatalf("saveInputHistory error: %v", err)
	}

	if got := loadInputHistory(path); !reflect.DeepEqual(got, []string{"new"}) {
		t.Fatalf("history = %#v, want new", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != inputHistoryFileMode {
		t.Fatalf("mode = %o, want %o", got, inputHistoryFileMode)
	}
}

func TestInputHistoryLoadCorruptFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_history.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := loadInputHistory(path); len(got) != 0 {
		t.Fatalf("expected corrupt history to load empty, got %#v", got)
	}
}

func TestInputHistoryTrimKeepsNewestThousand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_history.json")
	var history []string
	for i := 0; i < maxInputHistory+5; i++ {
		history = append(history, fmt.Sprintf("prompt-%d", i))
	}

	if err := saveInputHistory(path, history); err != nil {
		t.Fatalf("saveInputHistory error: %v", err)
	}
	got := loadInputHistory(path)

	if len(got) != maxInputHistory {
		t.Fatalf("len(history) = %d, want %d", len(got), maxInputHistory)
	}
	if got[0] != "prompt-5" {
		t.Fatalf("first retained prompt = %q, want prompt-5", got[0])
	}
}

func TestNewModelLoadsInputHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_history.json")
	if err := saveInputHistory(path, []string{"first", "second"}); err != nil {
		t.Fatalf("saveInputHistory error: %v", err)
	}

	m := newModel("/tmp/test.sock", path, ThemeDark)

	if !reflect.DeepEqual(m.inputHistory, []string{"first", "second"}) {
		t.Fatalf("inputHistory = %#v", m.inputHistory)
	}
	if m.inputHistoryIndex != 2 {
		t.Fatalf("inputHistoryIndex = %d, want 2", m.inputHistoryIndex)
	}
}

func TestModelSaveInputHistoryUsesConfiguredPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_history.json")
	m := newModel("/tmp/test.sock", path, ThemeDark)
	m.inputHistory = []string{"persist me"}

	if err := m.SaveInputHistory(); err != nil {
		t.Fatalf("SaveInputHistory error: %v", err)
	}
	got := loadInputHistory(path)

	if !reflect.DeepEqual(got, []string{"persist me"}) {
		t.Fatalf("saved history = %#v", got)
	}
}
