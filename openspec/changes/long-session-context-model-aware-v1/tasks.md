# Contexto model-aware — Tasks

**Change:** `long-session-context-model-aware-v1`  
**Dependency graph:** `T0 → T1 → T2 → T3 → T4 → T5`  
**Implementation branch:** `feature/long-session-context-model-aware`  
**Terminal boundary:** validação live no daemon; promoção/release exigem
aprovação explícita.

## T0 — Baseline e preflight

- [x] Baseline verde: `go build`, `go vet`, `go test ./... -short`,
      `tsc --noEmit`, `npm test` (177/177), `golangci-lint run`.
- [x] Fixture de regressão do caso 2026-09-09 (`input_tokens=506751` cumulativo,
      contexto real ~10% da janela 1M) codificada em
      `TestEvaluateLifecycle_CumulativeTokensDoNotDriveContext`.
- [x] Confirmado contrato de `get-session-stats`/`SessionStats`.

**Validation:** baseline verde; cenário de regressão definido.

## T1 — Bridge: expor contexto atual e janela

- [x] `handleGetSessionStats` devolve `context_usage_pct` (-1 = desconhecido),
      `context_tokens` e `context_window`.
- [x] `internal/bridge/protocol.go`: `SessionStats` ganha `ContextTokens` e
      `ContextWindow` com docstring distinguindo do cumulativo.
- [x] Teste Go do parse dos novos campos.

**Validation:** mock emite os três campos e o Go os parseia.

## T2 — Go: sinais e decisão por percentual

- [x] `session.HealthSignals` ganha `ContextUsagePct`, `ContextTokens`,
      `ContextWindow`.
- [x] `enrichLifecycleSignals` preenche a partir de `SessionStats`.
- [x] `SessionLifecycleConfig`/`LifecyclePolicy` ganham `WarnContextPct` (70) e
      `EmergencyRotateContextPct` (95), com defaults e validação; chaves
      absolutas ficam deprecadas.
- [x] `EvaluateLifecycle` marca `HealthLarge` por percentual (não por
      cumulativo) e ignora contexto desconhecido.
- [x] Testes: cumulativo alto + contexto baixo → healthy; 70% → large;
      desconhecido → healthy; validação de config.

**Validation:** `TestEvaluateLifecycle_CumulativeTokensDoNotDriveContext` passa;
config valida faixas.

## T3 — TokenGuard em contexto atual

- [x] `TokenGuard.Evaluate(key, contextPct, policy)` com rotação de emergência
      percentual e stall por variação do contexto.
- [x] `contextReduced` substitui `tokensReduced`; desconhecido não escala.
- [x] Testes reescritos (redução reconhecida, stall, emergência, desconhecido).

**Validation:** `go test ./internal/session` verde.

## T4 — Uso visível e nudge por percentual

- [x] `longSessionAttentionPct` = `warn_context_pct`; nudge mostra
      contexto/janela e continua 1× por sessão.
- [x] `formatSessionUsage` (Telegram `/usage`) mostra **Contexto: X / Y (Z%)**
      primeiro e **Billing** acumulado separado.
- [x] Painel TUI idem; `ProjectStatePayload` ganha `SessionContextTokens`/
      `SessionContextWindow`.
- [x] Testes: `formatSessionUsage`, nudge por percentual, payload.

**Validation:** `/usage` e painel mostram contexto atual + billing; nudge em 70%.

## T5 — Quality, review e validação live

- [x] `go build`, `go vet`, `go test ./... -short`, `-race` nos pacotes tocados.
- [x] `tsc --noEmit`, `npm test` (177/177), `golangci-lint run` (0 issues).
- [x] `make bridge` + sincronização de bundle.
- [x] Self-review (backend/segurança): métrica correta, sem segredos nos novos
      campos, bounds preservados, desconhecido não escala.
- [x] `make deploy` + validação live: `get-session-stats` retorna
      `context_tokens=15794`/`context_window=1048576`/`context_pct=1.5%` com
      `billing_in=15686` — métrica de contexto correta e distinta do cumulativo.
- [x] Evidence Matrix abaixo.
- [ ] Propor bump/changelog ao Igor.

## Evidence Matrix

| Assertion | Evidência | Status |
|---|---|---|
| A3 cumulativo não dirige contexto | `TestEvaluateLifecycle_CumulativeTokensDoNotDriveContext` | PASS |
| A3 uso observável (contexto + billing) | `TestFormatSessionUsage_ContextFirst` | PASS |
| A9 janela grande não rotaciona cedo | lifecycle por pct + `TestApplyLifecycle_TokenGuardLargeContextContinues` | PASS |
| A9 rotação de emergência por % | `TestTokenGuard_ImmediateRotateAtEmergencyCeiling`, `TestApplyLifecycle_TokenGuardImmediateRotate` | PASS |
| A9 redução de compactação detectável | `TestTokenGuard_ResetOnMeaningfulContextReduction`, `TestContextReduced` | PASS |
| A9 desconhecido não escala | `TestEvaluateLifecycle_UnknownContextIsHealthy`, `TestTokenGuard_UnknownContextNeverEscalates` | PASS |
| A9 contexto exposto | parse dos novos campos + live `/usage` | PASS |

## Explicit non-goals checklist

- [x] Não alterar `reserveTokens`/`keepRecentTokens` do SDK.
- [x] Não remover a compactação do PI SDK nem reimplementá-la.
- [x] Não mudar provider/modelo/janela.
- [x] Não impor rotação proativa por custo (só remover o falso positivo).
- [x] Não quebrar configs existentes (chaves absolutas seguem aceitas).
