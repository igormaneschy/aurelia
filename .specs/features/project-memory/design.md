# Context-Scoped Memory — Design Document

**Date:** 2026-05-30  
**Status:** Design (antes de implementação)  
**Spec:** `.specs/features/project-memory/spec.md`  
**Tasks:** `.specs/features/project-memory/tasks.md`  
**Depende de:** Multi-User Profiles (✅), Project Binding (✅)

---

## 1. Summary

A reformulação do modelo de memória de `(user_id, project_slug)` para `(user_id, context_key)` elimina `scanForProject`, transforma `/cwd` em overlay declarativo opt-in, e organiza a memória em 5 camadas com ordem fixa de prompt assembly. O eixo primário passa a ser o topic/thread — não mais o projeto.

---

## 2. Current State Audit

### 2.1 O que já existe (mapeado do código)

| Componente | Arquivo | O que faz |
|---|---|---|
| `runtime.PathResolver` | `internal/runtime/resolver.go` | Já tem `ProjectTeamMemoryDir(cwd)`, `ProjectMemoryDir(cwd)`, `ConversationProjectMemoryDir(cwd, chatID, threadID)`. **Falta:** `UserMemoryDir`, `TopicMemoryDir`, `TopicCwdOverlayDir`. |
| `users.Resolver` | `internal/users/resolver.go` | Já tem `MemoryDir(userID)`, `ProjectMemoryDir(userID, slug)`, `TopicsDir()`. **Falta:** `TopicMemoryDir(chatID, threadID)`, `TopicCwdOverlayDir(chatID, threadID)`. |
| `scanForProject` | `internal/pipeline/project_detect.go:73` | **Ainda existe.** Caminha filesystem procurando diretório de projeto. Chamado em `project_detect.go:214`. |
| Prompt assembly | `internal/pipeline/prompt_builder.go` | Já carrega camadas: persona, user memory, project private, topic, global, team. Mas paths são calculados ad-hoc (ex: `topicMemoryDirCanonical` faz `filepath.Join` direto). **Ordem atual não segue** a ordem canónica definida na spec. |
| Nudge | `internal/dream/nudge.go` | Usa `d.userResolver.MemoryDir(userID)`, topic via `filepath.Join`. Templates `nudge_global.tmpl` e `nudge_project.tmpl`. |
| Memory writer | `internal/dream/memory_writer.go` | Interface `memoryDirResolver` com `TopicMemoryDir`, `ProjectMemoryDir`, `TeamMemoryDir`. Usa layers `global`, `topic`, `project`, `team`. **Não tem `cwd_overlay`.** |
| Project binding | `internal/projectbinding/` | Já implementado com SQLite, `ConversationKey`, `/cwd`, `/cwd clear`. ✅ |

### 2.2 Problemas identificados

1. **`scanForProject` ainda existe** — a spec manda remover completamente.
2. **Paths de memória calculados em 3 lugares diferentes** — `prompt_builder.go`, `dream/nudge.go`, `memory_writer.go`. Nenhum usa um resolver canónico único.
3. **Camada `users/<id>/projects/<slug>/memory/` ainda referenciada** — `users.Resolver.ProjectMemoryDir` e `prompt_builder.go` user×project layer.
4. **`memory_writer.go` não conhece `cwd_overlay`** — só tem `global`, `topic`, `project`, `team`.
5. **Prompt assembly não segue ordem canónica** — a ordem definida na spec (persona → user global → topic → cwd_overlay → project team) não é respeitada.
6. **`wiki-memory/spec.md` ainda referencia `users/<id>/projects/<slug>/memory/`** — precisa ser patchado.

---

## 3. Target Architecture

### 3.1 Single Source of Truth: `PathResolver` canónico

Todo cálculo de path de memória deve passar por UM ponto: `internal/runtime/PathResolver`. Nenhum outro package faz `filepath.Join` para paths de memória.

```go
// internal/runtime/resolver.go — novos métodos (a adicionar)
func (r *PathResolver) UserMemoryDir(userID int64) string
func (r *PathResolver) TopicMemoryDir(chatID int64, threadID int) string
func (r *PathResolver) TopicCwdOverlayDir(chatID int64, threadID int) string
// Já existentes:
func (r *PathResolver) ProjectTeamMemoryDir(cwd string) string
func (r *PathResolver) ProjectMemoryDir(cwd string) string
func (r *PathResolver) ConversationProjectMemoryDir(cwd string, chatID int64, threadID int) string
```

### 3.2 Layout canónico

```text
~/.aurelia/
├── memory/personas/                    # IDENTITY.md + SOUL.md (global)
├── users/<user_id>/
│   ├── personas/USER.md
│   └── memory/                         # user_global (cross-context)
├── projects/<slug>/team/               # project team memory (compartilhado)
└── topics/chat_<id>/thread_<id>/
    ├── MEMORY.md                       # topic memory
    └── cwd_overlay/                    # só quando /cwd declarado
        └── MEMORY.md
```

**Removido:** `users/<id>/projects/<slug>/memory/` (o antigo user×project privado).

### 3.3 Prompt assembly — ordem fixa

```text
1. Aurelia persona (IDENTITY + SOUL)     — sempre
2. User global (USER.md + memory)        — sempre
3. Topic memory                          — sempre
4. CWD overlay                           — só quando /cwd ativo no tópico
5. Project team                          — só quando /cwd ativo
```

### 3.4 Camadas para o extractor/nudge

| Nome da camada | Path | Quando escrever |
|---|---|---|
| `user_global` | `PathResolver.UserMemoryDir(userID)` | Factos pessoais, preferências |
| `topic` | `PathResolver.TopicMemoryDir(chatID, threadID)` | Decisões do tópico |
| `cwd_overlay` | `PathResolver.TopicCwdOverlayDir(chatID, threadID)` | Contexto de trabalho atual (só se /cwd ativo) |
| `project_team` | `PathResolver.ProjectTeamMemoryDir(cwd)` | Stack, convenções, padrões (só se /cwd ativo) |

**Nota:** As camadas internas do nudge/dream mudam de `"project"` → `"cwd_overlay"`. A camada `"team"` permanece igual semanticamente.

---

## 4. Component Design

### 4.1 `PathResolver` — novos métodos

```go
// UserMemoryDir returns the per-user global memory directory.
// ~/.aurelia/users/<userID>/memory/
func (r *PathResolver) UserMemoryDir(userID int64) string {
    return filepath.Join(r.root, "users", fmt.Sprintf("%d", userID), "memory")
}

// TopicMemoryDir returns the topic-scoped memory directory.
// ~/.aurelia/topics/chat_<chatID>/thread_<threadID>/
func (r *PathResolver) TopicMemoryDir(chatID int64, threadID int) string {
    if threadID <= 0 {
        return "" // topic memory só existe em threads
    }
    return filepath.Join(r.root, "topics", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID))
}

// TopicCwdOverlayDir returns the cwd overlay memory directory for a topic.
// ~/.aurelia/topics/chat_<chatID>/thread_<threadID>/cwd_overlay/
// Only valid when /cwd is declared for the topic.
func (r *PathResolver) TopicCwdOverlayDir(chatID int64, threadID int) string {
    if threadID <= 0 {
        return ""
    }
    return filepath.Join(r.root, "topics", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID), "cwd_overlay")
}
```

**Validation:**
- `userID == 0` → erro
- `chatID == 0` || `threadID <= 0` → erro para `TopicMemoryDir`/`TopicCwdOverlayDir`
- `cwd` vazio → erro em `ProjectTeamMemoryDir`
- `ProjectSlug` continua determinístico, filesystem-safe, estável

### 4.2 `users.Resolver` — alinhamento

O `users.Resolver` delega para `runtime.PathResolver` onde possível:

```go
// Mantém os métodos existentes que são user-scoped:
func (r *Resolver) MemoryDir(userID int64) string     // → ~/.aurelia/users/<id>/memory/
func (r *Resolver) UserMdPath(userID int64) string     // → ~/.aurelia/users/<id>/personas/USER.md
func (r *Resolver) SkillsDir(userID int64) string      // → ~/.aurelia/users/<id>/skills/

// Deprecated/removido:
func (r *Resolver) ProjectMemoryDir(userID int64, slug string) string  // REMOVER — não existe no novo layout
```

**Decisão:** `users.Resolver` **não** duplica `TopicMemoryDir`/`TopicCwdOverlayDir` — estes pertencem ao `runtime.PathResolver`. O `users.Resolver` foca apenas no que é user-scoped.

### 4.3 Remoção de `scanForProject`

**Localização atual:** `internal/pipeline/project_detect.go`

**Callers:**
- `project_detect.go:214` — chamado quando o pipeline tenta auto-detectar projeto

**Ação:** Remover a função `scanForProject` e seu único caller. Auto-detecção de projeto sem confirmação explícita já é rejeitada pela spec de project-binding (P1: "Auto-detect não persiste sem confirmação").

### 4.4 Prompt builder — refactor para ordem canónica

**Estado atual (`prompt_builder.go`):**

A ordem atual de `buildSystemPrompt`:
1. Identity
2. Persona (com user-specific)
3. Agent instructions
4. Cron
5. Telegram instructions
6. Security
7. Continuity
8. Last run state
9. Memory instructions (com conteúdo)

Dentro de `loadMemoryContents`, a ordem atual é:
1. Project private (`ConversationProjectMemoryDir`)
2. User × project private (`users.Resolver.ProjectMemoryDir`) — **REMOVIDO**
3. User-specific (`users.Resolver.MemoryDir`)
4. Topic (`topicMemoryDirCanonical`)
5. Global (`bc.memoryDir`)
6. Project team (`ProjectTeamMemoryDir`)

**Nova ordem dentro de `loadMemoryContents`:**
1. User global → `PathResolver.UserMemoryDir(userID)`
2. Topic memory → `PathResolver.TopicMemoryDir(chatID, threadID)`
3. CWD overlay → `PathResolver.TopicCwdOverlayDir(chatID, threadID)` (só se /cwd ativo)
4. Project team → `PathResolver.ProjectTeamMemoryDir(cwd)` (só se /cwd ativo)

**Removido:** `ConversationProjectMemoryDir` e `users.Resolver.ProjectMemoryDir`.

**Mantido:** Global (`~/.aurelia/memory/`) como fallback de compatibilidade para deploys single-user não migrados.

### 4.5 Nudge — atualização de camadas

**Template data atual:**
```go
type nudgeTemplateData struct {
    GlobalDir  string
    TopicDir   string
    ProjectDir string  // ← vira CwdOverlayDir
    TeamDir    string
}
```

**Template data novo:**
```go
type nudgeTemplateData struct {
    GlobalDir     string  // PathResolver.UserMemoryDir(userID)
    TopicDir      string  // PathResolver.TopicMemoryDir(chatID, threadID)
    CwdOverlayDir string  // PathResolver.TopicCwdOverlayDir(chatID, threadID) — só se /cwd ativo
    TeamDir       string  // PathResolver.ProjectTeamMemoryDir(cwd) — só se /cwd ativo
}
```

**Nudge prompt templates** (`prompts/nudge_global.tmpl`, `prompts/nudge_project.tmpl`) precisam ser atualizados:
- `nudge_global.tmpl`: mantém `GlobalDir` + `TopicDir`
- `nudge_project.tmpl`: `GlobalDir` + `TopicDir` + `CwdOverlayDir` + `TeamDir`

**Camada "project" → "cwd_overlay":** No `memory_writer.go`, a layer `"project"` vira `"cwd_overlay"`.

### 4.6 Memory writer — nova camada `cwd_overlay`

```go
// memory_writer.go — resolveLayerTarget
case "cwd_overlay":
    if cwd == "" { ... erro ... }
    dir := w.resolver.TopicCwdOverlayDir(chatID, threadID)
    // containment root = instance root
```

`allowedLayers` muda de:
```go
{"global": true, "topic": true, "project": true, "team": true}
```
para:
```go
{"global": true, "topic": true, "cwd_overlay": true, "team": true}
```

### 4.7 `memoryDirResolver` interface — atualização

```go
type memoryDirResolver interface {
    Root() string
    TopicMemoryDir(chatID int64, threadID int) string
    TopicCwdOverlayDir(chatID int64, threadID int) string  // novo
    TeamMemoryDir(cwd string) string
    // REMOVIDO: ProjectMemoryDir(cwd, chatID, threadID)
}
```

---

## 5. Data Flow

### 5.1 Turno sem `/cwd`

```text
Telegram message
  → pipeline.buildSystemPrompt
    → resolve TurnContext (userID, chatID, threadID)
    → check project binding → nil (sem /cwd)
    → loadMemoryContents:
      1. PathResolver.UserMemoryDir(userID)     → user global
      2. PathResolver.TopicMemoryDir(chatID, threadID) → topic
      3. (skip cwd_overlay — sem binding)
      4. (skip project team — sem binding)
    → prompt assembly: persona + user + topic
```

### 5.2 Turno com `/cwd`

```text
Telegram message
  → pipeline.buildSystemPrompt
    → resolve TurnContext (userID, chatID, threadID)
    → check project binding → binding{CWD: "/repo/aurelia"}
    → loadMemoryContents:
      1. PathResolver.UserMemoryDir(userID)     → user global
      2. PathResolver.TopicMemoryDir(chatID, threadID) → topic
      3. PathResolver.TopicCwdOverlayDir(chatID, threadID) → cwd overlay
      4. PathResolver.ProjectTeamMemoryDir("/repo/aurelia") → team
    → prompt assembly: persona + user + topic + cwd_overlay + team
```

### 5.3 Nudge com `/cwd`

```text
Nudge trigger (após N turnos)
  → buildNudgePrompt(cwd, chatID, threadID, userID)
    → GlobalDir  = PathResolver.UserMemoryDir(userID)
    → TopicDir   = PathResolver.TopicMemoryDir(chatID, threadID)
    → CwdOverlayDir = PathResolver.TopicCwdOverlayDir(chatID, threadID)  // se binding existe
    → TeamDir    = PathResolver.ProjectTeamMemoryDir(cwd)                // se binding existe
  → LLM extract → updates com layers: user_global, topic, cwd_overlay, team
  → safeMemoryWriter.applyUpdates → cada update na camada correta
```

---

## 6. Migration Plan

### 6.1 O que migrar

| Origem | Destino |
|---|---|
| `~/.aurelia/memory/<ficheiros>` (excl. personas/) | `~/.aurelia/users/<target_id>/memory/` |
| `~/.aurelia/memory/personas/USER.md` | `~/.aurelia/users/<target_id>/personas/USER.md` |
| `~/.aurelia/projects/<slug>/memory/` (privado) | `~/.aurelia/users/<target_id>/memory/` (fallback — perde-se o escopo de projeto) |
| `~/.aurelia/projects/<slug>/team/` | **permanece onde está** |

**Não existe destino** `users/<id>/projects/` no novo modelo.

### 6.2 Estratégia de migração

```
Fase 1: copy → verify (byte-by-byte)
Fase 2: delete originals
Fase 3: write marker ~/.aurelia/.context-memory-migrated
```

**Marker:** distinto do `~/.aurelia/.multi-user-migrated` (que é da migração multi-user). O marker de context-memory inclui `target_id` e `timestamp`.

**Idempotência:** se o marker existe, aborta com mensagem clara.

### 6.3 Edge cases

- **Project memory sem cwd conhecido:** factos vão para `user_global` como fallback. Não tentamos adivinhar o tópico.
- **Conflito de ficheiros:** se o mesmo nome existe na origem e destino, migração para e pede resolução manual.
- **Deploy sem multi-user migration:** se `~/.aurelia/.multi-user-migrated` não existe, a migração de context-memory **depende** de multi-user migration ter sido executada primeiro.

---

## 7. Implementation Order

```text
E1 (patch specs)
  └── E2 (PathResolver canónico — novos métodos)
        └── E3 (remover scanForProject)
              ├── E4 (prompt assembly — refactor ordem canónica)
              │     └── E5 (wiring /cwd overlay — consumir ProjectBinding)
              │           ├── E6 (classificação de writes — extractor)
              │           │     └── E7 (dream escopado — memory writer + templates)
              │           └── E8 (migração — comando CLI)
              └── E9 (testes de isolamento e regressão)
                    └── E10 (observabilidade)
```

---

## 8. Risk Analysis

| Risco | Probabilidade | Impacto | Mitigação |
|---|---|---|---|
| Regressão em deploy single-user não migrado | Média | Alto | Manter fallback `~/.aurelia/memory/` como camada de compatibilidade; não remover leitura antes da migração |
| `memory_writer.go` quebrar com nova layer `cwd_overlay` | Baixa | Médio | A interface `memoryDirResolver` já abstrai; novos métodos são adicionais, não substitutos |
| Nudge templates exigirem re-teste extenso | Média | Médio | Templates novos são aditivos; template antigo `nudge_global.tmpl` continua igual |
| `scanForProject` ter callers ocultos | Baixa | Baixo | `grep` já confirma apenas 1 caller; se houver callers em testes, são tratados em E3 |
| Path traversal nos novos métodos do PathResolver | Baixa | Alto | Validação de userID=0, chatID=0, threadID≤0; testes unitários em E2.7 |
| wiki-memory spec desalinhada após patch | Média | Baixo | E1 trata isso antes de qualquer código |

---

## 9. Files Changed (estimated)

| Arquivo | Tipo de mudança | Sprint |
|---|---|---|
| `.specs/features/wiki-memory/spec.md` | Patch (remover `user_project`, atualizar paths) | E1 |
| `.specs/features/multi-user-profiles/spec.md` | Patch (marcar P1 como movida) | E1 |
| `internal/runtime/resolver.go` | Adicionar `UserMemoryDir`, `TopicMemoryDir`, `TopicCwdOverlayDir` | E2 |
| `internal/runtime/resolver_test.go` | Testes para novos métodos | E2 |
| `internal/pipeline/project_detect.go` | Remover `scanForProject` e caller | E3 |
| `internal/pipeline/prompt_builder.go` | Refactor `loadMemoryContents` e `buildMemoryInstructions` | E4 |
| `internal/pipeline/prompt_builder_test.go` | Testes de prompt com/sem /cwd | E4 |
| `internal/pipeline/pipeline.go` | Wiring de ProjectBinding → PathResolver | E5 |
| `internal/dream/nudge.go` | Atualizar `buildNudgePrompt`, template data | E6 |
| `internal/dream/prompts/nudge_global.tmpl` | Atualizar layers | E6 |
| `internal/dream/prompts/nudge_project.tmpl` | Atualizar layers (project→cwd_overlay) | E6 |
| `internal/dream/memory_writer.go` | Adicionar layer `cwd_overlay`, atualizar interface | E7 |
| `internal/dream/dream.go` | Atualizar `memoryDirResolver` interface | E7 |
| `internal/users/resolver.go` | Remover `ProjectMemoryDir(userID, slug)` | E2 |
| `cmd/aurelia/` | Comando `migrate-context-memory` | E8 |
| `internal/pipeline/*_test.go` | Testes de isolamento E9.1-E9.8 | E9 |
| `internal/memoryux/` | `ContextBudgetReport` | E10 |

---

## 10. Backward Compatibility

| Cenário | Comportamento |
|---|---|
| Deploy single-user sem migração | `~/.aurelia/memory/` continua a ser lido como fallback de `user_global` até a migração |
| Deploy multi-user migrado, sem context-memory migration | `users/<id>/memory/` já existe; novos paths `topics/...` são criados on-demand |
| Tópico sem `/cwd` | Comportamento idêntico ao atual (user global + topic) |
| Tópico com `/cwd` | Ganha 2 camadas adicionais: cwd_overlay + team |
| Nudge em tópico sem `/cwd` | Escreve em `user_global` + `topic` apenas (sem cwd_overlay) |
| `users.Resolver.ProjectMemoryDir` removido | Callers existentes em `prompt_builder.go` são migrados para `TopicCwdOverlayDir` |

---

## 11. Open Questions

1. **O que fazer com `ConversationProjectMemoryDir`?** — Atualmente usado para isolar memória de projeto por conversa. No novo modelo, `cwd_overlay` já é escopado por `(chat_id, thread_id)`, então `ConversationProjectMemoryDir` é redundante. **Decisão:** remover e substituir por `TopicCwdOverlayDir`.

2. **Manter `~/.aurelia/memory/` como fallback?** — Sim, para compatibilidade com deploys single-user não migrados. Remover apenas após migração confirmada (E8).

3. **`ProjectSlug` continua a ser usado para team memory?** — Sim. O slug determinístico baseado em `cwd` continua válido para `~/.aurelia/projects/<slug>/team/`. O UUID schema é futuro (Sprint F ou posterior).
