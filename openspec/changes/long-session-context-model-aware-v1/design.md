# Contexto model-aware — Design

## 1. Current state (as-implemented, v0.44.0)

### 1.1 Duas métricas, uma errada

| Fonte | Campo | Semântica | Uso atual no Go |
|---|---|---|---|
| `getSessionStats()` | `tokens.input` | **soma cumulativa** de `usage.input` de todos os turnos (`agent-session.js:2648`) | `HealthSignals.InputTokens` → lifecycle + `TokenGuard` |
| `getContextUsage()` | `percent` | `estimateContextTokens(messages) / model.contextWindow * 100` (`agent-session.js:2700`) | exposto como `context_usage_pct`, só exibido em `/usage` |

### 1.2 Compactação do SDK (model-aware)

```js
shouldCompact(contextTokens, contextWindow, settings) {
  if (!settings.enabled) return false;
  return contextTokens > contextWindow - settings.reserveTokens;
}
```
Defaults: `reserveTokens = 16384`, `keepRecentTokens = 20000`
(`settings-manager.js:560-563`). Para `muse-spark-1.3-contributor`
(`contextWindow = 1048576`) compacta em ~1.032.000 tokens.

### 1.3 O que o Go faz hoje

- `internal/config/config.go:90` — `CompactAfterInputTokens: 200000`,
  `RotateAfterInputTokens: 500000` (absolutos).
- `internal/pipeline/session_lifecycle.go:102` — compara
  `signals.InputTokens` (cumulativo) a esses limiares.
- `internal/session/token_guard.go` — `Evaluate(key, inputTokens, policy)`:
  - `inputTokens >= rotate_after` → `ActionRotate` (hard ceiling);
  - N turnos consecutivos "sem redução ≥5%" → `ActionCompact`.
- `internal/session/token_guard.go:125` — `tokensReduced(prev, cur)` retorna
  `false` quando `cur >= prev`; com cumulativo, `cur` **nunca** cai → o contador
  de stall incrementa todo turno → compact a cada `largeTurnsBeforeCompact`.
- `internal/pipeline/session_lifecycle.go:longSessionAttentionThreshold` —
  nudge = `min(60% de compact_after, warn)` sobre cumulativo.

### 1.4 Evidência (2026-09-09)

- `21:23:43` `compaction_start` (TokenGuard, 3 turnos "sem redução") → `21:24:11`
  `compaction_end tokens_before=87862 tokens_after=20281` (28s, efetiva).
- `21:35:54` `token guard ... action=rotate reason="input_tokens=506751 >= rotate_after=500000"`.
- `21:38:19` `rotation succeeded` → **145s** depois.
- `run 6ac26690` `first_feedback_ms=160s` (a espera é a rotação, não o modelo).

## 2. Target architecture

### 2.1 Bridge: stats completos de contexto

`get-session-stats` passa a devolver, além do que já devolve:

```json
{
  "context_usage_pct": 8.4,
  "context_tokens": 88000,
  "context_window": 1048576
}
```

- `context_tokens`/`context_window` vêm de `stats.contextUsage`
  (`tokens`/`contextWindow`); `null` quando o SDK não consegue estimar.
- `internal/bridge/protocol.go` `SessionStats` ganha `ContextTokens int` e
  `ContextWindow int` (o `ContextUsagePct float64` já existe).

### 2.2 Go: sinais model-aware

`session.HealthSignals` ganha:

```go
ContextUsagePct float64 // -1 = desconhecido
ContextTokens   int
ContextWindow   int
```

`enrichLifecycleSignals` (`session_lifecycle.go:315`) preenche a partir de
`stats`. `InputTokens` continua sendo preenchido, mas **não decide contexto**
(segue para billing/observabilidade).

### 2.3 Configuração percentual

`SessionLifecycleConfig` ganha:

| Chave | Default | Semântica |
|---|---|---|
| `warn_context_pct` | `70` | aviso/nudge de sessão longa |
| `emergency_rotate_context_pct` | `95` | rotação de emergência (janela) |

Depreciações (mantidas por compatibilidade de config, **ignoradas** na decisão
de contexto):

- `compact_after_input_tokens`
- `rotate_after_input_tokens`

`Validate` continua aceitando as antigas (não quebra configs existentes), mas
os novos defaults percentuais passam a reger o comportamento.

### 2.4 Lifecycle

`EvaluateLifecycle` continua decidindo por saúde (cold/suspect/healthy). A
camada de contexto (`applyLifecycle` + `TokenGuard`) passa a usar:

```text
contextPct = signals.ContextUsagePct   // -1 = desconhecido
if contextPct < 0            -> sem escalada por contexto (SDK/provider cobrem)
if contextPct >= emergency   -> ActionRotate (emergência)
if contextPct >= warn        -> nudge único (não força ação)
```

- `compact_after` deixa de existir como gatilho Go: a compactação normal é do
  SDK (que compacta em ~98% da janela).
- `rotate_after` absoluto sai do caminho; a rotação só ocorre em emergência
  percentual.

### 2.5 TokenGuard (reimplementado em contexto atual)

```go
func (g *TokenGuard) Evaluate(key SessionKey, contextPct float64, policy LifecyclePolicy) (Decision, bool)
```

- `contextPct >= emergency` → `ActionRotate` (hard ceiling percentual).
- Stall: N leituras consecutivas com `contextPct` **sem redução ≥5%** após uma
  compactação → `ActionCompact` (fallback). Agora a redução é detectável porque
  o contexto atual **cai** de fato após compactar.
- `contextPct` desconhecido → não escala.

### 2.6 Uso visível

- `/usage`: mostra **Contexto: 88k / 1.05M (8%)** em destaque e o **uso
  acumulado** (input/output/custo) como seção separada de billing.
- Painel TUI: idem.
- `aurelia debug metrics`: custo/tokens continuam por run (billing), sem mudança.

## 3. Thresholds (antes → depois)

| Conceito | Antes | Depois |
|---|---|---|
| Compactação normal | Go em 200k cumulativos | **SDK** em ~98% da janela |
| Rotação | 500k cumulativos | `emergency_rotate_context_pct=95` |
| Aviso/nudge | 120k cumulativos | `warn_context_pct=70` |
| Métrica de decisão | `input_tokens` cumulativo | `context_usage_pct` |

## 4. Riscos e mitigação

| Risco | Mitigação |
|---|---|
| `context_usage_pct` indisponível logo após compactação | não escalar por contexto; SDK/provider cobrem |
| Janela desconhecida (modelo sem `contextWindow`) | `percent` volta 0/null → sem escalada |
| Sessão crescer até o limite do provider | SDK compacta em ~98%; provider emite context-overflow e o SDK retenta |
| Custo por turno maior em janelas grandes | aviso percentual ao usuário; decisão de custo é de produto |
| Regressão de comportamento de config | chaves antigas aceitas; defaults novos percentuais |

## 5. Testing strategy

- Bridge (TS): `get-session-stats` inclui `context_tokens`/`context_window`
  (e `null`/0 quando indisponível).
- Go bridge: `SessionStats` parseia os novos campos.
- `session`: `TokenGuard.Evaluate` por percentual — rotação em 95, stall quando
  o contexto não cai após compactação, no-op quando desconhecido.
- `pipeline`: lifecycle escala só por `context_usage_pct`; `InputTokens`
  cumulativo alto **não** rotaciona (regressão do caso 506k/1M).
- `telegram`/`tui`: `/usage` e painel mostram contexto atual + billing.

## 6. Rollout

1. Implementar na branch de feature, testes por fase.
2. `make bridge` se `bridge/index.ts` mudar.
3. Gates: `go test ./... -short`, `-race`, `go vet`, `tsc`, `npm test`,
   `golangci-lint run`.
4. `make deploy` e validação live: sessão com janela 1M não rotaciona por
   cumulativo; `/usage` mostra contexto atual; compactação do SDK observada.
5. Revisão backend; Evidence Matrix; bump/changelog só com aprovação.
