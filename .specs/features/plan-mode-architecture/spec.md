# Plan Mode Architecture

**Status:** 🗑️ Superseded / removed — historical artifact only. Formal Plan Mode removed 2026-05-24. Legacy `aurelia-plan` + Aurelia orchestrator **also removed v0.38.0**. PI SDK owns agentic execution. Do not implement.

**Roadmap step:** 4  
**Depende de:** `.specs/features/multi-user-profiles/` (`TurnContext`, `SessionKey`, `UserGate`, comandos por usuário)  
**Depende de:** `.specs/features/project-binding/` (✅ done — `/cwd` persistente)  
**Depende de:** `.specs/features/agent-orchestration-execution/` (`ExecutionContext`, handoff seguro, executor fail-closed)  
**Desbloqueia:** Auto-Skills (planejamento estruturado)

## Problem Statement

Aurelia já tem partes que parecem Plan Mode, mas hoje elas formam um atalho implícito, não um modo operacional:

- `internal/pipeline/planning_intent.go` usa keywords amplas e inclui termos de aprovação/execução (`aprovado`, `pode fazer`, `execute`) no mesmo detector de planejamento.
- `internal/pipeline/prompt_builder.go` injeta `orchestrator.BuildOrchestratorPrompt` quando a mensagem parece planejamento e há `cwd`, sem perguntar ao usuário se ele quer entrar em modo plano.
- `internal/pipeline/pipeline.go` detecta `aurelia-plan` no texto final e chama `ExecuteApprovedPlan`, mas a integração atual não carrega `threadID`, `cwd` efetivo, usuário dono nem estado de planejamento.
- O event stream já traz `tool_use.input` pelo bridge, mas `ProcessBridgeEvents` só usa o nome da ferramenta para progresso; paths de `Write`/`Edit` não são rastreados.
- Com a revisão de User Isolation, estado pessoal deve ser indexado por `SessionKey{chatID, threadID, userID}`, enquanto `/cwd` e memória de tópico continuam compartilhados por conversa (`ConversationKey`).

O problema a resolver é transformar planejamento em um modo explícito, persistente, retomável e seguro, sem deixar a LLM disparar execução por acidente só porque uma keyword apareceu.

## Goals

- [ ] Plan Mode tem estado persistente em SQLite por `SessionKey{chat_id, thread_id, user_id}`
- [ ] `UserGate` roda antes de `/plan`, `/plan status`, `/plan list`, `/plan cancel` e antes de qualquer state lookup por usuário
- [ ] Intent heuristic apenas **oferece** Plan Mode com throttle; nunca injeta prompt de orquestração silenciosamente
- [ ] Context discovery roda ao entrar no modo e quando project binding (`/cwd`) muda, respeitando padrões existentes do projeto
- [ ] System prompt de Plan Mode injeta `ProjectContext`, fase, artefatos rastreados e regras de handoff
- [ ] LLM pode materializar artefatos via `Write`/`Edit`; Go observa, valida caminho de forma segura e registra múltiplos artefatos por fase
- [ ] Handoff para o executor usa o `ExecutionContext` da spec de orquestração e só limpa o estado depois que o executor aceita o plano
- [ ] Plan Mode pode declarar `capability_profile` por task e `peers` explícitos quando colaboração entre agentes for útil
- [ ] Plan Mode respeita guard-rails: materialização usa profile compatível e não pede tools fora do contexto aprovado
- [ ] Resume funciona após restart do daemon ou reset de sessão do bridge
- [ ] Comandos de status/cancel/list funcionam por usuário sem vazar estado entre pessoas no mesmo chat

## Out of Scope

- Templates rígidos por projeto. A LLM propõe estrutura com base no contexto detectado.
- Multi-feature paralelo no mesmo `SessionKey`; uma sessão ativa por usuário/conversa é suficiente para o MVP.
- Editor inline de spec/design/tasks via Telegram.
- Criação automática de skills a partir do plano; coberto por `auto-skills`.
- Executor paralelo completo; coberto por `agent-orchestration-execution`.

---

## User Stories

### P0: Estado isolado e roteamento seguro ⭐ MVP

**User Story:** Como usuário em um chat compartilhado, quero que meu Plan Mode não misture estado com outro usuário do mesmo tópico.

**Why P0:** Sem isso, Plan Mode vira vazamento de contexto pessoal e quebra a revisão de User Isolation.

**Acceptance Criteria:**

1. WHEN qualquer comando `/plan*` chega THEN Aurelia SHALL passar pelo `UserGate` antes de carregar/criar estado.
2. WHEN estado é salvo THEN a chave SHALL ser `SessionKey{chat_id, thread_id, user_id}`.
3. WHEN `cwd` é lido THEN Aurelia SHALL usar o `ConversationKey{chat_id, thread_id}` resolvido pela sessão/conversa, não um cwd privado por usuário.
4. WHEN dois usuários no mesmo tópico iniciam `/plan` THEN cada um SHALL ver somente seu próprio estado em `/plan status`.
5. WHEN usuário não está onboarding-complete THEN `/plan` SHALL recusar com a mesma UX de comandos protegidos.

**Independent Test:** Dois `user_id` diferentes no mesmo `chat_id/thread_id` criam estados distintos; `/plan status` retorna o estado correto para cada um.

---

### P0: Entrar, oferecer e sair do Plan Mode ⭐ MVP

**User Story:** Como usuário, quero ativar Plan Mode de forma explícita e receber uma sugestão quando Aurelia perceber uma tarefa grande, sem que o modo se imponha.

**Why P0:** O comportamento atual injeta orquestração em conversas normais e mistura intenção de planejar com intenção de executar.

**Acceptance Criteria:**

1. WHEN user manda `/plan` com `cwd` resolvido THEN Aurelia SHALL criar `planning_state` com `phase=specify`, `status=active`, `SessionKey`, `cwd` e `ProjectContext`.
2. WHEN user manda `/plan` sem project binding/effective cwd THEN Aurelia SHALL pedir `/cwd <path>` antes de criar estado.
3. WHEN `looksLikePlanningIntent` dispara e não há estado ativo THEN Aurelia SHALL oferecer Plan Mode uma vez por janela de TTL, sem alterar o system prompt daquele turno.
4. WHEN user aceita a oferta (`/plan`, botão futuro, ou frase explícita configurada) THEN Aurelia SHALL criar estado.
5. WHEN user manda `/plan cancel` THEN Aurelia SHALL deletar o estado e preservar artefatos físicos.
6. WHEN user manda `/cancel` e existe execução ativa THEN `/cancel` SHALL continuar significando cancelar execução; Plan Mode usa `/plan cancel` para evitar ambiguidade.

**Independent Test:** Mensagem com keyword cria oferta, não injeta `BuildPlanningPrompt` nem `BuildOrchestratorPrompt`; `/plan` cria row; `/plan cancel` remove row.

---

### P1: Context discovery ⭐ MVP

**User Story:** Como usuário, quando entro em Plan Mode num projeto, Aurelia deve descobrir o método de planejamento já usado e propor segui-lo.

**Why P1:** Evita criar uma estrutura paralela ou impor TLC onde o projeto já usa RFC/ADR/outro fluxo.

**Acceptance Criteria:**

1. WHEN Plan Mode inicia THEN Aurelia SHALL executar discovery stat-only no `cwd`:
   - `.git/`
   - `CLAUDE.md`, `AGENTS.md`, `README.md`
   - `.specs/features/`, `docs/rfc/`, `rfcs/`, `docs/adr/`, `adr/`, `planning/`
   - `go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`
2. WHEN múltiplos layouts coexistem THEN `ProjectContext` SHALL registrar todos e marcar `NeedsLayoutChoice=true`.
3. WHEN `NeedsLayoutChoice=true` THEN o prompt SHALL pedir escolha do usuário antes de materializar novos arquivos.
4. WHEN discovery completa THEN o resultado SHALL ser salvo em `planning_state.project_ctx`.
5. WHEN `cwd` muda durante estado ativo THEN Aurelia SHALL avisar, reexecutar discovery e registrar o novo `cwd`.

**Independent Test:** Discovery em projeto TLC, RFC, ADR, múltiplos layouts e diretório vazio produz `ProjectContext` determinístico.

---

### P1: Materialização observada, não imposta ⭐ MVP

**User Story:** Como dono do sistema, quero que a LLM decida se e onde gravar artefatos, enquanto Aurelia rastreia o que foi realmente escrito.

**Why P1:** A decisão é semântica, mas o estado operacional precisa ser confiável.

**Acceptance Criteria:**

1. WHEN system prompt é montado em Plan Mode THEN SHALL informar contexto detectado, caminhos sugeridos, artefatos já materializados e regras de materialização.
2. WHEN LLM chama `Write`, `Edit` ou `MultiEdit` THEN Go SHALL observar `bridge.Event.Input`, extrair paths e registrar em `planning_state.materialized`.
3. WHEN path é relativo THEN Aurelia SHALL resolvê-lo contra o `cwd` efetivo do turno.
4. WHEN path está fora do `cwd` THEN Aurelia SHALL registrar com `inside_cwd=false` e logar aviso, sem bloquear.
5. WHEN path parece dentro do `cwd` THEN Aurelia SHALL validar usando `filepath.Rel`/clean path, não prefixo de string.
6. WHEN tool call termina THEN Aurelia SHALL reconciliar por `os.Stat` quando possível para diferenciar intenção de escrita de arquivo confirmado.
7. WHEN múltiplos arquivos pertencem à mesma fase THEN `materialized` SHALL manter lista, não sobrescrever o anterior.

**Independent Test:** Eventos `Write`/`Edit` com paths absoluto, relativo, fora do cwd e múltiplos arquivos por fase atualizam o estado corretamente.

---

### P1: Resume nativo ⭐ MVP

**User Story:** Como usuário, depois de horas ou de um restart, quero continuar o planejamento sem repetir contexto.

**Why P1:** Plan Mode só é um modo se sobreviver ao processo e à sessão do bridge.

**Acceptance Criteria:**

1. WHEN daemon reinicia e existe `planning_state` ativa THEN a próxima mensagem nesse `SessionKey` SHALL carregar state e injetar `BuildPlanningPrompt`.
2. WHEN sessão do bridge é resetada por token threshold THEN `planning_state` SHALL ser preservada.
3. WHEN user pergunta `/plan status` THEN Aurelia SHALL mostrar fase, `cwd`, layout detectado, artefatos, idade e último erro de handoff se houver.
4. WHEN `planning_state.updated_at > 30 dias` THEN boot cleanup SHALL remover a row.
5. WHEN store detecta conflito de versão em update THEN Aurelia SHALL recarregar state e evitar last-writer silencioso.

**Independent Test:** Criar state, reiniciar serviço com mesmo SQLite, mandar nova mensagem; prompt contém estado salvo.

---

### P1: Handoff seguro para o executor ⭐ MVP

**User Story:** Como usuário, quando aprovo o plano, Aurelia deve passar para execução sem perder contexto e sem limpar estado antes da hora.

**Why P1:** O handoff é onde Plan Mode e Orchestration se encontram; se falha, o usuário perde o plano aprovado.

**Acceptance Criteria:**

1. WHEN user manifesta aprovação final (`/execute`, “aprovado”, “pode executar”, “manda ver”) durante Plan Mode THEN state SHALL ir para `phase=awaiting_exec`.
2. WHEN a LLM emite `aurelia-plan` sem aprovação explícita ou sem `phase=awaiting_exec` THEN Aurelia SHALL pedir confirmação em vez de executar.
3. WHEN `aurelia-plan` parseia com sucesso THEN pipeline SHALL montar `ExecutionContext` com `chatID`, `threadID`, `userID`, `cwd`, `feature`, `sourcePlanPath/materialized` e security context.
4. WHEN executor/preflight aceita o plano THEN `planning_state` SHALL ser deletada ou marcada `handoff_started` com cleanup posterior.
5. WHEN parse, preflight ou aceite do executor falha THEN `planning_state` SHALL permanecer com `last_handoff_error`.
6. WHEN handoff acontece THEN a resposta ao usuário SHALL explicar que a execução começou e referenciar o plano/fase, sem repetir JSON bruto.
7. WHEN o plano contém tasks executáveis THEN cada task SHOULD declarar `capability_profile` (`read_only`, `edit_project`, `execute_safe`) conforme necessidade.
8. WHEN o plano contém colaboração entre tasks THEN cada task SHALL declarar `peers` por `task_id`; peers não concedem tools adicionais.

**Independent Test:** Plano válido com executor mock aceito remove state; plano inválido ou executor recusado preserva state e registra erro.

---

### P2: Comandos auxiliares

**User Story:** Como usuário, quero inspecionar e gerenciar meus planos ativos sem depender da conversa principal.

**Acceptance Criteria:**

1. WHEN user manda `/plan status` com state ativa THEN SHALL mostrar fase, `cwd`, layout, artefatos, idade e status de handoff.
2. WHEN user manda `/plan status` sem state ativa THEN SHALL orientar `/plan`.
3. WHEN user manda `/plan list` em DM THEN SHALL listar sessões ativas do próprio `user_id`.
4. WHEN user manda `/plan reset` THEN SHALL pedir confirmação antes de descartar state existente.
5. WHEN user manda `/plan cancel` THEN SHALL sair do modo e listar artefatos preservados.

---

### P3: Materialização sob demanda

**User Story:** Como usuário, quero poder dizer “salva isso como spec” para forçar materialização quando a conversa já está madura.

**Acceptance Criteria:**

1. WHEN user pede “salva isso”, “grava como arquivo”, “materializa” durante Plan Mode THEN prompt SHALL orientar a LLM a usar `Write`/`Edit` no layout escolhido.
2. WHEN não há layout definido THEN Aurelia SHALL pedir escolha antes de materializar.
3. WHEN materialização falha ou não é confirmada por stat THEN `/plan status` SHALL mostrar artefato como `pending_confirmation`.

---

## Edge Cases

- WHEN bridge tool event não contém `input` parseável THEN observer SHALL registrar warning e não atualizar `materialized`.
- WHEN LLM emite `aurelia-plan` fora do Plan Mode THEN comportamento legado só poderá executar se a spec de orquestração considerar seguro; Plan Mode não deve criar state retroativo.
- WHEN project binding muda durante Plan Mode THEN state deve registrar o novo cwd, reexecutar discovery e preservar artefatos antigos com seus paths absolutos.
- WHEN artefato materializado é deletado manualmente THEN `/plan status` SHALL mostrar como missing depois de reconciliar.
- WHEN dois turnos concorrentes tentam salvar state THEN versionamento otimista SHALL evitar sobrescrita silenciosa.
- WHEN discovery encontra `.specs/features/` vazia THEN layout TLC ainda conta como detectado.
- WHEN `CLAUDE.md`/`AGENTS.md` conflitam com TLC THEN prompt deve instruir a LLM a seguir os arquivos do projeto.
- WHEN garbage collection remove state antigo THEN arquivos físicos não são removidos.

---

## Success Criteria

- [ ] `/plan`, `/plan status`, `/plan cancel` e `/plan list` funcionam por usuário
- [ ] Keywords oferecem Plan Mode, mas não injetam orquestração nem executam
- [ ] Discovery determinístico cobre TLC, RFC, ADR, múltiplos layouts e diretório vazio
- [ ] Observer rastreia paths de `Write`/`Edit`/`MultiEdit` com resolução segura
- [ ] Resume funciona após daemon restart e reset de sessão
- [ ] Handoff preserva state em falha e limpa somente após aceite do executor
- [ ] Conversa normal sem Plan Mode não sofre regressão
- [ ] `go build ./... && go vet ./... && go test ./...` limpo quando a implementação sair
