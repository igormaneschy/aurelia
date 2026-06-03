# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## v0.23.0 - 2026-06-03

### Security
- **Orchestrator verify allowlist** — plan/task `verify` commands no longer
  execute through free-form `sh -c`. Verification is parsed into an argv and
  accepted only for explicit build/test/typecheck commands (`go test/vet/build`,
  npm/pnpm/yarn test/typecheck/build/lint, `npx tsc`, `pytest`, `rspec`). Shell
  metacharacters are rejected before execution. This closes a repeated policy
  bypass class where execution paths outside the Bridge skipped Bridge Bash
  guardrails.
- **Read-like Bridge tools respect cwd boundary** — `Grep`, `Glob`, and `LS` now
  share the same `cwd` containment check as `Read`, preventing directory/content
  enumeration outside the bound project unless explicitly allowlisted.
- **Checkpoint redaction helper** — partial assistant checkpoints now use a
  redaction-before-truncation helper before persistence, preventing recurrence of
  the previously fixed secret-slicing class.

### Fixed
- **Runlog/tool state user isolation** — runlog state and live tool monitoring are
  keyed by `chatID:threadID:userID`, preventing users in the same group/topic from
  overwriting each other's run state, tool summaries, and `/status` diagnostics.
- **Bridge side-channel event routing** — `steer` and `follow-up` no longer replace
  the active query `request_id`, preventing live events from being emitted to a
  completed side-channel request and silently dropped.
- **Loop detector race/deadlock** — loop warning state and recent-tool snapshots
  are read under one mutex contract; race tests cover the pipeline/Telegram hot
  paths.
- **Silent error handling** — `/forgetme` now fails closed if cron job cleanup
  fails, `/debug run` surfaces `ListEvents` errors, and circuit-breaker open
  notifications are actually sent instead of computed and discarded.

### Changed
- **Validation guardrails** — `make check` now runs targeted race tests
  (`go test -race ./internal/pipeline ./internal/telegram`), CI runs the same race
  gate, and the PR template calls it out for async/session/pipeline/Telegram
  changes. `runLogKey` now requires an explicit `userID`, making user scoping a
  compile-time requirement instead of a convention.

## v0.22.0 - 2026-06-02

### Added
- **Per-agent tool budget** — agents can declare `tool_budget:
  <int>` in frontmatter to override tool-call warning/critical
  thresholds (default: 20/50). Warning = budget, critical = budget x 2.5.
- **Live tool state in /status** — active runs expose tool call
  count, loop detection status, and last 5 distinct tool names.
- **Loop detection events** — `loop_detected` is now recorded as
  a warn-level event in the run timeline with pattern type
  (consecutive_repeat, ping_pong, tool_spiral).

### Changed
- **Contextual loop steer** — loop warnings to the model now
  include the last 3 distinct tool names from the ring buffer
  (e.g. "Read -> Grep -> Read"), replacing the previous generic message.

## v0.21.1 - 2026-06-02

### Changed
- **Tool monitoring improvements** — `loopDetector.ResetForNewTurn()` resets
  warned flag and call history on `turn_start`, preventing stuck warnings and
  cross-turn false positives in multi-turn PI sessions.
- **Tool steer messages** now include elapsed time since run start
  (e.g. "Você já usou ferramentas 20 vezes (Bash) em 42s"), aiding
  diagnosis of tool-call explosions.
- **detectToolSpiral** uses case-insensitive prefix matching instead of exact
  equality, catching "Read", "ReadFile", "reading" and similar variants.

### Added
- **nudgeBuffer.AddToolEvent** — tool call summary from each successful turn
  is now attached to the nudge transcript, enriching nudge review context.

## v0.21.0 - 2026-06-02

### Added
- **Runlog Message Bridge** — `RunRecord` and `RunUpdate` now carry
  `InboundMessageID` and `OutboundMessageID`. Idempotent SQLite migration
  adds the columns on startup (zero default, COALESCE for legacy rows);
  the `idx_run_journal_session_outbound` index makes
  `GetLastOutboundMessage(session_file)` O(log n). Pipeline sets the
  inbound id at `Start` and best-effort updates the outbound id after
  the final `SendReply`. Content privacy preserved — only message IDs
  are persisted, never message text.
- **Mode Profiles** — per-user behavioural overlay system. New
  `Profile.ActiveMode` field (with PT/EN aliases: developer/dev/
  desenvolvedor, researcher/research/pesquisa/pesquisador, general/
  geral/auto). `mode_<name>.md` files in
  `users/<id>/personas/` are appended LAST in `BuildPromptForUser`
  (after IDENTITY/SOUL/USER/owner/project). Missing or invalid mode
  files are silently ignored. New `/mode` slash + natural-language
  command (registered via `bot.Handle` so Telegram's `@botname`
  suffix in groups is stripped). Active mode shown in `/agents` output
  with `(● ativo)` marker. Memory checkpoints get a `[mode:xxx]` tag
  in the note.
- **Profile enrichment** — new `Profile.Timezone` (IANA, empty = UTC)
  and `Profile.DefaultCWD` fields. Both decode from old JSON
  profiles as empty strings (backward-compat). `NormalizeTimezone`
  validates the IANA name and falls back to UTC on any error. Cron
  parser (`cronFastParse` + `parseCronWithLLM`) now uses
  user-local `now` and the timezone-aware prompt. Recurring cron
  jobs persist the timezone; the scheduler uses
  `computeNextRunInLocation` and normalizes the result to UTC so
  `ListDueJobs` comparisons remain correct. `DefaultCWD` is a final
  fallback in the CWD chain (agent > binding > session >
  `DefaultCWD`), applied only in private chats with no topic — never
  in groups or topics. Onboarding collects timezone (4 presets +
  manual IANA) after the profile step.

### Changed
- **Slash command dispatch is mandatory for new commands** — added
  `stripBotMention` defense in `MatchCommand` so any future slash
  command accidentally routed through text matching still works in
  groups/topics where Telegram appends `@<bot_username>`. The right
  fix remains `bc.bot.Handle("/cmd", handler)` so telebot strips the
  suffix and populates `c.Message().Payload`.
- **Cron time semantics use user-local time everywhere** — `now`
  passed through `cronFastParse` and the LLM prompt builder
  preserves the user's timezone, including in the rendered example
  (`%%s` placeholder ambiguity removed by substituting the actual
  offset directly).

### Fixed
- **`/mode` and `/agents` thread routing** — both now use
  `SendTextWithThread` so the reply stays in the forum topic where
  the command was sent. `/agents` was consolidated with the
  natural-language path (`lista agents` / `quais agents`) to use the
  same markdown + mode section output.
- **Stale-cache validation in `/model <name>`** — `cmdSetModel`
  now always force-refreshes from PI before validating, so a model
  added to PI 30 seconds ago can be set immediately. Cache is also
  cleared after the provider env refresh so the next listing
  reflects the new API key configuration.
- **`/model refresh` exposed as natural language** — `atualiza
  modelos`, `atualizar modelos`, `refresh modelos`, `recarregar
  modelos`, `atualiza a lista` all route to `cmdRefreshModels`.
- **Dead wrapper code** — removed the unused
  `BotController.buildSystemPrompt` wrapper in `telegram/pipeline.go`
  (it hardcoded `isPrivateChat: false`, which would have skipped the
  `DefaultCWD` fallback in private chats if ever called). The
  underlying `pipeline.Service.BuildSystemPrompt` is already
  thoroughly tested in `prompt_builder_test.go`.
- **Cron `%%s` in LLM prompt example** — was producing the literal
  `%s` in the rendered prompt, which could confuse the model into
  treating it as a format placeholder. Now substituted with the actual
  `offsetText` so the example is concrete
  (`2026-03-27T15:00:00-03:00`).
- **Code review remediation (PR #9)**:
  - `SendNudge` now wraps both HTML and plain-text retry errors
    instead of silently discarding the retry failure context.
  - `isMissingReplyTargetError` documented with migration path to
    `telebot.ErrNotFoundToReply` sentinel.
  - Dead legacy `processRun` (44 lines, `nolint:unused`) removed;
    active path is `processRunWithCancel`. Goroutine panic log
    messages corrected to reference the actual function name.
  - `GetLastOutboundMessage` error path now tested: when the run
    log returns an error, the nudge receipt is sent without reply
    targeting (no crash, no wrong reply).
  - Double-space formatting artifacts fixed in 3 test files.

### Migration Safety
- **Profile JSON** — new fields (`active_mode`, `timezone`,
  `default_cwd`) all use `omitempty` so existing profiles decode
  cleanly with empty values. No manual migration needed.
- **Runlog SQLite** — new columns are nullable with zero default;
  legacy rows return `0` via `COALESCE(...)` and remain valid.
- **Cron jobs** — no destructive change; existing rows have empty
  `timezone` and continue to run in UTC (the previous behavior).

## v0.20.4 - 2026-06-02

### Security
- **Context isolation in groups**: `ConversationKey` now includes `UserID`
  (3-column PK: `chat_id`, `thread_id`, `user_id`). Prevents user B in a
  group from seeing user A's continuity context. Legacy rows migrate
  automatically with `user_id=0` and remain accessible.
- **Session deactivation on crash**: `NewPersistentStore` now explicitly
  calls `DeactivateAll()` after loading the snapshot, guarding against
  stale active state on daemon crash between writes.

### Changed
- **Migration safety**: `ensureUserIDColumn()` runs inside a single SQLite
  transaction. If the daemon crashes mid-migration, the old table is
  untouched. Detection now uses `pragma_table_info.pk` instead of a
  fragile `strings.Contains` on the DDL.
- **Dream/nudge session accountability**: `SecurityContext` now includes
  `ChatID`, `ThreadID`, `UserID` in dream and nudge runs, enabling
  proper audit logging.

### Removed
- **Variadic `userID` from internal pipeline functions**: 8 functions now
  take `userID int64` directly instead of `...int64`. Removed the
  `sessionUserID()` helper — all call sites are explicit.

## v0.20.3 - 2026-06-01

### Fixed
- **Documentation drift**: corrected broken cross-references in
  `CONTRIBUTING.md` and `SECURITY.md`; aligned `README.md` current-state
  pointer and `internal/version/version.go` with the latest CHANGELOG entry.

## v0.20.2 - 2026-05-30

### Changed
- **Memory budget expanded**: `maxMemoryTotalChars` raised from 40K to 55K,
  `memorySummaryTriggerChars` from 30K to 45K. Compact mode now shows 5
  files (was 3), giving the model more context before switching to compact.
- **Memory cache TTL**: increased from 5s to 30s, reducing disk I/O during
  fast chat turns while still respecting explicit invalidations after writes.
- **Session lifecycle rotate threshold**: raised from 250K to 500K input
  tokens. The PI SDK handles auto-compaction internally (contextWindow -
  reserveTokens); rotation is now a last-resort safety net for corrupt
  sessions, not a routine token-management tool.
- **Compact threshold**: raised from 120K to 200K (informational only).
- **Post-rotate behavior**: after a successful rotation, `Continue` is now
  `true` instead of `false`, avoiding unnecessary cold_resume + checkpoint
  injection on the very next message.

### Fixed
- **MEMORY.md duplicate entries**: new `dedupMemoryIndex()` removes
  duplicate index entries by filename before loading memory into the prompt.
  Prevents duplicate entries from bloating the index and wasting token budget.

## v0.20.1 - 2026-05-29

### Fixed
- **Cron jobs without agent**: `BridgeCronRuntime` now extracts
  `Set cwd to <path>` from the cron prompt and elevates the capability
  profile to `execute_safe`, giving the LLM Bash/Read/Write tools.
  Previously, cron jobs without an explicit `AgentName` would run in
  `observe` mode (only Glob + LS) and fail to execute scripts.

## v0.20.0 - 2026-05-28

### Added
- **User × Project memory isolation**: `loadMemoryContents` now loads a
  user-scoped project memory layer between conversation project memory and
  user personal memory. Two users in the same project no longer share project
  memory. Cache invalidation covers the new directory.
  Closes Sprint E: user-scoped project memory is now wired end-to-end.

### Changed
- **Pipeline decomposition**: `pipeline.go` split into 4 cohesive files
  (`tool_monitoring.go`, `concurrent_message.go`, `redaction.go`,
  `turn_lifecycle.go`), reducing the file from 2586 to 1706 lines.
- **Bridge bundle sync**: `internal/bridge/bundle.ts` and compiled bundles
  synced with stall-steer fix and enhanced billing error detection.

### Fixed
- **Bridge billing error detection**: Added zero-token heuristic and
  diagnostic logging to detect silent PI SDK API errors (e.g., expired
  credits, auth failures) that previously returned empty unhelpful results.

## v0.19.8 - 2026-05-27

### Fixed
- **Session counter preservation**: `SetSession()` and `Set()` now use upsert
  pattern — suspect counters (emptyResults, processDeaths, suspectCount) and
  failure metadata are preserved when updating existing sessions. Fixes
  infinite cold-resume loop where rotation threshold could never be reached.
- **Streaming stall recovery**: `lastEventTime` now only updates on
  content-producing events (text deltas, tool execution), not lifecycle events
  (turn/compaction/retry). Steer flags reset when activity resumes, allowing
  repeated intervention on subsequent stalls. Added try/catch to prevent
  bridge crash on synchronous steer throw.
- **Stall observability**: Steer success now logged; urgent steer
  differentiated from regular steer in logs.
- **Silent PI SDK failure detection**: Added three detection paths for errors
  that the PI SDK clears from `state.errorMessage` between runs:
  `errorMessage`, last message `stopReason`/error fields, and a 0-token
  heuristic (session has prior work but returned no tokens). Previously
  sessions with API errors (e.g., 401 billing) would silently complete with
  zero tokens and contaminate suspect counters.
- **Billing error detection**: Added `isBillingError()` in Bridge to detect
  PI SDK credit/balance errors and surface a user-friendly Portuguese message
  ("Troque o modelo com /model ou adicione créditos"). Billing errors are
  flagged as `ACTION_REQUIRED` in the pipeline to prevent infinite rotation
  and noisy retries.

## v0.19.7 - 2026-05-26

### Fixed
- **Cold/inactive priority**: Sessions inactive without suspect failures now
  cold-resume regardless of enriched token stats, preventing unnecessary
  rotate+summary cycles on first message after idle.
- **Suspect thresholds**: Single empty result or process death no longer
  triggers rotation. Default thresholds increased from 1 to 2; only
  repeated suspect failures (≥2) cause rotation.
- **ActionRotate UX messages**: Replaced technical lifecycle phrases with
  neutral human-readable messages per Long Flow UX v2.

## v0.19.6 - 2026-05-26

### Added
- **Long Flow UX v2**: Lifecycle decision order updated so active healthy large
  sessions continue without Go-triggered compaction (PI SDK owns normal compaction).
  Cold/inactive sessions cold-resume before any compact decision. Dangerous
  sessions still rotate or recover safely.
- **UX message cleanup**: Tool-call tracker warnings, loop detector messages,
  and heartbeat updates now use human progress language without technical terms
  (`calls`, `ferramentas`, tool names, or raw counts).
- **Lifecycle tests**: Added `TestEvaluateLifecycle_InactiveAboveCompactThreshold`
  verifying cold resume takes priority over compaction for inactive sessions.
- **UX tests**: Added `TestToolCallTracker_WarningMessageOmitsTechnicalTerms`,
  `TestToolCallTracker_CriticalMessageOmitsTechnicalTerms`,
  `TestToolCallTracker_EveryNMessageOmitsTechnicalTerms`,
  `TestLoopDetector_UserMessageIsNeutral`,
  `TestHeartbeatMessage_OmitsChamadasDeFerramenta`, and
  `TestHeartbeatMessage_NormalModeOmitsToolCounts`.

### Changed
- `EvaluateLifecycle` no longer returns `ActionCompact` for sessions above
  `CompactAfterInputTokens`. Active healthy large sessions return `ActionContinue`.
- Cold/inactive check moved before the large-token check in `EvaluateLifecycle`
  priority order, ensuring cold sessions resume before any compaction decision.
- `toolCallTracker` user-facing messages: removed `calls`, `ferramentas`, tool
  name, and raw count; replaced with consolidation-focused progress language.
- `loopDetector` user-facing message: replaced "Pare o que está fazendo" with
  neutral "consolidar" wording; steer retains imperative instructions.
- `heartbeatMonitor` messages: removed `chamadas de ferramenta` and `ferramentas
  ativas`; normal mode shows simple "processando" notice.

## v0.19.5 - 2026-05-25

### Security
- Hardened Bridge Bash tool policy to fail closed for non-allowlisted commands.
- Blocked command-composition and sensitive-path bypasses in safe Git/build/test commands.
- Added Bridge policy regression tests and CI coverage for typecheck, build, and policy tests.
- Redacted sensitive tokens and paths before audit log persistence in Bridge and Go audit paths.

### Reliability
- Added panic recovery and bounded timeout handling for async runtime/pipeline paths.
- Ensured project index rebuild state resets after recovered panics.

## [0.19.4] - 2026-05-25

### Added
- **Cron thread isolation**: `TargetThreadID` for topic-thread delivery; jobs
  list/cancel scoped by owner+chat+thread
- **Cron SecurityContext**: attach enabled block-mode context with explicit
  safe `AllowedTools` (non-empty, excluding Read/Write/Edit/Bash for no-agent)

### Fixed
- **Bridge path traversal**: normalize relative/absolute paths before resolving;
  nested traversal via `subdir/../../outside` blocked
- **Bridge Bash/make policy**: block deploy/install/run; reject `-f`/`--file`/`-C`
  flags, variable assignments, and shell metacharacters in any position after `make`
- **Bridge orphan process**: kill/reap child on readLoop unexpected exit
- **Pipeline activeSessions race**: replace noop CancelFunc with real cancel
  context; `activeRun` struct + `CompareAndDelete` for race-free ownership
- **Pipeline runLogStates**: protect map iteration with mutex
- **Session persistence lost-update**: os.CreateTemp + atomic generation counter
  to prevent stale snapshots on concurrent persists
- **Cron no-agent/no-cwd**: defaults to observe profile with safe metadata-only
  tools instead of nil slice (avoid bridge SDK default fallback)

### Security
- **Cron security defaults**: always pass explicit non-nil `AllowedTools` to
  prevent bridge from applying SDK defaults (which may include Write/Edit/Bash)

## [0.19.3] - 2026-05-25

### Fixed
- Enforce Bridge source sync between `bridge/index.ts` and embedded `internal/bridge/bundle.ts`.
- Fix Bridge TypeScript type errors and add CI typecheck.
- Fix `make bridge` to use the canonical build script with `createRequire` banner.
- Resolve golangci-lint findings from review hardening.

## [0.19.2] - 2026-05-24

### Added
- **Long execution protection**: `toolCallTracker` warns user+model at 20, 50, and
  every 50 tool calls via `steer()` — prevents silent tool-call explosions
- **Loop detection**: `loopDetector` detects consecutive repeats, ping-pong
  patterns (A-B-A-B), and read-only spirals — breaks repetitive cycles with
  steer intervention
- **Timeout gradativo**: warns user at 25min and steers model to wrap up with
  partial summary, 5min before the 30-min hard timeout
- **Enhanced heartbeat**: includes tool call count every 8 beats
  (`"⏱️ 5m0s — processando (35 chamadas de ferramenta)"`)
- **Cold resume aprimorado**: checkpoint de timeout agora inclui a resposta
  parcial do assistente (`partialAssistant`) — modelo retoma de onde parou
  em vez de começar do zero
- **Streaming stall recovery**: bridge envia steer ao modelo após 60s e 120s
  de stall — quebra silêncio prolongado com instruções progressivas
  em vez de apenas logar o diagnóstico
- **Continuity snapshot**: inclui `LastCheckpoint` no fallback context para
  fornecer mais contexto no cold resume

## [0.19.1] - 2026-05-24

### Fixed
- **Goroutine recovery**: Added `defer recover()` to proxyChannel, album flush timer, and cmd.Wait() wrappers — prevents daemon crash on panic
- **Nil pointer safety**: Added nil bridge checks in `Cancel()` and `WorkStatus()` — prevents panic when called before bridge initialization
- **Process hang**: Added 5-second timeout to `cmd.Wait()` in `Stop()` and `cleanupAfterPanic()` — prevents infinite daemon hang on zombie processes
- **Mutex contention**: Released session mutex during disk I/O in `persistLocked()` — prevents cascading latency on all session operations
- **Race condition**: Fixed data race between `recordToolUse` and `completeRunLog` — prevents runlog corruption on concurrent tool updates
- **Goroutine leaks**: Added 30-min timeout to bridge Execute proxy and 30s timeout to ExecuteSync drain — prevents unbounded goroutine leaks
- **Audit reliability**: Logged audit write, file close, and rename errors instead of silently dropping them
- **Auth reliability**: Validated auth symlink target existence on startup — recreates broken symlink automatically
- **Secret redaction**: Applied redaction before truncation in 7 locations (cron memory loading, dream consolidation, prompt builder, document upload) — prevents secret leakage through truncated data
- **Secret leakage**: Added `RedactSecrets()` to 6 additional `log.Printf` calls in Telegram bootstrap and command handlers
- **Error visibility**: Logged 12 swallowed `SendError`/`SendText` failures — prevents silent message delivery failures
- **Session locking**: Reverted RLock to Lock in 5 session read methods that mutate `lastSeen` — prevents data races on concurrent reads

### Added
- **Regression tests**: 5 new tests for nil bridge guards and runLog WaitGroup correctness

## [0.19.0] - 2026-05-24

### Removed
- Plan Mode feature entirely removed (feature pruning)
  - Removed `internal/planning/` package and all planning state management
  - Removed `/plan*`, `/execute` Telegram commands, menu entries, help text
  - Removed Plan Mode prompt injection, offer heuristic, intent detection
  - Removed artifact observation and reconciliation
  - Planning remains conversational and user-driven, case by case
  - Orchestrator and `aurelia-plan` execution preserved for legacy path

## [0.18.0] - 2026-05-24

### Added
- **Plan Mode Architecture** (Sprint D, T0–T13):
  - `internal/planning/` package with `State`, `Store`, `OfferStore`, `Artifact`, `ProjectContext`
  - SQLite persistence for planning state and offers with optimistic locking
  - Stat-only project discovery: detects layouts (TLC, RFC, ADR, planning), stacks (Go, Node, Python, Rust), git, docs
  - `BuildPlanningPrompt()` — injects planning context into system prompt when state is active
  - Tool observer — watches Write/Edit/MultiEdit events and tracks materialized artifacts
  - Offer-only heuristic — offers Plan Mode on intent detection, never imposes; 5-minute throttle
  - Commands: `/plan`, `/plan status`, `/plan list`, `/plan cancel`, `/plan reset`, `/execute`
  - Safe handoff to orchestrator via `ExecutionContext` — only executes when state is `awaiting_exec`
  - State cleanup after successful execution; preservation on failure
  - E2E test covering full Plan Mode flow
- **Transport Abstraction** (T-1, from Sprint D):
  - `internal/transport/` package with generic `Transport` interface
  - `TelegramTransport` implementing `Transport`, reusing existing send helpers
  - Desacouples pipeline from Telegram-specific transport

### Changed
- `planningKeywords` no longer includes approval/execution terms ("aprovado", "execute", etc.)
- Replaced silent `BuildOrchestratorPrompt` injection with explicit Plan Mode offer

### Fixed
- **Plan Mode command registration** — wired `/plan*`, `/execute` handlers to Telegram bot routes, menu, and help text
- **Telegram menu registration** — removed invalid spaced commands (`plan status`, `plan list`, etc.) from `SetCommands` to prevent `BOT_COMMAND_INVALID (400)` failure
- **Plan Mode thread routing** — all plan command replies now include `ThreadID` so responses arrive in the correct topic/thread instead of the general chat

## [0.17.0] - 2026-05-24

### Added
- **transport**: Extract generic `Transport` interface from Telegram (T-1, Sprint D).
  - New `internal/transport/` package with `Transport`, `IncomingMessage`, `OutgoingMessage`, `ImageAttachment`.
  - `TelegramTransport` implements `Transport` reusing existing send/format helpers.
  - `telegramPipelineOutput` delegates `SendText`, `SendError`, `SendReply`, `StartTyping` to `Transport`.
  - `MockTransport` in `transport_test.go` for future pipeline tests.
  - Zero regressions: all Telegram and pipeline tests pass unchanged.
  - Prepares ground for TUI (Sprint J) and other future surfaces.

## [0.16.1] - 2026-05-24

### Security
- **H-01**: Fix topic memory containment root mismatch — topic memory writes now accepted under instance root.
- **H-02**: Prevent PII leakage in system prompt — absolute filesystem paths replaced with aliases.
- **M-01**: Cap memory file reads at 9000 bytes to prevent OOM.
- **M-02**: Cap memory fact writes at 1MB to prevent disk exhaustion.

### Changed
- Canonicalize topic memory path to `~/.aurelia/topics/...`.
- Canonicalize team memory path to `~/.aurelia/projects/<slug>/team/`.
- Update Node.js prerequisite to `>=20.6.0`.
- Refactor memory_writer.go and prompt_builder.go helpers for maintainability.

### Added
- `.specs/codebase/AGENT_RESPONSIBILITY_MODEL.md` — PI↔Aurelia responsibility boundary.
- Updated specs: project-memory, wiki-memory, ARCHITECTURE, STACK, ROADMAP.
- Updated README with 6-scope memory model.

## [0.16.0] - 2026-05-24

### Added

- **orchestration**: Close Orchestration Cycle — execution now closes end-to-end.
  - `ExecutionContext` with `RunID`, `RepoRoot`, `BaseBranch`, `ChatID`, `ThreadID`, `Feature`, `CreatePR`.
  - Git preflight validates clean base tree, correct branch, and `gh` availability.
  - Worktree run namespace (`worker/<runID>/<task>`) with isolation across runs.
  - Artifact collection per task: `git status`, `git diff --stat`, `git diff`, verify command.
  - Fail-closed validation with artifact-aware prompt; bridge/parse errors return error.
  - Per-task retry loop (up to 3 attempts) with feedback appended to user prompt.
  - Dependency skip: dependents of failed/unverified/escalated tasks are marked `skipped`.
  - Serial merge of approved worktrees in deterministic task-id order after each wave.
  - Merge conflict stops the run and preserves worktree/branch for manual recovery.
  - `ExecutionManifest` tracks task statuses: `pending`, `running`, `approved`, `failed`, `skipped`, `unverified`, `escalated`.
  - `UpdateTasksStatus` marks checkboxes only for `TaskApproved`.
  - `CommitChanges` stages only provided files; returns `ErrNothingToCommit`.
  - Optional PR creation when `plan.CreatePR=true` and `gh` available; friendly note otherwise.
  - Feature doc lookup via `plan.Feature` (`loadFeatureDocs`) instead of alphabetical glob.
  - Integration smoke test: one-task plan in temp repo on non-main branch.
- **plan schema**: `Plan` gains `Feature`, `CreatePR`, `Verify`; `Task` gains `Verify`.
- **worker prompt**: `BuildWorkerPrompt` no longer embeds full task body; uses summaries only.
- **orchestrator config**: `MaxValidationRetries` (default 3) and `VerifyTimeout` (default 2m).

### Changed

- `ExecutePlan` signature now accepts `ExecutionContext` and `Validator` callback, returning `(*ExecutionManifest, []TaskResult, error)`.
- `Validate` signature now accepts `*ArtifactSnapshot`; bridge/parse failures return error.
- `TaskResult` extended with `Status`, `Approved`, `Skipped`, `Attempts`, `ChangedFiles`, `Verify`.

### Fixed

- `BuildWorkerPrompt` duplicate task body removed (was wasting tokens and confusing log diffing).

## [0.15.0] - 2026-05-23

### Added

- **session-lifecycle**: gerenciamento automático de ciclo de vida de sessão PI.
  - Estados de saúde: healthy, large, suspect, dangerous, cold.
  - Decisões automáticas: continue, cold_resume, compact, rotate.
  - Estatísticas reais do PI (input_tokens) direcionam compactação/rotação.
  - Limiares `MaxEmptyResultsBeforeRotate` e `MaxProcessDeathsBeforeRotate`.
- **bridge**: novos comandos `get-session-stats`, `compact-session`, `rotate-session`.
- **bridge**: forwarding de eventos de lifecycle (compaction, agent, turn, auto_retry).
- **session**: metadados de falha (timeout, empty result, process death) persistem
  em `sessions.json` e sobrevivem a restart/deploy.
- **config**: seção `session_lifecycle` com validação de limites.
- **ux**: mensagens no Telegram para compactação e rotação de sessão.

### Fixed

- **session**: process death em retry agora marca o userID correto, preservando
  isolamento em grupos/forums.
- **session**: compactação/rotação com falha registra metadado de falha.
- **security**: summary de rotação passa por redaction e escape de delimitadores.

## [0.14.2] - 2026-05-23

### Added
- **ci**: adicionados gates paralelos de lint e segurança no GitHub Actions.
- **make**: adicionados `make lint`, `make sec` e `make check` para paridade local com CI.
- **session**: sessões PI do Telegram agora são persistidas e retomadas em modo frio após restart/deploy.
- **session**: notificação automática para sessões interrompidas recentemente, com retomada segura via `continuar`.

### Changed
- **lint**: baseline limpo com `errcheck`, `govet`, `ineffassign`, `staticcheck` e `unused` habilitados.

### Technical Debt
- **ci-hardening**: reavaliar futuramente `gocritic`, `misspell` e `goconst`.
  Eles ficaram fora do gate inicial por ruído de estilo/PT-BR/constantes artificiais.
- **staticcheck**: reavaliar checks de estilo `ST1020`, `ST1016` e `ST1005`,
  desabilitados no baseline atual para manter o gate high-signal.

## [0.14.1] - 2026-05-23

### Fixed
- **lint**: resolvidos 7 problemas de lint e testes identificados pelo relatório
  Conrado após o sprint de observabilidade v0.14.0.

## [0.14.0] - 2026-05-23

### Features

- **observability**: new local observability layer with run_events correlation,
  structured slog logging, phase constants, and Recorder interface.
- **schema**: run_journal extended with user_id, entrypoint, agent_name, provider,
  model, capability_profile, duration_ms, tokens, cost, error_class,
  timeout_origin, used_fallback, session_file, parent_run_id.
- **run_events table**: durable timeline per run with indexes.
- **pipeline correlation**: RunContext created before Bridge execution; events
  emitted for telegram_received, bridge_request_started, tool_use, tool_result,
  bridge_result, run_completed, run_failed, run_timed_out, retry, fallback.
- **/status upgrade**: now shows short run_id, provider/model, duration, tokens,
  cost, and error info for failed runs.
- **CLI debug**: `aurelia debug last|run|errors|metrics` with --json output.
- **Telegram debug**: owner-only /debug last, /debug run, /debug errors.
- **local metrics**: aggregate queries over time windows with breakdown by
  provider, model, and entrypoint (p50/p95 duration, tokens, cost).
- **docs/OBSERVABILITY.md**: operator guide covering CLI, Telegram, config.

## [0.13.9] - 2026-05-22

### Security
- Added environment variable overrides for API keys and Telegram bot token without writing env secrets back to config.
- Hardened `/cwd` path resolution with cleaned/symlink-resolved authorized prefix validation.
- Delimited and size-limited uploaded Markdown document content before prompt injection.
- Added dedicated rotating security audit log at `~/.aurelia/audit.log`.
- Differentiated `privileged` from `execute_safe` capability profile and added regression coverage.

## [0.13.8] - 2026-05-22

### Changed
- Roadmap/specs now mark User Isolation runtime hardening as complete and audited.
- Clarified that user×project private memory moved to Sprint D, while session/runtime/Bridge isolation is closed.

## [0.13.7] - 2026-05-22

### Fixed
- Bridge: modelo não encontrado agora lança erro claro em vez de log silencioso
  que fazia o processamento travar sem resposta.
- `/stop` agora passa `userID` para cancelar a sessão correta (antes usava
  `userID=0` que não casava com a chave de sessão).
- Daemon: `auth.json` agora é symlink para `~/.pi/agent/auth.json` em vez de
  cópia única, evitando credenciais stale que causavam hangs silenciosos.
- Config: `telegram_allowed_group_ids` não é mais perdido na serialização
  (removido `omitempty` que causava ciclo nil→vazio→omissão).
- Config: `default_owner_user_id` não é mais perdido, e normalize preenche
  do primeiro whitelist user quando zero.
- Goroutine `chatActionLoop` agora tem `defer recover()` para não morrer
  silenciosamente em caso de panic.

## [0.13.6] - 2026-05-22

### Fixed
- Model cache pre-warming: bridge agora inicia e popula cache de modelos durante
  a inicialização do bot, eliminando timeout de 10s e cache vazio por 5 minutos
  após restart do daemon.
- Timeout de `getModels` aumentado de 10s para 30s em operações de modelo
  (`cmdSetModel`, `handleModelCommand`) para garantir que bridge fria tenha
  tempo de iniciar.
- Resultado vazio do bridge não é mais cacheado — evita "modelo não encontrado"
  falso persistente quando a bridge retorna lista vazia temporariamente.
- Auto force-refresh: quando cache está vazio, `/model <nome>` tenta novamente
  com força total antes de declarar "não encontrado".
- `lista modelos` agora prioriza provedores locais (`ollama`, `ollama-tailscale`,
  `lm-studio`) antes do limite de exibição de 25 modelos.
- Botão refresh mostra resumo com timestamp e modelos locais, eliminando erro
  "message is not modified" do Telegram.
- Mensagem de permissão negada em callback de modelo restaurada para `c.Edit()`
  (Telegram inline), corrigindo teste quebrado `TestHandleModelCallback_NonOwnerSetDeniedWithoutMutation`.

### Dívida Técnica
- Model menu não mostra provedores locais (ollama-tailscale, lm-studio) em alguns
  cenários pós-restart. Suspeita: cache de modelos não é populado corretamente
  quando bridge inicia pela primeira vez (race condition entre prewarm e primeiro
  uso do `/model`). Solução permanente: migrar list-models para endpoint Go direto
  com PI SDK ou implementar descoberta Ollama nativa em Go.

## [0.13.5] - 2026-05-22

### Fixed
- Bridge `list-models` agora usa `ModelRegistry.getAvailable()` em vez de
  `getAll().filter(hasConfiguredAuth)`. O método anterior só verificava
  `auth.json`, omitindo provedores como Ollama cuja chave de API está definida
  no próprio `models.json` (ex: `"apiKey": "ollama"`).

## [0.13.4] - 2026-05-22

### Added
- `/model refresh` command and `🔄 Atualizar modelos` button for explicit PI model catalog refresh
- `mdl_refresh` callback handler with owner-gated force-refetch, cache update, and menu redraw
- `TestCmdRefreshModels_BridgeUnavailable`, `TestCmdSetModel_OwnerRefreshBypassesFreshCache`,
  `TestCmdSetModel_NonOwnerRefreshDeniedWithoutFetch`, and
  `TestHandleModelCallback_NonOwnerRefreshDeniedWithoutFetch` for refresh security boundary and edge cases

### Fixed
- Bridge `healthTimer` is now declared outside `try` to prevent `ReferenceError` in early-exit paths
- Removed redundant cache write in `handleModelCommand` (`getModels` already populates cache)
- `cmdRefreshModels` now handles empty model list gracefully instead of showing `✅ 0 modelos`

### Changed
- `getModels` uses `force` parameter to bypass 5-min cache TTL on refresh
- Model listing commands use `activeModelLister()` interface instead of direct `bc.bridge` access
- `sendProviderMenu` includes refresh button as first row

## [0.13.3] - 2026-05-22

### Changed
- Documentation and roadmap now describe the current architecture thesis: PI SDK owns the cognitive/execution engine, while Aurelia owns product identity, Telegram UX, scoped memory, Wiki direction, workflows, policy/audit and continuity.
- Updated stale specs to reflect v0.13.x state: PI compaction/context loading are active, security wraps `session.agent.beforeToolCall`, `internal/agents` remains an Aurelia product-layer boundary for now, and User Isolation hardening is focused on scoped `CancelAllForUser`.
- Added deterministic PI-boundary validation tests for Bridge compaction, context-file loading, `session_file` emission, security hook API usage, and Aurelia-owned specialist agent metadata.
- Orchestration handoff now preserves thread/cwd/user context, refuses plan execution without a bound cwd, runs git preflight before worker/doc operations, and uses run-scoped orchestrators for the handoff repository.
- Orchestration worktrees now use run-scoped branch/path namespaces, captured-base-branch merge, startup orphan cleanup counts, and per-repository serialization for base checkout mutations.
- Model selection now supports `PI default` mode via `/model auto`, allowing the PI SDK to choose the default model when no explicit Aurelia override is configured.

### Fixed
- Security guard-rails no longer throw "PI SDK version too old" — the bridge was using
  `session.on("tool_call")` which doesn't exist in the PI SDK. Replaced with wrapping
  `session.agent.beforeToolCall`, the correct hook for intercepting and blocking tool calls.
- `CancelAllForUser` now cancels only sessions owned by the target Telegram user and sends scoped bridge `abort` commands with `chat_id`, `thread_id`, and `user_id`, avoiding cross-user cancellation in shared chats/topics.
- Orchestration workers now use isolated non-persistent Bridge session scopes, worktree-required tasks fail closed if worktree creation fails, validation/consolidation run in the handoff cwd, and Telegram preflight errors no longer include dirty file paths or raw git output.
- Worktree merge failures now fail the task, emit a sanitized `merge_failed` event, preserve the worker branch/worktree for recovery, validate run IDs before git commands, and abort conflicted merges cleanly.
- Model switching is consistently owner-gated across slash commands, natural-language commands, and inline callbacks; callback selections are revalidated against the PI model list before persistence.

## [0.13.2] - 2026-05-21

### Fixed
- `EnsureBridge` now detects stale `bundle.js` on disk by comparing a SHA-256 hash of the embedded TypeScript source (`.source-hash`). When the source changes (e.g. timeout fix from 10min to 30min), the bundle is automatically rebuilt — eliminating the root cause of persistent query timeouts.

## [0.13.1] - 2026-05-21

### Fixed
- Reinstated per-user session scoping for bridge resume/session storage and bridge-side active session commands (`steer`, `follow-up`, `abort`, `get-state`), preserving User Isolation semantics.
- Migrated runtime reset, retry, timeout, empty-result, prompt continuity and status flows to user-scoped session APIs so two users in the same chat/topic do not share or clear each other's PI session.
- Increased the bridge idle watchdog from 2 to 15 minutes and aligned orchestration timeout to 30 minutes to avoid premature cancellation of long PI-managed runs.
- Timeout runlog/continuity entries now distinguish `max_execution_timeout`, `idle_bridge_timeout`, `bridge_query_timeout`, and `provider/pi_timeout` origins.

## [0.13.0] - 2026-05-21

### Changed
- Bridge model resolution: replaced custom `resolveModelFromRegistry()` (~42 lines of fuzzy matching) with native PI SDK `ModelRegistry.find()` + exact-ID fallback
- Bridge security hooks: simplified to single source of truth in TypeScript; Go now keeps only security config/profile types while enforcement runs in the Bridge
- Bridge session management: simplified `internal/session/store.go` to track `sessionFile` (disk path) instead of opaque `sessionID`; Bridge emits `session_file` in `system` and `result` events
- Bridge context pruning: enabled PI SDK `SettingsManager.compaction` (`enabled: true`); removed manual token-threshold auto-reset logic from the pipeline
- Bridge prompt assembly: delegated `CLAUDE.md`/`AGENTS.md` discovery to PI SDK `DefaultResourceLoader` (`noContextFiles: false`); removed `buildProjectDocsSection()` from prompt builder (~24 lines)
- `/usage` command now reports that token usage is managed by PI SDK compaction instead of manual tracking

### Removed
- `internal/security/security_test.go` — tests for removed Go policy evaluator
- `internal/session/tracker_test.go` — tests for removed manual tracker/reset behavior
- `buildProjectDocsSection()` from `internal/pipeline/prompt_builder.go`; PI SDK discovers context files natively
- Auto-reset logic in pipeline (`resetSessionAfterSuccessfulTurn`, token threshold checks)

### Fixed
- Reduced total codebase by ~1.312 lines while preserving all user-facing functionality

## [0.12.0] - 2026-05-20

### Added
- Bridge-side session management: sessions persist after query for steer,
  followUp, and abort commands
- New bridge commands: steer (interrompe e redireciona), follow-up
  (enfileira após atual), abort (cancela)
- get-state bridge command for pipeline to query session status
  (isStreaming, pendingCount)
- Idle timer (5 min) auto-disposes inactive sessions on the bridge

### Changed
- Pipeline no longer manages in-process queue — delegates
  cancel/supersede/follow-up classification to bridge commands
- Text splitting now converts markdown→HTML before splitting at tag
  boundaries, preventing broken HTML in Telegram messages
- ReportToolResult only appends " ✓" instead of full tool result
  for cleaner progress display

### Removed
- In-process runSupervisor (queue logic, cancel, supersede) — replaced
  by bridge-side session lifecycle

## [0.11.5] - 2026-05-20

### Changed
- Nomes das ferramentas no progresso encurtados de frases completas
  ("📖 lendo arquivo") para rótulos curtos ("📖 read")

### Removed
- Logs de debug do streaming (first-delta/first-3-chars) removidos do pipeline
  e do progress reporter — eliminando ruído nos logs.

## [0.11.4] - 2026-05-20

### Changed
- Nomes das ferramentas no progresso encurtados de frases completas
  ("📖 Reading file...") para rótulos curtos ("📖 read")

### Fixed
- Streaming: primeiro caractere do texto do assistente não é mais perdido
  — flush envia o texto completo acumulado (assistantText) em vez do
  incremental (textSinceLastFlush), evitando que o caracter inicial
  desapareça após tool_use intermediário

## [0.11.3] - 2026-05-20

### Corrigido
- Primeiro caractere do streaming não é mais cortado: `lastStreamFlush` agora
  inicializado com `time.Now()` em vez de zero value, evitando flush imediato
  do primeiro fragmento de texto (que sobrescrevia o início da frase).

### Adicionado
- Resumo dos resultados das tools agora aparece no progresso em tempo real:
  "📖 Reading file... → [3 files found, 240 lines]" em vez de apenas
  "📖 Reading file..."

## [0.11.2] - 2026-05-20

### Fixed
- Audio transcription failing: temp file now preserves original extension
  (.ogg/.mp3) so Groq Whisper API accepts the file format
- Error messages leaking to wrong chat in group topics: SendContextText now
  passes ThreadID via SendOptions, keeping responses in the correct topic
- Four additional SendContextText calls (album, document, image handlers)
  now also pass ThreadID for topic-safe error messages

### Changed
- Groq STT model upgraded from whisper-large-v3 to whisper-large-v3-turbo
  (faster, same accuracy)
- Added explicit language=pt and temperature=0.0 to Groq transcription
  requests for better Portuguese accuracy and deterministic output

## [0.11.1] - 2026-05-20

### Fixed
- Pipeline memory split: per-user memory layer added to loadMemoryContents,
  bridging nudge writes (~/.aurelia/users/<id>/memory/) with pipeline reads
- isOwner detection: UserGate now compares userID against config's
  DefaultOwnerUserID instead of hardcoded false
- Per-user USER.md stub fallback: when the per-user USER.md is an
  auto-generated stub, the rich global USER.md content is appended as
  "Legacy Preferences"
- migrate-multi-user --force flag: was defined but never actually
  bypassed conflict detection; now properly overwrites destination files

### Changed
- PromptBuilder.buildMemoryInstructions and loadMemoryContents accept
  userID int64 parameter for per-user memory loading
- NewUserGate accepts ownerUserID int64 for dynamic owner detection
- BotController wires ownerUserID from config.DefaultOwnerUserIDOrFallback()

## [0.11.0] - 2026-05-20

### Added
- User Isolation: runtime state now scoped per authorized Telegram user_id
- TurnContext, SessionKey{chat,thread,user}, ConversationKey{chat,thread}
- internal/users/ package: Profile (JSON), Resolver (paths), Store (CRUD)
- migrate-multi-user CLI with --dry-run, --resume, --force, two-phase moves
- Cron ownership: ListByOwner, GetByOwnerAndID, lifecycle by owner
- Session/persona per-user: BuildPromptForUser with per-user USER.md
- Memory/dream/nudge per-user: user-scoped dirs, per-SessionKey buffers
- Onboarding flow: conversational name+bio, language detection (pt/en)
- UserGate middleware: intercepts unprofiled users before commands/pipeline
- /users command (owner-only): list authorized users
- /forget-me command: self-deletion with inline confirmation
- Owner-only guard on /model and global config commands
- CancelAllForUser in pipeline for /forget-me cleanup

### Changed
- CWD remains shared per conversation (ConversationKey), not per-user
- Topic memory stays global in ~/.aurelia/topics/
- NudgeBuffer keyed by SessionKey (chat+thread+user)

## [0.10.0] - 2026-05-20

### Adicionado
- Timeout deslizante (idle timeout): substitui o timeout fixo de 10 minutos por
  um teto máximo de 30 minutos + janela deslizante de 2 minutos sem eventos.
  Se o bridge estiver produzindo eventos (text_delta, tool_use), o cronômetro
  reseta — eliminando timeouts falsos em tarefas longas mas ativas.
- Streaming de texto em tempo real: o texto gerado pelo modelo (eventos
  `assistant`/text_delta) é enviado ao usuário durante o processamento via
  edição da mensagem de progresso, eliminando o silêncio durante tarefas longas.
- Métricas de progresso: a barra de progresso agora exibe o número de
  ferramentas usadas, total aproximado de caracteres gerados, e um snippet
  do texto sendo produzido (truncado a 400 caracteres).

### Alterado
- `bridgeExecutionTimeout` aumentado de 10 para 30 minutos.
- Timeout do bridge JS (`timeoutMs`) aumentado de 10 para 30 minutos para
  alinhamento com o timeout máximo do Go.
- `handleContextOutcome` agora reconhece qualquer erro de contexto (`ctx.Err() != nil`),
  não apenas `DeadlineExceeded`, cobrindo corretamente cancelamentos por idle timeout.

## [0.9.0] - 2026-05-20

### Adicionado
- Progressive Summarization na Continuity Engine: a cada 5 turns, o LLM gera
  um resumo acumulado da conversa armazenado no `ConversationState`, substituindo
  o truncamento determinístico. Configurável via `summary_interval` (0 = desliga).
- Thinking heartbeat: quando o modelo processa sem ferramentas por >15s, envia
  "⏱️ Xm Xs — processando sem ferramentas ativas no momento" como feedback visual.

### Alterado
- Fila de mensagens agora aceita até 3 mensagens enfileiradas por chat/thread
  (antes: apenas 1, com sobrescrita silenciosa). Mensagens excedentes recebem
  aviso explícito. FIFO preservado.
- Continuity freshness: continuity block só é injetado quando necessário,
  economizando ~500-1000 tokens/turn em sessões ativas. Hot <5min + sessão ativa
  = skip; cold >7d = skip; continuation explícita = sempre injeta.
- Alinhamento com PI SDK: `MaxSessionTokens` aumentado de 100K para 180K, pois
  o PI SDK já gerencia compactação automática. Auto-reset agora é safety net.
- Fallback context transfer: antes de trocar para provedor secundário, captura
  resumo da continuidade e injeta no system prompt do fallback para minimizar
  perda de contexto.
- Warning zone adicionada ao tracker (log quando sessão atinge 80%+ do threshold)
  para futuros nudges informativos.

### Corrigido
- N/A (nenhum bug corrigido nesta versão; todas as mudanças são melhorias)

## [0.8.1] - 2026-05-20

### Corrigido
- Telegram reaction emojis agora usam apenas emojis permitidos (👀→👍, ✅→🎉),
  eliminando erros REACTION_INVALID nos logs.
- Log de divergência de conteúdo agora só dispara em diferenças significativas (>500 chars),
  reduzindo ruído de streaming normal do SDK.

### Removido
- Logs verbosos de session store e system prompt breakdown (~2 linhas/mensagem) removidos.

## [0.8.0] - 2026-05-20

### Segurança
- Implementado Security Guard-Rails completo com CapabilityProfile governance (5 níveis: observe, read_only, edit_project, execute_safe, privileged).
- Policy engine com EvaluateToolCall: detecção de comandos destrutivos, exfiltração e paths sensíveis (.env, ~/.ssh, etc).
- Bridge hook `pi.on('tool_call')` com fail-closed — bloqueia tools antes da execução.
- Audit trail em JSON lines (stderr) para todas as chamadas governadas.
- Duas fases: Warn (log only) e Block (padrão, bloqueia tudo) — configurável via `Security.Mode`.
- Integração com pipeline, config, dream, orchestrator e agents.
- 44 testes unitários no pacote `internal/security/`.

## [0.7.20] - 2026-05-20

### Segurança
- Fix path traversal em download de arquivos Telegram via `os.CreateTemp` + validação de `filepath.Base`.
- Fix vazamento entre chats no album buffer: chave do mapa agora inclui `chatID` (IDOR mitigation).
- Fix redaction de secrets antes de truncamento em `startRunLog`, evitando leak parcial de credenciais.
- Fix escape de `&` em delimitadores de continuity, prevenindo prompt injection via entidades HTML.

### Corrigido
- Fix falso sucesso em `ExecuteTask` quando o bridge fecha sem emitir evento `result` (regressão v0.7.2).
- Mitigação de DoS/OOM no bridge: `readLoop` agora usa `bufio.Scanner` com limite de 10MB por linha NDJSON.
- Adicionado `recover()` em 8 goroutines críticas (pipeline, bridge, dream, orchestrator) para prevenir morte do daemon.
- `cleanupAfterPanic` no bridge agora mata o processo filho zumbi e limpa estado.
- Log de erros de `SendText` nas branches de admission do pipeline.
- Timeout de 5s em `startRunLog` e 30min em execução de cron jobs.
- Log de worktree errors no orchestrator com fallback para repo root.
- Refatoração de `handleResultEvent` em helpers para reduzir complexidade ciclomática.

### Testes
- Testes para album buffer com chave composta por chat.
- Testes para `escapeUntrusted` com escape de `&`.
- Testes para bridge closed without result.
- Testes para panic recovery em callbacks e cleanup.

## [0.7.19] - 2026-05-19

### Corrigido
- Blocos internos `aurelia-plan` inválidos ou incompletos não são mais enviados crus ao Telegram.
- Runlog e continuidade agora armazenam versão sanitizada da resposta quando o modelo gera plano de execução interno.
- Parser do orquestrador agora detecta marcadores de plano mesmo quando o JSON está malformado, evitando fallback para resposta normal.

### Segurança
- Prompts internos de workers são omitidos das respostas de chat e da memória persistente quando a extração do plano falha.

## [0.7.18] - 2026-05-19

### Adicionado
- Preflight determinístico para pedidos de leitura/análise de codebase sem `cwd` ativo, respondendo localmente com orientação de `/cwd` e evitando chamada desnecessária ao LLM.
- Sugestões de projetos conhecidos são exibidas quando disponíveis, com comandos `/cwd <path>` prontos para uso.

### Alterado
- Prompt agora diferencia memória carregada/projetos conhecidos de `cwd` operacional ativo quando nenhum projeto está fixado.
- Quando há projetos conhecidos mas sem `cwd` ativo, o agente é instruído a sugerir `/cwd <path>` em vez de dizer que não lembra.
- `/cwd` sem argumentos agora mostra status efetivo com mais clareza e marca projetos conhecidos como sugestões, não como cwd ativo.

### Corrigido
- `/cwd` não mostra mais "nenhum cwd ativo" quando há binding ativo apenas no tópico.
- Numeração da cadeia de resolução de cwd agora é dinâmica.

## [0.7.17] - 2026-05-19

### Adicionado
- `/cwd` agora sugere projetos conhecidos do mesmo usuário quando o chat atual não tem projeto fixado.
- Pedidos de leitura/análise de codebase sem `cwd` agora recebem orientação explícita para fixar um projeto com `/cwd <path>`.
- Prompt breakdown agora inclui seções de `identity`, `continuity`, `last_run`, `long_task` e `project_docs`.

### Alterado
- Pipeline agora carrega `userID` até a montagem do prompt para permitir sugestões seguras de projetos por usuário.
- Logs em chat mode agora indicam quando ferramentas de arquivo foram desabilitadas por ausência de `cwd`.

### Segurança
- Sugestões de projetos conhecidos são filtradas por `created_by`, evitando exposição entre usuários.

## [0.7.16] - 2026-05-19

### Adicionado
- Continuity Engine v1 com estado persistente por chat/thread para preservar contexto mínimo entre rodadas.
- Novo store SQLite `conversation_state` para cwd, objetivo ativo, último intent, resumo, checkpoint, status de run e estado de sessão.
- Injeção de `Conversation Continuity` no prompt antes de memórias longas.
- Cobertura de lifecycle para sucesso, timeout, empty result, erro, process death, retry failure, auto-reset e bridge death.

### Segurança
- Dados de continuidade são redigidos e limitados antes da persistência.
- Blocos de continuidade/checkpoint escapam delimitadores para reduzir prompt injection persistente.

### Testes
- Adicionados testes de store, formatação, lifecycle e ordenação do prompt para continuidade.

## [0.7.15] - 2026-05-19

### Corrigido
- Auto-reset agora preserva `cwd` e project binding ao limpar apenas a sessão ativa.
- Turno atual é registrado antes do reset automático, preservando continuidade para nudge/memória.
- Uso de tokens agora é isolado por chat e tópico/thread.
- Memória de projeto e tópico agora tem prioridade sobre memória global no prompt.
- Checkpoints do run journal podem ser reinjetados no prompt para retomada após falhas, timeouts ou sessões frias.
- NudgeBuffer agora usa Snapshot/Commit com token de versão, evitando descarte de mensagens novas.
- Templates de nudge agora são JSON-only e não instruem uso de ferramentas desabilitadas.
- `/status` exibe checkpoint de forma segura, com truncamento UTF-8 e redaction ampliada.

### Segurança
- Transcripts enviados ao nudge são redigidos antes de chamadas ao LLM.
- `/memory checkpoint` sanitiza notas antes de persistir.
- Arquivos `.md` symlinkados em diretórios de memória são ignorados.

## [0.7.14] - 2026-05-19

### Corrigido
- Resultados vazios após execução com tokens/turns agora geram uma resposta de recuperação com checkpoint/resumo seguro.
- Sessões com resultado vazio após trabalho são desativadas para evitar continuar contexto PI suspeito.
- Nudge agora trata `{"updates":[]}` como noop válido.

### Alterado
- Injeção de memória entra em modo compacto quando o prompt ficaria grande demais, priorizando índices, `current_task.md` e arquivos recentes.

## [0.7.13] - 2026-05-19

### Adicionado
- Comando `/stop` para interromper o processamento ativo sem limpar sessão, uso, cwd ou memória.

## [0.7.12] - 2026-05-19

### Adicionado
- Run journal persistente para registrar progresso, status, checkpoints e resumo de ferramentas em tarefas longas.
- Detecção leve de tarefas longas com orientação para quebrar execução em etapas menores.
- `/status` agora inclui o último run persistido quando disponível.

### Corrigido
- Timeouts agora desativam a sessão corrente para evitar continuar sessões PI suspeitas.
- Nudge e dream agora parseiam respostas vindas em `text` ou `content`.
- Bridge embutido sincronizado com o source TypeScript atual.

### Segurança
- Redação reforçada de secrets em prompts, checkpoints, erros, logs e eventos do Bridge.
- Run journal usa permissões restritas para banco SQLite e sidecars.

## [0.7.11] - 2026-05-19

### Corrigido
- Nudge e dream agora aceitam respostas JSON com fences Markdown ou texto ao redor, reduzindo receipts `invalid`.
- Prompts de nudge/dream reforçados para retornar somente JSON.
- Receipts inválidos agora incluem diagnóstico seguro sem armazenar output bruto do modelo.

### Observabilidade
- Comandos `/memory status` e `/memory checkpoint` agora registram logs operacionais sem expor conteúdo sensível.
- Snippet de restart em `AGENTS.md` agora redireciona stdout/stderr para `~/.aurelia/logs/`.

## [0.7.10] - 2026-05-19

### Alterado
- Auto-Skills: specs revisadas para arquitetura PI-compatible (`<slug>/SKILL.md`), sem dependência de `pi-hermes-memory` ou escrita em `~/.pi/agent`.
- Learning Nudge: spec atualizada para detectar candidatos a skill sem escrever automaticamente.

### Adicionado
- Agent Comms: spec, design e tasks para comunicação entre agentes.
- Security Guard Rails: spec inicial com regras de segurança.

## [0.7.9] - 2026-05-18

### Corrigido
- Corrigido comando de rebuild do daemon em `AGENTS.md` para compilar `./cmd/aurelia/`, que contém o entrypoint real.

## [0.7.8] - 2026-05-18

### Adicionado
- Registro de receipts de atividade de memória em `memory_receipts.jsonl` para execuções de nudge e dream.
- `/memory status` agora mostra a última atividade de memória, incluindo fonte, status, itens aplicados, duração e custo quando disponíveis.

### Segurança
- Receipts armazenam apenas metadados sanitizados, sem transcripts, prompts, facts ou saída bruta do modelo.

## [0.7.7] - 2026-05-18

### Adicionado
- Comando `/memory status` para visualizar camadas de memória ativas, diretórios, arquivos Markdown e alvo de checkpoint.
- Comando `/memory checkpoint [nota]` para salvar `current_task.md` no melhor escopo disponível: project-private, topic ou global fallback.
- Matches naturais em português para status/checkpoint de memória.

### Segurança
- Checkpoints usam escrita atômica, diretórios `0700`, arquivos `0600` e proteção contra symlink escape.

## [0.7.6] - 2026-05-18

### Corrigido
- Lock file de instância agora usa permissão `0600` (owner-only) em vez de `0644`, reduzindo superfície de leitura por outros usuários do sistema.
- Removida função morta `isPersonasDirLexical` do código de produção (só usada em testes; substituída por helper em teste).

### Adicionado
- Testes de consolidação `applyMerge`: verificação de escrita de facts, atualização de `MEMORY.md`, remoção de source files, rejeição de symlink escape e permissões privadas em arquivos merged.

## [0.7.5] - 2026-05-18

### Corrigido
- `/cwd` voltou a aceitar diretórios de trabalho existentes mesmo sem marcadores de projeto como `.git`, `README.md` ou `go.mod`, preservando bloqueios para caminhos sensíveis.

## [0.7.4] - 2026-05-18

### Corrigido
- `/cwd` agora aceita `--group` e `--topic` para definir explicitamente se o projeto será persistido no grupo inteiro ou apenas no tópico atual.
- Ao definir `/cwd --group` dentro de um tópico, os caches de memória do grupo e do tópico atual são invalidados para refletir a nova herança.

## [0.7.3] - 2026-05-18

### Corrigido
- `/cwd` agora aceita caminhos com wrappers comuns de chat, como crases e aspas, e expande `~`/`~/...` antes da validação.
- Erros de `/cwd` agora incluem detalhe no log e na resposta do Telegram, facilitando diagnóstico em grupos e tópicos.

## [0.7.2] - 2026-05-18

### Corrigido
- Corrigido falso sucesso quando o Bridge retorna resultado vazio, evitando resposta "(sem resposta)" e contaminação da memória.
- Endurecido o sistema de nudge/dream para executar extração e consolidação de memória sem ferramentas de arquivo do PI SDK.
- Adicionado writer seguro em Go para memória, com validação de paths, bloqueio de `personas/`, proteção contra symlinks e sanitização de fatos/títulos.
- Adicionado rate limit por chat/thread para nudge, incluindo tentativas com erro ou JSON inválido.
- Memórias carregadas no prompt agora são marcadas como dados não confiáveis para reduzir risco de prompt injection persistente.

### Adicionado
- Testes de regressão para resposta vazia, parsing JSON de memória, path traversal, symlinks, sanitização, rate limit e consolidação segura.

## [0.7.1] - 2026-05-18

### Corrigido
- O lock do Dream agora preserva o timestamp da última consolidação entre conclusões (internal/dream/lock.go)
- As chaves do NudgeBuffer agora incluem chatID e threadID para evitar vazamento entre grupos (internal/session/nudge_buffer.go)
- A memória privada do projeto agora está isolada por conversa/thread, impedindo que anotações de um grupo/tópico do Telegram vazem para outra conversa vinculada ao mesmo repositório (internal/runtime/*, internal/pipeline/*, internal/telegram/bot_middleware.go)

### Adicionado
- Testes para preservação de timestamp do lock do Dream, isolamento do NudgeBuffer e memória de projeto escopo por conversa (internal/*_test.go)

## [v0.7.0] - 2026-05-17

### Added
- **Onboarding guardrail**: daemon now exits cleanly with instructions if run before `onboard` completes (`AppConfig.Onboarded()` check in `main.go`).
- **Telegram token validation**: onboarding wizard calls `getMe` API to verify bot tokens before saving config — catches invalid tokens immediately instead of failing at daemon startup.
- **Internationalization (i18n)**: new `internal/i18n/` package with Portuguese (pt-BR) default and English fallback. All user-facing Telegram messages now use the i18n bundle.
- **Linux systemd support**: `scripts/aurelia.service.tmpl` and `scripts/install-systemd.sh` for user-mode systemd installation. `Makefile` auto-detects OS (`install-service` works on both macOS and Linux).
- **Onboarding testability**: `validateToken` is overridable in tests to avoid real HTTP calls during onboarding unit tests.
- **Local models support**: Ollama provider added to onboarding wizard and configuration. README now includes a "Local Models" section with setup instructions for Ollama and OpenAI-compatible local inference servers.

### Changed
- `README.md` restructured with Prerequisites section, improved Quick Start flow, Linux service instructions, and Troubleshooting table.
- `internal/telegram/messages.go` migrated from hardcoded Portuguese constants to i18n-backed functions.
- **Provider rename**: "kilo" renamed to "opencode-go" throughout the codebase — provider ID, API key field, config migration, and onboarding UI all updated.
- **Documentation clarity**: README and onboarding wizard now explicitly state that the PI SDK installs automatically via npm on first run — no manual PI CLI installation required.
- **PI CLI isolation**: Aurelia now uses its own PI agent directory (`~/.aurelia/pi-agent/`) instead of sharing `~/.pi/agent/` with PI CLI. On first run, existing PI CLI auth/models config is automatically copied to the isolated directory. Credential conflicts between Aurelia and PI CLI are eliminated.

### Fixed
- **UX**: running daemon without onboarding produced cryptic Telegram API errors — now shows friendly step-by-step instructions.
- **UX**: invalid Telegram tokens were only discovered at runtime — now caught during onboarding wizard.
- **Reliability**: bridge setup now creates `~/.aurelia/pi-agent/` directory (instead of `~/.pi/agent/`) to ensure PI SDK has an isolated writable agent directory even when the user has never installed the PI CLI.

## [v0.6.9] - 2026-05-17

### Fixed
- **Security**: path traversal em `downloadTelegramFile` — sanitiza `filename` com `filepath.Base()` antes de `os.TempDir()`.
- **Crash**: panic não recuperado em `pipeline.processRun` goroutine — adiciona `recover()` com log.
- **Crash**: panic não recuperado em `orchestrator.ExecutePlan` worker goroutine — adiciona `recover()` que loga task ID e registra resultado falho.
- **Deadlock**: `cron.WithTx` sem `defer tx.Rollback()` — transação vazava conexão SQLite em panic, deadlockando o scheduler.
- **Hang**: `bridge.Stop()` esperava `<-done` sem timeout — adiciona 10s timeout antes de forçar kill.
- **Race**: `memoryCache.get()` validava mtimes fora do lock e retornava conteúdo stale se `invalidate()` deletasse a entrada no meio.
- **Leak**: erros de `worktree.Merge` e `worktree.Cleanup` eram descartados com `_` — agora logados explicitamente.
- **Data loss**: `dreamer.run()` zerava o turn counter no fim, perdendo turns que chegaram durante o dream — agora subtrai apenas os turns consumidos via CAS.
- **Logic**: `tryExecutePlan` retornava `OutcomeSuccess` sem chamar `afterSuccessfulTurn`, pulando dreamer update e memory invalidation.
- **Reliability**: `cron.scheduler.Start()` morria no primeiro erro do SQLite — agora loga e continua o loop.
- **Burst**: `computeNextRun` usava `now` (início do poll) em vez de `finishedAt` — jobs longos causavam reexecução imediata.
- **Resilience**: `agents.Load` abortava todo o registro no primeiro arquivo `.md` malformado — agora loga e pula o arquivo.
- **Thundering herd**: `getModels` tinha race no cache expiry — múltiplas goroutines batiam no bridge simultaneamente; agora o lock cobre toda a operação.
- **Silent errors**: `json.Unmarshal` no bridge, `os.Getwd` em `app.go` e `bot.go`, `os.UserHomeDir` em `app.go` — todos agora tratados ou logados.
- **Timeout**: `cmdCronCreate` usava `context.Background()` sem deadline para SQLite — agora usa 30s timeout.
- **Cleanup**: `worktree.Cleanup` não tentava deletar o branch se `git worktree remove` falhasse — agora tenta sempre.
- **Crash**: `onNotify` callbacks em `resilient_bridge.go` sem `recover()` — panic no output layer matava o daemon.

## [v0.6.8] - 2026-05-16

### Added
- `internal/telegram/cron_fast_parse.go` — regex parser for the common scheduling phrasings (`todo dia às Nh ...`, `toda <weekday> às Nh ...`, `amanhã às Nh ...`, `hoje às Nh ...`, `daqui N min ...`, `em N horas ...`). Bypasses the LLM round-trip in ~70% of cron creates — saves 1-3s and ~$0.001 per scheduled reminder.
- `BridgeCronRuntime` now injects scheduling instructions and global memory into the system prompt — cron-spawned agents can create follow-up jobs and have continuity across runs (parity with the Telegram pipeline).
- `BridgeCronRuntime.SetExePath()` so cron-injected CLI commands reference the real binary path.
- Album partial-success messages — when N of M photos fail to download or encode, the user gets a concrete `"⚠️ Consegui processar apenas X de Y imagens"` instead of silent log lines.
- `AppConfig.DiskScanEnabled` — opt-in flag for the disk-walking project auto-detection fallback.
- `collectPhotoAttachments` helper consolidating the album/single-photo download+encode loop.

### Changed
- `cmdCronCreate` tries `cronFastParse` before paying the LLM round-trip; falls through gracefully when the message doesn't match a supported pattern.
- `helpMessage` now documents cancel/supersede/status during processing and CWD inheritance between forum topics.
- `splitTelegramMarkdown` rune handling rewritten — converts to `[]rune` once and slices via rune index instead of re-decoding the tail per chunk (was O(n²) on long replies).
- `scanForProject` disk walk now gated by `DiskScanEnabled` (default false) — removes up to 3s of latency on the first message of a session. Project index and memory-file lookup still run.
- `sendProviderMenu` send arguments reordered so the inline keyboard markup is applied after send options (pre-existing fix in the working tree).

### Fixed
- N/A (no bug fixes in this release; all changes are quality-of-life improvements).

## [v0.6.7] - 2026-05-16

### Added
- `Makefile` com alvos `build`, `deploy` (atômico), `install-service`, `restart`, `stop`, `status`, `logs`
- `scripts/com.aurelia.agent.plist.tmpl` — template launchd com `KeepAlive` (auto-restart em crash) e `RunAtLoad` (start no login)
- `scripts/install-service.sh` — renderiza o plist e carrega o serviço (idempotente)
- `docs/OPERATIONS.md` — guia de deploy, recovery e troubleshooting do daemon
- `memoryCache.ttl` configurável (default 5s) para pular validação de mtime em chamadas rápidas
- `formatTokenCount()` — prefixa `~` somente quando o total é estimativa por turns

### Changed
- `ResilientBridge.validateChannel` agora valida só o primeiro evento e faz proxy live do restante — eventos `tool_use` voltam a chegar em tempo real ao `ProgressReporter` (antes ficavam buffered até o final da resposta)
- `progressReporter` aplica throttle de 1.5s entre edits para evitar `FloodError`
- `sendTextWithSender` / `sendTextReplyWithSender` pulam `sleep` de 200ms após o último chunk
- `routeAgent` pula classificação LLM quando há <2 agents ou texto curto (<10 chars); timeout reduzido de 15s → 5s
- TLC do orchestrator só é incluído no system prompt quando há `cwd` setado (economiza ~3-5k tokens em chats casuais)
- `MatchCommand` agora normaliza acentos — comandos funcionam com ou sem diacríticos
- `formatResetSummary` e `formatModelResetSummary` omitem `~` quando contagem de tokens é real
- `cmdCronCancel` distingue "ID não informado" de "ID não encontrado"

### Fixed
- `BotController` não cria `nudgeBuffer` redundante — ownership único no `pipeline.Service`

## [v0.6.6] - 2026-05-15

### Added
- Ack imediato 👀 com confirmação ✅ em todas as mensagens (middleware + pipeline)
- `/status` registrado como comando Telegram, com informações humanizadas (modelo, CWD, sessão, trabalho ativo, fila)
- Progress reporter com timer (⏱️ Xm Xs) e limite ampliado para 8 ferramentas
- Supressão de edits duplicados no progress reporter
- `/new` cancela processamento ativo (`pipeline.Cancel`) e mostra resumo da sessão resetada
- Active work status + queue info no `/status` via `pipeline.WorkStatus()`
- `pipeline.Service.Cancel()` e `runSupervisor.cancel()` para interromper execução ativa
- Mensagens de erro do bridge com dicas acionáveis (conexão, cooldown, timeout, retry)
- `FailureTracker.cooldownRemaining()` para mostrar tempo restante nas mensagens de cooldown
- Help com exemplos de comandos naturais
- Documentos não suportados com dica de conversão
- Fila transparente: mensagens incluem contexto do trabalho atual (`queueAdmittedMessage`, `queueStatusMessage`)
- `formatModelResetSummary()` com escopo (tópico/privado) e resumo de mensagens
- `humanBytes()` — bytes formatados como MB/KB/B legíveis
- Filtragem de formatos de imagem exóticos (`isSupportedImageMIME`)

### Changed
- `/model` agora limpa apenas a sessão do thread atual (`ClearSession(chatID, threadID)`, não `ClearAll`)
- `cmdSessionReset` refatorado para usar `resetCurrentSession` com captura de uso antes de limpar
- `cmdStatus` refatorado: remove session ID e warm/cold, adiciona CWD, resumo de sessão, emojis
- `progressReporter.startTime` inicializado no construtor
- `unsupportedDocumentMessage` atualizada com dica de conversão
- Mensagens de bridge error movidas para constantes centralizadas com dicas
- `imageTooLargeError.UserMessage()` usa `humanBytes()`

### Fixed
- Progress reporter não edita mensagem quando o texto não mudou (evita erro "message is not modified")
- `handleModelCommand` e handlers de comando usam `SendTextWithThread`/`SendErrorWithThread` (thread-aware)
- `handleCronCommand` usa `SendErrorWithThread` e `SendTextWithThread`
- `ReactToMessage` protege contra `bot` nulo
- `ackMiddleware` não reage a callbacks (só mensagens de texto/mídia)

### Validation
- **PI Resilience**: validation.md atualizado com evidências de todos os critérios (75 testes passando, circuit breaker, retry, fallback, error classification)
- **Agent Tools Fix**: validation.md atualizado, bundle rebuildado e instalado em `~/.aurelia/bridge/bundle.js`
- **UX Polish**: validation.md atualizado com status de cada user story e edge case

## [v0.6.5] - 2026-05-15

### Fixed
- `disallowed_tools` in agent frontmatter is now respected and filters tools sent to the PI SDK.
- Empty tool restriction (e.g. denylist removing all allowed tools) now returns `[]` instead of falling back to all default PI SDK tools.

### Added
- `Agent.IsReadOnly()` computes effective tool set considering both `allowed_tools` and `disallowed_tools`.
- Validation of unknown tool names in agent YAML frontmatter logs a warning instead of silently ignoring.
- `DisallowedTools` propagated through the full pipeline: pipeline, cron, orchestrator, and Telegram summaries.

## [v0.6.4] - 2026-05-15

### Added
- Run supervisor per chat/thread to serialize active Telegram agent work while allowing independent topics to run in parallel.
- Concurrent message handling for cancel, supersede/correction, status, and queued follow-up intents.
- Bridge cancel command for best-effort interruption of active PI SDK requests.

### Fixed
- Context cancellation and timeouts no longer look like bridge process death or trigger retry loops.
- Bridge pending requests are cleaned up when callers cancel.

## [v0.6.3] - 2026-05-14

### Refactor
- Extracted the LLM/message pipeline into `internal/pipeline.Service`, moving prompt building, project detection, memory cache, bridge execution, and event handling out of `internal/telegram`.
- Kept `BotController` focused on Telegram bootstrap, commands, and I/O through a `pipeline.Output` adapter.

### Changed
- Moved pipeline-focused tests for memory cache, prompt building, and project detection into `internal/pipeline`.
- Marked the optimization plan as fully complete after T14.

## [v0.6.2] - 2026-05-14

### Fixed
- Bridge first-run setup now embeds the TypeScript source, writes `index.ts`, installs `esbuild`, and builds `bundle.js` without requiring versioned JS bundles.
- Removed versioned bridge bundles from git while preserving runtime build support.
- Avoided nil-agent panics when the agent registry fails to load.
- Session GC now runs in production, uses configurable `session_ttl_hours`, and expires orphan CWD entries.
- Memory prompt injection now enforces the total character cap, including the first memory layer.
- Image uploads now honor configured `max_image_bytes` and return a clear user-facing error when oversized.
- Project detection fallback now respects cancellation and schedules debounced index rebuilds on misses.
- Bridge terminal events are preserved under backpressure so slow consumers do not turn dropped `result`/`error` events into false process deaths.

### Added
- Regression tests for bridge setup metadata, memory prompt cap, image size rejection, and orphan CWD GC.

## [v0.6.1] - 2026-05-14

### Added
- Memory cache by mtime — avoids redundant disk reads on every turn
- Project index for fast project lookup with background rebuild
- Album TTL GC — orphan albums cleaned up after 5 minutes
- Async album flush — handler returns immediately, no 900ms blocking
- Event drop logging + counter in bridge readLoop
- Structured logging (log/slog) with configurable level and format
- Image size limit (10 MB default) with validation
- Model list cache with 5-minute TTL
- ChatSender adapter — removes GetBot() leak
- Tests for album GC, memory cache, frontmatter extraction, dropped events

### Changed
- Whitelist lookup from O(n) slice to O(1) map
- SQLite DSN with busy_timeout=5000, synchronous=NORMAL, foreign_keys=ON
- Bridge readLoop: bufio.Scanner → bufio.Reader (no 1MB cap)
- Separated real tokens from estimated tokens in Tracker
- Session GC — periodic cleanup of stale entries
- Split input_pipeline.go (1138→5 files)
- Bundle.js removed from git — built from TS source on first use
- parseCronCreateResponse uses regex instead of manual fence stripping
- handleCwdCommand no longer triggers LLM classify
- deps.Check returns errors instead of log.Fatalf
- Normalized provider keys cached at startup

### Fixed
- Temp photo files now cleaned up after upload
- Bridge process.Kill checks ProcessState before killing
- SetOnDeath callback dispatched in goroutine
- Slice copy in bridgeFailureTracker to avoid backing array leak
- ResolveJobID rejects prefixes with % or _
- Silent event drops now logged + countable

## [v0.5.1] - 2026-05-14

### Changed
- Forum topic memory is now scoped per chat:
  `topics/chat_<chatID>/thread_<threadID>/` instead of `topics/<threadID>/`.
  Telegram threadIDs are only unique within a chat, so two groups with the
  same numeric topic id used to share memory. Existing memory under
  `topics/<id>/` will need to be moved manually (or left to be re-built by
  future nudges).
- `/cwd` display, memory layers, and Telegram instructions all resolve the
  effective working directory the same way the bridge does
  (`agent.Cwd > topic > group > none`). Previously the display claimed agent
  CWD was highest priority but only the bridge cwd and project-docs section
  honored it.
- `/model` (and its callback) now re-export the provider API key env vars
  after persisting, so the new provider's key is in place for the next query.
- Atomic write for `~/.aurelia/config/app.json` when `/model` changes the
  default — prevents truncated configs and lost API keys on mid-write crash.
- Bounded session-lookup cache in the bridge (256 entries, LRU-ish), so a
  long-running daemon does not grow it forever.

### Fixed
- `extractModelName` no longer falls back to the last word of the message.
  Messages misclassified as `CmdSetModel` (e.g. "olá tudo bem amigo") used to
  attempt model changes to garbage strings.
- `extractModelName` correctly handles leading whitespace; the prefix offset
  was computed off the trimmed text but slicing happened on the original.

### Refactor
- `NewBridgeCronRuntime` takes `defaultProvider string` instead of a variadic
  for a single optional argument; `startChatActionLoop` does the same for
  `threadID int`.
- `setupBridge` collapsed to a single `os.Stat` and a single return; the
  10 KB guard threshold now has a named constant and a comment explaining
  the reasoning (bundle is ~12 MB, anything tiny means a failed esbuild run).
- Dropped the unused `replyToID` parameter from `SendTextReply` /
  `SendTextReplyWithThread`.
- `gofmt` import order in `internal/dream/nudge.go`.

## [v0.5.0] - 2026-05-14

### Security
- **BREAKING:** Group chats now require both the group ID in
  `telegram_allowed_group_ids` AND the sender's user ID in
  `telegram_allowed_user_ids`. Previously, any member of a whitelisted group
  could interact with the bot regardless of the user allowlist. Existing
  groups will need user IDs added to keep working.

### Changed
- Removed bridge options that have no analogue in the PI SDK:
  `max_turns`, `permission_mode`, `mcp_servers`, `agents`, `disabled_tools`.
  These were silently ignored since v0.4.0; removing them prevents confusion
  in future development.
- `allowed_tools` no longer auto-includes `web_search`. Agents that need
  web search must list it explicitly in their markdown.

### Fixed
- Bridge no longer leaks PI sessions when `session.prompt()` throws
- Bundle.js is now written atomically (temp + rename) to avoid corruption
  during writes; startup fast-paths size check before reading 12 MB
- `setupBridge` falls back to tsx when bundle.js exists but is truncated
- Instance lock cleanup errors are logged instead of swallowed
- Session ID slicing in logs is now bounds-checked (was unsafe for tests)
- Bridge `duration_ms` reports real elapsed time (was hardcoded 0)

## [v0.4.2] - 2026-05-13

### Added
- Vision fallback model: configure `vision_model`/`vision_provider` in app.json
  for automatic model switching when images are present in the input
- Vision fallback step in onboarding TUI and prompt mode
- Bridge protocol for image attachments with proper PI AI SDK ImageContent format

### Fixed
- Bridge image format: was sending images in Anthropic API format
  (`source.media_type`/`source.data`), now uses PI AI SDK ImageContent
  (`data`/`mimeType`) — fixing silent vision API failures
- Removed invalid `deliverAs: "nextTurn"` from `sendUserMessage` call

## [v0.4.1] - 2026-04-06

### Added
- Runtime dependency checker: validates Node.js, npm, git, gh before startup
- Dependency checklist as Step 1 in onboarding TUI (blocks if required deps missing)
- Boot-time check with clear fatal/warning messages for missing dependencies
- Plain-text dependency check in non-TUI onboarding fallback

## [v0.4.0] - 2026-04-06

### Added
- Live model catalog for OpenRouter provider
- Periodic nudge review replacing per-turn extraction in dream system

### Fixed
- OpenRouter connectivity issues
- Nudge reliability for weak models
- Flush nudge state on session reset
- Windows bootstrap path resolution

## [v0.3.0] - 2026-03-27

### Added
- Project-scoped 3-layer memory system
- Persistent memory system with project context for Telegram
- Memory extraction and consolidation in dream system
- Feature specs for project memory and learning nudge

## [v0.2.0] - 2026-03-26

### Added
- Automatic bridge recovery with retry, session invalidation, and backoff
- LLM-generated bootstrap personas for Telegram
- Command layer for local system commands in Telegram
- Session token tracking with auto-reset and /usage command
- Cost, token, and session ID tracking per cron execution

### Changed
- Migrated documentation to .specs/ structure with CLAUDE.md
- Removed memory system from cron, added ResolveJobID to service
- Replaced magic numbers with named constants
- Broke bootstrapApp into focused sub-functions
- Encapsulated album buffering in dedicated struct
- Injected session.Store and session.Tracker via constructor
- Extracted LLM classification to registry with ClassifyFunc callback
- Extracted Telegram delivery to dedicated cron type
- Extracted session store and tracker from telegram package
- Removed dead code stubs from Telegram package
- Removed dead MemoryWindowSize config field

### Fixed
- Telegram typing indicator errors now logged instead of discarded
- Deactivate cron jobs with unknown schedule type
- Telegram reactions, chat index, and executable error handling
- Atomic transaction for RecordExecution and UpdateJob in cron
- Log swallowed Send and Close errors instead of discarding
- Normalize agent names to lowercase for case-insensitive routing
- Prevent bridge Stop() hang when called before Start()
- Prevent concurrent execution of same cron job

## [v0.1.0] - 2026-03-21

### Added
- TypeScript Bridge wrapping Claude Agent SDK
- Go client for the TypeScript Bridge
- Agent registry with markdown definitions
- Semantic memory with embeddings and cosine similarity
- Cron scheduler adapted to use Bridge for job execution
- Telegram bot with Bridge-based input pipeline
- End-to-end wiring tests
- App bootstrap wiring all components
- Long-lived Bridge process for session continuity
- Session resume for conversation continuity in Telegram
- Active session state tracking per chat
- Continue and agents options in Bridge request protocol
- Pre-fetch cloud MCPs from claude.ai for SDK queries
- Auto-update bundle.js on version mismatch
- LLM-based smart routing for agent classification
- Photo download and analysis via Bridge in Telegram
- Tool progress display during Bridge execution
- /cwd and /reset commands for session control
- Support for Anthropic subscription auth (Max plan)
- Full cron expression support via robfig/cron
- SDK-native agent delegation from Telegram
- BuildSDKAgents to convert registry to SDK format

### Changed
- Simplified persona loader, removed retrieval and memory dependencies
- Updated config schema for providers and embedding config
- Removed pkg/llm, inlined provider catalog for onboarding
- Removed replaced modules (agent, tools, llm, mcp, skill, observability, memory)
- Removed Voyage and Gemini embedders, kept local only

### Fixed
- Bridge SDK cli.js path resolution, always use ~/.aurelia/bridge
- Bridge tool_use emission, permissions flag, and SDK option mapping
- Telegram bypassPermissions for unattended execution
- Timeouts for bridge and memory operations
- Disabled session resume until Bridge became long-lived
- Bridge setup ensured on startup
