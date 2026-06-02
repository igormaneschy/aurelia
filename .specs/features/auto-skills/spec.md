# Auto-Skills

**Roadmap step:** 9  
**Status:** 🔴 Draft revisado  
**Depende de:** User Isolation (`multi-user-profiles`) para `TurnContext`, `SessionKey`, `UserGate` e diretórios privados por `user_id`
**Depende de:** Security Guard-Rails (✅ done)
**Ganha valor com:** Nudge, Orchestration, Agent Comms e a boundary PI/`ai-memory`, mas o MVP manual (`/skill save`) funciona sem eles

## Problem Statement

Aurelia já tem `internal/agents`: arquivos markdown em `~/.aurelia/agents/` com frontmatter YAML que viram agentes roteáveis por `@nome` ou classificação. Isso é uma base útil para carregar skills privadas, mas a spec antiga misturava três conceitos que o código atual não suporta do jeito descrito:

- O registry atual carrega só `~/.aurelia/agents/` no startup; ele não recebe `user_id`, não sabe origem do agent e não tem overlay por usuário.
- O bridge/PI carrega recursos nativos de `~/.pi/agent`, incluindo PI skills, mas `RequestOptions` não expõe diretório de skills por request. Escrever em `~/.aurelia/users/<id>/skills/` não cria uma PI-native skill automaticamente.
- O formato real de agent usa `allowed_tools` e `disallowed_tools`; frontmatter `tools` não é lido por `internal/agents`.
- O pipeline atual só guarda texto recente no `NudgeBuffer` por chat; não há transcript por usuário com tool calls, duração, modelo, agent e resultado.

Esta spec deve entregar Auto-Skills como **skills PI-compatible, mas Aurelia-managed e privadas por usuário**: procedimentos reutilizáveis gerados a partir de uma execução bem-sucedida, armazenados em layout `SKILL.md`, carregados pelo registry per-user do Aurelia e usados como agents normais. O Aurelia não deve depender de `pi-hermes-memory` nem escrever em `~/.pi/agent` no MVP; ele reaproveita o formato/conceito de skills do PI sem entregar o controle de escopo, UX e segurança para uma extensão externa.

## Architecture Decision — Option A

**Escolha:** Aurelia-native, PI-compatible.

O Aurelia continua sendo a fonte de verdade para `user_id`, `chat_id`, `thread_id`, project binding, permissões e UX. Procedimentos reutilizáveis são persistidos como skills no formato compatível com PI (`<slug>/SKILL.md`) para preservar portabilidade e alinhamento com o motor usado pelo bridge, mas a descoberta/roteamento no MVP é feita pelo registry per-user do Aurelia.

**Não faremos no MVP:** instalar/ativar `pi-hermes-memory`, salvar diretamente em `~/.pi/agent/skills`, ou depender de discovery global do PI. Isso evita vazamento entre usuários autorizados e evita duas fontes de memória divergentes.

## Goals

- [ ] Captura manual explícita (`/skill save <slug>`) a partir do último turno bem-sucedido desse `SessionKey`
- [ ] Recorder de transcript mínimo, redigido e com TTL curto, sem armazenar system prompt completo
- [ ] Geração via LLM dedicada, model-only, sem tools e com `NoUserSettings`
- [ ] Validação rígida do `SKILL.md` gerado antes de gravar: frontmatter permitido, slug seguro, sem `schedule`, sem `mcp_servers`, sem path/cwd perigoso
- [ ] Skills geradas declaram `capability_profile` compatível com guard-rails; `Bash` só via `execute_safe`
- [ ] Storage privado em `~/.aurelia/users/<user_id>/skills/<slug>/SKILL.md` ou path equivalente do resolver de User Isolation
- [ ] Escrita atômica e sem overwrite sem confirmação
- [ ] Registry per-user carrega agentes globais + skills do usuário sem permitir shadowing silencioso de agentes globais
- [ ] `/agents` e roteamento natural consideram as skills do usuário atual
- [ ] Auto-oferta heurística é P2, desligada por padrão, e só roda depois do fluxo manual estar estável

## Out of Scope

- Escrita/discovery direta em `~/.pi/agent/skills` no MVP.
- Uso de `pi-hermes-memory` como backend de memória no MVP.
- Auto-improvement/versioning automático de skills.
- Skill sharing entre usuários.
- Skills geradas automaticamente sem confirmação.
- Captura de dados sensíveis literais; a feature deve redigir antes de chamar a LLM e antes de gravar.
- Cron/schedule em auto-skills; qualquer automação recorrente continua em `internal/cron`.

---

## User Stories

### P0: Isolamento, privacidade e fonte capturável ⭐ MVP

**User Story:** Como usuário em um chat com mais pessoas autorizadas, quero que minhas skills e transcripts sejam privados e não misturem dados de outro usuário.

**Why P0:** Auto-Skills transforma histórico operacional em arquivo persistente. Sem isolamento e redaction, a feature cria vazamento permanente.

**Acceptance Criteria:**

1. WHEN qualquer comando `/skill*` ou `/skills*` chega THEN Aurelia SHALL passar pelo `UserGate`.
2. WHEN transcript recente é salvo THEN a chave SHALL ser `SessionKey{chat_id, thread_id, user_id}`.
3. WHEN skill é gravada THEN o path SHALL ficar dentro do diretório privado do `user_id`.
4. WHEN outro usuário no mesmo tópico roda `/skills` THEN ele SHALL NOT ver skills ou transcripts do primeiro.
5. WHEN transcript contém padrões de segredo (`token`, `password`, `secret`, chaves API, valores env-like) THEN Aurelia SHALL redigir deterministicamente antes de enviar ao generator.
6. WHEN user roda `/forget-me` na spec de User Isolation THEN skills privadas e transcripts pendentes do usuário SHALL ser removidos.

**Independent Test:** Dois usuários no mesmo chat salvam skills com mesmo slug; arquivos ficam em diretórios privados diferentes (`<user>/skills/<slug>/SKILL.md`) e `/skills` lista somente o próprio usuário.

---

### P1: Recorder de turno bem-sucedido ⭐ MVP

**User Story:** Como usuário, quero poder transformar o último trabalho bem-sucedido em skill sem ter que repetir manualmente os passos.

**Why P1:** `/skill save` só é útil se Aurelia capturar um resumo operacional confiável do turno anterior.

**Acceptance Criteria:**

1. WHEN um turno normal começa THEN recorder SHALL capturar `user_text`, agent usado, modelo, cwd efetivo, horário de início e tool events.
2. WHEN `tool_use` chega THEN recorder SHALL guardar nome da tool e input redigido/truncado.
3. WHEN `tool_result` chega THEN recorder SHALL guardar snippet redigido/truncado.
4. WHEN `result` chega com sucesso THEN recorder SHALL guardar resposta final redigida/truncada e stats.
5. WHEN turno termina com erro, timeout, cancelamento, handoff de plano conversacional ou chamada interna de generator/dream/validator THEN recorder SHALL NOT substituir o último transcript capturável.
6. WHEN transcript passa de limites configurados THEN eventos antigos/longos SHALL ser truncados com marcador explícito.

**Independent Test:** Fake bridge com `Read`, `Bash`, `tool_result` e `result` produz transcript com stats corretos e sem system prompt completo.

---

### P1: Captura manual explícita ⭐ MVP

**User Story:** Como usuário, quero salvar o último procedimento bem-sucedido como skill quando eu decidir que vale a pena.

**Why P1:** É a menor versão útil e evita spam/falso positivo antes do detector estar calibrado.

**Acceptance Criteria:**

1. WHEN user manda `/skill save <slug>` ou `/skills save <slug>` THEN Aurelia SHALL usar o último transcript capturável desse `SessionKey`.
2. WHEN não há transcript recente THEN SHALL responder que não existe execução recente para transformar em skill.
3. WHEN slug é inválido THEN SHALL pedir kebab-case (`letras`, `números`, `-`, tamanho máximo).
4. WHEN slug colide com skill do mesmo usuário THEN SHALL pedir confirmação de overwrite ou sugerir rename.
5. WHEN slug colide com agente global THEN SHALL bloquear e pedir outro slug no MVP; não há shadowing silencioso.
6. WHEN user confirma geração/salvamento THEN Aurelia SHALL gerar, validar e escrever a skill atomicamente.
7. WHEN geração falha duas vezes THEN SHALL desistir sem criar arquivo parcial.

**Independent Test:** Transcript fake + `/skill save backup-cron` + fake bridge com bloco `aurelia-skill` válido cria arquivo privado e registry per-user lista a skill.

---

### P1: Geração LLM validada ⭐ MVP

**User Story:** Como sistema, quero transformar transcript em um `SKILL.md` pequeno, executável pelo registry do Aurelia e compatível com o formato de skills do PI.

**Why P1:** A LLM sabe resumir intenção e armadilhas, mas o arquivo persistido precisa obedecer ao formato real do loader.

**Acceptance Criteria:**

1. WHEN geração inicia THEN Aurelia SHALL chamar bridge com prompt dedicado (`BuildSkillCapturePrompt`), transcript redigido, `NoUserSettings=true` e sem tools.
2. WHEN resposta volta THEN Aurelia SHALL extrair um bloco fenced `aurelia-skill`.
3. O frontmatter SHALL usar campos compatíveis com Agent Skills/PI e mapeáveis pelo adapter do Aurelia:
   ```yaml
   name: <slug>
   description: <short description>
   model: <optional model>
   capability_profile: read_only | edit_project | execute_safe
   allowed-tools: Read Bash
   metadata:
     aurelia:
       kind: auto_skill
       created_by: auto-skill
       created_at: <timestamp ISO>
   ```
4. O body SHALL conter seções `Procedure`, `Pitfalls` e `Verify`.
5. WHEN frontmatter contém campos proibidos (`schedule`, `cwd`, `mcp_servers`, path absoluto, shell secreto) THEN validator SHALL rejeitar ou remover conforme regra documentada.
6. WHEN `allowed-tools` inclui tool desconhecida THEN validator SHALL rejeitar.
7. WHEN skill inclui `Bash` THEN `capability_profile` SHALL ser `execute_safe`; `Bash` com `read_only` ou `edit_project` SHALL ser rejeitado.
8. WHEN skill pede `privileged` THEN validator SHALL rejeitar no MVP.
9. WHEN conteúdo ainda contém segredo detectável THEN writer SHALL recusar salvar e informar que a skill precisa ser regenerada/editada.

**Independent Test:** Fake bridge retorna skill com `tools` em vez de `allowed-tools`; validator rejeita e retry pede correção.

---

### P1: Registry per-user e uso como agent ⭐ MVP

**User Story:** Como usuário, depois de salvar uma skill, quero usá-la como agent normal em conversas futuras.

**Why P1:** Sem integração de registry, a skill vira arquivo morto.

**Acceptance Criteria:**

1. WHEN registry é resolvido para um turno THEN SHALL combinar agentes globais com skills PI-compatible do `user_id` via adapter Aurelia.
2. WHEN diretório de skills do usuário não existe THEN SHALL tratar como lista vazia, não erro.
3. WHEN skill e agente global têm mesmo nome THEN registry SHALL preservar o global e reportar colisão; writer já deve bloquear essa criação.
4. WHEN user manda `/agents` THEN SHALL ver agentes globais e suas skills com indicador `(auto-skill)`.
5. WHEN user manda `@<slug>` e `<slug>` é uma skill sem colisão THEN `routeAgent` SHALL retornar a skill do usuário.
6. WHEN outro usuário manda `@<slug>` sem ter essa skill THEN SHALL NOT rotear para skill alheia.

**Independent Test:** User A cria `backup-cron`; `@backup-cron` roteia para A. User B no mesmo chat não vê nem roteia.

---

### P2: Detector e oferta automática

**User Story:** Como usuário, quando uma execução complexa termina bem, quero que Aurelia ofereça salvar uma skill sem eu lembrar do comando.

**Why P2:** É valioso, mas só depois do manual capture estar sólido e sem risco de spam.

**Acceptance Criteria:**

1. WHEN `auto_skills.enabled=false` (default) THEN nenhuma oferta automática SHALL ser enviada.
2. WHEN habilitado e um turno termina com sucesso THEN detector SHALL avaliar stats:
   - tool calls >= N
   - duração >= M
   - diversidade de tools >= K
   - ou execução orquestrada finalizada com sucesso
3. WHEN candidato é detectado THEN Aurelia SHALL enviar uma oferta com resumo curto e botões `Salvar como skill` / `Não`.
4. WHEN user recusa ou expira THEN o mesmo transcript SHALL NOT ser oferecido de novo.
5. WHEN user aceita THEN fluxo SHALL pedir/confirmar slug e reutilizar o mesmo generator/validator/writer do MVP manual.

**Independent Test:** Auto-offer desligado não envia nada; ligado com stats acima do threshold envia oferta uma vez e respeita recusa.

---

### P2: Gerenciamento básico

**User Story:** Como usuário, quero listar, ver e remover minhas skills sem editar arquivos.

**Acceptance Criteria:**

1. WHEN user manda `/skills` THEN SHALL listar skills privadas com descrição, idade e tools.
2. WHEN user manda `/skills show <slug>` THEN SHALL retornar conteúdo ou resumo seguro da skill.
3. WHEN user manda `/skills delete <slug>` THEN SHALL pedir confirmação e remover atomicamente.
4. WHEN user manda `/skills rename <old> <new>` THEN SHALL validar colisões e atualizar frontmatter + arquivo.
5. WHEN operação afeta registry cache THEN SHALL invalidar cache desse `user_id`.

---

### P3: Export PI-native e improvement loop

**User Story:** Como dono do sistema, quero eventualmente exportar skills para o mecanismo nativo do PI quando houver contrato seguro.

**Acceptance Criteria:**

1. WHEN bridge expõe skill dirs/enabled skills por request THEN spec futura MAY permitir discovery PI-native controlado por user/request.
2. WHEN skill é usada muitas vezes com correções manuais THEN improvement loop SHALL propor update, nunca escrever sem confirmação.

---

## Edge Cases

- WHEN generator tenta usar tools THEN request deve estar sem tools; se o bridge não suportar `NoTools`, usar denylist total como fallback.
- WHEN skill gerada tem `schedule` THEN rejeitar no MVP.
- WHEN skill gerada tem `tools` em vez de `allowed-tools` THEN retry com erro estruturado.
- WHEN skill gerada omite `capability_profile` THEN validator SHALL defaultar para `read_only` se não houver write/bash, ou pedir correção estruturada se houver tools perigosas.
- WHEN transcript contém caminho absoluto sensível fora do cwd THEN redigir ou resumir.
- WHEN dois `/skill save` simultâneos usam mesmo slug do mesmo user THEN writer SHALL usar lock/atomic create para evitar corrida.
- WHEN skill file é symlink THEN writer SHALL recusar overwrite.
- WHEN agents dir global está ausente THEN registry per-user ainda pode carregar skills privadas.
- WHEN `/skills show` retornaria conteúdo enorme THEN truncar e indicar path privado.
- WHEN skill fica inválida após edição manual THEN `/agents` deve logar erro e listar como inválida em `/skills`, sem quebrar o registry.

---

## Success Criteria

- [ ] Manual capture end-to-end: turno bem-sucedido → `/skill save` → arquivo privado → `@slug` funciona
- [ ] Transcript não guarda system prompt completo e redige segredos antes do generator
- [ ] Validator rejeita frontmatter incompatível (`tools`, `schedule`, tool desconhecida, profile/tool incompatível)
- [ ] Registry per-user isola skills entre usuários e não shadowa global silenciosamente
- [ ] Skills são salvas em layout PI-compatible (`<slug>/SKILL.md`) sem escrever em `~/.pi/agent`
- [ ] Auto-offer P2 é configurável e default-off
- [ ] Testes cobrem isolamento, colisão, retry, atomic write, registry e comandos
- [ ] `go build ./... && go vet ./... && go test ./...` limpo quando implementado
