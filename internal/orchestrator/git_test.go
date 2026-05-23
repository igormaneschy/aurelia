package orchestrator

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepoForCommit(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestCommitChanges_StagesOnlyProvidedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	initRepoForCommit(t, dir)

	// Create initial commit
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGitInDir(t, dir, "add", "main.go")
	runGitInDir(t, dir, "commit", "-m", "initial")

	// Create two new files, only stage one
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main\n"), 0644)

	err := CommitChanges(dir, []string{"a.go"}, "add a")
	if err != nil {
		t.Fatalf("CommitChanges error: %v", err)
	}

	// Verify only a.go was committed
	out := runGitOutputInDir(t, dir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
	if !strings.Contains(out, "a.go") {
		t.Errorf("expected a.go in commit, got %q", out)
	}
	if strings.Contains(out, "b.go") {
		t.Errorf("b.go should not be in commit, got %q", out)
	}

	// b.go should remain untracked in working tree
	statusOut := runGitOutputInDir(t, dir, "status", "--porcelain")
	if !strings.Contains(statusOut, "b.go") {
		t.Errorf("expected b.go to remain untracked, got %q", statusOut)
	}
}

func TestCommitChanges_ErrNothingToCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	initRepoForCommit(t, dir)

	// Empty file list → ErrNothingToCommit immediately
	err := CommitChanges(dir, []string{}, "empty")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Errorf("expected ErrNothingToCommit, got %v", err)
	}

	// File list with already-committed file → ErrNothingToCommit after staging
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGitInDir(t, dir, "add", "main.go")
	runGitInDir(t, dir, "commit", "-m", "initial")

	err = CommitChanges(dir, []string{"main.go"}, "no change")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Errorf("expected ErrNothingToCommit for already-committed file, got %v", err)
	}
}

func TestCommitChanges_NoDirtyFilesStaged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	initRepoForCommit(t, dir)

	// Initial commit
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGitInDir(t, dir, "add", "main.go")
	runGitInDir(t, dir, "commit", "-m", "initial")

	// Modify main.go and create a new file
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main\n"), 0644)

	// Commit only new.go — main.go changes should NOT be staged
	err := CommitChanges(dir, []string{"new.go"}, "add new")
	if err != nil {
		t.Fatalf("CommitChanges error: %v", err)
	}

	// Verify main.go is still modified in working tree
	statusOut := runGitOutputInDir(t, dir, "status", "--porcelain")
	if !strings.Contains(statusOut, "main.go") {
		t.Errorf("expected main.go to remain modified in working tree, got %q", statusOut)
	}
}

func runGitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func runGitOutputInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
