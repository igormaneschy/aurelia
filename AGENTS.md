# AGENTS.md

Instructions for coding agents working in this repository.

## Development Commands

```bash
go build ./...           # compile check
go test ./... -short     # fast tests
go test ./... -v         # full test suite
go vet ./...             # static analysis
```

Bridge rebuild (after modifying `bridge/index.ts`):

```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
```

Note: The `npm run build` script includes `--banner:js` with `createRequire` to support PI SDK dependencies that use dynamic `require()`. Do not remove it.

Explicit equivalent:
```bash
cd bridge && npx esbuild index.ts --bundle --platform=node --target=node18 --outfile=bundle.js --format=esm --banner:js="import { createRequire as __piCreateRequire } from 'module';const require = __piCreateRequire(import.meta.url);"
cp bundle.js ../internal/bridge/bundle.js
```

## Branch Policy

**All implementations must be done in dedicated branches.** Never commit
directly to `main`. Changes only reach `main` after:

1. Implementation in a feature/fix branch
2. Live validation on the daemon
3. Explicit promotion by the user

Branch lifecycle:
```
feature/xxx  →  stable/xxx  →  main
  (impl)        (validation)    (release)
```

- **feature/*** — Active development. May be rebased, force-pushed, discarded.
- **stable/*** — Validated and deployed. Only bug fixes during validation.
  Merged to `main` when the user approves promotion.
- **main** — Production. Only updated via merge from a `stable/*` branch.

## Workflow

1. **Plan** — Understand the problem, break into atomic tasks
2. **Branch** — Create a `feature/<name>` branch from the latest `main`
3. **Execute** — One atomic task at a time, test-first, commit to feature branch
4. **Validate** — Run tests, verify completion criteria
5. **Deploy & Test live** — Rebuild, restart daemon, send a test message in
   Telegram, verify the change works end-to-end
6. **Promote to stable** — When feature is working live, merge into a
   `stable/<name>` branch for final validation
7. **User approval** — The user tests and approves
8. **Merge to main** — Conventional Commits: `type(scope): description`
9. **Push** — Push `main` to remote

For trivial fixes (one file, no risk of regression), the user may skip the
feature/stable branching and approve a direct commit to `main`.

### Step 6-8: Promotion to main

```bash
# Create stable branch (first time)
git checkout -b stable/<name> feature/<name>

# Deploy from stable for live validation
make deploy

# After user approval, merge to main
git checkout main
git merge stable/<name> --no-ff

# Update version and CHANGELOG (requires user approval)
edit internal/version/version.go
edit CHANGELOG.md
git commit -m "chore(release): bump to vX.Y.Z"

# Push
git push origin main
git push origin stable/<name>
```

### Step 6: Build & Restart (mandatory after every commit)

After every commit that changes Go or Bridge code, the binary **must** be rebuilt and the daemon restarted before considering the work done. This prevents testing with a stale binary.

```bash
# Atomic build + restart via launchd (KeepAlive so launchd respawns automatically)
make deploy
```

This uses `make install` (build → `.new` → `mv` — never corrupts a running binary) followed by `launchctl kickstart -k` which sends SIGTERM and lets launchd restart the daemon with the new binary.

> **Fallback** (if service is not loaded): `make install` then manually kill + restart via the old sequence below.

**Failure to rebuild + restart will produce false negatives during testing.** Treat this as part of "done".

> **Pro tip:** A `post-commit` git hook is installed at `.git/hooks/post-commit` that runs `make deploy` automatically after every commit. If enabled, step 6 is automatic — just commit and the daemon updates itself.

## Rules

- **Branch discipline**: All implementations in `feature/*` branches. Only
  `stable/*` branches are deployed for live testing. Only user-approved
  `stable/*` branches merge into `main`. Never commit directly to `main`.
- Service layer for business logic — never in handlers or entrypoints
- Errors treated explicitly — no silent swallowing
- `context.Context` with timeout on external operations
- Secrets never in repository — use `~/.aurelia/config/app.json`
- Tests required before marking work complete
- No new dependencies without justification
- Prefer editing over rewriting
- Keep interfaces small
- Update docs when behavior changes

## Key Packages

| Package | Responsibility |
|---------|---------------|
| `cmd/aurelia/` | Entrypoint, wiring, onboarding |
| `internal/bridge/` | Go client for the TS Bridge process |
| `internal/agents/` | Legacy Prompt Profile registry (`~/.aurelia/agents/*.md`, `@profile` compatibility) |
| `internal/session/` | PI session_file resume, cwd state, nudge buffers |
| `internal/persona/` | Identity files, prompt assembly |
| `internal/cron/` | Schedule store, scheduler, bridge-backed runtime |
| `internal/telegram/` | Telegram bot handlers |
| `internal/config/` | Config loading and validation |
| `internal/runtime/` | Instance and project path resolution |
| `bridge/` | TypeScript Bridge (PI SDK adapter) |
| `pkg/stt/` | Speech-to-text |

## Versioning & Changelog

Every change that goes into `main` **must** bump the version and update
`CHANGELOG.md`. The version bump (patch/minor/major) and changelog entry
**must be approved by Igor before committing** — propose the bump and
entry text, wait for confirmation, then commit.

## Lessons Learned

Historical lessons from prior implementations live in `.opencode/lessons/learned/`. Check `lessons/index.md` before implementing changes in related areas.

**Critical pattern: auth symlink:** The daemon's `~/.aurelia/pi-agent/auth.json` must be a symlink to `~/.pi/agent/auth.json` — never a copy. Stale credentials cause silent API hangs (model resolves but no events arrive). See `auth-symlink-instead-of-copy.md`.

**Critical pattern: models symlink:** The daemon's `~/.aurelia/pi-agent/models.json` must be a symlink to `~/.pi/agent/models.json` — never a stale copy. Telegram `/model` must match `pi --list-models`; a copied models file hides new PI providers/models even when caches refresh.

**Critical patterns from the 2026-05-20 code review remediation:**

- **Goroutine recovery**: Every background goroutine launched by a package must have `defer recover()` at the top. If it panics, the daemon dies or leaks state. See `goroutine-recovery-mandatory.md`.
- **Redaction before truncation**: Always redact secrets (`redactSecrets`, escaping) **before** truncating/slicing data. A secret sliced in half evades regex detection. See `redaction-before-truncation.md`.
- **Path traversal**: `filepath.Base("..")` returns `".."`. Never rely on `Base` alone for untrusted input — use `os.CreateTemp` for temp files and store original names as metadata only. See `filepath-base-traversal.md`.
- **Post-implementation review**: Self-review + passing build is not sufficient. After non-trivial changes, trigger specialized reviewers (security + backend) with an explicit validation checklist. See `post-impl-review-gaps.md`.

## Reference

- Architecture and codebase details: `.specs/codebase/`
- Project vision and roadmap: `.specs/project/`
- Lessons learned index: `.opencode/lessons/index.md`

<!-- ai-memory:start -->
## Long-term memory (ai-memory)

This project uses [ai-memory](https://github.com/akitaonrails/ai-memory)
for cross-session continuity.

**Default to the current project — always.** Every ai-memory tool
auto-scopes to the project resolved from your session's working
directory. **Do NOT pass `project`, `workspace`, or `cwd` arguments unless the user
explicitly references a *different* project by name** (e.g. "what did we
decide in the `other-app` project?"). Phrases like "this project",
"here", "we", "our work", "where did we leave off" all mean the *current*
project — call the tool with no scoping args. If the user asks about a
handoff and the SessionStart auto-fetched block is already in your
context, just answer from it; do not re-call the tool to "find it again"
in another project.

**Lifecycle hooks already capture every prompt + tool call
automatically.** You never need to manually write routine notes; the
SessionStart hook auto-fetches pending handoffs, and on session end
ai-memory writes a session-summary page and a handoff.
LLM consolidation (compiling observations into topical wiki pages) runs
on PreCompact, on demand via `memory_consolidate`, and at session end
only when the server sets `AI_MEMORY_CONSOLIDATE_ON_SESSION_END`. Only
write a durable wiki page when the user explicitly asks to remember or
annotate something permanently.

### When to reach for each tool

The user can express any of the intents below in plain English —
match the intent to the tool. They do not need to name the tool.

| User says / situation | Tool |
|---|---|
| "have we discussed X?" / "search memory for Y" / before proposing architecture | `memory_query` (current project; `scopes` for named siblings; `global=true` to search every project) |
| "what's been going on" / "show recent activity" (light) | `memory_recent` |
| "is ai-memory healthy?" / "how big is the wiki?" | `memory_status` |
| "give me the stats" / structured snapshot for the agent to consume | `memory_briefing` (read-only; never creates handoffs) |
| "catch me up" / "I've been away" / "what's important right now?" / open-ended exploration | `memory_explore` |
| "where did we leave off?" — and you see a `📥 ai-memory: pending handoff` block in your context | already done — answer from that block; do NOT re-call `memory_handoff_accept` |
| "where did we leave off?" — and no such block is visible | `memory_handoff_accept` (rare; the SessionStart hook usually got there first; pass `workspace` + `project` together only for a named sibling workspace/project) |
| "save context for the next session" / wrapping up / ending this session | `memory_handoff_begin` (session-end only; do **not** use for status/briefing; single-use handoff; terse summary; put detail in `open_questions` + `next_steps` bullets; pass `workspace` + `project` together only for a named sibling workspace/project) |
| "discard that handoff" / "I created a handoff by mistake" | `memory_handoff_cancel` (requires exact `handoff_id` from `memory_handoff_begin`; marks it expired before the next session sees it) |
| "consolidate this session" / "compile what we learned" (also runs on PreCompact; at session end only if `AI_MEMORY_CONSOLIDATE_ON_SESSION_END` is set) | `memory_consolidate` |
| "what did we learn from this session?" / "what memory should we add?" / explicit wrap-up learning review | `memory_auto_improve` (manual learning review for a completed session; omit `session_id` for latest completed session; the server also schedules background review for newly completed sessions in every project when configured) |
| "remember this permanently" / "save a note" / "add an annotation" / durable project knowledge | `memory_write_page` (write a wiki page; do **not** use handoff for permanent notes; put the title as a `# H1` on the first line of `body` and omit the `title` arg — ai-memory derives it from the H1) |
| "read the page about X" / "show me the full content of Y" / "open the page on Z" | `memory_read_page` (full body; pass a query to search or `path` for a direct lookup; pass `workspace` + `project` together only for a named sibling workspace/project) |
| "delete the page X" / "remove that note" | `memory_delete_page` (by exact `path`; idempotent; pass `workspace` + `project` together only for a named sibling workspace/project) |
| "audit the wiki" / "find contradictions" / "what rules should we add?" | `memory_lint` |
| "prune old pages" / "memory cleanup" | `memory_forget_sweep` |

`memory_explore` is the right default for the "I want to know what's
going on" use case — it returns a prose digest whose verbosity
scales automatically to how long it's been since the last activity
(< 1 h → one line; > 30 days → full catchup).

### When the current project comes up empty — broaden the search

`memory_query` searches only the **current** project by default. If a
search comes back empty or thin, the knowledge may live in a **sibling
project** — shared `infra`, `ops`, or a related app. Don't conclude
"we never recorded it" after a single project misses; broaden instead:

- **Know which projects to check?** Re-run with explicit `scopes`, e.g.
  `scopes: [{ "workspace": "default", "project": "infra" }]`.
- **Don't know where it lives?** Pass `global=true` to search every
  project in every workspace at once. Each hit is annotated with its
  workspace + project so you can tell where it came from. `global=true`
  cannot be combined with `scopes`/`project`/`workspace`.

`memory_query` returns **snippets, not full page bodies** — an empty or
short snippet does **not** mean the page is empty (a large page can
match outside the snippet window). To read the whole page, use
`memory_read_page` (by `path`, or pass a `query` to fetch the top hit's
full body; add `workspace` + `project` together only when the user names
a sibling workspace/project).

### Use Retrieved Memory As Operating Guidance

When `memory_query` or `memory_recent` returns `_rules/`, `gotchas/`,
`procedures/`, or `decisions/` pages that match the current task, treat
them as actionable context, not trivia:

- Read full pages with `memory_read_page` when the snippet looks relevant.
- Apply `_rules/` as constraints.
- Check `gotchas/` as preflight warnings before editing the same subsystem.
- Follow `procedures/` as checklists for releases, PR reviews, deploys,
  migrations, and other repeatable workflows.
- Use `decisions/` as prior architecture unless the user explicitly asks
  to revisit them.

Before non-trivial coding, debugging, deployment, release, auth, scope,
migration, PR-review, or data-preservation work, search memory for the
subsystem and task type first. If the first query is thin, broaden or
query specific error/subsystem terms before designing a fix.

### Learning Review

The server schedules background auto-improvement for newly completed sessions in
every project when an LLM provider is configured. `memory_auto_improve` is the manual version:
use it when the user asks what durable lessons this session suggests, or at
explicit wrap-up when reviewing proposed memory would be useful. Scheduled and
manual runs apply or stage validated edits through the auto-improvement approval
path. Admins can turn off scheduling with `[auto_improve.scheduler] enabled =
false`, or opt into manual proposal approval with `[auto_improve]
require_approval = true`, in which case scheduled and manual proposals stay in
pending-writes until approved.

### When you write a project rule, write it here

If you're about to write a durable project rule ("always X", "never
Y", "all PRs must …"), write it in the project's canonical agent
instruction file. Many projects use CLAUDE.md for Claude Code and
AGENTS.md for Codex / OpenCode / Cursor / Gemini CLI, but if the
project says one file is canonical, use that file. ai-memory's lint
pass surfaces the same hint automatically when a `kind: rule` page
lands in `_rules/`.

### Refreshing this snippet

This block is maintained by ai-memory. Two ways to refresh it with
the latest binary's recommended copy:

- **From the agent** (no terminal needed): ask "refresh the ai-memory
  routing in this project" — the agent calls
  `memory_install_self_routing`, picks the right filename for itself
  (Claude Code → `CLAUDE.md`; Codex / OpenCode / Cursor / Gemini →
  `AGENTS.md`), and uses its Write / Edit tool to land the block.
- **From the CLI**: `ai-memory install-instructions` (defaults to
  `CLAUDE.md`; pass `--target AGENTS.md` for non-Claude agents or
  projects that use `AGENTS.md` as the canonical instruction file).

Both are idempotent: re-runs replace the block bracketed by
`<!-- ai-memory:start -->` / `<!-- ai-memory:end -->` markers
without disturbing the rest of the file.
<!-- ai-memory:end -->
