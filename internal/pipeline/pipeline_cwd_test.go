package pipeline

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
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
	agent := &agents.Agent{Model: "openai/gpt-5.4"}

	req := svc.buildBridgeRequest("oi", "system", agent, 42, 0, 100, false)
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

func TestTryExecutePlan_PassesThreadAndCWD(t *testing.T) {
	// When a plan is detected and cwd is set, tryExecutePlan must pass
	// threadID, cwd, and userID through to ExecuteApprovedPlan.
	sessions := session.NewStore()
	sessions.SetCwd(1, 5, "/repo/project")

	fo := &fakeOutput{planDone: make(chan struct{})}
	s := &Service{
		output:       fo,
		orchestrator: orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:     sessions,
	}

	planText := "Here is the plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"1\",\"description\":\"task 1\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	_, _ = s.tryExecutePlan(1, 5, 100, planText, 42, false)

	select {
	case <-fo.planDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ExecuteApprovedPlan should be called when cwd is set")
	}
	if !fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should be called when cwd is set")
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
		output:       fo,
		orchestrator: orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:     session.NewStore(),
		usersStore:   store,
	}

	planText := "Here is the plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"1\",\"description\":\"task 1\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	_, _ = s.tryExecutePlan(1, 0, 100, planText, 42, true)

	select {
	case <-fo.planDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ExecuteApprovedPlan should be called with private DefaultCWD")
	}
	if !fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should be called with private DefaultCWD")
	}
	if fo.planCwd != want {
		t.Fatalf("planCwd = %q, want DefaultCWD %q", fo.planCwd, want)
	}
}
