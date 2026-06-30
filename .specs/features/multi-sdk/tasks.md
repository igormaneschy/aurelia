# Multi-SDK — Task DAG (track completo)

**Master spec:** `.specs/features/multi-sdk/spec.md`

Este ficheiro ordena trabalho através de **features já spec'd**. Não substitui os `tasks.md` de cada feature — referencia-os.

---

## Visão geral

```text
[A] project-work-state ──────┐
                             ├──► [C] multi-harness-routing
[B] bridge-adapter-interface ┘              │
                                            ▼
                                    [D] second-harness (TBD)
```

---

## Phase A — Cross-surface continuity

**Spec:** `.specs/features/project-work-state/`  
**Branch:** `feature/project-work-state`  
**Tasks:** ver `project-work-state/tasks.md` (Phases 0–5)

**Exit criteria:**
- [ ] `ProjectWorkState` em SQLite
- [ ] Prompt: cwd → project block; sem cwd → chat continuity
- [ ] `entrypoint: tui` no runlog
- [ ] Live: Telegram → TUI “onde paramos?”

**Bloqueia:** nada. **Recomendado antes de C** para validar prompt assembly estável.

---

## Phase B — Engine costura

**Spec:** `.specs/features/bridge-adapter-interface/`  
**Branch:** `refactor/bridge-adapter-interface`  
**Tasks:** ver `bridge-adapter-interface/tasks.md` (Tasks 1–7)

**Exit criteria:**
- [ ] `internal/engine/` + `PIAdapter`
- [ ] Pipeline sem `bridge.Request` em produção
- [ ] `ARCHITECTURE.md` actualizado

**Bloqueia:** Phase C  
**Pode paralelizar com A** se equipas/files disjoint (engine vs continuity)

---

## Phase C — Harness routing

**Spec:** `.specs/features/prompt-profiles/spec.md` §Phase 3 + `multi-sdk/design.md`  
**Branch:** `feature/multi-harness-routing`  
**Depende de:** A ✅ (recomendado), B ✅ (obrigatório)

### C.1 Registry

- [ ] `engine.Registry` com `Register` / `Resolve` / fail-closed
- [ ] Wire em `app.go`: `reg.Register("pi", NewPIAdapter(b))`
- [ ] Testes: unknown harness → error

### C.2 Pipeline routing

- [ ] `pipeline.Config.Registry *engine.Registry`
- [ ] Resolver harness: `profile.Harness` → default `pi`
- [ ] Substituir `s.bridge` directo por `registry.Resolve(harness)`
- [ ] Mensagem utilizador: `Harness "%s" ainda não está disponível.`

### C.3 Session key + harness

- [ ] `SessionKey.Harness` em `internal/session`
- [ ] Migração sessões existentes → `harness: pi`
- [ ] Resume/continue scoped por harness

### C.4 Observabilidade

- [ ] `run_journal.harness` column (migration runlog)
- [ ] `startRunLog` popula `harness` + `entrypoint`
- [ ] `aurelia debug last` mostra ambos
- [ ] `/status` opcional: harness activo

### C.5 Prompt

- [ ] Runtime Identity menciona harness efectivo
- [ ] Testes: switch harness → ProjectWorkState mantém-se; session nova

### C.6 Docs

- [ ] Actualizar `AGENT_RESPONSIBILITY_MODEL.md` — três camadas
- [ ] Actualizar `prompt-profiles/spec.md` Phase 3 checkboxes
- [ ] ai-memory wiki page `concepts/memory-system.md` — três camadas

**Exit criteria:**
- [ ] `go test ./... -short`
- [ ] Live: `@coder` (pi) inalterado; profile `harness: fake` fail-closed
- [ ] Deploy + smoke Telegram + TUI

---

## Phase D — Segundo harness (placeholder)

**Spec:** criar `.specs/features/multi-sdk/second-harness.md` quando motor escolhido  
**Branch:** `feature/second-harness-<name>`  
**Depende de:** C ✅

### D.0 Selecção (pré-impl)

- [ ] Igor escolhe candidato (ver `design.md` critérios)
- [ ] Spec dedicada com mapeamento eventos → `engine.Event`
- [ ] Estratégia ai-memory (MCP nativo vs sidecar)

### D.1 Implementação (esboço)

- [ ] `internal/<name>/adapter.go` implements `engine.Engine`
- [ ] `reg.Register("<name>", adapter)`
- [ ] Profile exemplo `harness: <name>`
- [ ] Testes integração + matriz cross-harness continuity

---

## Estimativas

| Phase | Esforço | Risco |
|---|---|---|
| A project-work-state | ~2.5d | Baixo |
| B bridge-adapter | ~2–3d | Médio (pipeline touch) |
| C harness routing | ~2d | Médio (session key migration) |
| D second harness | ~3–5d+ | Alto (depende do motor) |

**Total até multi-SDK “ready” (sem 2º motor):** ~7d  
**Total com 2º motor:** +3–5d após escolha

---

## Ordem de merge recomendada

```text
main
  ← stable/project-work-state      (A)
  ← stable/bridge-adapter-interface (B) — pode rebase sobre A
  ← stable/multi-harness-routing    (C) — rebase sobre A+B
```

Não mergear C sem B. Não mergear D sem C validado live.

---

## Validação end-to-end (track A+C, PI único)

1. Telegram `/cwd aurelia` — 3 turnos análise
2. TUI mesmo `/cwd` — “continua” (A)
3. `aurelia debug last` — `entrypoint=tui`, `harness=pi` (A+C)
4. Profile `harness: pi` — sem regressão (C)
5. Profile `harness: xyz` — erro claro, sem bridge call (C)