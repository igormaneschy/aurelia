# Feedback honesto e visibilidade em sessões longas — Design

## 1. Current state

Base: `main` @ v0.43.3, branch `feature/long-session-feedback-v2`.

### 1.1 O que a `long-session-ux-review-v1` entregou

- Eventos NDJSON `stall`/`steer` com `severity`/`silent_ms`/`source`,
  persistidos no runlog.
- `tool_result` com `duration_ms` medido (`ToolDurationTracker`).
- Agregados por run: `duration_ms`, `first_feedback_ms`, `max_silence_ms`,
  `stall_count`, `steer_count` (`internal/runlog/store_sqlite.go:474`).
- Contrato de progresso surface-neutral (`ProgressReporter`) com adapters
  Telegram (recibo único editável) e TUI (`EventTypeProgress`).
- Watchdog liveness-aware: sonda o Bridge antes de cancelar e escala
  warning → urgent → cancel.

### 1.2 Limites que esta change ataca

| # | Limite | Onde | Evidência |
|---|---|---|---|
| L1 | `lastEventTime` só muda no start/end da tool | `bridge/index.ts:2486-2500` | 457 stalls ≥60s com `bash`; run `79f69a1e` (tool 180s → 2 stalls + 2 steers) |
| L2 | Steer "pare o que está fazendo" é enviado durante tool | `bridge/index.ts:2652-2693` | `bridge_steer` warning+urgent no run `79f69a1e` |
| L3 | `detail` do estado `working` é descartado | `internal/telegram/progress.go:145`, `internal/tui/update.go:1552` | compactação nunca visível |
| L4 | Watchdog não conhece tool em voo | `livenessEventIsProductive` (`liveness_timeout.go:300`) | `idle timeout (20m0s)` em 2026-05-27 durante bash de 16min |
| L5 | Uso só vai para o log | `recordUsage` (`pipeline.go:1305`) | 89/89 runs com tokens/custo = 0 |
| L6 | `tool_count` nunca escrito | `CompleteWithEvents` não recebe uso | 89/89 runs com `tool_count = 0` |
| L7 | Aviso de contexto só em log | `session_lifecycle.go:103` (`WarnInputTokensThreshold = 500_000`) | sessão em 571k tokens sem sinalização |
| L8 | `/usage` é no-op | `bot_middleware.go:312` | usuário não consulta contexto/custo |

### 1.3 O que já existe e deve ser reaproveitado

- `bridge.GetSessionStats` + comando `get-session-stats` já retornam
  `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`,
  `total_tokens`, `cost`, `context_usage_pct` (`internal/bridge/protocol.go:54`,
  `bridge/index.ts:2976`).
- `ToolDurationTracker` (`bridge/index.ts:723`) já sabe o que está em voo; falta
  expor.
- `RunUpdate` já tem `InputTokens`, `OutputTokens`, `CostUSD`, `ToolCount`
  (`internal/runlog/types.go:99`).
- `ProgressPayload` já tem `State`, `Detail`, `ToolName`, `ToolDone`,
  `ElapsedMs` (`internal/ipc/types.go:219`).

Consequência: **não é preciso mudar schema de runlog nem inventar comando de
Bridge**; o trabalho é de interpretação, propagação e apresentação.

## 2. Target architecture

### 2.1 Bridge: contrato do evento `tool_running`

Novo evento NDJSON, correlacionado por `request_id`, emitido pelo health
monitor a cada `TOOL_RUNNING_TICK_MS` (default 15.000) **enquanto houver tool
em voo**:

```json
{
  "event": "tool_running",
  "request_id": "...",
  "tool_call_id": "tool-<digest>",
  "name": "Bash",
  "elapsed_ms": 182000,
  "source": "bridge_health"
}
```

Regras:

- `name` passa por `safeLabel(...)` / allowlist de labels (`normalizeToolLabel`
  no lado Go é a última linha de defesa). **Nunca** comando, args ou output.
- `elapsed_ms` derivado do `ToolDurationTracker` (`Date.now() - startedAt`),
  clampado a `[0, 24h]`.
- Evento **live-only**: não é telemetria durável. O pipeline **não** chama
  `recordPipelineEvent` para ele — o orçamento de telemetria por run
  (`maxTelemetryEventsPerRun = 64`, `long_session_telemetry.go:53`) continua
  reservado a stall/steer/compaction. Sem isso, um tool de 30min estouraria o
  orçamento e derrubaria o diagnóstico.
- Compatibilidade: clientes Go antigos ignoram eventos desconhecidos (switch
  sem `default` em `ProcessBridgeEvents`); nenhuma versão nova de protocolo.

### 2.2 Bridge: decisão do health monitor

Extrair uma função pura, testável isoladamente (mesmo padrão de
`stallTelemetryFor`):

```ts
export interface HealthInput {
  silentMs: number;
  inflightTool: { name: string; elapsedMs: number } | null;
  stallWarningSent: boolean;
  stallUrgentSent: boolean;
  toolSlowWarned: boolean;
}

export interface HealthDecision {
  toolRunning?: { name: string; elapsedMs: number };
  stall?: "warning" | "urgent";
  steer?: "warning" | "urgent";
  toolSlow?: true;
}
```

Regra:

1. `inflightTool != null` → emitir `toolRunning`; **nunca** `stall`/`steer`.
2. `inflightTool.elapsedMs >= TOOL_SLOW_WARN_MS` (default 10min) e ainda não
   avisado → emitir `toolSlow` (uma vez por tool). É um aviso honesto
   ("comando em execução há 10min"), não "o modelo travou".
3. Sem tool em voo → comportamento atual (60s warning, 120s urgent + steer).
4. Flags de stall resetam quando `silentMs < 30s` **ou** quando uma tool em voo
   passa a existir (transição modelo→tool).

`ToolDurationTracker` ganha `inflight(now)`: retorna a entrada mais antiga
ainda aberta como `{ name, elapsedMs }`. O tracker já guarda `safeID` e
`startedAt`, mas **não guarda o nome**; `tool_execution_start` deve passar o
label seguro para `start()` (parâmetro novo, compatível) para que
`tool_running` não precise consultar estado externo.

### 2.3 Pipeline: estado de progresso

- Novo `ProgressState`: `ProgressStateToolRunning = "tool_running"`.
- `ProcessBridgeEvents` mapeia `ev.Type == "tool_running"` →
  `progress.ReportState(ProgressStateToolRunning, detail)` com
  `detail = "<label> em execução há <d>"` (bounded, redigido).
- `tool_slow` → `ProgressStateStallWarning` **com detalhe de tool**, nunca o
  texto genérico de "modelo com dificuldade".
- `ProgressReporter` continua surface-neutral; nenhuma mensagem nova no chat.
- Prioridade: `tool_running` é classe "produtiva" — limpa a linha de stall e
  impede que o heartbeat `Waiting` a sobrescreva. Estender
  `stallPriorityReporter` (`liveness_timeout.go:335`) para segurar também
  `tool_running` na `stallHoldWindow` (90s) resolve sem novo decorator.
- `ProgressPayload` ganha `ToolElapsedMs int64` para os adapters renderizarem o
  cronômetro sem parsing de string.

### 2.4 Pipeline: watchdog de liveness

`livenessEventIsProductive` passa a considerar `tool_running` **produtivo para
o relógio de silêncio** (o Bridge está vivo e uma tool está executando), mas
**não** como feedback de primeira resposta em `trackRunFeedback` (não é saída
do modelo). Consequência: um `bash` de 40min não dispara warning/urgent/cancel
por idle; o cap duro de 30min (`bridgeExecutionTimeout`) continua sendo o teto
real e encerra com `timeout_origin=max_execution`.

Risco aceito: uma tool genuinamente travada não é cancelada pelo watchdog. O
`tool_slow` aos 10min + o cap duro de 30min cobrem isso; o usuário continua
podendo `/stop`.

### 2.5 Pipeline: persistência de uso (atômica)

Estender `runlog.CompletionAggregates` com:

```go
InputTokens  int64
OutputTokens int64
CostUSD      float64
ToolCount    int
```

e escrever essas colunas na mesma `UPDATE` terminal de `CompleteWithEvents`
(colunas já existem → sem migração). A fonte é o evento `result` do Bridge
(`ev.InputTokens`, `ev.OutputTokens`, `ev.CostUSD`) capturado em
`handleResultEvent`, mais `toolTracker.count()`.

Por que no terminal e não num `Update` separado: o runlog já tem
`claimRunFinalization`/`completeRunLogOwned` como único caminho de escrita
terminal; um `Update` concorrente reintroduz a corrida com a conclusão. Se o
processo morrer antes do terminal, os agregados ficam 0 — aceitável e já
registrado pelo `status`.

`recordUsage` deixa de ser só log: passa a ser o ponto de captura + log.

### 2.6 Superfícies de uso

**Telegram `/usage` real** (substitui o no-op): resolve o `session_file` atual,
chama `bridge.GetSessionStats` (15s de timeout, falha degrada para mensagem
clara) e responde:

```text
📊 Sessão atual
Contexto: 121k tokens (60% do limite de compactação de 200k)
Custo: $0,34 · Turnos: 18 · Mensagens: 96
Arquivo: ...019eec38-...jsonl
```

**TUI:** novo `EventTypeSessionStats` (ou reuso de `MsgTypeProjectState`) +
chip no header/`Project State` com `contexto 60% · $0,34`. O dado vem de
`GetSessionStats`, o mesmo caminho do lifecycle — sem custo novo de Bridge.

**Custo do `GetSessionStats`:** hoje ele cria uma sessão PI temporária com
extensions (`bridge/index.ts:2985`). O lifecycle já paga isso quando há resume;
`/usage` e o chip devem ser chamados sob demanda (comando/poll de painel), não
a cada evento.

### 2.7 Ponto de decisão proativo (P1)

`session_lifecycle.go:102-106` deixa de ser apenas `log.Printf`:

- limiar: `min(60% de compact_after, warn_threshold atual)` — default 120k.
- disparo: uma vez por sessão por janela (marcador no `TokenGuard`/session
  store), nunca repetido a cada turno.
- mensagem (Telegram e TUI, via `SendText`/estado de progresso):

```text
📈 Esta conversa está longa (~121k tokens, $0,34). Posso seguir normalmente;
se quiser um começo limpo, use /new (o histórico fica no Telegram).
```

- nenhuma ação automática: a decisão é do usuário. O `TokenGuard` continua
  sendo o fallback de emergência.

## 3. Configuração e thresholds

| Constante | Default | Onde | Muda? |
|---|---|---|---|
| `TOOL_RUNNING_TICK_MS` | 15.000 | bridge | novo |
| `TOOL_SLOW_WARN_MS` | 600.000 (10min) | bridge | novo |
| stall warning / urgent | 60s / 120s | bridge | inalterado (só suprimido com tool em voo) |
| `heartbeatInterval` / `heartbeatThreshold` | 10s / 15s | pipeline | inalterado |
| `stallHoldWindow` | 90s | pipeline | passa a cobrir `tool_running` |
| `idle_timeout_minutes` | 20 | config | inalterado |
| `bridgeExecutionTimeout` | 30min | pipeline | inalterado |
| `compact_after_input_tokens` | 200.000 | config | inalterado |
| nudge de sessão longa | 60% de compact_after | pipeline | novo |

Nenhum threshold existente é afrouxado às cegas: a supressão só vale **com
tool comprovadamente em voo**; silêncio real (modelo sem tool) mantém a
escalada atual.

## 4. Interação com o orçamento de telemetria

- `tool_running`: **não persistido**; só progresso ao vivo.
- `tool_slow`: persistido uma vez por tool como `bridge_stall` com
  `metadata.source="tool_slow"` e label redigido (cabe no orçamento).
- `stall`/`steer`/`compaction`: inalterados.
- Efeito esperado no runlog: `stall_count`/`steer_count` caem para ~0 nos runs
  com tool longa; `bridge_tool_result.duration_ms` continua explicando a
  duração real.

## 5. Riscos e mitigação

| Risco | Mitigação |
|---|---|
| Tool travada de verdade não é detectada | `tool_slow` aos 10min + cap duro 30min + `/stop` |
| Bridge antigo sem `tool_running` | Go ignora evento desconhecido; supressão é do lado do Bridge |
| `tool_running` polui o recibo do Telegram | throttle de 1,5s já existente; linha substitui, não acumula |
| Uso capturado só no terminal (perda em crash) | aceito e documentado; `interrupted` já sinaliza |
| `/usage` cria sessão PI temporária | on-demand com timeout 15s e degradação clara |
| Nudge virar spam | uma vez por sessão por janela, marcador explícito |
| Regressão do watchdog (não cancelar mais nada) | fixture: tool_running + probe falhando → cancela com `process_death` |

## 6. Testing strategy

- **Bridge (TS):** `healthDecisionFor` com tool em voo, tool lenta, transição
  tool→silêncio, flags de reset; `ToolDurationTracker.inflight`.
- **Go pipeline:** `tool_running` → `ProgressStateToolRunning` e limpeza de
  stall; `tool_slow` → warning com detalhe de tool; watchdog com `tool_running`
  não escala; watchdog com probe falhando ainda cancela.
- **Runlog:** `CompletionAggregates` com uso → colunas persistidas em uma única
  transação; compatibilidade com stores de teste.
- **Adapters:** Telegram renderiza `tool_running` + `detail` de compactação;
  TUI atualiza chip/linha e não insere nada no transcript.
- **Regressão:** fixture `79f69a1e` (Bash 180s) não produz nenhum `bridge_stall`
  nem `bridge_steer`.

## 7. Rollout

1. Implementar na branch de feature, testes por fase.
2. `cd bridge && npm run build` + `cp bundle.js ../internal/bridge/bundle.js`
   se `bridge/index.ts` mudar.
3. `go test ./... -short -count=1`, `go vet ./...`.
4. `make deploy` na branch e validação live: tool longa, stall real, `/usage`,
   compactação, nudge, TUI.
5. Revisão backend + segurança; Evidence Matrix; propor bump/changelog só
   depois de aprovação.
