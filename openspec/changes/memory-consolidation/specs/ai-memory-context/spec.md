## Purpose

Aurelia uses ai-memory as its single durable memory and session-continuity source, assembling a bounded, scoped working set for each turn instead of maintaining parallel local memory files and continuity state.

## ADDED Requirements

### Requirement: ai-memory is the single durable memory source

Durable project knowledge, user preferences and session handoffs SHALL be stored in and retrieved from ai-memory. After the migration gate, Aurelia SHALL NOT read or write `cwd_overlay`, `MEMORY.md`, `topic` memory, `user_global` files, continuity state, memoryux checkpoints/receipts or Dream/Nudge outputs as durable memory sources.

#### Scenario: Cross-surface durable recall
- **WHEN** a durable fact is recorded in one supported surface (Telegram or desktop) for a project
- **THEN** another supported surface using the same project cwd retrieves it from ai-memory without reading a local cwd_overlay/topic/user_global file

#### Scenario: No parallel durable writes
- **WHEN** a turn records durable memory after the migration gate
- **THEN** an ai-memory page is written and no new file is created under the legacy local memory paths

### Requirement: Handoff-based cross-session continuity

Cross-session continuity SHALL be provided by the ai-memory handoff lifecycle, session observations, briefing/explore and the post-compaction anchor. Aurelia SHALL NOT maintain a separate durable continuity store after the migration gate. A handoff SHALL be single-use and project-scoped; session-specific recovery SHALL use session observations, session pages or tags.

#### Scenario: Handoff at session boundary
- **WHEN** a session ends and a new session starts for the same project
- **THEN** the next session receives the pending ai-memory handoff (summary, open questions, next steps) and no separate continuity state is injected

#### Scenario: Recovery after compaction
- **WHEN** the context is compacted mid-session
- **THEN** the ai-memory post-compaction anchor, session observations or briefing re-ground the assistant without a local continuity store

#### Scenario: Reset
- **WHEN** the user resets a conversation
- **THEN** pending handoffs for that conversation are cancelled or expired, the PI session is cleared, and an old handoff is not injected into the next turn

#### Scenario: Shared project, separate conversations
- **WHEN** two conversations use the same project but require per-conversation isolation
- **THEN** the project-level handoff is not used as the only isolation mechanism; session observations or tagged session pages distinguish the conversations

### Requirement: Bounded working set from ai-memory

Before the first turn and after a cwd/session change, Aurelia SHALL assemble or receive a bounded working set from ai-memory handoff, briefing, pinned pages and/or session observations. It SHALL enforce a size cap, cache by project/session, and invalidate or refresh after ai-memory writes. It SHALL NOT inject the full wiki or an unbounded page body.

#### Scenario: Bounded prompt
- **WHEN** a project has many ai-memory pages
- **THEN** the injected working set contains only the bounded handoff/pinned/relevant digest and stays under the configured cap

#### Scenario: Stale working set
- **WHEN** a pinned or session page changes in ai-memory
- **THEN** the cached working set is invalidated or refreshed before the next turn

#### Scenario: ai-memory unavailable
- **WHEN** ai-memory is unavailable at turn assembly
- **THEN** Aurelia uses the last read-only working-set cache, reports degraded memory, does not write and does not fall back to the coding ai-memory instance

### Requirement: Scope mapping and isolation

ai-memory scopes SHALL map project from the cwd marker, user from the persona, and conversation from the PI session or an explicit session tag/page. The project handoff SHALL be shared by conversations using the same project (the personal hub) unless an explicit per-conversation isolation policy is enabled. Distinct projects SHALL remain isolated. `global` and `profile` reads/writes SHALL be explicit and SHALL NOT be silently mixed into project scope.

#### Scenario: Same project across surfaces
- **WHEN** Telegram and desktop use the same project cwd
- **THEN** they share the project pages and the project-level handoff according to the hub policy

#### Scenario: Different projects
- **WHEN** two Telegram topics use different project cwds
- **THEN** their project pages and handoffs are isolated, and a query does not return the other project unless it explicitly requests a broader scope

#### Scenario: Explicit global/profile
- **WHEN** a profile/global preference is read or written
- **THEN** the request uses the explicit profile/global scope and the UI/agent does not present it as project-private memory

### Requirement: Migration with rollback

Existing `cwd_overlay`, `user_global`, `topic`, continuity and memoryux data SHALL be inventoried, backed up and imported into ai-memory with source metadata (source paths, tags, session, pinned where appropriate) before the legacy stores are removed. The migration SHALL support a dry run, a verification report and a feature-flagged rollback to legacy read-only mode.

#### Scenario: Dry-run inventory
- **WHEN** the migration starts
- **THEN** it reports the files/pages/sessions to import, the target ai-memory scope and any conflicts or duplicates without writing to production

#### Scenario: Verified import
- **WHEN** the migration writes the approved data
- **THEN** a verification job reads the imported pages/profile/handoffs back and confirms the expected facts/links are present

#### Scenario: Rollback
- **WHEN** the ai-memory mode fails acceptance during the shadow/rollout period
- **THEN** the feature flag restores legacy read-only behavior without losing the imported ai-memory data or the legacy originals

### Requirement: Tool access, privacy and no fallback

The assistant SHALL have the ai-memory read, handoff, write and maintenance tools required by its capability profile. Durable writes SHALL occur only when the user explicitly requests them or an approved automatic consolidation/auto-improvement path is configured. Capture exclusions SHALL remain enforced. Personal memory SHALL use only the personal ai-memory instance; the coding instance SHALL NOT be a fallback.

#### Scenario: Read and handoff tools
- **WHEN** the assistant needs project context or a pending handoff
- **THEN** the relevant ai-memory read/handoff tools are available under the active profile

#### Scenario: Explicit durable write
- **WHEN** the user asks to remember a durable fact
- **THEN** the assistant writes an ai-memory page/profile entry as appropriate and does not create a legacy local memory file

#### Scenario: Memory outage
- **WHEN** the personal ai-memory is unavailable
- **THEN** the assistant reports the outage and does not write to or read from the coding ai-memory instance

#### Scenario: Capture exclusion
- **WHEN** content is covered by an approved capture exclusion
- **THEN** it does not appear in ai-memory observations or pages

### Requirement: Legacy removal and surface behavior

After acceptance, the legacy memory packages, files, commands and prompt layers SHALL be removed. `/memory`, `/reset`, project-state memory layers and the desktop project panel SHALL reflect ai-memory handoff/briefing/project state. The TUI client remains disabled; desktop and Telegram remain the supported surfaces.

#### Scenario: Clean removal
- **WHEN** the legacy stack is deleted
- **THEN** no runtime path reads or writes `cwd_overlay`, `topic`, `user_global`, continuity, memoryux or Dream/Nudge files, and no stale command references remain

#### Scenario: Surface state
- **WHEN** the user opens `/memory`/project state or the desktop project panel after the switch
- **THEN** the displayed layers/status come from ai-memory project/handoff/briefing data and show degraded state when applicable

### Requirement: Context and observability boundaries

The PI session/transcript/compaction, runlog, identity/persona, security/policy and project binding SHALL remain Aurelia/PI responsibilities. ai-memory SHALL NOT be treated as the live context window or runlog. The system SHALL expose observability for working-set size, ai-memory latency/availability, fallback use, handoff consumption and migration results.

#### Scenario: Compaction remains PI-owned
- **WHEN** the PI session compacts
- **THEN** the PI SDK remains responsible for the context window, with ai-memory only supplying recap/handoff/session observations

#### Scenario: Run observability
- **WHEN** a run completes or fails
- **THEN** the runlog still records its status/cost/duration/error independently of ai-memory page state

#### Scenario: Degraded mode telemetry
- **WHEN** the working set comes from the fallback cache
- **THEN** the system logs/metrics record degraded memory and the ai-memory failure without exposing memory content
