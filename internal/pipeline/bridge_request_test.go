package pipeline

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/security"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/users"
)

func TestBuildBridgeRequest_DisablesFileToolsInChatMode(t *testing.T) {
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: session.NewStore(),
		botCwd:   "/tmp/aurelia-daemon",
	}

	req := svc.buildBridgeRequest("oi", "system", nil, 42, 0, 0, false)
	for _, tool := range chatModeDisallowedTools {
		if !slices.Contains(req.Options.DisallowedTools, tool) {
			t.Fatalf("expected %s to be disallowed in chat mode, got %v", tool, req.Options.DisallowedTools)
		}
	}
}

func TestBuildBridgeRequest_OmitsModelOptionsInAutoMode(t *testing.T) {
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: session.NewStore(),
		botCwd:   "/tmp/aurelia-daemon",
	}

	req := svc.buildBridgeRequest("oi", "system", nil, 42, 0, 100, false)
	if req.Options.Provider != "" || req.Options.Model != "" {
		t.Fatalf("expected auto mode to omit provider/model, got provider=%q model=%q", req.Options.Provider, req.Options.Model)
	}
}

func TestBuildBridgeRequest_SendsExplicitModelOptions(t *testing.T) {
	svc := &Service{
		config: &config.AppConfig{
			DefaultProvider: "anthropic",
			DefaultModel:    "claude-sonnet-4-6",
		},
		sessions: session.NewStore(),
		botCwd:   "/tmp/aurelia-daemon",
	}

	req := svc.buildBridgeRequest("oi", "system", nil, 42, 0, 100, false)
	if req.Options.Provider != "anthropic" || req.Options.Model != "claude-sonnet-4-6" {
		t.Fatalf("expected explicit provider/model, got provider=%q model=%q", req.Options.Provider, req.Options.Model)
	}
}

func TestBuildBridgeRequest_AgentModelOverrideWorksWithAutoMode(t *testing.T) {
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: session.NewStore(),
		botCwd:   "/tmp/aurelia-daemon",
	}
	pp := &profiles.PromptProfile{Model: "openai/gpt-5.4"}

	req := svc.buildBridgeRequest("oi", "system", pp, 42, 0, 100, false)
	if req.Options.Provider != "" || req.Options.Model != "openai/gpt-5.4" {
		t.Fatalf("expected only agent model override, got provider=%q model=%q", req.Options.Provider, req.Options.Model)
	}
}

func TestBuildBridgeRequest_AllowsFileToolsWhenCwdBound(t *testing.T) {
	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/repo/aurelia")
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: sessions,
		botCwd:   "/tmp/aurelia-daemon",
	}

	req := svc.buildBridgeRequest("oi", "system", nil, 42, 0, 0, false)
	if len(req.Options.DisallowedTools) != 0 {
		t.Fatalf("expected no chat-mode disallowed tools when cwd is bound, got %v", req.Options.DisallowedTools)
	}
	if req.Options.Cwd != "/repo/aurelia" {
		t.Fatalf("Cwd = %q, want bound cwd", req.Options.Cwd)
	}
}

func TestBuildBridgeRequest_UsesPrivateDefaultCWD(t *testing.T) {
	dir := t.TempDir()
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	resolver := users.NewResolver(t.TempDir())
	store := users.NewStore(resolver)
	if err := store.Save(&users.Profile{UserID: 100, DefaultCWD: dir}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		config:     &config.AppConfig{},
		sessions:   session.NewStore(),
		usersStore: store,
		botCwd:     "/tmp/aurelia-daemon",
	}

	req := svc.buildBridgeRequest("oi", "system", nil, 42, 0, 100, true)
	if req.Options.Cwd != want {
		t.Fatalf("Cwd = %q, want DefaultCWD %q", req.Options.Cwd, want)
	}
	for _, tool := range chatModeDisallowedTools {
		if slices.Contains(req.Options.DisallowedTools, tool) {
			t.Fatalf("did not expect chat-mode tool %s to be disallowed with DefaultCWD: %v", tool, req.Options.DisallowedTools)
		}
	}
}

func TestBuildBridgeRequest_SecurityContext_PrivilegedDowngraded(t *testing.T) {
	svc := &Service{
		config: &config.AppConfig{
			SecurityConfig: security.SecurityConfig{
				Mode:                  security.PolicyBlock,
				AllowPrivilegedAgents: false,
				SensitivePathPatterns: []string{".env"},
				AllowedOutsideCWDPaths: []string{"/backup"},
			},
		},
		sessions: session.NewStore(),
		botCwd:   "/tmp/aurelia-daemon",
	}

	pp := &profiles.PromptProfile{
		Name:              "test-agent",
		CapabilityProfile: "privileged",
	}
	req := svc.buildBridgeRequest("test prompt", "system prompt", pp, 42, 0, 100, false)

	if req.Options.Security == nil {
		t.Fatal("SecurityContext is nil")
	}
	if req.Options.Security.Profile != "execute_safe" {
		t.Errorf("Security.Profile = %q, want execute_safe (downgraded)", req.Options.Security.Profile)
	}
	if len(req.Options.Security.SensitivePaths) == 0 || req.Options.Security.SensitivePaths[0] != ".env" {
		t.Errorf("SensitivePaths = %v, want [.env]", req.Options.Security.SensitivePaths)
	}
	if len(req.Options.Security.AllowedOutsideCWD) == 0 || req.Options.Security.AllowedOutsideCWD[0] != "/backup" {
		t.Errorf("AllowedOutsideCWD = %v, want [/backup]", req.Options.Security.AllowedOutsideCWD)
	}
}

func TestBuildBridgeRequest_SecurityContext_DisallowedToolsSurviveDowngrade(t *testing.T) {
	svc := &Service{
		config: &config.AppConfig{
			SecurityConfig: security.SecurityConfig{
				Mode:                  security.PolicyBlock,
				AllowPrivilegedAgents: false,
			},
		},
		sessions: session.NewStore(),
		botCwd:   "/tmp/aurelia-daemon",
	}

	pp := &profiles.PromptProfile{
		Name:              "test-agent",
		CapabilityProfile: "privileged",
		AllowedTools:      []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "LS"},
		DisallowedTools:   []string{"Edit"},
	}
	req := svc.buildBridgeRequest("test prompt", "system prompt", pp, 42, 0, 100, false)

	if req.Options.Security == nil {
		t.Fatal("SecurityContext is nil")
	}
	if req.Options.Security.Profile != "execute_safe" {
		t.Errorf("Security.Profile = %q, want execute_safe (downgraded)", req.Options.Security.Profile)
	}
	for _, tool := range req.Options.AllowedTools {
		if tool == "Edit" {
			t.Fatal("Edit must not be in AllowedTools after downgrade (agent disallowed it)")
		}
	}
}

func TestBuildBridgeRequest_SecurityContext_ForwardsSensitivePathsAndAllowedOutsideCWD(t *testing.T) {
	svc := &Service{
		config: &config.AppConfig{
			SecurityConfig: security.SecurityConfig{
				Mode:                   security.PolicyWarn,
				SensitivePathPatterns:  []string{".env", "secret"},
				AllowedOutsideCWDPaths: []string{"/tmp/external"},
			},
		},
		sessions: session.NewStore(),
		botCwd:   "/tmp/aurelia-daemon",
	}

	pp := &profiles.PromptProfile{
		Name:              "test-agent",
		CapabilityProfile: "execute_safe",
	}
	req := svc.buildBridgeRequest("test prompt", "system prompt", pp, 42, 0, 100, false)

	if req.Options.Security == nil {
		t.Fatal("SecurityContext is nil")
	}
	if req.Options.Security.Mode != "warn" {
		t.Errorf("Security.Mode = %q, want warn", req.Options.Security.Mode)
	}
	if len(req.Options.Security.SensitivePaths) != 2 {
		t.Fatalf("SensitivePaths = %v, want [.env secret]", req.Options.Security.SensitivePaths)
	}
	if req.Options.Security.SensitivePaths[0] != ".env" {
		t.Errorf("SensitivePaths[0] = %q, want .env", req.Options.Security.SensitivePaths[0])
	}
	if len(req.Options.Security.AllowedOutsideCWD) != 1 {
		t.Fatalf("AllowedOutsideCWD = %v, want [/tmp/external]", req.Options.Security.AllowedOutsideCWD)
	}
	if req.Options.Security.AllowedOutsideCWD[0] != "/tmp/external" {
		t.Errorf("AllowedOutsideCWD[0] = %q, want /tmp/external", req.Options.Security.AllowedOutsideCWD[0])
	}
}

func TestBuildBridgeRequest_SecurityContext_DefaultConfigOnNilService(t *testing.T) {
	svc := &Service{
		sessions: session.NewStore(),
		botCwd:   "/tmp/aurelia-daemon",
	}

	req := svc.buildBridgeRequest("test prompt", "system prompt", nil, 42, 0, 100, false)

	if req.Options.Security == nil {
		t.Fatal("SecurityContext is nil")
	}
	if req.Options.Security.Mode != "block" {
		t.Errorf("Security.Mode = %q, want block (default)", req.Options.Security.Mode)
	}
}