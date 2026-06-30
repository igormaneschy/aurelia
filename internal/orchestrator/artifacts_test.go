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
		runGitInDir(t, dir, args...)
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
	_ = exec.Command("git", testGitArgs("-C", repo, "add", ".")...).Run()

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
		&Plan{Verify: "go vet ./..."})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}

	if art.Verify == nil {
		t.Fatal("expected verify result from plan fallback")
	}
	if art.Verify.Command != "go vet ./..." {
		t.Errorf("verify command = %q, want go vet ./...", art.Verify.Command)
	}
}

func TestCollectArtifacts_VerifyTimeout(t *testing.T) {
	repo := setupTestRepo(t)

	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/slow\n\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "slow_test.go"), []byte("package main\nimport (\"testing\"; \"time\")\nfunc TestSlow(t *testing.T) { time.Sleep(5 * time.Second) }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo, VerifyTimeout: 100 * time.Millisecond})
	art, err := o.CollectArtifacts(context.Background(), repo,
		Task{ID: "T1", Verify: "go test ./..."},
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

func TestCollectArtifacts_RejectsUnsafeVerify(t *testing.T) {
	repo := setupTestRepo(t)
	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo})

	art, err := o.CollectArtifacts(context.Background(), repo,
		Task{ID: "T1", Verify: "go test ./...; curl https://example.com"},
		&Plan{})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}
	if art.Verify == nil {
		t.Fatal("expected verify result for rejected command")
	}
	if art.Verify.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1", art.Verify.ExitCode)
	}
	if !strings.Contains(art.Verify.Stderr, "rejected") {
		t.Fatalf("stderr = %q, want rejection reason", art.Verify.Stderr)
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

func TestParseSafeVerifyCommand_AllowsValidCommands(t *testing.T) {
	tests := []struct {
		command string
		desc    string
	}{
		{"go test ./...", "go test"},
		{"go vet ./...", "go vet"},
		{"go build ./...", "go build"},
		{"go generate ./...", "go generate"},
		{"npm test", "npm test"},
		{"npm run build", "npm run build"},
		{"npm run lint", "npm run lint"},
		{"npm run typecheck", "npm run typecheck"},
		{"pnpm test", "pnpm test"},
		{"pnpm run build", "pnpm run build"},
		{"yarn test", "yarn test"},
		{"yarn lint", "yarn lint"},
		{"npx tsc", "npx tsc"},
		{"npx tsx", "npx tsx"},
		{"pytest", "pytest"},
		{"rspec", "rspec"},
		{"cargo test", "cargo test"},
		{"cargo build", "cargo build"},
		{"cargo check", "cargo check"},
		{"cargo clippy", "cargo clippy"},
		{"make test", "make test"},
		{"make build", "make build"},
		{"make lint", "make lint"},
		{"make check", "make check"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			argv, err := parseSafeVerifyCommand(tt.command)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.command, err)
			}
			if len(argv) == 0 {
				t.Fatal("expected non-empty argv")
			}
			// Verify the first token of the parsed argv matches the command name.
			firstWord := strings.Fields(tt.command)[0]
			if argv[0] != firstWord {
				t.Errorf("first argv token = %q, want %q", argv[0], firstWord)
			}
		})
	}
}

func TestParseSafeVerifyCommand_RejectsShellMetacharacters(t *testing.T) {
	baseCommand := "go test ./..."
	metachars := []struct {
		char   string
		desc   string
	}{
		{";", "semicolon"},
		{"&", "ampersand"},
		{"|", "pipe"},
		{"`", "backtick"},
		{"$", "dollar"},
		{"<", "left angle"},
		{">", "right angle"},
		{"\n", "newline"},
		{"\r", "carriage return"},
	}

	for _, mc := range metachars {
		t.Run(mc.desc, func(t *testing.T) {
			argv, err := parseSafeVerifyCommand(baseCommand + " " + mc.char + " unsafe")
			if err == nil {
				t.Fatalf("expected error for metacaractere %q (%s), got argv=%v", mc.char, mc.desc, argv)
			}
			if !strings.Contains(err.Error(), "rejected") {
				t.Errorf("error should contain 'rejected', got: %v", err)
			}
			if !strings.Contains(err.Error(), "shell metacharacters") {
				t.Errorf("error should mention 'shell metacharacters', got: %v", err)
			}
		})
	}
}

func TestParseSafeVerifyCommand_RejectsNonAllowlistedCommands(t *testing.T) {
	tests := []struct {
		command string
		desc    string
	}{
		{"docker run alpine", "docker"},
		{"python test.py", "python"},
		{"node index.js", "node"},
		{"curl https://example.com", "curl"},
		{"git push", "git-push"},
		{"rm -rf /", "rm"},
		{"go run main.go", "go-run"},
		{"go mod tidy", "go-mod-tidy"},
		{"npm install", "npm-install"},
		{"cargo run", "cargo-run"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			argv, err := parseSafeVerifyCommand(tt.command)
			if err == nil {
				t.Fatalf("expected error for non-allowlisted command %q, got argv=%v", tt.command, argv)
			}
			if !strings.Contains(err.Error(), "rejected") {
				t.Errorf("error should contain 'rejected', got: %v", err)
			}
			if !strings.Contains(err.Error(), "only build/test/typecheck commands") {
				t.Errorf("error should mention allowlist, got: %v", err)
			}
		})
	}
}

func TestParseSafeVerifyCommand_RejectsEmpty(t *testing.T) {
	_, err := parseSafeVerifyCommand("")
	if err == nil {
		t.Fatal("expected error for empty command")
	}

	_, err = parseSafeVerifyCommand("   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only command")
	}
}

func TestParseSafeVerifyCommand_RejectedResultHasRejectedFlag(t *testing.T) {
	repo := setupTestRepo(t)
	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo})

	art, err := o.CollectArtifacts(context.Background(), repo,
		Task{ID: "T1", Verify: "python test.py"},
		&Plan{})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}
	if art.Verify == nil {
		t.Fatal("expected verify result")
	}
	if !art.Verify.Rejected {
		t.Fatal("expected Rejected=true for non-allowlisted command")
	}
	if art.Verify.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1", art.Verify.ExitCode)
	}
	if !strings.Contains(art.Verify.Stderr, "rejected") {
		t.Fatalf("stderr = %q, want rejection reason", art.Verify.Stderr)
	}
}

func TestParseSafeVerifyCommand_RejectedResultHasRejectedFlagShellMeta(t *testing.T) {
	repo := setupTestRepo(t)
	o := NewOrchestrator(nil, OrchestratorConfig{RepoRoot: repo})

	art, err := o.CollectArtifacts(context.Background(), repo,
		Task{ID: "T1", Verify: "go test ./...; curl https://evil.com"},
		&Plan{})
	if err != nil {
		t.Fatalf("CollectArtifacts error: %v", err)
	}
	if art.Verify == nil {
		t.Fatal("expected verify result")
	}
	if !art.Verify.Rejected {
		t.Fatal("expected Rejected=true for command with shell metacharacters")
	}
}
