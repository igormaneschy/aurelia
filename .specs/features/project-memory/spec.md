# Context-Scoped Memory — Specification

**Roadmap step:** 5
**Status:** 🟡 Parcial (70% — camadas existem, paths não são per-user; model reformulado em 2026-05-30)
**Depende de:** `.specs/features/multi-user-profiles/` (para paths `users/<id>/`)
**Complementa:** `.specs/features/wiki-memory/`, `.specs/features/learning-nudge/`, `.specs/features/auto-skills/`

> **Nota de reformulação (2026-05-30):** A spec original modelava memória de projeto como `(user_id, project_slug)`, com detecção automática de projeto via `scanForProject`. Isso foi reformulado para `(user_id, context_key)` onde o contexto emerge da conversa — alinhando com a remoção do Plan Mode e com o carácter amplo do Aurelia como agente pessoal, não como gestor de projetos. O `/cwd` continua a existir, mas como âncora **declarativa e opt-in** por tópico, não como eixo estruturante da memória.

## Problem Statement

Aurelia precisa lembrar fatos globais, preferências pessoais e contexto de trabalho sem misturar informações entre utilizadores autorizados nem entre conversas. O modelo anterior baseava-se em `project_slug` derivado automaticamente do `cwd` como eixo primário de memória, com `scanForProject` percorrendo o filesystem para resolver o slug.

Esse modelo tem dois problemas fundamentais:

1. **Impõe estrutura onde não existe:** um agente amplo trabalha em contextos fluidos — trading, idiomas, ideias de negócio, debugging espontâneo. Nenhum destes é um "projeto" com slug identificável. O sistema tentava adivinhar o contexto em vez de recebê-lo do utilizador.

2. **Fragilidade operacional:** `scanForProject` é heurístico, frágil em paths com symlinks, mounts ou checkouts múltiplos, e falha silenciosamente quando o projeto não tem marcadores reconhecíveis.

A nova direção é **deixar o contexto emergir da conversa**, com o utilizador a declarar explicitamente quando quer ancorar a sessão a um diretório de trabalho via `/cwd`.

## Model Central

A memória organiza-se à volta de **`(user_id, context_key)`** onde `context_key` tem três origens por ordem de especificidade:

```text
user_id  +  context_key
              ├── topic/thread  →  primário, sempre presente
              ├── cwd overlay   →  opt-in, declarado via /cwd
              └── user global   →  cross-context, sempre carregado
```

O **topic/thread** é o escopamento natural: cada conversa ou tópico Telegram tem identidade própria sem qualquer configuração. O **`/cwd`** é um overlay aditivo — quando o utilizador declara `/cwd /caminho/projeto`, esse contexto de trabalho é injetado como camada adicional sobre o topic, tornando o agente coerente com aquele diretório. Não há deteção automática.

Num grupo Telegram, cada tópico pode ter o seu próprio `/cwd` declarado:

```text
Grupo Telegram
  ├── Tópico "Trading"
  │     /cwd → ~/AutoTradersOMQS
  │     memória: user_global + topic + cwd_overlay + team (se existir)
  │
  ├── Tópico "Brewpub"
  │     /cwd → ~/BrewpubPlan
  │     memória: user_global + topic + cwd_overlay
  │
  └── Tópico geral (sem /cwd)
        memória: user_global + topic
```

## Goals

- [ ] Memória pessoal isolada por `user_id`
- [ ] Topic/thread como escopamento primário natural da memória
- [ ] `/cwd` como overlay declarativo e opt-in, sem deteção automática
- [ ] Prompt assembly injeta apenas camadas relevantes ao `TurnContext`
- [ ] Extrator/nudge classifica fatos na camada correta
- [ ] Dream consolida cada camada sem vazar entre utilizadores ou contextos
- [ ] Team memory como camada separada e opcional, ligada ao cwd quando declarado
- [ ] Migração single-user explícita, idempotente e reversível
- [ ] Layout compatível com Wiki/MCP transversal sem expor memória privada por omissão

## Out of Scope

- `scanForProject` ou qualquer deteção automática de projeto
- Abrir memória de um utilizador para outro
- Multi-tenant entre vários donos/deployments
- Busca full-text em todo o histórico de sessões
- Provider externo de memória (Mem0, Honcho, etc.)
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
│   └── policy/                          # global: regras do deployment
│
├── users/
│   └── <user_id>/
│       ├── profile.json
│       ├── personas/
│       │   └── USER.md                  # quem é este utilizador
│       ├── memory/
│       │   ├── MEMORY.md                # índice pessoal cross-context
│       │   ├── preferences.md
│       │   └── facts.md
│       └── skills/                      # Auto-Skills privadas, PI-compatible
│           └── <slug>/
│               └── SKILL.md
│
├── projects/
│   └── <project_slug>/
│       └── team/
│           ├── MEMORY.md                # índice compartilhável do projeto
│           ├── architecture.md
│           ├── conventions.md
│           └── stack.md
│
└── topics/
    └── chat_<chat_id>/thread_<thread_id>/
        ├── MEMORY.md                    # contexto do tópico
        └── cwd_overlay/                 # presente apenas quando /cwd declarado
            └── MEMORY.md               # contexto específico ao diretório de trabalho
```

> **Nota:** `users/<id>/projects/<slug>/memory/` foi **removido** como camada primária. A memória privada de utilizador num contexto de trabalho fica em `topics/.../cwd_overlay/` — escopada pelo tópico onde o `/cwd` foi declarado, não por slug global.

### Layer semantics

| Layer | Path | Scope | Quando injetado |
|---|---|---|---|
| Aurelia persona | `~/.aurelia/memory/personas/` | deployment global | sempre |
| User global | `~/.aurelia/users/<id>/memory/` | user cross-context | sempre |
| Topic memory | `~/.aurelia/topics/<chat>/<thread>/` | conversation shared | sempre |
| CWD overlay | `~/.aurelia/topics/<chat>/<thread>/cwd_overlay/` | user × cwd declarado | apenas quando `/cwd` ativo no tópico |
| Project team | `~/.aurelia/projects/<slug>/team/` | project shared | apenas quando `/cwd` ativo |
| Procedural skills | `~/.aurelia/users/<id>/skills/<slug>/SKILL.md` | user private | via Auto-Skills |

Estas camadas são o contrato de escopo para a Wiki. Qualquer gateway MCP ou cliente externo deve resolver operações contra uma destas camadas e falhar fechado quando `user_id`, `chat_id/thread_id` ou classificação de escopo forem insuficientes.

### Prompt assembly order

```text
1. Aurelia persona (IDENTITY + SOUL)            — sempre
2. User global (USER.md + preferences + facts)  — sempre
3. Topic memory                                 — sempre
4. CWD overlay (se /cwd declarado no tópico)    — opt-in
5. Project team (se /cwd declarado)             — opt-in
```

---

## User Stories

### P0: Prompt assembly com camadas corretas ⭐ MVP

**User Story:** Como utilizador autorizado, quero que Aurelia use as minhas memórias e o contexto correto da conversa sem misturar com outro utilizador.

**Acceptance Criteria:**

1. WHEN `TurnContext` contém `user_id` THEN prompt SHALL carregar `USER.md` e memória pessoal desse utilizador.
2. WHEN `chat_id/thread_id` existe THEN prompt SHALL carregar topic memory desse tópico.
3. WHEN `/cwd` está declarado no tópico THEN prompt SHALL adicionar cwd_overlay como camada aditiva.
4. WHEN `/cwd` está declarado THEN prompt MAY carregar team memory do projeto.
5. WHEN outro utilizador no mesmo tópico fala THEN ele SHALL partilhar topic memory e team memory, mas não USER nem cwd_overlay privado.
6. WHEN não há `/cwd` declarado THEN nenhuma camada de cwd_overlay ou team memory SHALL ser injetada.

**Independent Test:** User A e User B no mesmo tópico com `/cwd` declarado recebem prompts com mesma team/topic memory, mas USER e cwd_overlay com writes separados.

---

### P0: Path resolver para context_key ⭐ MVP

**User Story:** Como sistema, quero um único resolver para calcular paths de memória sem duplicar sanitização nem usar heurísticas de filesystem.

**Acceptance Criteria:**

1. `PathResolver.UserMemoryDir(userID)` SHALL apontar para `~/.aurelia/users/<id>/memory/`.
2. `PathResolver.TopicMemoryDir(chatID, threadID)` SHALL apontar para `~/.aurelia/topics/chat_<id>/thread_<id>/`.
3. `PathResolver.TopicCwdOverlayDir(chatID, threadID)` SHALL apontar para `~/.aurelia/topics/chat_<id>/thread_<id>/cwd_overlay/`.
4. `PathResolver.ProjectTeamMemoryDir(cwd)` SHALL apontar para `~/.aurelia/projects/<slug>/team/`.
5. `ProjectSlug(cwd)` permanece para team memory — determinístico, filesystem-safe e estável.
6. Path traversal, symlink ambíguo ou cwd vazio SHALL retornar erro claro.
7. Nenhum path de memória SHALL ser calculado a partir de `scanForProject` ou travessia automática do filesystem.

**Independent Test:** Mesmo cwd + utilizadores diferentes geram cwd_overlay em paths de topic distintos e team memory igual.

---

### P0: /cwd como overlay declarativo ⭐ MVP

**User Story:** Como utilizador, quero declarar `/cwd /caminho` num tópico para que Aurelia fique contextualizada com esse diretório de trabalho nessa conversa.

**Acceptance Criteria:**

1. `/cwd /caminho` SHALL persistir o binding por `ConversationKey{chat_id, thread_id}`.
2. Com `/cwd` ativo, prompt SHALL incluir cwd_overlay e team memory como camadas adicionais.
3. `/cwd clear` SHALL remover o binding e as camadas adicionais da conversa.
4. Sem `/cwd`, o agente funciona normalmente com user global + topic memory.
5. O mesmo tópico com `/cwd` diferente SHALL ter cwd_overlay separado.

**Independent Test:** Dois tópicos com `/cwd` para projetos distintos têm contextos completamente independentes.

---

### P1: Classificação de fatos por camada ⭐ MVP

**User Story:** Como Aurelia, quero salvar cada memória no lugar certo para reduzir ruído e evitar vazamento.

**Acceptance Criteria:**

1. Fatos pessoais e preferências SHALL ir para user global.
2. Notas e contexto do trabalho atual SHALL ir para cwd_overlay do tópico (quando `/cwd` ativo).
3. Stack, padrões e decisões compartilháveis SHALL ir para project team.
4. Decisões específicas do tópico SHALL ir para topic memory.
5. Fatos sensíveis ou ambíguos SHALL preferir camada privada do utilizador.
6. Extrator SHALL receber instruções explícitas de não escrever dados pessoais em team memory.

**Independent Test:** Entrada com nome do utilizador, stack do projeto e nota pessoal produz três writes em camadas distintas.

---

### P1: Dream/consolidação por camada ⭐ MVP

**User Story:** Como operador, quero consolidar memórias sem misturar escopos.

**Acceptance Criteria:**

1. Dream global de user SHALL consolidar apenas `~/.aurelia/users/<id>/memory/`.
2. Dream topic SHALL consolidar apenas topic memory daquele `ConversationKey`.
3. Dream cwd_overlay SHALL consolidar apenas o overlay do tópico com `/cwd` ativo.
4. Dream team SHALL consolidar apenas project team memory e nunca incluir USER facts.
5. Dream/nudge SHALL usar `CapabilityProfile=edit_project` sem `Bash`.

**Independent Test:** Rodar dream para User A não altera ficheiros de User B.

---

### P1: Migração single-user explícita ⭐ MVP

**User Story:** Como dono do deployment, quero migrar a memória atual para o novo layout sem risco de perda.

**Acceptance Criteria:**

1. Migração SHALL ser comando explícito, não automática no boot.
2. `--dry-run` SHALL mostrar todos os moves/copies planeados.
3. Migração SHALL copiar, verificar e só então remover origem.
4. `USER.md` legado SHALL ir para `users/<target_id>/personas/USER.md`.
5. Memórias pessoais legadas SHALL ir para `users/<target_id>/memory/`.
6. Project memories legadas com cwd conhecido SHALL ir para `topics/.../ cwd_overlay/` do tópico primário, ou para `users/<target_id>/memory/` como fallback.
7. Marker de migração SHALL evitar rerun acidental.

---

### P2: Team memory compartilhável

**User Story:** Como equipa, quero separar convenções compartilháveis das notas privadas.

**Acceptance Criteria:**

1. Team memory SHALL ter diretório próprio por projeto (`projects/<slug>/team/`).
2. Escritas em team memory SHALL exigir classificação clara como compartilhável.
3. Futuro sync via git SHALL ser possível sem exportar memórias privadas.
4. `/memory team` futuro MAY listar/resumir team memory.

---

### P2: Memory UX e checkpoints

**User Story:** Como utilizador em fluxos longos, quero entender qual memória está ativa e quando Aurelia guardou/atualizou contexto.

**Acceptance Criteria:**

1. `/memory status` MAY mostrar camadas ativas: user, topic, cwd_overlay (se ativo), team e último checkpoint.
2. `/memory checkpoint` MAY materializar um resumo curto do trabalho atual.
3. Nudge/dream SHOULD registar um receipt resumido: quantidade de writes, layers tocadas e sugestões de Auto-Skill.
4. Checkpoints SHALL ficar no escopo correto (`topic` ou `cwd_overlay`) e nunca em team memory sem classificação explícita.
5. Procedimentos reutilizáveis detetados SHALL virar apenas sugestão para Auto-Skills; criação de `SKILL.md` continua a exigir confirmação.

---

## Extraction Classification Guide

| Informação | Camada |
|---|---|
| Nome, idioma, estilo do utilizador | User global |
| Preferência pessoal de ferramenta | User global |
| "Neste contexto quero fazer X" | CWD overlay do tópico (se `/cwd` ativo) |
| "Hoje implementei Y" | CWD overlay do tópico |
| Stack, versões, comandos de validação | Project team |
| Convenções de arquitetura/testes | Project team |
| Decisão tomada no tópico atual | Topic memory |
| Workflow reutilizável aprendido | Auto-Skill PI-compatible após confirmação |
| PII, segredo, dado privado | User private ou descartar; nunca team |

---

## Affected Packages

| Package | Change |
|---|---|
| `internal/runtime/` | PathResolver sem `scanForProject`; métodos `TopicMemoryDir`, `TopicCwdOverlayDir`, `UserMemoryDir` |
| `internal/persona/` | USER por user; IDENTITY/SOUL globais |
| `internal/pipeline/` | Prompt assembly com `TurnContext` e camadas: user_global + topic + cwd_overlay + team |
| `internal/dream/` | Extrator/nudge com targets escopados por camada |
| `internal/session/` | `SessionKey` e `ConversationKey` sem dependência de `project_slug` |
| `internal/skills/` | Auto-Skills privadas em layout PI-compatible |
| `cmd/aurelia/` | Comando de migração explícito |

---

## Success Criteria

- [ ] User A não lê memória privada de User B
- [ ] Prompt sem `/cwd` não injeta cwd_overlay nem team memory
- [ ] Prompt com `/cwd` injeta overlay e team como camadas aditivas
- [ ] Dois tópicos com `/cwd` distintos têm contextos completamente independentes
- [ ] Dream/nudge não misturam camadas
- [ ] Nenhum path é calculado por `scanForProject` ou travessia automática
- [ ] Wiki/MCP pode consumir o mesmo layout sem bypass de isolamento
- [ ] Migração single-user preserva dados e é dry-run friendly
- [ ] `go build ./... && go vet ./... && go test ./...` limpo quando implementado

---

## Project UUID Schema (Document Only — No Migration Yet)

### Motivation

O slug atual (`ProjectSlug(cwd)`) é determinístico mas não sobrevive a renomes ou relocalizações de diretório. Para a team memory, que é partilhada e potencialmente versionável via git, um UUID estável seria preferível a longo prazo.

### Proposed Schema

```sql
-- Registry: identidade canónica do projeto
CREATE TABLE IF NOT EXISTS project_registry (
    project_uuid TEXT PRIMARY KEY,
    project_slug TEXT NOT NULL,
    cwd TEXT NOT NULL,
    display_name TEXT,
    first_seen_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

ALTER TABLE project_bindings ADD COLUMN project_uuid TEXT REFERENCES project_registry(project_uuid);
```

### Migration Strategy (Future Sprint)

1. Add `project_registry` table and backfill existing bindings.
2. On first `/cwd` resolution, generate UUID and register.
3. Switch team memory path to `projects/<project_uuid>/team/`.
4. Keep slug-based path as compatibility alias during deprecation.

### Non-Goals for This Document

- Implementar a migração UUID neste sprint.
- Alterar `ProjectTeamMemoryDir` neste sprint.
- Database schema changes neste sprint.
