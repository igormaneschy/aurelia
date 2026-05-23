package orchestrator

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNothingToCommit is returned when CommitChanges is called with an empty
// file list or when the staged diff is empty.
var ErrNothingToCommit = errors.New("nothing to commit")

// CommitChanges stages only the provided files and creates a commit.
// If files is empty, it returns ErrNothingToCommit without running git.
// Unrelated dirty files in the working tree are left unstaged.
func CommitChanges(repoRoot string, files []string, message string) error {
	if len(files) == 0 {
		return ErrNothingToCommit
	}

	// Stage only the provided files (not git add -A).
	args := append([]string{"add"}, files...)
	addCmd := exec.Command("git", args...)
	addCmd.Dir = repoRoot
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, out)
	}

	// Check if there's anything staged to commit
	statusCmd := exec.Command("git", "diff", "--cached", "--quiet")
	statusCmd.Dir = repoRoot
	if err := statusCmd.Run(); err == nil {
		// No diff means nothing staged → unstage and return sentinel error
		_ = exec.Command("git", "reset", "HEAD").Run()
		return ErrNothingToCommit
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = repoRoot
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, out)
	}

	return nil
}

// CreatePR creates a pull request using the gh CLI.
// Returns the PR URL or error.
func CreatePR(repoRoot, title, body, baseBranch string) (string, error) {
	if !IsGHAvailable() {
		return "", fmt.Errorf("gh CLI not available or not authenticated")
	}

	args := []string{"pr", "create", "--title", title, "--body", body}
	if baseBranch != "" {
		args = append(args, "--base", baseBranch)
	}

	cmd := exec.Command("gh", args...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w\n%s", err, out)
	}

	return strings.TrimSpace(string(out)), nil
}

// IsGHAvailable checks if the gh CLI is installed and authenticated.
func IsGHAvailable() bool {
	cmd := exec.Command("gh", "auth", "status")
	return cmd.Run() == nil
}

// GetCurrentBranch returns the current git branch name.
func GetCurrentBranch(repoRoot string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "HEAD"
	}
	return strings.TrimSpace(string(out))
}

// PushBranch pushes the current branch to remote with -u flag.
func PushBranch(repoRoot string) error {
	branch := GetCurrentBranch(repoRoot)
	cmd := exec.Command("git", "push", "-u", "origin", branch)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %w\n%s", err, out)
	}
	return nil
}
