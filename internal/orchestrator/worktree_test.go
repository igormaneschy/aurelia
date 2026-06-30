package orchestrator

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1", "1"},
		{"task-1-implement-health", "task-1-implement-health"},
		{"Task 1: Implement Health", "task-1-implement-health"},
		{"T1", "t1"},
		{"!!!", "task"}, // fallback
		{"a/b/c", "abc"},
	}
	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWorktreeManager_CreateAndCleanup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a temp git repo
	repoDir := t.TempDir()
	run(t, repoDir, "git", "init")
	run(t, repoDir, "git", "config", "user.email", "test@test.com")
	run(t, repoDir, "git", "config", "user.name", "Test")
	run(t, repoDir, "git", "checkout", "-b", "main")

	// Need at least one commit for worktree to work
	dummyFile := filepath.Join(repoDir, "README.md")
	_ = os.WriteFile(dummyFile, []byte("# test"), 0o644)
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "init")

	// Add .gitignore so .worktrees/ doesn't show as dirty
	_ = os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte(".worktrees/\n"), 0o644)
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "add gitignore")

	wm := NewWorktreeManager(repoDir)

	// Create worktree
	wt, err := wm.Create("run1", "t1", "main")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify worktree directory exists and path includes run namespace
	if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
		t.Fatal("worktree directory not created")
	}

	wantBranch := "worker/run1/t1"
	if wt.Branch != wantBranch {
		t.Errorf("branch = %q, want %q", wt.Branch, wantBranch)
	}

	wantPathSuffix := filepath.Join(".worktrees", "worker-run1-t1")
	if !strings.HasSuffix(wt.Path, wantPathSuffix) {
		t.Errorf("path = %q, want suffix %q", wt.Path, wantPathSuffix)
	}

	// Create a file in the worktree
	testFile := filepath.Join(wt.Path, "test.txt")
	_ = os.WriteFile(testFile, []byte("hello"), 0o644)
	run(t, wt.Path, "git", "add", ".")
	run(t, wt.Path, "git", "commit", "-m", "add test file")

	// Merge back
	if err := wm.Merge(wt, "main"); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Verify merged file exists in main repo
	mainTestFile := filepath.Join(repoDir, "test.txt")
	if _, err := os.Stat(mainTestFile); os.IsNotExist(err) {
		t.Error("merged file not found in main repo")
	}

	// Cleanup
	if err := wm.Cleanup(wt); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	// Verify worktree directory removed
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Error("worktree directory not removed after cleanup")
	}
}

func TestWorktreeCreate_UsesRunNamespace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)
	wm := NewWorktreeManager(repoDir)

	wt, err := wm.Create("run42", "Task 1: Implement Health", "main")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Branch must include runID and task slug
	if wt.Branch != "worker/run42/task-1-implement-health" {
		t.Errorf("branch = %q, want %q", wt.Branch, "worker/run42/task-1-implement-health")
	}

	// Path must include runID and task slug
	wantPathSuffix := filepath.Join(".worktrees", "worker-run42-task-1-implement-health")
	if !strings.HasSuffix(wt.Path, wantPathSuffix) {
		t.Errorf("path = %q, want suffix %q", wt.Path, wantPathSuffix)
	}

	// Cleanup
	if err := wm.Cleanup(wt); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
}

func TestWorktreeCreate_RejectsEmptyRunID(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)
	wm := NewWorktreeManager(repoDir)

	_, err := wm.Create("", "t1", "main")
	if err == nil {
		t.Fatal("expected error for empty runID, got nil")
	}
	if !errors.Is(err, ErrInvalidRunID) {
		t.Errorf("error = %v, want ErrInvalidRunID", err)
	}
}

func TestMerge_ChecksOutBaseBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir) // creates on main

	// Create a second branch
	runGit(t, repoDir, "checkout", "-b", "feature/x")

	// Create worktree from main (not current branch)
	wm := NewWorktreeManager(repoDir)
	wt, err := wm.Create("run1", "t1", "main")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Make a commit in the worktree
	dummyFile := filepath.Join(wt.Path, "work.txt")
	_ = os.WriteFile(dummyFile, []byte("work"), 0o644)
	run(t, wt.Path, "git", "add", ".")
	run(t, wt.Path, "git", "commit", "-m", "worktree work")

	// Switch repo root to feature/x (NOT main) before merge
	runGit(t, repoDir, "checkout", "feature/x")

	// Merge should checkout main first, then merge
	if err := wm.Merge(wt, "main"); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// After merge, repo root should be on main (the base branch)
	currentBranch := runGitOutput(t, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	if currentBranch != "main" {
		t.Errorf("after Merge, on branch %q, want %q", currentBranch, "main")
	}

	// Verify merged content exists
	if _, err := os.Stat(filepath.Join(repoDir, "work.txt")); os.IsNotExist(err) {
		t.Error("merged file not found in repo root")
	}

	wm.Cleanup(wt)
}

func TestMerge_RefusesOnDirtyTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)
	wm := NewWorktreeManager(repoDir)

	wt, err := wm.Create("run1", "t1", "main")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Make a commit in the worktree
	dummyFile := filepath.Join(wt.Path, "work.txt")
	_ = os.WriteFile(dummyFile, []byte("work"), 0o644)
	run(t, wt.Path, "git", "add", ".")
	run(t, wt.Path, "git", "commit", "-m", "worktree work")

	// Dirty the repo root
	dirtyFile := filepath.Join(repoDir, "dirty.txt")
	_ = os.WriteFile(dirtyFile, []byte("uncommitted"), 0o644)

	// Merge must refuse
	err = wm.Merge(wt, "main")
	if err == nil {
		t.Fatal("expected error for dirty base tree, got nil")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("error should mention uncommitted changes, got: %v", err)
	}

	wm.Cleanup(wt)
}

func TestCleanupAll_ReturnsCount(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)
	wm := NewWorktreeManager(repoDir)

	// Create two worktrees
	wt1, err := wm.Create("run1", "t1", "main")
	if err != nil {
		t.Fatalf("Create wt1: %v", err)
	}
	wt2, err := wm.Create("run1", "t2", "main")
	if err != nil {
		t.Fatalf("Create wt2: %v", err)
	}

	// CleanupAll should find 2 worktrees
	count, err := wm.CleanupAll()
	if err != nil {
		t.Fatalf("CleanupAll: %v", err)
	}
	if count != 2 {
		t.Errorf("CleanupAll count = %d, want 2", count)
	}

	// Verify directories are gone
	if _, err := os.Stat(wt1.Path); !os.IsNotExist(err) {
		t.Error("wt1 directory should not exist after CleanupAll")
	}
	if _, err := os.Stat(wt2.Path); !os.IsNotExist(err) {
		t.Error("wt2 directory should not exist after CleanupAll")
	}

	// Verify branches were deleted (best-effort, but should succeed)
	branches := runGitOutput(t, repoDir, "branch", "--list", "worker/run1/*")
	if branches != "" {
		t.Errorf("expected no worker branches, got: %s", branches)
	}
}

func TestCleanupAll_ReturnsZeroWhenEmpty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)
	wm := NewWorktreeManager(repoDir)

	count, err := wm.CleanupAll()
	if err != nil {
		t.Fatalf("CleanupAll: %v", err)
	}
	if count != 0 {
		t.Errorf("CleanupAll count = %d, want 0", count)
	}
}

func TestCreate_RejectsInvalidRunID(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)
	wm := NewWorktreeManager(repoDir)

	// These formats must be rejected
	invalid := []string{
		"",            // empty
		"abc",         // no "run" prefix
		"run",         // no digits
		"run-1",       // hyphen not allowed
		"run_1",       // underscore not allowed
		"RUN1",        // uppercase not allowed
		"run1/../foo", // path traversal (also fails regex)
	}
	for _, rid := range invalid {
		_, err := wm.Create(rid, "t1", "main")
		if err == nil {
			t.Errorf("expected error for invalid runID %q", rid)
		} else if !errors.Is(err, ErrInvalidRunID) {
			t.Errorf("runID %q: got error %v, want ErrInvalidRunID", rid, err)
		}
	}

	// These formats must be accepted
	valid := []string{"run1", "run42", "run0", "run999"}
	for _, rid := range valid {
		wt, err := wm.Create(rid, "t1", "main")
		if err != nil {
			t.Errorf("unexpected error for valid runID %q: %v", rid, err)
			continue
		}
		if err := wm.Cleanup(wt); err != nil {
			t.Errorf("Cleanup after valid runID %q: %v", rid, err)
		}
	}
}

// TestWorktreeManager_CrossInstanceSerialization verifies that two
// WorktreeManager instances for the same normalized repo root share the
// same per-repo mutex, ensuring cross-instance serialization of base-repo
// mutations (Merge, Cleanup, CleanupAll).
func TestWorktreeManager_CrossInstanceSerialization(t *testing.T) {
	repoDir := t.TempDir()
	initRepo(t, repoDir)

	// Two managers for the same repo root must share the same lock.
	wm1 := NewWorktreeManager(repoDir)
	wm2 := NewWorktreeManager(repoDir)

	mu1 := repoRootLocker(wm1.repoRoot)
	mu2 := repoRootLocker(wm2.repoRoot)
	if mu1 != mu2 {
		t.Error("two managers for the same repo root must share the same per-repo mutex")
	}

	// Different repo roots must have different locks.
	otherDir := t.TempDir()
	initRepo(t, otherDir)
	wmOther := NewWorktreeManager(otherDir)
	mu3 := repoRootLocker(wmOther.repoRoot)
	if mu1 == mu3 {
		t.Error("different repo roots must have different per-repo mutexes")
	}
}

// TestWorktreeManager_Normalization verifies that two path representations
// of the same directory (e.g. symlinked vs resolved) are normalized to the
// same repo root, and thus share the same per-repo lock.
func TestWorktreeManager_Normalization(t *testing.T) {
	repoDir := t.TempDir()
	initRepo(t, repoDir)

	wm1 := NewWorktreeManager(repoDir)

	// repoDir from t.TempDir is already absolute; verify normalization
	// preserves or resolves to the same canonical path.
	if wm1.repoRoot == "" {
		t.Fatal("repoRoot must not be empty after normalization")
	}
	if !filepath.IsAbs(wm1.repoRoot) {
		t.Errorf("repoRoot must be absolute after normalization, got %q", wm1.repoRoot)
	}
}

// TestWorktreeManager_Normalization_Symlinks verifies that a symlinked path
// and the resolved path produce the same normalized repo root, and thus share
// the same per-repo lock.
func TestWorktreeManager_Normalization_Symlinks(t *testing.T) {
	repoDir := t.TempDir()
	initRepo(t, repoDir)

	// Create a symlink to the repo directory
	linkDir := filepath.Join(t.TempDir(), "mylink")
	if err := os.Symlink(repoDir, linkDir); err != nil {
		t.Skip("symlink not supported:", err)
	}

	wmViaRepo := NewWorktreeManager(repoDir)
	wmViaLink := NewWorktreeManager(linkDir)

	mu1 := repoRootLocker(wmViaRepo.repoRoot)
	mu2 := repoRootLocker(wmViaLink.repoRoot)

	if mu1 != mu2 {
		t.Errorf("symlinked path and resolved path must share the same lock\n  repoRoot: %q\n  linkRoot: %q",
			wmViaRepo.repoRoot, wmViaLink.repoRoot)
	}
}

func TestMerge_ConflictAbortsCleanly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initRepo(t, repoDir)
	wm := NewWorktreeManager(repoDir)

	// Create a worktree and make a conflicting change
	wt, err := wm.Create("run1", "t1", "main")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write a file in the base repo (main branch)
	baseFile := filepath.Join(repoDir, "conflict.txt")
	if err := os.WriteFile(baseFile, []byte("base content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "add base file")

	// Write different content in the worktree (same file, will conflict)
	wtFile := filepath.Join(wt.Path, "conflict.txt")
	if err := os.WriteFile(wtFile, []byte("worktree content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, wt.Path, "add", ".")
	runGit(t, wt.Path, "commit", "-m", "worktree change")

	// Merge must fail with conflict
	if err := wm.Merge(wt, "main"); err == nil {
		t.Fatal("expected merge conflict error, got nil")
	}

	// Verify merge was aborted: no MERGE_HEAD or MERGE_MSG files
	mergeHead := filepath.Join(repoDir, ".git", "MERGE_HEAD")
	if _, err := os.Stat(mergeHead); !os.IsNotExist(err) {
		t.Error("MERGE_HEAD still exists after failed merge (merge-in-progress state)")
	}
	mergeMsg := filepath.Join(repoDir, ".git", "MERGE_MSG")
	if _, err := os.Stat(mergeMsg); !os.IsNotExist(err) {
		t.Error("MERGE_MSG still exists after failed merge (merge-in-progress state)")
	}

	// Verify repo root is on main (checkout was not left on a merge branch)
	currentBranch := runGitOutput(t, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	if currentBranch != "main" {
		t.Errorf("after failed merge, on branch %q, want %q", currentBranch, "main")
	}

	// Verify the base repo is clean (no staged or unmerged changes)
	status := runGitOutput(t, repoDir, "status", "--porcelain")
	if status != "" {
		t.Errorf("expected clean status after abort, got:\n%s", status)
	}

	// Verify the worktree directory still exists (was not removed by abort)
	if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
		t.Error("worktree directory was removed after failed merge, want preserved")
	}

	// Verify the worktree branch still exists (was not deleted by abort)
	branches := runGitOutput(t, repoDir, "branch", "--list", wt.Branch)
	if branches == "" {
		t.Errorf("worktree branch %q was deleted after failed merge, want preserved", wt.Branch)
	}

	// Cleanup the preserved worktree (test cleanup)
	if err := wm.Cleanup(wt); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmdArgs := args
	if name == "git" {
		cmdArgs = testGitArgs(args...)
	}
	cmd := exec.Command(name, cmdArgs...)
	cmd.Dir = dir
	cmd.Env = testGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
