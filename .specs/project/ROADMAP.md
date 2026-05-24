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

Aurelia continua sendo um **personal agent persistente via Telegram**, com PI como motor de execução e Go como camada de produto: Telegram UX, identidade/persona, memória, scheduling, project binding, governança e orquestração.

O conceito central está fechado assim:

- **PI SDK owns**: modelo, sessão/compaction, execução de tools, context files do projeto e capacidades agentic nativas.
- **Aurelia owns**: experiência Telegram, identidade, memória persistente, cron, multi-projeto, user/project scoping, auditoria, roadmap e workflows.
- **Regra de arquitetura**: quando algo já existe no PI, Aurelia só adapta/orquestra; não reimplementa.

Formulação-alvo:

```text
Telegram / CLI / Cron / Interfaces
        ↓
Aurelia Product Layer
identidade · persona · UX · workflows · memória · Wiki · políticas · continuidade
        ↓
PI SDK
reasoning · tools · sessions · agent runtime · providers/models
        ↓
Ferramentas / FS / Web / APIs / Projetos
```

O objetivo é evitar dois extremos: o Aurélia não deve virar apenas um wrapper fino do PI, nem deve reconstruir o runtime agentic que o PI já entrega. A Wiki é parte central dessa estratégia: ela transforma memória em conhecimento operacional transversal, local-first e auditável.

A próxima onda foca em tornar o sistema seguro e estável para trabalho autônomo em projetos reais:

1. manter fechado o hardening pós-v0.13 do limite PI↔Aurelia;
2. criar base de observabilidade operacional antes de ampliar execução autônoma;
3. usar a base de User Isolation já auditada para fechar o ciclo de execução orquestrada;
4. transformar plan mode em modo explícito, persistente e retomável;
5. escopar memória por usuário/projeto com semântica estável;
6. promover a memória para uma Wiki transversal via MCP;
7. só então ativar nudge profundo, agent comms e auto-skills.

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
**Prioridade:** P0 — Fechado

**O que foi entregue:**
- Bridge: `ModelRegistry.find()` + fallback por ID exato.
- Bridge: `SettingsManager.compaction.enabled=true`.
- Bridge: `DefaultResourceLoader(noContextFiles=false)` — PI SDK carrega `CLAUDE.md`/`AGENTS.md`.
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
**Prioridade:** P0 foundation — fechado para sessão/runtime

**Problem fechado:** A whitelist permite múltiplos `user_id`s. O runtime agora separa sessão PI, cancelamento, status, reset, active commands do Bridge, persona/user memory base, nudge buffer e cron owner por usuário.

**Entregue:**

- `TurnContext` e `SessionKey{chat_id, thread_id, user_id}`;
- `ConversationKey{chat_id, thread_id}` para `/cwd` e project binding compartilhado por conversa/tópico;
- `internal/users/` — Profile, Resolver, Store, Onboarder e SQLite onboarding state;
- `UserGate` antes de comandos/pipeline;
- USER/persona/memória pessoal por usuário;
- cron owner normalizado e lifecycle methods owner-scoped;
- comando CLI `migrate-multi-user` com lock/marker, `--resume` e `--force`;
- `/users`, `/forgetme`, owner-only guards;
- runtime sem chamadas legacy de sessão PI (`sessions.Get/Set/ClearSession/Deactivate/GetWithState`) fora de compat/testes;
- `Cancel`, `WorkStatus`, `CancelAllForUser`, Bridge `get-state/abort/steer/follow-up` e `chatKey` com `user_id`;
- regressões para dois usuários no mesmo chat/thread não compartilharem `session_file`/active run/reset.

**Fora deste sprint:**

- Memória privada de projeto por `(user_id, project_slug)` continua no Sprint E (`User-Scoped Project Memory`). Hoje há memória pessoal por usuário, mas `runtime.ProjectMemoryDir/ConversationProjectMemoryDir` ainda é principalmente cwd/chat/thread-scoped.
- O `continuity.Store` permanece `ConversationKey{chat_id, thread_id}` por semântica atual de conversa/tópico. Os patches usam o `session_file` user-scoped correto; continuidade privada por usuário fica como decisão futura antes de Nudge profundo, se necessário.

**Por que era P0:** sem `user_id` propagado integralmente, Plan Mode, Auto-Skills, memória e nudge poderiam vazar estado entre usuários autorizados. O caminho crítico de sessão/runtime está fechado.

---

## 2. Operational Observability

**Spec:** `.specs/features/operational-observability/`  
**Design:** `.specs/features/operational-observability/design.md`  
**Tasks:** `.specs/features/operational-observability/tasks.md`  
**Status:** ✅ Implementado em v0.14.0 (2026-05-23)  
**Prioridade:** P0 — Fechado

**Problem:** Aurelia já tem `runlog`, `/status`, progresso Telegram, audit log e cron executions, mas a observabilidade é fragmentada. Para depurar produção, ainda é preciso correlacionar manualmente Telegram input, `request_id`, Bridge events, session_file, runlog, audit.log e logs do daemon.

**Scope:**

- `run_id` propagado de Telegram/cron/orchestration até Bridge/runlog/audit;
- logs estruturados com campos estáveis (`run_id`, `request_id`, `chat_id`, `thread_id`, `user_id`, `phase`);
- expansão de `run_journal` com provider/model/agent/profile/duração/tokens/custo/fallback/timeout/error_class;
- tabela `run_events` com timeline fase-a-fase;
- `/debug` e `aurelia debug` para latest run, run específico, erros recentes e métricas;
- métricas locais por SQLite: sucesso/falha, latência, tokens, custo, fallback, provider/model e cron.

**Por que agora:** Orchestration e Plan Mode vão aumentar muito a complexidade operacional. Antes de executar workflows autônomos mais longos, precisamos conseguir responder rapidamente “qual run falhou, em que fase, com qual modelo, custo, tools e erro?”.

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

**Por que agora:** o scaffold já existe e ~40% do esforço total foi investido, mas o ciclo não fecha. Plan Mode precisa do handoff funcionando. É mais rápido conectar o que já existe do que reconstruir depois.

---

## 4. Plan Mode Architecture

**Spec:** `.specs/features/plan-mode-architecture/`
**Design:** `.specs/features/plan-mode-architecture/design.md`
**Tasks:** `.specs/features/plan-mode-architecture/tasks.md`
**Status:** 🟡 Parcial (detecção heurística + parsing de `aurelia-plan` existem)
**Depende de:** User Isolation + Operational Observability + Orchestration Cycle (ExecutionContext, handoff)

**Problem:** hoje planejamento é implícito: keywords disparam prompt de orquestração sem permissão, e `aurelia-plan` executa sem estado persistente. Precisa virar modo explícito, persistente e retomável.

**Scope:**

- **Fase 0 — Transport Abstraction (T-1):** extrair interface `Transport` do Telegram; desacoplar pipeline do transport específico; preparar base para TUI e outros surfaces;
- `/plan`, `/plan status`, `/plan cancel`, `/plan list`;
- state persistente em SQLite por `SessionKey`;
- discovery baseado no project binding;
- materialização observada via Write/Edit (`observer.go`);
- offer-only heuristic (oferece, nunca impõe);
- handoff seguro para executor via `ExecutionContext`.

**Por que depois da orquestração:** Plan Mode produz o plano que o executor consome. O handoff depende do `ExecutionContext` e do preflight definidos na Orchestration spec.

---

## 5. User-Scoped Project Memory

**Spec:** `.specs/features/project-memory/`
**Status:** 🟡 Parcial (70% — layers existem, mas não são isoladas por usuário)
**Depende de:** User Isolation (para paths `users/<id>/`)

**Problem:** a versão atual de memória por projeto é global por `cwd`. Com User Isolation, precisa ser escopada por `(user_id, project_slug)`. As camadas já existem (global, topic, project-private, team), mas os paths não são per-user.

**Scope:**

- user global memory em `~/.aurelia/users/<id>/memory/`;
- user × project private memory em `~/.aurelia/users/<id>/projects/<slug>/memory/`;
- project team memory em `~/.aurelia/projects/<slug>/team/` (✅ canonical desde D0);
- topic memory em `~/.aurelia/topics/chat_<id>/thread_<id>/` (✅ canonical desde D0);
- prompt assembly com camadas corretas por `TurnContext`;
- runtime.PathResolver com métodos `User*`.

**Por que antes da Wiki:** a Wiki MCP vai expor as mesmas camadas; precisa estar correta primeiro.

---

## 6. Wiki Memory Gateway

**Spec:** `.specs/features/wiki-memory/`
**Status:** 🔴 Spec arquitetural apenas
**Depende de:** User Isolation + Project Memory

**Problem:** a memória atual é útil dentro do Aurelia, mas não é transversal para outros pontos de entrada como PI/PI Code/opencode.

**Scope:**

- Wiki como LLM Wiki local-first do Aurelia;
- MCP gateway para query/save/ingest/lint/status;
- markdown como fonte auditável, SQLite/FTS como índice opcional;
- escopos fortes: user, user×project private, project team, topic, procedural;
- query-before-inject para reduzir prompt bloat;
- receipts/audit para escritas externas.

**Princípio:** acesso transversal, memória escopada.

---

## 7. Learning Nudge — Scoped Memory Review

**Spec:** `.specs/features/learning-nudge/`
**Status:** 🔴 Draft revisado
**Depende de:** User Isolation + Project Memory + Security Guard-Rails + Wiki Memory Gateway

**Problem:** extração por-turn/snippet perde contexto; nudge profundo precisa ser escopado para não vazar entre usuários/projetos e deve escrever através da Wiki.

**Scope:**

- transcript recorder por `SessionKey`;
- redaction antes de chamar PI;
- `CapabilityProfile=edit_project`, sem `Bash`;
- escrita nas camadas de memória corretas via Wiki;
- sugestões de Auto-Skills, sem criar skills automaticamente.

---

## 8. Agent Comms

**Spec:** `.specs/features/agent-comms/`
**Status:** 🔴 Draft
**Depende de:** Orchestration Cycle + Security Guard-Rails

**Problem:** workers especializados ganham qualidade quando podem consultar peers, mas precisa ser local, auditado e com limites.

**Scope:**

- Agent Bus local por run;
- peers explícitos por task;
- anti-loop/budget/timeouts;
- payload policy e audit;
- manifest com peer message counts;
- sem rede/cross-device no MVP.

**Por que depois da execução:** é melhoria da orquestração, não requisito para o primeiro executor seguro.

---

## 9. Auto-Skills

**Spec:** `.specs/features/auto-skills/`
**Status:** 🔴 Draft revisado
**Depende de:** User Isolation + Security Guard-Rails; ganha valor com as features anteriores

**Problem:** tarefas bem-sucedidas viram conhecimento perdido; Auto-Skills transforma procedimentos úteis em skills privadas, PI-compatible (`SKILL.md`), gerenciadas pelo Aurelia.

**Scope:**

- recorder de último turno bem-sucedido;
- `/skill save <slug>` explícito;
- geração via LLM sem tools;
- validação rígida de frontmatter Agent Skills/PI + adapter Aurelia;
- storage privado por user em layout `<slug>/SKILL.md`;
- `capability_profile` obrigatório/validado;
- registry per-user.

**Decisão:** Opção A — Aurelia-native, PI-compatible. Não usar `pi-hermes-memory` nem escrever em `~/.pi/agent` no MVP.

---

## 10. TUI (Terminal User Interface)

**Spec:** `docs/aurelia-tui-roadmap.md`
**Status:** 🔴 Proposta
**Depende de:** Plan Mode (Sprint D) + User-Scoped Project Memory (Sprint E) + Wiki Memory Gateway (Sprint F)

**Problem:** o Telegram é hoje o único ponto de entrada conversacional da Aurelia. Isso cria fricção no contexto de terminal, sessões não retomáveis cross-surface e dependência de conectividade externa.

**Scope:**

- abstração de transport (`Transport` interface) — Fase 0, pode ser feita antes;
- IPC layer via Unix socket para comunicação com daemon;
- TUI MVP com Bubble Tea: sidebar, viewport, input, streaming;
- multi-sessão local + retomada de sessões Telegram;
- Plan Mode visual com painel de artefatos;
- polish: temas, mouse, resize, flags `--session`/`--attach`.

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
4. Plan Mode Architecture
      │
      ▼
5. User-Scoped Project Memory
      │
      ▼
6. Wiki Memory Gateway
      │
      ▼
7. Learning Nudge
      │
      ▼
8. Agent Comms
      │
       ▼
9. Auto-Skills
       │
       ▼
10. TUI (Terminal User Interface)
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
  ├─ 🟡 Decision: keep internal/agents as Aurelia product feature for now; investigate PI-native discovery via agentsFilesOverride
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
  └─ ➡️ User×project private memory movida para Sprint E

Sprint B: Operational Observability (T0–T12 do tasks.md) ✅ v0.14.0
  ├─ ✅ RunContext + field map
  ├─ ✅ slog estruturado configurável
  ├─ ✅ run_journal expandido
  ├─ ✅ run_events timeline
  ├─ ✅ pipeline/Bridge retry/fallback/timeout events
  ├─ ✅ /status com run_id curto
  ├─ ✅ aurelia debug CLI
  ├─ ✅ /debug owner-only
  └─ ✅ métricas locais por SQLite

Sprint C: Close Orchestration Cycle (T0–T12 do tasks.md) ✅ v0.16.0
  ├─ ✅ ExecutionContext com cwd+threadID
  ├─ ✅ git preflight
  ├─ ✅ artifact collection + verify command
  ├─ ✅ fail-closed validation com retry
  ├─ ✅ merge serial + skip dependents
  ├─ ✅ commit + PR + tasks.md update
  ├─ ✅ orphan cleanup no startup
  └─ ✅ integration smoke test

Sprint D0: Memory Contract & Spec Hygiene ✅ v0.16.1
  ├─ AGENT_RESPONSIBILITY_MODEL.md — canonical PI↔Aurelia boundary
  ├─ project-memory spec: topic path `~/.aurelia/topics/`, team path `~/.aurelia/projects/<slug>/team/`
  ├─ wiki-memory spec: layers aligned with project-memory
  ├─ ARCHITECTURE.md: remove stale orchestration statements (cycle wired since v0.16.0)
  ├─ STACK.md: Go 1.26.3, Node >=20.6.0
  ├─ memoryux/ + pipeline/ topic dirs canonicalized to `~/.aurelia/topics/`
  ├─ runtime: ProjectTeamMemoryDir → `~/.aurelia/projects/<slug>/team/`
  └─ README: memory layers updated, Node.js >=20.6.0

Sprint D: Plan Mode (T-1–T13 do tasks.md)
  ├─ T-1: Transport Abstraction (2d) — pode antecipar; zero regressão
  ├─ T0–T13: internal/planning/ types + SQLite store
  ├─ context discovery
  ├─ BuildPlanningPrompt + observer
  ├─ offer-only heuristic
  ├─ /plan commands
  └─ handoff com ExecutionContext

Sprint E: User-Scoped Project Memory
  ├─ runtime.PathResolver per-user
  ├─ prompt assembly com camadas corretas
  └─ dream/nudge com targets escopados

Sprint F: Wiki Memory Gateway
  ├─ MCP server interno
  ├─ wiki_query/save/status
  └─ query-before-inject

Sprint G: Learning Nudge
  ├─ transcript recorder por SessionKey
  ├─ redaction + profile edit_project
  └─ escrita via Wiki scopes

Sprint H: Agent Comms
  ├─ Agent Bus local por run
  ├─ peers explícitos + limites
  └─ manifest + audit

Sprint I: Auto-Skills
  ├─ skill recorder
  ├─ /skill save + generator
  ├─ validator de SKILL.md
  └─ registry per-user

Sprint J: TUI
  ├─ Fase 0: Transport Abstraction (2d) — pode antecipar
  ├─ Fase 1: IPC Layer (3d)
  ├─ Fase 2: TUI MVP (5d)
  ├─ Fase 3: Multi-sessão (4d)
  ├─ Fase 4: Plan Mode Visual (4d)
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

O próximo trabalho deve assumir `user_id` real no handoff e evitar novos caminhos com `userID=0`, exceto compatibilidade/testes. `Orchestration Cycle` já pode usar `ExecutionContext` com `user_id` propagado.

## Backlog futuro

- Cross-device Agent Comms seguro
- Human approval flow para guard-rails ambíguos
- OS sandbox para Bridge
- Project history/favorites para `/cwd`
- Team memory sync via git
- TUI: gRPC local para múltiplas surfaces (desktop app, VS Code extension)

## Notas de visão

Aurelia ocupa o nicho de **personal agent persistente via Telegram**, com TUI como interface secundária local. Não é IDE, não é SaaS multi-tenant, não é apenas coding agent. PI SDK é o motor de inferência/execução; Go é a camada de orquestração, segurança, memória, persistência e UX (Telegram + TUI).
