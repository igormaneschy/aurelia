# Multi-SDK — Arquitectura e roadmap unificado

**Status:** Draft — 2026-06-30
**Motivação:** Unificar specs fragmentadas (`bridge-adapter-interface`, `prompt-profiles` Phase 3, `project-work-state`) num plano coerente com a tese de produto debatida em Jun/2026.
**Não é:** segundo SDK concreto, orchestrator, PREVC/dotcontext harness, wiki Aurelia-side.

---

## Tese de produto (fechada)

Aurelia é a **camada de personalidade, contexto operacional e UX** sobre um ou mais **motores de execução** (SDK/harness). Cada motor executa; Aurelia empacota e entrega.

```text
Telegram / TUI / Cron
        ↓
AURELIA (invariável entre SDKs)
  persona · memória operacional · continuidade · guard-rails
  project binding · cron · observabilidade · prompt profiles
        ↓
engine.Engine  (costura — um adapter por harness)
        ↓
PI SDK hoje · outros SDKs depois
        ↓
Tools / FS / Web / projetos
        ↓
REPO + ai-memory MCP (cross-harness, paralelo)
  AGENTS.md/CLAUDE.md · decisões wiki · handoffs
```

### Três camadas de contexto (não confundir)

| Camada | Dono | Escopo | Exemplo |
|---|---|---|---|
| **Produto** | Aurelia | user, chat, projeto (`/cwd`) | persona, `ProjectWorkState`, `cwd_overlay` |
| **Sessão** | SDK activo | `(chatID, threadID, userID, harness)` | histórico PI `session_file` |
| **Projeto durável** | Repo + ai-memory | `projectSlug` / workspace | `AGENTS.md`, wiki decisões, handoff MCP |

**Regra:** Aurelia **não** reimplementa execução agentic. **Não** adopta dotcontext PREVC/orchestrator. **Não** cria MCP wiki competindo com ai-memory.

---

## Problema que este track resolve

1. **Acoplamento PI no pipeline** — `bridge.Request` vaza para ~15 call sites (`bridge-adapter-interface`).
2. **Continuidade fragmentada por superfície** — Telegram e TUI usam `chatID` diferente; factos de projeto já unificados (`cwd_overlay`), trabalho activo não (`project-work-state`).
3. **Harness único implícito** — `profile.Harness` existe no schema mas routing não está implementado (`prompt-profiles` Phase 3).
4. **Prompt sempre Telegram-shaped** — TUI recebe instruções de bot Telegram (corrigido em `project-work-state` P0).
5. **Confusão ai-memory vs Aurelia memory** — utilizador e modelo misturam “onde paramos” (operacional) com “decisões do projeto” (wiki).

---

## Goals do track Multi-SDK

- [ ] Costura `engine.Engine` — pipeline desacoplado do protocolo PI
- [ ] Continuidade cross-surface com `/cwd` — `ProjectWorkState` por `(userID, projectSlug)`
- [ ] Routing `profile.Harness` → adapter registado
- [ ] Separar **entrypoint** (telegram/tui/cron) de **harness** (pi/…)
- [ ] Memória operacional e prompt assembly **SDK-agnósticos** (montados antes do adapter)
- [ ] ai-memory permanece via MCP do harness (PI hoje); estratégia documentada para 2º SDK
- [ ] Segundo harness concreto só após 1–5 estáveis

## Non-Goals

- Plan mode / worker orchestration / `aurelia-plan` (removido v0.38.0)
- Aurelia daemon como gateway MCP ai-memory
- Unificar `chatID` Telegram+TUI numa sessão
- Migrar `.specs/` para `.context/` (dotcontext)
- Auto-handoff ai-memory em cada turno

---

## Fases e dependências

```text
Phase A  project-work-state        cross-surface continuity (PI único)
    ↓
Phase B  bridge-adapter-interface  engine.Engine + PIAdapter
    ↓
Phase C  prompt-profiles Phase 3    HarnessRegistry + routing
    ↓
Phase D  segundo-harness (TBD)     primeiro adapter não-PI
```

| Fase | Spec | Branch sugerida | Entrega |
|---|---|---|---|
| **A** | `project-work-state/` | `feature/project-work-state` | `ProjectWorkState`, prompt por superfície, `entrypoint: tui` |
| **B** | `bridge-adapter-interface/` | `refactor/bridge-adapter-interface` | `internal/engine/`, `PIAdapter`, pipeline sem `bridge.Request` |
| **C** | `prompt-profiles/` §Phase 3 + `multi-sdk/design.md` | `feature/multi-harness-routing` | `HarnessRegistry`, fail-closed, runlog `harness` field |
| **D** | `multi-sdk/second-harness.md` (futuro) | TBD | Adapter #2 + matriz de testes cross-harness |

**Ordem obrigatória:** A pode paralelizar com B apenas se B não mexer em `prompt_builder` de forma conflituosa; **C exige B**. **D exige C**.

---

## O que permanece invariável (qualquer harness)

Montado em `pipeline.buildSystemPrompt` **antes** de `engine.Request`:

1. Runtime identity (Aurelia + harness name quando conhecido)
2. Persona (IDENTITY/SOUL/USER)
3. Surface instructions (Telegram vs TUI vs cron) — `entrypoint`
4. Security boundaries (capability profile — intersecta com hints do profile)
5. **Project Work State** (se `/cwd`) ou **Conversation Continuity** (sem `/cwd`)
6. Persistent memory layers (`user_global`, `topic`, `cwd_overlay`)
7. Effective Prompt Profile (um por turno)
8. User task (`engine.Request.Prompt`)

O adapter **não** remonta persona nem memória operacional. Pode acrescentar context files nativos do SDK (`AGENTS.md` via PI `ResourceLoader`, equivalente noutros motores).

---

## ai-memory no multi-SDK

| Papel | Mecanismo hoje | Multi-SDK |
|---|---|---|
| Decisões/gotchas wiki | MCP `memory_*` via PI extension | Cada harness expõe ai-memory MCP ou sidecar PI |
| Handoff entre sessões | `memory_handoff_*` + hook SessionStart | Igual — opt-in, cross-harness |
| Trabalho activo | **Aurelia `ProjectWorkState`** | Invariável — injectado no `SystemPrompt` |
| Factos operacionais | `cwd_overlay` markdown | Invariável |

**Não mover** “onde paramos nesta thread” para ai-memory como substituto automático de `ProjectWorkState`.

---

## Session model com multi-harness

Chave de sessão PI actual: `(chatID, threadID, userID)` → `session_file`.

Com multi-harness, a chave lógica passa a incluir harness:

```text
SessionKey = (chatID, threadID, userID, harness)
```

- Trocar `@profile` com `harness: pi` → `harness: X` **não** reutiliza a mesma `session_file` PI.
- `ProjectWorkState` **cruza** harnesses no mesmo projeto — é o fio de trabalho partilhado.
- Histórico rico da conversa permanece **por harness+chat**.

---

## User Stories (track completo)

### US-1: Cross-surface, single harness (Phase A)

**Given** trabalho no Telegram com `/cwd`  
**When** continuo na TUI com mesmo `/cwd`  
**Then** `Project Work State` no prompt reflecte o último turno Telegram  
**And** `entrypoint` correcto em cada superfície

### US-2: Pipeline desacoplado (Phase B)

**Given** apenas PI registado  
**When** qualquer turno  
**Then** pipeline usa `engine.Engine`; `grep bridge.Request internal/pipeline` = 0

### US-3: Profile routing (Phase C)

**Given** profile com `harness: pi`  
**When** mensagem processada  
**Then** `PIAdapter` executa; runlog regista `harness=pi`

**Given** profile com `harness: unknown`  
**Then** mensagem local: `Harness "unknown" ainda não está disponível.` — sem chamar motor

### US-4: Cross-harness continuity (Phase C+D)

**Given** turno com `harness: pi` no projeto X  
**When** próximo turno com outro harness no mesmo `/cwd`  
**Then** `ProjectWorkState` ainda visível no prompt  
**And** sessão do novo harness começa fria (sem misturar `session_file`)

---

## Success Criteria (track fechado após Phase C)

- [ ] Telegram ↔ TUI: “onde paramos?” coerente com `/cwd` (Phase A live)
- [ ] `engine.Engine` + `PIAdapter` em produção (Phase B)
- [ ] `profile.Harness` routing com fail-closed (Phase C)
- [ ] Documentação: três camadas de contexto em `.specs/codebase/AGENT_RESPONSIBILITY_MODEL.md`
- [ ] `go test ./... -short` verde em cada fase

Phase D (2º SDK) tem critérios próprios quando o motor for escolhido.

---

## Specs relacionadas

| Documento | Papel no track |
|---|---|
| `.specs/features/project-work-state/` | Phase A |
| `.specs/features/bridge-adapter-interface/` | Phase B |
| `.specs/features/prompt-profiles/` §13 Phase 3 | Phase C |
| `.specs/features/multi-sdk/design.md` | Registry, session key, diagramas |
| `.specs/features/multi-sdk/tasks.md` | DAG executável |
| `.specs/features/delegate-to-pi-sdk/` | ✅ Boundaries já fechados |
| `.specs/features/tui-transport-abstraction/` | ✅ Transport ≠ harness |
| `.specs/features/wiki-memory/` | 🗑️ Superseded por ai-memory MCP |

---

## Decisões explícitas (Jun/2026)

1. **dotcontext** — inspiração pontual (sensores, replay), não dependência; PREVC rejeitado.
2. **Segundo SDK** — escolha adiada; costura primeiro.
3. **ProjectWorkState antes de engine.Engine** — valor imediato no PI único; não bloqueia B.
4. **Cron** usa mesmo pipeline + `entrypoint: cron`; harness default `pi` até Phase C.