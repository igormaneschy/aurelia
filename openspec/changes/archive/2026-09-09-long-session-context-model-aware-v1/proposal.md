# Contexto model-aware: parar de gerenciar contexto com métrica cumulativa

**Status:** proposed  
**Created:** 2026-09-09  
**Owner:** Aurelia architecture  
**Change type:** correctness / UX / cost  
**Base:** `main` @ v0.44.0  
**Branch:** `feature/long-session-context-model-aware`

## Why

A camada Go de lifecycle decide compactar/rotacionar sessão comparando
`input_tokens` com limiares absolutos (`compact_after=200000`,
`rotate_after=500000`). Mas `input_tokens` (de `getSessionStats()`) é a **soma
cumulativa de billing** de todos os turnos da sessão, não o tamanho do contexto
atual. O PI SDK usa outra métrica — `estimateContextTokens(messages)` vs a
**janela do modelo** — e compacta em `contextWindow − reserveTokens` (~98% da
janela).

Consequências observadas na sessão de 2026-09-09 (11 runs, modelo
`muse-spark-1.3-contributor`, janela **1.048.576**):

- às 21:23 a compactação mediu **`tokens_before=87862`** (contexto real) enquanto
  o lifecycle reportava **`input_tokens=323326`** (cumulativo) — ~4x de diferença;
- às 21:35 o `TokenGuard` rotacionou com **`input_tokens=506751`**, quando o
  contexto real era ~100–120k — **~10% da janela de 1M**;
- a rotação bloqueou o início do pedido por **145s** (21:35:54 → 21:38:20),
  sem progresso além de uma mensagem estática, e descartou continuidade;
- o `TokenGuard` "detectou 3 turnos grandes sem redução ≥5%" porque a métrica
  cumulativa **nunca diminui** (`tokensReduced` retorna false sempre), então ele
  escala para compact/rotate a cada 3 turnos, independentemente de a compactação
  ter funcionado.

Ou seja: estamos impondo um teto ~10x menor que a janela do modelo, com a
métrica errada, pagando rotação cara e perda de contexto sem necessidade.

## Goal

Deixar o PI SDK — que já é model-aware — gerenciar a compactação normal, e fazer
a camada Go decidir com o **contexto atual relativo à janela do modelo**
(`context_usage_pct`), usando percentuais em vez de tokens absolutos. Go
intervém apenas em emergência.

## In scope

- Expor `context_tokens` e `context_window` no `get-session-stats` (além do
  `context_usage_pct` já existente).
- `session.HealthSignals` ganha `ContextUsagePct`, `ContextTokens`,
  `ContextWindow`; `enrichLifecycleSignals` os preenche.
- Lifecycle e `TokenGuard` passam a decidir por percentual do contexto atual.
- Novos limiares percentuais (`warn_context_pct`, `emergency_rotate_context_pct`);
  chaves absolutas ficam deprecadas para a decisão de contexto.
- Nudge de sessão longa e `/usage`/painel TUI passam a reportar contexto atual
  (tokens/janela/%) e mantêm o cumulativo como uso de billing separado.
- Testes e validação live.

## Out of scope

- Alterar `reserveTokens`/`keepRecentTokens` do SDK (fica para follow-up; hoje
  são absolutos 16k/20k).
- Mudar provider/modelo ou a janela de qualquer modelo.
- Remover a compactação do PI SDK ou reimplementá-la no Go.
- Rotação proativa por política de custo (decisão de produto; aqui só se remove
  o falso positivo e se dá base percentual para decidir depois).
- Bump de versão/CHANGELOG/promoção (aprovação separada).

## Decision

1. **Métrica**: `context_usage_pct` é a fonte de verdade para tamanho de
   contexto. `input_tokens` cumulativo passa a ser apenas billing/observabilidade.
2. **Dono**: PI SDK compacta no limite da janela; Go só escala em emergência.
3. **Limiares**: percentuais da janela, não absolutos.
4. **Desconhecido**: se o SDK não conseguir estimar o contexto (`percent=nil`
   logo após compactação, antes de um assistant pós-compactação), o Go **não**
   escala por contexto — o SDK e o provider cobrem o limite.

## Priorities

| Prioridade | Frente | Resultado esperado |
|---|---|---|
| P0 | Métrica correta | zero rotação/compactação por cumulativo |
| P0 | TokenGuard em contexto atual | redução de compactação volta a ser detectável |
| P0 | Limiares percentuais | 1M de janela não rotaciona em 500k cumulativos |
| P1 | Uso visível correto | `/usage`/TUI mostram contexto atual vs billing |
| P1 | Nudge por % | aviso reflete uso real da janela |

## Prior lessons applied

- `incident-regression-from-daemon-logs.md` → o caso 21:35 (rotação em 10% da
  janela) vira fixture de regressão.
- `delegate-to-dependency` → o SDK já é model-aware; não duplicar a decisão.
- `redaction-before-truncation.md` → novos campos de stats continuam sem
  segredos e bounded.
- `post-impl-review-gaps.md` → revisão backend após implementação.

## References

- `bridge/index.ts:3144` — `context_usage_pct` no `get-session-stats`.
- `bridge/node_modules/@earendil-works/pi-coding-agent/dist/core/agent-session.js:2648` — `getSessionStats()` soma usage (cumulativo).
- `.../core/agent-session.js:2700` — `getContextUsage()` (contexto atual / janela).
- `.../core/compaction/compaction.js:160` — `shouldCompact(contextTokens, contextWindow, settings)`.
- `internal/session/lifecycle.go:30` — `HealthSignals`.
- `internal/session/token_guard.go:125` — `tokensReduced` (quebrado com cumulativo).
- `internal/pipeline/session_lifecycle.go:102` — decisão por `InputTokens`.
- `internal/bridge/protocol.go:54` — `SessionStats` (já tem `ContextUsagePct`).
