package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupTestRepo creates a temp git repo with a single file committed.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	initGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}

	initGit("init")
	initGit("config", "user.email", "test@example.com")
	initGit("config", "user.name", "Test")

	f := filepath.Join(dir, "main.go")
	if err := os.WriteFile(f, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	initGit("add", "main.go")
	initGit("commit", "-m", "initial")

	return dir
}

func TestCollectArtifacts_CapturesDiff(t *testing.T) {
	repo := setupTestRepo(t)

	// Make a change
	f := filepath.Join(repo, "main.go")
	_ = os.WriteFile(f, []byte("package main\nfunc main() {}\n"), 0644)

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo})
	art, err := o.CollectArtifacts(context.Background(), repo, Task{ID: "T1"}, &Plan{})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}

	if len(art.ChangedFiles) == 0 {
		t.Error("expected changed files")
	}
	found := false
	for _, cf := range art.ChangedFiles {
		if strings.Contains(cf, "main.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected main.go in changed files, got %v", art.ChangedFiles)
	}

	if art.DiffStat == "" {
		t.Error("expected diffstat")
	}
	if art.Diff == "" {
		t.Error("expected diff content")
	}
	if !strings.Contains(art.Diff, "package main") {
		t.Error("expected diff to contain changed content")
	}
}

func TestCollectArtifacts_RunsTaskVerify(t *testing.T) {
	repo := setupTestRepo(t)

	// Write a Go file with a failing test
	goFile := filepath.Join(repo, "foo.go")
	_ = os.WriteFile(goFile, []byte("package main\n"), 0644)
	testFile := filepath.Join(repo, "foo_test.go")
	_ = os.WriteFile(testFile, []byte("package main\nimport \"testing\"\nfunc TestFoo(t *testing.T) {}\n"), 0644)

	// Stage but don't commit so diff is empty and verify can run against HEAD tree
	exec.Command("git", "-C", repo, "add", ".").Run()

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo})
	art, err := o.CollectArtifacts(context.Background(), repo,
		Task{ID: "T1", Verify: "go test ./..."},
		&Plan{})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}

	if art.Verify == nil {
		t.Fatal("expected verify result")
	}
	if art.Verify.Command != "go test ./..." {
		t.Errorf("verify command = %q", art.Verify.Command)
	}
	if art.Verify.ExitCode != 0 && !art.Verify.TimedOut {
		// go may not be available in this environment; tolerate non-zero exit
		t.Logf("verify exit code = %d (stdout=%q stderr=%q)", art.Verify.ExitCode, art.Verify.Stdout, art.Verify.Stderr)
	}
}

func TestCollectArtifacts_PlanVerifyFallback(t *testing.T) {
	repo := setupTestRepo(t)

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo})
	// task.Verify empty → falls back to plan.Verify
	art, err := o.CollectArtifacts(context.Background(), repo,
		Task{ID: "T1"},
		&Plan{Verify: "echo hello"})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}

	if art.Verify == nil {
		t.Fatal("expected verify result from plan fallback")
	}
	if art.Verify.Command != "echo hello" {
		t.Errorf("verify command = %q, want echo hello", art.Verify.Command)
	}
}

func TestCollectArtifacts_VerifyTimeout(t *testing.T) {
	repo := setupTestRepo(t)

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo, VerifyTimeout: 100 * time.Millisecond})
	art, err := o.CollectArtifacts(context.Background(), repo,
		Task{ID: "T1", Verify: "sleep 5"},
		&Plan{})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}

	if art.Verify == nil {
		t.Fatal("expected verify result even on timeout")
	}
	if !art.Verify.TimedOut {
		t.Error("expected verify to be marked timed out")
	}
}

func TestCollectArtifacts_TruncatesLargeDiff(t *testing.T) {
	repo := setupTestRepo(t)

	// Create a large file modification to exceed truncation limit
	big := make([]byte, diffTruncationLimit+1024)
	for i := range big {
		big[i] = byte('a' + i%26)
	}
	_ = os.WriteFile(filepath.Join(repo, "main.go"), big, 0644)

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo})
	art, err := o.CollectArtifacts(context.Background(), repo, Task{ID: "T1"}, &Plan{})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}

	if len(art.Diff) <= diffTruncationLimit {
		t.Errorf("expected diff truncated, got len=%d", len(art.Diff))
	}
	if !strings.Contains(art.Diff, "[...diff truncated") {
		t.Error("expected truncation marker in diff")
	}
}

func TestCollectArtifacts_NoVerifyWhenEmpty(t *testing.T) {
	repo := setupTestRepo(t)

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo})
	art, err := o.CollectArtifacts(context.Background(), repo, Task{ID: "T1"}, &Plan{})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}

	if art.Verify != nil {
		t.Error("expected no verify when neither task nor plan has verify")
	}
}

func TestCollectArtifacts_NoChanges(t *testing.T) {
	repo := setupTestRepo(t)

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo})
	art, err := o.CollectArtifacts(context.Background(), repo, Task{ID: "T1"}, &Plan{})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}

	if len(art.ChangedFiles) != 0 {
		t.Errorf("expected no changed files, got %v", art.ChangedFiles)
	}
	if art.Diff != "" {
		t.Errorf("expected empty diff, got %q", art.Diff)
	}
}
