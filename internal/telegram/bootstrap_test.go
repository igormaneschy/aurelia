package telegram

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/telebot.v3"
)

func TestBuildUserTemplate_UsesTelegramName(t *testing.T) {
	user := &telebot.User{
		ID:        42,
		FirstName: "Ana",
		LastName:  "Silva",
		Username:  "ana",
	}

	got := buildUserTemplate(user)

	if !strings.Contains(got, "Nome: Ana Silva") {
		t.Fatalf("expected full name in user template, got %q", got)
	}
	if strings.Contains(got, "Usuario 42") {
		t.Fatalf("should not write numeric placeholder, got %q", got)
	}
}

func TestBuildUserTemplate_FallsBackWithoutInventingIdentity(t *testing.T) {
	user := &telebot.User{ID: 42}

	got := buildUserTemplate(user)

	if !strings.Contains(got, "Nome: Nao definido") {
		t.Fatalf("expected unresolved placeholder, got %q", got)
	}
	if strings.Contains(got, "Usuario 42") {
		t.Fatalf("should not invent identity from telegram id, got %q", got)
	}
}

func TestBuildUserTemplateFromProfile_UsesConversationProfile(t *testing.T) {
	got := buildUserTemplateFromProfile("Me chamo Ana e quero respostas diretas, sem floreios.", "ana")

	if !strings.Contains(got, "Nome: Ana") {
		t.Fatalf("expected extracted name, got %q", got)
	}
	if !strings.Contains(got, "Preferencias: Me chamo Ana e quero respostas diretas, sem floreios.") {
		t.Fatalf("expected full profile text, got %q", got)
	}
}

func TestBuildUserTemplateFromProfile_FallsBackToTelegramName(t *testing.T) {
	got := buildUserTemplateFromProfile("Quero respostas diretas, sem floreios.", "ana")

	if !strings.Contains(got, "Nome: ana") {
		t.Fatalf("expected telegram fallback name, got %q", got)
	}
}

func TestExtractNameFromProfile(t *testing.T) {
	cases := map[string]string{
		"Me chamo Igor e quero respostas diretas.": "Igor",
		"Meu nome e Ana Silva.":                "Ana Silva",
		"Sou Igor.":                                "Igor",
		"Quero respostas diretas.":                 "",
	}

	for input, want := range cases {
		if got := extractNameFromProfile(input); got != want {
			t.Fatalf("extractNameFromProfile(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBotController_IsAllowedUser(t *testing.T) {
	bc := &BotController{
		allowedUsers: map[int64]struct{}{
			42: {},
			99: {},
		},
	}

	if !bc.isAllowedUser(42) {
		t.Fatal("expected user 42 to be allowed")
	}
	if bc.isAllowedUser(7) {
		t.Fatal("expected user 7 to be blocked")
	}
}

func TestBotController_IsAllowedGroup(t *testing.T) {
	bc := &BotController{
		allowedGroups: map[int64]struct{}{
			100: {},
			200: {},
		},
	}

	if !bc.isAllowedGroup(100) {
		t.Fatal("expected group 100 to be allowed")
	}
	if bc.isAllowedGroup(300) {
		t.Fatal("expected group 300 to be blocked")
	}
}

func TestBootstrapStartResponse_WhenAlreadyConfigured(t *testing.T) {
	message, menu := bootstrapStartResponse(true)

	if message != alreadyConfiguredMessage() {
		t.Fatalf("unexpected configured message: %q", message)
	}
	if menu != nil {
		t.Fatalf("expected no menu when already configured, got %#v", menu)
	}
}

func TestBootstrapStartResponse_WhenBootstrapNeeded(t *testing.T) {
	message, menu := bootstrapStartResponse(false)

	if message != bootstrapWelcomeMessage() {
		t.Fatalf("unexpected bootstrap welcome message: %q", message)
	}
	if menu == nil {
		t.Fatal("expected bootstrap menu")
		return
	}
	if len(menu.InlineKeyboard) != 2 {
		t.Fatalf("expected two inline rows, got %d", len(menu.InlineKeyboard))
	}
	if len(menu.InlineKeyboard[0]) != 1 || len(menu.InlineKeyboard[1]) != 1 {
		t.Fatalf("expected one button per row, got %#v", menu.InlineKeyboard)
	}
	if menu.InlineKeyboard[0][0].Unique != "btn_coder" {
		t.Fatalf("expected coder callback button, got %#v", menu.InlineKeyboard[0][0])
	}
	if menu.InlineKeyboard[1][0].Unique != "btn_assist" {
		t.Fatalf("expected assist callback button, got %#v", menu.InlineKeyboard[1][0])
	}
}

func TestBootstrapIdentityExists_UsesGivenDir(t *testing.T) {
	dir := t.TempDir()

	if bootstrapIdentityExists(dir) {
		t.Fatal("expected false for empty dir, got true")
	}

	if err := os.WriteFile(filepath.Join(dir, "IDENTITY.md"), []byte("# Identity"), 0o644); err != nil {
		t.Fatalf("failed to create IDENTITY.md: %v", err)
	}
	if !bootstrapIdentityExists(dir) {
		t.Fatal("expected true after creating IDENTITY.md, got false")
	}

	if bootstrapIdentityExists(t.TempDir()) {
		t.Fatal("expected false for different empty dir, got true")
	}
}

func TestWriteBootstrapPreset_WritesToDir(t *testing.T) {
	dir := t.TempDir()
	preset, err := bootstrapPresetForChoice("coder")
	if err != nil {
		t.Fatalf("bootstrapPresetForChoice() error = %v", err)
	}

	if err := writeBootstrapPreset(dir, preset); err != nil {
		t.Fatalf("writeBootstrapPreset() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "IDENTITY.md")); err != nil {
		t.Fatalf("IDENTITY.md not found in dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SOUL.md")); err != nil {
		t.Fatalf("SOUL.md not found in dir: %v", err)
	}

	if _, err := os.Stat("IDENTITY.md"); err == nil {
		t.Fatal("IDENTITY.md must not be written to CWD")
	}
	if _, err := os.Stat("SOUL.md"); err == nil {
		t.Fatal("SOUL.md must not be written to CWD")
	}
}

func TestBootstrapTimezone_PresetsExist(t *testing.T) {
	// Verify all preset choices map to valid IANA timezones.
	presets := bootstrapTimezonePresets
	if len(presets) != 4 {
		t.Fatalf("expected 4 preset timezones, got %d", len(presets))
	}
	for key, tz := range presets {
		// Verify key naming convention
		if !strings.HasPrefix(key, "tz_") {
			t.Errorf("preset key %q should start with tz_", key)
		}
		// Verify timezone is non-empty
		if tz == "" {
			t.Errorf("preset %q has empty timezone", key)
		}
	}
}

func TestBootstrapTimezoneMenu_HasFiveButtons(t *testing.T) {
	menu := newBootstrapTimezoneMenu()
	if menu == nil {
		t.Fatal("newBootstrapTimezoneMenu() returned nil")
	}
	// Inline keyboards have InlineKeyboard field
	rows := menu.InlineKeyboard
	totalButtons := 0
	for _, row := range rows {
		totalButtons += len(row)
	}
	if totalButtons != 5 {
		t.Fatalf("expected 5 buttons in timezone menu, got %d", totalButtons)
	}
}
