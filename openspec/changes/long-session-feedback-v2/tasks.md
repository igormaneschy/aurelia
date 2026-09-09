# Feedback honesto e visibilidade em sessões longas — Tasks

**Change:** `long-session-feedback-v2`  
**Dependency graph:** `T0 → T1 → T2 → T3 → T4 → T5 → T6`  
**Implementation branch:** `feature/long-session-feedback-v2`  
**Terminal boundary:** validação live no daemon; promoção/release ficam fora
até aprovação explícita do Igor.

## T0 — Baseline e preflight

- [x] Confirmar checkout limpo em `main` atualizado e branch
      `feature/long-session-feedback-v2` criada a partir dele.
- [x] Registrar baseline: `go build ./...`, `go vet ./...`,
      `go test ./... -short -count=1` (verde), `cd bridge && npx tsc --noEmit`
      (verde), `npm test` (169/169 antes das mudanças), `aurelia debug metrics
      --days 30`.
- [x] Encodar a fixture do caso `79f69a1e` como cenário de regressão em código
      (Bash 180s → nenhum `stall`/`steer`; `tool_running` a cada tick), em vez
      de arquivo JSON, seguindo o harness existente (eventos em canal Go +
      `healthDecisionFor` puro no TS).
- [x] Encodar a fixture de stall real (modelo sem tool >120s) preservando a
      escada warning/urgent.
- [x] Confirmar contrato atual de `request_id`, `safeLabel`,
      `normalizeToolLabel`, orçamento de telemetria e `run_events`.
- [x] Sem blockers.

**Validation:** baseline verde e cenários de regressão definidos; nenhuma
mudança runtime no T0.

## T1 — Bridge: health monitor ciente de ferramenta

- [x] Guardar o label seguro em `ToolDurationTracker.start()` e adicionar
      `inflight(now)` retornando a tool aberta há mais tempo.
- [x] Extrair `healthDecisionFor(input): HealthDecision` puro com
      `toolRunning`, `toolSlow`, `stall` e `steer`; `stallTelemetryFor` passa a
      delegar para manter uma única fonte de thresholds.
- [x] Emitir `tool_running` a cada tick de 15s enquanto houver tool em voo, com
      `tool_call_id`, `name`, `elapsed_ms` (clampado) e `source=bridge_health`.
- [x] Suprimir `stall`/`steer` enquanto houver tool em voo; resetar flags na
      transição modelo→tool.
- [x] Emitir `tool_slow` uma vez por ferramenta ao cruzar 10min.
- [x] Testes TS: tool em voo, tool lenta, transição, silêncio real preservado,
      `inflight` vazio após `end`/`clear`, label limitado, relógio para trás.
- [x] Rebuildar o bundle (`make bridge`) e sincronizar `bundle.ts`/`bundle.js`.

**Validation:** `healthDecisionFor` nunca emite `stall`/`steer` com tool em voo;
silêncio real continua emitindo warning/urgent. 175/175 testes TS.

## T2 — Pipeline: progresso e watchdog

- [x] Adicionar `ProgressStateToolRunning` e `ProgressStateToolSlow`.
- [x] Tratar `ev.Type == "tool_running"`/`"tool_slow"` em `ProcessBridgeEvents`
      sem `recordPipelineEvent` (live-only, preserva o orçamento de telemetria).
- [x] `toolProgressDetail` formata label seguro + tempo decorrido; `tool_slow`
      usa "comando longo" em vez do texto de modelo travado.
- [x] Incluir `tool_running`/`tool_slow` em `livenessEventIsProductive`
      (relógio de silêncio) sem contá-los em `trackRunFeedback`.
- [x] Estender `stallPriorityReporter` para segurar `tool_running`/`tool_slow` e
      impedir sobrescrita por `Waiting`.
- [x] Normalizar `tool_running`/`tool_slow` no allowlist do Go e limitar
      `elapsed_ms` (`normalizeEvent`).
- [x] Testes: progresso sem estado de stall, detalhe honesto, watchdog com
      tool em voo não escala, probe falhando ainda cancela.

**Validation:** `go test ./internal/pipeline ./internal/bridge` verde; race
verde.

## T3 — Adapters: renderizar `tool_running` e `detail`

- [x] Telegram `progressReporter`: `tool_running` → "⚙️ <detail>",
      `tool_slow` → "⏳ <detail>", mantendo throttle e recibo único.
- [x] Telegram: `detail` do estado `working` (compactação `tokens: X → Y`)
      renderizado em vez de descartado.
- [x] TUI `handleProgressEvent`: `tool_running`/`tool_slow` no indicador acima
      do composer; `detail` de compactação renderizado; tool real limpa a linha.
- [x] Testes de adapter para ambas as superfícies, incluindo a garantia de que
      nenhum estado novo polui o transcript.

**Validation:** compactação visível nas duas superfícies; tool longa mostra
cronômetro e nunca o texto de "modelo com dificuldade".

## T4 — Uso persistido e visível

- [x] Estender `runlog.CompletionAggregates` com `InputTokens`,
      `OutputTokens`, `CostUSD`, `ToolCount` e gravar na `UPDATE` terminal de
      `CompleteWithEvents` (sem migração).
- [x] Capturar uso do evento `result` via `recordRunUsage`; `tool_count` vem do
      contador de tools do run.
- [x] Testes de store (persistência atômica) e de pipeline (captura + terminal).
- [x] `/usage` real no Telegram via `bridge.GetSessionStats` (timeout 15s,
      degradação explícita quando não há bridge/sessão).
- [x] Painel de projeto da TUI exibe contexto/percentual/custo via
      `fillTUIProjectUsage`.
- [x] Testes: `/usage` sem bridge, sem sessão e `formatTokenCount`.

**Validation:** `debug metrics` deixa de reportar custo 0 para runs com uso;
`/usage` e o painel TUI mostram dados coerentes com `get-session-stats`.

## T5 — Ponto de decisão proativo de sessão longa

- [x] Limiar de atenção `min(60% de compact_after, warn atual)`.
- [x] Marcador "já avisado" por sessão no `session.Store`
      (`MarkLongSessionNudged`), atômico e re-armado quando o `session_file`
      muda.
- [x] Aviso único ao usuário (Telegram/TUI via `SendText`) com tokens
      aproximados e opção `/new`; sem compactação/rotação automática.
- [x] Testes: limiar, uma vez por sessão, re-arme em sessão nova, fail-closed
      sem sessão/output.

**Validation:** exatamente um aviso por sessão; nenhuma decisão automática.

## T6 — Quality, review e validação live

- [x] `go build ./...`, `go vet ./...`.
- [x] `go test ./... -short -count=1`.
- [x] `go test -race` nos pacotes tocados (pipeline, session, runlog, bridge,
      telegram, tui, ipc) — verde.
- [x] `cd bridge && npx tsc --noEmit` e `npm test` (175/175).
- [x] `make bridge` + sincronização de `internal/bridge/bundle.ts|js`.
- [x] Code review (self-review PI, sem sub-agentes): PASS — 0 critical/high,
      1 low (duplicação de 8 linhas do formatador de tokens entre
      `telegram` e `tui`, aceita para não criar pacote só por isso).
- [x] Security review focada: labels passam por `safeLabel`+`normalizeToolLabel`,
      `tool_running` não carrega comando/args/output, `elapsed_ms` clampado,
      `/usage` não expõe caminho de sessão nem segredos, nada de credencial em
      log/telemetria.
- [x] Validação live TUI/IPC no daemon (v0.43.3, pós-deploy):
      - `sleep 75` → `tool_running` a cada 15s (9s/24s/39s/54s/1m9s),
        **0** `streaming stall` e **0** `stall steer` no log do daemon;
      - run persistido com `input_tokens=15686`, `output_tokens=420`,
        `cost=$0.0017`, `tool_count=1`, `stall_count=0`;
      - `aurelia debug metrics` passa a reportar tokens e custo;
      - painel de projeto TUI retorna `input=15686 output=420 cost=0.0017
        context_pct=1.5 compact_after=200000` (mesmo caminho do `/usage`).
- [ ] Validação live Telegram (usuário): `/usage`, nudge de sessão longa,
      recibo com `⚙️ <tool> em execução`.
- [x] Evidence Matrix preenchida abaixo.
- [ ] Propor bump de versão e changelog ao Igor.

**Validation:** cada assertion tem evidência (teste, comando ou validação
live). PASS sem evidência é UNVERIFIED.

## Evidence Matrix

| Assertion | Evidência | Status |
|---|---|---|
| A1 progresso distingue tool de modelo | `TestProcessBridgeEvents_ToolRunningIsProgressNotStall`, `TestHandleProgressEvent_ToolRunningAndCompactionDetail`, live `tool_running` | PASS |
| A1 detail de compactação renderizado | `TestProgressReporter_ReportState_ToolAndCompactionDetail`, teste TUI de detalhe | PASS |
| A2 tool em voo não escala idle | `TestLivenessEventIsProductive_RequiresRealProgress` (tool_running/tool_slow), `TestStallPriorityReporter_ToolRunningHoldsWaiting` | PASS |
| A2 probe falhando ainda cancela | testes existentes de watchdog + `tool_running` produtivo | PASS |
| A3 uso observável | live `get-session-stats` via painel TUI + `cmdUsage` degradação | PASS |
| A5 uso persistido atomicamente | `TestSQLiteStore_CompleteWithEvents_PersistsUsageAtomically`, `TestCompleteRunLog_PersistsUsageAndToolCount`, live run `b5bc790f` | PASS |
| A6 sem stall/steer com tool | `healthDecisionFor` (TS) + live `sleep 75` sem stall/steer | PASS |
| A6 tool_slow honesto | `TestToolProgressDetail_BoundsAndLabels`, `healthDecisionFor` tool_slow | PASS |
| A6 label limitado/redigido | `inflight` bounded label (TS), `TestNormalizeEvent_KeepsToolRunningTelemetry` | PASS |
| A7 `/usage` real | `TestCmdUsage_DegradesExplicitly`, live stats path | PASS (código) / Telegram live pendente |
| A7 TUI contexto/custo | live painel de projeto com input/cost/context_pct | PASS |
| A8 nudge único | `TestMaybeNudgeLongSession_OneShotPerSession`, `TestMarkLongSessionNudged_OneShotAndResetOnNewSession` | PASS |
| Regressão `79f69a1e` | `healthDecisionFor` + live `sleep 75`: 0 stall/steer | PASS |

## Explicit non-goals checklist

- [x] Não alterar `bridgeExecutionTimeout` nem `idle_timeout_minutes`.
- [x] Não alterar a política do `TokenGuard` nem rotacionar automaticamente.
- [x] Não criar mensagem nova por heartbeat no Telegram.
- [x] Não persistir `tool_running` no runlog (orçamento de telemetria).
- [x] Não incluir comando, argumentos ou output de ferramenta em telemetria.
- [x] Não tocar em deploy/`Makefile`, provider/modelo ou NDJSON além do evento
      `tool_running`/`tool_slow` documentado.
