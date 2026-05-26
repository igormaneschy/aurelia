# Long Flow UX v2 — Especificação

**Status:** Proposed  
**Data:** 2026-05-26  
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

- [ ] Deixar a PI SDK gerenciar a compactação normal de sessões saudáveis.
- [ ] Remover compactação Go síncrona automática do caminho saudável antes do pedido principal.
- [ ] Manter Go responsável por segurança operacional: `cold_resume`, `rotate`, recuperação após timeout, empty result e process death.
- [ ] Reduzir mensagens técnicas no Telegram durante execução longa.
- [ ] Separar instruções internas ao modelo (`steer`) das mensagens visíveis ao usuário.
- [ ] Preservar segurança de sessão em estados perigosos ou suspeitos.
- [ ] Cobrir as decisões com testes de lifecycle e textos de UX.

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

### Decision 3: User messages must describe progress, not internals

Mensagens padrão ao usuário não devem expor:

- nomes de ferramentas;
- contagem de tool calls;
- comandos internos para o modelo;
- detalhes de session lifecycle, exceto quando houver uma ação inevitável e perceptível.

---

## User Stories

### P1: Sessão saudável longa não compacta no caminho crítico

**User Story:** Como usuário, quando envio uma nova mensagem em uma conversa longa porém saudável, quero que o Aurelia comece o trabalho sem mostrar manutenção interna antes do pedido.

**Acceptance Criteria:**

1. WHEN a sessão está ativa e sem falhas recentes AND `input_tokens >= compact_after_input_tokens` AND `input_tokens < rotate_after_input_tokens` THEN lifecycle SHALL return `ActionContinue`.
2. WHEN `ActionContinue` é retornado THEN o pedido SHALL seguir para o Bridge sem mensagem de compactação enviada pelo Go.
3. WHEN a PI SDK compacta internamente durante a execução THEN o Go SHALL not emit a proactive “compactando histórico” message from lifecycle.

**Independent Test:** mockar `HealthSignals{Active: true, InputTokens: CompactAfterInputTokens}` e verificar `ActionContinue`.

---

### P1: Sessão fria tem prioridade sobre compactação

**User Story:** Como usuário, quando volto depois de um tempo/restart, quero uma retomada segura, não uma compactação bloqueante antes do meu pedido.

**Acceptance Criteria:**

1. WHEN a sessão está inativa/cold THEN lifecycle SHALL return `ActionColdResume`.
2. WHEN uma sessão cold também tem tokens altos abaixo do limite de rotação THEN lifecycle SHALL still return `ActionColdResume`, not `ActionCompact`.
3. WHEN `ActionColdResume` é aplicado THEN request SHALL set `Continue=false` and preserve `Resume=session_file` when available.

**Independent Test:** mockar sessão inativa com tokens acima do antigo limite de compactação e verificar `ActionColdResume`.

---

### P1: Estados perigosos ainda protegem a sessão

**User Story:** Como operador, quero que o Aurelia ainda evite continuar sessões perigosas ou corrompidas mesmo removendo compactação proativa.

**Acceptance Criteria:**

1. WHEN `input_tokens >= rotate_after_input_tokens` THEN lifecycle SHALL return `ActionRotate`.
2. WHEN recent empty results reaches policy threshold THEN lifecycle SHALL return `ActionRotate` or the existing safe recovery action defined by policy.
3. WHEN recent process deaths reaches policy threshold THEN lifecycle SHALL return `ActionRotate` or the existing safe recovery action defined by policy.
4. WHEN a sessão tem uma única falha suspeita abaixo do threshold de rotação THEN lifecycle SHALL not continue hot.

**Independent Test:** preservar/ajustar testes existentes de dangerous/suspect lifecycle.

---

### P1: Tool-call tracker fala linguagem humana

**User Story:** Como usuário, quero entender que o Aurelia está consolidando um trabalho longo sem ver termos técnicos de execução.

**Acceptance Criteria:**

1. WHEN tool-call warning threshold is reached THEN user-visible message SHALL not contain `calls`, `ferramentas`, tool name, or raw count.
2. WHEN critical threshold is reached THEN user-visible message SHALL ask for consolidation in human terms.
3. WHEN steer is sent to the model THEN it MAY include technical details/counts, but this text SHALL not be sent verbatim to the user.

**Example user copy:**

```text
Estou analisando bastante contexto. Vou consolidar os achados antes de continuar.
```

---

### P1: Loop detector não expõe comando interno

**User Story:** Como usuário, quando o Aurelia detecta repetição, quero receber uma atualização calma, não uma ordem interna como se o bot estivesse falando consigo mesmo.

**Acceptance Criteria:**

1. WHEN loop detector fires THEN user-visible message SHALL not contain `Pare o que está fazendo`.
2. WHEN loop detector fires THEN internal steer SHALL still instruct the model to stop the repetitive pattern and summarize.
3. WHEN user-visible loop message is sent THEN it SHALL use neutral wording.

**Example user copy:**

```text
Vou consolidar o que já encontrei para evitar repetição.
```

---

### P2: Heartbeat evita contadores técnicos

**User Story:** Como usuário, quando uma execução fica longa, quero ver progresso compreensível sem detalhes de tool calls.

**Acceptance Criteria:**

1. WHEN heartbeat is sent in normal mode THEN message SHALL not include `chamadas de ferramenta`, `calls`, or tool names.
2. WHEN heartbeat is sent THEN message SHALL communicate continued work and expected consolidation.
3. WHEN debug/admin mode exists in future THEN technical counters MAY be shown only there; this spec does not implement that mode.

**Example user copy:**

```text
Ainda estou trabalhando no pedido. Vou consolidar o progresso em breve.
```

---

## Functional Requirements

### Lifecycle policy

The lifecycle decision order SHOULD become:

1. dangerous token threshold → rotate;
2. repeated suspect failures → rotate;
3. single suspect failures → cold resume / safe recovery;
4. cold/inactive session → cold resume;
5. healthy active session → continue, even if above old compact threshold;
6. compact action reserved for explicit/manual/fallback future use, not default healthy path.

`CompactAfterInputTokens` may remain in config for compatibility, but this phase SHALL not use it to block a healthy request with `ActionCompact`.

### User-visible messages

Normal user-facing long-flow messages SHALL prefer:

- “preparando contexto” over “compactando histórico”;
- “analisando bastante contexto” over “ferramentas/calls”;
- “consolidar achados” over “pare o que está fazendo”.

### Internal steering

Internal model steer MAY remain technical and imperative when needed, but it MUST be sent only through the steer channel, not reused as Telegram copy.

---

## Implementation Scope

### Allowed files

```text
internal/session/lifecycle.go
internal/session/lifecycle_test.go
internal/pipeline/session_lifecycle.go
internal/pipeline/pipeline.go
internal/pipeline/*_test.go
CHANGELOG.md
```

### Forbidden files

```text
bridge/index.ts
internal/bridge/bundle.ts
internal/config/*
internal/telegram/*
~/.aurelia/config/*
.env*
```

Bridge is forbidden in this phase because the goal is to stop duplicating normal compaction in Go, not to change the PI SDK integration.

---

## Validation Contract

### Behavior Assertions

- [ ] Active healthy large sessions continue without Go-triggered compact.
- [ ] Cold/inactive sessions cold-resume before any compact decision.
- [ ] Dangerous sessions still rotate or recover safely.
- [ ] User-visible messages do not include technical tool-call language in normal flow.
- [ ] Loop detector user message does not include internal imperative steering text.
- [ ] No proactive “🧠 Compactando histórico longo...” message is sent on normal healthy request start.

### Test Matrix

- [ ] Happy path: active healthy session above compact threshold returns `ActionContinue`.
- [ ] Edge case: inactive session above compact threshold returns `ActionColdResume`.
- [ ] Error path: empty result/process death does not continue hot.
- [ ] Dangerous path: rotate threshold still returns `ActionRotate`.
- [ ] UX text: tool tracker messages omit `calls`, `ferramentas`, raw tool name, and raw count.
- [ ] UX text: loop detector sends neutral user copy and strong internal steer separately.
- [ ] UX text: heartbeat omits `chamadas de ferramenta` in normal mode.

### Quality Gates

```bash
go test ./internal/session ./internal/pipeline -short
go test ./... -short
go vet ./...
go build ./...
```

### Review Gates

- Backend code review after implementation.
- Review focus: lifecycle decision regressions, Telegram UX copy, thread-safe message sending, tests mapping to this spec.

---

## Rollout Plan

1. Implement in dedicated branch: `feature/long-flow-ux-v2`.
2. Run validation gates.
3. Deploy only after user approval, following repository branch policy.
4. Live validation: send a normal message in a previously long session and confirm no compact message appears before task processing.
5. Inspect daemon logs for lifecycle decision: healthy large session should continue rather than compact.
6. Promote to `stable/*` only after live validation.

---

## Deferred Follow-up: Observability

This spec intentionally defers runlog repair to avoid mixing functional UX changes with observability plumbing.

Follow-up spec/task should address:

- lifecycle decisions missing from runlog because lifecycle currently happens before runlog state exists;
- `run_journal.tool_count` and `duration_ms` being zero despite tool events;
- durable capture of sanitized user-visible lifecycle/status messages for future UX audits.
