# Session Lifecycle Manager — Especificação

**Status:** Draft
**Data:** 2026-05-23

## Problem Statement

O Aurelia usa o PI SDK em sessões persistentes por Telegram. A sessão PI funciona bem para uso interativo, mas no Aurelia ela fica viva por dias, atravessa deploys/restarts, acumula histórico, sofre timeouts silenciosos e é retomada automaticamente com `continue`.

A análise dos logs dos últimos dias confirmou um ciclo de falha:

1. sessões longas são retomadas muitas vezes com `Continue=true`;
2. o contexto cresce muito (`input_tokens` observado acima de 200k, 600k e até ~950k);
3. o PI SDK/bridge fica instável (`empty result after work`, `bridge process died mid-request`, `idle_bridge_timeout`);
4. a sessão suspeita volta a ser retomada;
5. o problema se repete.

Exemplos observados:

- vários `idle_bridge_timeout` exatos em 15 minutos em 2026-05-22;
- `query timeout: no result after 10 minutes` em versões anteriores;
- múltiplos `empty result after work` com `cost=$0.0000 in=0 out=0`;
- múltiplos `bridge: process died mid-request, retrying`;
- sessões com `input_tokens` enormes em sequência.

O PI SDK já possui compaction, session tree, stats e eventos, mas o Aurelia ainda não possui uma política própria de ciclo de vida de sessão para decidir quando continuar, compactar, marcar cold ou criar uma nova sessão resumida.

## Goals

- [ ] Gerenciar sessão PI como recurso com ciclo de vida explícito: `warm`, `large`, `suspect`, `cold`, `rotated`
- [ ] Evitar `Continue=true` após timeouts, empty results, process death ou provider errors suspeitos
- [ ] Compactar proativamente sessões grandes antes que atinjam estado instável
- [ ] Criar nova sessão resumida quando a sessão atual estiver perigosa/corrompida
- [ ] Propagar eventos de compaction/retry/turn do PI SDK para resetar idle timeout e melhorar observabilidade
- [ ] Registrar decisões de lifecycle no runlog e continuity
- [ ] Melhorar UX no Telegram para retomada, compactação e rotação de sessão
- [ ] Reduzir ocorrências de `idle_bridge_timeout`, `empty result after work` e bridge death em sessões longas

## Out of Scope

- Alterar o PI SDK upstream
- Substituir o armazenamento JSONL nativo do PI
- Mudar provedores/modelos padrão
- Reescrever o sistema de continuidade completo
- Resolver todos os bugs de provider externo
- Automatizar merge/promoção para `main`

---

## User Stories

### P1: Sessão Suspeita não Continua Quente

**User Story:** Como usuário, quando uma execução falha por timeout, empty result ou morte do bridge, quero que a próxima tentativa retome com segurança, sem continuar um estado possivelmente corrompido.

**Acceptance Criteria:**

1. WHEN uma execução termina com `idle_bridge_timeout`, `bridge_query_timeout`, `provider/pi_timeout` ou `max_execution_timeout` THEN a sessão SHALL ser marcada cold no `session.Store` para aquele `chatID/threadID/userID`.
2. WHEN `handleErrorEvent` classifica um erro como timeout THEN a sessão SHALL ser desativada antes da próxima mensagem.
3. WHEN ocorre `empty result after work` THEN a sessão SHALL ser desativada.
4. WHEN ocorre `bridge process died mid-request` THEN a sessão SHALL ser tratada como suspect e a próxima retomada SHALL use cold resume ou rotação.
5. WHEN uma sessão está cold/suspect THEN `buildBridgeRequest` SHALL setar `Resume=session_file` e SHALL NOT setar `Continue=true`.

**Independent Test:** simular cada falha e verificar que `GetSessionWithState` retorna `active=false` e que a próxima request não inclui `Continue=true`.

---

### P1: Health Check antes de Query

**User Story:** Como sistema, quero avaliar a saúde da sessão antes de mandar prompt para o PI SDK para evitar executar em contexto já instável.

**Acceptance Criteria:**

1. WHEN há sessão persistida para o chat/thread/user THEN o bridge SHALL conseguir retornar stats básicos da sessão via novo comando ou metadata no `system` event.
2. WHEN a sessão excede limites configurados de tokens/mensagens/turns THEN o lifecycle manager SHALL classificar como `large` ou `dangerous`.
3. WHEN a sessão foi marcada por falha recente THEN o lifecycle manager SHALL classificar como `suspect`.
4. WHEN a sessão está saudável THEN execução SHALL seguir comportamento atual.
5. WHEN a sessão está `large` THEN o sistema SHALL compactar antes do prompt, se possível.
6. WHEN a sessão está `dangerous` ou `suspect` repetida THEN o sistema SHALL rotacionar para uma nova sessão resumida.

**Independent Test:** mockar stats de sessão e validar decisão: `continue`, `compact`, `cold_resume`, `rotate`.

---

### P1: Compaction Proativa com Eventos

**User Story:** Como usuário, quero que o Aurelia compacte automaticamente sessões grandes sem parecer travado.

**Acceptance Criteria:**

1. WHEN o lifecycle manager decide compactar THEN o bridge SHALL chamar `session.compact(customInstructions)` antes do prompt principal.
2. WHEN a compactação começa THEN o bridge SHALL emitir evento `compaction_start` para o Go.
3. WHEN a compactação termina THEN o bridge SHALL emitir evento `compaction_end` com pelo menos `tokens_before` e status.
4. WHEN eventos de compaction chegam ao Go THEN o idle timeout SHALL resetar como progresso válido.
5. WHEN a compactação falha THEN a sessão SHALL ser marcada suspect e a execução SHALL cair para cold resume ou rotação, conforme política.
6. WHEN a compactação ocorre THEN o Telegram SHOULD informar brevemente: `🧠 Compactando histórico longo para continuar com segurança...`.

**Independent Test:** mockar bridge emitindo `compaction_start/end` e verificar que o pipeline não dispara idle timeout durante compaction.

---

### P1: Rotação Inteligente de Sessão

**User Story:** Como usuário, quando a sessão antiga está grande ou instável demais, quero continuar o trabalho numa sessão nova com resumo suficiente para não perder contexto.

**Acceptance Criteria:**

1. WHEN a sessão é classificada como `dangerous` THEN o sistema SHALL criar uma nova sessão PI em vez de continuar a antiga.
2. WHEN cria nova sessão THEN o sistema SHALL injetar um resumo estruturado da sessão anterior como contexto inicial.
3. WHEN a nova sessão é criada THEN `session.Store` SHALL apontar para o novo `session_file`.
4. WHEN a sessão antiga é rotacionada THEN ela SHALL permanecer no disco para auditoria/retomada manual.
5. WHEN a rotação termina THEN continuity SHALL registrar o novo `session_id/session_file` e limpar `reset_reason` antigo em caso de sucesso.

**Independent Test:** fornecer sessão mock com estado dangerous; executar rotação; verificar novo session file no store e prompt inicial contendo resumo estruturado.

---

### P2: Configuração de Política de Sessão

**User Story:** Como operador, quero ajustar limites de lifecycle sem recompilar.

**Acceptance Criteria:**

1. `app.json` SHALL aceitar seção `session_lifecycle`.
2. Configuração SHALL incluir limites para tokens, mensagens, falhas recentes, idle timeout e modo de rotação.
3. Valores ausentes SHALL usar defaults seguros.
4. Config inválida SHALL produzir erro claro com valor ofensivo e formato esperado.

**Exemplo:**

```json
{
  "session_lifecycle": {
    "enabled": true,
    "compact_after_input_tokens": 120000,
    "rotate_after_input_tokens": 250000,
    "max_empty_results_before_rotate": 1,
    "max_process_deaths_before_rotate": 1,
    "idle_timeout_minutes": 20,
    "keep_recent_tokens": 8000,
    "reserve_tokens": 32768
  }
}
```

---

### P2: Observabilidade e Diagnóstico

**User Story:** Como operador, quero entender por que o Aurelia compactou, retomou cold ou rotacionou sessão.

**Acceptance Criteria:**

1. Cada decisão de lifecycle SHALL ser registrada no runlog como evento.
2. Logs SHALL incluir `chat`, `thread`, `user`, decisão, motivo e métricas principais.
3. `/status` ou comando debug SHOULD mostrar estado resumido da sessão sem expor caminho completo.
4. Erros de lifecycle SHALL ser redigidos antes de logar.

---

## Session Health States

| State | Meaning | Default Action |
|---|---|---|
| `healthy` | sessão pequena/normal e última execução ok | `continue` se active |
| `large` | contexto acima do limite de compactação | `compact_then_continue` |
| `suspect` | falha recente: timeout, empty result, process death | `cold_resume` ou `rotate` |
| `dangerous` | contexto muito alto ou falhas repetidas | `rotate` |
| `cold` | bridge/session warm state perdido | `resume` sem `continue` |
| `rotated` | sessão substituída por nova com resumo | usar nova sessão |

## Default Policy Proposal

| Signal | Threshold | Action |
|---|---:|---|
| input tokens | > 120k | compact before prompt |
| input tokens | > 250k | rotate session |
| empty result after work | >= 1 | mark suspect/cold |
| process death mid-request | >= 1 | mark suspect/cold |
| timeout | any | mark cold |
| deploy/restart restored session | any | cold resume; no `Continue=true` |
| repeated suspect in 30min | >= 2 | rotate session |
| no bridge events | 20min default | idle timeout, configurable |

## Existing Deploy Resume Behavior

Current implementation already supports basic safe resume after deploy:

1. `make deploy` sends SIGTERM via `launchctl kickstart -k`.
2. The daemon shuts down, stops the bridge, and restarts.
3. `sessions.json` persists known PI session files.
4. `NewPersistentStore` reloads all persisted sessions as `active=false` by design.
5. Startup calls `NotifyRecentInterruptedSessions(1 minute)` and asks the user to send `continuar`.
6. Sending `continuar` runs a special resume prompt in cold mode.

This does not conflict with the lifecycle manager. It is a special case of `cold` session state. The new lifecycle manager SHALL preserve this behavior and expand it with richer metadata and longer/configurable notice windows.

Potential gaps to address:

- Graceful deploy stop does not call `MarkColdForSessions`; sessions become cold through reload, but continuity may not get a deploy/reset reason.
- The interrupted-session notification window is only 1 minute.
- `continuar` is hardcoded and only works when a cold session exists for the sender.
- A normal new instruction after deploy already cold-resumes implicitly, but without the explicit safe-resume prompt.
- Runlog rows active during deploy may remain `running` unless separately reconciled.

## Success Criteria

- [ ] Reduzir timeouts por idle em sessões simples (`Oi`) causados por sessão travada
- [ ] Nenhuma request após timeout/empty result usa `Continue=true` na mesma sessão suspeita
- [ ] Sessões grandes são compactadas ou rotacionadas antes de ultrapassar limites perigosos
- [ ] Eventos de compaction aparecem no runlog
- [ ] Continuidade não mantém `reset_reason` antigo após sucesso
- [ ] Testes Go e bridge cobrem decisões principais
- [ ] `go test ./... -short`, `go build ./...`, bridge build passam
