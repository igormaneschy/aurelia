# Agent Comms — Comunicação Controlada entre Agentes

**Roadmap step:** 9  
**Status:** 🗑️ Descartado — 2026-06-02  
**Depende de:** `.specs/features/agent-orchestration-execution/` para workers, manifest e execução por waves  
**Depende de:** `.specs/features/security-guard-rails/` (✅ done) para capability profiles, payload policy e audit  
**Complementa:** execução de planos conversacionais aprovados e `.specs/features/auto-skills/`

## Problem Statement

O Aurelia usa o PI como motor de execução e já caminha para um modelo onde um plano aprovado é dividido em tarefas e executado por workers especializados. Esse modelo hierárquico é necessário e deve continuar existindo:

```text
Conversational approved plan → Orchestrator → Workers → Validator → Merge/Commit/PR
```

Mas alguns trabalhos melhoram quando agentes especializados conseguem conversar entre si durante a execução. Um worker de backend pode consultar um worker de testes; um worker de implementação pode pedir revisão a um worker de segurança; um agente que conhece uma ferramenta antiga pode responder dúvidas de outro agente que está criando uma skill equivalente.

Hoje esse tipo de colaboração só pode acontecer indiretamente, voltando tudo pelo orquestrador ou pelo usuário. Isso aumenta perda de informação, mistura contextos e reduz o benefício de agentes focados.

Esta feature adiciona um **Agent Bus local e governado pelo daemon Go**: um canal controlado para mensagens entre agentes PI dentro de uma mesma execução, com limites, auditoria e integração ao manifest. O MVP é local ao processo Aurelia; comunicação cross-device/network fica fora de escopo.

## Goals

- [ ] Permitir que workers autorizados troquem mensagens curtas durante uma execução orquestrada
- [ ] Manter Aurelia como coordenador principal; Agent Comms é complementar, não substitui o Orchestrator
- [ ] Registrar toda mensagem agent-to-agent no `ExecutionManifest`
- [ ] Impor limites de custo e loop: máximo de mensagens, hops, timeout e tamanho de payload
- [ ] Bloquear Agent Comms por padrão; habilitar somente quando o plano/tarefa declarar peers permitidos
- [ ] Garantir que mensagens usam `ExecutionContext` correto: run, task, chat, thread, user, cwd e `CapabilityProfile`
- [ ] Fornecer primitivas simples: listar peers, enviar mensagem, aguardar resposta e consultar resposta
- [ ] Integrar com audit logging e security policy antes de qualquer uso com dados sensíveis
- [ ] Manter progresso Telegram resumido, sem despejar cada mensagem interna no chat

## Out of Scope

- Comunicação entre máquinas, servidores ou redes externas no MVP
- Agentes externos entrando livremente em uma pool
- Transferência de dados de produção/PII entre ambientes
- Protocolo público de agent federation
- Substituir o fluxo de orquestração hierárquico atual
- Worker-to-worker merge ou alteração direta no worktree de outro worker
- Chat aberto e ilimitado entre agentes
- Expor todas as mensagens agent-to-agent ao usuário final por padrão

---

## User Stories

### P1: Agent Bus local por execução ⭐ MVP

**User Story:** Como Aurelia, quero criar um canal local por execução para que workers autorizados possam trocar informações sem sair do controle do daemon.

**Why P1:** O canal precisa ser escopado por run para evitar vazamento entre tarefas, chats, usuários ou projetos.

**Acceptance Criteria:**

1. WHEN uma execução orquestrada começa THEN Aurelia SHALL criar um `AgentBus` associado ao `ExecutionContext.RunID`.
2. WHEN a execução termina THEN o bus SHALL ser fechado e novas mensagens para aquele `RunID` SHALL ser rejeitadas.
3. WHEN um worker tenta usar Agent Comms sem bus ativo THEN a chamada SHALL falhar com erro claro.
4. WHEN um worker envia mensagem THEN ela SHALL conter `run_id`, `from_task_id`, `to_task_id`, `message_id`, timestamp e payload textual.
5. WHEN uma mensagem é registrada THEN ela SHALL aparecer no `ExecutionManifest` como evento interno.

**Independent Test:** Criar execução fake com dois tasks, enviar mensagem T1 → T2, finalizar run e verificar que nova mensagem é rejeitada.

---

### P1: Peers explícitos e deny-by-default ⭐ MVP

**User Story:** Como operador, quero que um worker só possa falar com peers explicitamente permitidos, para evitar conversas inesperadas e loops.

**Why P1:** Agentes conversando livremente aumentam custo e risco. O default deve ser seguro.

**Acceptance Criteria:**

1. WHEN um `Task` não declara peers THEN Agent Comms SHALL estar desabilitado para esse task.
2. WHEN um `Task` declara peers THEN ele SHALL poder enviar mensagens somente para os `task_id`s listados.
3. WHEN um worker tenta enviar para peer não autorizado THEN a mensagem SHALL ser rejeitada e auditada.
4. WHEN o plano declara um peer inexistente THEN preflight do plano SHALL retornar erro ou warning antes de executar workers.
5. WHEN um plano conversacional aprovado sugere colaboração THEN ele SHALL declarar peers de forma explícita no plano JSON.
6. WHEN uma task tem `CapabilityProfile=read_only` THEN Agent Comms SHALL continuar permitido apenas para mensagens; não concede novas tools.

**Independent Test:** T1 permite peer T2; envio T1 → T2 passa, envio T1 → T3 falha.

---

### P1: Primitivas simples para colaboração ⭐ MVP

**User Story:** Como worker, quero conseguir listar colegas permitidos, enviar uma pergunta e aguardar uma resposta curta.

**Why P1:** O valor vem de um contrato pequeno e previsível, não de um chat irrestrito.

**Acceptance Criteria:**

1. WHEN worker pede peers THEN recebe apenas peers autorizados para sua task.
2. WHEN worker envia mensagem THEN recebe um `message_id` rastreável.
3. WHEN worker aguarda resposta THEN o await SHALL respeitar timeout configurado.
4. WHEN timeout expira THEN o worker recebe resposta explícita de timeout e deve seguir sem bloquear a execução inteira.
5. WHEN resposta chega depois do timeout THEN ela SHALL ser registrada, mas não destravar a tentativa já expirada.

**Independent Test:** Fake workers trocam pergunta/resposta; outro teste segura a resposta e verifica timeout sem deadlock.

---

### P1: Limites anti-loop e orçamento ⭐ MVP

**User Story:** Como operador, quero limites rígidos para que agentes não entrem em conversa infinita nem queimem tokens sem controle.

**Why P1:** Agent-to-agent comms aumenta custo de forma linear com número de agentes e número de mensagens.

**Acceptance Criteria:**

1. WHEN uma run excede `MaxPeerMessagesPerRun` THEN novas mensagens SHALL ser rejeitadas.
2. WHEN uma task excede `MaxPeerMessagesPerTask` THEN novas mensagens dessa task SHALL ser rejeitadas.
3. WHEN uma cadeia excede `MaxPeerHops` THEN o bus SHALL rejeitar a próxima mensagem com erro de loop budget.
4. WHEN payload excede `MaxPeerMessageBytes` THEN ele SHALL ser rejeitado antes de chegar ao peer.
5. WHEN qualquer limite é atingido THEN o manifest SHALL registrar o motivo.

**Independent Test:** Configurar limites baixos e verificar rejeição determinística por run, task, hop e payload.

---

### P1: Auditoria e segurança ⭐ MVP

**User Story:** Como operador, quero saber qual agente falou com qual agente, sobre qual task, e bloquear mensagens inseguras.

**Why P1:** Mensagens internas podem carregar paths, diffs, comandos ou dados sensíveis. Sem audit, não há forense.

**Acceptance Criteria:**

1. WHEN mensagem agent-to-agent é enviada THEN `slog` SHALL registrar evento estruturado com `run_id`, `from_task_id`, `to_task_id`, `chat_id`, `thread_id`, `user_id` e tamanho do payload.
2. WHEN security policy estiver configurada THEN ela SHALL validar payload antes da entrega.
3. WHEN payload aparenta conter segredo ou PII por heurística básica THEN mensagem SHALL ser rejeitada no MVP.
4. WHEN mensagem é rejeitada por policy THEN o sender recebe erro seguro, sem ecoar o conteúdo sensível.
5. WHEN audit logger falha THEN execução SHALL continuar, mas o manifest SHALL registrar warning.

**Independent Test:** Enviar payload com `api_key=...`; policy rejeita, audit registra sem vazar valor completo.

---

### P2: Uso por validator/reviewer especializado

**User Story:** Como Aurelia, quero que um worker peça revisão curta a um agente especializado antes de finalizar mudanças arriscadas.

**Acceptance Criteria:**

1. WHEN uma task declara peer `security-review` THEN worker pode enviar resumo/diff relevante para revisão.
2. WHEN reviewer responde com issues THEN worker SHALL considerar feedback antes de finalizar tentativa.
3. WHEN reviewer não responde no timeout THEN worker SHALL prosseguir e registrar que revisão peer não ocorreu.
4. WHEN validação final roda THEN ela continua obrigatória; peer review não substitui `Validate`.

---

### P2: Feature parity e criação de skills colaborativa

**User Story:** Como usuário, quero que um agente que conhece uma ferramenta existente ajude outro agente a criar uma skill equivalente para uma ferramenta nova.

**Acceptance Criteria:**

1. WHEN uma task de Auto-Skills declara peer especialista THEN o gerador pode fazer perguntas sobre comandos, inputs, outputs e edge cases.
2. WHEN peer responde THEN essas mensagens SHALL ser incluídas como artefatos da skill capture.
3. WHEN skill é salva THEN o resumo SHALL mencionar se houve colaboração agent-to-agent.

---

### P3: Comunicação cross-device segura

**User Story:** Como operador avançado, quero conectar agentes em ambientes diferentes, como dev e produção, sem vazar dados sensíveis.

**Acceptance Criteria:**

1. Comunicação cross-device SHALL exigir autenticação forte, allowlist, TLS ou canal local confiável.
2. Agente remoto SHALL declarar capabilities e data policy antes de entrar na pool.
3. Mensagens SHALL passar por redaction/policy antes de sair da máquina.
4. PII/data transfer SHALL ser opt-in explícito e auditado.

> P3 é deliberadamente futuro. Não implementar antes do MVP local, security guard-rails e auditoria estarem sólidos.

---

## Edge Cases

- WHEN dois workers tentam responder um ao outro em loop THEN `MaxPeerHops` e `MaxPeerMessages*` SHALL encerrar a conversa.
- WHEN peer falha ou worker termina antes de responder THEN await retorna peer unavailable/timeout.
- WHEN task dependente foi skipped THEN ela não participa do bus.
- WHEN worker tenta enviar diff grande THEN payload é rejeitado ou deve ser resumido antes do envio.
- WHEN uma mensagem é relevante para auditoria mas não para o usuário THEN ela fica no manifest/log, não no Telegram.
- WHEN execução é cancelada pelo usuário THEN bus fecha e todos awaits retornam cancelamento.
- WHEN validation falha após peer review ter aprovado informalmente THEN validation final vence.

---

## Success Criteria

- [ ] Workers só conseguem falar com peers explicitamente permitidos
- [ ] Toda mensagem agent-to-agent aparece no manifest e audit log
- [ ] Limites anti-loop impedem conversa infinita
- [ ] Timeouts não travam a execução inteira
- [ ] Security policy consegue rejeitar payload sensível
- [ ] Validator final continua obrigatório
- [ ] Nenhuma comunicação de rede é criada no MVP
- [ ] `go build ./... && go vet ./... && go test ./...` limpo quando implementado
