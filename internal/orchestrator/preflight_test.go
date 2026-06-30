package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBaseBranch_NormalBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	wm := NewWorktreeManager(repoDir)
	branch, err := wm.ResolveBaseBranch()
	if err != nil {
		t.Fatalf("ResolveBaseBranch: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want %q", branch, "main")
	}
}

func TestResolveBaseBranch_NonMainBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)
	runGit(t, repoDir, "checkout", "-b", "feature/foo")

	wm := NewWorktreeManager(repoDir)
	branch, err := wm.ResolveBaseBranch()
	if err != nil {
		t.Fatalf("ResolveBaseBranch: %v", err)
	}
	if branch != "feature/foo" {
		t.Errorf("branch = %q, want %q", branch, "feature/foo")
	}
}

func TestResolveBaseBranch_RejectsDetachedHEAD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	// Checkout a specific commit to get into detached HEAD state
	hash := runGitOutput(t, repoDir, "rev-parse", "HEAD")
	runGit(t, repoDir, "checkout", hash)

	wm := NewWorktreeManager(repoDir)
	_, err := wm.ResolveBaseBranch()
	if err != ErrDetachedHEAD {
		t.Fatalf("expected ErrDetachedHEAD, got: %v (type: %T)", err, err)
	}
}

func TestPreflightExecution_RejectsNonGitRepo(t *testing.T) {
	o := NewOrchestrator(nil, OrchestratorConfig{})
	nonGitDir := t.TempDir()

	_, err := o.PreflightExecution(t.Context(), nonGitDir, false)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error should mention git repository, got: %v", err)
	}
}

func TestPreflightExecution_RejectsDetachedHEAD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)
	hash := runGitOutput(t, repoDir, "rev-parse", "HEAD")
	runGit(t, repoDir, "checkout", hash)

	o := NewOrchestrator(nil, OrchestratorConfig{})
	_, err := o.PreflightExecution(t.Context(), repoDir, false)
	if err == nil {
		t.Fatal("expected error for detached HEAD")
	}
	if !strings.Contains(err.Error(), "detached HEAD") && !strings.Contains(err.Error(), "base branch") {
		t.Errorf("error should mention detached HEAD or base branch, got: %v", err)
	}
}

func TestPreflightExecution_RejectsDirtyBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	// Create an uncommitted file
	dirtyFile := filepath.Join(repoDir, "dirty.txt")
	if err := os.WriteFile(dirtyFile, []byte("uncommitted"), 0o644); err != nil {
		t.Fatal(err)
	}

	o := NewOrchestrator(nil, OrchestratorConfig{})
	_, err := o.PreflightExecution(t.Context(), repoDir, false)
	if err == nil {
		t.Fatal("expected error for dirty base tree")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("error should mention uncommitted changes, got: %v", err)
	}
	if !strings.Contains(err.Error(), "dirty.txt") {
		t.Errorf("error should include dirty file path, got: %v", err)
	}
}

func TestPreflightExecution_CleanRepo_Success(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	o := NewOrchestrator(nil, OrchestratorConfig{})
	result, err := o.PreflightExecution(t.Context(), repoDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BaseBranch != "main" {
		t.Errorf("BaseBranch = %q, want %q", result.BaseBranch, "main")
	}
	if len(result.DirtyPaths) != 0 {
		t.Errorf("expected clean tree, got %d dirty paths", len(result.DirtyPaths))
	}
}

func TestPreflightExecution_GHMissingNonFatal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	o := NewOrchestrator(nil, OrchestratorConfig{})
	// createPR=true but gh may not be available — should not error
	result, err := o.PreflightExecution(t.Context(), repoDir, true)
	if err != nil {
		t.Fatalf("missing gh should not block execution: %v", err)
	}
	_ = result // GHAvailable may be true or false depending on machine
}

func TestPreflightExecution_GHIsOptionalForCreatePRFalse(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	o := NewOrchestrator(nil, OrchestratorConfig{})
	result, err := o.PreflightExecution(t.Context(), repoDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GHAvailable {
		t.Log("gh is available on this machine")
	}
}

func TestPreflightUserMessage_NonGitRepo(t *testing.T) {
	msg := PreflightUserMessage(fmt.Errorf("not a git repository: exit status 128\nfatal: not a git repository"))
	if !strings.Contains(msg, "não é um repositório") {
		t.Errorf("expected Portuguese repo message, got: %q", msg)
	}
	if strings.Contains(msg, "exit status") {
		t.Error("user message should not contain raw git output")
	}
}

func TestPreflightUserMessage_DetachedHEAD(t *testing.T) {
	msg := PreflightUserMessage(fmt.Errorf("base branch check: detached HEAD: cannot determine base branch"))
	if !strings.Contains(msg, "detached HEAD") {
		t.Errorf("expected detached HEAD message, got: %q", msg)
	}
}

func TestPreflightUserMessage_DirtyTree(t *testing.T) {
	msg := PreflightUserMessage(fmt.Errorf("base tree has uncommitted changes (2 files): dirty.txt, untracked.go; commit or stash before running plan"))
	if !strings.Contains(msg, "alterações não salvas") {
		t.Errorf("expected Portuguese dirty message, got: %q", msg)
	}
	if strings.Contains(msg, "dirty.txt") {
		t.Error("user message should not contain file paths")
	}
}

func TestPreflightUserMessage_NilError(t *testing.T) {
	if msg := PreflightUserMessage(nil); msg != "" {
		t.Errorf("expected empty for nil error, got: %q", msg)
	}
}

func TestPreflightUserMessage_UnknownError(t *testing.T) {
	msg := PreflightUserMessage(fmt.Errorf("some unexpected internal error"))
	if !strings.Contains(msg, "não está pronto") {
		t.Errorf("expected generic Portuguese message, got: %q", msg)
	}
}

func TestGetDirtyPaths_Clean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	paths, err := getDirtyPaths(t.Context(), repoDir)
	if err != nil {
		t.Fatalf("getDirtyPaths: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected clean tree, got %d paths", len(paths))
	}
}

func TestGetDirtyPaths_Unstaged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)

	dirtyFile := filepath.Join(repoDir, "modified.txt")
	if err := os.WriteFile(dirtyFile, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := getDirtyPaths(t.Context(), repoDir)
	if err != nil {
		t.Fatalf("getDirtyPaths: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("expected dirty paths, got none")
	}
	found := false
	for _, p := range paths {
		if strings.Contains(p, "modified.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'modified.txt' in dirty paths, got %v", paths)
	}
}

// initRepo creates a git repo on main with an initial commit.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "checkout", "-b", "main")

	// Create .gitignore to exclude worktree directories from dirty checks
	gitignore := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignore, []byte(".worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dummyFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(dummyFile, []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial commit")
}

func TestWithRepoRoot_ReturnsCopyWithNewRepoRoot(t *testing.T) {
	repoA := t.TempDir()
	repoB := t.TempDir()

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repoA})

	// Verify original uses repoA
	if o.config.RepoRoot != repoA {
		t.Fatalf("original RepoRoot = %q, want %q", o.config.RepoRoot, repoA)
	}
	if o.worktree == nil {
		t.Fatal("expected original to have worktree manager")
	}

	// Create a copy with repoB
	o2 := o.WithRepoRoot(repoB)
	if o2 == o {
		t.Fatal("WithRepoRoot should return a different instance for different repoRoot")
	}
	if o2.config.RepoRoot != repoB {
		t.Fatalf("copy RepoRoot = %q, want %q", o2.config.RepoRoot, repoB)
	}
	if o2.worktree == nil {
		t.Fatal("expected copy to have worktree manager")
	}

	// Original must be unchanged
	if o.config.RepoRoot != repoA {
		t.Fatalf("original RepoRoot was mutated: got %q, want %q", o.config.RepoRoot, repoA)
	}

	// Both should share the same bridge (nil both, so pointer equality works)
	if o.bridge != o2.bridge {
		t.Fatal("copy should share the same bridge")
	}
}

func TestWithRepoRoot_SamePathReturnsSameInstance(t *testing.T) {
	repo := t.TempDir()
	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo})

	o2 := o.WithRepoRoot(repo)
	if o2 != o {
		t.Fatal("WithRepoRoot should return same instance for same repoRoot")
	}
}

func TestWithRepoRoot_EmptyPathReturnsSameInstance(t *testing.T) {
	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: "/some/path"})

	o2 := o.WithRepoRoot("")
	if o2 != o {
		t.Fatal("WithRepoRoot should return same instance for empty path")
	}
}

func TestWithRepoRoot_CreatesWorktreeForNewRepoRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoA := t.TempDir()
	initRepo(t, repoA)

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repoA})

	// Confirm original worktree resolves branch from repoA
	branch, err := o.worktree.ResolveBaseBranch()
	if err != nil {
		t.Fatalf("original worktree: %v", err)
	}
	t.Logf("original worktree branch: %s", branch)

	// Create a second repo with a different name
	repoB := t.TempDir()
	initRepo(t, repoB)
	runGit(t, repoB, "checkout", "-b", "develop")

	// Copy with repoB
	o2 := o.WithRepoRoot(repoB)
	branch2, err := o2.worktree.ResolveBaseBranch()
	if err != nil {
		t.Fatalf("copy worktree: %v", err)
	}
	if branch2 != "develop" {
		t.Errorf("copy worktree branch = %q, want %q", branch2, "develop")
	}

	// Original still resolves from repoA
	branchOrig, err := o.worktree.ResolveBaseBranch()
	if err != nil {
		t.Fatalf("original worktree after copy: %v", err)
	}
	if branchOrig != "main" && branchOrig != "master" {
		t.Errorf("original worktree still resolves from repoA, got branch = %q", branchOrig)
	}
}

func TestPreflightExecution_UsesRunScopedWithRepoRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create daemon-side orchestrator with RepoRoot not matching handoff cwd
	daemonRepo := t.TempDir()
	initRepo(t, daemonRepo)
	daemonRoot := daemonRepo

	// Create separate handoff cwd with a different branch
	handoffRoot := t.TempDir()
	initRepo(t, handoffRoot)
	runGit(t, handoffRoot, "checkout", "-b", "feature/handoff")

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: daemonRoot})

	// Preflight runs on handoff cwd, not daemon root
	result, err := o.PreflightExecution(t.Context(), handoffRoot, false)
	if err != nil {
		t.Fatalf("PreflightExecution: %v", err)
	}
	if result.BaseBranch != "feature/handoff" {
		t.Errorf("BaseBranch = %q, want %q (handoff cwd)", result.BaseBranch, "feature/handoff")
	}

	// Original orchestrator should still reference daemon root
	if o.config.RepoRoot != daemonRoot {
		t.Errorf("original RepoRoot was mutated: %q", o.config.RepoRoot)
	}

	// Run-scoped copy should use handoff cwd
	runOrch := o.WithRepoRoot(handoffRoot)
	if runOrch.Config().RepoRoot != handoffRoot {
		t.Errorf("run-scoped RepoRoot = %q, want %q", runOrch.Config().RepoRoot, handoffRoot)
	}
}
