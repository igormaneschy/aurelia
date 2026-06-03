# Project Structure

**Root:** `aurelia/`

## Directory Tree

```
.
├── .air.toml                  # Hot reload config
├── .github/workflows/         # CI: test, lint, vulncheck, gitleaks
├── AGENTS.md                  # Architecture docs for AI assistants
├── README.md                  # Project overview & setup
├── go.mod / go.sum            # Go dependencies
├── mcp_servers.example.json   # MCP config template
│
├── cmd/aurelia/               # CLI entry points
│   ├── main.go                # Subcommand dispatcher
│   ├── app.go                 # Dependency wiring & lifecycle
│   ├── cron_cli.go            # Cron job management CLI
│   └── telegram_cli.go        # Telegram message CLI
│
├── bridge/                    # TypeScript bridge source
│   ├── index.ts               # PI SDK adapter (NDJSON runtime)
│   ├── bundle.js              # Compiled JS (embedded in Go binary)
│   ├── package.json           # SDK dependency
│   └── tsconfig.json
│
├── internal/                  # Core application packages
│   ├── agents/                # Legacy Prompt Profile registry & @profile routing
│   ├── bridge/                # Go↔TS IPC client (PI SDK)
│   ├── config/                # App configuration
│   ├── cron/                  # Scheduled jobs (SQLite-backed)
│   ├── deps/                  # Runtime dependency checks (Node/npm/git/gh)
│   ├── dream/                 # Background memory consolidation + nudges
│   ├── onboarding/            # Interactive setup wizard
│   ├── orchestrator/          # Workflow orchestration (plan/workers/validate/git)
│   ├── persona/               # Identity & prompt assembly
│   ├── pipeline/              # Turn processing service (resilience + active-run tracking)
│   ├── runtime/               # Path resolution
│   ├── session/               # PI session_file resume, cwd state, nudge buffers
│   ├── telegram/              # Telegram bot I/O
│   └── version/               # Build version constant
│
├── pkg/stt/                   # Public: speech-to-text client
│
└── e2e/                       # End-to-end tests
```

## Module Organization

### cmd/aurelia — Application Entry
**Purpose:** CLI commands, dependency wiring, process lifecycle
**Key files:** `main.go` (dispatch), `app.go` (bootstrap + start/stop)

### internal/telegram — Telegram Bot Interface
**Purpose:** All Telegram I/O — input handling, output formatting, markdown rendering, commands
**Key files:** `bot.go` (controller), `input_pipeline.go` (message flow), `output.go` (event processing), `send.go` (chunked delivery), `orchestration.go` (executeApprovedPlan), `worker_status.go` (per-worker status messages)

### internal/pipeline — Turn Processing Service
**Purpose:** Encapsulates a single conversational turn — prompt assembly, bridge call, event handling, recovery, plan detection. Extracted from the Telegram controller so the same logic powers cron jobs and other entrypoints.
**Key files:** `service.go` (entrypoint + active-run tracking), `pipeline.go` (event loop + plan dispatch), `prompt_builder.go`, `resilient_bridge.go` (retry + circuit breaker), `planning_intent.go` (heuristic plan-mode detection)

### internal/orchestrator — Workflow Orchestration
**Purpose:** Plan→workers→validate cycle. Detects `aurelia-plan` blocks in bridge responses, spawns workers per wave in isolated git worktrees, validates results via a quality gate, and contains scaffold for docs/tasks/git delivery. The full closed cycle is still roadmap work.
**Key files:** `orchestrator.go` (struct + BridgeExecutor interface), `plan.go` (Plan/Task model + topological ExecutionOrder), `extract.go` (parse `aurelia-plan` blocks), `execute.go` (wave execution + ExecuteTask), `validate.go` (quality gate), `worktree.go` (git worktree CRUD), `defaults.go` (worker fallback + ResolveAgentConfig), `prompt.go` (TLC + worker + validation prompt builders), `agents_md.go` (`AGENTS.md`/`CLAUDE.md` generators), `tasks_status.go` (update `.specs/.../tasks.md` checkboxes), `git.go` (commit + `gh pr create`)

### internal/bridge — LLM Bridge Client
**Purpose:** Manages TypeScript process, NDJSON protocol, request multiplexing. The bridge adapts the PI SDK (`@earendil-works/pi-coding-agent`) — Aurelia treats PI as its core inference/execution engine.
**Key files:** `bridge.go` (process + IPC), `protocol.go` (types), `events.go` (event model), `embed.go` (bundle embedding)

### internal/cron — Job Scheduler
**Purpose:** Persistent scheduled job execution with SQLite storage
**Key files:** `scheduler.go` (polling loop), `store.go` + `store_*.go` (SQLite CRUD), `runtime.go` (bridge execution), `delivery.go` (Telegram delivery)

### internal/dream — Background Memory Consolidation
**Purpose:** Lightweight Haiku-driven nudge that reviews recent turns and writes consolidated facts to memory. Runs out-of-band so it never blocks user-facing turns.
**Key files:** `dream.go` (lifecycle), `nudge.go` (turn buffer), `prompt.go` (consolidation prompt), `prompts/` (markdown templates)

### internal/persona — Identity Management
**Purpose:** Load persona files and assemble system prompts
**Key files:** `canonical_service.go` (prompt builder), `loader.go` (file parser)

### internal/agents — Legacy Prompt Profile Registry
**Purpose:** Load legacy markdown profiles from `~/.aurelia/agents/` and support `@profile` compatibility. Conceptually these files inject context/personality into SDK requests; SDK harnesses execute.
**Key files:** `registry.go` (loading + routing), `types.go` (legacy Agent/Profile struct with `AllowedTools`/`DisallowedTools`/`MaxTurns`)

### internal/onboarding — Setup Wizard
**Purpose:** Interactive first-run flow — provider/model selection, API keys, persona, dependency checks
**Key files:** `onboard.go` (entrypoint), `onboard_providers.go`, `onboard_catalog.go`, `onboard_ui.go`

### internal/deps — Runtime Dependency Checks
**Purpose:** Verify Node, npm, git and `gh` are installed and meet minimum versions; surface clear errors before the bridge fails opaquely
**Key files:** `check.go`

### internal/version — Build Version
**Purpose:** Single source of truth for the release version string

## Where Things Live

**Telegram message processing:**
- Input handlers: `internal/telegram/input.go`, `input_pipeline.go`
- Turn execution: `internal/pipeline/service.go`, `pipeline.go`
- Output processing: `internal/telegram/output.go`
- Message sending: `internal/telegram/send.go`
- Markdown→HTML: `internal/telegram/markdown*.go`
- Orchestrated plans: `internal/telegram/orchestration.go`, `worker_status.go`

**Workflow orchestration:**
- Plan detection: `internal/orchestrator/extract.go` (called from `internal/pipeline/pipeline.go`)
- Wave execution: `internal/orchestrator/execute.go`
- Worktrees: `internal/orchestrator/worktree.go`
- Quality gate: `internal/orchestrator/validate.go`
- Git delivery: `internal/orchestrator/git.go`

**LLM communication:**
- Go client: `internal/bridge/bridge.go`
- TS wrapper: `bridge/index.ts` (PI SDK)
- Protocol: `internal/bridge/protocol.go`, `events.go`
- Resilience: `internal/pipeline/resilient_bridge.go`, `circuit_breaker.go`

**Persistence:**
- Database: `internal/cron/store*.go` (SQLite)
- Sessions: `internal/session/store.go` (in-memory mapping to PI `session_file` for resume)
- Config: `internal/config/config.go` (JSON file)
- Personas: `internal/persona/loader*.go` (markdown files)

**Runtime configuration:**
- App config: `~/.aurelia/config/app.json`
- Prompt profile defs: `~/.aurelia/agents/*.md` (legacy path; conceptually Prompt Profiles)
- Personas: `~/.aurelia/memory/personas/` plus per-user `~/.aurelia/users/<id>/personas/USER.md`
- Database: `~/.aurelia/data/cron.db`
