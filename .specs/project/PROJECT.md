# Project

## Vision

Aurelia OS is an autonomous agent operating system accessible via Telegram. The goal is not to reimplement what PI already does — it's to **orchestrate it**, adding persistence, scheduling, multi-project support, and a natural Telegram interface on top.

One persistent Go daemon, many projects, many agents.

## Architectural Thesis

Aurelia is a product/operating layer over the PI SDK engine:

```text
Telegram / CLI / Cron / future interfaces
        ↓
Aurelia Product Layer
- identity and persona
- Telegram-native UX
- workflows and conversational planning
- operational memory and continuity context
- user/project/topic scoping
- policy, audit, continuity and scheduling
        ↓
PI SDK
- reasoning and tool execution
- sessions and compaction
- model/provider abstraction
- agent runtime, skills and extensions
        ↓
Tools / filesystem / web / APIs / projects
```

The architectural rule is: **delegate engine capabilities to PI; keep product continuity in Aurelia**. Aurelia must not become a thin PI wrapper, but it also must not rebuild model routing, session compaction, context loading, tool execution or MCP-backed memory when PI already provides them.

The strategic differentiator is the persistent Telegram product layer: identity, UX, continuity, scheduling, orchestration, guard-rails and operational context over PI. Transversal Wiki memory is delegated to PI via the existing `ai-memory` MCP instead of being reimplemented inside Aurelia.

## Goals

- **Natural interface** — Talk to an AI assistant via Telegram with text, photos, voice, documents. No CLI required for daily use.
- **Agent orchestration** — Route messages to specialist agents, schedule autonomous execution, deliver results back to Telegram.
- **Local-first** — Single binary, SQLite, no cloud dependencies beyond LLM providers. Runs on your machine, owns your data.
- **Stay light** — Don't rebuild what the PI SDK already provides. Wrap it, orchestrate it, extend it.
- **Multi-provider** — Not locked to Anthropic. Support Kimi, OpenRouter, Zai, Alibaba, and whatever comes next.

## Constraints

- **Single user** — Personal assistant, not a multi-tenant platform
- **Telegram-only interface** — No web UI, no other chat platforms (for now)
- **Bridge dependency** — LLM reasoning requires Node.js runtime for the PI SDK bridge
- **Cross-platform** — CI and development target macOS, Windows, and Linux
- **No Docker** — Single binary deployment, no container orchestration

## Current State (May 2026)

### Core operational
- Core loop working: Telegram → Agent routing → Bridge → PI SDK → Response
- Persona system: IDENTITY.md + SOUL.md + USER.md assembled into system prompts
- Cron scheduler: SQLite-backed, recurring and one-time jobs, Telegram delivery
- Multi-modal input: text, photos (albums), voice (Groq STT), documents
- Session continuity: resume via PI `session_file`; context pruning delegated to PI SDK compaction
- Agent registry: markdown-defined Aurelia specialists with model/tool/MCP overrides (migration to PI-native agents remains open)
- Onboarding CLI: interactive setup for providers, tokens, and configuration
- Vision model fallback + Groq STT + bridge image format (PI SDK compatible)

### Recently completed (v0.11.0–v0.16.0)
- **User Isolation MVP + runtime hardening**: user profiles, owner gate, per-user persona/memory loading, user-scoped sessions/active runs/Bridge commands, cron ownership, `/users`, `/forgetme`, migration CLI.
- **Delegate to PI SDK Native — core slice**: PI model resolution, PI context-file loading, PI compaction, `session_file` resume, Bridge-side session lifecycle (`steer`/`followUp`/`abort`).
- **Security Guard-Rails**: CapabilityProfile governance, PI tool_call hooks in the Bridge, audit trail, fail-closed. 5 profiles: observe→privileged.
- **Persistent Project Binding**: SQLite-backed `/cwd` that survives restart, topic→group fallback, explicit clear, pipeline integration.
- **Continuity Engine v1**: Persistent conversation state, progressive summarization, checkpoint/run journal.
- **UX Polish**: Streaming text, idle timeout, live progress metrics, `/stop`, `/status`, queue system, Telegram ack flow.
- **Bridge Resilience**: Circuit breaker, retry with backoff, translated error messages, scanner-based NDJSON with 10MB limit.
- **CI Hardening**: Lint gates (`errcheck`, `govet`, `staticcheck`, `unused`), security scan (`gosec`), local parity via `make check`.
- **Operational Observability v0.14.0**: `run_id` correlation, structured `slog`, expanded `run_journal`, `run_events` timeline, `/debug` CLI/Telegram commands, local metrics.
- **Session Lifecycle Manager v0.15.0**: Health states (healthy/large/suspect/dangerous/cold), auto-decisions (continue/compact/rotate/cold_resume), bridge commands (`get-session-stats`, `compact-session`, `rotate-session`), failure metadata persistence.
- **Close Orchestration Cycle v0.16.0**: `ExecutionContext` with cwd/threadID, git preflight, artifact collection, fail-closed validation with retry, serial merge, dependency skip, commit + optional PR, `ExecutionManifest`.

### In progress
- Closing the conceptual boundary: PI owns model/session/context/tool execution; Aurelia owns Telegram UX, identity/persona, persistence, scheduling, memory, project binding, policy/audit and orchestration.
- Agent registry boundary decision: keep Aurelia specialists as a product-layer feature for now; investigate PI-native parsing/discovery later via `agentsFilesOverride` rather than forcing a user-facing migration.
- Memory boundary realignment: project memory scopes remain Aurelia operational context; transversal Wiki memory is handled by PI via `ai-memory` MCP.

## Roadmap

Ver `.specs/project/ROADMAP.md` para o sequenciamento completo. Resumo:

```
Sprint 0 → Delegate to PI SDK Native core ✅; remaining: agent registry boundary decision
Sprint A → User Isolation MVP + runtime hardening ✅; remaining user×project memory moved to Sprint E
Sprint B → Operational Observability (run_id, timeline, /debug, métricas locais)
Sprint C → Close Orchestration Cycle (conectar scaffold existente)
Sprint D → ~~Plan Mode Architecture~~ 🗑️ Removido 2026-05-24; planejamento conversational
Sprint E → User-Scoped Project Memory
Sprint F → Memory Boundary Realignment (PI + ai-memory MCP, no internal Wiki Gateway)
Sprint G → Learning Nudge escopado
Sprint H → Agent Comms
Sprint I → Auto-Skills
```
