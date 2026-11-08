## Context

See [proposal.md](proposal.md) — Why. Aurelia currently owns four prompt-memory
layers (`user_global`, `topic`, project-scoped `cwd_overlay`), a continuity
store, memoryux checkpoints/receipts, Dream/Nudge writes and their prompt
assembly paths. ai-memory 3.4.3 already provides the lifecycle extension,
handoffs, session observations, briefing/explore, pinned pages, profile/global
scopes, consolidation, auto-improvement, linting, retention and page restore.

The current personal deployment is NAS + desktop + Telegram; the TUI client is
disabled. The existing data volume is still small (the homebrewing overlay is
effectively empty, finance has no overlay, and the personal hub has 24 pages),
which makes this the right time to consolidate before the parallel stores grow.

Constraints:

- ai-memory is a local service/extension; the personal instance is the only
  allowed memory backend. The coding instance is never a fallback.
- The ai-memory handoff is single-use and project-scoped. It is not a per-turn
  chat/thread store.
- The PI session/transcript/compaction, runlog, identity/persona,
  security/policy and project binding remain Aurelia/PI responsibilities.
- The TUI client remains disabled; desktop and Telegram are the supported
  surfaces.
- The previous memory architecture analysis (`docs/...analysis_v2.md`, v0.16)
  predates ai-memory and still treats the four local layers as the contract.

## Goals / Non-Goals

**Goals:**

- Make ai-memory the single durable memory and handoff source.
- Replace continuity with the ai-memory handoff lifecycle plus session
  observations/briefing/explore.
- Replace the editable `cwd_overlay`/`MEMORY.md`, `topic` and `user_global`
  layers with ai-memory pages, pinned context, profile/global scopes and a
  bounded derived working set.
- Replace memoryux/Dream/Nudge with ai-memory page writes, briefing/status,
  feedback, lint, forget-sweep, restore, consolidation and auto-improvement.
- Migrate existing data safely with a feature flag, shadow mode, backups and a
  rollback path, then remove the legacy stack.
- Preserve PI session/context/compaction, runlog, identity/security/project
  binding and the desktop/Telegram UX.

**Non-Goals:**

- Replacing the PI context window, compaction or the runlog with ai-memory.
- Changing the model/provider, Telegram/desktop UX beyond memory state, or
  enabling the TUI.
- Importing the coding wiki or changing the personal ai-memory server internals.
- Adding a new embedding/vector store or a new external memory service.
- Redesigning multi-user ai-memory permissions beyond the personal profile.

## Decisions

### 1. ai-memory is the memory OS; Aurelia is the context adapter

ai-memory owns durable knowledge, session capture, handoffs, retrieval,
consolidation and hygiene. Aurelia assembles the prompt, maps conversation
scopes, applies policy and injects a bounded working set. This removes the
second editable store and the competing scopes.

Alternatives considered:

- **Keep the local layers and mirror into ai-memory** — retains drift and
  double writes; rejected.
- **Build a new local memory service** — duplicates ai-memory; rejected.
- **Use ai-memory only through optional agent tool calls** — loses deterministic
  working-set injection and depends on model compliance; rejected for the
  essential working set, kept for deep retrieval.

### 2. Handoffs replace continuity; PI session and runlog stay

The continuity fields are covered as follows:

| Continuity field | Replacement |
|---|---|
| Active goal, last intent, last assistant summary | ai-memory handoff summary/open questions/next steps; `memory_read_session_observations` for raw evidence |
| Last checkpoint | handoff next steps or a `sessions/<id>.md` page |
| Last run status/tools | runlog and ai-memory session observations |
| Session id/cold state | PI session store; ai-memory session id where the hook forwards it |
| Per-turn injection | live PI session context; handoff at SessionStart; post-compaction anchor/recap |

ai-memory creates/consumes handoffs at SessionEnd/SessionStart, is single-use,
and is project-scoped. The default policy is hub sharing: conversations using
the same project (Telegram DM + desktop DM) share the project handoff.
Conversations with distinct project cwds (the Telegram topics) are isolated.
If strict per-conversation isolation is needed for a shared project, use
session observations/session pages or a `chat:<id>/thread:<id>` tag; do not
reintroduce a full continuity store.

Reset semantics: `/reset` cancels or expires pending handoffs for the
conversation, clears the PI session and prevents an old handoff from being
injected. A reset reason may be recorded as a session/page observation, not as a
parallel continuity state.

### 3. Working set is derived, bounded and never an editable wiki

The working set is assembled from:

- the ai-memory handoff block injected at SessionStart;
- `memory_briefing` (pinned/recent/rules/pending-handoff counts);
- pinned page bodies via `memory_read_page` (bounded);
- `memory_query` with `pin_first` and project scope for task-relevant snippets;
- `memory_explore`/`memory_read_session_observations` for catch-up and recovery.

It has a configured character/token cap, is cached by project/session and is
invalidated or refreshed after ai-memory writes. It never injects the full wiki
and never writes a local `MEMORY.md`/`current_task.md` as a second source.

Injection point: prefer the ai-memory lifecycle extension (it already injects
the profile digest, recovery snapshot and post-compaction anchor at
SessionStart/PreCompact). If the extension cannot inject a **project**
briefing/pinned digest in the PI runtime, a thin Aurelia `MemoryAdapter` calls
the local ai-memory interface to build the same bounded working set. The
adapter is a read-through cache, not a durable store.

### 4. Scope mapping and isolation

- **Project**: cwd + `.ai-memory.toml` (already used by the NAS topics and the
  personal hub). This is the durable scope.
- **User**: the persona user id; profile/global pages are explicit.
- **Conversation**: PI session id, or a session page/tag
  `chat:<chat_id>/thread:<thread_id>` where per-conversation isolation is
  required.
- **Handoff**: project-level by default; hub sharing for DM/desktop; distinct
  topic projects remain isolated.
- **Queries**: project scope by default; `scopes`/`global` only on an explicit
  request. `global_scope_hits`/profile context is surfaced separately, not
  silently merged into project memory.

### 5. Migration and removal

1. Inventory and baseline the four layers, continuity rows, memoryux receipts
   and Dream/Nudge files; measure prompt sizes, reads/writes and duplicates
   against existing ai-memory pages.
2. Dry-run the import and map each source to an ai-memory scope/page:
   - project `cwd_overlay` wiki notes → project pages (kind/tier/tags;
     ADRs/decisions pinned);
   - `user_global` → `scope: "profile"` or `_global` pages;
   - `topic` notes → project pages with a conversation tag or session page;
   - continuity → `sessions/<id>.md` and/or handoff;
   - memoryux checkpoints/receipts → session pages/feedback, not a local file.
3. Import with `memory_write_pages_batch`/`memory_resolve_write_target`,
   preserving source paths/links and backing up originals. Keep originals
   read-only until acceptance.
4. Add the `MemoryAdapter`/working-set path and a feature flag
   (`legacy | ai-memory | shadow`), shadow-reading both and comparing recall,
   prompt size and latency.
5. Switch the default to ai-memory; keep legacy read-only for one release and a
   rollback path.
6. Remove legacy packages, files, commands and prompt layers; update `/memory`,
   `/reset`, project state, the desktop project panel, tests and docs.
7. Archive the OpenSpec change and retire the superseded memory specs.

### 6. Tools, allowlist and security

Required ai-memory tools by role:

- **Read/context**: `memory_query`, `memory_read_page`, `memory_briefing`,
  `memory_explore`, `memory_recent`, `memory_status`,
  `memory_read_session_observations`.
- **Handoff**: `memory_handoff_list`, `memory_handoff_accept`,
  `memory_handoff_begin`, `memory_handoff_cancel`; the SessionStart/End hooks
  handle the normal path.
- **Durable writes**: `memory_write_page`, `memory_patch_page`,
  `memory_write_pages_batch`, `memory_resolve_write_target`; only on explicit
  user request or approved automatic consolidation.
- **Maintenance**: `memory_consolidate`, `memory_auto_improve`,
  `memory_feedback`, `memory_lint`, `memory_forget_sweep`,
  `memory_restore_page`, `memory_delete_page` (destructive tools remain
  privileged).

The personal assistant's capability profile must allow the read/handoff tools
and the explicit-write path if the product needs it. Capture exclusions remain
enforced; the personal instance is the only backend; no coding fallback. The
handoff/consolidation hooks require the extension to receive the session id;
verify this in the NAS PI runtime.

### 7. Observability and acceptance

Record working-set characters/tokens, ai-memory latency/availability, fallback
count, handoff created/consumed, migration imported/conflicts, and memory
outage events (without memory content). Acceptance tests cover:

- cross-surface durable recall (Telegram ↔ desktop);
- project isolation (different topic cwds) and hub handoff sharing;
- cold start with a handoff, compaction recovery and `/reset`;
- bounded prompt with a large wiki;
- ai-memory unavailable (cached working set, degraded notice, no coding
  fallback, no writes);
- capture exclusions;
- migration dry-run, verified import and rollback;
- tool allowlist for the personal profile.

## Risks / Trade-offs

- **Handoff is project-scoped and single-use** → default hub sharing is
  intentional; distinct topics have distinct projects; strict per-conversation
  isolation uses session observations/tags; reset cancels old handoffs.
- **Extension/session-id dependency** → verify SessionStart/End/PreCompact hooks
  and session-id forwarding in the NAS PI runtime; manual `memory_handoff_list`
  / `accept` fallback; feature flag.
- **ai-memory availability/latency** → bounded cached working set, degraded
  notice, no coding fallback, latency metrics.
- **Auto-capture/privacy** → keep capture exclusions; only explicit durable
  writes; review retention/consolidation; never inject the full wiki.
- **Migration duplicates/staleness** → dry run, source metadata, verification,
  read-only originals, backups and rollback.
- **Prompt budget/ranking** → cap the working set, pin only standing context,
  use `pin_first`/briefing, measure tokens.
- **Tool allowlist gaps** → add the required read/handoff/write tools to the
  personal profile; keep destructive/bulk tools privileged.
- **Thread isolation** → distinct cwd/project by default; tags/session pages
  for shared projects.
- **Runlog/context confusion** → PI session/compaction and runlog remain
  authoritative; ai-memory supplies recap/handoff, not the live window.
- **Extension version drift** → pin/verify ai-memory 3.4.3+ and its hook
  configuration before removing the legacy fallback.

## Migration Plan

| Phase | Deliverable | Exit gate |
|---|---|---|
| 0. Baseline | Inventory, usage metrics, prompt/latency/recall baseline, scope map | User approves the import map and acceptance criteria |
| 1. Import | Dry run, backups, pages/profile/session/handoff import, verification report | Imported facts/links verified; originals untouched/read-only |
| 2. Adapter | `MemoryAdapter`/working set + feature flag; shadow read; allowlist; tests | Shadow recall/latency/prompt-size acceptable; no coding fallback |
| 3. Switch | ai-memory default; legacy read-only; outage/reset/compaction tests; user acceptance | Telegram + desktop recall and handoff accepted live |
| 4. Remove | Delete legacy packages/files/commands/layers; update `/memory`, `/reset`, project state, desktop panel, docs/specs | `go build`/`go vet`/`go test` green; no stale references; backup retained |
| 5. Close | Archive OpenSpec, retire superseded specs, release notes | Next release includes the change; live checks pass |

Rollback: before phase 4, set the feature flag to `legacy` and keep the imported
ai-memory data intact; originals remain read-only. Backups are taken before
import and before removal.

## Open Questions

- Does the ai-memory extension inject a **project** briefing/pinned digest in the
  NAS PI runtime, or is a thin Go adapter/CLI read required? (Spike in phase 2;
  both provide the same working-set contract.)
- What is the exact default working-set cap and refresh interval? (Measure in
  phase 0; configurable, no spec change.)
- Do we need strict per-conversation isolation for the shared DM/desktop hub, or
  is project-level hub sharing the accepted default? (Default: hub sharing;
  session tags if strict isolation is requested.)
