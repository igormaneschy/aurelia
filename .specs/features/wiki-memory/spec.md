# Wiki Memory Gateway — Specification

**Roadmap step:** 6  
**Status:** 🔴 Spec arquitetural apenas  
**Depende de:** `.specs/features/multi-user-profiles/`, `.specs/features/project-binding/` (✅ done), `.specs/features/security-guard-rails/` (✅ done), `.specs/features/project-memory/`  
**Complementa:** `.specs/features/learning-nudge/`, `.specs/features/plan-mode-architecture/`, `.specs/features/auto-skills/`

## Problem Statement

Aurelia já possui memória persistente em markdown, nudge, dream e camadas por projeto/conversa. Porém, essa memória ainda é principalmente consumida pelo fluxo Telegram → Aurelia → PI SDK. Quando o mesmo usuário trabalha diretamente pelo PI/PI Code/opencode ou outro agente compatível com MCP, o contexto aprendido pelo Aurelia não é automaticamente consultado nem atualizado.

A direção evolutiva é promover a memória do Aurelia para uma **Wiki / LLM Wiki local-first**: uma camada canônica, textual, auditável e escopada, exposta por um gateway MCP para que múltiplos pontos de entrada possam consultar e atualizar a mesma memória.

A tese central: **a Wiki deve ser transversal no acesso, mas nunca global no escopo**.

```text
Aurelia Telegram ─┐
PI / PI Code   ───┼── Wiki MCP Gateway ─── Markdown + SQLite metadata/search
opencode       ───┘
```

## Goals

- [ ] Definir Wiki como fonte canônica de memória textual do Aurelia
- [ ] Expor operações MCP para query/save/ingest/lint/status sem acoplar ao Telegram
- [ ] Preservar escopos fortes: user, user×project private, project team, topic e procedural skills
- [ ] Permitir uso transversal por Aurelia, PI direto, PI Code/opencode e futuros agentes MCP
- [ ] Manter markdown como formato auditável/human-readable
- [ ] Adicionar metadata/search incremental sem substituir markdown como fonte humana
- [ ] Aplicar guard-rails de escopo e redaction antes de qualquer escrita
- [ ] Registrar receipts/audit para toda escrita externa via MCP
- [ ] Evitar vendor lock: memória deve continuar portável entre ferramentas

## Out of Scope

- Substituir o PI SDK ou o pipeline do Aurelia
- Sincronização cloud ou multi-device no MVP
- Vector DB externo obrigatório
- Escrita livre sem `user_id`/`scope`/`project_slug` validáveis
- Compartilhar memória privada entre usuários
- Criar Auto-Skills automaticamente sem confirmação
- UI web completa para edição de Wiki

---

## Architecture Decision

### Escolha: Aurelia-native Wiki, MCP-compatible

O Aurelia continua dono de identidade, escopo, segurança e UX. A Wiki é uma camada nativa do Aurelia, mas exposta via MCP para interoperar com outros pontos de entrada.

**Racional:**

- O artigo sobre LLM Wiki/agentmemory reforça que memória de agente é texto gerenciado, não estado proprietário.
- O Aurelia já possui base markdown, nudge/dream e camadas de memória; a evolução natural é estruturar isso como Wiki.
- Um MCP externo genérico só deve ser adotado se respeitar os escopos do Aurelia; caso contrário, vira fonte de vazamento.
- SQLite FTS/BM25 pode ser introduzido antes de embeddings externos, mantendo custo e dependência baixos.

### Non-negotiable principle

```text
Transversal access, scoped memory.
```

O MCP pode ser chamado por vários clientes, mas cada operação deve resolver e validar seu escopo antes de ler ou escrever.

---

## Wiki Layer Model

```text
~/.aurelia/
├── memory/
│   ├── personas/                     # deployment global: IDENTITY/SOUL
│   └── policy/                       # deployment policies, future
├── users/<user_id>/
│   ├── memory/                       # user global
│   ├── projects/<project_slug>/memory/ # user × project private
│   └── skills/<slug>/SKILL.md        # procedural memory, user private
├── projects/
│   └── <project_slug>/team/          # project team Wiki
└── topics/
    └── chat_<chat_id>/thread_<thread_id>/ # topic Wiki
```

The canonical paths match the project-memory spec:

| Scope | Canonical Path |
|---|---|
| Project team | `~/.aurelia/projects/<slug>/team/` |
| Topic memory | `~/.aurelia/topics/chat_<id>/thread_<id>/` |
| User global | `~/.aurelia/users/<id>/memory/` |
| User project private | `~/.aurelia/users/<id>/projects/<slug>/memory/` |
| Procedural skills | `~/.aurelia/users/<id>/skills/<slug>/SKILL.md` |

| Scope | Purpose | Examples | Default visibility |
|---|---|---|---|
| `user` | Personal cross-project memory | preferences, personal facts | private |
| `user_project` | Personal project memory | work log, TODOs, private decisions | private |
| `project_team` | Shared project knowledge | stack, architecture, conventions | shared among authorized users |
| `topic` | Conversation/topic context | meeting decisions, temporary context | shared within conversation |
| `procedural` | Reusable workflows | Auto-Skills | private unless exported later |

---

## MCP Tool Surface — MVP

Names are illustrative; final names should be stable and grepable.

### `wiki_query`

Search scoped memory.

```json
{
  "query": "why did we reject Redis?",
  "user_id": "12345",
  "project_slug": "-Users-igor-aurelia",
  "scopes": ["user", "user_project", "project_team", "topic"],
  "chat_id": 123,
  "thread_id": 456,
  "limit": 8
}
```

Returns snippets with file path, scope, title, confidence/source metadata and redaction notices.

### `wiki_save`

Save durable facts or decisions to a validated scope.

```json
{
  "user_id": "12345",
  "scope": "project_team",
  "project_slug": "-Users-igor-aurelia",
  "title": "Memory architecture decision",
  "facts": [
    "Wiki should be transversal in access but scoped in storage."
  ],
  "source": "opencode"
}
```

### `wiki_ingest`

Ingest source text into one or more Wiki pages through LLM-assisted extraction, subject to redaction and scope policy.

### `wiki_lint`

Run a conservative lint/consolidation pass over a single scope: duplicates, contradictions, stale claims, missing index entries.

### `wiki_status`

Return active layers, file counts, latest receipts, search index health and last lint/nudge activity.

---

## User Stories

### P0: Canonical scoped gateway ⭐ MVP

**User Story:** Como usuário, quero que Aurelia, PI direto e opencode consultem a mesma memória sem misturar usuários ou projetos.

**Acceptance Criteria:**

1. WHEN uma operação Wiki chega via MCP THEN ela SHALL declarar `user_id` ou usar um contexto autenticado equivalente.
2. WHEN `scope=user` THEN leitura/escrita SHALL usar apenas `~/.aurelia/users/<user_id>/memory/`.
3. WHEN `scope=user_project` THEN leitura/escrita SHALL exigir `user_id` e `project_slug` validável.
4. WHEN `scope=project_team` THEN escrita SHALL exigir classificação explícita como compartilhável.
5. WHEN dados pessoais ou ambíguos aparecem em escrita team THEN operação SHALL recusar ou redirecionar para private.
6. WHEN escopo não pode ser resolvido THEN operação SHALL fail-closed.

**Independent Test:** PI/opencode e Aurelia salvam memórias no mesmo project team scope; User B não vê memórias privadas do User A.

---

### P0: Markdown remains the source of truth ⭐ MVP

**User Story:** Como operador, quero poder auditar e editar a memória com arquivos markdown normais.

**Acceptance Criteria:**

1. Wiki pages SHALL be markdown files with deterministic filenames.
2. `MEMORY.md` SHALL remain the human-readable index per scope.
3. SQLite metadata/search MAY index markdown, but SHALL NOT be the only source of truth.
4. Manual markdown edits SHALL be picked up by status/query after cache/index refresh.
5. Wiki write operations SHALL avoid opaque binary/proprietary state.

---

### P1: Query-before-inject ⭐ MVP

**User Story:** Como Aurelia, quero consultar a Wiki por relevância antes de injetar memória inteira no prompt.

**Acceptance Criteria:**

1. Prompt assembly SHOULD prefer relevant Wiki snippets over loading all files up to a static char budget.
2. Query SHALL search active scopes using user text, agent name, cwd/project and plan state when available.
3. If search/index is unavailable THEN fallback SHALL be current markdown injection behavior.
4. Search result SHALL include scope labels so the model can distinguish personal vs team vs topic facts.
5. Query results SHALL remain wrapped as untrusted memory.

---

### P1: Search backend local-first

**User Story:** Como operador, quero melhor recall sem assumir provider pago de embedding.

**Acceptance Criteria:**

1. MVP MAY use SQLite FTS/BM25 over markdown pages and metadata.
2. Vector embeddings MAY be added later behind optional config.
3. Search index SHALL be rebuildable from markdown files.
4. Index corruption SHALL degrade to markdown scan, not data loss.

---

### P1: Receipts and audit

**User Story:** Como usuário, quero saber quando e por qual cliente uma memória foi criada/alterada.

**Acceptance Criteria:**

1. Every MCP write SHALL append a receipt with timestamp, source, user_id, scope, project/topic metadata and changed files.
2. Receipts SHALL redact sensitive values.
3. `/memory status` and `wiki_status` SHOULD show latest Wiki activity.
4. Failed writes SHALL also be auditable without leaking rejected content.

---

### P2: LLM Wiki schema and lint

**User Story:** Como operador, quero que a memória não vire um monte de markdown sem taxonomia.

**Acceptance Criteria:**

1. Each Wiki scope MAY include a `WIKI_SCHEMA.md` or equivalent schema file.
2. Schema SHALL define page naming, tags, required sections and classification rules.
3. `wiki_lint` SHALL detect duplicates, contradictions, stale claims and missing cross-references.
4. Lint SHALL be conservative: never delete unique information without explicit action/receipt.

---

## Security Requirements

- Wiki MCP must fail closed when user/project/scope context is missing.
- Redaction must run before LLM-assisted ingest or save.
- Project team memory must reject personal facts, secrets and ambiguous private notes.
- Writes must validate path containment and reject symlink escapes.
- MCP clients must not receive arbitrary filesystem paths outside allowed Wiki roots.
- Procedural memory belongs to Auto-Skills and requires explicit user confirmation.

---

## Affected Packages

| Package | Change |
|---|---|
| `internal/runtime/` | Canonical Wiki path resolver per user/project/topic/team |
| `internal/memoryux/` | Status/checkpoint evolve toward Wiki operations and receipts |
| `internal/pipeline/` | Query-before-inject and Wiki context in prompt assembly |
| `internal/dream/` | Nudge/dream write through Wiki scopes |
| `internal/security/` | Redaction and capability checks for Wiki writes |
| `internal/mcp/` or new package | Local MCP server exposing Wiki tools |
| `cmd/aurelia/` | Start/configure Wiki MCP gateway if embedded in daemon |

---

## Success Criteria

- [ ] Same Wiki memory is usable from Aurelia and at least one external MCP-compatible client
- [ ] User-private memory never leaks across users
- [ ] Project team memory remains shared but excludes personal/private facts
- [ ] Markdown remains manually inspectable/editable
- [ ] Query-before-inject reduces prompt bloat without reducing recall for known memories
- [ ] Wiki writes are audited with receipts
- [ ] `go build ./... && go vet ./... && go test ./...` clean when implemented
