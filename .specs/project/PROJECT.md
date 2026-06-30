# Project

## Vision

Aurelia OS is a personality, context, and product layer accessible via Telegram and local terminal surfaces. The goal is not to reimplement what PI or future SDK harnesses already do — it's to **wrap them with product context**, adding identity, Prompt Profiles, persistence, scheduling, multi-project support, and natural conversational interfaces on top. Agentic execution (planning, tools, multi-step work) belongs to the PI SDK — not Aurelia (v0.38.0).

One persistent Go daemon, many projects, many prompt profiles.

## Architectural Thesis

Aurelia is a product/operating layer over the PI SDK engine:

```text
Telegram / TUI / CLI / Cron / future interfaces
        ↓
Aurelia Product Layer
- identity and persona
- Prompt Profile selection/injection (`/mode`, `/agents`, `@profile`)
- Telegram-native UX
- scheduling and cron delivery
- operational memory and continuity context
- user/project/topic scoping
- policy, audit, continuity and scheduling
- tool monitoring and loop defenses
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

The strategic differentiator is the persistent Telegram/TUI product layer: identity, UX, continuity, scheduling, guard-rails and operational context over PI. Transversal Wiki memory is delegated to PI via the existing `ai-memory` MCP instead of being reimplemented inside Aurelia.

## Goals

- **Natural interface** — Talk to an AI assistant via Telegram with text, photos, voice, documents, or locally via TUI when working in the terminal. No raw CLI required for daily use.
- **Prompt Profile selection** — Package each request with the right personality/context profile; PI SDK executes; Aurelia delivers results to Telegram/TUI.
- **Local-first** — Single binary, SQLite, no cloud dependencies beyond LLM providers. Runs on your machine, owns your data.
- **Stay light** — Don't rebuild what the PI SDK already provides. Adapt to it; extend only the product layer.
- **Multi-provider** — Not locked to Anthropic. Support Kimi, OpenRouter, Zai, Alibaba, and whatever comes next.

## Constraints

- **Single user** — Personal assistant, not a multi-tenant platform
- **Primary Telegram UX + local TUI** — Telegram remains the main personal assistant surface; `aurelia-tui` is the local terminal surface for project work. No web UI or external chat platforms yet.
- **Bridge dependency** — LLM reasoning requires Node.js runtime for the PI SDK bridge
- **Cross-platform** — CI and development target macOS, Windows, and Linux
- **No Docker** — Single binary deployment, no container orchestration

## Current State (June 2026 — v0.38.0)

### Core operational
- **Pipeline:** message → prompt assembly → bridge (PI SDK) → reply (Telegram/TUI). No planning mode, no `aurelia-plan`, no Aurelia-side orchestrator.
- Persona: IDENTITY + SOUL + USER; Prompt Profiles via `/mode`, `@profile`, `/agents`
- Cron, multi-modal input (text/photo/voice/docs), session resume via PI `session_file`
- TUI Sprint J complete: IPC, multi-session, vision, attachments, tool activity (`v0.35.0+`)
- Tool monitoring, observability (`run_id`, run_events), security guard-rails, project binding

### Foundation closed (P0–P2)
- Delegate to PI SDK, User Isolation, Operational Observability, Context-Scoped Memory, Memory Boundary Realignment, Session/Profile Operability, Learning Nudge, TUI

### Removed (v0.38.0)
- `internal/orchestrator/`, `aurelia-plan` interception, pending plans, `/execute` — PI SDK owns agentic execution

### Active track (post-v0.38.0)
- Project-scoped memory (`cwd_overlay` by project slug)
- Prompt Profiles Phase 2–3 (user-private profiles)
- Bridge Adapter Interface (`engine.Engine` costura)
- Long-flow UX v2, efficiency audit residual

See `.specs/project/ROADMAP.md` §13 for details.

## Roadmap

Ver `.specs/project/ROADMAP.md`. Resumo:

```
Sprint 0–12  → Foundation + TUI ✅
Sprint C/D   → Orchestration + Plan Mode 🗑️ Removed v0.38.0
Sprint 13    → Active track: project-scoped memory, profiles, bridge adapter
```
