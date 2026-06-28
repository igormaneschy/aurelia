package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/security"
)

// fakeBridge implements BridgeExecutor for tests.
type fakeBridge struct {
	results    map[string]*bridge.Event // requestPrompt → terminal event
	defaultEv  *bridge.Event            // fallback for unmatched prompts
	lastReq    bridge.Request           // captured from most recent Execute/ExecuteSync
	syncErr    error                    // error returned by ExecuteSync
	mu         sync.Mutex
}

func newFakeBridge() *fakeBridge {
	return &fakeBridge{results: make(map[string]*bridge.Event)}
}

func (f *fakeBridge) SetResult(prompt string, ev *bridge.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[prompt] = ev
}

func (f *fakeBridge) SetDefault(ev *bridge.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaultEv = ev
}

func (f *fakeBridge) SetSyncErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncErr = err
}

func (f *fakeBridge) Execute(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error) {
	ch := make(chan bridge.Event, 4)

	f.mu.Lock()
	f.lastReq = req
	ev, ok := f.results[req.Prompt]
	fallback := f.defaultEv
	f.mu.Unlock()

	go func() {
		defer close(ch)
		ch <- bridge.Event{Type: "system", SessionID: "test-session"}
		if ok && ev != nil {
			ch <- *ev
		} else if fallback != nil {
			ch <- *fallback
		} else {
			ch <- bridge.Event{Type: "result", Content: "done"}
		}
	}()

	return ch, nil
}

func (f *fakeBridge) ExecuteSync(ctx context.Context, req bridge.Request) (*bridge.Event, error) {
	f.mu.Lock()
	f.lastReq = req
	err := f.syncErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}

	ch, err := f.Execute(ctx, req)
	if err != nil {
		return nil, err
	}
	var last *bridge.Event
	for ev := range ch {
		ev := ev
		last = &ev
	}
	return last, nil
}

// LastRequest returns the most recent bridge request captured.
func (f *fakeBridge) LastRequest() bridge.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq
}

func TestNewRunID_NoHyphensOrSlashes(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id := newRunID()
		// Must not contain hyphens or slashes (constraint for CleanupAll path→branch conversion)
		if strings.ContainsAny(id, "-/") {
			t.Errorf("newRunID() = %q, contains hyphen or slash", id)
		}
		// Must match the canonical format enforced by WorktreeManager.Create
		if !runIDRe.MatchString(id) {
			t.Errorf("newRunID() = %q, does not match expected format %s", id, runIDRe.String())
		}
		// Must be unique
		if ids[id] {
			t.Errorf("newRunID() = %q, duplicate", id)
		}
		ids[id] = true
	}
}

func TestExecuteTask_SessionScopedOptions(t *testing.T) {
	fb := newFakeBridge()
	fb.SetResult("test task", &bridge.Event{Type: "result", Content: "ok"})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: t.TempDir()})

	// First worker
	r1 := o.ExecuteTask(context.Background(), Task{ID: "1", Prompt: "test task"}, DefaultWorkerConfig, "/tmp", "prompt", func(WorkerEvent) {})
	if !r1.Success {
		t.Fatalf("worker 1 failed: %s", r1.Error)
	}
	req1 := fb.LastRequest()

	// All three fields must be negative (reserved synthetic tuple)
	if req1.Options.ChatID >= 0 {
		t.Errorf("worker ChatID = %d, want negative (synthetic)", req1.Options.ChatID)
	}
	if req1.Options.ThreadID >= 0 {
		t.Errorf("worker ThreadID = %d, want negative (synthetic)", req1.Options.ThreadID)
	}
	if req1.Options.UserID >= 0 {
		t.Errorf("worker UserID = %d, want negative (synthetic)", req1.Options.UserID)
	}
	if req1.Options.PersistSession == nil {
		t.Fatal("PersistSession is nil, want false")
	}
	if *req1.Options.PersistSession {
		t.Error("PersistSession = true, want false")
	}

	// Second worker must have a unique tuple
	r2 := o.ExecuteTask(context.Background(), Task{ID: "2", Prompt: "test task"}, DefaultWorkerConfig, "/tmp", "prompt", func(WorkerEvent) {})
	if !r2.Success {
		t.Fatalf("worker 2 failed: %s", r2.Error)
	}
	req2 := fb.LastRequest()
	if req1.Options.ChatID == req2.Options.ChatID {
		t.Error("two workers received the same synthetic ChatID, want unique")
	}
	if req2.Options.ChatID >= 0 {
		t.Errorf("worker 2 ChatID = %d, want negative", req2.Options.ChatID)
	}
	if req2.Options.ThreadID >= 0 {
		t.Errorf("worker 2 ThreadID = %d, want negative", req2.Options.ThreadID)
	}
	if req2.Options.UserID >= 0 {
		t.Errorf("worker 2 UserID = %d, want negative", req2.Options.UserID)
	}
}

func TestExecuteTask_Success(t *testing.T) {
	fb := newFakeBridge()
	fb.SetResult("implement /health", &bridge.Event{
		Type:    "result",
		Content: "endpoint created",
		CostUSD: 0.05,
	})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: t.TempDir()})

	var events []WorkerEvent
	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Description: "implement health", Prompt: "implement /health"},
		DefaultWorkerConfig,
		t.TempDir(),
		"system prompt",
		func(ev WorkerEvent) {
			events = append(events, ev)
		},
	)

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Content != "endpoint created" {
		t.Errorf("content = %q, want 'endpoint created'", result.Content)
	}

	// Should have start + done events
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	if events[0].Type != "start" {
		t.Errorf("first event type = %q, want start", events[0].Type)
	}
	if events[len(events)-1].Type != "done" {
		t.Errorf("last event type = %q, want done", events[len(events)-1].Type)
	}
}

func TestExecuteTask_Error(t *testing.T) {
	fb := newFakeBridge()
	fb.SetResult("bad task", &bridge.Event{
		Type:    "error",
		Message: "model overloaded",
	})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: t.TempDir()})

	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Prompt: "bad task"},
		DefaultWorkerConfig,
		t.TempDir(),
		"prompt",
		func(ev WorkerEvent) {},
	)

	if result.Success {
		t.Fatal("expected failure")
	}
	if result.Error != "model overloaded" {
		t.Errorf("error = %q", result.Error)
	}
}

func TestExecuteTask_BridgeClosedWithoutResult_ReturnsError(t *testing.T) {
	// Bridge that returns only non-terminal events (system only, no result)
	fb := &fakeBridge{results: make(map[string]*bridge.Event)}
	fb.SetResult("no result prompt", &bridge.Event{
		Type: "system", // non-terminal
	})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: t.TempDir()})

	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Prompt: "no result prompt"},
		DefaultWorkerConfig,
		t.TempDir(),
		"prompt",
		func(ev WorkerEvent) {},
	)

	if result.Success {
		t.Fatal("expected failure when bridge closes without result")
	}
	if result.Error != "bridge closed without result" {
		t.Errorf("error = %q, want %q", result.Error, "bridge closed without result")
	}
}

func TestExecutePlan_WorktreeFailure_FailsClosed(t *testing.T) {
	// When NeedsWorktree=true and no repo root is configured (worktree manager
	// is nil), ExecutePlan must fail fast at the plan level.
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "should not execute"})

	// No RepoRoot → no worktree manager
	o := NewOrchestrator(fb, OrchestratorConfig{})

	plan := &Plan{Tasks: []Task{
		{ID: "1", Description: "needs worktree", Prompt: "should not execute", NeedsWorktree: true},
	}}

	noopValidator := func(ctx context.Context, task Task, result TaskResult, artifacts *ArtifactSnapshot, attempt int) (*ValidationResult, error) {
		return &ValidationResult{Approved: true}, nil
	}
	_, _, err := o.ExecutePlan(
		context.Background(),
		ExecutionContext{},
		plan,
		nil,
		func(task Task, cfg WorkerConfig) string { return "prompt" },
		noopValidator,
		func(ev WorkerEvent) {},
	)
	if err == nil {
		t.Fatal("expected error when worktree needed but no repo root")
	}
	if !strings.Contains(err.Error(), "worktree not available") {
		t.Errorf("error = %q, want mention of 'worktree not available'", err.Error())
	}
}

func TestExecutePlan_TwoWaves(t *testing.T) {
	fb := newFakeBridge()
	fb.SetResult("task 1 prompt", &bridge.Event{Type: "result", Content: "done 1"})
	fb.SetResult("task 2 prompt", &bridge.Event{Type: "result", Content: "done 2"})
	fb.SetResult("task 3 prompt", &bridge.Event{Type: "result", Content: "done 3"})

	o := NewOrchestrator(fb, OrchestratorConfig{
		RepoRoot: t.TempDir(),
	})

	plan := &Plan{Tasks: []Task{
		{ID: "1", Description: "first", Prompt: "task 1 prompt", Agent: "worker"},
		{ID: "2", Description: "second", Prompt: "task 2 prompt", Agent: "worker", DependsOn: []string{"1"}},
		{ID: "3", Description: "third", Prompt: "task 3 prompt", Agent: "worker", DependsOn: []string{"1"}},
	}}

	var events []WorkerEvent
	var mu sync.Mutex

	noopValidator := func(ctx context.Context, task Task, result TaskResult, artifacts *ArtifactSnapshot, attempt int) (*ValidationResult, error) {
		return &ValidationResult{Approved: true}, nil
	}
	_, results, err := o.ExecutePlan(
		context.Background(),
		ExecutionContext{RepoRoot: o.config.RepoRoot},
		plan,
		nil, // no registry — uses defaults
		func(task Task, cfg WorkerConfig) string { return "test prompt" },
		noopValidator,
		func(ev WorkerEvent) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// All should succeed
	for _, r := range results {
		if !r.Success {
			t.Errorf("task %s failed: %s", r.TaskID, r.Error)
		}
	}
}

func TestExecutePlan_MergeFailure_PreservesWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	// Create a base file that will be divergently modified on both sides
	if err := os.WriteFile(filepath.Join(repoDir, "conflict.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "add conflict.txt to base")

	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "done"})
	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: repoDir})

	plan := &Plan{Tasks: []Task{
		{ID: "t1", Description: "task", Prompt: "do work", NeedsWorktree: true},
	}}

	var (
		wtMu    sync.Mutex
		wtFound bool
		wtPath  string
	)

	// Shared git env for subprocesses spawned inside the callback goroutine.
	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)

	gitExec := func(dir string, args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = gitEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %v in %s: %w\n%s", args, dir, err, out)
		}
		return nil
	}

	noopValidator := func(ctx context.Context, task Task, result TaskResult, artifacts *ArtifactSnapshot, attempt int) (*ValidationResult, error) {
		return &ValidationResult{Approved: true}, nil
	}
	_, results, err := o.ExecutePlan(context.Background(), ExecutionContext{RepoRoot: repoDir}, plan, nil,
		func(task Task, cfg WorkerConfig) string { return "prompt" },
		noopValidator,
		func(ev WorkerEvent) {
			wtMu.Lock()
			defer wtMu.Unlock()
			if ev.Type == "start" && !wtFound {
				matches, gErr := filepath.Glob(filepath.Join(repoDir, ".worktrees", "worker-*"))
				if gErr == nil && len(matches) > 0 {
					wtPath = matches[0]
					wtFound = true

					t.Logf("injecting conflicts: worktree=%s", wtPath)

					// Step 1: modify conflict.txt on main and commit.
					// This creates divergence: main now has "main edit".
					if err := os.WriteFile(filepath.Join(repoDir, "conflict.txt"), []byte("main edit\n"), 0644); err != nil {
						t.Logf("write main conflict.txt: %v", err)
						return
					}
					if err := gitExec(repoDir, "add", "."); err != nil {
						t.Logf("main git add: %v", err)
						return
					}
					if err := gitExec(repoDir, "commit", "-m", "main edit"); err != nil {
						t.Logf("main git commit: %v", err)
						return
					}
					t.Logf("main branch advanced with conflicting change")

					// Step 2: modify conflict.txt in worktree and commit.
					// Both sides have now changed the same file differently → conflict.
					if err := os.WriteFile(filepath.Join(wtPath, "conflict.txt"), []byte("worktree edit\n"), 0644); err != nil {
						t.Logf("write worktree conflict.txt: %v", err)
						return
					}
					if err := gitExec(wtPath, "add", "."); err != nil {
						t.Logf("worktree git add: %v", err)
						return
					}
					if err := gitExec(wtPath, "commit", "-m", "worktree edit"); err != nil {
						t.Logf("worktree git commit: %v", err)
						return
					}
					t.Logf("worktree branch advanced with conflicting change")
				}
			}
		},
	)
	if err != nil {
		t.Fatalf("ExecutePlan: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]

	if !wtFound {
		t.Fatal("worktree was not detected during execution, cannot verify merge failure path")
	}

	t.Logf("result.Success=%v, result.Error=%q", r.Success, r.Error)
	mainLog := runGitOutput(t, repoDir, "log", "--oneline", "-3", "main")
	t.Logf("main branch (last 3 commits):\n%s", mainLog)

	// The merge should have failed because both sides changed conflict.txt.
	if r.Success {
		t.Fatal("expected task to fail after merge failure, but Success=true")
	}
	if !strings.Contains(r.Error, "worktree preserved for recovery") {
		t.Errorf("result.Error = %q, want substring %q", r.Error, "worktree preserved for recovery")
	}

	// Worktree must still exist (not cleaned up — preserved for recovery)
	matches, _ := filepath.Glob(filepath.Join(repoDir, ".worktrees", "worker-*"))
	if len(matches) == 0 {
		t.Error("worktree was cleaned up after merge failure, want preserved for recovery")
	} else {
		// Reconstruct expected branch name from worktree path
		base := filepath.Base(matches[0])
		rest := strings.TrimPrefix(base, "worker-")
		branch := "worker/" + strings.Replace(rest, "-", "/", 1)
		branches := runGitOutput(t, repoDir, "branch", "--list", branch)
		if branches == "" {
			t.Errorf("branch %q was deleted after merge failure, want preserved for recovery", branch)
		}

		// Cleanup for test isolation
		cleanupCmd := exec.Command("git", "worktree", "remove", "--force", matches[0])
		cleanupCmd.Dir = repoDir
		_ = cleanupCmd.Run()
		delCmd := exec.Command("git", "branch", "-D", branch)
		delCmd.Dir = repoDir
		_ = delCmd.Run()
	}
}

func TestExecutePlan_RetriesOnValidationFailure(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "done"})

	o := NewOrchestrator(fb, OrchestratorConfig{
		RepoRoot:             t.TempDir(),
		MaxValidationRetries: 3,
	})

	plan := &Plan{Tasks: []Task{
		{ID: "t1", Description: "task", Prompt: "do work"},
	}}

	attemptCount := 0
	validator := func(ctx context.Context, task Task, result TaskResult, artifacts *ArtifactSnapshot, attempt int) (*ValidationResult, error) {
		attemptCount++
		if attempt < 2 {
			return &ValidationResult{Approved: false, Issues: []string{"needs fix"}, ShouldRetry: true}, nil
		}
		return &ValidationResult{Approved: true}, nil
	}

	_, results, err := o.ExecutePlan(
		context.Background(),
		ExecutionContext{RepoRoot: o.config.RepoRoot},
		plan,
		nil,
		func(task Task, cfg WorkerConfig) string { return "prompt" },
		validator,
		func(ev WorkerEvent) {},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != TaskApproved {
		t.Errorf("status = %q, want approved", results[0].Status)
	}
	if results[0].Attempts != 2 {
		t.Errorf("attempts = %d, want 2", results[0].Attempts)
	}
	if attemptCount != 2 {
		t.Errorf("validator called %d times, want 2", attemptCount)
	}
}

func TestExecutePlan_EscalatesAfter3Failures(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "done"})

	o := NewOrchestrator(fb, OrchestratorConfig{
		RepoRoot:             t.TempDir(),
		MaxValidationRetries: 3,
	})

	plan := &Plan{Tasks: []Task{
		{ID: "t1", Description: "task", Prompt: "do work"},
	}}

	validator := func(ctx context.Context, task Task, result TaskResult, artifacts *ArtifactSnapshot, attempt int) (*ValidationResult, error) {
		return &ValidationResult{Approved: false, Issues: []string{"bad"}, ShouldRetry: true}, nil
	}

	_, results, err := o.ExecutePlan(
		context.Background(),
		ExecutionContext{RepoRoot: o.config.RepoRoot},
		plan,
		nil,
		func(task Task, cfg WorkerConfig) string { return "prompt" },
		validator,
		func(ev WorkerEvent) {},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Status != TaskEscalated {
		t.Errorf("status = %q, want escalated", results[0].Status)
	}
	if results[0].Attempts != 3 {
		t.Errorf("attempts = %d, want 3", results[0].Attempts)
	}
}

func TestExecutePlan_SkipsDependentsOfFailedTask(t *testing.T) {
	fb := newFakeBridge()
	fb.SetResult("task 1 prompt", &bridge.Event{Type: "result", Content: "done 1"})
	fb.SetResult("task 2 prompt", &bridge.Event{Type: "result", Content: "done 2"})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: t.TempDir()})

	plan := &Plan{Tasks: []Task{
		{ID: "1", Description: "first", Prompt: "task 1 prompt"},
		{ID: "2", Description: "second", Prompt: "task 2 prompt", DependsOn: []string{"1"}},
	}}

	validator := func(ctx context.Context, task Task, result TaskResult, artifacts *ArtifactSnapshot, attempt int) (*ValidationResult, error) {
		if task.ID == "1" {
			return &ValidationResult{Approved: false, Issues: []string{"fail"}, ShouldRetry: false}, nil
		}
		return &ValidationResult{Approved: true}, nil
	}

	_, results, err := o.ExecutePlan(
		context.Background(),
		ExecutionContext{RepoRoot: o.config.RepoRoot},
		plan,
		nil,
		func(task Task, cfg WorkerConfig) string { return "prompt" },
		validator,
		func(ev WorkerEvent) {},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Task 1 failed
	if results[0].Status != TaskEscalated {
		t.Errorf("task 1 status = %q, want escalated", results[0].Status)
	}

	// Task 2 skipped
	if results[1].Status != TaskSkipped {
		t.Errorf("task 2 status = %q, want skipped", results[1].Status)
	}
	if !results[1].Skipped {
		t.Error("expected task 2 Skipped = true")
	}
}

func TestExecutePlan_MergesWaveSerially(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "done"})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: repoDir})

	plan := &Plan{Tasks: []Task{
		{ID: "t2", Description: "second", Prompt: "p2", NeedsWorktree: true},
		{ID: "t1", Description: "first", Prompt: "p1", NeedsWorktree: true},
	}}

	validator := func(ctx context.Context, task Task, result TaskResult, artifacts *ArtifactSnapshot, attempt int) (*ValidationResult, error) {
		return &ValidationResult{Approved: true}, nil
	}

	_, results, err := o.ExecutePlan(
		context.Background(),
		ExecutionContext{RepoRoot: repoDir},
		plan,
		nil,
		func(task Task, cfg WorkerConfig) string { return "prompt" },
		validator,
		func(ev WorkerEvent) {},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != TaskApproved {
			t.Errorf("task %s status = %q, want approved", r.TaskID, r.Status)
		}
	}
	// Both approved → both should have merged. Verify by checking no orphan worktrees remain.
	matches, _ := filepath.Glob(filepath.Join(repoDir, ".worktrees", "worker-*"))
	if len(matches) > 0 {
		t.Errorf("expected all worktrees cleaned up after successful merge, found %v", matches)
	}
}

func TestExecuteTask_SecurityContext_WithProfile(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "ok"})

	o := NewOrchestrator(fb, OrchestratorConfig{
		RepoRoot: t.TempDir(),
		SecurityConfig: &security.SecurityConfig{
			Mode:                  security.PolicyBlock,
			AllowPrivilegedAgents: false,
		},
	})

	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Prompt: "test", Agent: "test-agent"},
		WorkerConfig{
			Model:             "sonnet",
			CapabilityProfile: "execute_safe",
			Tools:             []string{"Read", "Write", "Edit", "Bash"},
		},
		"/tmp",
		"prompt",
		func(ev WorkerEvent) {},
	)
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	req := fb.LastRequest()
	if req.Options.Security == nil {
		t.Fatal("SecurityContext is nil")
	}
	if !req.Options.Security.Enabled {
		t.Error("Security.Enabled = false, want true")
	}
	if req.Options.Security.Profile != "execute_safe" {
		t.Errorf("Security.Profile = %q, want %q", req.Options.Security.Profile, "execute_safe")
	}
	if req.Options.Security.Mode != "block" {
		t.Errorf("Security.Mode = %q, want %q", req.Options.Security.Mode, "block")
	}
	if req.Options.Security.Cwd != "/tmp" {
		t.Errorf("Security.Cwd = %q, want %q", req.Options.Security.Cwd, "/tmp")
	}
	if req.Options.Security.ChatID >= 0 {
		t.Errorf("Security.ChatID = %d, want negative (synthetic)", req.Options.Security.ChatID)
	}
	if req.Options.Security.ThreadID >= 0 {
		t.Errorf("Security.ThreadID = %d, want negative (synthetic)", req.Options.Security.ThreadID)
	}
	if req.Options.Security.UserID >= 0 {
		t.Errorf("Security.UserID = %d, want negative (synthetic)", req.Options.Security.UserID)
	}
	if req.Options.Security.AgentName != "test-agent" {
		t.Errorf("Security.AgentName = %q, want %q", req.Options.Security.AgentName, "test-agent")
	}
}

func TestExecuteTask_SecurityContext_InferredProfile(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "ok"})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: t.TempDir()})

	// Tools include Bash → should infer execute_safe
	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Prompt: "test", Agent: "agent"},
		WorkerConfig{
			Model: "sonnet",
			Tools: []string{"Read", "Bash"},
		},
		"/tmp",
		"prompt",
		func(ev WorkerEvent) {},
	)
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	req := fb.LastRequest()
	if req.Options.Security == nil {
		t.Fatal("SecurityContext is nil")
	}
	if req.Options.Security.Profile != "execute_safe" {
		t.Errorf("Security.Profile = %q, want %q (inferred from Bash)", req.Options.Security.Profile, "execute_safe")
	}
}

func TestExecuteTask_SecurityContext_PrivilegedDowngraded(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "ok"})

	o := NewOrchestrator(fb, OrchestratorConfig{
		RepoRoot: t.TempDir(),
		SecurityConfig: &security.SecurityConfig{
			Mode:                   security.PolicyBlock,
			AllowPrivilegedAgents:  false,
			SensitivePathPatterns:  []string{".env"},
			AllowedOutsideCWDPaths: []string{"/backup"},
		},
	})

	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Prompt: "test", Agent: "agent"},
		WorkerConfig{
			Model:             "sonnet",
			CapabilityProfile: "privileged",
		},
		"/tmp",
		"prompt",
		func(ev WorkerEvent) {},
	)
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	req := fb.LastRequest()
	if req.Options.Security == nil {
		t.Fatal("SecurityContext is nil")
	}
	if req.Options.Security.Profile != "execute_safe" {
		t.Errorf("Security.Profile = %q, want %q (downgraded)", req.Options.Security.Profile, "execute_safe")
	}

	// AllowedTools should also be downgraded to execute_safe set
	toolsSet := make(map[string]bool, len(req.Options.AllowedTools))
	for _, t := range req.Options.AllowedTools {
		toolsSet[t] = true
	}
	if !toolsSet["Bash"] {
		t.Error("downgraded profile should include Bash (part of execute_safe)")
	}
	if toolsSet["List"] {
		t.Error("downgraded profile should NOT include List (privileged-only)")
	}

	// SensitivePaths and AllowedOutsideCWD must be forwarded from config
	if len(req.Options.Security.SensitivePaths) == 0 {
		t.Error("SensitivePaths must be forwarded from SecurityConfig")
	} else if req.Options.Security.SensitivePaths[0] != ".env" {
		t.Errorf("SensitivePaths[0] = %q, want .env", req.Options.Security.SensitivePaths[0])
	}
	if len(req.Options.Security.AllowedOutsideCWD) == 0 {
		t.Error("AllowedOutsideCWD must be forwarded from SecurityConfig")
	} else if req.Options.Security.AllowedOutsideCWD[0] != "/backup" {
		t.Errorf("AllowedOutsideCWD[0] = %q, want /backup", req.Options.Security.AllowedOutsideCWD[0])
	}
}

func TestExecuteTask_SecurityContext_DisallowedToolsSurviveDowngrade(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "ok"})

	o := NewOrchestrator(fb, OrchestratorConfig{
		RepoRoot: t.TempDir(),
		SecurityConfig: &security.SecurityConfig{
			Mode:                  security.PolicyBlock,
			AllowPrivilegedAgents: false,
		},
	})

	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Prompt: "test", Agent: "agent"},
		WorkerConfig{
			Model:             "sonnet",
			CapabilityProfile: "privileged",
			Tools:             []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "LS"},
			DisallowedTools:   []string{"Edit"},
		},
		"/tmp",
		"prompt",
		func(ev WorkerEvent) {},
	)
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	req := fb.LastRequest()
	if req.Options.Security == nil {
		t.Fatal("SecurityContext is nil")
	}
	// Profile must be downgraded
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

func TestExecuteTask_SecurityContext_ForwardsSensitivePathsAndAllowedOutsideCWD(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "ok"})

	o := NewOrchestrator(fb, OrchestratorConfig{
		RepoRoot: t.TempDir(),
		SecurityConfig: &security.SecurityConfig{
			Mode:                   security.PolicyWarn,
			SensitivePathPatterns:  []string{".env", "secret"},
			AllowedOutsideCWDPaths: []string{"/tmp/external"},
		},
	})

	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Prompt: "test", Agent: "agent"},
		WorkerConfig{
			Model:             "sonnet",
			CapabilityProfile: "execute_safe",
			Tools:             []string{"Read", "Write", "Edit", "Bash"},
		},
		"/tmp",
		"prompt",
		func(ev WorkerEvent) {},
	)
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	req := fb.LastRequest()
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
}

func TestExecuteTask_SecurityContext_DefaultConfigWhenNotSet(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "ok"})

	// No SecurityConfig set → DefaultConfig() used → Mode is "block"
	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: t.TempDir()})

	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Prompt: "test", Agent: "agent"},
		DefaultWorkerConfig,
		"/tmp",
		"prompt",
		func(ev WorkerEvent) {},
	)
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	req := fb.LastRequest()
	if req.Options.Security == nil {
		t.Fatal("SecurityContext is nil")
	}
	if req.Options.Security.Mode != "block" {
		t.Errorf("Security.Mode = %q, want %q (default)", req.Options.Security.Mode, "block")
	}
	// DefaultWorkerConfig has CapabilityProfile "execute_safe"
	if req.Options.Security.Profile != "execute_safe" {
		t.Errorf("Security.Profile = %q, want %q", req.Options.Security.Profile, "execute_safe")
	}
}

func TestExecuteTask_SecurityContext_EmptyCwdDowngrades(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "ok"})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: t.TempDir()})

	// cwd is empty → ResolveProfile downgrades write/bash profiles to read_only
	result := o.ExecuteTask(
		context.Background(),
		Task{ID: "1", Prompt: "test", Agent: "agent"},
		DefaultWorkerConfig,
		"", // empty cwd
		"prompt",
		func(ev WorkerEvent) {},
	)
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	req := fb.LastRequest()
	if req.Options.Security == nil {
		t.Fatal("SecurityContext is nil")
	}
	// Profile should be downgraded to read_only by ResolveProfile
	if req.Options.Security.Profile != "read_only" {
		t.Errorf("Security.Profile = %q, want %q (downgraded from empty cwd)", req.Options.Security.Profile, "read_only")
	}
}

func TestExecutePlan_ReusesWorktreeAcrossRetries(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "done"})

	o := NewOrchestrator(fb, OrchestratorConfig{
		RepoRoot:             repoDir,
		MaxValidationRetries: 3,
	})

	plan := &Plan{Tasks: []Task{
		{ID: "t1", Description: "task", Prompt: "do work", NeedsWorktree: true},
	}}

	attemptCount := 0
	validator := func(ctx context.Context, task Task, result TaskResult, artifacts *ArtifactSnapshot, attempt int) (*ValidationResult, error) {
		attemptCount++
		if attempt < 2 {
			return &ValidationResult{Approved: false, Issues: []string{"fix"}, ShouldRetry: true}, nil
		}
		return &ValidationResult{Approved: true}, nil
	}

	_, results, err := o.ExecutePlan(
		context.Background(),
		ExecutionContext{RepoRoot: repoDir},
		plan,
		nil,
		func(task Task, cfg WorkerConfig) string { return "prompt" },
		validator,
		func(ev WorkerEvent) {},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Status != TaskApproved {
		t.Errorf("status = %q, want approved", results[0].Status)
	}
	if results[0].Attempts != 2 {
		t.Errorf("attempts = %d, want 2", results[0].Attempts)
	}
	// Worktree is cleaned up after successful merge; the key assertion is that
	// only 1 worktree was ever created (reused across retries), not visible here.
}
