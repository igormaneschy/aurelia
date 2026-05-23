# Session Lifecycle Manager — Design

**Spec:** `.specs/features/session-lifecycle-manager/spec.md`  
**Status:** Draft

---

## Architecture Overview

A solução adiciona uma camada de lifecycle entre o pipeline Go e o bridge TypeScript.

```
Telegram message
  ↓
pipeline.Process
  ↓
buildBridgeRequest
  ↓
SessionLifecycleManager.Evaluate
  ├─ healthy  → continue/current behavior
  ├─ large    → bridge compact → continue
  ├─ cold     → resume without continue
  ├─ suspect  → cold resume or rotate
  └─ dangerous→ rotate to summarized session
  ↓
bridge.Execute
  ↓
PI SDK session
  ├─ events: agent/turn/tool/compaction/retry
  ├─ result
  └─ error/process death/timeout
  ↓
Lifecycle outcome update
```

## SDK Capabilities Used

| SDK feature | Use |
|---|---|
| `session.compact(customInstructions)` | compaction proativa antes do prompt |
| `session.getSessionStats()` | health check por tokens/mensagens/custo/context usage |
| `session.subscribe()` events | propagar `compaction_start/end`, `agent_start/end`, `turn_start/end`, retry |
| `SessionManager.open(path)` | ler/abrir sessão persistida |
| `SessionManager.create(cwd)` | criar nova sessão ao rotacionar |
| session JSONL tree | preservar sessão antiga e criar sessão nova resumida |
| `sendCustomMessage` or first prompt context | injetar resumo na sessão nova |

## New Components

### 1. `internal/session/lifecycle.go`

Responsável por tipos e decisão pura.

```go
type HealthState string
const (
    HealthHealthy HealthState = "healthy"
    HealthLarge HealthState = "large"
    HealthCold HealthState = "cold"
    HealthSuspect HealthState = "suspect"
    HealthDangerous HealthState = "dangerous"
)

type LifecycleAction string
const (
    ActionContinue LifecycleAction = "continue"
    ActionColdResume LifecycleAction = "cold_resume"
    ActionCompact LifecycleAction = "compact"
    ActionRotate LifecycleAction = "rotate"
)

type HealthSignals struct {
    Active bool
    InputTokens int
    OutputTokens int
    TotalMessages int
    AssistantMessages int
    ToolResults int
    RecentTimeouts int
    RecentEmptyResults int
    RecentProcessDeaths int
    LastError string
    LastSeen time.Time
}

type Decision struct {
    State HealthState
    Action LifecycleAction
    Reason string
}
```

This package should stay pure and unit-testable.

### 2. Bridge commands/events

Add commands to `bridge/index.ts`:

| Command | Purpose |
|---|---|
| `get-session-stats` | open/resolve session and return stats without running prompt |
| `compact-session` | compact current/resumed session, emit events, return stats/result |
| `rotate-session` | create new session seeded with summary/context |

Potential request options reuse current `chat_id/thread_id/user_id/resume/cwd/provider/model`.

New events emitted during normal query:

```json
{ "event": "compaction_start", "request_id": "...", "reason": "threshold" }
{ "event": "compaction_end", "request_id": "...", "tokens_before": 240000, "success": true }
{ "event": "agent_start", "request_id": "..." }
{ "event": "agent_end", "request_id": "...", "will_retry": false }
{ "event": "turn_start", "request_id": "..." }
{ "event": "turn_end", "request_id": "..." }
{ "event": "auto_retry_start", "request_id": "...", "attempt": 1 }
{ "event": "auto_retry_end", "request_id": "...", "success": true }
```

The Go side can treat these as activity to reset idle timeout and as observability events.

### 3. Pipeline integration

Add lifecycle evaluation in `buildBridgeRequest` or immediately before `executeAsync`.

Recommended flow:

1. Build base request including resume/cwd/security.
2. Ask lifecycle manager for current decision.
3. Apply action:
   - `continue`: keep `Continue=true` if store active.
   - `cold_resume`: force `Continue=false` but keep `Resume`.
   - `compact`: call bridge `compact-session`, then continue/cold resume depending result.
   - `rotate`: call bridge `rotate-session`, update store with new session file, force `Continue=false` for first prompt.
4. Record decision to runlog.
5. Execute query.

Avoid putting network/bridge calls inside pure decision code.

### 4. Store extensions

`session.Store` currently stores `sessionFile`, `active`, `lastSeen`.

Add optional metadata either in the existing snapshot or a sidecar map:

```go
type entry struct {
    sessionFile string
    active bool
    lastSeen time.Time
    lastFailure string
    suspectCount int
    lastFailureAt time.Time
    lastLifecycleAction string
}
```

If changing snapshot, maintain backward compatibility: missing fields load as zero values.

### 5. Config

Add to `internal/config`:

```go
type SessionLifecycleConfig struct {
    Enabled bool `json:"enabled"`
    CompactAfterInputTokens int `json:"compact_after_input_tokens"`
    RotateAfterInputTokens int `json:"rotate_after_input_tokens"`
    MaxEmptyResultsBeforeRotate int `json:"max_empty_results_before_rotate"`
    MaxProcessDeathsBeforeRotate int `json:"max_process_deaths_before_rotate"`
    IdleTimeoutMinutes int `json:"idle_timeout_minutes"`
    KeepRecentTokens int `json:"keep_recent_tokens"`
    ReserveTokens int `json:"reserve_tokens"`
}
```

Defaults:

- enabled: true
- compact after input tokens: 120000
- rotate after input tokens: 250000
- empty results before rotate: 1
- process deaths before rotate: 1
- idle timeout: 20 minutes
- keep recent tokens: 8000
- reserve tokens: 32768

Note: also pass compaction settings to the bridge `SettingsManager` via override instead of relying only on `~/.aurelia/pi-agent/settings.json`.

## Data Flow Details

### Healthy continue

```
Store active=true + stats below threshold
  → req.Options.Resume=sessionFile
  → req.Options.Continue=true
```

### Cold resume

```
Store active=false OR recent timeout
  → req.Options.Resume=sessionFile
  → req.Options.Continue=false
  → inject continuity block if needed
```

### Compact before query

```
stats.inputTokens > compact threshold
  → bridge compact-session
  → events compaction_start/end
  → if success: normal query
  → if fail: mark suspect, cold resume or rotate
```

### Rotate session

```
stats.inputTokens > rotate threshold OR repeated suspect
  → generate structured summary from session/continuity/runlog
  → bridge create new session with summary custom message or seed prompt
  → store new sessionFile active=true/cold=false after system event
  → old session remains on disk
```

## Summary Format for Rotation

Use the PI compaction docs format, aligned with Aurelia continuity:

```md
## Goal

## Current State

## Completed Work

## In Progress

## Key Decisions

## Files Read
<read-files>
...
</read-files>

## Files Modified
<modified-files>
...
</modified-files>

## Commands / Validations

## Open Risks

## Next Actions
```

The summary is untrusted context, not instruction source. Wrap in delimiters when inserted:

```md
<previous_session_summary_untrusted>
...
</previous_session_summary_untrusted>
```

## Idle Timeout Changes

Current Go idle timeout kills the run after no bridge events. With new bridge lifecycle events:

- `compaction_*`, `agent_*`, `turn_*`, `auto_retry_*`, `tool_*`, `assistant` all reset idle timer.
- Timeout duration should come from config.
- Message to user should include origin.

## Error/Lifecycle Outcome Updates

Update these paths:

| Path | Required lifecycle update |
|---|---|
| `handleContextOutcome` timeout | mark cold/suspect |
| `handleErrorEvent` timeout | mark cold/suspect |
| `handleEmptyResult` with work | mark cold/suspect |
| process death before retry | mark suspect/process death count |
| retry success after process death | clear or reduce suspect state only if result succeeds |
| success | clear `reset_reason`, `lastFailure`, maybe suspect count |

## Observability

Runlog events:

- `session_health_checked`
- `session_lifecycle_decision`
- `session_compaction_started`
- `session_compaction_completed`
- `session_rotation_started`
- `session_rotation_completed`
- `session_marked_cold`

Log message fields:

- chat/thread/user
- session basename only
- state/action/reason
- input/output tokens
- suspect counters

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Compaction itself hangs | compaction command has timeout + emits events; Go idle timeout watches it |
| Summary loses critical context | structured summary + continuity/runlog fallback + old session retained |
| Provider token stats unreliable | combine stats with local message counts and failure signals |
| Rotating too often loses continuity | thresholds configurable, record reason, keep old sessions |
| Race with active runs | lifecycle action evaluated once per run before query; activeSessions prevents concurrent same user run |
| Custom provider has huge contextWindow configured | use Aurelia thresholds independent of provider contextWindow |

## Deploy / Restart Resume Compatibility

Existing deploy flow already creates cold sessions indirectly:

- `make deploy` uses `launchctl kickstart -k`, sending SIGTERM.
- `main.waitForShutdownSignal` triggers graceful shutdown.
- `app.close` calls `bridge.Stop()`; because this is intentional, `Bridge.SetOnDeath` is not invoked.
- `session.Store` has already persisted session file paths in `sessions.json`.
- On next startup, `NewPersistentStore` reloads all sessions with `active=false`.
- Startup calls `NotifyRecentInterruptedSessions(InterruptedSessionMaxAge)` after 2s.
- User can send `continuar`; Telegram converts that to `interruptedResumePrompt()` and pipeline resumes cold (`Resume` set, `Continue=false`).

Lifecycle integration should treat deploy/restart as first-class `cold` state, not as process death. That means:

1. Preserve `NewPersistentStore` behavior: restored sessions are always inactive.
2. Do not auto-rotate just because a deploy happened; first cold resume should be allowed.
3. Consider marking continuity cold on graceful shutdown or startup reconciliation so prompt context has reset reason `daemon restarted/deployed`.
4. Extend notification window from hardcoded 1 minute to config.
5. Reconcile `run_journal` rows left `running` before startup, marking them interrupted/cold with deploy reason.
6. Keep explicit `continuar` behavior, but let normal follow-up instructions use the same cold-session safeguards.

## Migration / Compatibility

- Existing `sessions.json` remains valid.
- New metadata fields are optional.
- Existing deploy resume remains valid and becomes a subset of lifecycle cold resume.
- If bridge does not support new commands, lifecycle manager should fail open to current behavior but mark warning in logs.
- Feature can be disabled via config for emergency rollback.

## Validation Strategy

- Unit tests for pure lifecycle decisions.
- Bridge tests for `get-session-stats`, `compact-session`, event emission.
- Pipeline tests that verify request options after failures.
- Integration test with fake bridge generating compaction events.
- Live validation on daemon with deliberately long session.
