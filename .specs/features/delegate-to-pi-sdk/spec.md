# Delegate to PI SDK Native — Eliminate Reimplementations

**Status:** ✅ Concluído em v0.13.7 (2026-05-22)  
**Companion specs:** `.specs/features/security-guard-rails/`, `.specs/features/project-binding/`  
**Related:** `.specs/features/project-memory/`, `.specs/features/wiki-memory/` (superseded), `ROADMAP.md`  
**Prerequisite:** `security-guard-rails` (v0.8.0) — must be stable before removing Go policy engine  

---

## Problem Statement

O Aurélia reimplementa em Go e TypeScript várias funcionalidades que o **PI SDK** (`@earendil-works/pi-coding-agent`) já oferece nativamente. Isso viola o princípio fundamental do projeto:

> *"The goal is not to reimplement what PI already does. The goal is to orchestrate it."*

A análise completa (ver research session) identificou **7 áreas** de duplicação:

1. **Legacy Agent Registry** — Go parser + indexação manual vs. PI `AGENTS.md` discovery nativo. Este ponto foi realinhado por `.specs/features/prompt-profiles/`: arquivos `~/.aurelia/agents/*.md` permanecem como Prompt Profiles Aurelia-managed, não como agentes executores concorrentes.
2. **Security Policy** — Go policy engine (~514 linhas) + Bridge TS hooks (~300 linhas) vs. PI `beforeToolCall` nativo
3. **Session Management** — Go in-memory store + Bridge LRU cache vs. PI `SessionManager` persistente
4. **Token Tracking / Compaction** — Go manual tracker + auto-reset vs. PI `SettingsManager.compaction`
5. **Memory Loading** — Go manual `CLAUDE.md`/`AGENTS.md` loading vs. PI `DefaultResourceLoader`
6. **Model Registry** — Bridge fuzzy resolution vs. PI `ModelRegistry.find()`
7. **System Prompt Assembly** — Go manual assembly de 15 seções vs. PI `systemPromptOverride` + context files

**Custo da duplicação:**
- ~1.816 linhas de código para manter e testar
- Duplicação de lógica de segurança (Go + TS)
- Complexidade desnecessária no prompt builder (861 linhas)
- Risco de divergência entre implementações

**O que NÃO muda:** Persona, memória operacional, Cron, Telegram e Orchestrator continuam diferenciais do Aurélia. Wiki memory transversal é delegada ao PI via `ai-memory` MCP quando disponível.

---

## Goals

### P0: Bridge Cleanup — Model Registry + Security Hooks

- [ ] Eliminar `resolveModelFromRegistry()` do Bridge — usar `ModelRegistry.find()` nativo
- [ ] Simplificar security hooks no Bridge — delegar para PI `beforeToolCall` quando disponível
- [ ] Reduzir `bridge/index.ts` em ~340 linhas
- [ ] Bridge compila clean após mudanças

### P0: Go Security Cleanup

- [ ] Eliminar `internal/security/policy.go` (~514 linhas) — mover policy para Bridge/extension PI
- [ ] Eliminar `internal/security/profiles.go` — usar PI nativo ou manter apenas constantes
- [ ] Manter `internal/security/audit.go` — audit logging ainda é responsabilidade do Aurélia
- [ ] Go build/test/vet clean após remoção

### P1: Session Store Simplification

- [ ] Reduzir `internal/session/store.go` de 265 para ~80 linhas
- [ ] Remover tracking de sessionID em memória — PI `SessionManager` já persiste em disco
- [ ] Manter apenas: mapeamento chat→sessionFile (para bridge recovery) + cwd tracking

### P1: Token Tracker Elimination

- [ ] Remover `internal/session/tracker.go` (131 linhas)
- [ ] Usar `SettingsManager.inMemory({ compaction: { enabled: true } })` no Bridge
- [ ] Manter `/usage` command extraindo stats do Bridge (display only, não decision)

### P1: Prompt Builder Refactor

- [ ] Delegar carregamento de `CLAUDE.md`/`AGENTS.md` ao PI `DefaultResourceLoader`
- [ ] Reduzir `internal/pipeline/prompt_builder.go` de ~861 para ~500 linhas
- [ ] Manter injeção de: Telegram context, cron, security, memory layers, continuity

### P2: Agent Registry Migration

- [ ] Migrar agentes de `~/.aurelia/agents/` para PI nativo `~/.pi/agent/agents/`
- [ ] Eliminar `internal/agents/registry.go` e `internal/agents/types.go`
- [ ] Script de migração automática de dados

---

## Out of Scope

- **Persona system** (`internal/persona/`) — sem equivalente no PI SDK; é diferencial do Aurélia
- **Operational memory layers** (`internal/dream/`, `internal/pipeline/memory_*`, nudge) — usadas para UX/continuidade/prompt context do Aurelia
- **Wiki memory transversal** — fora do Aurelia; usar PI + `ai-memory` MCP, não implementar gateway próprio
- **Cron scheduler** (`internal/cron/`) — PI não tem scheduling
- **Telegram interface** (`internal/telegram/`) — PI é agnóstico de interface
- **Orchestrator** (`internal/orchestrator/`) — específico do fluxo Aurélia
- **Bridge protocol** (`internal/bridge/bridge.go`, `protocol.go`) — necessário para Go↔TS communication

---

## Architecture Decision

**Opção A: Big Bang** — Migrar tudo de uma vez  
**Opção B: Faseado por risco** — Bridge primeiro, depois Go, depois agent registry  
**Opção C: Bridge-only** — Só limpar o Bridge, manter Go como está

**Escolhida: Opção B (Faseado)**

Rationale:
- Bridge (TS) é o ponto de contato direto com o PI SDK — mudanças têm efeito imediato e são fáceis de reverter
- Go tem mais testes e dependências cruzadas — precisa de validação cuidadosa
- Agent registry é a mudança mais disruptiva para usuários (mover arquivos) — deixar para o final
- Cada fase pode ser merged independentemente

---

## Data Models

### Bridge — Simplified Model Resolution

**Antes:**
```typescript
function resolveModelFromRegistry(registry, provider, modelID) {
  // ~40 linhas de fuzzy matching, aliases, fallback
}
```

**Depois:**
```typescript
const model = registry.find(mappedProvider, mappedModel)
  || registry.getAll().find(m => m.id === mappedModel);
// Eliminar aliases custom — usar provider nativo do PI
```

### Bridge — Security via PI `beforeToolCall`

**Antes:**
```typescript
session.on("tool_call", async (event) => {
  const decision = evaluateToolPolicy(event.toolName, event.args, opts.security);
  // ~300 linhas de policy duplicada
});
```

**Depois (implementado via `session.agent.beforeToolCall`):**
```typescript
const { session } = await createAgentSession({ /* ... */ });
const originalBeforeToolCall = session.agent.beforeToolCall;
session.agent.beforeToolCall = async (ctx, signal) => {
  const decision = evaluateToolPolicy(ctx.toolCall.name, ctx.args, securityContext);
  if (decision.decision === "block") return { block: true, reason: decision.reason };
  return originalBeforeToolCall?.(ctx, signal);
};
```

**Nota:** `beforeToolCall` não é opção de `createAgentSession`; ele é propriedade do `session.agent`. O fallback antigo `session.on("tool_call")` foi removido porque essa API não existe na versão atual do PI SDK.

### Go — Simplified Session Store

**Antes:**
```go
type Store struct {
    sessions map[SessionKey]*entry  // sessionID + active + lastSeen
    cwds     map[ConversationKey]string
    cwdSeen  map[ConversationKey]time.Time
}
```

**Depois:**
```go
type Store struct {
    sessionFiles map[SessionKey]string  // apenas sessionFile path para resume
    cwds         map[ConversationKey]string
    cwdSeen      map[ConversationKey]time.Time
}
```

### Go — Eliminated Token Tracker

**Removido:** `internal/session/tracker.go`

**Bridge passa `compaction: enabled`:**
```typescript
const settingsManager = SettingsManager.inMemory({
  compaction: { enabled: true },  // auto-prune context
});
```

---

## Implementation Map

### Phase 0: Discovery & Validation (1 dia)

| Step | Action | Validation |
|------|--------|------------|
| 0.1 | Verificar PI SDK version | `npm list @earendil-works/pi-coding-agent` |
| 0.2 | Confirmar `ModelRegistry.find()` | Teste unitário Bridge |
| 0.3 | Confirmar `beforeToolCall` disponível | Teste com sessão dummy |
| 0.4 | Confirmar `SettingsManager.compaction` | Docs + teste |
| 0.5 | Documentar breaking changes | `docs/pi-sdk-api-validation.md` |

### Phase 1: Bridge Cleanup (2 dias)

| File | Action | Lines |
|------|--------|-------|
| `bridge/index.ts` | Eliminar `resolveModelFromRegistry()` | -40 |
| `bridge/index.ts` | Simplificar security hooks (usar PI nativo ou manter on-hook) | -260 |

### Phase 2: Go Security Cleanup (2 dias)

| File | Action | Lines |
|------|--------|-------|
| `internal/security/policy.go` | Delete | -514 |
| `internal/security/profiles.go` | Delete (ou reduzir a constantes) | -120 |
| `internal/security/security_test.go` | Delete | -200 |
| `internal/security/audit.go` | Keep | — |

### Phase 3: Session Store (2 dias)

| File | Action | Lines |
|------|--------|-------|
| `internal/session/store.go` | Simplify — remove sessionID tracking | -185 |
| `internal/session/tracker.go` | Delete | -131 |
| `internal/session/tracker_test.go` | Delete | -? |

### Phase 4: Prompt Builder (3 dias)

| File | Action | Lines |
|------|--------|-------|
| `internal/pipeline/prompt_builder.go` | Delegate CLAUDE.md/AGENTS.md to PI; simplify assembly | -260 |

### Phase 5: Legacy Agent Registry / Prompt Profiles (superseded)

**Objetivo original:** Eliminar `internal/agents/registry.go` e delegar discovery ao PI SDK.

**Atualização conceitual (Junho 2026):** esta fase foi supersedida pela spec de Prompt Profiles. O diretório `~/.aurelia/agents/` deve ser tratado como storage legado de Prompt Profiles Aurelia-managed. A execução continua no SDK; a Aurelia injeta personalidade/contexto. Delegar parsing nativo ao PI via `agentsFilesOverride` pode ser revisitado por adapter, mas não deve mudar a semântica de produto.

#### Opção A: Migração para diretório PI-native (mais disruptiva)

Mover agentes de `~/.aurelia/agents/*.md` para `~/.pi/agent/agents/*.md`.

Script de migração: `scripts/migrate-agents.sh`

**Risco:** User-facing. Requer migração de dados.  
**Prioridade:** P3 futuro.

#### Opção B: `agentsFilesOverride` (recomendada, menos disruptiva)

Manter agentes em `~/.aurelia/agents/` e usar PI native discovery:

```typescript
const resourceLoader = new DefaultResourceLoader({
  agentsFilesOverride: () => ({
    agentsFiles: globSync(join(agentDir, "agents", "*.md")),
  }),
});
```

**Vantagens:** Zero migração, path familiar, PI faz parsing nativo.  
**Desvantagens:** Agentes não aparecem em `pi /agents` quando rodado diretamente.  
**Recomendação:** Implementar Opção B no MVP.

| File | Action | Lines |
|------|--------|-------|
| `internal/agents/registry.go` | Delete | -211 |
| `internal/agents/types.go` | Delete | -58 |
| `internal/agents/registry_test.go` | Delete | -? |
| `scripts/migrate-agents.sh` | Create (Opção A) | +30 |

### Phase 6: Validation (2 dias)

| Gate | Command |
|------|---------|
| Build | `go build ./...` |
| Tests | `go test ./... -short` |
| Vet | `go vet ./...` |
| Bridge | `make bridge` |
| E2E | `go test ./e2e/...` |

---

## Rollout

### Phase 1: Bridge-only (low risk)
- Model registry simplification
- Security hooks simplification (if `beforeToolCall` available)
- Rebuild bundle, validate no regressions

### Phase 2: Go cleanup (medium risk)
- Remove security policy engine
- Simplify session store
- Validate all tests pass

### Phase 3: Prompt builder (medium risk)
- Delegate context file loading to PI
- Simplify system prompt assembly
- Validate agent still sees CLAUDE.md/AGENTS.md

### Phase 4: Agent registry (high risk — user-facing)
- Migration script
- Update docs
- Notify users

---

## Edge Cases

- **PI SDK `beforeToolCall` não exposto em `createAgentSession`:** Manter `session.on("tool_call")` por enquanto; revisit em próxima versão do PI SDK
- **ModelRegistry.find() não encontra modelo:** Adicionar fallback `registry.getAll().filter()`; se persistir, manter aliases mínimos
- **Agentes com `schedule` no frontmatter:** Verificar se PI SDK descobre `schedule`; se não, manter parsing custom mínimo
- **Perda de `/usage` command:** Extrair stats do Bridge (`cost_usd`, `input_tokens`, `output_tokens`) e manter display
- **CLAUDE.md não carregado quando cwd muda:** Forçar reload do `DefaultResourceLoader` no Bridge
- **Usuários com agentes customizados em `~/.aurelia/agents/`:** Script de migração automática

---

## Success Criteria

- [ ] `go build ./...` clean
- [ ] `go test ./... -short` 100% pass
- [ ] `go vet ./...` zero warnings
- [ ] Bridge compila (`make bridge`)
- [ ] E2E smoke test pass
- [ ] Telegram message → response funciona
- [ ] Agent routing (`@agentname`) funciona
- [ ] Security hooks ainda bloqueiam `rm -rf /`
- [ ] Session resume após crash funciona
- [ ] `/usage` ainda mostra tokens/custo
- [ ] CLAUDE.md/AGENTS.md ainda aparecem no contexto do agente
- [ ] ~1.816 linhas removidas (estimativa)

---

## Validation Commands

```bash
go build ./...
go vet ./...
go test ./... -short
go test ./... -v
```

Bridge rebuild required if `bridge/index.ts` changes:

```bash
cd bridge && npm run build
cp bundle.js ../internal/bridge/bundle.js
```

---

## Estimativa

| Phase | Duração | LOC removidas |
|-------|---------|---------------|
| 0. Discovery | 1 dia | — |
| 1. Bridge | 2 dias | ~340 |
| 2. Security | 2 dias | ~600 |
| 3. Session | 2 dias | ~185 |
| 4. Prompt | 3 dias | ~260 |
| 5. Agents | 3 dias | ~300 |
| 6. Validation | 2 dias | — |
| **Total** | **16 dias** | **~1.816** |
