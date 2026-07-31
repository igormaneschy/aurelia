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

> **Pro tip:** A `post-commit` git hook runs `make deploy` automatically
> after every commit. It lives in the OpenCode global hooks dir
> (`~/.config/opencode/tools/git-hooks/post-commit`) because `core.hooksPath`
> redirects all hooks there and `.git/hooks/post-commit` is never executed.
> Runs async via `nohup` (never blocks git); output goes to
> `~/.aurelia/logs/post-commit.log`. If enabled, step 6 is automatic — just
> commit and the daemon updates itself.

## Architecture constraint

**No planning mode / no orchestrator.** Aurelia does not implement `aurelia-plan` blocks, `/execute`, pending plans, or `internal/orchestrator/`. Agentic execution belongs to the PI SDK. The pipeline is message → bridge → reply.

## Rules

- Service layer for business logic — never in handlers or entrypoints
- `context.Context` with timeout on external operations
- Secrets via `~/.aurelia/config/app.json`

## PI SDK Configuration

O Aurelia isola seu ambiente PI em `~/.aurelia/pi-agent/` (via `PI_CODING_AGENT_DIR`). O PI CLI global usa `~/.pi/agent/`. São diretórios **separados**.

- **Symlinks gerenciados pelo `setup.go` (EnsureBridge)**: 5 recursos são sincronizados do PI global → Aurelia PI:
  - `auth.json` — credenciais de API
  - `npm/` — extensions: `pi-mcp-adapter`, `pi-web-access`, `pi-hermes-memory`
  - `mcp.json` — servidores MCP configurados
  - `mcp-cache.json` — cache de manifests
  - `mcp-npx-cache.json` — cache de resolução npx
- **Sem o symlink `npm/`**, as extensions não carregam e o modelo não consegue usar `web_search`, `mcp`, `memory_search` etc.
- **Sem os symlinks `mcp*.json`**, o `pi-mcp-adapter` não descobre servidores MCP.
- **Diagnóstico "ferramenta não funciona"**:
  1. Verificar se a tool está no `allowed_tools` → `translateAllowedTools`
  2. Verificar se a extension está carregada → `ls ~/.aurelia/pi-agent/npm/`
  3. Verificar se o MCP server está configurado → `cat ~/.aurelia/pi-agent/mcp.json`
  4. **Não confiar em `getActiveToolNames()`** — bug de timing no PI SDK; usar `translateAllowedTools()` para o report.

## Key Packages

| Package | Responsibility |
|---------|---------------|
| `cmd/aurelia/` | Entrypoint, wiring, onboarding |
| `internal/bridge/` | Go client for the TS Bridge process |
| `internal/pipeline/` | Turn driver: prompt + bridge + resilience + run supervisor (no plan/orchestrator) |
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


