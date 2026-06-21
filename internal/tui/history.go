package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const inputHistoryFileMode os.FileMode = 0o600

func defaultInputHistoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".aurelia", "tui_history.json")
}

func loadInputHistory(path string) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var history []string
	if err := json.Unmarshal(data, &history); err != nil {
		return nil
	}
	return trimInputHistory(history)
}

func saveInputHistory(path string, history []string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(trimInputHistory(history), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".tui_history_*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(inputHistoryFileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, inputHistoryFileMode)
}

func trimInputHistory(history []string) []string {
	if len(history) <= maxInputHistory {
		return history
	}
	return history[len(history)-maxInputHistory:]
}

// SaveInputHistory persists the current input history for the next TUI run.
func (m Model) SaveInputHistory() error {
	return saveInputHistory(m.historyPath, m.inputHistory)
}
