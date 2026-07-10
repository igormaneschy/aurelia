# Long Flow UX v2 — Especificação

**Status:** Implemented  
**Data:** 2026-05-26  
**Implementado:** v0.19.6 (2026-05-26)  
**Revisão spec:** 2026-07-10 — alinhamento com código em `main` (v0.39.2)  
**Origem:** análise dos logs de chat/daemon dos últimos 2 dias + revisão do `CHANGELOG.md` v0.15.0 e v0.19.2–v0.19.5.

## Problem Statement

As proteções recentes para sessões longas melhoraram a confiabilidade do Aurelia, mas algumas ficaram visíveis demais no Telegram e criaram uma experiência confusa em fluxos longos.

O caso mais evidente é a compactação síncrona no início de uma nova mensagem:

```text
Usuário envia um pedido
→ Aurelia envia “🧠 Compactando histórico longo...”
→ espera a compactação terminar
→ só então começa o pedido real
```

Isso expõe manutenção interna antes de reconhecer o trabalho solicitado. Além disso, a PI SDK já possui compaction habilitada no Bridge, então o Go não deve duplicar a responsabilidade de compactação normal da sessão.

Outros sintomas observados:

1. mensagens técnicas como “ferramentas”, “calls” e contadores internos aparecem ao usuário;
2. loop detector envia texto que parece instrução interna ao modelo: “Pare o que está fazendo...”;
3. heartbeat fala de “chamadas de ferramenta” em vez de progresso humano;
4. lifecycle/runlog tem observabilidade incompleta, dificultando auditoria futura de UX.

## Goals

- [x] Deixar a PI SDK gerenciar a compactação normal de sessões saudáveis.
- [x] Remover compactação Go síncrona automática do caminho saudável antes do pedido principal.
- [x] Manter Go responsável por segurança operacional: `cold_resume`, `rotate`, recuperação após timeout, empty result e process death.
- [x] Reduzir mensagens técnicas no Telegram durante execução longa.
- [x] Separar instruções internas ao modelo (`steer`) das mensagens visíveis ao usuário.
- [x] Preservar segurança de sessão em estados perigosos ou suspeitos.
- [x] Cobrir as decisões com testes de lifecycle e textos de UX.
- [ ] Observabilidade/runlog completa (deferred — ver secção abaixo).

## Non-Goals

- Alterar o PI SDK upstream.
- Alterar `bridge/index.ts` ou rebuildar o bundle nesta fase.
- Criar um status manager completo.
- Resolver todos os problemas de observabilidade/runlog nesta fase.
- Mudar auth, model registry, cron, Telegram handlers ou config global.
- Automatizar deploy, restart daemon, promoção para `stable/*` ou merge em `main`.

## Prior Lessons Applied

- `delegate-to-dependency`: não reimplementar compactação que a PI SDK já fornece via `SettingsManager.compaction`.
- `accessory-feature-core-regression`: proteger o caminho principal de execução; features auxiliares não devem bloquear o pedido normal.
- `post-impl-review-gaps`: mudança não trivial precisa de revisão backend pós-implementação.
- `daemon-log-visibility`: validação live deve conferir logs do daemon após deploy.
- `telegram-threadid-explicit`: qualquer mensagem Telegram deve preservar `threadID`.

---

## Architecture Decision

### Decision 1: PI SDK owns normal compaction

O Go não deve compactar automaticamente uma sessão ativa e saudável apenas porque `input_tokens >= compact_after_input_tokens`.

**Antes:**

```text
active + healthy + large tokens → ActionCompact → mensagem Telegram → compact-session → pedido
```

**Depois:**

```text
active + healthy + large tokens → ActionContinue → PI SDK decide se compacta internamente
```

Rationale:

- a Bridge já cria sessões PI com compaction habilitada;
- compactação síncrona bloqueia o pedido do usuário;
- duplicar threshold/decisão no Go aumenta divergência com a SDK;
- Go deve orquestrar continuidade e segurança, não microgerenciar pruning normal.

### Decision 2: Go still owns recovery and dangerous lifecycle

Go continua decidindo ações de segurança quando há sinais de sessão problemática.

Estados que continuam exigindo intervenção Go:

- sessão fria/inativa;
- empty result recente;
- process death recente;
- timeouts e falhas suspeitas;
- tokens em limite perigoso, quando rotação é mais segura que continuar.

**As-implemented (pós v0.19.6):** rotação automática saiu de `EvaluateLifecycle` e passou para `TokenGuard` em `applyLifecycle`. Sessões suspect/dangerous por empty result ou process death fazem `ActionColdResume` (preservam `session_file` original) em vez de `ActionRotate` automático — regressão evitada no incidente 2026-06-01 (~371k tokens). Rotação imediata ocorre apenas quando `input_tokens >= rotate_after_input_tokens` via `TokenGuard`.

### Decision 3: User messages must describe progress, not internals

Mensagens padrão ao usuário não devem expor:

- nomes de ferramentas;
- contagem de tool calls;
- comandos internos para o modelo;
- detalhes de session lifecycle, exceto quando houver uma ação inevitável e perceptível.

### Decision 4: TokenGuard — fallback de emergência (pós-implementação)

`internal/session/token_guard.go` escala além de `ActionContinue` apenas quando a PI SDK falha em reduzir tokens em tempo:

1. **Hard ceiling:** `input_tokens >= rotate_after_input_tokens` → `ActionRotate` imediato.
2. **Stall:** N turnos consecutivos acima de `compact_after` sem redução ≥5% → `ActionCompact` (fallback).

Este caminho **não** é o default saudável. Mensagem ao usuário usa “Preparando o contexto...”, não “Compactando histórico longo...”. Testes: `token_guard_test.go`, `token_guard_lifecycle_test.go`.

---

## User Stories

### P1: Sessão saudável longa não compacta no caminho crítico — ✅

**User Story:** Como usuário, quando envio uma nova mensagem em uma conversa longa porém saudável, quero que o Aurelia comece o trabalho sem mostrar manutenção interna antes do pedido.

**Acceptance Criteria:**

1. WHEN a sessão está ativa e sem falhas recentes AND `input_tokens >= compact_after_input_tokens` AND `input_tokens < rotate_after_input_tokens` THEN lifecycle SHALL return `ActionContinue`. ✅
2. WHEN `ActionContinue` é retornado THEN o pedido SHALL seguir para o Bridge sem mensagem de compactação enviada pelo Go. ✅
3. WHEN a PI SDK compacta internamente durante a execução THEN the Go SHALL not emit a proactive “compactando histórico” message from lifecycle. ✅

**Independent Test:** `TestEvaluateLifecycle_LargeInputTokens`, `TestApplyLifecycle_HealthyContinue`.

---

### P1: Sessão fria tem prioridade sobre compactação — ✅

**User Story:** Como usuário, quando volto depois de um tempo/restart, quero uma retomada segura, não uma compactação bloqueante antes do meu pedido.

**Acceptance Criteria:**

1. WHEN a sessão está inativa/cold THEN lifecycle SHALL return `ActionColdResume`. ✅
2. WHEN uma sessão cold também tem tokens altos abaixo do limite de rotação THEN lifecycle SHALL still return `ActionColdResume`, not `ActionCompact`. ✅
3. WHEN `ActionColdResume` é aplicado THEN request SHALL set `Continue=false` and preserve `Resume=session_file` when available. ✅

**Independent Test:** `TestEvaluateLifecycle_InactiveAboveCompactThreshold`, `TestApplyLifecycle_ColdResume`.

---

### P1: Estados perigosos ainda protegem a sessão — ✅ (política refinada)

**User Story:** Como operador, quero que o Aurelia ainda evite continuar sessões perigosas ou corrompidas mesmo removendo compactação proativa.

**Acceptance Criteria (as-implemented):**

1. WHEN `input_tokens >= rotate_after_input_tokens` THEN `applyLifecycle` + `TokenGuard` SHALL return `ActionRotate` (não `EvaluateLifecycle` isolado). ✅
2. WHEN recent empty results reaches policy threshold THEN lifecycle SHALL return `ActionColdResume` with `HealthDangerous` (safe recovery, não rotate automático). ✅
3. WHEN recent process deaths reaches policy threshold THEN lifecycle SHALL return `ActionColdResume` with `HealthDangerous`. ✅
4. WHEN a sessão tem uma única falha suspeita abaixo do threshold de rotação THEN lifecycle SHALL not continue hot (`Continue=false`). ✅

**Nota:** A spec original previa `ActionRotate` em `EvaluateLifecycle` para tokens perigosos e falhas repetidas. A implementação prioriza preservar o `session_file` PI via cold resume; rotate fica no hard ceiling do `TokenGuard`.

**Independent Test:** `TestEvaluateLifecycle_RepeatedSuspectColdResumes`, `TestApplyLifecycle_TokenGuardImmediateRotate`.

---

### P1: Tool-call tracker fala linguagem humana — ✅

**User Story:** Como usuário, quero entender que o Aurelia está consolidando um trabalho longo sem ver termos técnicos de execução.

**Acceptance Criteria:**

1. WHEN tool-call warning threshold is reached THEN user-visible message SHALL not contain `calls`, `ferramentas`, tool name, or raw count. ✅
2. WHEN critical threshold is reached THEN user-visible message SHALL ask for consolidation in human terms. ✅
3. WHEN steer is sent to the model THEN it MAY include technical details/counts, but this text SHALL not be sent verbatim to the user. ✅

**Example user copy (em produção):**

```text
Estou analisando bastante contexto. Vou consolidar os achados antes de continuar.
```

**Independent Test:** `TestToolCallTracker_WarningMessageOmitsTechnicalTerms`, `TestToolCallTracker_CriticalMessageOmitsTechnicalTerms`.

---

### P1: Loop detector não expõe comando interno — ✅

**User Story:** Como usuário, quando o Aurelia detecta repetição, quero receber uma atualização calma, não uma ordem interna como se o bot estivesse falando consigo mesmo.

**Acceptance Criteria:**

1. WHEN loop detector fires THEN user-visible message SHALL not contain `Pare o que está fazendo`. ✅
2. WHEN loop detector fires THEN internal steer SHALL still instruct the model to stop the repetitive pattern and summarize. ✅
3. WHEN user-visible loop message is sent THEN it SHALL use neutral wording. ✅

**Example user copy (em produção):**

```text
Vou consolidar o que já encontrei para evitar repetição.
```

**Independent Test:** `TestLoopDetector_UserMessageIsNeutral`.

---

### P2: Heartbeat evita contadores técnicos — ✅

**User Story:** Como usuário, quando uma execução fica longa, quero ver progresso compreensível sem detalhes de tool calls.

**Acceptance Criteria:**

1. WHEN heartbeat is sent in normal mode THEN message SHALL not include `chamadas de ferramenta`, `calls`, or tool names. ✅
2. WHEN heartbeat is sent THEN message SHALL communicate continued work and expected consolidation. ✅
3. WHEN debug/admin mode exists in future THEN technical counters MAY be shown only there; this spec does not implement that mode. — n/a

**Example user copy (em produção):**

```text
Ainda estou trabalhando no pedido. Vou consolidar o progresso em breve.
```

**Independent Test:** `TestHeartbeatMessage_OmitsChamadasDeFerramenta`, `TestHeartbeatMessage_NormalModeOmitsToolCounts`.

---

## Functional Requirements

### Lifecycle policy

**`EvaluateLifecycle` priority order (as-implemented):**

1. cold/inactive session → cold resume;
2. suspect failures (empty result, process death) → cold resume (repeated failures marked `HealthDangerous`, still cold resume);
3. large or very large token counts → continue (PI SDK owns compaction);
4. healthy active session → continue.

**`applyLifecycle` + `TokenGuard` escalation (after base lifecycle):**

5. hard ceiling `input_tokens >= rotate_after` → rotate;
6. stall: N consecutive large turns without ≥5% token reduction → compact (fallback only).

`CompactAfterInputTokens` remains in config for compatibility and `TokenGuard` tracking; `EvaluateLifecycle` SHALL not use it to block a healthy request with `ActionCompact`.

### User-visible messages

Normal user-facing long-flow messages (as-implemented):

| Antes | Depois |
|-------|--------|
| “Compactando histórico longo...” | “Preparando o contexto da conversa antes de continuar...” |
| “ferramentas / calls / contadores” | “analisando bastante contexto / consolidar achados” |
| “Pare o que está fazendo...” (user) | “Vou consolidar o que já encontrei...” |
| “chamadas de ferramenta” (heartbeat) | “Ainda estou processando / consolidar o progresso” |

### Internal steering

Internal model steer MAY remain technical and imperative when needed, but it MUST be sent only through the steer channel, not reused as Telegram copy.

---

## Implementation Scope

### Files changed (v0.19.6+)

```text
internal/session/lifecycle.go
internal/session/lifecycle_test.go
internal/session/token_guard.go          # post-v0.19.6 escalation layer
internal/session/token_guard_test.go
internal/pipeline/session_lifecycle.go
internal/pipeline/pipeline.go
internal/pipeline/tool_monitoring.go
internal/pipeline/ux_messages_test.go
internal/pipeline/token_guard_lifecycle_test.go
internal/pipeline/session_lifecycle_test.go
CHANGELOG.md
```

### Forbidden files (unchanged)

```text
bridge/index.ts
internal/bridge/bundle.ts
internal/config/*
internal/telegram/*
~/.aurelia/config/*
.env*
```

Bridge was not changed because the goal was to stop duplicating normal compaction in Go, not to change the PI SDK integration.

---

## Validation Contract

### Behavior Assertions

- [x] Active healthy large sessions continue without Go-triggered compact.
- [x] Cold/inactive sessions cold-resume before any compact decision.
- [x] Dangerous sessions still rotate or recover safely (via `TokenGuard` + cold resume).
- [x] User-visible messages do not include technical tool-call language in normal flow.
- [x] Loop detector user message does not include internal imperative steering text.
- [x] No proactive “🧠 Compactando histórico longo...” message is sent on normal healthy request start.

### Test Matrix

- [x] Happy path: active healthy session above compact threshold returns `ActionContinue`.
- [x] Edge case: inactive session above compact threshold returns `ActionColdResume`.
- [x] Error path: empty result/process death does not continue hot.
- [x] Dangerous path: rotate threshold returns `ActionRotate` via `TokenGuard` in `applyLifecycle`.
- [x] UX text: tool tracker messages omit `calls`, `ferramentas`, raw tool name, and raw count.
- [x] UX text: loop detector sends neutral user copy and strong internal steer separately.
- [x] UX text: heartbeat omits `chamadas de ferramenta` in normal mode.

### Quality Gates

```bash
go test ./internal/session ./internal/pipeline -short   # ✅ verified 2026-07-10
go test ./... -short
go vet ./...
go build ./...
```

### Review Gates

- Backend code review after implementation. ✅ (v0.19.6)
- Review focus: lifecycle decision regressions, Telegram UX copy, thread-safe message sending, tests mapping to this spec.

---

## Rollout Plan

1. ~~Implement in dedicated branch: `feature/long-flow-ux-v2`.~~ ✅ v0.19.6
2. ~~Run validation gates.~~ ✅
3. ~~Deploy after user approval.~~ ✅ (em produção desde v0.19.6)
4. ~~Live validation: normal message in long session — no compact message before task.~~ ✅
5. ~~Inspect daemon logs: healthy large session continues rather than compact.~~ ✅
6. Promoted to `main` via release train (current: v0.39.2).

---

## Deferred Follow-up: Observability

This spec intentionally defers runlog repair to avoid mixing functional UX changes with observability plumbing.

Follow-up spec/task should address:

- lifecycle decisions missing from runlog because lifecycle currently happens before runlog state exists;
- `run_journal.tool_count` and `duration_ms` being zero despite tool events;
- durable capture of sanitized user-visible lifecycle/status messages for future UX audits.

`recordLifecycleDecision` exists in `session_lifecycle.go` but full audit trail remains incomplete.