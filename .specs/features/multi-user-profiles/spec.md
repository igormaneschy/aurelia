# User Isolation

**Roadmap step:** 1 — P0 Foundation  
**Status:** ✅ MVP + runtime hardening auditados em 2026-05-22  
**Depende de:** Nada (fundação do roadmap)  
**Desbloqueia:** Orchestration Cycle, Plan Mode, Project Memory, Auto-Skills

> Working directory name remains `multi-user-profiles` for compatibility with existing references, but the product framing is **User Isolation**: isolate data for each whitelisted Telegram `user_id`, not turn Aurelia into a multi-tenant platform.
>
> Implementation note 2026-05-22: the current audited task/status summary lives in `tasks.md`. Session/runtime isolation is closed; user×project private memory moved to Sprint E (`.specs/features/project-memory/`).

## Problem Statement

Aurelia já aceita uma whitelist de `user_id`s (`TelegramAllowedUserIDs`) e bloqueia qualquer outro sender — auth está resolvida. Mas **internamente** a arquitetura trata o estado como se houvesse um único usuário:

- `~/.aurelia/memory/` é uma pasta global. Se você autorizar dois `user_id`s, eles compartilham fatos pessoais, preferências, IDENTITY/SOUL/USER prompts.
- `USER.md` (a parte da persona que descreve quem é o user) é única — Aurelia "conhece" só uma pessoa.
- `cron_jobs` já tem `owner_user_id` no schema atual, mas a semântica precisa ser normalizada: todo job deve ter owner real, list/cancel precisam filtrar por sender, e deployments antigos precisam de backfill/migração.
- O cron CLI ainda consegue criar jobs sem owner explícito; como a LLM é instruída a usar CLI para agendar, isso precisa herdar o user dono do turno/job.
- Project memory (em `~/.aurelia/projects/`) é global por projeto — se dois users trabalham no mesmo repo, suas anotações se sobrescrevem.
- Dream/nudge consolida pra um destino único.
- Usage tracking, active runs e comandos de controle (`/new`, `/usage`, status/cancelamento) ainda são por chat/thread, não por user. Em grupos, um user pode afetar ou observar o trabalho do outro.
- `SessionKey` hoje também carrega CWD. Se simplesmente adicionarmos `user_id` nessa chave, `/cwd` deixaria de ser uma propriedade compartilhada do tópico/grupo e viraria invisivelmente per-user.

Hoje funciona porque a whitelist tipicamente tem só você. Mas se você adicionar uma segunda pessoa autorizada (ou se você mesmo tiver múltiplos `user_id` — DM × grupo onde você é só o membro), o estado vaza.

Esta spec resolve **isolamento interno de dados entre os users já autorizados pela whitelist** — não abre o bot pra qualquer um do Telegram. A whitelist continua sendo o gate.

## Goals — status auditado

- [x] Memória pessoal base (fatos, `USER.md`, dream output) isolada por `user_id`.
- [ ] ~~Project memory privada por `(user_id, project_slug)`~~ — **reformulada para `cwd_overlay` por tópico** (Sprint E, `.specs/features/project-memory/`). A memória privada de utilizador em contexto de trabalho vive em `topics/.../cwd_overlay/`, escopada por `(chat_id, thread_id)`, não por `(user_id, project_slug)`.
- [x] IDENTITY.md e SOUL.md continuam globais — Aurelia é uma só.
- [x] Cron jobs têm `owner_user_id` normalizado; user só vê/cancela seus próprios.

> **Nota sobre delegate-to-pi-sdk:** Com a simplificação do session store, `SessionKey` mapeia para `sessionFile` (path da sessão PI no disco) em vez de `sessionID` (string opaca em memória). O isolamento por `user_id` continua válido — cada usuário tem seu próprio `sessionFile`. O store simplificado mantém `SessionKey{chat_id, thread_id, user_id}`.

- [x] User novo (autorizado mas sem profile) recebe onboarding conversacional via Telegram.
- [x] Onboarding/user gate roda antes de comandos e antes do pipeline LLM, depois apenas da whitelist e do bootstrap inicial do deployment.
- [x] Migração do layout single-user legado para layout isolado por user é um comando CLI explícito e idempotente.
- [x] Sessão LLM, usage tracking, active run e fila são isolados por `(chat_id, thread_id, user_id)`.
- [x] CWD vira project binding persistente escopado à conversa `(chat_id, thread_id)`, compartilhado entre users autorizados do mesmo tópico.
- [x] Cron CLI, cron follow-ups e cron criado por agents sempre gravam owner real; nenhum job novo fica com `owner_user_id` vazio.
- [x] Comandos que alteram configuração global do deployment são owner-only.
- [x] Zero regressão esperada para 1 user autorizado após migração.
- [x] Em grupos, cada user mantém **sessão LLM independente** (conversation history, PI Resume, persona) — apenas a memória escrita do tópico (`~/.aurelia/topics/`) continua compartilhada. **⚠️ Breaking change deliberada** vs. comportamento antigo.

## Out of Scope

- Abrir bot pra qualquer Telegram user — whitelist continua sendo o gate
- Multi-tenant (vários donos do bot, vários deployments) — Aurelia continua single-deployment
- Compartilhamento explícito de memória entre users
- UI web pra gerenciar profiles — gestão via Telegram (`/users`) ou CLI
- Quotas/billing por user — pode entrar em P3 futuro, não nesta spec

---

## User Stories

### P0: Separar escopo de sessão e escopo de conversa ⭐

**User Story**: Como user autorizado em um grupo/tópico, quero que minha sessão LLM seja privada, mas que o `/cwd` do tópico continue sendo compartilhado como hoje.

**Why P0**: O código atual usa a mesma chave para sessão e CWD. Adicionar `user_id` nessa chave sem separar os conceitos criaria uma regressão de UX: cada user teria um `/cwd` invisível diferente no mesmo tópico.

**Acceptance Criteria**:

1. WHEN estado de sessão LLM é lido/escrito THEN SHALL usar `SessionKey{chat_id, thread_id, user_id}`
2. WHEN usage tracking, active run e fila são lidos/escritos THEN SHALL usar o mesmo escopo de sessão por user
3. WHEN `/cwd` é lido/escrito THEN SHALL usar `ConversationKey{chat_id, thread_id}` sem `user_id`
4. WHEN tópico herda CWD do grupo THEN a herança SHALL continuar funcionando por `ConversationKey`
5. WHEN memória de tópico é lida/escrita THEN SHALL usar `~/.aurelia/topics/chat_<id>/thread_<id>/`, compartilhada entre users autorizados do tópico
6. WHEN dois users no mesmo tópico conversam ao mesmo tempo THEN um não SHALL ver, resetar, cancelar ou continuar a sessão LLM do outro

**Independent Test**: User A e user B no mesmo `(chat_id, thread_id)` usam o mesmo `/cwd`, mas geram `SessionKey` diferentes, usage separado e active runs independentes.

---

### P0: User gate antes de comandos ⭐

**User Story**: Como owner, quero que qualquer user autorizado sem profile passe por onboarding antes de conseguir criar cron, trocar modelo, rodar pipeline ou executar comandos sensíveis.

**Why P0**: Hoje o roteamento de comandos acontece antes do pipeline. Se onboarding ficar só no pipeline, um user novo consegue atravessar `/cron`, `/model`, `/cwd` etc. sem profile.

**Acceptance Criteria**:

1. WHEN uma mensagem chega THEN whitelist SHALL rodar primeiro
2. WHEN bootstrap inicial do deployment (`IDENTITY/SOUL`) ainda está pendente THEN fluxo existente de bootstrap SHALL ter precedência
3. WHEN sender autorizado não tem `profile.json` THEN UserGate SHALL interceptar a mensagem antes de `MatchCommand` e antes do pipeline
4. WHEN sender está no meio do onboarding THEN UserGate SHALL processar o próximo passo antes de comandos/pipeline
5. WHEN onboarding completa THEN a mensagem original SHALL ser reenfileirada no roteador normal com `TurnContext` completo
6. WHEN `user_id == 0` THEN UserGate SHALL recusar e logar, sem cair em comandos/pipeline

**Independent Test**: User novo envia `/cron list`; Aurelia inicia onboarding em vez de listar cron.

---

### P1: Memória pessoal isolada por user_id ⭐ MVP

**User Story**: Como user autorizado, quero que minha memória (fatos, preferências) seja minha — outro user autorizado não SHALL conseguir ler nem sobrescrever.

**Why P1**: Vazamento de memória pessoal é o pior bug possível quando há mais de um `user_id` autorizado. É o motivo de existir desta spec.

**Acceptance Criteria**:

1. WHEN pipeline processa mensagem THEN SHALL receber `TurnContext` já validado pelo UserGate e propagar `user_id` através de prompt assembly, memory cache, dream e cron helpers
2. WHEN memória pessoal é lida THEN SHALL vir de `~/.aurelia/users/<user_id>/memory/` — nunca de `~/.aurelia/memory/` (que passa a hospedar só personas globais)
3. WHEN memória pessoal é gravada THEN SHALL ir pra `~/.aurelia/users/<user_id>/memory/`
4. WHEN dois users autorizados conversam concorrentemente THEN suas memórias SHALL permanecer isoladas (sem race conditions, sem cache leak)
5. WHEN um teste de integração cria fatos para user A e lê com user B THEN B SHALL NOT enxergar nada de A

**Independent Test**: Test de integração com dois `user_id`s configurados na whitelist. User A escreve fato "favorite color blue". User B pergunta "qual minha cor favorita?" — Aurelia responde "não sei" (não vaza).

---

### P1: USER.md por user; IDENTITY/SOUL globais ⭐ MVP

**User Story**: Como user, quero que minha relação com a Aurelia (como ela me chama, o que sabe sobre mim) seja minha — mas Aurelia em si continua sendo a mesma personalidade pra todos.

**Why P1**: A persona da Aurelia (`IDENTITY.md`, `SOUL.md`) é "quem é Aurelia". Isso não muda por user — é injusto duplicar isso e abrir espaço pra inconsistência. Mas `USER.md` é "quem é o user que está falando com Aurelia" — isso é por user, obviamente.

**Acceptance Criteria**:

1. WHEN persona é montada pra um user THEN SHALL ler `IDENTITY.md` e `SOUL.md` de `~/.aurelia/memory/personas/` (global)
2. WHEN persona é montada pra um user THEN SHALL ler `USER.md` de `~/.aurelia/users/<user_id>/personas/USER.md`
3. WHEN um user não tem `USER.md` THEN persona montada SHALL incluir um stub mínimo (frontmatter com `user_id`) — nunca compartilhar `USER.md` de outro user
4. WHEN onboarding gera USER.md THEN o arquivo SHALL ficar em `~/.aurelia/users/<user_id>/personas/USER.md`
5. WHEN o user edita manualmente seu USER.md THEN próxima conversa SHALL usar a versão editada (cache invalidação por user_id)

**Independent Test**: User A onboard com nome "Alice", user B onboard com nome "Bob". User A manda "como você me chama?" — Aurelia diz "Alice". User B manda a mesma mensagem — Aurelia diz "Bob".

---

### P1: Cron jobs com owner_user_id normalizado ⭐ MVP

**User Story**: Como user, quero criar cron jobs e ter certeza de que só eu posso vê-los e cancelá-los.

**Why P1**: Cron job é ação agendada que executa em nome do user. Permitir que outro user cancele ou veja é vazamento operacional.

**Acceptance Criteria**:

1. WHEN cron é criado (via `/cron`, comando natural, CLI, cron follow-up ou agent com `schedule:`) THEN SHALL gravar `owner_user_id` no `cron_jobs`
2. WHEN pipeline injeta instruções de cron CLI no prompt THEN SHALL incluir `--owner-user-id <sender.id>`
3. WHEN um cron job cria follow-up via CLI THEN SHALL usar o `owner_user_id` do job original
4. WHEN user lista cron jobs (`/cron list`) THEN SHALL ver apenas registros com `owner_user_id = sender.id`
5. WHEN user pausa, retoma ou cancela cron (`/cron pause|resume|cancel N`) THEN SHALL ser permitido apenas se `owner_user_id = sender.id`; senão SHALL retornar "não encontrado"
6. WHEN cron pré-existente não tem owner confiável (`owner_user_id` vazio, `NULL` em DB antigo, ou placeholder legado) THEN SHALL ser tratado como pertencente ao user-id da migração (preenchido pelo comando de migração)
7. WHEN agent com `schedule:` é registrado THEN o cron criado SHALL ter `owner_user_id` derivado de `app.json.default_owner_user_id`, não da posição atual da whitelist
8. WHEN CLI `aurelia cron add|once` é chamado sem owner após migração THEN SHALL usar `default_owner_user_id` ou rejeitar com erro claro; nunca gravar owner vazio

**Independent Test**: User A cria `/cron daily backup às 9h`. User B manda `/cron list` — não vê. User B tenta `/cron cancel 1` — vê "não encontrado".

---

### P1: Estado operacional por user ⭐ MVP

**User Story**: Como user em um grupo, quero que meus comandos de controle (`/new`, `/usage`, status, cancelamento) afetem só meu trabalho.

**Why P1**: Sem isso, isolamento de sessão fica incompleto: um user pode resetar usage, cancelar run ou receber status do outro.

**Acceptance Criteria**:

1. WHEN usage é registrado THEN SHALL ser acumulado por `SessionKey{chat_id, thread_id, user_id}`
2. WHEN `/usage` é chamado THEN SHALL mostrar apenas usage do sender no chat/thread atual
3. WHEN `/new` é chamado THEN SHALL resetar apenas sessão, usage e nudge buffer do sender no chat/thread atual
4. WHEN mensagem concorrente pergunta status THEN SHALL retornar apenas active run/fila do sender
5. WHEN mensagem concorrente pede cancelamento THEN SHALL cancelar apenas active run/fila do sender
6. WHEN `/forget-me` é confirmado THEN SHALL cancelar todos os active runs do user em todos os chats/threads antes de apagar arquivos

**Independent Test**: User A tem run ativo no tópico; User B manda "cancela". O run de A continua, B recebe status/cancelamento do próprio escopo.

---

### P1: Project memory por user × projeto ⭐ MVP — 🟡 Movida para Sprint E

> **Reformulada em 2026-05-30:** Esta user story foi movida para Sprint E (`.specs/features/project-memory/`). O modelo original `(user_id, project_slug)` com path `users/<id>/projects/<slug>/memory/` foi substituído por `cwd_overlay` escopado por `(chat_id, thread_id)`. A memória privada de utilizador em contexto de trabalho vive em `topics/.../cwd_overlay/`, que é criada apenas quando `/cwd` é declarado no tópico. Ver `.specs/features/project-memory/spec.md` para a spec completa.

---

### P1: Onboarding conversacional pra user novo ⭐ MVP

**User Story**: Como dono do deployment, quando eu adiciono um user_id novo na whitelist, esse user batendo no bot SHALL passar por uma curta conversa de boas-vindas que gera o USER.md dele — sem eu precisar criar arquivos manualmente.

**Why P1**: Sem onboarding remoto, isolamento por user é impraticável — o owner teria que entrar no host, criar USER.md, etc. Quebra a UX "Aurelia é controlada pelo Telegram".

**Acceptance Criteria**:

1. WHEN user autorizado bate no bot AND não tem `~/.aurelia/users/<user_id>/profile.json` THEN UserGate SHALL entrar em modo onboarding antes de comandos/pipeline (estado persistido em SQLite)
2. WHEN onboarding inicia THEN Aurelia SHALL apresentar a si mesma e perguntar como o user prefere ser chamado
3. WHEN user responde nome THEN Aurelia SHALL oferecer "quer me contar algo sobre você ou seu trabalho? (opcional, pode pular)". Idioma SHALL ser inferido da primeira mensagem (default `pt` se ambíguo) — NÃO perguntado explicitamente
4. WHEN onboarding completa THEN Aurelia SHALL gravar `profile.json` (name, detected_language, onboarded_at, is_owner) + `USER.md` mínimo no diretório do user e sair do modo onboarding
5. WHEN o onboarding completa THEN a primeira mensagem original do user SHALL ser respondida (não perdida)
6. WHEN o user reinicia o bot ou daemon morre durante onboarding THEN SHALL retomar do passo onde parou (estado em SQLite)
7. WHEN user já onboarded volta THEN SHALL pular onboarding completamente

**Independent Test**: User novo manda "olá, tudo bem?" → Aurelia infere `pt` e pergunta nome → user diz "Bob" → Aurelia oferece contar algo → user pula → Aurelia agradece, grava arquivos (com `language: "pt"`), e processa "olá, tudo bem?" normalmente.

---

### P1: Comando CLI `aurelia migrate-multi-user` ⭐ MVP

**User Story**: Como dono do deployment, quero rodar um comando único que move meu estado atual single-user pro novo layout isolado por user, sem perder nada.

**Why P1**: Migração é one-shot, mas tem que ser segura e reversível. Comando explícito ≠ migração automática no boot (você pediu explícito).

**Acceptance Criteria**:

1. WHEN comando é rodado THEN SHALL mover `~/.aurelia/memory/<arquivos pessoais>` pra `~/.aurelia/users/<target_id>/memory/`
2. WHEN comando é rodado THEN `~/.aurelia/memory/personas/IDENTITY.md` e `SOUL.md` SHALL permanecer onde estão (são globais)
3. WHEN comando é rodado THEN `~/.aurelia/memory/personas/USER.md` SHALL mover pra `~/.aurelia/users/<target_id>/personas/USER.md`
4. ~~WHEN comando é rodado THEN `~/.aurelia/projects/` SHALL mover pra `~/.aurelia/users/<target_id>/projects/`~~ — **REMOVIDO (2026-05-30):** project team memory permanece em `~/.aurelia/projects/<slug>/team/` (compartilhado). Memória privada contextualizada vai para `topics/.../cwd_overlay/` (ver Sprint E). O passo de migração `projects/` → `users/<id>/projects/` não se aplica ao novo modelo.
5. WHEN comando é rodado THEN cron_jobs com owner ausente/vazio/legado SHALL receber `<target_id>`
6. WHEN `--user-id <id>` é passado THEN `<target_id> = id`; senão SHALL usar `default_owner_user_id` se existir, ou o primeiro da `TelegramAllowedUserIDs`
7. WHEN `--dry-run` é passado THEN SHALL imprimir o que faria sem alterar nada
8. WHEN a migração completa THEN SHALL criar `~/.aurelia/.multi-user-migrated` (marker) com timestamp e target_id
9. WHEN comando é rodado num host já migrado THEN SHALL detectar via marker e abortar com mensagem clara
10. WHEN qualquer passo falha THEN SHALL parar e reportar; estado anterior preservado (move = copy + verify + delete original, em duas fases)

**Independent Test**: Estado completo single-user + 2 cron jobs + memória + USER.md. Roda `aurelia migrate-multi-user --user-id 12345`. Verifica: layout novo correto, cron jobs com owner_user_id=12345, marker presente, segundo run no-op.

---

### P2: Comando `/users` no Telegram

**User Story**: Como dono, quero listar quem está autorizado e em que estado (onboarded/não) sem precisar abrir terminal.

**Acceptance Criteria**:

1. WHEN owner manda `/users` THEN SHALL listar todos os IDs em `TelegramAllowedUserIDs` com status: nome (se onboarded), idioma, total de cron jobs
2. WHEN um non-owner manda `/users` THEN SHALL responder "permissão negada" (só `Profile.IsOwner` ou `app.json.default_owner_user_id`)

---

### P2: `/forget-me` — user deleta a si mesmo

**User Story**: Como user, quero apagar meus dados sem precisar pedir pro owner.

**Acceptance Criteria**:

1. WHEN user manda `/forget-me` THEN Aurelia SHALL pedir confirmação (botão inline "Confirmar / Cancelar")
2. WHEN confirmado THEN SHALL remover `~/.aurelia/users/<user_id>/` inteiro e cancelar seus cron jobs
3. WHEN deletado THEN próxima mensagem do user SHALL disparar onboarding novamente (como user novo)
4. WHEN user_id é o único da whitelist THEN SHALL recusar (evita travar o sistema)

---

### P2: Comandos globais owner-only

**User Story**: Como owner do deployment, quero que apenas eu altere configuração global compartilhada.

**Acceptance Criteria**:

1. WHEN non-owner tenta `/model <x>` THEN SHALL receber "permissão negada"
2. WHEN non-owner tenta comando que altera config global do deployment THEN SHALL receber "permissão negada"
3. WHEN owner executa `/model <x>` THEN comportamento atual SHALL ser preservado
4. WHEN comando é apenas informativo e não sensível (`/help`, `/status`, `/agents`) THEN pode continuar disponível a qualquer user autorizado, com dados escopados quando aplicável

---

## Edge Cases

- WHEN `user_id` é 0 (Telegram retorna 0 em situações de erro) THEN SHALL recusar e logar
- WHEN user autorizado sem profile envia comando (`/cron`, `/model`, `/cwd`, texto natural de agendamento) THEN UserGate SHALL interceptar onboarding antes do comando
- WHEN whitelist está vazia THEN bot rejeita tudo (comportamento atual mantido)
- WHEN user removido da whitelist depois de onboarded THEN seus dados em `~/.aurelia/users/<user_id>/` SHALL ser preservados (delete só por `/forget-me` ou intervenção manual)
- WHEN grupo tem múltiplos users autorizados E todos falam THEN cada user SHALL ter uma **sessão LLM independente** (chave `(chat_id, thread_id, user_id)` em vez do atual `(chat_id, thread_id)`). Cada um vê só seu próprio histórico de conversa; o que é compartilhado é a memória **escrita** do tópico em `~/.aurelia/topics/chat_<id>/thread_<id>/` (markdown legível por todos). **⚠️ Mudança de UX vs. v0.6.x:** antes, em grupo, vários users compartilhavam a mesma sessão da PI (cada um via o histórico do outro). Agora ficam isolados — documentar no CHANGELOG como breaking change deliberada
- WHEN dois users no mesmo tópico usam `/cwd` THEN o último `/cwd` continua definindo o projeto compartilhado daquele tópico; isto é intencional e documentado como escopo de conversa, não escopo pessoal
- WHEN onboarding é interrompido (user some no meio) THEN o state SHALL expirar após 24h e o user SHALL precisar começar de novo
- WHEN cron criado por agent automático (sem sender humano) THEN `owner_user_id = app.json.default_owner_user_id`
- WHEN cron job cria follow-up via CLI THEN owner SHALL herdar `job.owner_user_id`; não usar sender inexistente nem default owner
- WHEN `OWNER_PLAYBOOK.md` contém contexto do owner THEN esse bloco SHALL ser injetado apenas para `Profile.IsOwner=true`; conteúdo global para todos deve viver em arquivo explicitamente não pessoal
- WHEN o comando de migração roda em um deployment com whitelist vazia THEN SHALL abortar com erro "configure TelegramAllowedUserIDs antes de migrar"
- WHEN duas migrações são iniciadas concorrentes THEN a segunda SHALL falhar via lock file
- WHEN um arquivo em `~/.aurelia/memory/` tem nome que conflita (existe lá e em `~/.aurelia/users/<id>/memory/`) THEN migração SHALL parar e pedir resolução manual

---

## Success Criteria

- [ ] Test de integração: 2 users na whitelist, conversam concorrentemente, memórias nunca vazam (asserção determinística)
- [ ] Test: cron de user A invisível pra user B
- [ ] Test: USER.md de cada user é diferente, IDENTITY/SOUL idênticas
- [ ] Test: migração idempotente (roda 2x sem dano)
- [ ] Test: onboarding gera profile.json e USER.md, primeira mensagem é respondida após onboarding
- [ ] Test: dois users no mesmo `(chat_id, thread_id)` (grupo) têm sessões LLM independentes — A não vê histórico de B
- [ ] Test: dois users no mesmo tópico compartilham CWD, mas não compartilham usage/active run/session
- [ ] Test: user novo enviando `/cron list` dispara onboarding, não comando
- [ ] Test: cron CLI injetado no prompt inclui `--owner-user-id`
- [ ] Test: cron follow-up herda `job.owner_user_id`
- [ ] Test: `/model` é owner-only
- [ ] Test: `OWNER_PLAYBOOK.md` só entra no prompt do owner
- [ ] Test: `/forget-me` durante run ativo cancela o run antes de apagar (sem corrupção)
- [ ] Test: NudgeBuffer por `SessionKey` — facts de A/tópico X não vazam para B/tópico Y nem para dream consolidation de outro user
- [ ] Test: onboarding concorrente de dois users novos não corrompe estado
- [ ] Test: migração com lock-file presente e sem marker recusa start até intervenção
- [ ] Manual smoke: onboarding completo de um user fake na sua whitelist, depois `/forget-me`
- [ ] Zero regressão: você (1 user só) após migração tem comportamento idêntico
- [ ] `go build ./... && go vet ./... && go test ./...` limpo
