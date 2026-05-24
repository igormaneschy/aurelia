# Aurelia — Análise de Arquitectura de Memória e Roadmap
**Versão 2 — Actualizada em 24 de Maio de 2026 após merge de v0.14.0–v0.16.0**

---

## Contexto e Objectivo

Este documento consolida a análise da arquitectura de memória do Aurelia, o estado actual da codebase, as lacunas identificadas e o roadmap de evolução planeado. O objectivo central é fechar o ciclo do Aurelia como **assistente pessoal persistente** — capaz de conhecer o utilizador, o projecto em que está a trabalhar, e evoluir esse conhecimento de forma segura, escopada e auditável.

A análise foi feita a partir da leitura directa da codebase no repositório `igormaneschy/aurelia`, dos ficheiros de spec em `.specs/features/`, e do `ROADMAP.md`. Esta versão incorpora as entregas de v0.14.0 (Observability), v0.15.0 (Session Lifecycle) e v0.16.0 (Close Orchestration Cycle), que foram concluídas entre a análise inicial e esta revisão.

---

## Estado Actual da Arquitectura de Memória

### As 4 Layers

O sistema de memória opera com quatro camadas distintas, definidas em `internal/pipeline/` e geridas pelo `safeMemoryWriter`:

| Layer | Scope | Path actual (codebase) | Bloqueio personas |
|---|---|---|---|
| `global` | Todo o utilizador | `~/.aurelia/memory/` | ✅ |
| `topic` | Por thread/conversa | `~/.aurelia/topics/chat_<id>/thread_<id>/` | ✅ |
| `project` | Por repositório/cwd | `ConversationProjectMemoryDir(cwd, chatID, threadID)` | ❌ |
| `team` | Partilhado por projecto | `ProjectTeamMemoryDir(cwd)` | ❌ |

As layers `project` e `team` não precisam do bloqueio de personas porque vivem fora do directório `~/.aurelia/memory/personas/` por natureza. O `memoryux.Service` (novo em v0.16.x) já expõe estas layers com introspection (`Status()`) e escrita de checkpoints com prioridade `project > topic > global`.

### Pipeline de Escrita — `safeMemoryWriter.applyOne`

O fluxo de escrita executa **11 etapas de validação antes de gravar**:

1. Sanitização de título e facts (`sanitizeTitle`, `sanitizeFact`)
2. Limite de facts por ficheiro + deduplicação
3. Validação da layer (apenas as 4 permitidas)
4. Validação do filename (`.md`, sem separadores, sem `..`, sem ponto inicial)
5. Resolução do layer target (base dir + containment root + política de personas)
6. Verificação léxica de containment (`isSubDirLexical`) antes de qualquer I/O
7. `os.MkdirAll` com permissões `0700`
8. Resolução de symlinks (root, base, parent do target) + recheck de containment
9. Exclusão do directório `personas/` quando aplicável
10. Verificação de symlink residual no target (`checkTargetSymlink` — mitiga H-01)
11. `appendUniqueFacts` — só acrescenta linhas ainda não presentes

Após gravar, actualiza o `MEMORY.md` index com entrada `- [Title](filename.md)`.

### Módulo `internal/memoryux/` (novo em v0.16.x)

Este módulo não existia na análise inicial. Contém:

- **`service.go`** — `Service` com `Status()` e `WriteCheckpoint()`. Introspects todas as layers activas (global, topic, project, team) e selecciona o target de escrita mais específico disponível.
- **`checkpoint.go`** — lógica de escrita de checkpoints de memória por layer.
- **`receipts.go`** e **`receipts_test.go`** — audit trail de escritas, com `LatestReceipt()`. Já cobre o rastreio de "o que foi gravado, quando, em que layer".
- **`format.go`** — formatação de output para UX Telegram.

**Relevância para o Sprint E:** o `Service` já recebe um `*runtime.PathResolver` como dependência. Quando o `PathResolver` ganhar os métodos `User*` no Sprint E, o `memoryux.Service` actualiza automaticamente os paths sem mudanças de interface — a integração é limpa.

### Consolidação (Dream)

O módulo `internal/dream/` contém `consolidation_contract.go`, que define o contrato de consolidação periódica de facts — um processo de "sonho" que compacta/sumariza memórias acumuladas, mantendo o contexto enxuto ao longo do tempo.

### Integração no Pipeline

Em `internal/pipeline/`, o `prompt_builder.go` injeta as memórias no system prompt de cada turn. O `memory_cache.go` serve de buffer RAM para evitar releituras de disco a cada mensagem. O `session_lifecycle.go` (entregue em v0.15.0) gere agora o ciclo de vida completo da sessão, incluindo checkpoints.

---

## O que foi Entregue desde a Análise Inicial

Os sprints B e C foram concluídos após a sessão de análise que gerou a versão anterior deste documento. O roadmap avançou dois sprints:

### Sprint B — Operational Observability ✅ v0.14.0

- `run_id` propagado de Telegram/cron/orchestration até Bridge/runlog/audit
- Logs estruturados com campos estáveis (`run_id`, `request_id`, `chat_id`, `thread_id`, `user_id`, `phase`)
- `run_journal` expandido com provider/model/agent/profile/duração/tokens/custo/fallback/timeout/error_class
- Tabela `run_events` com timeline fase-a-fase
- `/debug` e `aurelia debug` para latest run, run específico, erros recentes e métricas
- Novo módulo `internal/observability/`

### Sprint C — Close Orchestration Cycle ✅ v0.16.0

- `ExecutionContext` com cwd persistente, thread/user/security context
- Git preflight (recusa dirty base, detached HEAD)
- Validation com diff/verify real + retry com feedback
- Merge serial com dependentes skipped
- Update `tasks.md`, commit seguro e PR opcional
- Orphan worktree cleanup no startup
- Artifact collection + manifest
- `ExecutionManifest`, `TaskRecord`, `TaskStatus` enum completo

A **lacuna da orquestração incompleta** identificada na análise inicial (ciclo não fechava, `currentBranch()` hardcoded, thread ID perdido no handoff) foi **fechada**.

---

## As 3 Lacunas que Permanecem em Aberto

Estas são as três lacunas que o documento original identificou como prioritárias para fechar o objectivo de assistente pessoal. **Todas continuam abertas** e confirmadas pelo próprio ROADMAP.

### Lacuna 1 — Memória não escopada por `(user_id, project_slug)`

**Estado actual:** as 4 layers existem e funcionam, mas `runtime.ProjectMemoryDir` e `ConversationProjectMemoryDir` são ainda scoped por `cwd + chatID + threadID`, não por `userID`. O ROADMAP confirma explicitamente: *"User×project private memory movida para Sprint E"* e *"runtime.PathResolver ainda é principalmente cwd/chat/thread-scoped"*.

**Consequência:** dois utilizadores autorizados no mesmo chat, trabalhando no mesmo projecto, partilham memória de projecto. Isto contradiz directamente o modelo de assistente pessoal.

**O que falta fazer (Sprint E):**
- `runtime.PathResolver` ganhar métodos `UserGlobalMemoryDir(userID)`, `UserProjectMemoryDir(userID, slug)`, etc.
- Paths migram de `~/.aurelia/projects/<slug>/` para `~/.aurelia/users/<id>/projects/<slug>/memory/`
- `prompt_builder.go` actualizar o assembly de camadas para usar `TurnContext.UserID`
- `dream/consolidation` actualizar targets de escrita para paths per-user
- `memoryux.Service` recebe os novos métodos do `PathResolver` automaticamente (interface já preparada)

### Lacuna 2 — `project_slug` frágil (ausência de UUID estável de projecto)

**Estado actual:** o `project_slug` é derivado do `cwd` (basename do path). Se o utilizador mover ou renomear o directório do projecto, cria um novo slug e perde toda a memória associada ao projecto anterior. Não existe mapeamento persistente entre projecto e identidade estável.

**Consequência:** a memória de projecto acumulada ao longo de semanas pode ser silenciosamente "perdida" por uma simples renomeação de directório.

**O que falta fazer (pode ser antecipado para antes do Sprint E):**
- Tabela SQLite `projects` com `id UUID`, `slug TEXT`, `root_path TEXT`, `created_at`
- `project_slug` passa a ser o UUID gerado no primeiro bind
- `PathResolver.UserProjectMemoryDir(userID, projectID)` usa o UUID, não o basename
- `ProjectBindingStore` actualizado para persistir e resolver o UUID
- Migração para projectos existentes (gerar UUID para cada entrada existente)

### Lacuna 3 — Ausência de harness de evals para memória

**Estado actual:** existe `receipts_test.go` (17KB) e `service_test.go` (17KB) em `internal/memoryux/`, que cobrem a UX de escrita. No entanto, não existe nenhum harness que teste a qualidade dos facts extraídos pelo LLM — se o que é guardado é relevante, correcto, e não-redundante com o que já existe.

**Consequência:** não é possível medir se o sistema de memória está a melhorar ou a degradar com mudanças no prompt de extracção, no modelo, ou na lógica de consolidação. Evoluções futuras (Wiki, Nudge) ficam sem baseline de qualidade.

**O que falta fazer (pode ser um sprint independente, baixo custo):**
- Fixtures de conversas representativas (10-20 exemplos com expected facts)
- Script de avaliação: corre o pipeline de extracção contra as fixtures, compara com expected
- Métricas simples: precision, recall, taxa de duplicados, taxa de facts irrelevantes
- Integração no CI como gate opcional (não bloqueia, mas reporta regressões)

---

## O Novo Módulo `memoryux` e a sua Relação com o Sprint E

O `memoryux.Service` já foi desenhado com a integração per-user em mente:

```go
// service.go — já recebe PathResolver como dependência
func New(memoryDir string, resolver *runtime.PathResolver) *Service

// checkpointTarget usa resolver para paths de projecto
func (s *Service) projectMemoryDir(cwd string, chatID int64, threadID int) string {
    return s.Resolver.ConversationProjectMemoryDir(cwd, chatID, threadID)
}
```

Quando o Sprint E adicionar `resolver.UserProjectMemoryDir(userID, slug)`, a assinatura do `Service.New()` não muda — apenas os métodos internos do `PathResolver` evoluem. O `memoryux.Service` actualiza automaticamente. Este é um bom sinal de design: a interface está preparada para a migração.

O que **precisa de ser decidido** antes do Sprint E arrancar:
- O `WriteCheckpoint` e o `Status` do `memoryux.Service` vão receber `userID` como parâmetro, ou o `Service` passa a ser construído com `userID` incorporado?
- A opção recomendada é **passar `userID` nos métodos** (não no construtor), para manter o `Service` stateless e reutilizável por múltiplos utilizadores no mesmo processo.

---

## Modelo de Responsabilidades (Contrato Arquitectural)

O principal risco de drift identificado na análise é a falta de um contrato explícito sobre o que pertence ao PI SDK e o que pertence ao Aurelia. O ROADMAP define isto de forma clara na secção "Current Evolution Track", mas não existe um ficheiro de spec auditável que codifique estas regras de forma verificável.

**Divisão actual:**

| Responsabilidade | Dono | Risco se violar |
|---|---|---|
| Modelo, sessão, compaction, context files | PI SDK | Reimplementação → divergência silenciosa |
| Persona, identidade, UX Telegram | Aurelia | Migrar para PI → perda de controlo |
| Memória persistente (4 layers) | Aurelia | Duplicar no PI → estado inconsistente |
| Extracção de facts por LLM | Aurelia (usa PI como motor) | — |
| Consolidação/Dream | Aurelia | — |
| Guard-rails, audit, capability profiles | Aurelia | Duplicar no Bridge → conflito de políticas |
| Wiki/MCP gateway | Aurelia (Sprint F) | — |
| Session lifecycle | PI SDK + Aurelia coordena | Gerir nos dois → loop de reset |

**Recomendação:** criar `.specs/codebase/AGENT_RESPONSIBILITY_MODEL.md` com esta tabela e as regras de arquitectura derivadas do ROADMAP. Este ficheiro serve de referência para code review e impede que futuros agentes (OpenCode, Copilot, Cline) tomem decisões que violem o contrato.

---

## Roadmap Actualizado — Estado Real em 24 Mai 2026

```text
Foundation validada (Security Guard-Rails + Project Binding + Bridge Resilience)
      │
      ▼
Sprint 0: Delegate to PI SDK Native ✅ v0.13.7
      │
      ▼
Sprint A: User Isolation MVP + runtime hardening ✅ v0.13.x
      │
      ▼
Sprint B: Operational Observability ✅ v0.14.0
      │
      ▼
Sprint B+: Session Lifecycle Manager ✅ v0.15.0
      │
      ▼
Sprint C: Close Orchestration Cycle ✅ v0.16.0
      │
      ▼
Sprint D: Plan Mode Architecture 🟡 próximo
      │
      ▼
Sprint E: User-Scoped Project Memory ← LACUNA 1 + LACUNA 2
      │
      ▼
Sprint F: Wiki Memory Gateway ← ponte para transversalidade
      │
      ▼
Sprint G: Learning Nudge
      │
      ▼
Sprint H: Agent Comms
      │
      ▼
Sprint I: Auto-Skills
```

**Itens adicionais recomendados (sem sprint atribuído):**
- `project_uuid` estável — pequeno o suficiente para antecipar para Sprint D ou início do E (Lacuna 2)
- Harness de evals de memória — sprint independente, pode correr em paralelo com Sprint D (Lacuna 3)
- `.specs/codebase/AGENT_RESPONSIBILITY_MODEL.md` — uma tarde de trabalho, elimina risco de drift crescente

---

## Síntese Estratégica

O Aurelia está numa posição sólida: os três sprints de fundação mais críticos para execução autónoma segura (Observability, Session Lifecycle, Orchestration Cycle) foram entregues. O sistema consegue agora executar workflows multi-tarefa com rastreabilidade completa por `run_id`, gitops correcto, e ciclo validar→commit→PR fechado.

O próximo trabalho de alto impacto para o objectivo de **assistente pessoal** é o Sprint E. Mas antes de arrancar, há dois itens pequenos que valem a pena fazer ainda no Sprint D:

1. **`project_uuid` estável** — uma tarde de trabalho que elimina o risco silencioso de perda de memória por renomeação de directório. Quanto mais tarde for feito, mais dados de memória existem para migrar.

2. **`.specs/codebase/AGENT_RESPONSIBILITY_MODEL.md`** — o ROADMAP já tem o contrato escrito em prosa. Codificá-lo num ficheiro de spec auditável previne que os agentes de desenvolvimento (que lêem specs, não o ROADMAP inteiro) tomem decisões que violem a separação PI/Aurelia.

O módulo `memoryux` novo é uma boa notícia: já está desenhado para receber a evolução per-user sem mudanças de interface. A migração do Sprint E está mais preparada do que parecia.

---

*Documento gerado a partir de leitura directa da codebase em `igormaneschy/aurelia@main` (commit `84b5870`) e análise de `.specs/project/ROADMAP.md`. Versão 2 incorpora entregas de v0.14.0–v0.16.0 e o novo módulo `internal/memoryux/`.*
