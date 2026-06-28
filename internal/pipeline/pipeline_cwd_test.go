package pipeline

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
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

func TestTryExecutePlan_RequiresCWD(t *testing.T) {
	// When a plan is detected but no cwd is bound, tryExecutePlan must refuse
	// execution and send an error without calling ExecuteApprovedPlan.
	fo := &fakeOutput{}
	s := &Service{
		output:       fo,
		orchestrator: orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:     session.NewStore(),
	}

	planText := "Here is the plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"1\",\"description\":\"task 1\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	handled, _ := s.tryExecutePlan(1, 0, 100, planText, 42, false)
	if !handled {
		t.Fatal("expected true (plan found, execution refused)")
	}
	if fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should NOT be called when cwd is missing")
	}
	if fo.lastError == "" {
		t.Fatal("expected an error message about missing cwd")
	}
}

func TestTryExecutePlan_StoresPendingWhenCwdSet(t *testing.T) {
	// When a plan is detected and cwd is set, tryExecutePlan must store the
	// plan as pending (not execute it), and the plan must be retrievable via
	// ExecutePendingPlan with the correct threadID, cwd, and userID.
	sessions := session.NewStore()
	sessions.SetCwd(1, 5, "/repo/project")

	fo := &fakeOutput{}
	s := &Service{
		output:        fo,
		orchestrator:  orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:      sessions,
		pendingPlans:  make(map[string]*pendingPlan),
	}

	planText := "Here is the plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"1\",\"description\":\"task 1\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	_, _ = s.tryExecutePlan(1, 5, 100, planText, 42, false)

	// Plan should NOT be executed yet
	if fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should NOT be called immediately; plan must be pending")
	}

	// Plan should be pending
	if !s.HasPendingPlan(1, 5, 42) {
		t.Fatal("plan should be pending after tryExecutePlan")
	}

	// ExecutePendingPlan should trigger execution with correct params
	fo.planDone = make(chan struct{})
	if s.ExecutePendingPlan(1, 5, 42) != PlanExecuted {
		t.Fatal("ExecutePendingPlan should return PlanExecuted when a pending plan exists")
	}

	select {
	case <-fo.planDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ExecuteApprovedPlan should be called after ExecutePendingPlan")
	}
	if !fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should be called after ExecutePendingPlan")
	}
	if fo.planThreadID != 5 {
		t.Errorf("planThreadID = %d, want %d", fo.planThreadID, 5)
	}
	if fo.planCwd != "/repo/project" {
		t.Errorf("planCwd = %q, want %q", fo.planCwd, "/repo/project")
	}
	if fo.planUserID != 42 {
		t.Errorf("planUserID = %d, want %d", fo.planUserID, 42)
	}
}

func TestTryExecutePlan_UsesPrivateDefaultCWD(t *testing.T) {
	dir := t.TempDir()
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := users.NewStore(users.NewResolver(t.TempDir()))
	if err := store.Save(&users.Profile{UserID: 42, DefaultCWD: dir}); err != nil {
		t.Fatal(err)
	}
	fo := &fakeOutput{planDone: make(chan struct{})}
	s := &Service{
		output:        fo,
		orchestrator:  orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:      session.NewStore(),
		usersStore:    store,
		pendingPlans:  make(map[string]*pendingPlan),
	}

	planText := "Here is the plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"1\",\"description\":\"task 1\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	_, _ = s.tryExecutePlan(1, 0, 100, planText, 42, true)

	// Plan should be pending, not executed immediately
	if fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should NOT be called immediately; plan must be pending")
	}
	if !s.HasPendingPlan(1, 0, 42) {
		t.Fatal("plan should be pending after tryExecutePlan")
	}

	// ExecutePendingPlan should use the correct DefaultCWD
	if s.ExecutePendingPlan(1, 0, 42) != PlanExecuted {
		t.Fatal("ExecutePendingPlan should return PlanExecuted")
	}

	select {
	case <-fo.planDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ExecuteApprovedPlan should be called after ExecutePendingPlan")
	}
	if !fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should be called after ExecutePendingPlan")
	}
	if fo.planCwd != want {
		t.Fatalf("planCwd = %q, want DefaultCWD %q", fo.planCwd, want)
	}
}

func TestBuildBridgeRequest_SecurityContext_PrivilegedDowngraded(t *testing.T) {
	svc := &Service{
		config: &config.AppConfig{
			SecurityConfig: security.SecurityConfig{
				Mode:                  security.PolicyBlock,
				AllowPrivilegedAgents: false,
				SensitivePathPatterns:  []string{".env"},
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
	// SensitivePaths and AllowedOutsideCWD must be forwarded
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

	// DisallowedTools must survive the downgrade
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
	// When Service.config is nil, getSecurityConfig() returns DefaultConfig.
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
