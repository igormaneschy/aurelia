# CLAUDE.md

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
make bridge              # rebuilds + copies bundle into internal/bridge/
```

## Deploying changes

Use the Makefile — never edit `~/.aurelia/bin/aurelia` by hand:

```bash
make install-service     # one-time: install launchd plist
make deploy              # build atomically + restart the daemon
make logs                # tail daemon stderr
make status              # check launchd state
```

Full operations guide: [`docs/OPERATIONS.md`](docs/OPERATIONS.md) — covers
auto-restart, recovery from orphan daemons, troubleshooting, etc.

## Workflow

1. **Plan** — Understand the problem, break into atomic tasks
2. **Review** — Question the plan before executing
3. **Execute** — One atomic task at a time, test-first
4. **Validate** — Run tests, verify completion criteria
5. **Commit** — Conventional Commits: `type(scope): description`

For trivial tasks, implement directly and validate.

## Rules

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
| `cmd/aurelia/` | Entrypoint, wiring, lifecycle |
| `internal/bridge/` | Go client for the TS Bridge process (PI SDK) |
| `internal/pipeline/` | Reusable turn driver: prompt + bridge + plan dispatch + resilience + run supervisor |
| `internal/orchestrator/` | Plan→workers→validate cycle, worktrees, quality gate, git/PR |
| `internal/agents/` | Legacy Prompt Profile registry (`~/.aurelia/agents/*.md`, `@profile` compatibility) |
| `internal/session/` | PI session_file resume, cwd state, nudge buffers |
| `internal/persona/` | Identity files, prompt assembly |
| `internal/cron/` | Schedule store, scheduler, bridge-backed runtime |
| `internal/dream/` | Background memory consolidation and nudges |
| `internal/telegram/` | Telegram bot handlers + command layer |
| `internal/config/` | Config loading and validation |
| `internal/runtime/` | Instance and project path resolution |
| `internal/onboarding/` | Interactive setup wizard |
| `internal/deps/` | Runtime dependency checks (Node, npm, git, gh) |
| `internal/version/` | Build version constant |
| `bridge/` | TypeScript Bridge (PI SDK adapter) |
| `pkg/stt/` | Speech-to-text |

## Versioning & Changelog

Every change that goes into `main` **must** bump the version and update
`CHANGELOG.md`. The version bump (patch/minor/major) and changelog entry
**must be approved by Igor before committing** — propose the bump and
entry text, wait for confirmation, then commit.

## Lessons Learned

Historical lessons from prior implementations live in `.opencode/lessons/learned/`. Check `lessons/index.md` before implementing changes in related areas.

**Critical patterns from the 2026-05-20 code review remediation:**

- **Goroutine recovery**: Every background goroutine launched by a package must have `defer recover()` at the top. If it panics, the daemon dies or leaks state. See `goroutine-recovery-mandatory.md`.
- **Redaction before truncation**: Always redact secrets (`redactSecrets`, escaping) **before** truncating/slicing data. A secret sliced in half evades regex detection. See `redaction-before-truncation.md`.
- **Path traversal**: `filepath.Base("..")` returns `".."`. Never rely on `Base` alone for untrusted input — use `os.CreateTemp` for temp files and store original names as metadata only. See `filepath-base-traversal.md`.
- **Post-implementation review**: Self-review + passing build is not sufficient. After non-trivial changes, trigger specialized reviewers (security + backend) with an explicit validation checklist. See `post-impl-review-gaps.md`.

## Reference

- Architecture and codebase details: `.specs/codebase/`
- Project vision and roadmap: `.specs/project/`
- Lessons learned index: `.opencode/lessons/index.md`
