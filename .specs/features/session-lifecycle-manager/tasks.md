# Session Lifecycle Manager — Tasks

**Spec:** `.specs/features/session-lifecycle-manager/spec.md`
**Design:** `.specs/features/session-lifecycle-manager/design.md`
**Status:** Draft

---

## Execution Plan

```
Phase 0: Baseline / tests
  T0

Phase 1: Pure lifecycle foundation
  T1 ─ T2 ─ T3

Phase 2: Bridge SDK capabilities
  T4 ─ T5 ─ T6

Phase 3: Pipeline integration
  T7 ─ T8 ─ T9

Phase 4: Rotation and UX
  T10 ─ T11 ─ T12

Phase 5: validation
  T13 ─ T14
```

---

## T0: Baseline log fixture and regression tests

**What:** Capture current failure modes as test fixtures where practical.
**Where:** `internal/pipeline/*_test.go`, `internal/session/*_test.go`
**Depends on:** None

**Implementation details:**
- Add tests for timeout via `handleErrorEvent` ensuring current gap is visible before fix.
- Add tests for `empty result after work` marking cold.
- Add tests for process death path marking suspect once metadata exists.

**Done when:**
- [ ] Tests describe current desired behavior, even if initially failing in feature branch
- [ ] Test names reference the production symptoms: idle timeout, bridge query timeout, empty result, process death

**Verify:**
```bash
go test ./internal/pipeline ./internal/session -run 'Session|Timeout|Empty|ProcessDeath' -v
```

---

## T1: Config schema for session lifecycle

**What:** Add `session_lifecycle` config with defaults and validation.
**Where:** `internal/config/config.go`, `internal/config/config_editable.go`, tests
**Depends on:** None

**Implementation details:**
- Add `SessionLifecycleConfig` struct.
- Add defaults from design.
- Validate positive values and relationship: rotate threshold > compact threshold.
- Include in editable config comparison if needed.

**Done when:**
- [ ] Defaults load when config omits section
- [ ] Invalid thresholds return clear error with offending value
- [ ] Tests cover defaulting and validation

**Verify:**
```bash
go test ./internal/config -run SessionLifecycle -v
```

---

## T2: Pure lifecycle decision engine

**What:** Implement health states and action decision logic.
**Where:** `internal/session/lifecycle.go`, `internal/session/lifecycle_test.go`
**Depends on:** T1

**Implementation details:**
- Define `HealthSignals`, `HealthState`, `LifecycleAction`, `Decision`.
- Implement `EvaluateLifecycle(signals, config) Decision`.
- Keep package pure: no bridge, no Telegram, no DB.

**Done when:**
- [ ] healthy active session → continue
- [ ] inactive session → cold resume
- [ ] input tokens > compact threshold → compact
- [ ] input tokens > rotate threshold → rotate
- [ ] timeout/empty/process death signal → cold/suspect
- [ ] repeated suspect → rotate

**Verify:**
```bash
go test ./internal/session -run Lifecycle -v
```

---

## T3: Session store suspect metadata

**What:** Extend session store with failure/suspect metadata and persistence.
**Where:** `internal/session/store.go`, `internal/session/persistence.go`, tests
**Depends on:** T2

**Implementation details:**
- Add metadata fields to `entry`.
- Add methods:
  - `MarkFailure(chatID, threadID, userID, reason string)`
  - `MarkProcessDeath(...)`
  - `MarkEmptyResult(...)`
  - `ClearFailureState(...)`
  - `HealthSignals(...)`
- Persist fields in `sessions.json`; load old snapshots safely.

**Done when:**
- [ ] Old snapshots without metadata load
- [ ] Failure metadata survives restart
- [ ] Success clears/reset relevant failure state
- [ ] No user isolation regression

**Verify:**
```bash
go test ./internal/session -run 'Persistence|Failure|Health' -v
```

---

## T4: Bridge stats command

**What:** Add `get-session-stats` command using PI SDK stats/session manager.
**Where:** `bridge/index.ts`, `internal/bridge/protocol.go`, bridge tests
**Depends on:** None

**Implementation details:**
- Resolve session with existing `resolveSessionManager(opts)`.
- Create or open PI session enough to call `session.getSessionStats()`.
- Return JSON content with stats and session file/id.
- Dispose temporary session when not retained in `chatSessions`.

**Done when:**
- [ ] Command returns stats for existing session file
- [ ] Missing session returns clear error or empty stats without panic
- [ ] Go protocol has struct for stats response

**Verify:**
```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
go test ./internal/bridge -run Stats -v
```

---

## T5: Bridge compaction command and events

**What:** Add `compact-session` command and propagate compaction events during query.
**Where:** `bridge/index.ts`, `internal/bridge/events.go`, pipeline event handling
**Depends on:** T4

**Implementation details:**
- Command opens/resumes session and calls `session.compact(customInstructions)`.
- Subscribe to SDK `compaction_start/end` events and emit NDJSON.
- During normal query, forward SDK compaction events too.
- Include `tokens_before`, `success`, `error` where available.

**Done when:**
- [ ] Bridge emits `compaction_start` and `compaction_end`
- [ ] Go parses events without treating them as unknown/no-op only
- [ ] Events reset idle timer by passing through event stream
- [ ] Failure emits terminal error or result with success=false consistently

**Verify:**
```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
go test ./internal/bridge ./internal/pipeline -run Compaction -v
```

---

## T6: Bridge lifecycle event forwarding

**What:** Forward `agent_start/end`, `turn_start/end`, and `auto_retry_start/end`.
**Where:** `bridge/index.ts`, `internal/bridge/events.go`, `internal/pipeline/pipeline.go`
**Depends on:** T5

**Implementation details:**
- Map SDK events to bridge `Event.Type` strings.
- Do not expose sensitive content.
- Pipeline records observability events and treats them as activity.

**Done when:**
- [ ] Events appear in test stream
- [ ] Unknown event spam is avoided
- [ ] Idle timeout wrapper resets timer for these events

**Verify:**
```bash
go test ./internal/pipeline -run 'Idle|LifecycleEvent' -v
```

---

## T7: Apply lifecycle decision before query

**What:** Integrate decision engine into request building/execution.
**Where:** `internal/pipeline/pipeline.go`, maybe new `internal/pipeline/session_lifecycle.go`
**Depends on:** T1-T6

**Implementation details:**
- Gather signals from store metadata + optional bridge stats.
- Decide action.
- Force `Continue=false` for cold/suspect.
- Trigger compact command for `ActionCompact`.
- Record runlog event.

**Done when:**
- [ ] Healthy path unchanged
- [ ] Cold/suspect path never sets `Continue=true`
- [ ] Compact path runs before query and records decision
- [ ] If lifecycle disabled, old behavior is preserved

**Verify:**
```bash
go test ./internal/pipeline -run Lifecycle -v
```

---

## T8: Mark failures cold/suspect in all outcome paths

**What:** Ensure all failure outcomes update session lifecycle metadata.
**Where:** `internal/pipeline/pipeline.go`
**Depends on:** T3, T7

**Implementation details:**
- `handleContextOutcome`: MarkFailure timeout.
- `handleErrorEvent`: MarkFailure for timeout/provider errors; deactivate session when timed_out.
- `handleEmptyResult`: MarkEmptyResult.
- process death retry path: MarkProcessDeath.
- success path: ClearFailureState and clear continuity reset reason.

**Done when:**
- [ ] Timeout from context and bridge event both mark cold
- [ ] Empty result marks cold/suspect
- [ ] Process death increments suspect metadata
- [ ] Success clears stale reset reason/failure state

**Verify:**
```bash
go test ./internal/pipeline -run 'Timeout|EmptyResult|ProcessDeath|Continuity' -v
```

---

## T9: Configurable idle timeout and UX by origin

**What:** Move idle timeout to config and improve messages.
**Where:** `internal/pipeline/pipeline.go`, config tests
**Depends on:** T1, T6

**Implementation details:**
- Replace constant-only `idleBridgeTimeout` usage with service config.
- Keep sane default.
- User message includes origin and suggests continuation/rotation where relevant.

**Done when:**
- [ ] Config can set idle timeout
- [ ] Tests verify default and override
- [ ] Messages differ for idle/provider/max/query timeout

**Verify:**
```bash
go test ./internal/pipeline ./internal/config -run 'IdleTimeout|TimeoutMessage' -v
```

---

## T10: Smart rotation command

**What:** Implement bridge-side session rotation.
**Where:** `bridge/index.ts`, protocol/tests
**Depends on:** T4-T5

**Implementation details:**
- Generate summary using SDK compaction or custom prompt based on current session and continuity snapshot.
- Create new persistent session with same cwd/model/tools/security.
- Seed summary as custom/untrusted context.
- Emit system/result with new session file.

**Done when:**
- [ ] Dangerous session creates new session file
- [ ] Old file remains unchanged
- [ ] New session contains or receives summary context
- [ ] Store updates to new session file after rotation

**Verify:**
```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
go test ./internal/bridge ./internal/pipeline -run Rotate -v
```

---

## T11: Telegram UX and status/debug

**What:** Add user-facing notices and optional status/debug output.
**Where:** `internal/telegram`, `internal/pipeline`
**Depends on:** T7-T10

**Implementation details:**
- Send short notices for compaction and rotation.
- Extend status/debug without exposing full session paths.
- Keep messages concise.

**Done when:**
- [ ] Compaction notice appears once per action
- [ ] Rotation notice explains safe continuation
- [ ] Status avoids leaking `session_file`

**Verify:**
```bash
go test ./internal/telegram ./internal/pipeline -run 'Status|Lifecycle|Compaction|Rotation' -v
```

---

## T12: Deploy/restart reconciliation

**What:** Preserve and improve existing post-deploy cold resume behavior.
**Where:** `cmd/aurelia/app.go`, `internal/telegram/resume_notice.go`, runlog/continuity stores if needed
**Depends on:** T3, T8

**Implementation details:**
- Keep `NewPersistentStore` loading sessions as inactive.
- Make interrupted resume notice window configurable via session lifecycle config.
- On startup, optionally mark continuity rows with active sessions as cold with reason `daemon restarted/deployed` if they were not already cold.
- Reconcile stale `run_journal.status='running'` rows from before process start as interrupted/canceled/timed_out with reset reason.
- Ensure `continuar` still uses cold resume and never sets `Continue=true`.

**Done when:**
- [ ] Existing `continuar` tests still pass
- [ ] Restored sessions are cold after persistent load
- [ ] Notice window is configurable
- [ ] Continuity/reset reason is consistent after deploy
- [ ] Stale running runs are not left indefinitely as running

**Verify:**
```bash
go test ./cmd/aurelia ./internal/telegram ./internal/session ./internal/pipeline -run 'Resume|Restart|Deploy|Cold|Running' -v
```

---

## T13: Documentation and operator guidance

**What:** Document lifecycle config, operational diagnosis, deploy resume behavior, and rollback.
**Where:** `CHANGELOG.md` later on release, `.specs/features/session-lifecycle-manager/validation.md`, maybe README/operator docs
**Depends on:** T1-T12

**Done when:**
- [ ] Config examples documented
- [ ] Rollback: `session_lifecycle.enabled=false`
- [ ] Existing deploy-resume behavior documented
- [ ] Known limitations documented

---

## T14: Full validation

**What:** Run repository validation.
**Depends on:** all implementation tasks

**Verify:**
```bash
go test ./... -short
go build ./...
go vet ./...
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
go test ./... -short
```

---

## T15: Live daemon validation

**What:** Deploy and validate with Telegram.
**Depends on:** T14

**Steps:**
1. Commit feature branch.
2. `make deploy`.
3. Send normal short message; verify no regression.
4. Force lifecycle compact threshold low in test config; verify compaction notice/event.
5. Simulate timeout/empty result with fake or controlled failure; verify next run is cold.
6. Test long-session rotation in a non-production chat/topic.
7. Promote to `stable/session-lifecycle-manager` only after user confirms live behavior.

**Done when:**
- [ ] Live tests pass
- [ ] User approves stable promotion
