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

---

## Current Evolution Track

Aurelia continua sendo um **personal agent persistente via Telegram**, com PI como motor de execução e Go como camada de produto: Telegram UX, identidade/persona, memória operacional, scheduling, project binding, governância e orquestração.

O conceito central está fechado assim:

- **PI SDK owns**: modelo, sessão/compaction, execução de tools, context files do projeto, MCP tools e capacidades agentic nativas.
- **PI + ai-memory MCP owns**: memória Wiki transversal usada diretamente por PI/PI Code/opencode.
- **Aurelia owns**: experiência Telegram, identidade, memória operacional/produto, cron, multi-projeto, user/project scoping, auditoria, roadmap e workflows.
- **Regra de arquitetura**: quando algo já existe no PI, Aurelia só adapta/orquestra; não reimplementa.

Formulação-alvo:

```text
Telegram / CLI / Cron / Interfaces
        ↓
Aurelia Product Layer
identidade · persona · UX · workflows · memória operacional · políticas · continuidade
        ↓
PI SDK
reasoning · tools · sessions · agent runtime · providers/models
        ↓
Ferramentas / FS / Web / APIs / Projetos
```

O objetivo é evitar dois extremos: o Aurélia não deve virar apenas um wrapper fino do PI, nem deve reconstruir o runtime agentic que o PI já entrega. A memória Wiki transversal agora fica no PI via `ai-memory` MCP; Aurelia mantém apenas o contexto operacional necessário para UX, continuidade e workflows Telegram.

A próxima onda foca em tornar o sistema seguro e estável para trabalho autônomo em projetos reais:

1. manter fechado o hardening pós-v0.13 do limite PI↔Aurelia;
2. criar base de observabilidade operacional antes de ampliar execução autônoma;
3. usar a base de User Isolation já auditada para fechar o ciclo de execução orquestrada;
4. planejamento permanece conversacional, sem Plan Mode explícito (removido em 2026-05-24);
5. escopar memória por utilizador e contexto conversacional - **topic/thread como eixo primário, `/cwd` como overlay declarativo opt-in** (reformulado em 2026-05-30; ver Sprint 5);
6. realinhar a boundary de memória, descartando o Wiki MCP interno em favor de PI + `ai-memory` MCP;
7. fechar operabilidade de sessão/perfil antes de ampliar memória/nudge: message bridge, timezone, default cwd e mode profiles;
8. só então ativar nudge profundo, agent comms e auto-skills.

**Ordem é importante:** cada spec depende da anterior, técnica e conceitualmente. O refactoring do PI SDK pode ser feito em paralelo com User Isolation, mas deve ser merged antes para reduzir a superfície de código.

> **Nota sobre o delta real:** Security Guard-Rails e Project Binding já foram implementados (revisão de Maio 2026), então o roadmap foi reordenado para refletir o estado real da codebase. Antes de fechar Orchestration, entrou uma fundação curta de Observability porque execução autônoma só é segura se cada run puder ser depurado por `run_id`, timeline, provider/model, tokens/custo, erro e fase de falha.

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
- `internal/persona/`, `internal/dream/`, `internal/cron/`, `internal/orchestrator/` mantidos.

**Fixes adicionais no fechamento (v0.13.7):**
- Modelo não encontrado → erro claro (não mais log silencioso)
- Auth symlink (credenciais sempre em sync)
- `/stop` com userID
- Config: `omitempty` não perde mais campos sensíveis
- Goroutine `chatActionLoop` com `defer recover()`
- Branch policy: feature/stable/main workflow

**Princípio:** preservar persona, memory, cron, Telegram UX, project binding e orchestrator no Aurelia; delegar engine/session/context/tools ao PI.

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

- `run_id` propagado de Telegram/cron/orchestration até Bridge/runlog/audit;
- logs estruturados com campos estáveis (`run_id`, `request_id`, `chat_id`, `thread_id`, `user_id`, `phase`);
- expansão de `run_journal` com provider/model/agent/profile/duração/tokens/custo/fallback/timeout/error_class;
- tabela `run_events` com timeline fase-a-fase;
- `/debug` e `aurelia debug` para latest run, run específico, erros recentes e métricas;
- métricas locais por SQLite: sucesso/falha, latência, tokens, custo, fallback, provider/model e cron.

**Por que agora:** Orchestration e workflows autônomos vão aumentar muito a complexidade operacional. Antes de executar workflows mais longos, precisamos conseguir responder rapidamente "qual run falhou, em que fase, com qual modelo, custo, tools e erro?".

---

## 3. Close Orchestration Cycle

**Spec:** `.specs/features/agent-orchestration-execution/`
**Design:** `.specs/features/agent-orchestration-execution/design.md`
**Tasks:** `.specs/features/agent-orchestration-execution/tasks.md`
**Status:** ✅ Implementado em v0.16.0 (2026-05-24)
**Depende de:** Operational Observability (✅); User Isolation runtime hardening (✅); Project Binding (✅)

**Problem:** Aurelia já tem `internal/orchestrator/` com worktree, waves, git.go, validate.go, tasks_status.go (80% do código), mas **o ciclo não fecha**: `Validate`, `CommitChanges`, `CreatePR`, `UpdateTasksStatus` não são chamados no fluxo real. `currentBranch()` retorna hardcoded `"HEAD"`. Thread ID é perdido no handoff. O executor funcional prometido pela spec nunca foi entregue.

**Scope:**

- `ExecutionContext` com cwd persistente, thread/user/security context;
- git preflight (recusa dirty base, detached HEAD);
- validation com diff/verify real + retry com feedback;
- merge serial com dependentes skipped;
- update `tasks.md`, commit seguro e PR opcional;
- orphan worktree cleanup no startup;
- artifact collection + manifest.

**Por que agora:** o scaffold já existe e ~40% do esforço total foi investido, mas o ciclo não fecha. O handoff entre planejamento conversacional e execução precisa funcionar. É mais rápido conectar o que já existe do que reconstruir depois.

---

## 4. Plan Mode (Removed)

**Decision (2026-05-24):** Aurelia no longer has a formal Plan Mode. Planning remains conversational and user-driven, case by case.

**What was removed:**
- `internal/planning/` package (types, SQLite store, observer, prompt, discover)
- `/plan*` and `/execute` commands, menu entries, help text
- Plan Mode prompt injection, offer heuristic, planning intent detection
- Artifact observation and reconciliation in the pipeline

**What was preserved:**
- Orchestrator and `tryExecutePlan` for legacy/conversational `aurelia-plan` execution
- `sanitizeExecutionPlanForChat` - still sanitizes invalid plan blocks
- `ExecuteApprovedPlan` on the Output interface (used by orchestrator)

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
**Status:** 🟡 Parcial — MVP semantics implementado em v0.21.0; Phase 1 (`internal/profiles`) pendente
**Depende de:** Session/Profile Operability + Security Guard-Rails + Bridge Adapter Interface

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

## 12. TUI (Terminal User Interface)

**Spec:** `docs/aurelia-tui-roadmap.md` + `.specs/features/tui-transport-abstraction/`
**Status:** 🔴 Proposta
**Depende de:** TUI — Transport Abstraction (Fase 0, ✅) + Context-Scoped Memory (Sprint E, ✅) + Memory Boundary Realignment (Sprint F, ✅) + Session/Profile Operability (Sprint G, ✅)

**Problem:** o Telegram é hoje o único ponto de entrada conversacional da Aurelia. Isso cria fricção no contexto de terminal, sessões não retomáveis cross-surface e dependência de conectividade externa.

**Scope:**

- IPC layer via Unix socket para comunicação com daemon (Fase 1);
- TUI MVP com Bubble Tea: sidebar, viewport, input, streaming (Fase 2);
- multi-sessão local + retomada de sessões Telegram (Fase 3);
- painel de estado do projeto (cwd, artefatos, checkpoints) (Fase 4);
- polish: temas, mouse, resize, flags `--session`/`--attach` (Fase 5).

**Decisão:** Unix socket + JSON lines no MVP; gRPC em P2. Bubble Tea v2 + Lipgloss + Bubbles + Glamour. Binary separado `aurelia-tui`.

---

## Sequenciamento resumido

```text
Foundation validada (Security Guard-Rails + Project Binding + Bridge Resilience)
      │
      ├──→ 0. Delegate to PI SDK Native core ✅
      │
      ▼
1. User Isolation MVP + runtime hardening ✅
      │
      ▼
2. Operational Observability ✅
      │
      ▼
3. Close Orchestration Cycle ✅
      │
      ▼
D0. Memory Contract & Spec Hygiene ✅
      │
      ▼
5. Context-Scoped Memory ✅
      │
      ▼
6. Memory Boundary Realignment ✅
      │
      ▼
 7. Session/Profile Operability ✅
      │
      ▼
 8b. Prompt Profiles 🟡
      │
      ▼
 8. Learning Nudge ✅
      │
      ▼
  11. TUI — Transport Abstraction (Fase 0) ✅
       │
       ▼
  12. TUI (Terminal User Interface) 🔴
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

Sprint C: Close Orchestration Cycle (T0-T12 do tasks.md) ✅ v0.16.0
  ├─ ✅ ExecutionContext com cwd+threadID
  ├─ ✅ git preflight
  ├─ ✅ artifact collection + verify command
  ├─ ✅ fail-closed validation com retry
  ├─ ✅ merge serial + skip dependents
  ├─ ✅ commit + PR + tasks.md update
  ├─ ✅ orphan cleanup no startup
  └─ ✅ integration smoke test

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
  internal/planning/ removido. /plan* e /execute removidos.
  Orquestrador e aurelia-plan preservados para execução legada.

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

Sprint G2: Prompt Profiles 🟡 (parcial — v0.21.0)
  ├─ ✅ `/mode` = default Prompt Profile
  ├─ ✅ `@profile` = one-shot Prompt Profile override (via `agents.Route`)
  ├─ ✅ `/agents` = compatible Prompt Profile catalog
  ├─ ✅ no default composition of mode overlay + agent prompt
  └─ ✅ metadata-safe catalog in groups/non-owner contexts
  └─ ⏳ `internal/profiles` package abstraction (Phase 1 da spec)

Sprint H: Learning Nudge ✅ (v0.9.0–v0.21.1)
  ├─ ✅ transcript recorder por SessionKey
  ├─ ✅ redaction + profile edit_project
  └─ ✅ sugestões/updates escopados; Wiki transversal só via PI + ai-memory quando configurado

Sprint K: TUI
  ├─ ✅ Fase 0: Transport Abstraction (~2d) — implementado em `feature/tui-transport-abstraction`
  ├─ Fase 1: IPC Layer (3d)
  ├─ Fase 2: TUI MVP (5d)
  ├─ Fase 3: Multi-sessão (4d)
  ├─ Fase 4: Painel de Estado do Projeto (3d) - cwd, artefatos, checkpoints
  └─ Fase 5: Polish + Distribuição (3d)
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

O próximo trabalho deve assumir `user_id` real no handoff e evitar novos caminhos com `userID=0`, exceto compatibilidade/testes. Sprint E foi concluído em v0.20.0; `TopicMemoryDir` e `TopicCwdOverlayDir` estão em produção desde então.

## Backlog futuro

- Cross-device Agent Comms seguro
- Human approval flow para guard-rails ambíguos
- OS sandbox para Bridge
- Project history/favorites para `/cwd`
- Team memory sync via git
- TUI: gRPC local para múltiplas surfaces (desktop app, VS Code extension)

## Notas de visão

Aurelia ocupa o nicho de **personal agent persistente via Telegram**, com TUI como interface secundária local. Não é IDE, não é SaaS multi-tenant, não é apenas coding agent. PI SDK é o motor de inferência/execução; Go é a camada de orquestração, segurança, memória, persistência e UX (Telegram + TUI).
