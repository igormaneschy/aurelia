# UX Polish — Especificação

**Status:** Mostly validated — see `.specs/features/ux-polish/validation.md` for manual smoke-test gaps.

## Problem Statement

A experiência do usuário no Telegram tem vários pontos de fricção pequenos mas cumulativos que deixam a interação menos fluida do que poderia ser:

1. **Silêncio inicial**: Ao enviar uma mensagem (especialmente com imagem ou áudio), há um gap de 1–3 segundos antes de qualquer feedback visual. O usuário não sabe se a mensagem foi recebida.
2. **Concorrência delegada**: Quando o bot está ocupado, o PI SDK recebe e gerencia as mensagens subsequentes. Aurelia não deve inventar posição de fila; deve apenas sinalizar recebimento/processamento sem criar uma fila paralela enganosa.
3. **Destruição de contexto**: Trocar de modelo via `/model` limpa **todas** as sessões do chat, incluindo tópicos de fórum não relacionados — surpreendente e destrutivo.
4. **Erros sem saída**: Mensagens como *"Falha ao conectar com o processador"* não sugerem o que fazer a seguir.
5. **Progresso limitado**: O reporter de ferramentas trunca em 5 itens, não mostra tempo decorrido, e não distingue "pensando" de "executando".
6. **Números crus**: Limites de imagem são mostrados em bytes (10485760) em vez de MB (10 MB).
7. **Status técnico**: `/status` exibe `sid=abc12345 (warm)` — linguagem interna, não útil para o usuário.
8. **Reset prolixo/enganoso**: `/new` deve confirmar o reset sem inventar contagens de mensagens/tokens, pois contexto e tokens são gerenciados pelo PI SDK.
9. **Help minimalista**: `/help` lista comandos mas não ensida o uso natural ("agenda todo dia às 9h").
10. **Formatos bloqueados sem alternativa**: Ao enviar .docx, o bot apenas diz o que não suporta.

Cada um desses pontos é pequeno isoladamente, mas juntos criam a sensação de um bot "meio cru" em vez de um assistente refinado.

## Goals

- [ ] Usuário recebe feedback visual imediato ao enviar qualquer mensagem
- [ ] Usuário recebe feedback sem Aurelia inventar posição de fila quando o PI SDK gerencia concorrência
- [ ] Troca de modelo afeta apenas o contexto atual (thread), não todo o chat
- [ ] Toda mensagem de erro sugere um próximo passo concreto
- [ ] Progresso de ferramentas mostra tempo e mais contexto
- [ ] Todos os tamanhos de arquivo/arquivos são mostrados em unidades legíveis
- [ ] `/status` é compreensível para não-desenvolvedores
- [ ] `/new` confirma o reset com mensagem curta e sem métricas que Aurelia não controla
- [ ] `/help` ensina comandos naturais, não só slash commands
- [ ] Formatos não suportados vêm com sugestão de alternativa

## Out of Scope

- Mudanças na arquitetura do pipeline ou bridge
- Novos comandos ou funcionalidades (ex: busca de modelos por texto)
- Suporte a novos formatos de arquivo (.docx, .xlsx)
- Internacionalização (i18n) — permanece em português
- Mudanças no onboarding CLI

---

## User Stories

### P1: Ack de Recebimento Imediato — MVP

**User Story**: Como usuário do Telegram, quero saber imediatamente que minha mensagem foi recebida, pra não ficar em dúvida se o bot está vivo.

**Why P1**: Gap de silêncio de 1–3s é a primeira impressão. Sem ack, usuários reenviam a mensagem.

**Acceptance Criteria**:

1. WHEN o usuário envia qualquer mensagem (texto, foto, áudio, documento) THEN o bot SHALL reagir com 👀 no message original dentro de 500ms
2. WHEN o bot começa a processar a mensagem THEN a reação 👀 SHALL ser removida ou substituída
3. WHEN o processamento termina THEN nenhuma reação residual SHALL permanecer no message original
4. WHEN a mensagem é um comando local (ex: /new, /status, /help) THEN o ack ainda SHALL ocorrer antes do comando ser processado

**Independent Test**: Enviar mensagem no Telegram. Verificar que 👀 aparece imediatamente e some antes da resposta.

---

### P1: Concorrência Delegada ao PI SDK — MVP

**User Story**: Como usuário, quando envio outra mensagem enquanto o bot ainda trabalha, quero receber feedback de que ela chegou sem Aurelia fingir conhecer uma fila que é gerenciada pelo PI SDK.

**Why P1**: Posição de fila inventada piora a confiança. Como o PI SDK é o dono da execução concorrente, Aurelia deve manter a UX honesta e leve.

**Acceptance Criteria**:

1. WHEN uma mensagem chega durante processamento THEN Aurelia SHALL dar ack visual normalmente.
2. WHEN o PI SDK aceita a mensagem subsequente THEN Aurelia SHALL evitar prometer posição/ordem que não controla.
3. WHEN o usuário pergunta "status" enquanto o bot processa THEN a resposta SHALL focar no estado observável: processando/recebido/interrompível.
4. WHEN houver mensagem de concorrência THEN ela SHALL deixar claro que o processamento é gerenciado pelo PI SDK, sem criar fila paralela.

**Independent Test**: Enviar mensagem A (lenta), depois mensagem B rapidamente. Verificar que não há promessa falsa de posição na fila.

---

### P1: Model Switch Local — MVP

**User Story**: Como usuário em um grupo com tópicos, quero trocar de modelo sem perder o contexto de conversas em outros tópicos.

**Why P1**: `ClearAll(chatID)` é destrutivo e surpreendente. O usuário não espera que um comando no tópico X afete o tópico Y.

**Acceptance Criteria**:

1. WHEN o usuário troca de modelo via `/model` ou callback inline THEN o sistema SHALL limpar apenas a sessão do thread atual (chatID + threadID)
2. WHEN o modelo é trocado em um tópico de fórum THEN sessões de outros tópicos do mesmo grupo SHALL permanecer intactas
3. WHEN o modelo é trocado em chat privado (threadID=0) THEN a sessão privada SHALL ser limpa (comportamento equivalente ao anterior)
4. WHEN a sessão é limpa após troca de modelo THEN a mensagem de confirmação SHALL mencionar o escopo: `"✅ Modelo alterado. Sessão deste tópico foi resetada."` ou `"Sessão privada resetada."`

**Independent Test**: Criar grupo com 2 tópicos. Conversar em ambos. Trocar modelo em um. Verificar que o outro mantém contexto.

---

### P2: Erros Actionable — Should Have

**User Story**: Como usuário, quando algo dá errado, quero saber o que posso fazer em seguida, não só que deu errado.

**Why P2**: Reduz suporte implícito. Usuários que sabem o próximo passo não ficam paralisados.

**Acceptance Criteria**:

1. WHEN o bridge falha ao conectar (`bridge.Execute` error) THEN a mensagem de erro SHALL incluir: `"Dica: verifique se o daemon está rodando. Se persistir, tente /new para reiniciar a sessão."`
2. WHEN o bridge está em cooldown (`inCooldown`) THEN a mensagem SHALL incluir o tempo restante estimado: `"⏳ Processador em recuperação. Tente novamente em ~%d segundos."`
3. WHEN o cron parser falha (`cmdCronCreate` parse error) THEN a mensagem SHALL incluir um exemplo: `"Tente algo como: \"agenda todo dia às 9h revisar emails\""`
4. WHEN o processamento atinge timeout (10min) THEN a mensagem SHALL sugerir: `"A solicitação foi muito complexa. Tente dividir em partes menores."`

**Independent Test**: Simular cada cenário de erro e verificar que a mensagem contém uma dica acionável.

---

### P2: Progresso Rico — Should Have

**User Story**: Como usuário, quando o bot está executando tarefas longas, quero ver o tempo decorrido e mais ferramentas, pra ter noção de progresso.

**Why P2**: 5 ferramentas é pouco para orquestrações. Tempo decorrido dá sensação de movimento.

**Acceptance Criteria**:

1. WHEN o progress reporter exibe ferramentas THEN ele SHALL manter até 8 ferramentas (aumentar de 5)
2. WHEN o progress reporter atualiza THEN cada linha SHALL incluir o tempo decorrido desde o início do processamento: `"⏱️ 2m 34s"` no topo
3. WHEN o progress reporter mostra uma ferramenta THEN o formato SHALL ser: `"⏱️ 2m 34s\n📖 Reading file...\n✍️ Writing file..."`
4. WHEN o progress reporter é deletado (fim do processamento) THEN a mensagem de progresso SHALL ser removida normalmente

**Independent Test**: Enviar tarefa que usa 6+ ferramentas. Verificar que todas aparecem e o timer incrementa.

---

### P2: Unidades Humanas — Should Have

**User Story**: Como usuário, quero ver tamanhos de arquivo em MB ou KB, não em bytes crus.

**Why P2**: 15728640 bytes requer esforço mental. 15 MB é instantâneo.

**Acceptance Criteria**:

1. WHEN uma imagem excede o limite de tamanho THEN a mensagem de erro SHALL usar unidades legíveis: `"Imagem muito grande (15.0 MB). O limite é 10.0 MB."`
2. WHEN uma imagem é pequena (< 1 KB) THEN a mensagem SHALL usar bytes: `"512 B"`
3. WHEN uma imagem é média (1 KB – 1 MB) THEN a mensagem SHALL usar KB: `"850.5 KB"`
4. WHEN o limite é exibido em qualquer contexto THEN ele também SHALL usar unidades legíveis

**Independent Test**: Enviar imagem de 15MB. Verificar mensagem em unidades humanas.

---

### P2: Status para Humanos — Should Have

**User Story**: Como usuário, quero ver `/status` em linguagem que eu entenda, não IDs de sessão e flags técnicas.

**Why P2**: `sid=abc12345 (warm)` é jargão interno. O usuário quer saber se está funcionando e quanto custou.

**Acceptance Criteria**:

1. WHEN o usuário executa `/status` THEN a saída SHALL omitir o session ID e o flag `warm/cold`
2. WHEN o usuário executa `/status` THEN a saída SHALL incluir, se disponível: modelo atual, projeto/diretório atual e se há conversa ativa, sem estimar mensagens/tokens do PI SDK
3. WHEN não há sessão ativa THEN a mensagem SHALL ser clara: `"Nenhuma conversa ativa no momento."`
4. WHEN há agendamentos ativos THEN a mensagem SHALL mostrar a contagem de forma amigável

**Independent Test**: Executar /status em chat com sessão ativa. Verificar saída sem termos técnicos.

---

### P2: Reset Curto — Should Have

**User Story**: Como usuário, quando reseto a sessão, quero uma confirmação clara e curta de que a próxima mensagem começa uma conversa nova.

**Why P2**: Contexto e tokens são responsabilidade do PI SDK. Aurelia não deve inventar contagens de mensagens/tokens sem uma API explícita do PI.

**Acceptance Criteria**:

1. WHEN o usuário executa `/new` ou "nova conversa" THEN a resposta SHALL ser: `"🗑️ Sessão resetada. Próxima mensagem inicia conversa nova."`
2. WHEN o reset interrompe um processamento ativo THEN a resposta SHALL mencionar a interrupção antes da confirmação curta.
3. WHEN o reset ocorre via troca de modelo THEN a confirmação SHALL mencionar apenas o escopo resetado (privado/tópico), sem contagem de mensagens/tokens.

**Independent Test**: Executar /new e verificar a mensagem curta sem "mensagens" ou "tokens".

---

### P2: Help Rica — Should Have

**User Story**: Como usuário novo, quero saber não só os comandos slash, mas também que posso falar naturalmente com o bot.

**Why P2**: O diferencial do Aurelia são os comandos naturais em português. Esconder isso no /help é desperdiçar o valor principal.

**Acceptance Criteria**:

1. WHEN o usuário executa `/help` THEN a mensagem SHALL incluir uma seção de exemplos naturais após os comandos slash
2. WHEN a seção de exemplos é exibida THEN ela SHALL ter pelo menos 3 exemplos práticos: agendamento, troca de modelo, limpeza de contexto
3. WHEN a mensagem é enviada THEN o formato SHALL ser: comandos listados, separador, exemplos com 💡
4. WHEN o usuário envia um exemplo natural (ex: "agenda todo dia às 9h") THEN o bot SHALL processar corretamente (comportamento existente, apenas documentado)

**Independent Test**: Executar /help. Verificar que a mensagem contém exemplos naturais.

---

### P2: Documentos Não Suportados com Dica — Should Have

**User Story**: Como usuário, quando envio um formato que o bot não suporta, quero saber o que posso fazer em vez disso.

**Why P2**: Bloqueio sem alternativa é dead-end. Uma dica converte frustração em ação.

**Acceptance Criteria**:

1. WHEN o usuário envia um documento não suportado (ex: .docx, .xlsx) THEN a mensagem de erro SHALL incluir uma sugestão de workaround: `"💡 Dica: converta para .pdf ou copie o texto diretamente."`
2. WHEN a mensagem de erro é exibida THEN ela SHALL listar os formatos suportados primeiro, depois a dica
3. WHEN o formato não suportado é uma imagem em formato exótico THEN a mensagem de erro ainda SHALL incluir a dica de conversão

**Independent Test**: Enviar .docx. Verificar que a resposta inclui a dica de conversão.

---

## Edge Cases

- WHEN o bot recebe múltiplas mensagens em sequência rápida THEN o ack 👀 pode acumular reações — o bot SHALL garantir que no máximo 1 reação 👀 esteja presente por mensagem do usuário
- WHEN o bot está em cooldown e o usuário envia mensagem THEN o ack 👀 ainda SHALL aparecer antes da mensagem de erro
- WHEN o progress reporter é deletado e recriado (retry do bridge) THEN o timer SHALL resetar para 0
- WHEN Aurelia não possui métricas do PI SDK THEN `/new` e `/status` SHALL omitir contagens de mensagens/tokens em vez de estimar
- WHEN o `/status` é chamado em grupo sem CWD configurado THEN a mensagem SHALL omitir a seção de projeto em vez de mostrar vazio
- WHEN o usuário troca de modelo em chat privado e depois em tópico de fórum do mesmo grupo THEN as sessões privada e do tópico SHALL ser independentes

---

## Success Criteria

- [ ] Mensagem no Telegram recebe reação 👀 em < 500ms
- [ ] Concorrência não promete posição de fila controlada fora do PI SDK
- [ ] Troca de modelo em fórum não afeta outros tópicos
- [ ] 100% dos erros principais incluem dica acionável
- [ ] Progress reporter mostra timer e até 8 ferramentas
- [ ] Todos os tamanhos de arquivo usam MB/KB/B
- [ ] /status não contém jargão técnico (session ID, warm/cold)
- [ ] /new mostra confirmação curta sem métricas inventadas
- [ ] /help inclui exemplos de comandos naturais
- [ ] Formatos não suportados vêm com sugestão de conversão
- [ ] Testes unitários cobrem todas as mensagens de UX modificadas
- [ ] Nenhuma regressão nos testes existentes
