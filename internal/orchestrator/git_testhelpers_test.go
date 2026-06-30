package orchestrator

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Production helpers (e.g. CommitChanges) spawn git without testGitArgs;
	// disable OpenCode protected-branch blocking on ephemeral test repos.
	// Non-empty sentinel: empty string still falls back to "main master" in the hook.
	_ = os.Setenv("OPENCODE_PROTECTED_BRANCHES", "__test_no_protected_branches__")
	os.Exit(m.Run())
}

// testGitArgs prefixes git CLI args so ephemeral test repos do not inherit
// the developer's global core.hooksPath (e.g. OpenCode protected-branch
// pre-commit), which fails on unborn HEAD during the first commit.
func testGitArgs(args ...string) []string {
	return append([]string{"-c", "core.hooksPath="}, args...)
}

func testGitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
		"OPENCODE_PROTECTED_BRANCHES=__test_no_protected_branches__",
	)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", testGitArgs(args...)...)
	cmd.Dir = dir
	cmd.Env = testGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", testGitArgs(args...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func runGitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", testGitArgs(append([]string{"-C", dir}, args...)...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func runGitOutputInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", testGitArgs(append([]string{"-C", dir}, args...)...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}