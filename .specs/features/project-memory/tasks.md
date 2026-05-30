# Sprint E — Context-Scoped Memory · Tasks

**Roadmap:** Sprint E  
**Spec:** `.specs/features/project-memory/`  
**Status geral:** 🔴 Não iniciado  
**Depende de:** Sprint 1 (User Isolation ✅), Sprint 3 (Project Binding ✅)  
**Desbloqueia:** Sprint F (Wiki Memory Gateway), Sprint G (Learning Nudge)

---

## Contexto e decisão de design (2026-05-30)

O modelo anterior centrava-se em `(user_id, project_slug)` com deteção automática de projeto via `scanForProject`. Foi reformulado para `(user_id, context_key)` onde o contexto emerge da conversa, não do filesystem. O `/cwd` continua a existir, mas como overlay **declarativo e opt-in** por tópico — não como eixo estruturante da memória.

**Novo layout canónico:**

```text
~/.aurelia/
├── memory/personas/          IDENTITY.md + SOUL.md  (global)
├── users/<user_id>/
│   ├── personas/USER.md
│   └── memory/               user_global (cross-context)
├── projects/<slug>/team/     project team memory
└── topics/chat_<id>/thread_<id>/
    ├── MEMORY.md             topic memory
    └── cwd_overlay/          presente só quando /cwd declarado no tópico
        └── MEMORY.md
```

**Camadas de prompt assembly (ordem fixa):**

```text
1. Persona global (IDENTITY + SOUL)
2. User global (USER.md + memory)
3. Topic memory
4. CWD overlay      ← só quando /cwd ativo no tópico
5. Project team     ← só quando /cwd ativo
```

---

## E1 — Patch das specs afetadas

**Prioridade:** P0 · **Pré-condição:** nenhuma · **Esforço estimado:** 1–2h

### Objetivo

Actualizar `wiki-memory/spec.md` e `multi-user-profiles/spec.md` para remover referências ao scope `user_project` e ao path `users/<id>/projects/<slug>/memory/`, adoptando os escopos canónicos do novo modelo.

### Tasks

- [ ] **E1.1** — `wiki-memory/spec.md`: remover `users/<id>/projects/<slug>/memory/` do directory model e da tabela de paths canónicos
- [ ] **E1.2** — `wiki-memory/spec.md`: substituir scope `user_project` por `cwd_overlay` (escopado por `ConversationKey{chat_id, thread_id}`, não por `user×slug`)
- [ ] **E1.3** — `wiki-memory/spec.md`: actualizar tabela de scope names para os nomes canónicos: `user_global`, `topic`, `cwd_overlay`, `project_team`, `procedural`
- [ ] **E1.4** — `wiki-memory/spec.md`: ajustar `wiki_query` e `wiki_save` para não exigirem `project_slug` em scope cwd_overlay — substituir por `chat_id + thread_id`
- [ ] **E1.5** — `multi-user-profiles/spec.md`: marcar user story "Project memory por user × projeto" (P1) como **movida para Sprint E** com nota de reformulação
- [ ] **E1.6** — `multi-user-profiles/spec.md`: corrigir acceptance criterion da migração CLI — o passo `~/.aurelia/projects/` → `users/<id>/projects/` deve ser removido (project team continua em `projects/<slug>/team/`; memória privada contextualizada vai para `topics/.../cwd_overlay/`)
- [ ] **E1.7** — `multi-user-profiles/spec.md`: actualizar goals list — marcar "Project memory privada por `(user_id, project_slug)`" como **reformulada** para `cwd_overlay` por tópico

### Acceptance criteria

1. Nenhuma spec activa deve referenciar `users/<id>/projects/<slug>/memory/` como path de escrita.
2. `wiki-memory` deve listar apenas os 5 escopos canónicos e os paths correspondentes em `project-memory`.
3. `multi-user-profiles` deve reflectir que memória privada contextualizada de trabalho vive em `cwd_overlay`, não em `user×project`.

---

## E2 — PathResolver canónico

**Prioridade:** P0 · **Pré-condição:** E1 · **Esforço estimado:** 2–3h  
**Package:** `internal/runtime/`

### Objetivo

Implementar (ou fechar) os métodos do `PathResolver` que servem de único ponto de verdade para os caminhos de memória. Eliminar qualquer cálculo de path duplicado ou hardcoded fora deste resolver.

### Tasks

- [ ] **E2.1** — Implementar `PathResolver.UserMemoryDir(userID string) string` → `~/.aurelia/users/<id>/memory/`
- [ ] **E2.2** — Implementar `PathResolver.TopicMemoryDir(chatID int64, threadID int) string` → `~/.aurelia/topics/chat_<id>/thread_<id>/`
- [ ] **E2.3** — Implementar `PathResolver.TopicCwdOverlayDir(chatID int64, threadID int) string` → `~/.aurelia/topics/chat_<id>/thread_<id>/cwd_overlay/`
- [ ] **E2.4** — Manter `PathResolver.ProjectTeamMemoryDir(cwd string) string` → `~/.aurelia/projects/<slug>/team/` usando `ProjectSlug(cwd)` determinístico
- [ ] **E2.5** — Garantir que path traversal, symlink ambíguo ou `cwd` vazio retornam `error` claro (não path silenciosamente errado)
- [ ] **E2.6** — Garantir que nenhum outro package calcula paths de memória fora do `PathResolver` (grep por `filepath.Join` em contextos de memória fora de `internal/runtime/`)
- [ ] **E2.7** — Testes unitários para todos os métodos do PathResolver, incluindo edge cases: `userID` vazio, `chatID=0`, `threadID=0`, cwd com symlink

### Acceptance criteria

1. `PathResolver.TopicCwdOverlayDir(123, 456)` retorna `~/.aurelia/topics/chat_123/thread_456/cwd_overlay/`.
2. `PathResolver.UserMemoryDir("12345")` retorna `~/.aurelia/users/12345/memory/`.
3. Cwd vazio em `ProjectTeamMemoryDir` retorna erro, não path com slug vazio.
4. Nenhum path de memória é calculado fora de `internal/runtime/PathResolver`.

---

## E3 — Remoção de `scanForProject` e heurísticas automáticas

**Prioridade:** P0 · **Pré-condição:** E2 · **Esforço estimado:** 1–2h  
**Packages afectados:** `internal/runtime/`, `internal/pipeline/`, callers relevantes

### Objetivo

Eliminar completamente `scanForProject` e qualquer mecanismo que tente inferir o projecto por travessia automática do filesystem sem declaração explícita do utilizador.

### Tasks

- [ ] **E3.1** — Localizar todos os callers de `scanForProject` (ou equivalente) e mapear o que cada um faz com o resultado
- [ ] **E3.2** — Remover a função `scanForProject` e os seus callers do código de produção
- [ ] **E3.3** — Para cada caller removido: substituir por leitura do binding persistente via `ConversationKey` (já implementado em project-binding ✅) ou por ausência de contexto de projecto quando não há `/cwd`
- [ ] **E3.4** — Garantir que auto-detect de projecto não persiste sem confirmação explícita do utilizador (alinhado com project-binding P1 já implementado)
- [ ] **E3.5** — Remover path `users/<id>/projects/<slug>/memory/` de qualquer código de criação/leitura de directórios
- [ ] **E3.6** — `go build ./... && go vet ./...` limpo após remoção

### Acceptance criteria

1. `grep -r "scanForProject" .` retorna zero resultados em código de produção.
2. Sem `/cwd` declarado, pipeline não injeta nenhuma camada de projecto no prompt.
3. `go vet ./...` limpo.

---

## E4 — Prompt assembly com camadas explícitas

**Prioridade:** P0 · **Pré-condição:** E2, E3 · **Esforço estimado:** 3–5h  
**Package:** `internal/pipeline/`

### Objetivo

Garantir que o prompt assembly usa as 5 camadas na ordem fixa definida pelo novo modelo, recebendo o `TurnContext` completo (com `user_id`, `chat_id`, `thread_id`) e consultando apenas o `PathResolver` para os caminhos.

### Tasks

- [ ] **E4.1** — Mapear o estado actual do prompt assembly: quais blocos são injectados, em que ordem, e como os paths são calculados hoje
- [ ] **E4.2** — Refactorizar para ordem canónica: persona global → user global → topic → cwd_overlay (condicional) → project team (condicional)
- [ ] **E4.3** — Camadas cwd_overlay e project team só são injectadas quando `ProjectBinding` existe para o `ConversationKey` do turno
- [ ] **E4.4** — User global usa `PathResolver.UserMemoryDir(ctx.UserID)` — nunca `~/.aurelia/memory/` directamente
- [ ] **E4.5** — Topic memory usa `PathResolver.TopicMemoryDir(ctx.ChatID, ctx.ThreadID)`
- [ ] **E4.6** — CWD overlay usa `PathResolver.TopicCwdOverlayDir(ctx.ChatID, ctx.ThreadID)` apenas quando binding activo
- [ ] **E4.7** — Testes: prompt sem `/cwd` não inclui cwd_overlay nem project team; prompt com `/cwd` inclui ambas as camadas adicionais
- [ ] **E4.8** — Testes: dois utilizadores no mesmo tópico partilham topic memory mas têm user global independente

### Acceptance criteria

1. Prompt sem `/cwd` declarado não contém nenhuma secção de cwd_overlay ou project team.
2. Prompt com `/cwd` activo contém exactamente as 5 camadas na ordem definida.
3. Dois `user_id` diferentes no mesmo `(chat_id, thread_id)` produzem prompts com user global diferente mas topic memory idêntica.
4. Nenhum path hardcoded em `internal/pipeline/` — todos via `PathResolver`.

---

## E5 — Wiring do `/cwd` como overlay declarativo

**Prioridade:** P0 · **Pré-condição:** E2, E3, E4 · **Esforço estimado:** 2–3h  
**Packages:** `internal/pipeline/`, `internal/session/` ou equivalente

### Objetivo

Garantir que o binding persistente de `/cwd` por `ConversationKey` (já implementado em project-binding) é correctamente consumido pelo pipeline para activar as camadas cwd_overlay e project team — e apenas essas camadas, apenas quando o binding existe.

### Tasks

- [ ] **E5.1** — Pipeline lê `ProjectBinding` para `ConversationKey{ChatID, ThreadID}` no início de cada turno
- [ ] **E5.2** — Se binding não existe: pipeline não cria directórios de cwd_overlay, não injeta essas camadas, não falha
- [ ] **E5.3** — Se binding existe: pipeline resolve `TopicCwdOverlayDir` e `ProjectTeamMemoryDir` e injeta no prompt
- [ ] **E5.4** — `/cwd clear` remove o binding e as camadas adicionais desaparecem na próxima rodada
- [ ] **E5.5** — Dois tópicos com `/cwd` para projectos distintos têm `cwd_overlay` completamente independentes
- [ ] **E5.6** — Teste de regressão: comportamento de tópico sem `/cwd` é idêntico ao comportamento anterior (apenas user global + topic)

### Acceptance criteria

1. Tópico A com `/cwd /repo/aurelia` e tópico B com `/cwd /repo/brewpub` têm `cwd_overlay` em paths distintos e não partilham conteúdo.
2. Após `/cwd clear`, prompt do tópico já não contém cwd_overlay na rodada seguinte.
3. Tópico sem `/cwd` tem comportamento functionally idêntico ao anterior ao Sprint E.

---

## E6 — Classificação de writes por camada

**Prioridade:** P1 · **Pré-condição:** E4, E5 · **Esforço estimado:** 3–4h  
**Package:** `internal/dream/` (extractor/nudge)

### Objetivo

Fazer o extractor/nudge decidir explicitamente para qual camada cada facto vai, com fail-safe para mandar dados ambíguos para a camada privada do utilizador.

### Tasks

- [ ] **E6.1** — Definir critérios de classificação explícitos no extractor (ver tabela da spec):
  - Factos pessoais/preferências → `user_global`
  - Contexto de trabalho actual ("hoje implementei X") → `cwd_overlay` do tópico (se `/cwd` activo)
  - Stack, versões, convenções → `project_team`
  - Decisões específicas do tópico → `topic memory`
  - PII, segredos, dados ambíguos → `user_global` ou descartar; nunca `project_team`
- [ ] **E6.2** — Extractor usa `TurnContext` para saber se `/cwd` está activo antes de decidir escrever em cwd_overlay
- [ ] **E6.3** — Extractor não escreve dados pessoais em `project_team` — validação explícita
- [ ] **E6.4** — Quando `/cwd` não está activo, factos de contexto de trabalho vão para `topic memory` (não para cwd_overlay)
- [ ] **E6.5** — Teste: entrada com nome do utilizador + stack do projecto + nota pessoal produz 3 writes em camadas distintas
- [ ] **E6.6** — Teste: sem `/cwd`, não há writes em cwd_overlay

### Acceptance criteria

1. Nenhum dado pessoal (nome, preferências) é escrito em `project_team`.
2. Sem `/cwd` activo, extractor não cria directório `cwd_overlay`.
3. Factos ambíguos ou com PII vão para `user_global` ou são descartados.

---

## E7 — Dream/consolidação escopada por camada

**Prioridade:** P1 · **Pré-condição:** E6 · **Esforço estimado:** 2–3h  
**Package:** `internal/dream/`

### Objetivo

Separar os alvos de consolidação (dream) para que cada camada seja consolidada de forma independente, sem misturar dados entre escopos ou utilizadores.

### Tasks

- [ ] **E7.1** — Dream `user_global` consolida apenas `~/.aurelia/users/<id>/memory/` do utilizador alvo
- [ ] **E7.2** — Dream `topic` consolida apenas `~/.aurelia/topics/<chat>/<thread>/` do `ConversationKey` alvo
- [ ] **E7.3** — Dream `cwd_overlay` consolida apenas `~/.aurelia/topics/<chat>/<thread>/cwd_overlay/` quando `/cwd` estiver activo no tópico
- [ ] **E7.4** — Dream `project_team` consolida apenas `~/.aurelia/projects/<slug>/team/` e nunca inclui factos de USER
- [ ] **E7.5** — Dream usa `CapabilityProfile=edit_project` sem `Bash` (alinhado com spec)
- [ ] **E7.6** — Teste: dream de user A não altera ficheiros de user B
- [ ] **E7.7** — Teste: dream de `cwd_overlay` do tópico X não altera o tópico Y

### Acceptance criteria

1. Rodar dream para user A com user B presente no sistema não altera nenhum ficheiro de B.
2. Dream de `project_team` nunca inclui `USER.md` ou preferências pessoais.
3. Dream sem `/cwd` activo no tópico não cria nem altera `cwd_overlay`.

---

## E8 — Migração explícita para novo layout

**Prioridade:** P1 · **Pré-condição:** E2, E3 · **Esforço estimado:** 3–4h  
**Package:** `cmd/aurelia/`

### Objetivo

Desenhar e implementar o comando de migração do legado para o novo layout, sem recriar `users/<id>/projects/<slug>/memory/`. O layout destino deve ser o canónico do novo modelo.

### Tasks

- [ ] **E8.1** — Mapear o que existe actualmente no deployment single-user que precisa de ser movido:
  - `~/.aurelia/memory/` (excl. personas globais) → `users/<id>/memory/`
  - `~/.aurelia/memory/personas/USER.md` → `users/<id>/personas/USER.md`
  - `~/.aurelia/projects/<slug>/` (memória de projecto partilhável) → permanece em `projects/<slug>/team/` (sem mover)
  - Não existe destino `users/<id>/projects/` no novo modelo
- [ ] **E8.2** — Implementar `--dry-run` que mostra todos os moves/copies sem alterar nada
- [ ] **E8.3** — Migração em duas fases: copy → verify → delete original (nunca move atómico directo)
- [ ] **E8.4** — Marker de migração `~/.aurelia/.context-memory-migrated` (distinto do marker multi-user) com timestamp e `target_id`
- [ ] **E8.5** — Detecção de marker existente → abortar com mensagem clara (idempotente)
- [ ] **E8.6** — Testes: migração idempotente (2 runs sem dano); `--dry-run` não altera ficheiros; marker criado apenas quando tudo OK

### Acceptance criteria

1. Após migração, não existe `users/<id>/projects/` no filesystem.
2. `~/.aurelia/projects/<slug>/team/` permanece inalterado.
3. `--dry-run` não altera nenhum ficheiro.
4. Segunda execução com marker presente aborta sem erros.

---

## E9 — Testes de isolamento e regressão

**Prioridade:** P1 · **Pré-condição:** E4, E5, E6, E7 · **Esforço estimado:** 3–4h

### Objetivo

Cobrir os cenários de isolamento definidos nos success criteria da spec.

### Cenários obrigatórios

- [ ] **E9.1** — Dois tópicos com `/cwd` distintos têm `cwd_overlay` completamente independentes (sem partilha de conteúdo)
- [ ] **E9.2** — Mesmo tópico sem `/cwd`: apenas user global + topic memory no prompt; zero referências a cwd_overlay ou project team
- [ ] **E9.3** — Dois utilizadores no mesmo tópico: partilham topic memory e project team, mas têm user global e cwd_overlay independentes
- [ ] **E9.4** — Dream de user A não altera ficheiros de user B (isolamento de consolidação)
- [ ] **E9.5** — Nenhum path calculado usa `scanForProject` ou travessia automática (grep automático no CI)
- [ ] **E9.6** — Wiki/MCP pode consumir o mesmo layout sem bypass do isolamento (teste de contrato de paths)
- [ ] **E9.7** — Migração: estado single-user legado migra correctamente; segundo run é no-op
- [ ] **E9.8** — `go build ./... && go vet ./... && go test ./...` limpo

### Acceptance criteria

Todos os cenários E9.1–E9.8 passam no CI sem flags `// nolint`.

---

## E10 — Observabilidade mínima das camadas de memória

**Prioridade:** P2 · **Pré-condição:** E4, E5 · **Esforço estimado:** 1–2h  
**Package:** `internal/pipeline/`, `internal/memoryux/`

### Objetivo

Tornar visível quais camadas foram carregadas em cada turno para facilitar diagnóstico e debugging.

### Tasks

- [ ] **E10.1** — Adicionar `ContextBudgetReport` mínimo (alinhado com continuity-engine spec): lista de camadas com `included bool`, `chars int`, `reason string`
- [ ] **E10.2** — Log por turno com camadas activas: `user_global`, `topic`, `cwd_overlay` (se activo), `project_team` (se activo) e tamanho em caracteres de cada uma
- [ ] **E10.3** — `/memory status` (futuro) ou `/status` mostra camadas activas e último checkout
- [ ] **E10.4** — Nudge/dream regista receipt: quantidade de writes, layers tocadas, sugestões de Auto-Skill (sem criar SKILL.md automaticamente)

### Acceptance criteria

1. Log de cada turno inclui lista de camadas com tamanho.
2. Nenhuma camada activa é silenciosa (sem log).

---

## Ordem de execução recomendada

```text
E1 (specs)
  └── E2 (PathResolver)
        └── E3 (remover scanForProject)
              ├── E4 (prompt assembly)
              │     └── E5 (wiring /cwd overlay)
              │           ├── E6 (classificação de writes)
              │           │     └── E7 (dream escopado)
              │           └── E8 (migração)
              └── E9 (testes, paralelo com E6-E8)
                    └── E10 (observabilidade)
```

**Bloqueador de merge:** E3.6 (`go vet` limpo) e E9.8 (`go test` limpo) devem passar antes de qualquer PR desta sprint ser mergeada para main.

---

## Links

- Spec principal: `.specs/features/project-memory/spec.md`
- Specs a patchear: `.specs/features/wiki-memory/spec.md`, `.specs/features/multi-user-profiles/spec.md`
- Depende de: `.specs/features/project-binding/` (✅), `.specs/features/multi-user-profiles/` (✅)
- Desbloqueia: `.specs/features/wiki-memory/` (Sprint F), `.specs/features/learning-nudge/` (Sprint G)
