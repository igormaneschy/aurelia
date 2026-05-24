# User-Scoped Project Memory — Specification

**Roadmap step:** 5  
**Status:** 🟡 Parcial (70% — camadas existem, paths não são per-user)  
**Depende de:** `.specs/features/multi-user-profiles/` (para paths `users/<id>/`)  
**Depende de:** `.specs/features/project-binding/` (✅ done — project slug/effective cwd persistente)  
**Complementa:** `.specs/features/wiki-memory/`, `.specs/features/learning-nudge/`, `.specs/features/auto-skills/`

## Problem Statement

Aurelia precisa lembrar fatos globais, preferências pessoais e contexto de projeto sem misturar informações entre usuários autorizados nem entre projetos. A versão anterior desta spec modelava memória de projeto como global por `cwd`:

```text
~/.aurelia/projects/<project_slug>/memory/
```

Isso conflita com **User Isolation**: dois usuários autorizados podem trabalhar no mesmo repositório, mas suas anotações pessoais, work log, decisões individuais e preferências de projeto não devem se misturar.

A nova direção é separar claramente os escopos que serão usados pela Wiki / LLM Wiki local-first do Aurelia:

1. **Aurelia global** — identidade da Aurelia, políticas e conhecimento compartilhado do deployment.
2. **User global** — fatos e preferências pessoais do usuário, cross-project.
3. **User × Project private** — memória pessoal daquele usuário naquele projeto.
4. **Project team memory** — convenções e decisões compartilháveis do projeto, opcionalmente versionáveis no futuro.
5. **Conversation/topic memory** — contexto compartilhado por tópico Telegram autorizado.
6. **Procedural memory** — procedimentos reutilizáveis não ficam em arquivos genéricos de memória; viram Auto-Skills privadas em layout PI-compatible (`users/<id>/skills/<slug>/SKILL.md`) quando o usuário confirma.

Esta spec define o **modelo de escopos e diretórios**. A spec `wiki-memory` promove esses escopos para uma camada transversal acessível via MCP, para que Aurelia, PI direto, PI Code/opencode e futuros agentes possam consultar a mesma memória sem violar isolamento.

## Goals

- [ ] Memória pessoal isolada por `user_id`
- [ ] Memória de projeto privada isolada por `(user_id, project_slug)`
- [ ] Team memory separada da memória privada, com possibilidade futura de sincronizar via git
- [ ] IDENTITY/SOUL continuam globais ao deployment; USER é por usuário
- [ ] System prompt injeta apenas camadas relevantes ao `TurnContext`
- [ ] Extrator/nudge classifica fatos na camada correta
- [ ] Dream consolida cada camada sem vazar entre usuários
- [ ] `/cwd` é um project binding persistente por conversa/thread, mas memória pessoal vem do sender
- [ ] Migração single-user é explícita, idempotente e reversível em duas fases
- [ ] Layout resultante é compatível com Wiki/MCP transversal sem expor memória privada por padrão

## Out of Scope

- Abrir memória de um usuário para outro usuário
- Multi-tenant entre vários donos/deployments
- Busca full-text em todo histórico de sessões
- Provider externo de memória (Mem0, Honcho etc.)
- Substituir markdown por banco proprietário de memória
- Sincronização automática da team memory via git no MVP
- UI web para editar memória

---

## Memory Layers

### Directory model

```text
~/.aurelia/
├── memory/
│   ├── personas/
│   │   ├── IDENTITY.md                  # global: quem é Aurelia
│   │   └── SOUL.md                      # global: personalidade/valores
│   └── policy/                          # global: regras do deployment, futuras
│
├── users/
│   └── <user_id>/
│       ├── profile.json
│       ├── personas/
│       │   └── USER.md                  # quem é este usuário
│       ├── memory/
│       │   ├── MEMORY.md                # índice pessoal cross-project
│       │   ├── preferences.md
│       │   └── facts.md
│       ├── projects/
│       │   └── <project_slug>/
│       │       └── memory/
│       │           ├── MEMORY.md        # índice pessoal neste projeto
│       │           ├── notes.md
│       │           └── work_log.md
│       └── skills/                      # Auto-Skills privadas, PI-compatible
│           └── <slug>/
│               └── SKILL.md
│
├── projects/
│   └── <project_slug>/
│       └── team/
│           ├── MEMORY.md                # índice compartilhável
│           ├── architecture.md
│           ├── conventions.md
│           └── stack.md
│
└── topics/
    └── chat_<chat_id>/thread_<thread_id>/
        └── MEMORY.md                    # contexto compartilhado do tópico
```

### Layer semantics

| Layer | Path | Scope | Examples |
|---|---|---|---|---|
| Aurelia persona | `~/.aurelia/memory/personas/` | deployment global | IDENTITY, SOUL |
| User global | `~/.aurelia/users/<id>/memory/` | user cross-project | nome, idioma, preferências |
| User project private | `~/.aurelia/users/<id>/projects/<slug>/memory/` | user × project | work log, notas pessoais |
| Project team | `~/.aurelia/projects/<slug>/team/` | project shared | stack, padrões, ADRs resumidos |
| Topic memory | `~/.aurelia/topics/chat_<id>/thread_<id>/` | conversation shared | decisões do tópico, contexto temporário |
| Procedural skills | `~/.aurelia/users/<id>/skills/<slug>/SKILL.md` | user private | procedimentos reutilizáveis, workflows, runbooks |

Essas camadas são o contrato de escopo para a Wiki. Qualquer gateway MCP ou cliente externo deve resolver operações contra uma dessas camadas e falhar fechado quando `user_id`, `project_slug`, `chat_id/thread_id` ou classificação de escopo forem insuficientes.

---

## User Stories

### P0: Prompt assembly com camadas corretas ⭐ MVP

**User Story:** Como usuário autorizado, quero que Aurelia use minhas memórias, o contexto do projeto e o tópico correto sem misturar com outro usuário.

**Acceptance Criteria:**

1. WHEN `TurnContext` contém `user_id` THEN prompt SHALL carregar `USER.md` e memória pessoal desse usuário.
2. WHEN `cwd` efetivo existe THEN prompt SHALL carregar memória privada `(user_id, project_slug)`.
3. WHEN `cwd` efetivo existe THEN prompt MAY carregar team memory do projeto.
4. WHEN `chat_id/thread_id` existe THEN prompt MAY carregar topic memory compartilhada.
5. WHEN outro usuário no mesmo tópico fala THEN ele SHALL compartilhar `/cwd` e topic memory, mas não USER/memória privada.
6. WHEN não há `cwd` THEN nenhuma memória de projeto SHALL ser injetada.

**Independent Test:** User A e User B no mesmo tópico/projeto recebem prompts com mesma team/topic memory, mas USER e project-private diferentes.

---

### P0: Path resolver para user × project ⭐ MVP

**User Story:** Como sistema, quero um único resolver para calcular paths de memória sem duplicar sanitização.

**Acceptance Criteria:**

1. `PathResolver.UserMemoryDir(userID)` SHALL apontar para `~/.aurelia/users/<id>/memory/`.
2. `PathResolver.UserProjectMemoryDir(userID, cwd)` SHALL apontar para `~/.aurelia/users/<id>/projects/<slug>/memory/`.
3. `PathResolver.ProjectTeamMemoryDir(cwd)` SHALL apontar para `~/.aurelia/projects/<slug>/team/`.
4. `ProjectSlug(cwd)` SHALL ser determinístico, filesystem-safe e estável entre boots.
5. Path traversal, symlink ambíguo ou cwd vazio SHALL retornar erro claro.

**Independent Test:** Mesmo cwd + users diferentes geram project-private diferentes e team memory igual.

---

### P1: Classificação de fatos por camada ⭐ MVP

**User Story:** Como Aurelia, quero salvar cada memória no lugar certo para reduzir ruído e evitar vazamento.

**Acceptance Criteria:**

1. Fatos pessoais e preferências SHALL ir para user global.
2. Work log e notas individuais do projeto SHALL ir para user project private.
3. Stack, padrões e decisões compartilháveis SHALL ir para project team.
4. Decisões específicas do tópico SHALL ir para topic memory.
5. Fatos sensíveis ou ambíguos SHALL preferir camada privada do usuário.
6. Extrator SHALL receber instruções explícitas de não escrever dados pessoais em team memory.

**Independent Test:** Entrada com nome do usuário, stack do projeto e nota pessoal produz três writes em camadas distintas.

---

### P1: Dream/consolidação por camada ⭐ MVP

**User Story:** Como operador, quero consolidar memórias sem misturar escopos.

**Acceptance Criteria:**

1. Dream global de user SHALL consolidar apenas `~/.aurelia/users/<id>/memory/`.
2. Dream project private SHALL consolidar apenas `(user_id, project_slug)`.
3. Dream team SHALL consolidar apenas project team memory e nunca incluir USER facts.
4. Dream topic SHALL consolidar apenas topic memory daquele `ConversationKey`.
5. Dream/nudge SHALL usar `CapabilityProfile=edit_project` sem `Bash`.

**Independent Test:** Rodar dream para User A não altera arquivos de User B.

---

### P1: Migração single-user explícita ⭐ MVP

**User Story:** Como dono do deployment, quero migrar a memória atual para o novo layout por usuário sem risco de perda.

**Acceptance Criteria:**

1. Migração SHALL ser comando explícito, não automática no boot.
2. `--dry-run` SHALL mostrar todos os moves/copies planejados.
3. Migração SHALL copiar, verificar e só então remover origem.
4. `USER.md` legado SHALL ir para `users/<target_id>/personas/USER.md`.
5. Memórias pessoais legadas SHALL ir para `users/<target_id>/memory/`.
6. Project memories legadas SHALL ir para `users/<target_id>/projects/<slug>/memory/` salvo classificação explícita como team.
7. Marker de migração SHALL evitar rerun acidental.

---

### P2: Team memory compartilhável

**User Story:** Como time, quero separar convenções compartilháveis das notas privadas.

**Acceptance Criteria:**

1. Team memory SHALL ter diretório próprio por projeto.
2. Escritas em team memory SHALL exigir classificação clara como compartilhável.
3. Futuro sync via git SHALL ser possível sem exportar memórias privadas.
4. `/memory team` futuro MAY listar/resumir team memory.

---

### P2: Memory UX e checkpoints

**User Story:** Como usuário em fluxos longos, quero entender qual memória está ativa e quando Aurelia salvou/atualizou contexto, para reduzir sensação de degradação ou improviso.

**Acceptance Criteria:**

1. `/memory status` MAY mostrar camadas ativas: user, project-private, team, topic e último checkpoint.
2. `/memory checkpoint` MAY materializar um resumo curto do trabalho atual: objetivo, status, decisões, próximos passos e arquivos relevantes.
3. Nudge/dream SHOULD registrar um receipt resumido: quantidade de writes, layers tocadas e sugestões de Auto-Skill.
4. Checkpoints SHALL ficar no escopo correto (`topic` ou user project private) e nunca em team memory sem classificação explícita.
5. Procedimentos reutilizáveis detectados em checkpoints SHALL virar apenas sugestão para Auto-Skills; criação de `SKILL.md` continua exigindo confirmação.

---

## Extraction Classification Guide

| Informação | Camada |
|---|---|
| Nome, idioma, estilo do usuário | User global |
| Preferência pessoal de ferramenta | User global |
| “Neste projeto eu quero revisar X” | User project private |
| “Hoje implementei Y” | User project private |
| Stack, versões, comandos de validação | Project team |
| Convenções de arquitetura/testes | Project team |
| Decisão tomada no tópico atual | Topic memory ou Project team, conforme escopo |
| Workflow reutilizável aprendido | Auto-Skill PI-compatible após confirmação |
| PII, segredo, dado privado | User private ou descartar; nunca team |

---

## Affected Packages

| Package | Change |
|---|---|
| `internal/runtime/` | Path resolver para user/project/team/topic memory |
| `internal/persona/` | USER por user; IDENTITY/SOUL globais |
| `internal/pipeline/` | Prompt assembly com `TurnContext` e camadas corretas |
| `internal/dream/` | Extrator/nudge com targets escopados |
| `internal/session/` | Separar `SessionKey` de `ConversationKey` |
| `internal/skills/` | Auto-Skills privadas em layout PI-compatible, fora das camadas de fatos |
| `cmd/aurelia/` | Comando de migração explícito |

---

## Success Criteria

- [ ] User A não lê memória privada de User B
- [ ] Mesmo projeto tem private memory por user e team memory compartilhada
- [ ] Prompt sem cwd não injeta memória de projeto
- [ ] Dream/nudge não misturam camadas
- [ ] Wiki/MCP pode consumir o mesmo layout sem bypass de isolamento
- [ ] Migração single-user preserva dados e é dry-run friendly
- [ ] `go build ./... && go vet ./... && go test ./...` limpo quando implementado

---

## Project UUID Schema (Document Only — No Migration Yet)

### Motivation

The current project path uses `ProjectSlug(cwd)` — a deterministic, filesystem-safe encoding of the working directory path. This is sufficient for single-deployment scenarios but has limitations:

- **Renaming/relocating a project** changes the slug and breaks all existing memory associations.
- **Same project accessed via different paths** (symlinks, mounts, different checkout directories) creates duplicate project entries.
- **Cross-referencing** between project memory and other Aurelia stores (bindings, runlog, cron) requires a stable key that survives directory moves.

### Proposed Schema

Add a `project_uuid` column to the `project_bindings` table and a new `project_registry` table:

```sql
-- Registry: canonical project identity
CREATE TABLE IF NOT EXISTS project_registry (
    project_uuid TEXT PRIMARY KEY,        -- v7 UUID, generated on first /cwd
    project_slug TEXT NOT NULL,           -- current slug (may change on rename)
    cwd TEXT NOT NULL,                    -- current resolved path
    display_name TEXT,                    -- human-readable name (e.g., repo name)
    first_seen_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Bindings point to project_uuid, not slug directly
ALTER TABLE project_bindings ADD COLUMN project_uuid TEXT REFERENCES project_registry(project_uuid);
```

### Memory Path Resolution with project_uuid

Once the registry exists, memory paths would resolve via `project_uuid`:

```text
~/.aurelia/projects/<project_uuid>/team/          # team memory (stable)
~/.aurelia/users/<user_id>/projects/<project_uuid>/memory/  # user×project private (stable)
```

The slug-based path would remain as a compatibility alias during migration:

```text
~/.aurelia/projects/<project_slug>/team/          # legacy alias (symlink or migration)
```

### Migration Strategy (Future Sprint)

1. Add `project_registry` table and `project_uuid` column to `project_bindings`.
2. On first `/cwd` resolution, generate a UUID and register the project.
3. Add a migration command to backfill existing bindings.
4. After all bindings have UUIDs, switch memory path resolution to use UUID.
5. Add a compatibility layer (symlinks or redirect) for existing slug-based paths.
6. Remove slug-based fallback after a deprecation period.

### Non-Goals for This Document

- Implementing the migration — documented here for architecture alignment.
- Changing the current `ProjectMemoryDir` / `ProjectTeamMemoryDir` path resolution.
- Database schema changes in this sprint.
