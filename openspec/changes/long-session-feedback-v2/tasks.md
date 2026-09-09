# Feedback honesto e visibilidade em sessões longas — Tasks

**Change:** `long-session-feedback-v2`  
**Dependency graph:** `T0 → T1 → T2 → T3 → T4 → T5 → T6`  
**Implementation branch:** `feature/long-session-feedback-v2`  
**Terminal boundary:** validação live no daemon; promoção/release ficam fora
até aprovação explícita do Igor.

## T0 — Baseline e preflight

- [ ] Confirmar checkout limpo em `main` atualizado e branch
      `feature/long-session-feedback-v2` criada a partir dele.
- [ ] Registrar baseline: `go test ./... -short -count=1`, `go vet ./...`,
      `cd bridge && npx tsc --noEmit` (ou `npm run build`) e
      `aurelia debug metrics --days 30`.
- [ ] Exportar fixture redigida do caso `run 79f69a1e` (Bash 180s → 2 stalls +
      2 steers) para `internal/pipeline/testdata/` e `bridge/` conforme o
      harness existente.
- [ ] Exportar fixture de stall real (modelo sem tool >120s) para garantir que
      a escalada legítima não regride.
- [ ] Confirmar contrato atual de `request_id`, `safeLabel`,
      `normalizeToolLabel`, orçamento de telemetria e `run_events`.
- [ ] Parar e reportar blocker se fixtures ou baseline não forem obtidos.

**Validation:** baseline e fixtures documentados; nenhuma mudança runtime.

## T1 — Bridge: health monitor ciente de ferramenta

- [ ] Guardar o label seguro em `ToolDurationTracker.start()` e adicionar
      `inflight(now)` retornando `{name, elapsedMs}` da entrada mais antiga.
- [ ] Extrair `healthDecisionFor(input): HealthDecision` puro, com `toolRunning`,
      `stall`, `steer` e `toolSlow`, preservando `stallTelemetryFor`.
- [ ] Emitir `tool_running` a cada `TOOL_RUNNING_TICK_MS` (15s) enquanto houver
      tool em voo, com `tool_call_id`, `name`, `elapsed_ms` e
      `source=bridge_health`.
- [ ] Suprimir `stall`/`steer` enquanto houver tool em voo; resetar flags de
      stall na transição modelo→tool.
- [ ] Emitir `tool_slow` uma vez por ferramenta ao cruzar
      `TOOL_SLOW_WARN_MS` (10min).
- [ ] Testes TS: tool em voo, tool lenta, transição tool→silêncio, silêncio real
      mantém 60s/120s, `inflight` vazio após `end`/`clear`.
- [ ] Rebuildar o bundle (`cd bridge && npm run build && cp bundle.js
      ../internal/bridge/bundle.js`) se `bridge/index.ts` mudar.

**Validation:** teste do caso 180s não emite `stall` nem `steer`; teste de
silêncio real continua emitindo warning/urgent.

## T2 — Pipeline: progresso e watchdog

- [ ] Adicionar `ProgressStateToolRunning` e tratar `ev.Type == "tool_running"`
      em `ProcessBridgeEvents`, sem `recordPipelineEvent` (live-only).
- [ ] Mapear `tool_slow` para aviso com detalhe de ferramenta, sem o texto
      genérico de modelo travado.
- [ ] Adicionar `ToolElapsedMs` a `ProgressPayload` e emitir via `tuiOutput`.
- [ ] Estender `stallPriorityReporter` para segurar `tool_running` na
      `stallHoldWindow` e impedir sobrescrita por `Waiting`.
- [ ] Incluir `tool_running` em `livenessEventIsProductive` (relógio de
      silêncio) sem contá-lo em `trackRunFeedback`.
- [ ] Testes: watchdog com `tool_running` não escala; probe falhando durante
      tool cancela com `process_death`; `tool_running` limpa linha de stall.

**Validation:** fixture 180s não gera warning ao usuário; fixture de silêncio
real gera warning/urgent; probe falhando ainda cancela.

## T3 — Adapters: renderizar `tool_running` e `detail`

- [ ] Telegram `progressReporter`: novo `case` para `tool_running` com linha
      `⚙️ <label> em execução há <d>`, mantendo throttle de 1,5s e recibo único.
- [ ] Telegram: renderizar o `detail` do estado `working` (compactação
      `tokens: X → Y`) sem criar mensagem nova.
- [ ] TUI `handleProgressEvent`: estado `tool_running` atualiza a linha de
      atividade/indicador com cronômetro; nunca entra no transcript.
- [ ] TUI: renderizar `detail` de compactação no indicador, uma vez por run.
- [ ] Testes: `progress_test.go` (Telegram) e testes TUI de `progress_event`;
      regressão de que nenhum estado novo polui o transcript.

**Validation:** compactação visível nas duas superfícies; tool longa mostra
cronômetro e nenhum texto de stall.

## T4 — Uso persistido e visível

- [ ] Estender `runlog.CompletionAggregates` com `InputTokens`,
      `OutputTokens`, `CostUSD`, `ToolCount` e gravar na `UPDATE` terminal de
      `CompleteWithEvents` (sem migração).
- [ ] Capturar uso do evento `result` em `handleResultEvent` e `tool_count` do
      `toolTracker`; `recordUsage` deixa de ser apenas log.
- [ ] Testes de store: colunas persistidas atomicamente; store genérico de
      teste continua funcionando; compatibilidade com runs sem uso.
- [ ] Implementar `/usage` real no Telegram via `bridge.GetSessionStats`
      (timeout 15s, degradação explícita).
- [ ] Adicionar indicador de contexto/custo na TUI (painel de estado e/ou chip
      no header) on-demand.
- [ ] Testes: `/usage` com sessão, `/usage` sem sessão, `/usage` com falha do
      Bridge; render do indicador TUI.

**Validation:** `debug metrics` deixa de mostrar custo 0; `/usage` e TUI
mostram contexto/percentual/custo coerentes com `get-session-stats`.

## T5 — Ponto de decisão proativo de sessão longa

- [ ] Definir limiar de atenção (`min(60% de compact_after, warn atual)`) e
      marcador "já avisado" por sessão (sem repetir por turno).
- [ ] Substituir o `log.Printf` de `session_lifecycle.go:103` por aviso único ao
      usuário em Telegram e TUI, com tokens/custo aproximados e opção `/new`.
- [ ] Garantir que o aviso não dispara compactação/rotação nem altera o
      `TokenGuard`.
- [ ] Testes: dispara uma vez, não repete, não força compactação, funciona com
      `sessions == nil`.

**Validation:** sessão cruzando o limiar recebe exatamente um aviso; nenhuma
decisão automática de sessão é tomada.

## T6 — Quality, review e validação live

- [ ] Executar `go test ./... -short -count=1`.
- [ ] Executar `go test ./... -race -count=1` nos pacotes tocados.
- [ ] Executar `go vet ./...` e `make check` quando as ferramentas estiverem
      disponíveis.
- [ ] Se `bridge/index.ts` mudou: `cd bridge && npm run build` +
      sincronizar `internal/bridge/bundle.js` e rodar os testes TS.
- [ ] Solicitar code review focado em Bridge health monitor, watchdog e
      adapters.
- [ ] Solicitar security review: labels redigidos, nenhum comando/args/output em
      telemetria, `/usage` sem vazar segredos.
- [ ] `make deploy` na branch e validação live Telegram: tool longa (>3min),
      silêncio real, `/usage`, compactação, nudge, `/stop`.
- [ ] Validação live TUI: tool longa, cronômetro, indicador de contexto/custo,
      compactação, cancelamento.
- [ ] Preencher Evidence Matrix para A1, A2, A3, A5, A6, A7 e A8.
- [ ] Propor bump de versão e changelog ao Igor; não commitar release sem
      aprovação.

**Validation:** cada assertion tem evidência (teste, comando ou validação
live). PASS sem evidência é UNVERIFIED.

## Explicit non-goals checklist

- [ ] Não alterar `bridgeExecutionTimeout` nem `idle_timeout_minutes`.
- [ ] Não alterar a política do `TokenGuard` nem rotacionar automaticamente.
- [ ] Não criar mensagem nova por heartbeat no Telegram.
- [ ] Não persistir `tool_running` no runlog (orçamento de telemetria).
- [ ] Não incluir comando, argumentos ou output de ferramenta em telemetria.
- [ ] Não tocar em deploy/`Makefile`, provider/modelo ou NDJSON além do evento
      `tool_running`/`tool_slow` documentado.
