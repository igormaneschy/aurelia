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
      telegram, tui, ipc).
- [x] `cd bridge && npx tsc --noEmit` e `npm test` (175/175).
- [x] `make bridge` + sincronização de `internal/bridge/bundle.ts|js`.
- [ ] Code review focado em Bridge health monitor, watchdog e adapters.
- [ ] Security review: labels redigidos, nenhum comando/args/output em
      telemetria, `/usage` sem vazar segredos.
- [ ] `make deploy` na branch e validação live Telegram: tool longa (>3min),
      silêncio real, `/usage`, compactação, nudge, `/stop`.
- [ ] Validação live TUI: tool longa, cronômetro, indicador de contexto/custo,
      compactação, cancelamento.
- [ ] Preencher Evidence Matrix para A1, A2, A3, A5, A6, A7 e A8.
- [ ] Propor bump de versão e changelog ao Igor.

**Validation:** cada assertion tem evidência (teste, comando ou validação
live). PASS sem evidência é UNVERIFIED.

## Explicit non-goals checklist

- [x] Não alterar `bridgeExecutionTimeout` nem `idle_timeout_minutes`.
- [x] Não alterar a política do `TokenGuard` nem rotacionar automaticamente.
- [x] Não criar mensagem nova por heartbeat no Telegram.
- [x] Não persistir `tool_running` no runlog (orçamento de telemetria).
- [x] Não incluir comando, argumentos ou output de ferramenta em telemetria.
- [x] Não tocar em deploy/`Makefile`, provider/modelo ou NDJSON além do evento
      `tool_running`/`tool_slow` documentado.
