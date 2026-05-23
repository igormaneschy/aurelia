# CI Hardening — Lint & Security Gates

**Depends on:** none (standalone quality gate)  
**Desbloqueia:** confidence in every push/PR, catches regression before merge

## Problem Statement

O CI atual (`.github/workflows/ci.yml`) só roda `go test` + `go build`. Zero verificações de lint, análise estática ou segurança. O relatório do Conrado na branch `feature/lint-cleanup-v0.14.1` revelou 7 issues que passaram batido no CI:

| Categoria | Issues | Exemplos |
|-----------|--------|----------|
| `errcheck` | 3 | `rows.Close()` sem checar erro |
| `staticcheck` | 24 | `WriteString(fmt.Sprintf(...))` → `fmt.Fprintf(...)` |
| `unused` | 4 | Funções/variáveis mortas |
| CI config | 1 | `git config` faltando para testes |

Se essas issues tivessem sido pegas no momento do push/PR, não teriam acumulado. Sem gates de segurança, vulnerabilidades como path traversal, secrets em logs, ou goroutine sem recover também passam sem alarme.

## Goals

- [ ] **Lint gate**: `golangci-lint` rodando no CI com linters que cubram errcheck, staticcheck, unused, govet, ineffassign, misspell, gocritic
- [ ] **Security gate**: `gosec` rodando no CI para SQL injection, path traversal, file perms, hardcoded secrets
- [ ] **Vulnerability gate**: `govulncheck` escaneando dependências por CVEs conhecidas
- [ ] **Makefile**: targets `make lint`, `make sec`, `make check` (lint+sec+test+vet)
- [ ] **CI fails on any warning**: zero tolerância — lint warning ou security finding = build vermelho
- [ ] **Local parity**: mesmas checagens rodam localmente antes do push

## Non-Goals

- Não adicionar linters experimentais ou de estilo (wsl, nlreturn, godox, etc.)
- Não modificar código da aplicação — só tooling e CI
- Não alterar o comportamento de build/deploy
- Não adicionar SAST externo (SonarQube, Snyk, etc.)
- Não adicionar pre-commit hooks (pode ser feito depois, fora do escopo)

## User Stories

### P0: CI bloqueia código com erro ignorado

WHEN um developer faz push de código que ignora erro de `Close()` ou `Write()`  
THEN o job `lint` do CI deve falhar com output claro do `errcheck`  
AND o job `test-build` não deve rodar até o lint passar

### P0: CI bloqueia código com vulnerabilidade de segurança

WHEN um developer faz push de código que usa `filepath.Join` com input não sanitizado em path de arquivo  
THEN o job `security` do CI deve falhar com output do `gosec` (G304)  
AND o PR não deve ser mergeável

### P0: CI detecta dependência com CVE

WHEN uma dependência no `go.sum` tem uma vulnerabilidade conhecida  
THEN o job `security` deve falhar com output do `govulncheck`  
AND o output deve incluir o CVE ID e recomendação de upgrade

### P1: Developer roda as mesmas checagens localmente

WHEN um developer roda `make check` localmente  
THEN as mesmas checagens do CI devem rodar (lint, sec, test, vet)  
AND o developer vê os mesmos resultados que o CI veria

### P1: CI workflow é rápido e paralelo

WHEN um push ou PR dispara o CI  
THEN os jobs `lint`, `security`, e `test-build` devem rodar em paralelo  
AND o tempo total de CI não deve exceder ~3 minutos

### P2: Configuração de lint é versionada e reproduzível

WHEN um developer clona o repositório e instala `golangci-lint`  
THEN `golangci-lint run` deve usar o `.golangci.yml` do repositório  
AND o resultado deve ser idêntico ao CI

## Edge Cases

- **Falso positivo em security**: `gosec` pode reportar G304 em uso legítimo de `filepath.Join`. Nesses casos, adicionar comentário `//nolint:gosec` com justificativa — não desabilitar a regra globalmente.
- **CVE sem fix disponível**: `govulncheck` pode reportar vulnerabilidade sem versão corrigida. Nesse caso, CI deve gerar warning (não failure) — implementar via `continue-on-error: true` com step de triagem.
- **Linter novo quebrando CI existente**: ao adicionar um linter novo, primeiro rodar local, corrigir issues, depois adicionar ao CI. Nunca adicionar linter novo direto no CI sem baseline limpa.
- **CI vs ambiente local**: versões de Go e ferramentas devem ser pinadas. CI usa `golangci-lint` latest via `golangci/golangci-lint-action`. Local usa `brew install golangci-lint` ou `go install`.

## Success Criteria

- [ ] `make lint` passa com zero warnings no código atual
- [ ] `make sec` passa com zero findings (ou findings justificados com `//nolint`)
- [ ] `make check` roda lint + sec + test + vet e passa
- [ ] CI workflow atualizado com jobs `lint`, `security`, `test-build` em paralelo
- [ ] Push com código que viola lint/security é bloqueado pelo CI
- [ ] `.golangci.yml` expandido com os linters definidos
- [ ] Nenhuma mudança de código de aplicação (exceto comentários `//nolint` se necessário)
