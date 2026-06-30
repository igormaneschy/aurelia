# Roadmap

## Done / Validated Foundation

Estas features já foram implementadas ou têm validação registrada. Elas são base do roadmap atual.

| Feature | Spec | Status | Notes |
|---------|------|:------:|-------|
| Bridge Recovery | `.specs/features/bridge-recovery/` | Validated | Auto retry with session resume, cooldown after consecutive bridge failures |
| Command Layer | `.specs/features/command-layer/` | Done | Local interception of deterministic commands; avoids unnecessary LLM calls |
| Agent Tools Fix | `.specs/features/agent-tools-fix/` | Validated | `disallowed_tools` honored end-to-end; future governance moved to guard-rails |
| PI Resilience | `.specs/features/pi-resilience/` | Validated | Retry, fallback, circuit breaker, translated actionable errors |
| Dependency Checker | `.specs/features/dependency-checker/` | Validated | Pre-flight checks for Node/npm/git/gh |
| UX Polish | `.specs/features/ux-polish/` | Mostly validated | Ack, queue/status/progress polish, better errors and help |
| **Security Guard-Rails** | `.specs/features/security-guard-rails/` | **✅ 100%** | CapabilityProfile, policy engine, bridge hooks, audit, 44 tests. Profiles: observe→privileged. Fail-closed. |
| **Persistent Project Binding** | `.specs/features/project-binding/` | **✅ 95%** | SQLite store, `/cwd` persistente via restart, fallback tópico→grupo, pipeline resolve. Só falta integração com User Isolation. |
| **TUI** | `docs/aurelia-tui-roadmap.md` | **✅ Sprint J** | `aurelia-tui`, IPC peer auth, multi-sessão, vision, attachments, Charm v2, tool activity (`v0.35.0`). |

---

## Current Evolution Track

Aurelia continua sendo um **personal agent persistente via Telegram e TUI**, com PI SDK como motor de execução agentica e Go como camada de produto: Telegram/TUI UX, identidade/persona, memória operacional, scheduling, project binding, governância e continuidade.

O conceito central está fechado assim:

- **PI SDK owns**: modelo, sessão/compaction, execução de tools, context files do projeto, MCP tools e capacidades agentic nativas.
- **PI + ai-memory MCP owns**: memória Wiki transversal usada diretamente por PI/PI Code/opencode.
- **Aurelia owns**: experiência Telegram/TUI, identidade, memória operacional/produto, cron, multi-projeto, user/project scoping, auditoria e continuidade.
- **Regra de arquitetura**: quando algo já existe no PI SDK, Aurelia adapta — não reimplementa execução agentica, planning mode, worker orchestration ou `aurelia-plan` (removidos em v0.38.0).

Formulação-alvo (evolução multi-SDK — ver `.specs/features/multi-sdk/spec.md`):

```text
Telegram / TUI / Cron
        ↓
Aurelia Product Layer (SDK-agnóstico)
persona · memória operacional · ProjectWorkState · guard-rails · scheduling · prompt profiles
        ↓
engine.Engine  ← costura (Phase B); HarnessRegistry (Phase C)
        ↓
PI SDK hoje · outros harnesses depois
        ↓
Tools / FS / Web / APIs / Projetos
        ↓
Repo (AGENTS.md/CLAUDE.md) + ai-memory MCP (wiki cross-harness)
```

**Três camadas de contexto** (não confundir):

| Camada | Dono | Exemplo |
|---|---|---|
| Produto | Aurelia | persona, `ProjectWorkState`, `cwd_overlay` |
| Sessão | harness activo | `session_file`, histórico PI |
| Projeto durável | repo + ai-memory | decisões wiki, handoffs opt-in |

O objetivo é evitar dois extremos: o Aurelia não deve virar apenas um wrapper fino do PI, nem deve reconstruir o runtime agentic que o PI já entrega. A memória Wiki transversal fica no PI via `ai-memory` MCP; Aurelia mantém contexto operacional (incluindo continuidade cross-surface Telegram↔TUI quando `/cwd` activo).

**Fundação P0–P2 (fechada em v0.38.0):** PI delegation, user isolation, observability, context-scoped memory, memory boundary realignment, session/profile operability, learning nudge, TUI Sprint J, e remoção total de planning mode + orchestrator Aurelia-side.

**Track ativo pós-v0.38.0** (ver também §13; **plano unificado multi-SDK:** `.specs/features/multi-sdk/`):

> **v0.39.0 (2026-06-30):** Prompt Profiles Phase 2 completa — user-private profiles (`profiles.GetForUser`/`ListForUser`), TUI `/mode`+`/agents` parity, pipeline resolver-only path, metadata-safe catalog. Próximo: Project Work State (Phase A). Ver §8b.

~~1. **Higiene documental**~~ — ✅ v0.39.0: ROADMAP/PROJECT/specs superseded alinhados; README já alinhado anteriormente.
2. ~~**Project-scoped memory**~~ — ✅ v0.37.0 + closure v0.38.1; `cwd_overlay` unificado por slug (TUI + Telegram + grupos); spec `.specs/features/project-scoped-memory/`.
3. **Project Work State** — Phase A multi-SDK: continuidade cross-surface (Telegram↔TUI) por `projectSlug` quando `/cwd` activo; spec `.specs/features/project-work-state/`.
4. **Bridge Adapter Interface** — Phase B: costura `engine.Engine` + `PIAdapter`; spec `.specs/features/bridge-adapter-interface/`.
5. **Prompt Profiles Phase 3** — Phase C: multi-harness routing (`profile.Harness` → `engine.Registry`); Phase 2 ✅; spec `.specs/features/prompt-profiles/` + `multi-sdk/design.md`.
6. **Segundo harness** — Phase D (TBD): primeiro adapter não-PI; critérios em `multi-sdk/design.md`.
7. **Long-flow UX v2** — polish Telegram em sessões longas; spec `.specs/features/long-flow-ux-v2/`.
8. **Efficiency audit residual** — project index roots, receipts rotation (maps GC obsoleto após v0.38.0).

**Ordem recomendada:** 3 → 4 → 5 → (6 quando motor escolhido). Itens 3 e 4 podem paralelizar com cuidado em ficheiros distintos.

**Descartados permanentemente (não reabrir):** Plan Mode formal, `aurelia-plan` interception, Aurelia orchestrator, Agent Comms, Auto-Skills — PI SDK owns agentic execution.

> **Nota histórica:** Observability (Sprint B) foi priorizado antes de ampliar execução autônoma porque cada run precisa ser depurável por `run_id`. A execução autônoma Aurelia-side (Sprint C) foi entregue em v0.16.0 e **removida** em v0.38.0 por duplicar o PI SDK.

### Future Quality Gates

CI hardening começou pelos linters high-signal (`errcheck`, `govet`,
`ineffassign`, `staticcheck`, `unused`) com baseline limpo. Reavaliar em sprint
futuro os gates que ficaram fora por ruído de estilo/PT-BR ou constantes
artificiais: `gocritic`, `misspell`, `goconst`, além dos checks de estilo do
`staticcheck` `ST1020`, `ST1016` e `ST1005`.

---

## 0. Delegate to PI SDK Native ✅

**Spec:** `.specs/features/delegate-to-pi-sdk/`
**Tasks:** `.specs/features/delegate-to-pi-sdk/tasks.md`
**Status:** ✅ Concluído em v0.13.7 (2026-05-22)
**Prioridade:** P0 - Fechado

**O que foi entregue:**
- Bridge: `ModelRegistry.find()` + fallback por ID exato.
- Bridge: `SettingsManager.compaction.enabled=true`.
- Bridge: `DefaultResourceLoader(noContextFiles=false)` - PI SDK carrega `CLAUDE.md`/`AGENTS.md`.
- Bridge: Security hooks via `session.agent.beforeToolCall`.
- Go: session store simplificada (session_file em vez de sessionID).
- Go: auto-reset por token threshold removido; PI compaction é fonte de verdade.
- Go: evaluator de policy removido; Bridge é fonte de verdade para enforcement.
- Go: prompt builder delegou loading de context files ao PI SDK.

**Decisões:**
- `internal/agents/` mantido como produto Aurelia. Sem migração para PI SDK.
- `internal/persona/`, `internal/dream/`, `internal/cron/` mantidos.

**Fixes adicionais no fechamento (v0.13.7):**
- Modelo não encontrado → erro claro (não mais log silencioso)
- Auth symlink (credenciais sempre em sync)
- `/stop` com userID
- Config: `omitempty` não perde mais campos sensíveis
- Goroutine `chatActionLoop` com `defer recover()`
- Branch policy: feature/stable/main workflow

**Princípio:** preservar persona, memory, cron, Telegram UX e project binding no Aurelia; delegar engine/session/context/tools/execução agentica ao PI SDK.

---

## 1. User Isolation

**Spec:** `.specs/features/multi-user-profiles/`
**Design:** `.specs/features/multi-user-profiles/design.md`
**Tasks:** `.specs/features/multi-user-profiles/tasks.md`
**Status:** ✅ MVP + runtime hardening auditados em 2026-05-22
**Prioridade:** P0 foundation - fechado para sessão/runtime

**Problem fechado:** A whitelist permite múltiplos `user_id`s. O runtime agora separa sessão PI, cancelamento, status, reset, active commands do Bridge, persona/user memory base, nudge buffer e cron owner por usuário.

**Entregue:**

- `TurnContext` e `SessionKey{chat_id, thread_id, user_id}`;
- `ConversationKey{chat_id, thread_id}` para `/cwd` e project binding compartilhado por conversa/tópico;
- `internal/users/` - Profile, Resolver, Store, Onboarder e SQLite onboarding state;
- `UserGate` antes de comandos/pipeline;
- USER/persona/memória pessoal por usuário;
- cron owner normalizado e lifecycle methods owner-scoped;
- comando CLI `migrate-multi-user` com lock/marker, `--resume` e `--force`;
- `/users`, `/forgetme`, owner-only guards;
- runtime sem chamadas legacy de sessão PI (`sessions.Get/Set/ClearSession/Deactivate/GetWithState`) fora de compat/testes;
- `Cancel`, `WorkStatus`, `CancelAllForUser`, Bridge `get-state/abort/steer/follow-up` e `chatKey` com `user_id`;
- regressões para dois usuários no mesmo chat/thread não compartilharem `session_file`/active run/reset.

**Fora deste sprint:**

- ✅ Memória escopada por contexto conversacional implementada em Sprint E (`Context-Scoped Memory`, v0.20.0). `runtime.TopicMemoryDir` e `TopicCwdOverlayDir` estão em produção.
- O `continuity.Store` permanece `ConversationKey{chat_id, thread_id}` por semântica atual de conversa/tópico. Os patches usam o `session_file` user-scoped correto; continuidade privada por usuário fica como decisão futura antes de Nudge profundo, se necessário.

**Por que era P0:** sem `user_id` propagado integralmente, Auto-Skills, memória e nudge poderiam vazar estado entre usuários autorizados. O caminho crítico de sessão/runtime está fechado.

---

## 2. Operational Observability

**Spec:** `.specs/features/operational-observability/`
**Design:** `.specs/features/operational-observability/design.md`
**Tasks:** `.specs/features/operational-observability/tasks.md`
**Status:** ✅ Implementado em v0.14.0 (2026-05-23)
**Prioridade:** P0 - Fechado

**Problem:** Aurelia já tem `runlog`, `/status`, progresso Telegram, audit log e cron executions, mas a observabilidade é fragmentada. Para depurar produção, ainda é preciso correlacionar manualmente Telegram input, `request_id`, Bridge events, session_file, runlog, audit.log e logs do daemon.

**Scope:**

- `run_id` propagado de Telegram/cron até Bridge/runlog/audit;
- logs estruturados com campos estáveis (`run_id`, `request_id`, `chat_id`, `thread_id`, `user_id`, `phase`);
- expansão de `run_journal` com provider/model/agent/profile/duração/tokens/custo/fallback/timeout/error_class;
- tabela `run_events` com timeline fase-a-fase;
- `/debug` e `aurelia debug` para latest run, run específico, erros recentes e métricas;
- métricas locais por SQLite: sucesso/falha, latência, tokens, custo, fallback, provider/model e cron.

**Por que era P0:** runs longos no PI SDK exigem correlacionar Telegram input, bridge events, runlog e timeline — sem isso, debugging de produção é manual.

---

## 3. Close Orchestration Cycle (Removed)

**Spec:** `.specs/features/agent-orchestration-execution/` (superseded)
**Status:** 🗑️ Removido em v0.38.0 (2026-06-30)

**Decision:** Aurelia-side orchestration (`internal/orchestrator/`, `aurelia-plan` interception, `/execute`) was removed entirely. Agentic execution — planning, tools, multi-step work, human approvals — belongs to the **PI SDK**. The pipeline is message → bridge → reply.

**Historical note:** v0.16.0 shipped a closed orchestration cycle, but the architecture duplicated PI SDK capabilities and assumed Aurelia-specific project layouts. Do not reintroduce.

---

## 4. Plan Mode (Removed)

**Decision (2026-05-24):** Aurelia no longer has a formal Plan Mode. Planning remains conversational and user-driven, case by case.

**What was removed:**
- `internal/planning/` package (types, SQLite store, observer, prompt, discover)
- `/plan*` and `/execute` commands, menu entries, help text
- Plan Mode prompt injection, offer heuristic, planning intent detection
- Artifact observation and reconciliation in the pipeline

**Also removed in v0.38.0 (2026-06-30):**
- Legacy `aurelia-plan` interception, pending plans, `/execute`, and `internal/orchestrator/`
- Agentic execution is owned by the PI SDK; Aurelia delivers bridge results as-is

---

## 5. Context-Scoped Memory

**Spec:** `.specs/features/project-memory/`
**Status:** ✅ Concluído em v0.20.0 (2026-05-28)
**Depende de:** User Isolation (para paths `users/<id>/`)

> **Decisão de design (2026-05-30):** O modelo original `(user_id, project_slug)` foi substituido por `(user_id, context_key)`. O `project_slug` como eixo central da memória impunha uma estrutura de "projeto" que o Aurelia deliberadamente não quer impor - pela mesma razão que o Plan Mode foi removido. A memória deve emergir do contexto conversacional, não de uma entidade formal.

**Problem:** a memória atual é global por `cwd` com detecção automática via `scanForProject`. Com User Isolation, precisa ser escopada por utilizador - mas o eixo correto é o **topic/thread** como escopamento primário natural, com `/cwd` como overlay declarativo e opt-in quando o utilizador quer ancorar a sessão a um diretório de trabalho.

**Modelo de camadas:**

```text
Prompt assembly por TurnContext:
  1. Aurelia persona (IDENTITY + SOUL)     - sempre
  2. User global                           - sempre
  3. Topic memory                          - sempre
  4. CWD overlay (se /cwd declarado)       - opt-in, por tópico
  5. Project team (se /cwd declarado)      - opt-in, compartilhado
```

**Scope:**

- `runtime.PathResolver` com métodos `UserMemoryDir`, `TopicMemoryDir`, `TopicCwdOverlayDir`;
- **remover `scanForProject`** e qualquer travessia automática do filesystem;
- `/cwd` como overlay declarativo persistido por `ConversationKey{chat_id, thread_id}`;
- topic memory em `~/.aurelia/topics/chat_<id>/thread_<id>/` (✅ canonical desde D0);
- cwd overlay em `~/.aurelia/topics/chat_<id>/thread_<id>/cwd_overlay/`;
- project team memory em `~/.aurelia/projects/<slug>/team/` (✅ canonical desde D0) - opcional, só quando `/cwd` ativo;
- prompt assembly com camadas corretas por `TurnContext`;
- dream/nudge com targets escopados por camada.

**Por que antes do nudge:** estas camadas continuam sendo o contrato operacional de contexto do Aurelia; precisam estar corretas antes de qualquer aprendizado em background.

---

## 6. Memory Boundary Realignment

**Spec:** `.specs/features/wiki-memory/`
**Status:** ✅ Concluído como decisão documental em 2026-06-02; Wiki Gateway interno descartado
**Depende de:** User Isolation + Context-Scoped Memory

**Problem:** o plano anterior criava um Wiki MCP interno no Aurelia. Isso duplicaria o `ai-memory` MCP já usado diretamente pelo PI e violaria a regra de não competir com capacidades do PI/MCP.

**Scope:**

- descartar servidor MCP Wiki interno no Aurelia;
- registrar `ai-memory` MCP como camada Wiki transversal no PI;
- separar memória operacional/produto do Aurelia de memória Wiki transversal;
- revisar `project-memory`, `learning-nudge`, `PROJECT.md`, `README.md` e responsibility model;
- não especificar chamadas PI/`ai-memory` não verificadas.

**Princípio:** PI + `ai-memory` MCP owns Wiki memory; Aurelia owns product context and guard-rails.

---

## 7. Session/Profile Operability

**Spec:** `.specs/features/session-profile-operability/`
**Status:** ✅ Concluído em v0.21.0 (2026-06-02)
**Depende de:** User Isolation + Operational Observability/runlog + Project Binding + Security Guard-Rails

**Problem:** antes de aprofundar memória/nudge, Aurelia precisa fechar lacunas operacionais básicas: correlação durável entre Pi session e mensagens Telegram, perfis de modo por usuário, timezone de cron e fallback de cwd para chats privados.

**Scope:**

- Runlog Message Bridge: `inbound_message_id`, `outbound_message_id`, `GetLastOutboundMessage` e nudge threading;
- Mode Profiles: `Profile.ActiveMode`, overlays `mode_<name>.md`, comando `/mode`, listagem ativa e checkpoint tag;
- Profile enrichment: `Profile.Timezone`, `Profile.DefaultCWD`, cron timezone-aware e onboarding de timezone;
- features independentes, sem reimplementar histórico de conversa do PI.

**Prioridade:** P0/P1 - bloqueia Learning Nudge confiável e melhora operação diária de cron/private chats.

---

## 8. Learning Nudge - Scoped Memory Review

**Spec:** `.specs/features/learning-nudge/`
**Status:** ✅ Implementado (v0.9.0–v0.21.1, evolução contínua)
**Depende de:** User Isolation + Context-Scoped Memory + Security Guard-Rails + Memory Boundary Realignment + Session/Profile Operability

**Problem:** extração por-turn/snippet perde contexto; nudge profundo precisa ser escopado para não vazar entre usuários/contextos e não deve depender de uma Wiki interna do Aurelia.

**Scope:**

- transcript recorder por `SessionKey`;
- redaction antes de chamar PI;
- `CapabilityProfile=edit_project`, sem `Bash`;
- sugestões/updates escopados para memória operacional; escrita em Wiki transversal só via PI/`ai-memory` quando houver caminho explicitamente configurado e verificado;
- sugestões de Auto-Skills, sem criar skills automaticamente.

---

## 8b. Prompt Profiles — `/mode`, `/agents`, `@profile`

**Spec:** `.specs/features/prompt-profiles/`
**Status:** 🟡 Parcial — Phase 0–2 ✅ (`internal/profiles`, user-private, TUI/Telegram parity); **Phase 3 = multi-SDK Phase C**
**Depende de:** Session/Profile Operability + Security Guard-Rails + **Bridge Adapter Interface (Phase B)**
**Complementa:** Project Work State (Phase A) — continuidade cross-surface independente do harness

**Problem:** `/mode`, `/agents` e `@agent` estavam conceitualmente próximos demais: todos eram prompt injections/context hints enviados ao SDK, mas a documentação tratava parte deles como “agentes” executores. Isso conflita com a boundary canônica: SDKs executam; Aurelia injeta personalidade, contexto e policy.

**Scope:**

- unificar o conceito como **Prompt Profiles**;
- `/mode <profile>` define o profile padrão;
- `@profile <pedido>` aplica override one-shot;
- `/agents` permanece por compatibilidade, mas vira catálogo de profiles;
- regra de precedência: `@profile` explícito > `/mode` ativo > `general`;
- evitar composição concorrente de overlays fortes por padrão;
- esconder metadata operacional no catálogo público/grupo.

**Princípio:** Aurelia é a camada de personalidade/contexto acima dos harnesses. Profiles empacotam o pedido; SDKs executam.

**Phase 3 (pendente):** `profile.Harness` → `engine.Registry.Resolve()`; fail-closed se harness desconhecido; `run_journal.harness`; sessão keyed por `(chat, thread, user, harness)`. Ver `.specs/features/prompt-profiles/spec.md` §13 e `multi-sdk/design.md`.

---

## 8c. Multi-SDK — Plano unificado

**Spec:** `.specs/features/multi-sdk/`
**Status:** 📋 Draft — 2026-06-30
**Depende de:** Delegate to PI SDK ✅, Prompt Profiles Phase 0–2 ✅, TUI ✅, Project-Scoped Memory ✅

**Problem:** specs fragmentadas (`bridge-adapter-interface`, `prompt-profiles` Phase 3) sem tese de produto unificada; continuidade Telegram↔TUI incompleta apesar de `cwd_overlay` unificado; `entrypoint` vs `harness` não distinguidos.

**Fases:**

| Fase | Feature | Branch sugerida |
|---|---|---|
| A | Project Work State | `feature/project-work-state` |
| B | Bridge Adapter Interface | `refactor/bridge-adapter-interface` |
| C | Harness routing | `feature/multi-harness-routing` |
| D | Segundo harness | TBD |

**Invariante:** `buildSystemPrompt` (persona, memória, `ProjectWorkState`) monta **antes** de qualquer adapter — ver `bridge-adapter-interface/spec.md` §Product Layer Invariants.

**Estimativa até multi-SDK ready (sem 2º motor):** ~7 dias.

---

## 9. Agent Comms (Discarded)

**Spec:** `.specs/features/agent-comms/`
**Status:** 🗑️ Descartado - 2026-06-02
**Decision:** Agent-to-agent communication between workers is the PI SDK's
responsibility, not Aurelia's. Aurelia manages Telegram messages, identity,
operational memory, and guard-rails. Worker orchestration (task decomposition,
subtask execution, inter-worker messaging) belongs to the PI SDK runtime.
Keeping Agent Comms in Aurelia would duplicate PI SDK capabilities and violate
the architectural boundary established in Sprint 0 (Delegate to PI SDK).

---

## 10. Auto-Skills (Discarded)

**Spec:** `.specs/features/auto-skills/`  
**Status:** 🗑️ Descartado — 2026-06-02  
**Decision:** Skill creation, loading, and execution are PI SDK responsibilities.
Aurelia's job is to know WHO is talking (identity/persona), in WHAT mode
(developer/research/general), and with WHAT context (memory layers + /cwd).
The execution engine (tools, skills, task decomposition) belongs to the PI SDK
runtime. Duplicating skill management in Aurelia would violate the architectural
boundary established in Sprint 0 (Delegate to PI SDK).

---

## 11. TUI — Transport Abstraction (Fase 0)

**Spec:** `.specs/features/tui-transport-abstraction/`
**Status:** ✅ Validated
**Implementado em:** `feature/tui-transport-abstraction`
**Depende de:** Session/Profile Operability (Sprint G, ✅)

**Problem:** o `pipeline.Output` e sua única implementação (`telegramPipelineOutput`) misturavam comportamento genérico de envio de mensagens com comportamento específico do Telegram (reações com emoji, deleção de mensagens, progresso baseado em edição, execução de planos via `BotController`). Construir a TUI diretamente sobre esse código forçaria no-ops forçados ou vazamento de tipos `telebot`.

**Scope:**

- tornar `pipeline.Output` transport-agnóstico;
- estender `transport.Transport` com `MessageHandle` e capacidades opcionais (`DeletableTransport`, `ReactableTransport`);
- criar `transportOutput` — implementação genérica de `Output` sobre qualquer `transport.Transport`;
- refatorar `telegramPipelineOutput` para thin adapter sobre `TelegramTransport`;
- atualizar todos os fakes de teste de `Output` e `Transport`;
- zero regressão no Telegram.

**Decisão:** `MessageHandle` opaco (`any`); capabilities via type assertion; `transportOutput` genérico em `internal/pipeline/transport_output.go`.

---

## 5.5. Project-Scoped CWD Overlay ✅

**Spec:** `.specs/features/project-scoped-memory/`
**Status:** ✅ Validated (v0.37.0 code, v0.38.1 closure)

**Problem:** `cwd_overlay` fragmentado por `(chatID, threadID)` — TUI e Telegram não partilhavam factos de projeto com o mesmo `/cwd`.

**Entregue:**

- `cwd_overlay` em `~/.aurelia/projects/<slug>/cwd_overlay/` (independente de chat/thread);
- migração `migrate-cwd-overlay`;
- `topic` memory permanece por conversa; `user_global` inalterado.

**Gap remanescente (Sprint L):** continuidade de *trabalho activo* (goal, intent, checkpoint) ainda por `chatID` — ver §13 Phase A (`project-work-state`).

---

## 13. Active Track (post-v0.38.0)

**Plano unificado:** `.specs/features/multi-sdk/` (spec + design + tasks)

| Phase | Priority | Spec | Status | Entrega |
|-------|:--------:|------|:------:|---------|
| — | — | `project-scoped-memory/` | ✅ | `cwd_overlay` por `projectSlug` |
| **A** | **P0** | `project-work-state/` | 📋 Draft | `ProjectWorkState` cross-surface; prompt TUI; `entrypoint: tui` |
| **B** | **P1** | `bridge-adapter-interface/` | 📋 Draft | `internal/engine/` + `PIAdapter`; pipeline sem `bridge.Request` |
| **C** | **P1** | `prompt-profiles/` Phase 3 + `multi-sdk/design.md` | 🟡 Partial | `HarnessRegistry`; `profile.Harness` routing; session+harness |
| **D** | **P2** | `multi-sdk/` (2º harness TBD) | ⏳ | Primeiro adapter não-PI — motor por escolher |
| — | P2 | `long-flow-ux-v2/` | Proposed | Polish sessões longas Telegram |
| — | P3 | Efficiency audit | Partial | project index roots, receipts rotation |

**Ordem obrigatória:** A → B → C → (D). A e B podem paralelizar com cuidado (ficheiros distintos).

**Superseded specs (do not implement):** `agent-orchestration-execution/`, `plan-mode-architecture/`, `auto-skills/`, `agent-comms/`, `wiki-memory/` (gateway interno).

**Rejeitado como dependência:** dotcontext PREVC/orchestrator — inspiração pontual apenas (sensores, replay).

---

## 12. TUI (Terminal User Interface)

**Spec:** `docs/aurelia-tui-roadmap.md` + `.specs/features/tui-transport-abstraction/`
**Status:** ✅ Sprint J completo — Fases 0–5 mergeadas em `main` (`v0.35.0`)
**Depende de:** TUI — Transport Abstraction (Fase 0, ✅) + Context-Scoped Memory (Sprint E, ✅) + Memory Boundary Realignment (Sprint F, ✅) + Session/Profile Operability (Sprint G, ✅)

**Problem:** o Telegram é hoje o único ponto de entrada conversacional da Aurelia. Isso cria fricção no contexto de terminal, sessões não retomáveis cross-surface e dependência de conectividade externa.

**Scope:**

- IPC layer via Unix socket para comunicação com daemon (Fase 1);
- TUI MVP com Bubble Tea: sidebar, viewport, input, streaming (Fase 2);
- multi-sessão local com namespace reservado (Fase 3);
- painel de estado do projeto (cwd, memória, checkpoints) (Fase 4);
- image input, document attachments, polish Charm v2, `--session`, tool activity (Fases 4.5–5);
- IPC peer UID auth (`SO_PEERCRED` / `LOCAL_PEERCRED`) (Fase 1, concluída).

**Decisão:** Unix socket + JSON lines no MVP; gRPC em P2. Bubble Tea/Lipgloss/Bubbles/Glamour v1 no MVP para reduzir risco de migração de module paths; migração Charm v2 fica para polish/distribuição. Binary separado `aurelia-tui`.

---

## Sequenciamento resumido

```text
Foundation (P0–P2) ✅  →  v0.38.0
      │
      ▼
5.5 Project-Scoped cwd_overlay ✅  (v0.37–v0.38.1)
      │
      ▼
┌─────────────────────────────────────────────────────┐
│  Active Track — Multi-SDK (post-v0.38.0)            │
│  Plano: .specs/features/multi-sdk/                  │
├─────────────────────────────────────────────────────┤
│  Phase A  Project Work State      📋 próximo        │
│  Phase B  engine.Engine + PIAdapter                 │
│  Phase C  HarnessRegistry + profile.Harness         │
│  Phase D  2º harness (TBD)                          │
└─────────────────────────────────────────────────────┘
      │
      ├──→ long-flow UX v2 (paralelo, P2)
      └──→ efficiency audit (paralelo, P3)
```

**Fundação histórica (completa):**

```text
0. Delegate to PI SDK ✅ → 1. User Isolation ✅ → 2. Observability ✅
→ ~~3. Orchestration~~ 🗑️ → D0 Memory hygiene ✅ → 5. Context Memory ✅
→ 6. Memory Boundary ✅ → 7. Session/Profile ✅ → 8b. Prompt Profiles 🟡
→ 8. Nudge ✅ → 11. Transport Abstraction ✅ → 12. TUI ✅
```

## Mapa de implementação por sprint

```
Sprint 0: Delegate to PI SDK Native
  ├─ ✅ Bridge: simplify model resolution
  ├─ ✅ Bridge: PI compaction + PI context-file loading
  ├─ ✅ Go: remove policy evaluator duplication; keep config/profile types
  ├─ ✅ Go: simplify session store around PI session_file
  ├─ ✅ Go: remove auto-reset/token-threshold lifecycle
  ├─ 🟡 Go: prompt builder reduced, but still owns Aurelia persona/memory/Telegram sections
  ├─ 🟡 Decision superseded by Prompt Profiles: keep legacy `internal/agents` as compatibility loader for Aurelia Prompt Profiles; investigate SDK-native mappings via adapters later
  └─ 🟡 Validation/docs: E2E specialist + stale specs cleanup

Sprint A: User Isolation MVP + runtime hardening
  ├─ ✅ TurnContext + SessionKey/ConversationKey
  ├─ ✅ internal/users/ (Profile, Resolver, Store, Onboarder)
  ├─ ✅ CLI migrate-multi-user
  ├─ ✅ cron owner normalizado
  ├─ ✅ session isolation + persona per-user
  ├─ ✅ memory/dream per-user base
  ├─ ✅ pipeline integration + UserGate
  ├─ ✅ owner-only commands
  ├─ ✅ CancelAllForUser + active run/cancel/status/get-state user-scoped
  └─ ➡️ Context-scoped memory (topic + cwd overlay) movida para Sprint E

Sprint B: Operational Observability (T0-T12 do tasks.md) ✅ v0.14.0
  ├─ ✅ RunContext + field map
  ├─ ✅ slog estruturado configurável
  ├─ ✅ run_journal expandido
  ├─ ✅ run_events timeline
  ├─ ✅ pipeline/Bridge retry/fallback/timeout events
  ├─ ✅ /status com run_id curto
  ├─ ✅ aurelia debug CLI
  ├─ ✅ /debug owner-only
  └─ ✅ métricas locais por SQLite

Sprint C: ~~Close Orchestration Cycle~~ 🗑️ Removed v0.38.0
  Entregue em v0.16.0, removido por duplicar PI SDK (ver §3).
  Não reintroduzir `internal/orchestrator/` nem `aurelia-plan`.

Sprint D0: Memory Contract & Spec Hygiene ✅ v0.16.1
  ├─ AGENT_RESPONSIBILITY_MODEL.md - canonical PI↔Aurelia boundary
  ├─ project-memory spec: topic path `~/.aurelia/topics/`, team path `~/.aurelia/projects/<slug>/team/`
  ├─ wiki-memory spec: layers aligned with project-memory (superseded later by PI + ai-memory MCP)
  ├─ ARCHITECTURE.md: remove stale orchestration statements (cycle wired since v0.16.0)
  ├─ STACK.md: Go 1.26.3, Node >=20.6.0
  ├─ memoryux/ + pipeline/ topic dirs canonicalized to `~/.aurelia/topics/`
  ├─ runtime: ProjectTeamMemoryDir → `~/.aurelia/projects/<slug>/team/`
  └─ README: memory layers updated, Node.js >=20.6.0

Sprint D: ~~Plan Mode (T-1-T13 do tasks.md)~~  🗑️ Removido 2026-05-24
  Planejamento permanece conversacional, sem Plan Mode explícito.
  internal/planning/ removido. /plan* removidos.
  Orquestrador e aurelia-plan removidos em v0.38.0 — PI SDK executa.

Sprint E: Context-Scoped Memory ✅ v0.20.0
  ├─ ✅ runtime.PathResolver: UserMemoryDir, TopicMemoryDir, TopicCwdOverlayDir
  ├─ ✅ remover scanForProject e travessia automática do filesystem
  ├─ ✅ /cwd como overlay declarativo por ConversationKey (sem auto-detecção)
  ├─ ✅ cwd_overlay em ~/.aurelia/topics/<chat>/<thread>/cwd_overlay/
  ├─ ✅ prompt assembly: user_global + topic + cwd_overlay + team
  └─ ✅ dream/nudge com targets escopados por camada

Sprint F: Memory Boundary Realignment ✅ docs/decision
  ├─ ✅ descartar MCP Wiki interno
  ├─ ✅ registrar PI + ai-memory MCP como Wiki transversal
  ├─ ✅ separar memória operacional do Aurelia de Wiki memory do PI
  └─ ✅ ajustar specs dependentes principais

Sprint G: Session/Profile Operability ✅
  ├─ Runlog Message Bridge: completar nudge threading/fallback se ainda pendente
  ├─ Profile fields: ActiveMode, Timezone, DefaultCWD
  ├─ /mode + mode overlays no prompt
  ├─ cron timezone-aware
  ├─ DefaultCWD fallback só em private chat
  └─ onboarding timezone

Sprint G2: Prompt Profiles 🟡 (Phase 0–2 ✅)
  ├─ ✅ `/mode` = default Prompt Profile
  ├─ ✅ `@profile` = one-shot Prompt Profile override
  ├─ ✅ `/agents` = compatible Prompt Profile catalog
  ├─ ✅ `internal/profiles` + user-private profiles (Phase 2)
  ├─ ✅ TUI `/mode` + `/agents` parity
  └─ ⏳ Phase 3 harness routing → Sprint N

Sprint H: Learning Nudge ✅ (v0.9.0–v0.21.1)
  ├─ ✅ transcript recorder por SessionKey
  ├─ ✅ redaction + profile edit_project
  └─ ✅ sugestões/updates escopados; Wiki transversal só via PI + ai-memory quando configurado

Sprint K: TUI ✅ (v0.27.1–v0.35.0)
  ├─ ✅ Fase 0: Transport Abstraction
  ├─ ✅ Fase 1: IPC Layer + peer UID auth
  ├─ ✅ Fase 2: TUI MVP
  ├─ ✅ Fase 3: Multi-sessão local
  ├─ ✅ Fase 4: Painel de Estado do Projeto
  ├─ ✅ Fases 4.5–4.6: Image input + document attachments
  └─ ✅ Fase 5: Polish Charm v2, distribuição, tool activity

Sprint K2: Project-Scoped Memory ✅ (v0.37.0–v0.38.1)
  ├─ ✅ cwd_overlay por projectSlug (TUI + Telegram + grupos)
  ├─ ✅ migrate-cwd-overlay
  └─ ⏳ gap: continuidade activa ainda por chatID → Sprint L

Sprint L: Project Work State — Multi-SDK Phase A 📋
  ├─ ⏳ ProjectWorkState (userID + projectSlug) em continuity.db
  ├─ ⏳ dual-write turn lifecycle quando /cwd activo
  ├─ ⏳ prompt: Project Work State vs Conversation Continuity
  ├─ ⏳ buildSurfaceInstructions (TUI sem bloco Telegram)
  ├─ ⏳ entrypoint: tui no runlog
  └─ ⏳ live: Telegram → TUI "onde paramos?"

Sprint M: Bridge Adapter Interface — Multi-SDK Phase B 📋
  ├─ ⏳ internal/engine/ (Engine, Request, Event, MockEngine)
  ├─ ⏳ bridge/adapter.go (PIAdapter)
  ├─ ⏳ pipeline sem bridge.Request em produção
  └─ ⏳ ARCHITECTURE.md + product layer invariants

Sprint N: Multi-Harness Routing — Multi-SDK Phase C 📋
  ├─ ⏳ engine.Registry + wire app.go
  ├─ ⏳ profile.Harness → Resolve (fail-closed)
  ├─ ⏳ SessionKey + harness; migração sessões → pi
  ├─ ⏳ run_journal.harness + debug CLI
  └─ ⏳ ProjectWorkState invariável ao trocar harness

Sprint O: Segundo Harness — Multi-SDK Phase D ⏳
  └─ ⏳ motor TBD; spec dedicada quando Igor escolher
```

## Nota de implementação incremental

A base incremental de `User Isolation` já foi entregue e auditada:

```text
TurnContext
SessionKey com user_id
ConversationKey para /cwd e project binding
internal/users/
UserGate
cron owner
migração CLI
active run / Bridge commands user-scoped
```

O próximo trabalho é o **track Multi-SDK Phase A** (`project-work-state`): fechar continuidade Telegram↔TUI no mesmo `/cwd`, antes da costura `engine.Engine`. Assumir `user_id` real em todos os caminhos novos.

## Backlog futuro

- **Multi-SDK Phase D:** segundo harness (escolha pendente)
- **ai-memory UX:** reforço handoff/long-flow; comando `/handoff` opcional (sem gateway Go)
- **Sensores/evidência** inspirados em dotcontext (cron, validação pós-turno) — não PREVC
- Human approval flow para guard-rails ambíguos
- OS sandbox para Bridge
- Project history/favorites para `/cwd`
- TUI: gRPC local para múltiplas surfaces (desktop app, VS Code extension)

**Backlog descartado:** Cross-device Agent Comms, Auto-Skills, Plan Mode, Aurelia orchestrator, Wiki MCP interno.

## Notas de visão

Aurelia é **personal agent persistente** via Telegram (primário) e TUI (terminal). Não é IDE, SaaS multi-tenant, nem harness universal tipo dotcontext.

- **Go / Aurelia:** produto — persona, memória operacional, continuidade cross-surface, UX, cron, guard-rails.
- **engine.Engine:** costura para um ou mais SDKs; PI é o motor hoje.
- **ai-memory MCP:** wiki e handoffs cross-harness (via tools do motor, não daemon Go).
- **Repo:** regras de execução (`AGENTS.md`, `.specs/`).

Ver `.specs/features/multi-sdk/spec.md` e `.specs/codebase/AGENT_RESPONSIBILITY_MODEL.md`.
