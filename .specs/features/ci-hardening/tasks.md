# CI Hardening — Tasks

## Execution Plan

### Phase 1: Tooling Configuration (local)

| ID | Task | What | Where | Depends | Done When | Verify |
|----|------|------|-------|---------|-----------|--------|
| T1 | [P] Instalar golangci-lint e gosec | `brew install golangci-lint` + `go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest` | local system | — | Ferramentas disponíveis no PATH | `golangci-lint --version && gosec --version` |
| T2 | Expandir `.golangci.yml` | Adicionar linters: errcheck, govet, ineffassign, unused, misspell, goconst, gocritic | `.golangci.yml` | T1 | Arquivo atualizado com config completa | `golangci-lint linters` lista todos |
| T3 | Rodar lint baseline e corrigir issues | `golangci-lint run ./...` e corrigir qualquer finding | Vários arquivos .go | T2 | Zero findings no lint | `golangci-lint run ./...` sai com 0 |
| T4 | [P] Rodar gosec baseline e corrigir/triar | `gosec ./...` e corrigir findings ou adicionar `//nolint:gosec` | Vários arquivos .go | T1 | Zero findings (ou triados com justificativa) | `gosec ./...` sai com 0 |

### Phase 2: Makefile

| ID | Task | What | Where | Depends | Done When | Verify |
|----|------|------|-------|---------|-----------|--------|
| T5 | Adicionar targets `lint`, `sec`, `check` | Targets Makefile com mensagens de erro amigáveis | `Makefile` | T3, T4 | `make check` roda e passa | `make check` |

### Phase 3: CI Pipeline

| ID | Task | What | Where | Depends | Done When | Verify |
|----|------|------|-------|---------|-----------|--------|
| T6 | [P] Adicionar job `lint` ao CI | `golangci/golangci-lint-action@v7` com `--timeout=5m` | `.github/workflows/ci.yml` | T2 | CI roda lint em push/PR | Push na branch → CI job aparece |
| T7 | [P] Adicionar job `security` ao CI | `gosec` + `govulncheck` steps, com triagem de CVE sem fix | `.github/workflows/ci.yml` | T4 | CI roda security scan em push/PR | Push na branch → CI job aparece |

### Phase 4: Validation & Docs

| ID | Task | What | Where | Depends | Done When | Verify |
|----|------|------|-------|---------|-----------|--------|
| T8 | Validar CI completo | Push na branch, verificar 3 jobs passam | GitHub Actions | T5, T6, T7 | Todos os jobs verdes | CI dashboard |
| T9 | Documentar no README/OPERATIONS | Adicionar seção sobre `make check` e CI gates | `docs/OPERATIONS.md` ou `README.md` | T8 | Documentação atualizada | Review manual |

## Resumo de Dependências

```
T1 ──┬── T2 ── T3 ──┐
     │               ├── T5 ──┐
     └── T4 ─────────┘        ├── T8 ── T9
                               │
     T2 ────────────── T6 ────┘
     T4 ────────────── T7 ────┘
```

- Jobs marcados [P] são paralelizáveis
- T3 (corrigir issues de lint) é o de maior risco — pode revelar bugs reais
- T4 (gosec baseline) é o segundo de maior risco — pode revelar vulnerabilidades
