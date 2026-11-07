# Memory Consolidation Baseline — 2026-11-07

Structural baseline for the `memory-consolidation` change. Collected read-only
on the NAS personal instance (ai-memory 3.4.3, Aurelia v0.53.0). No note,
prompt, session or page content is reproduced here.

Status: **structural baseline complete; recall fixtures pending** (the recall
run happens in the shadow phase before the provider switch).

## ai-memory instance

| Metric | Value |
|---|---:|
| Pages | 33 (57 versions) |
| Sessions | 22 |
| Observations | 377 |
| Embeddings | 57 rows, 0 latest pages missing |
| Storage | 1.6 MiB |
| Links | 23 latest-page links; 10 unresolved |
| Health | server + client OK, loopback, bearer token |

Project page counts: personal hub 30, one group project 2, one default project
1; the four named topic projects have 0 pages and are created on demand.

A direct MCP probe of `memory_briefing` for the personal hub returned a
4,065-character payload with 5 recent pages, 0 rules, empty context slots, and
1 pending handoff. The payload contained counts, activity, recent pages, recent
sessions, rules and pending handoff/message counts. The working-set builder
must not inject recent-session ids/cwd or the full payload.

## Legacy stores

| Store | Count | Size / notes |
|---|---:|---:|
| `cwd_overlay` | 2 directories, 4 files | 3,181 bytes total (2,712 + 469) |
| `user_global` | 0 | no per-user memory directories |
| `topic` | 0 | no topic-scoped memory directories |
| continuity | 13 conversation states + 6 project-work states | 36,864-byte DB |
| memoryux receipts | 0 | no `memory_receipts.jsonl` |
| Dream/Nudge files | 0 | none found |

All four legacy overlay files have distinct SHA-256 hashes; no duplicate
content exists *within* the legacy overlays. Overlap with the 30 personal-hub
ai-memory pages is intentionally left to the migration dry-run, which reports
conflicts/duplicates without writing.

Recent prompt-memory log lines show `[user_global topic cwd_overlay]` with
**542 characters** (about 135 tokens) for the DM turns. The user-global and
topic layers are empty today; the value comes from the project overlay and the
prompt assembly. The initial working-set cap for the new provider is **4,000
characters**, configurable and measured against this baseline.

Recent terminal runs (all entry points, including model/tool time, not memory
in isolation): 54 runs, average 24.5s, p50 13.3s, p95 81.5s. These are a
system-level latency reference, not a memory-layer benchmark.

## Interface findings

The Pi extension `ai-memory-pi.ts` is installed in the shared PI agent
extension directory. It:

- forwards the PI session id (`ctx.sessionManager.getSessionId()`);
- runs `session_start`, `before_agent_start`, tool call/result,
  `session_before_compact`, `session_compact`, `agent_end` and
  `session_shutdown` hooks;
- fetches a pending handoff at `before_agent_start` via `GET /handoff` and
  injects it as a custom `ai-memory-handoff` message;
- posts session/prompt/tool/stop events to `/hook`; the server owns the
  session observations and automatic handoff;
- starts the MCP bridge at `http://127.0.0.1:49374/mcp?flavor=moonshot` and
  registers the ai-memory tools.

The Pi extension does **not** inject a project briefing/pinned digest into the
model context; it only injects the handoff. The project working set therefore
belongs to the thin Aurelia adapter. The adapter will use the MCP endpoint with
the bearer token from the ai-memory data dir (`auth-token`), call
`memory_briefing` with explicit `workspace`/`project`, read a bounded number of
recent page bodies with `memory_read_page`, and format only counts, rules, page
titles/excerpts and pending-handoff count. It must not inject recent-session
cwd/ids or the full wiki.

The MCP tool surface observed: `memory_briefing`, `memory_query`,
`memory_read_page`, `memory_recent`, `memory_status`,
`memory_read_session_observations`, `memory_handoff_*`, `memory_write_page`,
`memory_patch_page`, `memory_write_pages_batch`, `memory_consolidate`,
`memory_auto_improve`, `memory_feedback`, `memory_lint`, `memory_forget_sweep`,
`memory_restore_page`, `memory_delete_page`, and the messaging tools.

The handoff path is project-scoped and single-use; the default hub policy shares
it between Telegram DM and desktop when they use the same project. The four
named Telegram topics use distinct project cwds and remain isolated.

## Scope map

| Source | Target |
|---|---|
| personal hub `cwd_overlay` | project pages in `personal/aurelia`; decisions/rules pinned |
| one topic `cwd_overlay` | pages in that topic's project; conversation/session tag |
| `user_global` (currently empty) | `scope: "profile"` or `_global` pages |
| `topic` (currently empty) | project page with a `chat:<id>/thread:<id>` tag/session page |
| continuity rows | handoff / `sessions/<id>.md` page |
| memoryux receipts (none) | session pages/feedback, not a local file |
| Dream/Nudge (none) | `memory_consolidate` / `memory_auto_improve` on the server |

Project resolution is the `.ai-memory.toml` marker (six cwds/markers currently:
the personal hub, one group general, four named topics; six bindings). The
private project labels and ids are intentionally omitted from this public
document.

## Recall fixtures

The following fixtures will be measured against the legacy mode now and the
ai-memory mode in the shadow phase. Results are pending and must be recorded
before the switch.

1. Cross-surface recall: record a durable fact in Telegram, retrieve it from the
   desktop in the same project.
2. Project isolation: query a fact from a different topic project; expect no
   result without an explicit broader scope.
3. Hub handoff: end a desktop DM session, start a Telegram DM session; the
   handoff summary/open questions appear once.
4. Compaction: compact a long session and recover the working context from the
   post-compaction path.
5. Reset: reset a conversation; an old handoff is not injected into the next
   turn.
6. Outage: stop the personal ai-memory; use the cached working set/degraded
   state; no coding instance is touched and no write occurs.
7. Capture exclusion: an excluded path produces no observation/page.
8. Migration: dry-run, verified import and feature-flagged rollback.
