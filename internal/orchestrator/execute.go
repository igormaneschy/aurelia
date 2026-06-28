package orchestrator

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/security"
)

// runIDCounter produces unique run identifiers for ExecutePlan worktree namespaces.
var runIDCounter int64

// newRunID generates a short filesystem-safe run identifier with no hyphens
// or slashes. This is a constraint: CleanupAll's path-to-branch conversion
// depends on the absence of hyphens in the runID to correctly reconstruct
// branch names from worktree paths.
func newRunID() string {
	c := atomic.AddInt64(&runIDCounter, 1)
	return fmt.Sprintf("run%d", c)
}

// workerSessionCounter produces unique, all-negative synthetic session tuples
// for worker bridge requests. Real app session keys may have a positive or
// negative ChatID (group chats are negative), but ThreadID is always ≥0 and
// UserID is positive or zero (transitional paths). An all-negative
// (ChatID, ThreadID, UserID) tuple is therefore reserved for internal worker
// sessions and cannot collide with real session keys.
var workerSessionCounter int64

// Validator is the callback used by ExecutePlan to validate a worker's result.
// It receives the task, bridge result, collected artifacts, and attempt number.
// Implementations should be fail-closed: return error for infrastructure failures.
type Validator func(ctx context.Context, task Task, result TaskResult, artifacts *ArtifactSnapshot, attempt int) (*ValidationResult, error)

// ExecuteTask runs a single task as a worker via the bridge.
// It streams events and calls onEvent for visual feedback.
func (o *Orchestrator) ExecuteTask(
	ctx context.Context,
	task Task,
	cfg WorkerConfig,
	cwd string,
	systemPrompt string,
	onEvent func(WorkerEvent),
) TaskResult {
	start := time.Now()

	onEvent(WorkerEvent{TaskID: task.ID, Type: "start", Message: task.Description})

	// Each worker gets a synthetic session scope where ChatID, ThreadID, and
	// UserID are all negative. Real ChatID can be positive or negative (group
	// chats), but ThreadID is always ≥0 and UserID is positive/zero in this
	// app. An all-negative tuple is therefore reserved for internal workers and
	// cannot collide with real session keys. Session persistence is disabled
	// so parallel workers never share or overwrite each other's state.
	workerID := atomic.AddInt64(&workerSessionCounter, 1)
	nID := -workerID
	req := bridge.Request{
		Command: "query",
		Prompt:  task.Prompt,
		Options: bridge.RequestOptions{
			Model:           cfg.Model,
			Cwd:             cwd,
			SystemPrompt:    systemPrompt,
			AllowedTools:    cfg.Tools,
			DisallowedTools: cfg.DisallowedTools,
			NoUserSettings:  true,
			ChatID:          nID,
			ThreadID:        int(nID),
			UserID:          nID,
			PersistSession:  boolPtr(false),
		},
	}

	// ── Resolve and attach security context ──
	capProfile := security.CapabilityProfile(cfg.CapabilityProfile)
	if capProfile == "" {
		// Infer profile from configured tools
		capProfile = inferCapabilityProfile(cfg.Tools)
	}

	secCfg := security.DefaultConfig()
	if o.config.SecurityConfig != nil {
		secCfg = *o.config.SecurityConfig
	}

	_, effectiveTools, secCtx := bridge.BuildSecurityContext(
		capProfile,
		cfg.Tools,
		cfg.DisallowedTools,
		cwd != "",
		&secCfg,
		cwd,
		nID,
		int(nID),
		nID,
		task.Agent,
		"",
	)
	req.Options.AllowedTools = effectiveTools
	req.Options.Security = secCtx

	ch, err := o.bridge.Execute(ctx, req)
	if err != nil {
		onEvent(WorkerEvent{TaskID: task.ID, Type: "error", Message: err.Error()})
		return TaskResult{
			TaskID:     task.ID,
			Success:    false,
			Error:      err.Error(),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	var content string
	var costUSD float64
	var numTurns int
	// gotResult prevents false success when the bridge closes without returning
	// a "result" event. Channel ordering guarantees happens-before: a result
	// event is always observable on the channel before the channel is closed.
	var gotResult bool

	for ev := range ch {
		switch ev.Type {
		case "tool_use":
			onEvent(WorkerEvent{TaskID: task.ID, Type: "progress", ToolName: ev.Name})
		case "assistant":
			if ev.Text != "" {
				content = ev.Text
			}
		case "result":
			gotResult = true
			if ev.Content != "" {
				content = ev.Content
			}
			costUSD = ev.CostUSD
			numTurns = ev.NumTurns
		case "error":
			errMsg := ev.Message
			if errMsg == "" {
				errMsg = "unknown bridge error"
			}
			onEvent(WorkerEvent{TaskID: task.ID, Type: "error", Message: errMsg})
			return TaskResult{
				TaskID:     task.ID,
				Success:    false,
				Content:    content,
				Error:      errMsg,
				DurationMs: time.Since(start).Milliseconds(),
				CostUSD:    costUSD,
			}
		}
	}

	duration := time.Since(start).Milliseconds()
	if !gotResult {
		onEvent(WorkerEvent{TaskID: task.ID, Type: "error", Message: "bridge closed without result"})
		return TaskResult{
			TaskID:     task.ID,
			Success:    false,
			Content:    content,
			Error:      "bridge closed without result",
			DurationMs: duration,
			CostUSD:    costUSD,
		}
	}

	onEvent(WorkerEvent{TaskID: task.ID, Type: "done", Message: fmt.Sprintf("Completed in %dms (%d turns)", duration, numTurns)})

	return TaskResult{
		TaskID:     task.ID,
		Content:    content,
		Success:    true,
		DurationMs: duration,
		CostUSD:    costUSD,
	}
}

// ExecutePlan executes all tasks in the plan, respecting dependencies (wave-based).
// It resolves agent config per task, manages worktrees, validates results,
// retries on validation failure, and merges approved worktrees serially after
// each wave.
//
// The signature includes ExecutionContext for run metadata and a Validator
// callback so the orchestrator package stays unaware of Telegram concerns.
func (o *Orchestrator) ExecutePlan(
	ctx context.Context,
	exec ExecutionContext,
	plan *Plan,
	registry *agents.Registry,
	systemPromptBuilder func(task Task, cfg WorkerConfig) string,
	validate Validator,
	onEvent func(WorkerEvent),
) (*ExecutionManifest, []TaskResult, error) {
	waves, err := plan.ExecutionOrder()
	if err != nil {
		return nil, nil, fmt.Errorf("resolving execution order: %w", err)
	}

	// Determine if any task requires a worktree
	needsWorktree := planHasWorktree(waves)
	if needsWorktree && o.worktree == nil {
		return nil, nil, fmt.Errorf("worktree not available: no repo root configured")
	}

	// Resolve base branch once if worktrees are needed
	var baseBranch string
	var runID string
	if needsWorktree {
		var bbErr error
		baseBranch, bbErr = resolveBaseBranch(ctx, exec.RepoRoot)
		if bbErr != nil {
			return nil, nil, fmt.Errorf("resolving base branch for worktree tasks: %w", bbErr)
		}
		runID = exec.RunID
		if runID == "" {
			runID = newRunID()
		}
	}

	manifest := NewExecutionManifest(runID, exec.RepoRoot, baseBranch, exec.Feature, plan.Tasks)

	// taskWorktrees holds created worktrees keyed by task ID so they can be
	// reused across validation retries.
	taskWorktrees := make(map[string]*Worktree)
	var taskWorktreesMu sync.Mutex

	// completedStatuses tracks the final status of each task for dependency skip logic.
	completedStatuses := make(map[string]TaskStatus)
	var completedMu sync.Mutex

	var allResults []TaskResult
	var mu sync.Mutex

	for _, wave := range waves {
		// --- Skip dependents whose dependencies are not approved ---
		readyTasks := make([]Task, 0, len(wave))
		for _, t := range wave {
			if shouldSkip(t, completedStatuses) {
				onEvent(WorkerEvent{TaskID: t.ID, Type: "skipped", Message: "dependency did not ship"})
				mu.Lock()
				allResults = append(allResults, TaskResult{
					TaskID:  t.ID,
					Success: false,
					Status:  TaskSkipped,
					Skipped: true,
					Error:   "skipped because dependency did not ship",
				})
				mu.Unlock()
				completedMu.Lock()
				completedStatuses[t.ID] = TaskSkipped
				completedMu.Unlock()
				manifest.RecordResult(allResults[len(allResults)-1])
			} else {
				readyTasks = append(readyTasks, t)
			}
		}

		if len(readyTasks) == 0 {
			continue
		}

		// --- Execute ready tasks in parallel ---
		sem := make(chan struct{}, o.config.MaxConcurrentWorkers)
		var wg sync.WaitGroup

		for _, task := range readyTasks {
			wg.Add(1)
			sem <- struct{}{}

			go func(t Task) {
				defer wg.Done()
				defer func() { <-sem }()
				defer func() {
					if r := recover(); r != nil {
						log.Printf("orchestrator: panic executing task %s: %v", t.ID, r)
						mu.Lock()
						result := TaskResult{
							TaskID:  t.ID,
							Success: false,
							Status:  TaskFailed,
							Error:   fmt.Sprintf("panic: %v", r),
						}
						allResults = append(allResults, result)
						mu.Unlock()
						completedMu.Lock()
						completedStatuses[t.ID] = TaskFailed
						completedMu.Unlock()
						manifest.RecordResult(result)
					}
				}()

				cfg := ResolveAgentConfig(registry, t.Agent)

				// Create worktree once per task (reused across retries).
				var wt *Worktree
				var cwd string
				if t.NeedsWorktree {
					taskWorktreesMu.Lock()
					wt = taskWorktrees[t.ID]
					if wt == nil {
						var wtErr error
						wt, wtErr = o.worktree.Create(runID, t.ID, baseBranch)
						if wtErr != nil {
							result := TaskResult{
								TaskID:  t.ID,
								Success: false,
								Status:  TaskFailed,
								Error:   fmt.Sprintf("worktree creation failed: %v", wtErr),
							}
							onEvent(WorkerEvent{TaskID: t.ID, Type: "error", Message: result.Error})
							mu.Lock()
							allResults = append(allResults, result)
							mu.Unlock()
							completedMu.Lock()
							completedStatuses[t.ID] = TaskFailed
							completedMu.Unlock()
							manifest.RecordResult(result)
							taskWorktreesMu.Unlock()
							return
						}
						taskWorktrees[t.ID] = wt
					}
					taskWorktreesMu.Unlock()
					cwd = wt.Path
				} else {
					cwd = exec.RepoRoot
				}

				// --- Per-task attempt loop ---
				var result TaskResult
				attempt := 1
				workingTask := t
				for attempt <= o.config.MaxValidationRetries {
					manifest.Tasks[workingTask.ID].Status = TaskRunning
					manifest.Tasks[workingTask.ID].Attempts = attempt

					prompt := systemPromptBuilder(workingTask, cfg)
					result = o.ExecuteTask(ctx, workingTask, cfg, cwd, prompt, onEvent)

					if !result.Success {
						result.Status = TaskFailed
						break // bridge error, no point validating
					}

					// Collect artifacts for validation
					var artifacts *ArtifactSnapshot
					if t.NeedsWorktree {
						art, artErr := o.CollectArtifacts(ctx, cwd, workingTask, plan)
						if artErr != nil {
							log.Printf("orchestrator: artifact collection failed for task %s: %v", workingTask.ID, artErr)
						}
						artifacts = art
					}

					// Validate
					vr, valErr := validate(ctx, workingTask, result, artifacts, attempt)
					if valErr != nil {
						result.Status = TaskUnverified
						result.Error = "validation unavailable: " + valErr.Error()
						break
					}
					if vr.Approved {
						result.Status = TaskApproved
						result.Approved = true
						if artifacts != nil {
							result.ChangedFiles = artifacts.ChangedFiles
							result.Verify = artifacts.Verify
						}
						break
					}
					if !vr.ShouldRetry || attempt == o.config.MaxValidationRetries {
						result.Status = TaskEscalated
						result.Error = "validation failed after attempts: " + strings.Join(vr.Issues, "; ")
						onEvent(WorkerEvent{TaskID: workingTask.ID, Type: "escalated", Message: result.Error})
						break
					}

					// Build retry: append feedback to user prompt
					workingTask.Prompt = workingTask.Prompt + "\n\nPrevious attempt issues:\n- " + strings.Join(vr.Issues, "\n- ")
					attempt++
				}

				result.Attempts = attempt
				mu.Lock()
				allResults = append(allResults, result)
				mu.Unlock()
				completedMu.Lock()
				completedStatuses[t.ID] = result.Status
				completedMu.Unlock()
				manifest.RecordResult(result)
			}(task)
		}

		wg.Wait()

		// --- Serial merge of approved worktrees in deterministic task-id order ---
		if needsWorktree {
			approvedTasks := approvedTaskIDsInWave(readyTasks, completedStatuses)
			for _, tid := range approvedTasks {
				taskWorktreesMu.Lock()
				wt := taskWorktrees[tid]
				taskWorktreesMu.Unlock()
				if wt == nil {
					continue
				}
				if err := o.worktree.Merge(wt, baseBranch); err != nil {
					log.Printf("orchestrator: worktree merge failed for task %s, worktree preserved at %s: %v", tid, wt.Path, err)
					// Update the result to reflect merge failure
					completedMu.Lock()
					completedStatuses[tid] = TaskFailed
					completedMu.Unlock()
					for i := range allResults {
						if allResults[i].TaskID == tid {
							allResults[i].Status = TaskFailed
							allResults[i].Success = false
							allResults[i].Error = "merge failed; worktree preserved for recovery"
							manifest.RecordResult(allResults[i])
							onEvent(WorkerEvent{TaskID: tid, Type: "merge_failed", Message: allResults[i].Error})
							break
						}
					}
				} else {
					// Merge succeeded — safe to remove worktree and branch
					if err := o.worktree.Cleanup(wt); err != nil {
						log.Printf("orchestrator: worktree cleanup failed for task %s: %v", tid, err)
					}
					taskWorktreesMu.Lock()
					delete(taskWorktrees, tid)
					taskWorktreesMu.Unlock()
				}
			}
		}
	}

	manifest.FinishedAt = time.Now()
	return manifest, allResults, nil
}

// shouldSkip returns true if any of the task's dependencies is not approved.
func shouldSkip(task Task, statuses map[string]TaskStatus) bool {
	for _, dep := range task.DependsOn {
		if statuses[dep] != TaskApproved {
			return true
		}
	}
	return false
}

// approvedTaskIDsInWave returns the task IDs in the wave that have status
// TaskApproved, sorted deterministically for serial merge ordering.
func approvedTaskIDsInWave(wave []Task, statuses map[string]TaskStatus) []string {
	var ids []string
	for _, t := range wave {
		if statuses[t.ID] == TaskApproved {
			ids = append(ids, t.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// planHasWorktree returns true if any task in any wave has NeedsWorktree set.
func planHasWorktree(waves [][]Task) bool {
	for _, wave := range waves {
		for _, t := range wave {
			if t.NeedsWorktree {
				return true
			}
		}
	}
	return false
}

// boolPtr returns a pointer to v for use in optional bool request fields.
func boolPtr(v bool) *bool {
	return &v
}

// inferCapabilityProfile determines the appropriate capability profile from
// the configured tool list. Used when no explicit profile is set on WorkerConfig.
//
//   - Bash present → execute_safe (governed shell)
//   - Write/Edit present → edit_project (no shell)
//   - Read-only tools present → read_only
//   - No tools → observe
func inferCapabilityProfile(tools []string) security.CapabilityProfile {
	var hasBash, hasWriteEdit, hasRead bool
	for _, t := range tools {
		switch t {
		case "Bash":
			hasBash = true
		case "Write", "Edit":
			hasWriteEdit = true
		case "Read", "Grep", "Glob", "LS":
			hasRead = true
		}
	}
	switch {
	case hasBash:
		return security.ProfileExecuteSafe
	case hasWriteEdit:
		return security.ProfileEditProject
	case hasRead:
		return security.ProfileReadOnly
	default:
		return security.ProfileObserve
	}
}
