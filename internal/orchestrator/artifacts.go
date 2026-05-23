package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ArtifactSnapshot captures the post-execution state of a worktree:
// changed files, git status, diff statistics, and truncated diff content.
type ArtifactSnapshot struct {
	ChangedFiles []string
	Status       string
	DiffStat     string
	Diff         string
	Verify       *VerifyResult
}

// VerifyResult holds the outcome of running a verification command
// in the worktree (e.g. go test ./...).
type VerifyResult struct {
	Command    string
	ExitCode   int
	Stdout     string
	Stderr     string
	DurationMs int64
	TimedOut   bool
}

// diffTruncationLimit is the maximum bytes of diff content preserved
// for the validation prompt. Beyond this, diff is truncated with a marker.
const diffTruncationLimit = 64 * 1024 // 64 KB

// CollectArtifacts gathers git status, diff statistics, diff content,
// and optionally runs a verify command for a completed worker task.
//
// The verify command precedence is:
//   1. task.Verify (if non-empty)
//   2. plan.Verify (if non-empty)
//   3. none (Verify is nil)
//
// The verify command runs with the orchestrator's configured VerifyTimeout
// (default 2 minutes). Diff content is truncated at diffTruncationLimit
// with an explicit marker so validation can still reason about changed files
// and diffstat.
func (o *Orchestrator) CollectArtifacts(ctx context.Context, cwd string, task Task, plan *Plan) (*ArtifactSnapshot, error) {
	art := &ArtifactSnapshot{}

	// 1. Changed files via git status --porcelain
	statusOut, err := gitCommand(ctx, cwd, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	art.Status = string(statusOut)
	art.ChangedFiles = parseChangedFiles(statusOut)

	// 2. Diffstat
	diffStatOut, err := gitCommand(ctx, cwd, "diff", "--stat")
	if err != nil {
		return nil, fmt.Errorf("git diff --stat: %w", err)
	}
	art.DiffStat = string(diffStatOut)

	// 3. Diff content (truncated)
	diffOut, err := gitCommand(ctx, cwd, "diff", "--", ".")
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	art.Diff = truncateDiff(string(diffOut))

	// 4. Verify command (if any)
	verifyCmd := resolveVerifyCommand(task, plan)
	if verifyCmd != "" {
		vr, err := runVerify(ctx, cwd, verifyCmd, o.config.VerifyTimeout)
		if err != nil {
			// Verification infrastructure failure is recorded but does not
			// abort artifact collection — the caller decides what to do.
			// Preserve the returned vr if present (it may carry TimedOut).
			if vr == nil {
				vr = &VerifyResult{Command: verifyCmd}
			}
			if vr.Stderr == "" {
				vr.Stderr = err.Error()
			}
		}
		art.Verify = vr
	}

	return art, nil
}

// resolveVerifyCommand returns the effective verify command for a task,
// using task-level override with plan-level fallback.
func resolveVerifyCommand(task Task, plan *Plan) string {
	if task.Verify != "" {
		return task.Verify
	}
	if plan != nil && plan.Verify != "" {
		return plan.Verify
	}
	return ""
}

// gitCommand executes a git subcommand in the given directory and returns stdout.
func gitCommand(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	return cmd.Output()
}

// runVerify executes the verify shell command in the given directory with
// the specified timeout. It captures stdout, stderr, exit code, and duration.
func runVerify(ctx context.Context, cwd, command string, timeout time.Duration) (*VerifyResult, error) {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	vctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(vctx, "sh", "-c", command)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	dur := time.Since(start).Milliseconds()

	vr := &VerifyResult{
		Command:    command,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: dur,
	}

	// Check context timeout before interpreting command exit errors,
	// because a killed process produces ExitError, not ctx error.
	if vctx.Err() == context.DeadlineExceeded {
		vr.TimedOut = true
		return vr, fmt.Errorf("verify timed out after %s", timeout)
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		vr.ExitCode = exitErr.ExitCode()
		return vr, nil // non-zero exit is a concrete verify result, not an infra error
	}
	if err != nil {
		return vr, err
	}
	return vr, nil
}

// parseChangedFiles extracts file names from git status --porcelain output.
func parseChangedFiles(status []byte) []string {
	var files []string
	for _, raw := range bytes.Split(status, []byte("\n")) {
		// porcelain format: XY <path> or XY <path> -> <path2>
		// Do NOT TrimSpace the whole line because leading spaces are
		// significant status indicators (e.g. " M" = modified in worktree).
		line := bytes.TrimSuffix(raw, []byte("\n"))
		if len(line) == 0 {
			continue
		}
		if len(line) >= 3 && line[2] == ' ' {
			rest := string(bytes.TrimSpace(line[3:]))
			// Handle rename arrow
			if idx := strings.Index(rest, " -> "); idx != -1 {
				rest = strings.TrimSpace(rest[idx+len(" -> "):])
			}
			if rest != "" {
				files = append(files, rest)
			}
		}
	}
	return files
}

// truncateDiff limits diff text to diffTruncationLimit bytes. If truncated,
// an explicit marker is appended so validators know the diff is incomplete.
func truncateDiff(diff string) string {
	if len(diff) <= diffTruncationLimit {
		return diff
	}
	truncated := diff[:diffTruncationLimit]
	marker := fmt.Sprintf("\n\n[...diff truncated at %d bytes; %d bytes omitted...]",
		diffTruncationLimit, len(diff)-diffTruncationLimit)
	return truncated + marker
}
