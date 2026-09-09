# Contexto model-aware — Tasks

**Change:** `long-session-context-model-aware-v1`  
**Dependency graph:** `T0 → T1 → T2 → T3 → T4 → T5`  
**Implementation branch:** `feature/long-session-context-model-aware`  
**Terminal boundary:** validação live no daemon; promoção/release exigem
aprovação explícita.

## T0 — Baseline e preflight

- [ ] Registrar baseline: `go test ./... -short -count=1`, `go vet ./...`,
      `cd bridge && npx tsc --noEmit && npm test`, `golangci-lint run`.
- [ ] Exportar fixture de regressão do caso 2026-09-09 21:35 (`input_tokens`
      cumulativo 506751, contexto real ~100k, janela 1M) e do caso 21:23
      (`tokens_before=87862`).
- [ ] Confirmar contrato de `get-session-stats` e `SessionStats` em Go.
- [ ] Parar e reportar blocker se o baseline ou as fixtures falharem.

**Validation:** baseline verde e fixtures definidas; nenhuma mudança runtime.

## T1 — Bridge: expor contexto atual e janela

- [ ] `handleGetSessionStats` inclui `context_tokens` e `context_window`
      (0/null quando o SDK não estima).
- [ ] `internal/bridge/protocol.go`: `SessionStats` ganha `ContextTokens` e
      `ContextWindow`; parsing tolerante a ausência.
- [ ] Testes TS do payload e Go do parse.

**Validation:** `get-session-stats` devolve os três campos de contexto; ausência
degrada para 0 sem erro.

## T2 — Go: sinais e decisão por percentual

- [ ] `session.HealthSignals` ganha `ContextUsagePct` (-1 desconhecido),
      `ContextTokens`, `ContextWindow`.
- [ ] `enrichLifecycleSignals` preenche a partir de `SessionStats`.
- [ ] `SessionLifecycleConfig` ganha `warn_context_pct` (70) e
      `emergency_rotate_context_pct` (95); `Validate` valida faixas e mantém as
      chaves absolutas aceitas (deprecadas).
- [ ] `LifecyclePolicy` propaga os percentuais.
- [ ] `applyLifecycle`/lifecycle deixam de escalar por `InputTokens`; escalam
      só por `ContextUsagePct` (emergência) e não escalam quando desconhecido.
- [ ] Testes: cumulativo alto não rotaciona; percentual de emergência rotaciona;
      desconhecido não escala.

**Validation:** fixture do caso 506k/1M não rotaciona; 95% rotaciona.

## T3 — TokenGuard em contexto atual

- [ ] `TokenGuard.Evaluate` passa a receber percentual do contexto (ou tokens do
      contexto atual + janela).
- [ ] Detecção de stall usa a variação do contexto atual após compactação
      (redução volta a ser detectável); reduzir para `ActionCompact` só como
      fallback.
- [ ] `Reset` e saturação preservados; desconhecido não escala.
- [ ] Testes: redução reconhecida, não-redução escalona, desconhecido no-op.

**Validation:** `go test ./internal/session` cobre os três caminhos.

## T4 — Uso visível e nudge por percentual

- [ ] `longSessionAttentionThreshold` passa a ser percentual da janela
      (`warn_context_pct`); nudge continua uma vez por sessão.
- [ ] `/usage` (Telegram) mostra contexto atual/janela/% em destaque e billing
      acumulado separado.
- [ ] Painel TUI idem; `ProjectStatePayload` ganha os campos de contexto.
- [ ] Testes de `cmdUsage`/painel e do nudge por percentual.

**Validation:** `/usage` e painel mostram contexto atual + billing; nudge dispara
em 70% da janela.

## T5 — Quality, review e validação live

- [ ] `go build`, `go vet`, `go test ./... -short`, `-race` nos pacotes tocados.
- [ ] `cd bridge && npx tsc --noEmit && npm test`; `golangci-lint run`.
- [ ] `make bridge` se `bridge/index.ts` mudou.
- [ ] Code review (backend) e security review (stats sem segredos, bounds).
- [ ] `make deploy` + validação live: sessão longa com janela 1M **não** rotaciona
      por cumulativo; `/usage` mostra contexto atual; compactação do SDK
      observada no log sem escalada Go.
- [ ] Evidence Matrix para A3 (modified) e A9.
- [ ] Propor bump/changelog ao Igor.

**Validation:** cada assertion tem evidência; PASS sem evidência é UNVERIFIED.

## Explicit non-goals checklist

- [ ] Não alterar `reserveTokens`/`keepRecentTokens` do SDK.
- [ ] Não remover a compactação do PI SDK nem reimplementá-la.
- [ ] Não mudar provider/modelo/janela.
- [ ] Não impor rotação proativa por custo (só remover o falso positivo).
- [ ] Não quebrar configs existentes (chaves absolutas seguem aceitas).
