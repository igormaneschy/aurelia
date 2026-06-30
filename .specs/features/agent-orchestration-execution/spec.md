# Agent Orchestration — Execution Mode

> **SUPERSEDED — v0.38.0 (2026-06-30):** `internal/orchestrator/` and all `aurelia-plan` execution were **removed**. PI SDK owns agentic execution. See ROADMAP §3, README § "No Planning Mode / No Orchestrator", and ai-memory `decisions/no-planning-mode-or-orchestrator.md`. **Do not implement.**

**Roadmap step:** ~~3 — Close Orchestration Cycle~~ Removed  
**Status:** 🗑️ Historical artifact only

## Problem Statement

When Aurelia emits a structured execution plan, Go should fan that plan out to workers via the PI SDK bridge, validate the results, and ship the change. The pipeline is mostly there — `internal/orchestrator/` parses plans, runs waves in worktrees, calls a quality gate, and merges — but the cycle isn't closed:

- The "base branch" used by the worktree manager is the literal string `"HEAD"` rather than the current branch
- Validation runs once; the promised retry-with-feedback (up to 3 attempts before escalating) is missing
- `CommitChanges`, `CreatePR`, `UpdateTasksStatus` exist but nothing calls them after consolidation
- Orphan worktrees from killed processes are never cleaned on startup
- The task user prompt and the system prompt both carry the task body — workers see it twice
- Feature artifact lookup (`spec.md`/`design.md`) picks the last alphabetical match, which breaks once a project has more than one feature
- The repo root is captured from the daemon process cwd, not from the chat/thread's effective `/cwd`; an approved plan can execute in the wrong repository
- The Telegram thread id is lost during the plan handoff, so orchestration status can be posted to the wrong forum topic
- Same-wave workers can merge concurrently into the same repo root; merge must be serialized even when execution is parallel
- Downstream tasks are not skipped when a dependency fails validation
- The validator reviews the worker's prose, not the actual git diff / changed files / verification output, and validation infrastructure failure currently approves by default
- `CommitChanges` stages the whole repo with `git add -A`, which can accidentally commit unrelated user changes

This spec scopes the work to **close the execution cycle** so an approved conversational plan can ride all the way from "plan JSON detected" to "commit + PR posted on Telegram". Creating the `.specs/features/<feat>/{spec,design,tasks}.md` artifacts is **out of scope** here and belongs to explicit user/agent planning work, not a formal Plan Mode.

## Goals

- [ ] Worktrees branch off the real current branch, not the string `"HEAD"`
- [ ] The execution context resolves the repo from persistent project binding/effective cwd and preserves Telegram `thread_id`
- [ ] Git preflight refuses unsafe execution before spawning workers
- [ ] Worker execution applies `CapabilityProfile` per task and passes security context into the PI bridge
- [ ] Agent Comms peer metadata, if present, is validated before execution and recorded in the manifest
- [ ] Validation supports retry-with-feedback with a hard cap before escalating
- [ ] Validation reviews actual artifacts: git status, diff/stat, changed files, and verify output
- [ ] Same-wave worker execution remains parallel, but merges happen serially and dependents of failed/unverified tasks are skipped
- [ ] Approved consolidation runs commit + (optional) PR + `tasks.md` status update
- [ ] Commit staging is limited to approved orchestration changes; unrelated local changes are never swept into the commit
- [ ] Orphan worktrees from crashed runs are removed on startup
- [ ] Workers receive the task content exactly once
- [ ] Feature artifact lookup is unambiguous (the plan declares which feature it belongs to)
- [ ] Existing wave execution, worktree creation, plan extraction, and consolidate flow keep working unchanged

## Out of Scope

- Aurelia creating spec/design/tasks artifacts automatically
- Routing decisions by `@mention` — Aurelia decides
- Worker-to-worker delegation
- Multi-repo workflows
- Per-worker session resume across tasks
- Web UI / dashboards — feedback stays in Telegram
- Auto-resolving merge conflicts
- Implementing PI `max_turns` support until the bridge/SDK exposes a reliable option

---

## User Stories

### P0: Execution context from chat cwd + thread-safe handoff ⭐

**User Story:** As a user, when I approve a plan from a Telegram topic with a configured persistent `/cwd`, I want execution to happen in that project and all status updates to stay in the same topic.

**Why P0:** The orchestrator is currently constructed with `os.Getwd()` and the handoff interface drops `threadID`. That is a correctness bug before any worker starts.

**Acceptance Criteria:**

1. WHEN `tryExecutePlan` detects an `aurelia-plan` block THEN the pipeline SHALL pass `(chat_id, thread_id, message_id, effective_cwd)` into the execution handoff
2. WHEN `effective_cwd` is empty THEN orchestration SHALL refuse to execute and ask the user to set `/cwd <path>`
3. WHEN `effective_cwd` is not a git repository THEN orchestration SHALL refuse to execute with a clear error
4. WHEN status, errors, consolidation, PR URL, or friendly notes are sent THEN they SHALL target the original `thread_id`
5. WHEN the daemon repo differs from the chat cwd THEN the chat cwd SHALL win for this run

**Independent test:** Configure `/cwd` to a scratch repo different from the Aurelia repo, emit a one-task plan in a forum topic, verify workers run in the scratch repo and every Telegram send uses that thread id.

---

### P0: Git preflight before workers ⭐

**User Story:** As an operator, I want Aurelia to refuse unsafe execution before it spawns workers, rather than discovering the repo is dirty or detached halfway through.

**Why P0:** Autonomous workers plus `git add -A` is risky when the base repo already contains unrelated local changes.

**Acceptance Criteria:**

1. WHEN orchestration starts THEN it SHALL run a preflight against the resolved repo root: git repo exists, branch is not detached, and base working tree is clean
2. WHEN `plan.create_pr=true` THEN preflight SHALL also check whether `gh` is available/authenticated and report upfront if PR creation will be skipped
3. WHEN preflight finds a dirty base tree THEN it SHALL abort before spawning workers and list the first few dirty paths
4. WHEN preflight passes THEN it SHALL return an `ExecutionContext` containing repo root, base branch, chat id, thread id, user id, message id, run id, feature, create_pr, and security defaults
5. WHEN any preflight check fails THEN no worktree SHALL be created

**Independent test:** Put an unstaged file in the base repo, emit a plan, verify orchestration aborts before `WorktreeManager.Create`.

---

### P1: Real current branch for worktrees ⭐

**User Story:** As an operator, I want worker worktrees to be cut from whatever branch I'm actually on so that the merge back lands somewhere meaningful.

**Why P1:** `currentBranch()` returns the string `"HEAD"` (see `internal/orchestrator/execute.go`). `git worktree add -b worker/... <path> HEAD` happens to work but the merge target is wrong — the merge implementation runs from `repoRoot` without checking out the originating branch, so we end up merging into whichever branch happens to be checked out at merge time. This is a correctness bug masked by the fact that most users keep `main` checked out.

**Acceptance Criteria:**

1. WHEN the orchestrator resolves a base branch THEN it SHALL call `git rev-parse --abbrev-ref HEAD` in the repo root
2. WHEN that command fails or returns `HEAD` (detached) THEN orchestration SHALL abort with a clear error rather than proceeding
3. WHEN `WorktreeManager.Merge` runs THEN it SHALL `git checkout <baseBranch>` (if needed) before merging and SHALL refuse to merge into a different branch than the one originally captured
4. WHEN the merge succeeds THEN the working tree SHALL be left on the original base branch
5. WHEN a worktree branch is created THEN it SHALL include a run id namespace (`worker/<runID>/<taskID>`) to avoid collisions between runs

**Independent test:** Switch to a non-main branch, trigger a one-task plan, verify the worktree is created from that branch and the merge lands on it.

---

### P1: Validation retry with feedback ⭐

**User Story:** As a user, when a worker's output fails the quality gate, I want Aurelia to feed the issues back into a retry instead of marking the task failed immediately.

**Why P1:** Spec promises "WHEN validation fails THEN Aurelia SHALL spawn a correction worker with specific feedback" and the edge case "WHEN validation fails 3x THEN escalate". Today (`internal/telegram/orchestration.go`) the validator runs once; failures are recorded and that's it.

**Acceptance Criteria:**

1. WHEN `Validate` returns `Approved=false` with `ShouldRetry=true` THEN the orchestrator SHALL re-spawn the same worker with the original task plus a "Previous attempt issues" block listing `ValidationResult.Issues`
2. WHEN a retry validates successfully THEN the task SHALL count as approved
3. WHEN the validator has rejected the task 3 times THEN orchestration SHALL stop retrying that task and mark it `Success=false` with `Error="validation failed after 3 attempts: <issues>"`
4. WHEN a task is escalated THEN the Telegram status reporter SHALL post a single "needs human review" message rather than spamming each retry
5. WHEN a retry is attempted THEN the worktree SHALL be reused (no new branch per retry)
6. WHEN the validation bridge call fails or returns unparsable output THEN the task SHALL become `unverified` rather than approved by default

**Independent test:** Use a fake bridge whose first validation returns `approved=false, should_retry=true, issues=["missing test"]` and second returns approved. Verify the worker is called twice and the final result is approved.

---

### P1: Validate real artifacts, not worker prose ⭐

**User Story:** As a reviewer, I want the quality gate to inspect the actual diff and verification output so a confident worker summary cannot hide broken or unrelated changes.

**Why P1:** `Validate` currently receives only `TaskResult.Content`. That is not enough to decide whether to merge and commit.

**Acceptance Criteria:**

1. WHEN a worker attempt finishes in a worktree THEN orchestration SHALL collect `git status --porcelain`, `git diff --stat`, and `git diff` for that worktree
2. WHEN `task.verify` is present THEN orchestration SHALL run that command in the worktree with a timeout and capture exit code/stdout/stderr
3. WHEN `task.verify` is absent THEN orchestration MAY run a plan-level default verify command if provided; otherwise validation SHALL explicitly note "no verify command"
4. WHEN validation runs THEN the validator SHALL receive task prompt, worker result, changed files, diff/stat, status, and verify output
5. WHEN the diff is empty for a write task THEN validation SHALL reject or mark unverified unless the task explicitly declares read-only behavior
6. WHEN validation rejects with `should_retry=true` THEN retry feedback SHALL include concrete issues plus relevant verify failure snippets

**Independent test:** Fake worker reports success without changing files; validation receives empty diff and rejects the task.

---

### P1: Merge serially and skip failed dependents ⭐

**User Story:** As an operator, I want parallel workers to stay parallel, but merges into the base branch to happen one at a time and downstream tasks to stop when their prerequisites did not ship.

**Why P1:** Parallel merge into one repo root is unsafe, and running a dependent task after its prerequisite failed produces misleading output.

**Acceptance Criteria:**

1. WHEN a wave has multiple ready tasks THEN worker execution MAY run in parallel up to `MaxConcurrentWorkers`
2. WHEN tasks in a wave finish validation THEN approved worktrees SHALL be merged serially in deterministic task-id order
3. WHEN a task fails, is unverified, or is escalated THEN every task depending on it SHALL be marked `skipped` before its wave starts
4. WHEN a task is skipped THEN no bridge request SHALL be sent for it and Telegram SHALL show a concise skipped reason
5. WHEN a merge conflict occurs THEN orchestration SHALL stop the run, keep the worktree/branch for manual resolution, and mark not-yet-run dependents as skipped

**Independent test:** T1 fails validation, T2 depends on T1. Verify T2 is never executed and final results include `Skipped=true`.

---

### P1: Commit + PR + tasks.md update ⭐

**User Story:** As a user, after Aurelia consolidates the worker results, I want a real commit (Conventional Commits) and an optional PR, and I want the source `tasks.md` checkboxes to reflect what shipped.

**Why P1:** `git.go` (`CommitChanges`, `CreatePR`, `IsGHAvailable`) and `tasks_status.go` (`UpdateTasksStatus`) are implemented and tested but **never called** from the pipeline. Five P1 acceptance criteria from the old spec died in `executeApprovedPlan`.

**Acceptance Criteria:**

1. WHEN every wave has run and at least one task is approved THEN the orchestrator SHALL call `UpdateTasksStatus` against the originating `tasks.md` to flip its checkboxes
2. WHEN at least one task is approved AND there are staged changes THEN the orchestrator SHALL call `CommitChanges` with a Conventional Commit message derived from the plan (`feat(<scope>): <description>`)
3. WHEN there are no staged changes after merge THEN orchestration SHALL skip the commit and log it, not error
4. WHEN the plan or user request flags "create PR" AND `IsGHAvailable()` THEN `CreatePR` SHALL be called and the URL posted to Telegram
5. WHEN `IsGHAvailable()` returns false AND a PR was requested THEN orchestration SHALL post a friendly message ("commit landed locally, install/auth `gh` to publish a PR") and not error
6. WHEN any of these steps fail THEN orchestration SHALL post the error and stop — it SHALL NOT attempt to rewrite history or push
7. WHEN committing THEN staging SHALL be limited to files changed by approved worktrees plus the updated `tasks.md`; unrelated base-repo changes SHALL remain unstaged
8. WHEN approved changed files cannot be determined safely THEN orchestration SHALL skip commit and ask for human review rather than running `git add -A`

**Independent test:** Run a one-task plan in a scratch repo, verify a commit lands on the base branch with the expected message and `tasks.md` checkboxes flip.

---

### P1: Orphan worktree cleanup on startup ⭐

**User Story:** As an operator, if the daemon dies during execution, the next startup should clean up stale `.worktrees/worker-*` so future runs don't collide.

**Why P1:** Edge Case "WHEN bridge dies during worker THEN Go follows recovery, cleans worktrees" is asserted in the old spec. `WorktreeManager.CleanupAll()` exists but is never called.

**Acceptance Criteria:**

1. WHEN the Orchestrator is constructed THEN it SHALL call `CleanupAll()` once on the configured `RepoRoot` after construction (best-effort, errors logged not fatal)
2. WHEN `CleanupAll()` removes a worktree THEN it SHALL also delete the associated `worker/<slug>` branch if it exists
3. WHEN startup cleanup runs THEN it SHALL log the count of worktrees removed
4. WHEN no orphan worktrees exist THEN startup cleanup SHALL be a no-op

**Independent test:** Manually create `.worktrees/worker-fake/` and a `worker/fake` branch, restart the daemon, verify both are gone.

---

### P1: Single source of truth for task prompt ⭐

**User Story:** As an operator reviewing worker logs, I want each worker to see its task instructions exactly once, not duplicated across system prompt and user prompt.

**Why P1:** `BuildWorkerPrompt` (`internal/orchestrator/prompt.go`) layers CLAUDE.md + AGENTS.md + spec + design + **task + siblings** into the system prompt. Meanwhile `ExecuteTask` sends `task.Prompt` (which is the same task body) as the user prompt. Wastes tokens and confuses log diffing.

**Acceptance Criteria:**

1. WHEN `BuildWorkerPrompt` runs THEN it SHALL NOT include the task body — only base agent prompt, CLAUDE.md, AGENTS.md, spec, design, and sibling **summaries** (just IDs and descriptions, not full prompts)
2. WHEN `ExecuteTask` builds the bridge request THEN it SHALL send `task.Prompt` as the user prompt and the layered prompt as the system prompt — no duplication
3. WHEN a worker reads its system prompt THEN it SHALL see the project context but not its own task body twice
4. WHEN unit tests run THEN `TestBuildWorkerPrompt` SHALL verify the task body is not embedded

**Independent test:** Run `TestBuildWorkerPrompt` and assert the rendered system prompt does not contain `task.Prompt`.

---

### P1: Feature artifact resolution ⭐

**User Story:** As Aurelia, when I execute a plan, I need to know which feature directory under `.specs/features/` to read so workers receive the right `spec.md` and `design.md`.

**Why P1:** `findFeatureDoc` (`internal/telegram/orchestration.go`) globs `.specs/features/*/<filename>` and returns the **last alphabetical match**. With two features that breaks immediately.

**Acceptance Criteria:**

1. WHEN the Aurelia orchestrator emits an `aurelia-plan` block THEN the JSON SHALL include a top-level `feature` field (e.g., `"feature":"agent-orchestration-execution"`)
2. WHEN no `feature` field is present THEN orchestration SHALL log a warning and fall back to "no feature context" (workers get CLAUDE.md/AGENTS.md only, no spec/design)
3. WHEN a `feature` field is present THEN orchestration SHALL read `.specs/features/<feature>/spec.md` and `.specs/features/<feature>/design.md` and pass those exact files
4. WHEN the named feature directory does not exist THEN orchestration SHALL log a warning and proceed with no feature context (do not crash)

**Independent test:** Plans with two different `feature` values produce different spec/design content in worker prompts.

---

### P2: Execution manifest and resume-friendly run metadata

**User Story:** As an operator debugging a long run, I want a concise manifest of what happened: repo, branch, tasks, attempts, diffs, verify commands, status and cost.

**Acceptance Criteria:**

1. WHEN orchestration starts THEN it SHALL create an in-memory `ExecutionManifest`
2. WHEN each attempt finishes THEN manifest SHALL record status, attempts, changed files, verify output summary, cost, duration, capability profile, security decisions summary, and peer message counts
3. WHEN consolidation runs THEN the PR body and final Telegram summary SHALL be derived from the manifest
4. WHEN future persistence is added THEN the manifest structure SHALL be serializable without changing public behavior

---

### P2: Per-worker max_turns (blocked on PI bridge support)

**User Story:** As an operator I want each agent to honor its own `max_turns` so a verbose worker can't burn through the whole budget.

**Acceptance Criteria:**

1. WHEN the PI bridge exposes a reliable max-turns option THEN an agent's `.md` `max_turns` SHALL be passed to the bridge request
2. UNTIL that support exists THEN `max_turns` SHALL remain documented as unsupported and SHALL NOT be presented as enforced
3. WHEN support is added THEN tests SHALL prove that a worker exceeding max_turns is marked failed with `Error="max_turns exceeded"`

---

### P2: Cost tracking per worker

**User Story:** As a user I want to see how much each worker cost when the run completes.

**Acceptance Criteria:**

1. WHEN a worker completes THEN `TaskResult.CostUSD` SHALL be populated from the bridge `result` event
2. WHEN the status reporter marks a worker done THEN it SHALL append the cost and duration to the message
3. WHEN consolidation runs THEN the total cost across all workers SHALL be included in the final summary

---

### P3: Budget enforcement per worker

**Acceptance Criteria:**

1. WHEN `OrchestratorConfig.MaxBudgetUSDPerWorker > 0` AND a worker exceeds it THEN orchestration SHALL cancel the worker context and mark the task failed
2. WHEN a worker is cancelled for budget THEN the status reporter SHALL emit "budget exceeded: $X.XX of $Y.YY"

---

## Edge Cases

- WHEN the bridge dies mid-wave THEN existing recovery + cleanup logic SHALL handle it (covered by `pi-resilience` / `bridge-recovery`)
- WHEN a wave has zero tasks (degenerate plan) THEN orchestration SHALL skip the wave without error
- WHEN `git merge --no-ff` conflicts THEN orchestration SHALL leave the worktree intact, post the conflict to Telegram, and stop the run — it SHALL NOT auto-resolve or auto-abort
- WHEN a plan has a circular dependency THEN `ExecutionOrder()` SHALL return an error and orchestration SHALL post the error and stop before spawning any workers
- WHEN a dependency fails validation THEN dependent tasks SHALL be skipped without bridge calls
- WHEN validation infrastructure fails THEN the task SHALL be `unverified`, not auto-approved
- WHEN the base working tree becomes dirty between preflight and merge THEN merge/commit SHALL stop and report the dirty paths
- WHEN the rate limit hits the status reporter THEN updates SHALL be dropped silently and execution continues
- WHEN the plan JSON references an agent name that isn't in the registry THEN `ResolveAgentConfig` SHALL fall back to the default worker (existing behavior)

---

## Success Criteria

- [ ] All P1 ACs pass with deterministic tests against a fake bridge
- [ ] `go build ./... && go vet ./... && go test ./...` clean
- [ ] An end-to-end smoke (manual): take a real one-task plan in a scratch repo and verify worktree, validate, merge, commit, `tasks.md` update — all on a branch that isn't `main`
- [ ] No regression in conversational (non-plan) turns — they continue to flow through `pipeline.Service` unchanged
