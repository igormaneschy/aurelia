# Feedback honesto e visibilidade em sessões longas

**Status:** proposed  
**Created:** 2026-09-09  
**Owner:** Aurelia architecture  
**Change type:** UX / observability / correctness  
**Base:** `main` @ v0.43.3  
**Branch:** `feature/long-session-feedback-v2`

## Why

A `long-session-ux-review-v1` tornou a sessão longa observável *por dentro*
(stall/steer/compaction/first-feedback no runlog) e liveness-aware. A revisão
dos logs e do runlog atuais mostra que a camada de instrumentação ficou boa,
mas a **interpretação** do sinal continua errada em dois pontos que o usuário
sente direto:

1. **Ferramenta longa é tratada como modelo travado.** O health monitor do
   Bridge só atualiza `lastEventTime` no início e no fim de uma tool
   (`bridge/index.ts:2486-2500`). Enquanto um `bash` legítimo roda, o relógio
   de silêncio anda, e aos 60s/120s o Bridge emite `stall` **e injeta um steer
   dizendo ao modelo para parar e resumir**. O usuário recebe aviso de falha
   durante trabalho normal.
2. **Contexto, custo e compactação são invisíveis.** `recordUsage` só faz
   `log.Printf` (`internal/pipeline/pipeline.go:1305`); as colunas
   `input_tokens`/`output_tokens`/`cost_usd`/`tool_count` do `run_journal`
   ficam zeradas; `/usage` é um no-op; e o detalhe "contexto compactado" é
   descartado pelos dois adapters. Em sessão longa o usuário não vê que a
   conversa ficou cara, que o contexto foi podado, nem recebe chance de decidir
   antes de a compactação ser forçada.

### Evidência operacional

Fonte: `~/.aurelia/logs/aurelia.stderr.log` (janela 2026-05-11 → 2026-09-04),
`~/.aurelia/data/runlog.db` (89 runs) e `aurelia debug metrics`.

- 3.129 eventos `streaming stall`; 1.260 acima de 600s; pico de 1.786s.
- Nos runs `rid=run-*`: **457 stalls ≥60s com `bash` como última ferramenta**;
  **25 runs distintos** (de 59 observados) tiveram ≥1 stall falso por tool.
- Caso reproduzido no runlog: `run 79f69a1e`, um único `Bash` de
  **180,003s** produziu `bridge_stall` warning (61s) + urgent (121s) e
  `bridge_steer` warning + urgent — dois avisos ao usuário e duas mensagens
  "pare o que está fazendo" ao modelo. Idem `db4803d6` (180,001s) e
  `f49e866c` (79,148s).
- Em **89/89 runs** `input_tokens = output_tokens = cost_usd = tool_count = 0`,
  apesar de `bridge_result` carregar `tokens_in=399368 tokens_out=28731
  cost=$0.0561`. `debug metrics` reporta custo 0.
- Sessão real escalando 295k → 399k → 499k → 571k tokens no mesmo dia até o
  `TokenGuard` forçar compactação, sem nenhuma sinalização ao usuário
  (`session_lifecycle.go:103` é apenas `log.Printf`).
- 13 compactações registradas; maior `tokens_before=146528`; sessões de até
  2,5 MB / 1.286 mensagens.
- Cancelamento por idle já matou trabalho legítimo: 2026-05-27
  `idle timeout (20m0s) — cancelling context` durante um `bash` de 16min.

### Qualidade da evidência

- As contagens de stall vêm de grep no stderr (linhas do Bridge sem `rid`
  completo são ignoradas); a atribuição "última tool" é heurística por run.
- Os 89 runs do runlog cobrem ~3 semanas e são a base dos números de
  tokens/custo zerados.
- O `run_events` é limitado pelo orçamento de telemetria por run; contagens de
  stall/steer podem estar subestimadas em runs muito longos.

## Goal

Fazer o feedback de uma sessão longa ser **verdadeiro e acionável**: o usuário
vê o que o agente está realmente fazendo (tool em execução, há quanto tempo),
é avisado quando o modelo de fato silencia, enxerga o peso/custo do contexto e
decide quando começar uma sessão nova — sem avisos falsos e sem compactação
silenciosa.

## In scope

- Health monitor do Bridge ciente de ferramenta em execução (`tool_running`),
  com supressão de stall/steer enquanto houver tool em voo.
- Estado de progresso surface-neutral com distinção explícita entre
  "tool em execução" e "modelo silencioso"; renderização do `detail` (inclui
  compactação) em Telegram e TUI.
- Watchdog de liveness tratando tool em voo como atividade (sem cancelar
  trabalho legítimo por silêncio).
- Persistência atômica de tokens/custo/tool_count no terminal do run e
  superfícies de uso: `/usage` real no Telegram e indicador de contexto/custo
  na TUI.
- Ponto de decisão proativo de sessão longa (um aviso, decisão do usuário).
- Testes unitários/fixtures para cada cenário e validação live documentada.

## Out of scope

- Recibo Telegram rico por tool, cronômetro por tool na TUI, paginação de
  histórico da TUI e registro real de `/continuar` → change de follow-up
  `long-session-surfaces-v2`.
- Alterar `bridgeExecutionTimeout` (30min) ou a política do `TokenGuard`.
- Trocar provider/modelo, protocolo NDJSON além do evento documentado,
  deploy/`Makefile`, rotação summary-seeded.
- Bump de versão, `CHANGELOG.md`, `stable/*`, merge ou push.

## Decision

Executar em três fases, nesta ordem:

1. **Feedback honesto (P0)** — o Bridge e o watchdog passam a entender tool em
   execução; nenhum aviso falso, nenhum steer de "pare" durante tool.
2. **Visibilidade de contexto/custo (P0)** — persistir e expor uso; compactação
   deixa de ser silenciosa.
3. **Decisão proativa (P1)** — avisar uma vez quando a sessão fica pesada e
   deixar a escolha com o usuário.

## Priorities

| Prioridade | Frente | Resultado esperado |
|---|---|---|
| P0 | Tool em execução não é stall | zero aviso/steer falso durante tool; usuário vê a tool e o tempo |
| P0 | Watchdog não mata tool longa | silêncio real continua escalando; tool em voo não cancela |
| P0 | Uso persistido e visível | tokens/custo/contexto consultáveis em Telegram e TUI |
| P0 | Compactação visível | delta de tokens informado ao usuário, uma vez por run |
| P1 | Ponto de decisão de sessão longa | usuário decide entre seguir e `/new` |

## Prior lessons applied

- `incident-regression-from-daemon-logs.md` → o caso `79f69a1e` vira fixture de
  regressão, não apenas um threshold ajustado.
- `goroutine-recovery-mandatory.md` → novas goroutines de heartbeat levam
  `defer recover()`.
- `redaction-before-truncation.md` → labels de tool passam por
  `normalizeToolLabel` antes de qualquer persistência/log.
- `post-impl-review-gaps.md` → revisão backend + segurança antes de qualquer
  operação mutável.

## Production path

```text
Telegram/TUI
  → internal/pipeline (progress surface-neutral, watchdog, runlog)
  → internal/bridge (Go/NDJSON)
  → bridge/index.ts (health monitor ciente de tool)
  → PI SDK (AgentSession / ToolDurationTracker)
```

## Surface scope

- **Core compartilhado:** `bridge/index.ts`, `internal/bridge`,
  `internal/pipeline`, `internal/runlog`.
- **Telegram:** estado `tool_running` e `detail` de compactação no recibo único;
  `/usage` real.
- **TUI:** mesmo estado via `EventTypeProgress`; chip de contexto/custo.

O contrato de progresso continua surface-neutral; só o adapter de apresentação
varia.

## Rollout boundary

A change termina com implementação na branch de feature, testes locais,
revisão e validação live documentada (Telegram + TUI). `stable/*`, bump de
versão, changelog, merge, push e mudanças de deploy exigem aprovação explícita
posterior do Igor.

## References

- `bridge/index.ts:723` — `ToolDurationTracker`.
- `bridge/index.ts:2611` — health monitor (stall/steer).
- `internal/pipeline/liveness_timeout.go` — watchdog e `stallPriorityReporter`.
- `internal/pipeline/pipeline.go:812,1305` — heartbeat e `recordUsage`.
- `internal/runlog/store_sqlite.go:474` — `CompleteWithEvents`.
- `internal/telegram/progress.go:124` / `internal/tui/update.go:1537` —
  adapters que descartam o `detail`.
- `internal/bridge/protocol.go:54` — `SessionStats` (já traz custo e
  `context_usage_pct`).
- `openspec/specs/long-session-ux/spec.md` — requisitos A1–A5 vigentes.
