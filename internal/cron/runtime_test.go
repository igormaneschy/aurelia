package cron

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/persona"
)

// --- fakes ---

type fakeBridgeExecutor struct {
	lastReq bridge.Request
	result  *bridge.Event
	err     error
}

func (f *fakeBridgeExecutor) Execute(_ context.Context, req bridge.Request) (*bridge.Event, error) {
	f.lastReq = req
	return f.result, f.err
}

type fakeRegistry struct {
	agents map[string]*agents.Agent
}

func (f *fakeRegistry) Get(name string) *agents.Agent {
	return f.agents[name]
}

type fakePersona struct {
	prompt string
	err    error
}

func (f *fakePersona) BuildPrompt() (string, error) {
	return f.prompt, f.err
}

func (f *fakePersona) BuildPromptForUser(_ int64, _ persona.UserPromptResolver, _ bool, _ string) (string, error) {
	return f.prompt, f.err
}

// --- tests ---

func TestBridgeCronRuntime_ExecuteJob(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{
		result: &bridge.Event{Type: "result", Content: "daily summary ready"},
	}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{
		"news": {
			Name:         "news",
			Model:        "claude-sonnet-4-20250514",
			Prompt:       "You are a news agent.",
			AllowedTools: []string{"web_search"},
		},
	}}
	persona := &fakePersona{prompt: "I am Aurelia."}

	runtime := NewBridgeCronRuntime(executor, registry, persona, "/tmp/test-memory", "", "")

	job := CronJob{
		ID:           "job-1",
		AgentName:    "news",
		ScheduleType: "cron",
		CronExpr:     "0 8 * * *",
		Prompt:       "Resumo diario de noticias",
		Active:       true,
	}

	result, err := runtime.ExecuteJob(context.Background(), job)
	if err != nil {
		t.Fatalf("ExecuteJob() error = %v", err)
	}
	if result.Output != "daily summary ready" {
		t.Fatalf("unexpected output: %q", result.Output)
	}

	// Verify bridge request
	if executor.lastReq.Command != "query" {
		t.Fatalf("expected command %q, got %q", "query", executor.lastReq.Command)
	}
	if executor.lastReq.Prompt != "Resumo diario de noticias" {
		t.Fatalf("unexpected prompt: %q", executor.lastReq.Prompt)
	}
	if executor.lastReq.Options.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("unexpected model: %q", executor.lastReq.Options.Model)
	}

	// System prompt should contain persona + agent prompt
	sp := executor.lastReq.Options.SystemPrompt
	if sp == "" {
		t.Fatal("system prompt is empty")
	}
	if !contains(sp, "I am Aurelia.") {
		t.Fatalf("system prompt missing persona: %q", sp)
	}
	if !contains(sp, "You are a news agent.") {
		t.Fatalf("system prompt missing agent prompt: %q", sp)
	}
}

func TestBridgeCronRuntime_InjectsCronInstructionsForTargetChat(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{
		result: &bridge.Event{Type: "result", Content: "ok"},
	}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{
		"news": {Name: "news", Prompt: "news agent"},
	}}
	persona := &fakePersona{prompt: "I am Aurelia."}

	runtime := NewBridgeCronRuntime(executor, registry, persona, "", "", "")
	runtime.SetExePath("/opt/aurelia/bin/aurelia")

	job := CronJob{
		ID:           "job-instr",
		AgentName:    "news",
		TargetChatID: 4242,
		Prompt:       "Resumo",
	}

	if _, err := runtime.ExecuteJob(context.Background(), job); err != nil {
		t.Fatalf("ExecuteJob() error = %v", err)
	}

	sp := executor.lastReq.Options.SystemPrompt
	if !contains(sp, "Scheduling Tasks") {
		t.Fatalf("expected cron instructions in system prompt:\n%s", sp)
	}
	if !contains(sp, "--chat-id 4242") {
		t.Fatalf("expected target chat in scheduling instructions:\n%s", sp)
	}
	if !contains(sp, "/opt/aurelia/bin/aurelia cron add") {
		t.Fatalf("expected exePath in scheduling instructions:\n%s", sp)
	}
}

func TestBridgeCronRuntime_NoChatIDSkipsCronInstructions(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{
		result: &bridge.Event{Type: "result", Content: "ok"},
	}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{}}
	persona := &fakePersona{prompt: "base"}

	runtime := NewBridgeCronRuntime(executor, registry, persona, "", "", "")
	runtime.SetExePath("/opt/aurelia/bin/aurelia")

	// TargetChatID 0 → no scheduling section (can't supply --chat-id).
	if _, err := runtime.ExecuteJob(context.Background(), CronJob{ID: "x", Prompt: "p"}); err != nil {
		t.Fatalf("ExecuteJob() error = %v", err)
	}

	if contains(executor.lastReq.Options.SystemPrompt, "Scheduling Tasks") {
		t.Fatalf("did not expect cron instructions when TargetChatID is 0:\n%s", executor.lastReq.Options.SystemPrompt)
	}
}

func TestBridgeCronRuntime_NoAgent(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{
		result: &bridge.Event{Type: "result", Content: "done without agent"},
	}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{}}
	persona := &fakePersona{prompt: "base"}

	runtime := NewBridgeCronRuntime(executor, registry, persona, "/tmp/test-memory", "", "")

	job := CronJob{
		ID:     "job-2",
		Prompt: "test",
	}

	result, err := runtime.ExecuteJob(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "done without agent" {
		t.Fatalf("output = %q", result.Output)
	}

	// No-agent/no-cwd cron must send an explicit non-empty safe AllowedTools
	// list (nil or empty is omitted by omitempty, causing SDK default fallback).
	req := executor.lastReq
	if len(req.Options.AllowedTools) == 0 {
		t.Fatal("AllowedTools must be non-empty for no-agent/no-cwd cron (nil/empty is omitted by omitempty)")
	}
	for _, tool := range req.Options.AllowedTools {
		if tool == "Read" || tool == "Write" || tool == "Edit" || tool == "Bash" {
			t.Fatalf("no-agent/no-cwd cron must not include tool %q in AllowedTools", tool)
		}
	}
}

func TestExtractCwdFromPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prompt string
		want   string
	}{
		{
			name:   "standard format with . Run:",
			prompt: "Set cwd to /Volumes/Dados/Workspaces/AutoTradersOMQS. Run: python script.py",
			want:   "/Volumes/Dados/Workspaces/AutoTradersOMQS",
		},
		{
			name:   "with newline after path",
			prompt: "Set cwd to /home/project\nRun the analysis script",
			want:   "/home/project",
		},
		{
			name:   "path at end of string",
			prompt: "Set cwd to /tmp/test",
			want:   "/tmp/test",
		},
		{
			name:   "no Set cwd to prefix",
			prompt: "Run: python script.py",
			want:   "",
		},
		{
			name:   "empty prompt",
			prompt: "",
			want:   "",
		},
		{
			name:   "path with trailing space",
			prompt: "Set cwd to /some/path. Run: test",
			want:   "/some/path",
		},
		{
			name:   "quoted path with spaces",
			prompt: "Set cwd to \"/some/path with spaces\". Run: test",
			want:   "/some/path with spaces",
		},
		{
			name:   "missing delimiter with long trailing prompt",
			prompt: "Set cwd to /some/path " + strings.Repeat("x", maxCronCwdChars+1),
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCwdFromPrompt(tt.prompt)
			if got != tt.want {
				t.Fatalf("extractCwdFromPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBridgeCronRuntime_NoAgentWithCwdInPrompt(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{
		result: &bridge.Event{Type: "result", Content: "executed with cwd from prompt"},
	}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{}}
	persona := &fakePersona{prompt: "base"}

	runtime := NewBridgeCronRuntime(executor, registry, persona, "/tmp/test-memory", "", "")
	cwd := t.TempDir()

	job := CronJob{
		ID:     "job-cwd",
		Prompt: "Set cwd to " + cwd + ". Run: python funnel_analysis.py",
	}

	result, err := runtime.ExecuteJob(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "executed with cwd from prompt" {
		t.Fatalf("output = %q", result.Output)
	}

	req := executor.lastReq

	// Cwd must be set from the prompt
	if req.Options.Security.Cwd != cwd {
		t.Fatalf("Security.Cwd = %q, want %q", req.Options.Security.Cwd, cwd)
	}

	// Profile must be execute_safe (not observe) so the LLM has Bash/Read/Write
	if req.Options.Security.Profile != "execute_safe" {
		t.Fatalf("Security.Profile = %q, want %q", req.Options.Security.Profile, "execute_safe")
	}

	// Verify execute_safe tools are present
	hasBash := false
	hasRead := false
	for _, t := range req.Options.AllowedTools {
		if t == "Bash" {
			hasBash = true
		}
		if t == "Read" {
			hasRead = true
		}
	}
	if !hasBash {
		t.Fatal("AllowedTools must include Bash for cwd-from-prompt cron")
	}
	if !hasRead {
		t.Fatal("AllowedTools must include Read for cwd-from-prompt cron")
	}
}

func TestBridgeCronRuntime_UsesExplicitJobCwd(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{result: &bridge.Event{Type: "result", Content: "ok"}}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{}}
	persona := &fakePersona{prompt: "base"}
	runtime := NewBridgeCronRuntime(executor, registry, persona, "", "", "")
	cwd := t.TempDir()

	_, err := runtime.ExecuteJob(context.Background(), CronJob{
		ID:     "job-explicit-cwd",
		Cwd:    cwd,
		Prompt: "Run: python report.py",
	})
	if err != nil {
		t.Fatalf("ExecuteJob() error = %v", err)
	}

	if executor.lastReq.Options.Cwd != cwd {
		t.Fatalf("Options.Cwd = %q, want %q", executor.lastReq.Options.Cwd, cwd)
	}
	if executor.lastReq.Options.Security.Profile != "execute_safe" {
		t.Fatalf("Security.Profile = %q, want execute_safe", executor.lastReq.Options.Security.Profile)
	}
}

func TestBridgeCronRuntime_InvalidLongCwdFallsBackSafely(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{result: &bridge.Event{Type: "result", Content: "ok"}}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{}}
	persona := &fakePersona{prompt: "base"}
	runtime := NewBridgeCronRuntime(executor, registry, persona, "", "", "")

	// Invalid cwd → validateCronCwd fails → cwd stripped → no agent present.
	// Fallback chain: observe (no tools) → empty tools stripped → nil slice
	// → final guard injects [Glob, LS] + read_only profile.
	// This ensures a broken cwd never silently grants filesystem access.
	_, err := runtime.ExecuteJob(context.Background(), CronJob{
		ID:     "job-long-cwd",
		Cwd:    "/tmp/" + strings.Repeat("x", maxCronCwdChars+1),
		Prompt: "Run: python report.py",
	})
	if err != nil {
		t.Fatalf("ExecuteJob() error = %v", err)
	}

	if executor.lastReq.Options.Cwd != "" {
		t.Fatalf("Options.Cwd = %q, want empty for invalid long cwd", executor.lastReq.Options.Cwd)
	}
	if executor.lastReq.Options.Security.Profile != "read_only" {
		t.Fatalf("Security.Profile = %q, want read_only fallback", executor.lastReq.Options.Security.Profile)
	}
}

func TestBridgeCronRuntime_PersistsAgentNameRuntimePath(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{result: &bridge.Event{Type: "result", Content: "ok"}}
	cwd := t.TempDir()
	registry := &fakeRegistry{agents: map[string]*agents.Agent{
		"reports": {Name: "reports", Cwd: cwd, CapabilityProfile: "execute_safe"},
	}}
	persona := &fakePersona{prompt: "base"}
	runtime := NewBridgeCronRuntime(executor, registry, persona, "", "", "")

	_, err := runtime.ExecuteJob(context.Background(), CronJob{
		ID:        "job-agent",
		AgentName: "reports",
		Prompt:    "Run: python report.py",
	})
	if err != nil {
		t.Fatalf("ExecuteJob() error = %v", err)
	}

	if executor.lastReq.Options.Cwd != cwd {
		t.Fatalf("Options.Cwd = %q, want agent cwd %q", executor.lastReq.Options.Cwd, cwd)
	}
	if executor.lastReq.Options.Security.AgentName != "reports" {
		t.Fatalf("Security.AgentName = %q, want reports", executor.lastReq.Options.Security.AgentName)
	}
}

func TestBridgeCronRuntime_DefaultModelFallback(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{
		result: &bridge.Event{Type: "result", Content: "ok"},
	}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{}} // no agent
	persona := &fakePersona{prompt: "base"}

	// defaultProvider empty, defaultModel = "big-pickle"
	runtime := NewBridgeCronRuntime(executor, registry, persona, "", "", "big-pickle")

	_, err := runtime.ExecuteJob(context.Background(), CronJob{
		ID:     "job-default-model",
		Prompt: "run report",
	})
	if err != nil {
		t.Fatalf("ExecuteJob() error = %v", err)
	}

	if executor.lastReq.Options.Model != "big-pickle" {
		t.Fatalf("Options.Model = %q, want %q", executor.lastReq.Options.Model, "big-pickle")
	}
}

func TestBridgeCronRuntime_DefaultModelFallback_AgentOverrides(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{
		result: &bridge.Event{Type: "result", Content: "ok"},
	}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{
		"reports": {Name: "reports", Model: "claude-sonnet-4-20250514"},
	}}
	persona := &fakePersona{prompt: "base"}

	// defaultModel should NOT override agent's explicit model
	runtime := NewBridgeCronRuntime(executor, registry, persona, "", "", "big-pickle")

	_, err := runtime.ExecuteJob(context.Background(), CronJob{
		ID:        "job-agent-model",
		AgentName: "reports",
		Prompt:    "run report",
	})
	if err != nil {
		t.Fatalf("ExecuteJob() error = %v", err)
	}

	if executor.lastReq.Options.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("Options.Model = %q, want agent model %q",
			executor.lastReq.Options.Model, "claude-sonnet-4-20250514")
	}
}

func TestBridgeCronRuntime_BridgeError(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{
		result: &bridge.Event{Type: "error", Message: "timeout"},
	}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{
		"test": {Name: "test", Prompt: "test agent"},
	}}
	persona := &fakePersona{prompt: "base"}

	runtime := NewBridgeCronRuntime(executor, registry, persona, "/tmp/test-memory", "", "")

	job := CronJob{ID: "job-3", AgentName: "test", Prompt: "test"}

	_, err := runtime.ExecuteJob(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for bridge error event")
	}
	if !contains(err.Error(), "bridge error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBridgeCronRuntime_BridgeExecuteFailure(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("connection refused")
	executor := &fakeBridgeExecutor{err: expectedErr}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{
		"test": {Name: "test", Prompt: "test agent"},
	}}
	persona := &fakePersona{prompt: "base"}

	runtime := NewBridgeCronRuntime(executor, registry, persona, "/tmp/test-memory", "", "")

	job := CronJob{ID: "job-4", AgentName: "test", Prompt: "test"}

	_, err := runtime.ExecuteJob(context.Background(), job)
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "bridge execute") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBridgeCronRuntime_PersonaError(t *testing.T) {
	t.Parallel()

	executor := &fakeBridgeExecutor{}
	registry := &fakeRegistry{agents: map[string]*agents.Agent{
		"test": {Name: "test", Prompt: "test agent"},
	}}
	persona := &fakePersona{err: errors.New("file not found")}

	runtime := NewBridgeCronRuntime(executor, registry, persona, "/tmp/test-memory", "", "")

	job := CronJob{ID: "job-5", AgentName: "test", Prompt: "test"}

	_, err := runtime.ExecuteJob(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for persona failure")
	}
	if !contains(err.Error(), "build persona prompt") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNotifyingRuntime_Delivers(t *testing.T) {
	t.Parallel()

	inner := &stubRuntime{result: &ExecutionResult{Output: "hello"}, err: nil}
	var delivered bool
	nr := NewNotifyingRuntime(inner, func(_ context.Context, _ CronJob, result *ExecutionResult, _ error) error {
		delivered = true
		if result.Output != "hello" {
			t.Fatalf("unexpected output in delivery: %q", result.Output)
		}
		return nil
	})

	job := CronJob{ID: "job-n1", AgentName: "test", Prompt: "test"}
	result, err := nr.ExecuteJob(context.Background(), job)
	if err != nil {
		t.Fatalf("ExecuteJob() error = %v", err)
	}
	if result.Output != "hello" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if !delivered {
		t.Fatal("delivery func was not called")
	}
}

func TestNotifyingRuntime_NilInner(t *testing.T) {
	t.Parallel()

	nr := NewNotifyingRuntime(nil, nil)
	_, err := nr.ExecuteJob(context.Background(), CronJob{})
	if err == nil {
		t.Fatal("expected error for nil inner runtime")
	}
}

// --- helpers ---

type stubRuntime struct {
	result *ExecutionResult
	err    error
}

func (s *stubRuntime) ExecuteJob(_ context.Context, _ CronJob) (*ExecutionResult, error) {
	return s.result, s.err
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
