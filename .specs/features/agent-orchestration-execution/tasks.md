# Agent Orchestration — Execution Mode — Tasks

**Design:** `.specs/features/agent-orchestration-execution/design.md`
**Roadmap step:** 3 — Close Orchestration Cycle
**Status:** ✅ Implementado em v0.16.0 (2026-05-24)
**Depende de:** User Isolation runtime hardening (✅) + Project Binding (✅ done) + Operational Observability

> Foundational components from the prior iteration are already implemented: plan parsing, wave ordering, basic worktrees, worker execution, validator, prompt builders, status reporter, `UpdateTasksStatus`, and git/PR helpers. This task list closes correctness and safety gaps before autonomous execution can commit.

---

## Execution Plan

### Phase 0: Safe Handoff + Preflight

Execution must use the chat/thread's effective `/cwd`, preserve Telegram `thread_id`, and refuse unsafe git state before workers start.

```
T0 → T1
```

### Phase 1: Core Orchestrator Model

Plan schema, worktree branch naming, artifact collection, and fail-closed validation.

```
T2 ─┐
T3 ─┼→ T4 → T5
T6 ─┘
```

### Phase 2: Wave Engine + Telegram Wiring

Parallel execution remains, but validation is artifact-based, merges are serialized, dependents are skipped, and status stays in the original topic.

```
T5 → T7 → T8
```

### Phase 3: Delivery

Update tasks, commit only approved files, optionally open PR, then consolidate.

```
T8 → T9 → T10
```

### Phase 4: Release Validation

```
T10 → T11 → T12
```

---

## Task Breakdown

### T0: Thread-safe `ExecutionContext` handoff

**What:** Change the pipeline/output handoff so orchestration receives `chatID`, `threadID`, `messageID`, effective cwd, and the parsed plan. Add `orchestrator.ExecutionContext`.
**Where:** `internal/pipeline/service.go`, `internal/pipeline/pipeline.go`, `internal/telegram/pipeline.go`, `internal/telegram/orchestration.go`, `internal/orchestrator/orchestrator.go`
**Depends on:** None
**Reuses:** `Service.effectiveCwd`, existing `Output.ExecuteApprovedPlan`

**Done when:**
- [x] `pipeline.Output.ExecuteApprovedPlan` signature includes `threadID` and `cwd`
- [x] `tryExecutePlan` resolves effective cwd and refuses empty cwd before handoff
- [x] Telegram output passes the original thread id through to `executeApprovedPlan`
- [x] `ExecutionContext` includes `RunID`, `RepoRoot`, `BaseBranch`, `ChatID`, `ThreadID`, `MessageID`, `Feature`, `CreatePR`, `StartedAt`
- [x] All orchestration sends touched in this slice use `ThreadID`
- [x] Tests: `TestTryExecutePlan_PassesThreadAndCWD`, `TestTryExecutePlan_RequiresCWD`

**Verify:**
```bash
go test ./internal/pipeline/... ./internal/telegram/... -run "TestTryExecutePlan|TestExecuteApprovedPlan.*Thread" -v
```

---

### T1: Git preflight before worker spawn

**What:** Add `PreflightExecution` to prove repo safety before workers. It checks git repo, base branch, clean base tree, and optional `gh` availability for PR requests.
**Where:** `internal/orchestrator/preflight.go`, `internal/orchestrator/worktree.go`, `internal/telegram/orchestration.go`
**Depends on:** T0
**Reuses:** `git rev-parse`, `IsGHAvailable`

**Done when:**
- [x] `WorktreeManager.ResolveBaseBranch() (string, error)` implemented with typed `ErrDetachedHEAD`
- [x] `PreflightExecution(ctx, repoRoot, createPR)` rejects non-git, detached HEAD, and dirty base tree
- [x] Dirty preflight error includes first few dirty paths in logs/internal errors; Telegram receives sanitized user-safe text
- [x] `executeApprovedPlan` calls preflight before `EnsureClaudeMd`, `EnsureAgentsMd`, or any worktree create
- [x] `create_pr=true` records `GHAvailable`, but missing `gh` does not block the run
- [x] Tests: `TestPreflightExecution_RejectsDirtyBase`, `TestPreflightExecution_RejectsDetachedHEAD`, `TestPreflightExecution_GHMissingNonFatal`

**Additional slice-1 hardening delivered:**
- [x] Run-scoped orchestrator uses the handoff cwd without mutating the shared orchestrator
- [x] Worker bridge requests use isolated non-persistent synthetic session scopes
- [x] `Validate` and `Consolidate` pass the run repo cwd to the Bridge
- [x] `NeedsWorktree` tasks fail closed if worktree creation is unavailable or fails

**Verify:**
```bash
go test ./internal/orchestrator/... -run "TestPreflight|TestResolveBaseBranch" -v
```

---

### T2: Worktree run namespace + branch-safe merge + startup cleanup count

**What:** Add run-id namespacing to worktree branches/paths, make merge checkout the captured base branch and refuse dirty base trees, and make orphan cleanup return a count.
**Where:** `internal/orchestrator/worktree.go`, `internal/orchestrator/orchestrator.go`
**Depends on:** T1
**Reuses:** Existing worktree helpers

**Done when:**
- [x] `Create(runID, taskID, baseBranch)` creates branch `worker/<runID>/<taskSlug>`
- [x] Worktree path is `.worktrees/worker-<runID>-<taskSlug>`
- [x] `Merge` checks out `baseBranch` and refuses dirty base tree
- [x] `CleanupAll() (int, error)` removes `.worktrees/worker-*` and associated `worker/*` branches best-effort
- [x] `NewOrchestrator` calls cleanup once and logs count when > 0
- [x] Merge failures mark the task failed, emit a sanitized `merge_failed` event, and preserve the worktree/branch for manual recovery
- [x] Base-repo git mutations (`Merge`, `Cleanup`, `CleanupAll`) are serialized by normalized repo root across `WorktreeManager` instances
- [x] `runID` is strictly validated before git commands and worktree paths are containment-checked under `.worktrees/`
- [x] Tests: `TestWorktreeCreate_UsesRunNamespace`, `TestMerge_ChecksOutBaseBranch`, `TestMerge_RefusesOnDirtyTree`, `TestCleanupAll_ReturnsCount`, `TestExecutePlan_MergeFailure_PreservesWorktree`, `TestMerge_ConflictAbortsCleanly`, `TestWorktreeManager_CrossInstanceSerialization`

**Verify:**
```bash
go test ./internal/orchestrator/... -run "TestWorktree|TestMerge|TestNewOrchestrator_Cleans" -v
```

---

### T3: Plan schema, verify fields, and worker prompt cleanup

**What:** Extend plan/task JSON and remove duplicate task body from worker system prompt.
**Where:** `internal/orchestrator/plan.go`, `internal/orchestrator/prompt.go`
**Depends on:** None
**Reuses:** Existing prompt builders and extract tests

**Done when:**
- [x] `Plan` has `Feature`, `CreatePR`, `Verify`
- [x] `Task` has `Verify`
- [x] `BuildOrchestratorPrompt` and `BuildExecutionPrompt` show the new schema
- [x] `ParsePlan` remains backward compatible with old plans
- [x] `BuildWorkerPrompt` excludes `task.Prompt` and sibling prompts; it includes only task id/description and sibling summaries
- [x] Tests: `TestBuildWorkerPrompt_DoesNotEmbedTaskBody`, `TestExtractPlan_WithFeatureCreatePRVerify`

**Verify:**
```bash
go test ./internal/orchestrator/... -run "TestBuildWorkerPrompt|TestExtractPlan|TestBuildExecutionPrompt" -v
```

---

### T4: Artifact collection and verify command execution

**What:** Collect real worktree artifacts after each worker attempt: changed files, git status, diffstat, truncated diff, and verify command output.
**Where:** `internal/orchestrator/artifacts.go`
**Depends on:** T3
**Reuses:** `os/exec`, context timeouts

**Done when:**
- [x] `ArtifactSnapshot` and `VerifyResult` defined
- [x] `CollectArtifacts(ctx, cwd, task, plan)` captures `git status --porcelain`, `git diff --stat`, `git diff`
- [x] `task.Verify` overrides `plan.Verify`; empty verify is allowed but recorded
- [x] Verify command runs in the worktree with `OrchestratorConfig.VerifyTimeout` defaulting to 2m
- [x] Diff is truncated with an explicit truncation marker; changed file list and diffstat are preserved
- [x] Tests: `TestCollectArtifacts_CapturesDiff`, `TestCollectArtifacts_RunsTaskVerify`, `TestCollectArtifacts_VerifyTimeout`

**Verify:**
```bash
go test ./internal/orchestrator/... -run TestCollectArtifacts -v
```

---

### T5: Fail-closed validation with artifact-aware prompt

**What:** Upgrade validation to review artifacts and return errors instead of approving by default when validation infrastructure fails.
**Where:** `internal/orchestrator/validate.go`, `internal/orchestrator/prompt.go`
**Depends on:** T4
**Reuses:** Existing `ValidationResult` parser

**Done when:**
- [x] `Validator` signature includes `ArtifactSnapshot`
- [x] `buildValidationUserPrompt` includes changed files, status, diffstat, truncated diff, and verify result
- [x] Bridge/parse failure returns error to caller, not `Approved=true`
- [x] Empty diff for a write task is treated as a concrete issue (warning injected in prompt)
- [x] Tests: `TestValidate_ReceivesDiffAndVerifyOutput`, `TestValidateBridgeFailure_IsNotApproved`, `TestValidate_EmptyDiffRejectedForWriteTask`

**Verify:**
```bash
go test ./internal/orchestrator/... -run TestValidate -v
```

---

### T6: Manifest and task status model

**What:** Introduce `ExecutionManifest`, `TaskRecord`, `TaskStatus`, and richer `TaskResult` fields.
**Where:** `internal/orchestrator/manifest.go`, `internal/orchestrator/plan.go`
**Depends on:** None
**Reuses:** Existing `TaskResult` as compatibility surface

**Done when:**
- [x] `TaskStatus` includes `pending`, `running`, `approved`, `failed`, `skipped`, `unverified`, `escalated`
- [x] `TaskResult` has `Status`, `Approved`, `Skipped`, `Attempts`, `ChangedFiles`, `Verify`
- [x] `ExecutionManifest` records repo, branch, feature, run id, started/finished, task records
- [x] Helpers expose `ApprovedResults`, `ApprovedChangedFiles`, and total cost/duration
- [x] Existing tests updated to assert status instead of only `Success`

**Verify:**
```bash
go test ./internal/orchestrator/... -run "Test.*Manifest|TestExecuteTask" -v
```

---

### T7: ExecutePlan retry loop, dependency skip, and serial merge

**What:** Refactor `ExecutePlan` to receive `ExecutionContext`, validate inside the attempt loop, reuse worktrees across retries, skip failed dependents, and merge approved worktrees serially after each wave.
**Where:** `internal/orchestrator/execute.go`
**Depends on:** T2, T4, T5, T6
**Reuses:** Existing wave sorting and `ExecuteTask`

**Done when:**
- [x] `ExecutePlan(ctx, exec, plan, registry, systemPromptBuilder, validate, onEvent)` returns `(*ExecutionManifest, []TaskResult, error)`
- [x] Each task creates at most one worktree across retries
- [x] Retry feedback is appended to the user prompt
- [x] Validation errors mark task `unverified`
- [x] Three validation failures mark task `escalated`
- [x] Dependents of failed/unverified/escalated tasks are marked `skipped` before bridge execution
- [x] Approved worktrees merge serially in deterministic task-id order after the wave completes
- [x] Merge conflict stops the run, keeps the conflicted worktree/branch, and skips not-yet-run dependents
- [x] Tests: `TestExecutePlan_RetriesOnValidationFailure`, `TestExecutePlan_EscalatesAfter3Failures`, `TestExecutePlan_ReusesWorktreeAcrossRetries`, `TestExecutePlan_SkipsDependentsOfFailedTask`, `TestExecutePlan_MergesWaveSerially`

**Verify:**
```bash
go test ./internal/orchestrator/... -run TestExecutePlan -v
```

---

### T8: Telegram orchestration wiring and status states

**What:** Wire the new execution context, validator closure, feature doc lookup, and status states into `executeApprovedPlan`.
**Where:** `internal/telegram/orchestration.go`, `internal/telegram/worker_status.go`
**Depends on:** T0, T1, T7
**Reuses:** `WorkerStatusReporter`, `loadFeatureDocs`

**Done when:**
- [x] `executeApprovedPlan` accepts thread id and cwd
- [x] It builds `ExecutionContext` from preflight result and plan (BaseBranch captured)
- [x] `loadFeatureDocs(repoRoot, plan.Feature)` replaces alphabetical glob lookup
- [x] Validator closure captures spec/design and artifact snapshot
- [x] Old post-execution validate-once loop removed
- [x] Status reporter handles `skipped`, `unverified`, and `escalated`
- [x] All status/error/final messages include the original thread id
- [x] Tests: `TestLoadFeatureDocs_UsesPlanFeature`, `TestLoadFeatureDocs_FallsBackWhenFeatureEmpty`, `TestLoadFeatureDocs_MissingDir`

**Verify:**
```bash
go test ./internal/telegram/... -run "TestLoadFeatureDocs|TestExecuteApprovedPlan" -v
```

---

### T9: Safe tasks update, commit, and PR delivery

**What:** Update `tasks.md` based on approved status, commit only approved changed files, and build PR body from the manifest.
**Where:** `internal/orchestrator/tasks_status.go`, `internal/orchestrator/git.go`, `internal/telegram/orchestration.go`
**Depends on:** T6, T8
**Reuses:** `UpdateTasksStatus`, `CreatePR`, `IsGHAvailable`

**Done when:**
- [x] `UpdateTasksStatus` marks checkboxes only for `TaskApproved`, not generic `Success`
- [x] `CommitChanges(repoRoot, files, message)` stages only provided files
- [x] `CommitChanges` returns `ErrNothingToCommit` for empty/no-op file list
- [x] Unrelated dirty files remain unstaged
- [x] `tasks.md` path is included in staged files only when it was successfully updated
- [x] PR body includes manifest summary, approved/skipped/unverified tasks, changed files, and verify summaries
- [x] Missing `gh` with `create_pr=true` posts friendly note, not error
- [x] Tests: `TestUpdateTasksStatus_OnlyApproved`, `TestCommitChanges_StagesOnlyProvidedFiles`, `TestCommitChanges_ErrNothingToCommit`, `TestCommitChanges_NoDirtyFilesStaged`

**Verify:**
```bash
go test ./internal/orchestrator/... ./internal/telegram/... -run "TestUpdateTasksStatus|TestCommitChanges|TestExecuteApprovedPlan" -v
```

---

### T10: Integration smoke test in scratch repo

**What:** Add a deterministic integration test or manual smoke script notes for a one-task plan on a non-main branch.
**Where:** `e2e/` or `internal/telegram/orchestration_test.go`
**Depends on:** T9
**Reuses:** Fake bridge where possible; real git repo in `t.TempDir()`

**Done when:**
- [x] One-task write plan runs in a temp git repo on a non-main branch
- [x] Worktree is created from that branch
- [x] Verify command runs
- [x] Approved diff merges serially
- [x] Commit contains only approved files
- [x] `tasks.md` checkbox flips
- [x] Thread id is preserved in fake Telegram sender

**Verify:**
```bash
go test ./internal/orchestrator/... -run "TestOrchestrationCycle_Smoke" -v
```

---

### T11: Version and changelog proposal

**What:** Per project policy, propose version bump and changelog entry before committing.
**Where:** `internal/version/version.go`, `CHANGELOG.md`
**Depends on:** T10

**Done when:**
- [x] Proposed bump type and changelog text posted to Igor
- [x] After approval: version constant and changelog updated

---

### T12: Full validation pass

**What:** Run the standard repo validation.
**Where:** project root
**Depends on:** T11

**Done when:**
- [x] `go build ./...` clean
- [x] `go vet ./...` clean
- [x] `go test ./... -v` all green
- [x] Integration smoke test passes (temp repo, non-main branch, worktree → verify → serial merge → commit → tasks.md update)

**Verify:**
```bash
go build ./... && go vet ./... && go test ./... -v
```

---

## Deferred / Blocked

- **Per-worker `max_turns`** remains blocked until PI bridge support exists. Current `internal/bridge/protocol.go` explicitly notes that the PI SDK does not expose a max-turns analogue. Do not claim enforcement until the bridge supports it.
- **Persistent execution resume** is not part of this spec. `ExecutionManifest` is intentionally serializable so persistence can be added later.
- **Automatic merge conflict resolution** is out of scope; conflicts stop the run and keep branches for manual review.

---

## Already-Done (carried forward from prior iteration)

- Agent struct fields (`DisallowedTools`, `MaxTurns`; note `MaxTurns` is not enforced by PI yet)
- `BuildSDKAgents` mapping
- `Plan`, `Task`, `TaskResult`, `WorkerEvent`, `WorkerConfig` types
- `Plan.ExecutionOrder` topological sort
- `WorktreeManager.Create/Merge/Cleanup/CleanupAll` baseline
- `DefaultWorkerConfig` + `ResolveAgentConfig`
- `Orchestrator` struct + `BridgeExecutor` interface
- `ExtractPlan` + `StripPlanBlock`
- `ExecutePlan` + `ExecuteTask` baseline
- `Validate` + JSON heuristic fallback baseline
- Prompt builders baseline
- `WorkerStatusReporter` baseline
- `EnsureClaudeMd`, `EnsureAgentsMd`
- `UpdateTasksStatus`
- `CommitChanges`, `CreatePR`, `IsGHAvailable`
- Wiring from `pipeline.tryExecutePlan` to `BotController.executeApprovedPlan`
