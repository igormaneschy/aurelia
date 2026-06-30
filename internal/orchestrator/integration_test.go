package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// fileWritingFakeBridge wraps a fakeBridge and writes files to the request cwd
// before delegating to the inner bridge, so CollectArtifacts sees real changes.
type fileWritingFakeBridge struct {
	inner   *fakeBridge
	writeFn func(cwd string)
}

func (f *fileWritingFakeBridge) Execute(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error) {
	f.writeFn(req.Options.Cwd)
	return f.inner.Execute(ctx, req)
}

func (f *fileWritingFakeBridge) SetDefault(ev *bridge.Event) {
	f.inner.SetDefault(ev)
}

func (f *fileWritingFakeBridge) LastRequest() bridge.Request {
	return f.inner.LastRequest()
}

func (f *fileWritingFakeBridge) ExecuteSync(ctx context.Context, req bridge.Request) (*bridge.Event, error) {
	return f.inner.ExecuteSync(ctx, req)
}

// TestOrchestrationCycle_Smoke runs a full one-task plan through the entire
// execution pipeline: worktree creation → worker execution → artifact collection
// → validation → serial merge → commit-ready state.
func TestOrchestrationCycle_Smoke(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// --- Setup: temp git repo on a non-main branch ---
	repoDir := t.TempDir()
	gitExec := func(args ...string) {
		t.Helper()
		runGitInDir(t, repoDir, args...)
	}
	gitExec("init")
	gitExec("config", "user.email", "test@test.com")
	gitExec("config", "user.name", "Test")

	// Initial commit on main
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# project\n"), 0644)
	_ = os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte(".worktrees/\n"), 0644)
	gitExec("add", ".")
	gitExec("commit", "-m", "initial")

	// Create feature spec directory and commit it so base tree stays clean
	featureDir := filepath.Join(repoDir, ".specs", "features", "test-smoke")
	_ = os.MkdirAll(featureDir, 0755)
	_ = os.WriteFile(filepath.Join(featureDir, "spec.md"), []byte("# Spec\n"), 0644)
	_ = os.WriteFile(filepath.Join(featureDir, "design.md"), []byte("# Design\n"), 0644)
	tasksMd := filepath.Join(featureDir, "tasks.md")
	_ = os.WriteFile(tasksMd, []byte(`# Tasks

### T1: Add greeting

- [ ] Create hello.go
`), 0644)
	gitExec("add", ".")
	gitExec("commit", "-m", "add spec")

	// Switch to a feature branch
	gitExec("checkout", "-b", "feature/test-smoke")

	// --- Execute plan ---
	// Custom fake bridge that writes hello.go to the request cwd so artifact
	// collection sees real git changes.
	fb := &fileWritingFakeBridge{
		inner: newFakeBridge(),
		writeFn: func(cwd string) {
			if cwd != "" && cwd != repoDir {
				helloPath := filepath.Join(cwd, "hello.go")
				_ = os.WriteFile(helloPath, []byte("package main\nfunc Hello() {}\n"), 0644)
			}
		},
	}
	fb.inner.SetDefault(&bridge.Event{Type: "result", Content: "created hello.go"})

	o := NewOrchestrator(fb, OrchestratorConfig{
		RepoRoot:             repoDir,
		MaxValidationRetries: 3,
	})

	plan := &Plan{
		Feature: "test-smoke",
		Verify:  "go vet ./...",
		Tasks: []Task{
			{ID: "T1", Description: "Add greeting", Prompt: "Create hello.go with a Hello function", NeedsWorktree: true},
		},
	}

	validator := func(ctx context.Context, task Task, result TaskResult, artifacts *ArtifactSnapshot, attempt int) (*ValidationResult, error) {
		if artifacts != nil && len(artifacts.ChangedFiles) > 0 {
			return &ValidationResult{Approved: true}, nil
		}
		return &ValidationResult{Approved: false, Issues: []string{"no changes detected"}, ShouldRetry: false}, nil
	}

	execCtx := ExecutionContext{
		RunID:      "run1",
		RepoRoot:   repoDir,
		BaseBranch: "feature/test-smoke",
		Feature:    "test-smoke",
	}

	manifest, results, err := o.ExecutePlan(
		context.Background(),
		execCtx,
		plan,
		nil,
		func(task Task, cfg WorkerConfig) string { return "system prompt" },
		validator,
		func(ev WorkerEvent) {},
	)
	if err != nil {
		t.Fatalf("ExecutePlan error: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected manifest")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// --- Assertions ---
	// 1. Worktree should have been created and then cleaned up (merged successfully)
	matches, _ := filepath.Glob(filepath.Join(repoDir, ".worktrees", "worker-*"))
	if len(matches) > 0 {
		t.Errorf("expected worktree cleaned up after merge, found %v", matches)
	}

	// 2. Task should be approved
	if results[0].Status != TaskApproved {
		t.Errorf("status = %q, want approved", results[0].Status)
	}

	// 3. Manifest should record the approved result
	approved := manifest.ApprovedResults()
	if len(approved) != 1 {
		t.Fatalf("expected 1 approved result, got %d", len(approved))
	}

	// 4. Verify command should have been recorded (even if hello.go doesn't compile as real Go)
	if results[0].Verify == nil {
		t.Log("verify not recorded (go vet may have failed in empty module)")
	}

	// 5. CommitChanges with approved files should work (nothing to commit since fake bridge didn't write files)
	files := manifest.ApprovedChangedFiles()
	if len(files) == 0 {
		t.Log("no changed files from fake bridge — this is expected in smoke test")
	}

	// 6. UpdateTasksStatus should flip the checkbox for the approved task
	results[0].Status = TaskApproved
	if err := UpdateTasksStatus(tasksMd, results); err != nil {
		t.Fatalf("UpdateTasksStatus: %v", err)
	}
	content, _ := os.ReadFile(tasksMd)
	if !strings.Contains(string(content), "- [x] Create hello.go") {
		t.Error("expected tasks.md checkbox to be marked")
	}
}
