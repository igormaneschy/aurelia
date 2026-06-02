# Learning Nudge — Scoped Memory Review

**Roadmap step:** 7  
**Status:** 🔴 Draft revisado  
**Depende de:** `.specs/features/multi-user-profiles/`, `.specs/features/project-binding/` (✅ done), `.specs/features/project-memory/`, `.specs/features/security-guard-rails/` (✅ done), Memory Boundary Realignment  
**Complementa:** `.specs/features/auto-skills/`

## Problem Statement

Aurelia aprende hoje por extrações pequenas e frequentes, normalmente baseadas em snippets do turno. Isso perde contexto: tool calls, decisões intermediárias, erros corrigidos e padrões reutilizáveis ficam fora da memória.

Por outro lado, um nudge profundo sem escopo correto pode vazar dados entre usuários, projetos ou tópicos. A versão anterior desta spec também mencionava detalhes antigos de SDK/path e permitia `Bash` para o nudge, o que não combina com o novo modelo de guard-rails.

A nova direção é um **Learning Nudge escopado**: uma revisão periódica em background, executada pelo PI com contexto suficiente, mas limitada por `SessionKey{chat_id, thread_id, user_id}`, `ConversationKey{chat_id, thread_id}`, project binding/effective `cwd` e `CapabilityProfile=edit_project` sem `Bash`.

O nudge não deve depender de uma **Wiki interna** do Aurelia. A Wiki transversal pertence ao PI via `ai-memory` MCP. No MVP, o nudge produz sugestões/updates escopados e só persiste memória operacional local do Aurelia quando isso é necessário para UX, continuidade ou prompt context. Escrita em Wiki transversal só pode ser adicionada depois de um caminho PI/`ai-memory` explicitamente configurado e verificado.

## Goals

- [ ] Rodar revisão periódica em background a cada N turns ou em eventos relevantes
- [ ] Usar transcript escopado por `SessionKey`, sem ler sessões de outros usuários
- [ ] Classificar memórias nas camadas de `project-memory`
- [ ] Produzir sugestões/updates escopados para memória operacional, com receipts/audit locais
- [ ] Redigir secrets antes de enviar transcript ao nudge
- [ ] Usar `CapabilityProfile=edit_project` sem `Bash`
- [ ] Nunca criar skills automaticamente; apenas sugerir candidatos para `auto-skills`
- [ ] Evitar nudges concorrentes por sessão/projeto
- [ ] Registrar custo, duração, writes e camada alvo
- [ ] Não interromper o fluxo principal do usuário
- [ ] Identificar procedimentos reutilizáveis como candidatos a Auto-Skills PI-compatible, sem gravar `SKILL.md` automaticamente

## Out of Scope

- Criação automática de skills sem confirmação
- Leitura direta de arquivos internos de sessão do PI como contrato primário
- Uso de Bash pelo nudge
- Consolidação cross-user
- Full-text search em histórico antigo
- UI completa de revisão/edição de memória
- Implementar gateway Wiki/MCP interno
- Assumir API específica do `ai-memory` sem verificação no PI instalado

---

## Architecture

```text
Pipeline turn complete
  → Recorder appends scoped transcript event
  → Nudge gate evaluates interval/budget/running state
  → Background nudge request to PI
  → PI returns structured scoped memory suggestions/updates
  → Go applies only allowed operational-memory writes or records suggestions
  → Go records receipts/audit
```

### Trigger gates

1. `config.nudge_enabled == true`
2. `turns_since_nudge >= nudge_turns` OR explicit event (`/new`, handoff complete, long run complete)
3. no nudge currently running for same `SessionKey`
4. transcript has enough new material
5. budget/cooldown allows execution

### Transcript source

Preferred: Go-owned scoped transcript recorder.

The recorder captures:

- user text/media summary
- selected agent/model
- effective `cwd`
- assistant final answer
- tool_use/tool_result summaries, redigidos e truncados
- result stats: tokens, cost, duration
- orchestration/agent-comms summaries when present

Do **not** depend on PI internal session file paths for MVP.

---

## User Stories

### P0: Scoped transcript recorder ⭐ MVP

**User Story:** Como sistema, quero registrar contexto suficiente para aprendizado sem vazar dados entre usuários.

**Acceptance Criteria:**

1. WHEN um turno começa THEN recorder SHALL associar eventos a `SessionKey{chat_id, thread_id, user_id}`.
2. WHEN há `cwd` THEN recorder SHALL armazenar project slug derivado, não depender de path bruto para lookup.
3. WHEN tool events chegam THEN recorder SHALL guardar nome da tool e input/output redigidos/truncados.
4. WHEN turno falha/cancela THEN recorder MAY registrar erro, mas não substituir último sucesso capturável de Auto-Skills.
5. WHEN user pede `/forget-me` THEN transcripts pendentes desse user SHALL ser removidos.

**Independent Test:** Dois users no mesmo tópico geram transcripts separados; User B não aparece no nudge de User A.

---

### P0: Redaction antes do nudge ⭐ MVP

**User Story:** Como operador, quero impedir que secrets sejam enviados ao review agent ou gravados em memória.

**Acceptance Criteria:**

1. Redactor SHALL cobrir API keys, bearer tokens, passwords, env assignments e strings high-entropy.
2. Paths sensíveis (`~/.ssh`, `~/.aurelia/config`, `~/.pi`, `.env`) SHALL ser mascarados.
3. Redaction SHALL acontecer antes de montar o prompt do nudge.
4. Se conteúdo continuar suspeito após redaction THEN nudge SHALL abortar fail-closed.
5. Audit SHALL registrar redaction counts, não valores.

**Independent Test:** Transcript com `OPENAI_API_KEY=...` chega ao nudge como `<REDACTED:api_key>`.

---

### P1: Scoped memory review por camadas ⭐ MVP

**User Story:** Como Aurelia, quero transformar conversas em memória útil no escopo correto.

**Acceptance Criteria:**

1. Nudge prompt SHALL listar targets operacionais permitidos: user global, topic memory, cwd_overlay e project team.
2. Nudge SHALL receber guia de classificação da spec `project-memory` e a boundary: Wiki transversal pertence ao PI via `ai-memory`, não ao Aurelia.
3. Nudge SHALL ser instruído a preferir camada privada quando houver dúvida.
4. Nudge SHALL nunca escrever fatos pessoais em team memory.
5. Go SHALL aplicar apenas atualizações permitidas de memória operacional ou registrar sugestões pendentes quando a escrita não for segura.
6. Writes em Wiki transversal SHALL NOT ser implementados no Aurelia sem contrato verificado do PI/`ai-memory`.

**Independent Test:** Transcript com fato pessoal + convenção de projeto resulta em writes nas camadas corretas.

---

### P1: Capability profile mínimo ⭐ MVP

**User Story:** Como operador, quero que o nudge escreva memórias, mas não execute comandos shell.

**Acceptance Criteria:**

1. Nudge request SHALL usar `CapabilityProfile=edit_project`.
2. Active tools SHALL incluir no máximo `Read`, `Grep`, `Glob/Find`, `LS`, `Write`, `Edit`.
3. `Bash` SHALL estar indisponível.
4. `NoUserSettings=true` SHOULD ser usado para evitar extensões/settings pessoais no review interno.
5. Security policy hook SHALL continuar ativo mesmo com `NoUserSettings=true`.

**Independent Test:** Fake bridge recebe request de nudge sem Bash e com security context.

---

### P1: Sugestão de Auto-Skill, não criação automática ⭐ MVP

**User Story:** Como usuário, quero que Aurelia perceba padrões úteis, mas só crie skill quando eu confirmar.

**Acceptance Criteria:**

1. Nudge MAY identificar candidato a skill quando detectar procedimento reutilizável.
2. Nudge SHALL salvar apenas sugestão/resumo em memória operacional ou output estruturado.
3. Nudge SHALL NOT escrever em `users/<id>/skills/` nem em `~/.pi/agent/skills`.
4. `/skill save <slug>` continua sendo o caminho explícito para criar uma skill PI-compatible gerenciada pelo Aurelia.
5. Auto-Skills MAY consumir sugestões do nudge como contexto para gerar `<slug>/SKILL.md` após confirmação do usuário.

---

### P2: Event-based nudge after orchestration

**User Story:** Como Aurelia, quero aprender após execuções longas ou orquestradas sem esperar N turnos.

**Acceptance Criteria:**

1. WHEN orchestration completes THEN nudge MAY run with execution manifest summary.
2. WHEN Agent Comms occurred THEN nudge MAY include peer summary, not raw sensitive payloads.
3. WHEN execution failed THEN nudge MAY save lessons learned/workarounds.
4. Nudge SHALL respect same budget/cooldown gates.

---

## Prompt Requirements

Nudge prompt deve incluir:

- SessionKey e ConversationKey como metadados, não como conteúdo narrativo
- cwd/project slug efetivo
- camadas de memória permitidas e seus paths
- camadas operacionais permitidas e regras de classificação
- transcript redigido/truncado
- instrução para atualizar arquivos existentes, não duplicar
- instrução para não salvar secrets/PII desnecessária
- instrução para não criar skills automaticamente nem escrever em diretórios de skills do Aurelia/PI

---

## Config

```go
type NudgeConfig struct {
    Enabled          bool
    Turns            int
    MinTranscriptLen int
    Cooldown         time.Duration
    MaxTranscriptBytes int
    Model            string
}
```

Defaults sugeridos:

```json
{
  "nudge_enabled": true,
  "nudge_turns": 10,
  "nudge_min_transcript_len": 2000,
  "nudge_max_transcript_bytes": 80000
}
```

---

## Affected Packages

| Package | Change |
|---|---|
| `internal/session/` | Transcript buffer keyed by SessionKey |
| `internal/pipeline/` | Recorder observes turns and tool events |
| `internal/dream/` | Nudge runner, prompt, scoped suggestions and reconciliation |
| `internal/memoryux/` | Operational memory status/receipts used by nudge |
| `internal/security/` | Redaction + capability profile integration |
| `internal/runtime/` | Memory target paths |
| `internal/config/` | Nudge config fields |

---

## Success Criteria

- [ ] Nudge never mixes users in same chat/thread
- [ ] Nudge writes to correct memory layers
- [ ] Nudge does not require an internal Wiki/MCP gateway
- [ ] Nudge runs without Bash
- [ ] Secrets are redacted before PI call
- [ ] Auto-Skills remains explicit user-confirmed flow
- [ ] Background nudge does not block main response
- [ ] `go build ./... && go vet ./... && go test ./...` clean when implemented
