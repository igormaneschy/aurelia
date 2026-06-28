package telegram

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/igormaneschy/aurelia/internal/orchestrator"

	"gopkg.in/telebot.v3"
)

func TestLoadFeatureDocs_UsesPlanFeature(t *testing.T) {
	bc := &BotController{}

	dir := t.TempDir()
	featDir := filepath.Join(dir, ".specs", "features", "my-feature")
	_ = os.MkdirAll(featDir, 0755)
	_ = os.WriteFile(filepath.Join(featDir, "spec.md"), []byte("spec content"), 0644)
	_ = os.WriteFile(filepath.Join(featDir, "design.md"), []byte("design content"), 0644)

	spec, design := bc.loadFeatureDocs(dir, "my-feature")
	if spec != "spec content" {
		t.Errorf("spec = %q, want 'spec content'", spec)
	}
	if design != "design content" {
		t.Errorf("design = %q, want 'design content'", design)
	}
}

func TestLoadFeatureDocs_FallsBackWhenFeatureEmpty(t *testing.T) {
	bc := &BotController{}

	dir := t.TempDir()
	// Create two feature dirs; legacy fallback picks the last alphabetically
	featA := filepath.Join(dir, ".specs", "features", "aaa")
	featB := filepath.Join(dir, ".specs", "features", "zzz")
	_ = os.MkdirAll(featA, 0755)
	_ = os.MkdirAll(featB, 0755)
	_ = os.WriteFile(filepath.Join(featA, "spec.md"), []byte("aaa spec"), 0644)
	_ = os.WriteFile(filepath.Join(featB, "spec.md"), []byte("zzz spec"), 0644)
	_ = os.WriteFile(filepath.Join(featA, "design.md"), []byte("aaa design"), 0644)
	_ = os.WriteFile(filepath.Join(featB, "design.md"), []byte("zzz design"), 0644)

	spec, design := bc.loadFeatureDocs(dir, "")
	if spec != "zzz spec" {
		t.Errorf("spec = %q, want 'zzz spec' (legacy fallback)", spec)
	}
	if design != "zzz design" {
		t.Errorf("design = %q, want 'zzz design' (legacy fallback)", design)
	}
}

func TestLoadFeatureDocs_MissingDir(t *testing.T) {
	bc := &BotController{}

	dir := t.TempDir()
	spec, design := bc.loadFeatureDocs(dir, "nonexistent")
	if spec != "" {
		t.Errorf("expected empty spec for missing dir, got %q", spec)
	}
	if design != "" {
		t.Errorf("expected empty design for missing dir, got %q", design)
	}
}

// M4: nil orchestrator guard — executeApprovedPlan returns without panic.
func TestExecuteApprovedPlan_NilOrchestrator(t *testing.T) {
	bot, err := telebot.NewBot(telebot.Settings{Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	bc := &BotController{
		bot:          bot,
		orchestrator: nil,
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("executeApprovedPlan panicked with nil orchestrator: %v", r)
		}
	}()

	bc.executeApprovedPlan(&telebot.Chat{ID: 1, Type: telebot.ChatPrivate}, 0, 0, t.TempDir(), 42, &orchestrator.Plan{})
}
