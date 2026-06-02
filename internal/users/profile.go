package users

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Profile holds mutable per-user metadata.
// Fields added after the initial schema use omitempty tags so existing
// JSON profiles without these fields still decode successfully.
type Profile struct {
	UserID      int64     `json:"user_id"`
	Name        string    `json:"name"`
	Language    string    `json:"language"` // "pt" or "en"
	IsOwner     bool      `json:"is_owner"`
	ActiveMode  string    `json:"active_mode,omitempty"`  // developer | researcher | "" (general)
	Timezone    string    `json:"timezone,omitempty"`     // IANA tz; empty = UTC
	DefaultCWD  string    `json:"default_cwd,omitempty"`  // fallback CWD for private chats
	OnboardedAt time.Time `json:"onboarded_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// NormalizeMode canonicalizes a raw mode string input. Aliases:
//
//	"", "auto", "general", "geral" → ""
//	"dev", "developer", "desenvolvedor" → "developer"
//	"researcher", "research", "pesquisa", "pesquisador" → "researcher"
//
// Returns an error containing the rejected value for unrecognized inputs.
func NormalizeMode(raw string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "auto", "general", "geral":
		return "", nil
	case "dev", "developer", "desenvolvedor":
		return "developer", nil
	case "researcher", "research", "pesquisa", "pesquisador":
		return "researcher", nil
	default:
		return "", fmt.Errorf("modo inválido %q", raw)
	}
}

// NormalizeTimezone returns the canonical IANA name and *time.Location for a
// timezone string. Empty input returns "" + time.UTC. Invalid input returns
// an error containing the rejected value.
func NormalizeTimezone(raw string) (string, *time.Location, error) {
	tz := strings.TrimSpace(raw)
	if tz == "" {
		return "", time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", nil, fmt.Errorf("invalid timezone %q: %w", raw, err)
	}
	return tz, loc, nil
}

// Load reads a Profile from a JSON file. Returns nil, nil if file does not exist.
func Load(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	return &p, nil
}

// Save writes a Profile to a JSON file, creating parent directories as needed.
// Uses atomic write (temp file + rename) with exclusive temp file creation
// to prevent symlink attacks and partial writes on crash.
func Save(path string, p *Profile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	data = append(data, '\n')
	f, err := os.CreateTemp(filepath.Dir(path), ".profile-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename profile: %w", err)
	}
	return nil
}
