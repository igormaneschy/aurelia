# Project Work State — Tasks

**Track:** `.specs/features/multi-sdk/` Phase **A** — ver `multi-sdk/tasks.md` para DAG completo.

**Branch:** `feature/project-work-state`
**Spec:** `.specs/features/project-work-state/spec.md`
**Design:** `.specs/features/project-work-state/design.md`

---

## Phase 0 — Higiene de superfície (P0, pode land primeiro)

- [x] **T0.1** `observability.EntryPointTUI = "tui"`
- [x] **T0.2** `pipeline.Config.EntryPoint` + wire Telegram / TUI / cron
- [x] **T0.3** `startRunLog` usa `s.entryPoint` em vez de hardcode `telegram`
- [x] **T0.4** `buildSurfaceInstructions` — extrair TUI block sem instruções Telegram
- [x] **T0.5** Testes: TUI prompt sem `telegram react`; runlog entrypoint tui

**Nota — gap cron:** T0.2 inclui Telegram e TUI. Cron permanece um gap documentado:
`BridgeCronRuntime` (`internal/cron/runtime.go`) não usa `pipeline.Service` nem `runlog`;
constrói o system prompt e chama `bridge.ExecuteSync` directamente. Um refactor amplo
para ligar entrypoint no cron está fora do escopo da Phase 0 (exigiria ou fazer cron
passar por `pipeline.Config` ou adicionar campo próprio de entrypoint + runlog tracking).
Decisão de tratar na Phase 1+ ou num follow-up estreito fica a cargo do architecture review.

**Critério:** `go test ./internal/pipeline/... ./internal/observability/... -short`

---

## Phase 1 — Storage (MVP core)

- [x] **T1.1** Tipos `ProjectWorkKey`, `ProjectWorkState`, `ProjectWorkPatch` em `continuity/types.go`
- [x] **T1.2** Migration `project_work_state` table em `store_sqlite.go`
- [x] **T1.3** `GetProjectWork` + `PatchProjectWork` (merge patch, sanitize, caps)
- [x] **T1.4** `FormatProjectWorkSection` em `format.go`
- [x] **T1.5** Testes unitários store + format (redaction, caps, upsert)

**Critério:** `go test ./internal/continuity/... -v`

---

## Phase 2 — Pipeline dual-write

- [x] **T2.1** `mirrorProjectWork()` em `turn_lifecycle.go`
- [x] **T2.2** Chamar mirror em `patchContinuityAfterSuccess`, `patchContinuityFailure`, `patchContinuitySessionCold`
- [x] **T2.3** Helper partilhado para evitar drift entre chat patch e project patch
- [x] **T2.4** Testes `turn_lifecycle`: cwd set → project row exists; cwd empty → no row

**Critério:** teste cross-chatID mesmo slug

---

## Phase 3 — Prompt injection

- [x] **T3.1** `buildProjectWorkSection` em `prompt_builder.go`
- [x] **T3.2** `buildContinuitySection`: cwd activo → project block; senão → chat block
- [x] **T3.3** Regra cross-surface: `LastEntrypoint != current` → always inject
- [x] **T3.4** Linha no bloco Persistent Memory sobre ai-memory vs Project Work State
- [x] **T3.5** Testes prompt_builder (cwd/no-cwd, cross-surface, stale, continuation)

**Critério:** `go test ./internal/pipeline/... -short`

---

## Phase 4 — Validação live

- [x] **T4.1** `make deploy` no daemon
- [x] **T4.2** Telegram: `/cwd aurelia`, pergunta de análise, 2–3 turnos
- [x] **T4.3** TUI: mesmo `/cwd`, “onde paramos?” / “continua”
- [x] **T4.4** `aurelia debug last` confirma entrypoint correcto em cada superfície
- [x] **T4.5** Chat mode sem cwd: confirmar que não há Project Work State no prompt (log debug)

**Critério:** aprovação Igor → `stable/project-work-state` ✅ (2026-07-10)

---

## Phase 5 — Release (após aprovação)

- [x] **T5.1** Bump v0.39.2 + CHANGELOG (security: Go 1.26.5, goldmark v1.7.17)
- [x] **T5.2** Merge `feature/project-work-state` → `main` (2026-07-10)
- [ ] **T5.3** Actualizar `.specs/features/continuity-engine/spec.md` com pointer para project work state
- [ ] **T5.4** Página ai-memory `concepts/memory-system.md` — três camadas (operational / project facts / wiki)

---

## Estimativa

| Phase | Esforço |
|---|---|
| P0 Higiene | 0.5 dia |
| P1 Storage | 0.5 dia |
| P2 Dual-write | 0.5 dia |
| P3 Prompt | 0.5 dia |
| P4 Live | 0.5 dia |
| **Total** | **~2.5 dias** |

---

## DAG

```text
T0.* ──┐
       ├──► T1.* ──► T2.* ──► T3.* ──► T4.* ──► T5.*
       │
       (T0 pode paralelizar com T1)
```