# Tool Monitoring

> Documentação técnica do subsistema de monitoramento de tool calls no `internal/pipeline`.
> Criado em Junho 2026 após discussão de design sobre loop detection, heartbeat e AddToolEvent.

## Problema

Runs de longa duração com muitas tool calls eram silenciosas: o utilizador não sabia se o modelo
estava a trabalhar, em loop ou simplesmente parado. Sem feedback proactivo, timeouts de 30 min
surgiam sem aviso, e sessões com loops custavam tokens sem entregar resultado.

## Componentes

### 1. `toolCallTracker` — Explosão de tool calls

**Ficheiro:** `tool_monitoring.go`

Contabiliza tool calls cumulativos por turn. Em dois limiares envia:
1. Uma mensagem Telegram para o utilizador (human-language, sem contagens técnicas)
2. Um `steer` para o modelo pedindo que consolide

| Constante | Valor | Acção |
|---|---|---|
| `toolCallWarningThreshold` | 20 | Aviso + steer de consolidação |
| `toolCallCriticalThreshold` | 50 | Aviso crítico + steer urgente |

**Steer format:** inclui contagem, nome da última tool, e elapsed time desde `startedAt`.

```
"Você já usou ferramentas 20 vezes (Read) em 4m30s. Consolide o que já descobriu."
```

**Nota:** As mensagens ao utilizador (`sendWarning`) são intencionalmente vagas —
não expõem contagens técnicas. Só o steer ao modelo é preciso.

---

### 2. `loopDetector` — Padrões repetitivos

**Ficheiro:** `tool_monitoring.go`

Mantém um ring buffer circular das últimas `loopDetectorWindow=12` tool calls.
Cada entrada é um `toolCallSnapshot{name, inputFp}` onde `inputFp` é o JSON dos
argumentos truncado em 200 chars.

**Padrões detectados:**

| Padrão | Descrição | Limiar |
|---|---|---|
| Consecutive repeat | Mesma tool+input N vezes seguidas | `loopRepeatThreshold=3` |
| Ping-pong | A-B-A-B com mesmos inputs | `loopPingPongLength=4` |
| Tool spiral | Só tools com prefixo "read" nas últimas N calls | `loopOnlyReadLength=8` |

**Detecção de spiral:** usa `strings.HasPrefix(strings.ToLower(name), "read")` — 
case-insensitive e match por prefixo, cobrindo `Read`, `ReadFile`, `read_file`, etc.

**Reset por turn:** o campo `warned bool` é resetado no início de cada novo turn
(chamada a `ResetForNewTurn()` no case `result` de `ProcessBridgeEvents`). Isto
garante que um segundo loop numa sessão longa também emite aviso.

**Em caso de detecção:**
1. Telegram: `"🔁 Vou consolidar o que já encontrei para evitar repetição."`
2. Steer: menciona o nome da tool em loop e pede ao modelo para parar e resumir

---

### 3. Heartbeat Monitor — Silêncio prolongado

**Ficheiro:** `pipeline.go:heartbeatMonitor`

Goroutine independente que detecta períodos sem tool_use events (modelo a raciocinar
sem usar ferramentas).

| Constante | Valor |
|---|---|
| `heartbeatInterval` | 10s (tick do ticker) |
| `heartbeatThreshold` | 15s (sem tool_use → envia mensagem) |
| `heartbeatToolThreshold` | 8 (a cada 8 beats inclui elapsed) |

**Fluxo:**
```
toolUseSignal ch ──► reset lastTool timer
ctx.Done()    ──► stop goroutine
ticker (10s)  ──► se since(lastTool) ≥ 15s → sendText heartbeat
```

**Formato da mensagem:**
- Normal: `"⏱️ 25s — Ainda estou processando."`
- Com elapsed (beats múltiplos de 8): `"⏱️ 4m10s — Ainda estou trabalhando no pedido. Vou consolidar o progresso em breve."`

---

### 4. `AddToolEvent` — Integração com nudgeBuffer

**Ficheiro:** `turn_lifecycle.go:afterSuccessfulTurn`

Após um turn bem-sucedido, o tool summary do run (lista de tools usadas, ex:
`"Read, Write, Bash → [ok]", "Read, Grep"`) é propagado para o `nudgeBuffer`
via `AddToolEvent`. Isto permite ao sistema de nudges considerar o padrão de
tools usadas na sessão ao decidir sugestões proactivas.

**Regra:** só chama `AddToolEvent` se o summary não for vazio. A chamada
acontece antes de `AfterTurnNudge` para que o nudge engine já veja os dados.

---

## Wiring em `ProcessBridgeEvents`

```
case "tool_use":
    toolTracker.increment(toolName)       // explosão
    loopDetect.record(toolName, ev.Input) // loops
    toolUseSignal <- struct{}{}            // heartbeat reset
    recordPipelineEvent(...)              // observability

case "result":
    loopDetect.ResetForNewTurn()          // reset warned para próximo turn
    → handleResultEvent
```

## Thresholds e tuning

Todos os limiares são constantes em `pipeline.go` e `tool_monitoring.go`.
Para ajustar sem tocar na lógica:

```go
// pipeline.go
toolCallWarningThreshold  = 20
toolCallCriticalThreshold = 50
heartbeatInterval         = 10 * time.Second
heartbeatThreshold        = 15 * time.Second
heartbeatToolThreshold    = 8

// tool_monitoring.go
loopDetectorWindow  = 12
loopRepeatThreshold = 3
loopPingPongLength  = 4
loopOnlyReadLength  = 8
```

## Decisões de design relevantes

### Por que steer e não cancel?

`steer` injeta uma mensagem no contexto do modelo sem interromper a execução.
Cancelar seria destrutivo — o modelo pode estar genuinamente perto de concluir.
O steer é a forma menos invasiva de reorientar o comportamento.

### Por que não configurar os thresholds via `app.json`?

Os valores actuais (20/50 tool calls, 15s heartbeat) são conservadores e
cobrem 99% dos casos. Configurabilidade adicionaria superfície de configuração
sem benefício claro. Pode ser revisitado se houver agentes com padrões
radicalmente diferentes (ex: agentes de análise de grandes codebases).

### Por que ring buffer e não slice crescente?

O ring buffer tem memória O(1) fixa independentemente da duração do run.
Um slice crescente teria custo proporcional ao número de tool calls, que pode
chegar às centenas. O custo de detectar loops nas últimas 12 calls é negligível.
