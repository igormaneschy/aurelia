# Agent Responsibility Model

**Purpose:** Document the explicit boundary between PI SDK and Aurelia capabilities. This is the canonical reference for what each layer owns.

## Principle

Aurelia is the **product layer** on top of the PI SDK engine. When the PI SDK already owns a capability, Aurelia adapts or orchestrates it rather than reimplementing it. When the capability is product-specific — continuity, memory, policy, UX, or workflow state — Aurelia remains the source of truth.

```text
Telegram / CLI / Cron / future interfaces
        ↓
Aurelia Product Layer
identity · persona · Telegram UX · workflows · memory · Wiki · policies · continuity
        ↓
PI SDK
reasoning · tool execution · sessions · agent runtime · model/provider abstraction
        ↓
Tools / filesystem / web / APIs / projects
```

## PI SDK Owns

| Capability | Details | Implementation |
|---|---|---|
| Model/Provider Resolution | Model selection, fallback, routing | `ModelRegistry.find()` in Bridge |
| Session Runtime | Session lifecycle, compaction via `SettingsManager` | PI SDK manages `session_file`, compaction |
| Tool Execution | Filesystem R/W, search, Bash, MCP tools | PI SDK `agent.tools`, `beforeToolCall` hooks |
| Context File Loading | `CLAUDE.md`, `AGENTS.md`, `SKILL.md` from project | `DefaultResourceLoader(noContextFiles=false)` |
| Skills/Extensions | PI-compatible skills, MCP server support | PI SDK native extension loading |
| Agentic Execution | Reasoning, planning, turn orchestration | PI SDK agent runtime |

## Aurelia Owns

| Capability | Details | Implementation |
|---|---|---|
| Identity | Daemon identity, deployment-level persona (IDENTITY, SOUL) | `internal/persona/` — markdown identity files |
| User Management | User profiles, onboarding, per-user personas (USER.md) | `internal/users/` — Profile, Resolver, Store, Onboarder |
| Telegram UX | Message parsing, progress, reactions, reply flow | `internal/telegram/` — BotController, input pipeline |
| Persistent Memory | Global, user, topic, project-private, team memory layers | `internal/memoryux/`, `internal/dream/` |
| Memory Extraction | Nudge (turn-based extraction), Dream (background consolidation) | `internal/dream/` — Dreamer, safeMemoryWriter |
| Cron Scheduling | Persistent schedule store, cron CLI, Telegram delivery | `internal/cron/` — Scheduler, Store, Runtime |
| Project Binding | `/cwd` persistence, project slug resolution | `internal/projectbinding/` — SQLite store |
| Audit & Observability | Run journal, event timeline, metrics, debug commands | `internal/runlog/`, structured slog logging |
| Orchestration | Plan → workers → validate → commit/PR cycle | `internal/orchestrator/` — ExecutionContext, worktrees |
| Security Governance | Capability profiles, access control, redaction | `internal/security/` — policy engine, bridge hooks |
| Continuity | Durable conversation state recovery | `internal/continuity/` — state store, formatting |
| Wiki (future) | Transversal memory gateway via MCP | `.specs/features/wiki-memory/spec.md` |
| Path Resolution | Canonical runtime paths for all memory layers | `internal/runtime/` — PathResolver |

## Boundary Rules

1. **PI SDK is the engine.** Aurelia does not reimplement model routing, session management, context compaction, or tool execution.
2. **Aurelia is the product.** Identity, UX, persistence, scheduling, governance, and workflow state are Aurelia concerns.
3. **The Bridge is the adapter.** `bridge/index.ts` maps PI SDK primitives to Go-interpretable events and vice versa. It owns no product logic.
4. **Security is layered.** PI SDK enforces tool-call hooks (`beforeToolCall`). Aurelia enforces capability profiles, access control, redaction, and audit.
5. **Memory is Aurelia's domain.** PI SDK has no awareness of Aurelia's memory layers, topics, team memory, or Wiki scopes. Aurelia injects them into the prompt as context.

## Reference

- Architecture thesis: `README.md` (Architectural Thesis section)
- Current evolution track: `.specs/project/ROADMAP.md`
- Memory layer specification: `.specs/features/project-memory/spec.md`
- Wiki memory specification: `.specs/features/wiki-memory/spec.md`
