# Project-Scoped CWD Overlay — Tasks

**Design:** `.specs/features/project-scoped-memory/design.md`
**Status:** ✅ Done (v0.37.0 implementation, v0.38.1 validation + doc closure)

---

## Execution Plan

### Ordem de implementação

```text
T0 (resolver) → T1 (callers) → T2 (migração) → T3 (prompt docs) → T4 (cleanup)
                    ↕
             build + test a cada passo
```

T0 → T1 → T2 → T3 → T4

---

## T0: Adicionar `ProjectCwdOverlayDir`

**O quê:** Novo método no PathResolver.

**Onde:**
- `internal/runtime/resolver.go` — adicionar `ProjectCwdOverlayDir(cwd string) string`
- `internal/runtime/resolver_test.go` — testes

**Feito quando:**
```
go build ./...    → OK
go test ./internal/runtime/ -run TestProjectCwdOverlayDir -v  → OK
```

**Verificação:**
- [ ] `ProjectCwdOverlayDir("/a/b")` retorna `~/.aurelia/projects/-a-b/cwd_overlay/`
- [ ] `ProjectCwdOverlayDir("")` retorna `""`
- [ ] Mesmo slug em chats diferentes retorna mesmo path
- [ ] `TopicCwdOverlayDir` docstring diz "Deprecated: use ProjectCwdOverlayDir"

---

## T1: Migrar Callers

### T1a: `internal/dream/memory_writer.go`

**Mudança:** `resolveLayerTarget` case `"cwd_overlay"` (linha 76-91):
- Substituir `w.resolver.TopicCwdOverlayDir(chatID, threadID)` por `w.resolver.ProjectCwdOverlayDir(cwd)`
- `chatID` e `threadID` deixam de ser usados neste case

**Testes:** `go test ./internal/dream/ -run TestMemoryWriter -v`
- Ajustar resolvers mock nos testes existentes
- Adicionar teste: nudge escreve no mesmo `ProjectCwdOverlayDir` para chatIDs diferentes (TUI vs Telegram)

### T1b: `internal/dream/nudge.go`

**Mudança:** `buildNudgePrompt` (linha ~264):
- Substituir `d.resolver.TopicCwdOverlayDir(chatID, threadID)` por `d.resolver.ProjectCwdOverlayDir(cwd)`

**Testes:** `go test ./internal/dream/ -run TestNudge -v`

### T1c: `internal/pipeline/prompt_builder.go`

**Mudança:** `loadMemoryContents` (linha ~462):
- Substituir `bc.resolver.TopicCwdOverlayDir(chatID, threadID)` por `bc.resolver.ProjectCwdOverlayDir(cwd)`

**Testes:**
- `go test ./internal/pipeline/ -run TestLoadMemory -v`
- Ajustar paths esperados nos testes E9 (prompt_builder_test.go)
- Verificar que cwd_overlay ainda NÃO é injetado quando não há /cwd ativo (E9.2)

### T1d: `internal/pipeline/memory_cache.go`

**Mudança:** `InvalidateMemoryDirs` (linha ~170):
- Substituir `bc.resolver.TopicCwdOverlayDir(chatID, threadID)` por `bc.resolver.ProjectCwdOverlayDir(cwd)`
- Nota: o método já recebe `cwd` como parâmetro

**Testes:** `go test ./internal/pipeline/ -run TestMemoryCache -v`

### T1e: `internal/memoryux/service.go`

**Mudança:**
- Método `cwdOverlayDir(chatID, threadID)` → `cwdOverlayDir(cwd)`
- Atualizar `Status()`, `checkpointTarget()`, `WriteCheckpoint()` para passar `cwd` em vez de `(chatID, threadID)` para `cwdOverlayDir`

**Testes:**
- `go test ./internal/memoryux/ -v`
- Atualizar paths esperados em `service_test.go`

### T1f: `internal/runtime/bootstrap.go`

**Mudança:** `BootstrapConversationProjectMemory`:
- Substituir `r.TopicCwdOverlayDir(chatID, threadID)` por `r.ProjectCwdOverlayDir(cwd)`
- Opcional: remover parâmetros `chatID`/`threadID` da assinatura (verificar callers)

**Testes:** `go test ./internal/runtime/ -run TestBootstrap -v`

### T1 Checkpoint

```
go build ./...            → OK
go test ./... -short      → OK
go vet ./...              → OK
```

---

## T2: Script de Migração de Dados

**O quê:** Script que move dados existentes de `topics/*/cwd_overlay/` para `projects/<slug>/cwd_overlay/`.

**Onde:** `cmd/aurelia/` como subcomando ou script shell separado.

**Algoritmo:**

```
1. Listar todos os diretórios topics/*/cwd_overlay/ e topics/*/thread_*/cwd_overlay/
2. Para cada diretório:
   a. Extrair chatID e threadID do path
   b. Buscar binding no SQLite pelo (chatID, threadID)
   c. Se binding encontrado:
      - Destino = resolver.ProjectCwdOverlayDir(binding.CWD)
      - Para cada .md no source:
        - Se destino já tem o arquivo: merge facts únicos
        - Se não: copia arquivo
      - Merge MEMORY.md indexes
   d. Se binding não encontrado:
      - Log de skip com sugestão manual
      - Opcional: re-aproximar slug via basename do path (fallback)
3. Opcional: remover diretórios source após confirmação
```

**Flags:**
- `--dry-run` — só mostra o que faria
- `--yes` — executa sem confirmação
- `--remove-source` — apaga diretórios de origem após migração

**Testes:** testar com diretórios temporários simulando estrutura.

---

## T3: Atualizar Instruções do Prompt

**O quê:** Atualizar a tabela de memória e o texto explicativo injetado no prompt do agente.

**Onde:** `internal/pipeline/prompt_builder.go`

**Mudanças:**
- Linha ~364: atualizar path na tabela
- Linha ~374: atualizar descrição do propósito

**Antes:**
```
| **CWD Overlay** | `~/.aurelia/topics/chat_<id>/thread_<id>/cwd_overlay/` | Work context, session notes, decisions for project "X" in this topic — Aurelia is a personal assistant |
```

**Depois:**
```
| **CWD Overlay** | `~/.aurelia/projects/<slug>/cwd_overlay/` | Project-scoped memory — shared across all conversations with this /cwd. Architecture, patterns, project decisions |
```

---

## T4: Limpeza (Cleanup)

**O quê:** Remover código morto e fazer limpeza fina.

**Tarefas:**
- [ ] Verificar se `TopicCwdOverlayDir` tem callers fora de teste após T1
- [ ] Se nenhum: remover método (com docstring de breaking change)
- [ ] Se algum caller externo (ex: plugin): manter como deprecated
- [ ] Rodar `golangci-lint` para detectar parâmetros não utilizados (`chatID`/`threadID` em funções que só usam `cwd`)
- [ ] Verificar se `project_team` redirect em `memory_writer.go` (case `"project_team"`) ainda faz sentido apontar para `ProjectCwdOverlayDir`

---

## Resumo de Arquivos

| Arquivo | T0 | T1a | T1b | T1c | T1d | T1e | T1f | T2 | T3 | T4 |
|---|---|---|---|---|---|---|---|---|---|---|
| `internal/runtime/resolver.go` | ✅ | | | | | | | | | ✅ |
| `internal/runtime/resolver_test.go` | ✅ | | | | | | | | | |
| `internal/dream/memory_writer.go` | | ✅ | | | | | | | | |
| `internal/dream/memory_writer_test.go` | | ✅ | | | | | | | | |
| `internal/dream/nudge.go` | | | ✅ | | | | | | | |
| `internal/pipeline/prompt_builder.go` | | | | ✅ | | | | | ✅ | |
| `internal/pipeline/prompt_builder_test.go` | | | | ✅ | | | | | ✅ | |
| `internal/pipeline/memory_cache.go` | | | | | ✅ | | | | | |
| `internal/memoryux/service.go` | | | | | | ✅ | | | | |
| `internal/memoryux/service_test.go` | | | | | | ✅ | | | | |
| `internal/runtime/bootstrap.go` | | | | | | | ✅ | | | |
| `cmd/aurelia/` (migrate subcommand) | | | | | | | | ✅ | | |
| `internal/projectbinding/` (consulta) | | | | | | | | ✅ | | |
