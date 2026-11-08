## Why

Aurelia still maintains a parallel memory stack — `user_global`, `topic`,
`cwd_overlay`/`MEMORY.md`, continuity, memoryux checkpoints/receipts and
Dream/Nudge — while ai-memory already provides handoffs, session observations,
briefing/explore, pinned pages, profile/global scopes, consolidation,
auto-improvement, linting, retention and page restore. The duplication causes
drift, extra code/tests, prompt-token cost and competing scopes. Now that
ai-memory is the personal memory system on the NAS and the desktop/Telegram
surfaces are live, the duplication should be removed.

## What Changes

- Make ai-memory the single source of truth for durable memory and session
  context; Aurelia becomes a thin context adapter over it.
- Replace Aurelia continuity with ai-memory handoff + session observations +
  briefing/explore + the post-compaction anchor. Keep only the PI session
  pointer/lifecycle and runlog where they are execution state, not memory.
- Replace `cwd_overlay`/`MEMORY.md` with ai-memory pages (pinned decisions,
  rules, gotchas, session pages) plus a bounded, derived working set from
  briefing/pinned/query. Do not keep a second editable wiki.
- Remove the `topic` and `user_global` prompt-file layers; use ai-memory project
  scopes, tags, session pages, profile and global scopes instead.
- Replace memoryux checkpoints/status/receipts and Dream/Nudge with ai-memory
  handoff, page writes, briefing/status, feedback, lint, forget-sweep, restore,
  consolidation and auto-improvement.
- Add a small `MemoryAdapter`/working-set boundary in Aurelia: map
  `chat/thread/user` to project/session/tag, load handoff/briefing/pinned
  context, expose the ai-memory tools, and keep a bounded read-only fallback
  cache when ai-memory is unavailable.
- **BREAKING**: remove or redefine the local memory layout, `/memory
  checkpoint`, continuity-backed `/reset`, project-state memory layers and the
  legacy memory commands. Existing data is migrated before removal.
- Migrate existing `cwd_overlay`, `user_global`, `topic`, continuity and
  memoryux data into ai-memory; use a feature flag/shadow period and a rollback
  path, then delete the legacy packages, tests, specs and docs.
- Keep PI session/transcript/compaction, prompt assembly, identity/persona,
  security/policy, project binding, runlog/observability and the
  Telegram/desktop/TUI surfaces.
- Update the NAS runbook, desktop README, root docs and the memory architecture
  analysis to describe ai-memory as the memory OS and Aurelia as the context
  adapter.

## Capabilities

### New Capabilities

- `ai-memory-context`: ai-memory is the durable memory and handoff source;
  Aurelia loads a bounded working set from ai-memory, maps conversation scopes,
  degrades safely when ai-memory is unavailable, and removes the duplicated
  local memory/continuity stack after migration.

### Modified Capabilities

Nenhuma. O repositório ainda não tem specs principais em `openspec/specs/`; a
mudança do runtime NAS permanece um artefato de planeamento separado. A nova
capacidade cobre o comportamento introduzido por esta mudança.

## Impact

- **Code**: `internal/continuity`, `internal/memoryux`, `internal/dream`,
  `internal/pipeline` (prompt builder, memory cache, continuity lifecycle),
  `internal/runtime` (resolver/bootstrap), `cmd/aurelia` (TUI/Telegram handlers,
  wiring, migration), `internal/security/extension_tools.go`, desktop
  project-state types/UI, and tests.
- **Data/config**: `~/.aurelia/projects/*/cwd_overlay`, `~/.aurelia/topics/*`,
  `~/.aurelia/users/*/memory`, continuity state and memoryux receipts migrate
  to ai-memory pages/profile/session records. Originals stay read-only until
  verification; backups are taken first.
- **Dependencies**: ai-memory 3.4.3+ with its lifecycle extension/hooks and the
  relevant tools enabled. No new external service is introduced.
- **Behavior**: `/memory`, `/reset`, project-state memory layers and the
  desktop project panel change; cross-surface recall moves to ai-memory
  handoff/briefing/session queries. Telegram and desktop remain the supported
  surfaces; the TUI client stays disabled.
- **Risk surface**: handoff is project-scoped and single-use, not per
  chat/thread; session-id forwarding, hook configuration, ai-memory
  availability/latency, auto-capture privacy and migration correctness must be
  validated before the legacy stack is deleted.
