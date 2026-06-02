package users

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProfile_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")

	now := time.Now().Round(time.Second).UTC()
	original := &Profile{
		UserID:      42,
		Name:        "Alice",
		Language:    "pt",
		IsOwner:     true,
		OnboardedAt: now,
		LastSeenAt:  now,
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got == nil {
		t.Fatal("Load() returned nil")
	}

	if got.UserID != original.UserID {
		t.Errorf("UserID = %d, want %d", got.UserID, original.UserID)
	}
	if got.Name != original.Name {
		t.Errorf("Name = %q, want %q", got.Name, original.Name)
	}
	if got.Language != original.Language {
		t.Errorf("Language = %q, want %q", got.Language, original.Language)
	}
	if got.IsOwner != original.IsOwner {
		t.Errorf("IsOwner = %v, want %v", got.IsOwner, original.IsOwner)
	}
	if !got.OnboardedAt.Equal(original.OnboardedAt) {
		t.Errorf("OnboardedAt = %v, want %v", got.OnboardedAt, original.OnboardedAt)
	}
	if !got.LastSeenAt.Equal(original.LastSeenAt) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, original.LastSeenAt)
	}
}

func TestProfile_LoadMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if p != nil {
		t.Fatal("Load() should return nil for missing file")
	}
}

func TestProfile_SaveCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nested", "profile.json")

	p := &Profile{
		UserID:   1,
		Name:     "Test",
		Language: "en",
	}
	if err := Save(path, p); err != nil {
		t.Fatalf("Save() to nested dir error = %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Save() did not create the file")
	}
}

func TestNormalizeMode_AcceptsAliases(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"", ""},
		{"auto", ""},
		{"general", ""},
		{"geral", ""},
		{"dev", "developer"},
		{"developer", "developer"},
		{"desenvolvedor", "developer"},
		{"research", "researcher"},
		{"researcher", "researcher"},
		{"pesquisa", "researcher"},
		{"pesquisador", "researcher"},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := NormalizeMode(tc.raw)
			if err != nil {
				t.Fatalf("NormalizeMode(%q) error = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("NormalizeMode(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeMode_InvalidErrors(t *testing.T) {
	invalid := []string{"invalid", "programador", "admin", "owner"}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			_, err := NormalizeMode(raw)
			if err == nil {
				t.Fatalf("NormalizeMode(%q) should error", raw)
			}
			if !strings.Contains(err.Error(), raw) {
				t.Errorf("error %q should contain rejected value %q", err.Error(), raw)
			}
		})
	}
}

func TestNormalizeTimezone_EmptyReturnsUTC(t *testing.T) {
	name, loc, err := NormalizeTimezone("")
	if err != nil {
		t.Fatalf("NormalizeTimezone(\"\") error = %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want \"\"", name)
	}
	if loc != time.UTC {
		t.Errorf("loc = %v, want UTC", loc)
	}
}

func TestNormalizeTimezone_ValidIANA(t *testing.T) {
	name, loc, err := NormalizeTimezone("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("NormalizeTimezone error = %v", err)
	}
	if name != "America/Sao_Paulo" {
		t.Errorf("name = %q, want America/Sao_Paulo", name)
	}
	if loc == nil {
		t.Fatal("loc is nil")
	}
	if loc.String() != "America/Sao_Paulo" {
		t.Errorf("loc.String() = %q", loc.String())
	}
}

func TestNormalizeTimezone_InvalidErrors(t *testing.T) {
	_, _, err := NormalizeTimezone("Not/A_Timezone")
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
	if !strings.Contains(err.Error(), "Not/A_Timezone") {
		t.Errorf("error %q should contain rejected value", err.Error())
	}
}

func TestProfile_JSONBackwardCompatibility(t *testing.T) {
	// Old JSON without active_mode, timezone, default_cwd should decode
	// with empty string defaults.
	oldJSON := `{
  "user_id": 42,
  "name": "Alice",
  "language": "pt",
  "is_owner": true,
  "onboarded_at": "2026-01-01T00:00:00Z",
  "last_seen_at": "2026-01-01T00:00:00Z"
}`
	var p Profile
	if err := json.Unmarshal([]byte(oldJSON), &p); err != nil {
		t.Fatalf("Unmarshal old JSON: %v", err)
	}
	if p.ActiveMode != "" {
		t.Errorf("ActiveMode = %q, want \"\"", p.ActiveMode)
	}
	if p.Timezone != "" {
		t.Errorf("Timezone = %q, want \"\"", p.Timezone)
	}
	if p.DefaultCWD != "" {
		t.Errorf("DefaultCWD = %q, want \"\"", p.DefaultCWD)
	}
}

func TestProfile_SaveLoadNewFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")

	original := &Profile{
		UserID:     1,
		Name:       "Test",
		Language:   "en",
		ActiveMode: "developer",
		Timezone:   "America/Sao_Paulo",
		DefaultCWD: "/home/test/projects",
	}
	if err := Save(path, original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ActiveMode != "developer" {
		t.Errorf("ActiveMode = %q, want developer", got.ActiveMode)
	}
	if got.Timezone != "America/Sao_Paulo" {
		t.Errorf("Timezone = %q, want America/Sao_Paulo", got.Timezone)
	}
	if got.DefaultCWD != "/home/test/projects" {
		t.Errorf("DefaultCWD = %q, want /home/test/projects", got.DefaultCWD)
	}
}

func TestResolver_UserModePath(t *testing.T) {
	r := NewResolver("/home/user/.aurelia")
	path := r.UserModePath(42, "developer")
	want := filepath.FromSlash("/home/user/.aurelia/users/42/personas/mode_developer.md")
	if path != want {
		t.Errorf("UserModePath = %q, want %q", path, want)
	}
}
