# Memory Consolidation Baseline — 2026-11-07

Structural baseline for the `memory-consolidation` change. Collected read-only
on the NAS personal instance (ai-memory 3.4.3, Aurelia v0.53.0). No note,
prompt, session or page content is reproduced here.

Status: **structural baseline + legacy migration + live ai-memory switch
complete; Telegram acceptance and failure-mode fixtures pending.** The live
switch was performed directly after the import; the shadow-mode comparison was
not run, and the direct-switch validation is recorded below.

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

The legacy prompt baseline and the first live ai-memory check are recorded
below. The remaining fixtures are still pending and must be run for acceptance.

| Measurement | Legacy | ai-memory live |
|---|---:|---:|
| DM prompt-memory characters | 542 (~135 tokens) | 3,126 (3 sources) |
| Working-set cap | none | 4,000 characters |
| Desktop recall without tools | not measured | **passed** (see below) |
| End-to-end latency reference | p95 81.5s (54 runs) | live sample pending |

The desktop recall probe created a temporary TUI session on the NAS, asked the
model to answer **without tools** using the injected project working set, and
was then deleted. The question was about the most recent `/status` session page
in `personal/aurelia`; the answer correctly returned the prompt `/status` and
`19 observations`. No memory content is reproduced here.

Pending fixtures:

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

## Migration result (2026-11-07)

A pre-import ai-memory backup was written to
`~/.aurelia/backup-test/pre-memory-consolidation-20261107224526.tar.gz`
(704,423 bytes; SHA-256
`c51be982b22bced158d8f5ae2217c8759d8e47d54c3466d047a2fa4a8d91d07a`).

The dry run reported 23 pages, 0 duplicate targets and no blocking warnings.
The final `migrate-memory --apply --verify` wrote and verified **23/23 pages**,
0 skipped. The finance project had a non-fatal `memory_resolve_write_target`
warning because it had no pages yet; the write created/registered the project
and the page verified.

Project distribution after import:

| Project | Pages | Delta |
|---|---:|---:|
| personal/aurelia | 42 | +12 |
| personal/aurelia-brewpub | 1 | +1 |
| personal/aurelia-finance | 1 | +1 (created) |
| personal/aurelia-group | 7 | +5 |
| personal/aurelia-homeassistant | 1 | +1 |
| personal/aurelia-homebrewing | 3 | +3 |
| **Total imported** | | **23** |

ai-memory status after import: 57 pages (89 versions), 23 sessions, 600
observations, 1.9 MiB storage. The observation increase may include unrelated
live extension capture; it is not attributed solely to the migration.

The legacy files, continuity database and memoryux files were only read; their
originals remain untouched. The imported pages are tagged `legacy-import`.

## Live ai-memory switch (2026-11-07)

The live NAS config was switched to `memory_provider=ai-memory`; the pre-switch
backups are `~/.aurelia/config/app.json.pre-memory-switch-20261107230812` and
`~/.aurelia/bin/aurelia.prev-memory-switch-20261107230812`. The service
restarted cleanly and logged the first working set:

```text
memory: ai-memory working set project=personal/aurelia chars=3126 sources=3 degraded=false pending_handoffs=1
```

A TUI smoke turn over the same Unix socket used by the desktop replied `OK`.
The desktop then showed `bridge online`, the `personal/aurelia` scope and the
project state, with no error banner. The recall probe described above passed
without tool calls.

Rollback before the legacy removal: set `memory_provider=legacy` in
`~/.aurelia/config/app.json` and restart `aurelia.service`; the legacy files and
continuity DB are still present. After the user accepts the live behavior, the
remaining OpenSpec tasks remove the legacy packages, paths, commands and prompt
layers.

### Project attribution check (2026-11-07)

The homebrewing topic initially reported `Sessions: 0` because its PI session
had started in the group-general cwd before the topic binding existed; the
ai-memory session row stayed in `aurelia-group` while the observations moved to
`aurelia-homebrewing`. The `/cwd` project-change reset fixed the cause. After
`/reset` and one message, the topic reported `Sessions: 1`, `Pages: 3`,
`Observations: 315`, `Degraded: false` and 0 pending handoffs; the new session
cwd is the topic project and the working set is
`personal/aurelia-homebrewing` (3,154 characters, 3 sources).
