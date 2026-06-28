package pipeline

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/session"
)

func TestLooksLikeCodebaseRead(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		// Portuguese
		{"leia a code base do aurelia", true},
		{"leia o código fonte", true},
		{"leia o codigo do projeto", true},
		{"leia a base de código", true},
		{"leia a base de codigo", true},
		{"leia o projeto", true},
		{"leia o repositório", true},
		{"leia o repositorio", true},
		{"analise o código", true},
		{"analise o codigo", true},
		{"analisa o código", true},
		{"analise o projeto", true},
		{"analise o repositório", true},
		{"veja o código do sistema", true},
		{"veja o projeto", true},
		// English
		{"read the codebase", true},
		{"read the code base", true},
		{"read the code please", true},
		{"read the project", true},
		{"read the repo", true},
		{"read the repository", true},
		{"analyze the codebase", true},
		{"analyze the code base", true},
		{"analyze the code", true},
		{"analyze the project structure", true},
		{"analyze the repo", true},
		{"analyze the repository", true},
		{"analyse the codebase", true},
		{"go through the code", true},
		{"browse the code", true},
		{"browse the codebase", true},
		{"browse the project", true},
		// False negatives — casual chat should never match
		{"bom dia", false},
		{"qual o status do servidor", false},
		{"implementa a feature", false},
		{"", false},
		{"como funciona o cache", false},
		// False negatives — project/cwd mentions without file action
		{"qual projeto você prefere?", false},
		{"memória do projeto", false},
	}

	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			if got := looksLikeCodebaseRead(tc.text); got != tc.want {
				t.Errorf("looksLikeCodebaseRead(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestCodebaseReadGuidance_InjectedWhenNoCwd(t *testing.T) {
	svc := &Service{
		config:      &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:    session.NewStore(),
		memoryDir:   t.TempDir(),
		memoryCache: NewMemoryCache(),
	}

	// When user asks to read code but no cwd is set, guidance should appear
	prompt, err := svc.buildSystemPrompt("leia a code base do aurelia", nil, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(prompt, "Codebase / Project Analysis Request") {
		t.Fatal("expected codebase-read guidance when user asks to read code without cwd")
	}
	if !strings.Contains(prompt, "/cwd") {
		t.Fatal("expected /cwd suggestion in codebase-read guidance")
	}
}

func TestCodebaseReadGuidance_SkippedWhenCwdSet(t *testing.T) {
	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/repo/test")
	svc := &Service{
		config:      &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:    sessions,
		memoryDir:   t.TempDir(),
		memoryCache: NewMemoryCache(),
	}

	prompt, err := svc.buildSystemPrompt("leia a code base do aurelia", nil, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(prompt, "Codebase / Project Analysis Request") {
		t.Fatal("codebase-read guidance should NOT appear when cwd is already set")
	}
}

func TestCodebaseReadGuidance_NotInjectedForNormalChat(t *testing.T) {
	svc := &Service{
		config:      &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:    session.NewStore(),
		memoryDir:   t.TempDir(),
		memoryCache: NewMemoryCache(),
	}

	prompt, err := svc.buildSystemPrompt("bom dia", nil, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(prompt, "Codebase / Project Analysis Request") {
		t.Fatal("codebase-read guidance should NOT appear for normal chat messages")
	}
}

func TestCodebaseReadGuidance_IncludesKnownProjectsForUser(t *testing.T) {
	bindings, err := projectbinding.NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bindings.Close()

	ctx := t.Context()
	if err := bindings.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 100, ThreadID: 0},
		CWD:         "/repo/aurelia",
		ProjectSlug: "-repo-aurelia",
		Source:      projectbinding.BindingManual,
		CreatedBy:   42,
	}); err != nil {
		t.Fatal(err)
	}
	if err := bindings.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 200, ThreadID: 0},
		CWD:         "/repo/other-project",
		ProjectSlug: "-repo-other",
		Source:      projectbinding.BindingManual,
		CreatedBy:   42,
	}); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		config:      &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:    session.NewStore(),
		bindings:    bindings,
		memoryDir:   t.TempDir(),
		memoryCache: NewMemoryCache(),
	}

	prompt, err := svc.buildSystemPrompt("leia a code base do aurelia", nil, 42, 1, 0, 42, false)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(prompt, "/repo/aurelia") {
		t.Fatal("expected known project /repo/aurelia in guidance for user 42")
	}
	if !strings.Contains(prompt, "/repo/other-project") {
		t.Fatal("expected known project /repo/other-project in guidance for user 42")
	}
	if !strings.Contains(prompt, "previously bound") {
		t.Fatal("expected 'previously bound' language in guidance")
	}
	if !strings.Contains(prompt, "suggestions, not the active cwd") {
		t.Fatal("expected 'suggestions, not the active cwd' distinction in guidance")
	}
	if !strings.Contains(prompt, "do NOT claim you cannot remember") {
		t.Fatal("expected 'do NOT claim you cannot remember' language in guidance")
	}
}

func TestCodebaseReadGuidance_NoCrossChatForUserIDZero(t *testing.T) {
	bindings, err := projectbinding.NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bindings.Close()

	ctx := t.Context()
	if err := bindings.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 100, ThreadID: 0},
		CWD:         "/repo/aurelia",
		ProjectSlug: "-repo-aurelia",
		Source:      projectbinding.BindingManual,
		CreatedBy:   99,
	}); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		config:      &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:    session.NewStore(),
		bindings:    bindings,
		memoryDir:   t.TempDir(),
		memoryCache: NewMemoryCache(),
	}

	prompt, err := svc.buildSystemPrompt("leia a code base do aurelia", nil, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	// userID=0 means unidentifiable sender — no cross-chat suggestions
	if strings.Contains(prompt, "/repo/aurelia") {
		t.Fatal("should NOT show known projects for userID=0")
	}
	if strings.Contains(prompt, "previously bound") {
		t.Fatal("should NOT show 'previously bound' language for userID=0")
	}
	if strings.Contains(prompt, "other chats") {
		t.Fatal("should NOT mention other chats for userID=0")
	}
}

func TestGeneralCwdGuidance_IncludedWhenCwdEmptyWithKnownProjects(t *testing.T) {
	bindings, err := projectbinding.NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bindings.Close()

	ctx := t.Context()
	if err := bindings.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 100, ThreadID: 0},
		CWD:         "/repo/aurelia",
		ProjectSlug: "-repo-aurelia",
		Source:      projectbinding.BindingManual,
		CreatedBy:   42,
	}); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		config:      &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:    session.NewStore(),
		bindings:    bindings,
		memoryDir:   t.TempDir(),
		memoryCache: NewMemoryCache(),
	}

	// Use a general chat message (NOT codebase-read) to verify broad guidance
	prompt, err := svc.buildSystemPrompt("bom dia, tudo bem?", nil, 42, 1, 0, 42, false)
	if err != nil {
		t.Fatal(err)
	}

	// Should include memory distinction
	if !strings.Contains(prompt, "do NOT claim you cannot remember") {
		t.Fatal("expected 'do NOT claim you cannot remember' memory distinction for general chat")
	}
	// Should include known projects suggestion
	if !strings.Contains(prompt, "/repo/aurelia") {
		t.Fatal("expected known project /repo/aurelia in general cwd guidance")
	}
	if !strings.Contains(prompt, "NOT the active operational cwd") {
		t.Fatal("expected 'NOT the active operational cwd' distinction in general guidance")
	}
}

func TestGeneralCwdGuidance_OmittedForNoKnownProjects(t *testing.T) {
	svc := &Service{
		config:      &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:    session.NewStore(),
		memoryDir:   t.TempDir(),
		memoryCache: NewMemoryCache(),
	}

	// General chat, no cwd, no known projects — should NOT add known-project suggestions
	prompt, err := svc.buildSystemPrompt("bom dia, tudo bem?", nil, 42, 1, 0, 42, false)
	if err != nil {
		t.Fatal(err)
	}

	// Should still include the memory distinction (always there when cwd empty)
	if !strings.Contains(prompt, "do NOT claim you cannot remember") {
		t.Fatal("expected 'do NOT claim you cannot remember' memory distinction even without known projects")
	}
	// Should NOT include known project paths (none exist)
	if strings.Contains(prompt, "NOT the active operational cwd") {
		t.Fatal("should NOT include active cwd distinction when no known projects exist")
	}
}

func TestGeneralCwdGuidance_OmittedForUserIDZero(t *testing.T) {
	bindings, err := projectbinding.NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bindings.Close()

	ctx := t.Context()
	if err := bindings.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 100, ThreadID: 0},
		CWD:         "/repo/secret-project",
		ProjectSlug: "-repo-secret",
		Source:      projectbinding.BindingManual,
		CreatedBy:   99,
	}); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		config:      &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:    session.NewStore(),
		bindings:    bindings,
		memoryDir:   t.TempDir(),
		memoryCache: NewMemoryCache(),
	}

	// userID=0 — unidentifiable sender, should NOT get cross-chat suggestions
	prompt, err := svc.buildSystemPrompt("bom dia", nil, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(prompt, "/repo/secret-project") {
		t.Fatal("should NOT show known projects for userID=0 in general guidance")
	}
	if strings.Contains(prompt, "NOT the active operational cwd") {
		t.Fatal("should NOT include active cwd distinction for userID=0 in general guidance")
	}
}

func TestCodebaseReadGuidance_NoCrossChatForOtherUser(t *testing.T) {
	bindings, err := projectbinding.NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bindings.Close()

	ctx := t.Context()
	// Only user 99 has bindings
	if err := bindings.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 100, ThreadID: 0},
		CWD:         "/repo/secret-project",
		ProjectSlug: "-repo-secret",
		Source:      projectbinding.BindingManual,
		CreatedBy:   99,
	}); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		config:      &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:    session.NewStore(),
		bindings:    bindings,
		memoryDir:   t.TempDir(),
		memoryCache: NewMemoryCache(),
	}

	// User 42 has no bindings — should get standard guidance without suggestions
	prompt, err := svc.buildSystemPrompt("leia a code base do aurelia", nil, 42, 1, 0, 42, false)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(prompt, "/repo/secret-project") {
		t.Fatal("should NOT show another user's projects")
	}
	if !strings.Contains(prompt, "you know its path") {
		t.Fatal("expected standard guidance text when user has no known projects")
	}
}
