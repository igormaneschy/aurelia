package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const tuiPrefsFileMode os.FileMode = 0o600

// TUIPrefs stores persisted TUI appearance settings.
type TUIPrefs struct {
	Theme       Theme `json:"theme,omitempty"`
	Transparent bool  `json:"transparent,omitempty"`
}

func defaultTUIPrefsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".aurelia", "tui_prefs.json")
}

// LoadTUIPrefs reads saved TUI preferences. Missing or invalid files return zero prefs.
func LoadTUIPrefs() TUIPrefs {
	path := defaultTUIPrefsPath()
	if path == "" {
		return TUIPrefs{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return TUIPrefs{}
	}
	var prefs TUIPrefs
	if err := json.Unmarshal(data, &prefs); err != nil {
		return TUIPrefs{}
	}
	prefs.Theme = normalizeStoredTheme(prefs.Theme)
	return prefs
}

// normalizeStoredTheme maps missing or invalid stored themes to the dark
// default. Explicitly saved values ("auto", "light", "dark") are preserved.
func normalizeStoredTheme(t Theme) Theme {
	switch t {
	case ThemeAuto, ThemeLight, ThemeDark:
		return t
	default:
		return ThemeDark
	}
}

func saveTUIPrefs(prefs TUIPrefs) error {
	path := defaultTUIPrefsPath()
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, tuiPrefsFileMode)
}

func (m *Model) persistAppearancePrefs() {
	_ = saveTUIPrefs(TUIPrefs{
		Theme:       m.theme,
		Transparent: m.transparent,
	})
}
