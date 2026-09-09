# long-session-ux Specification

## Purpose
Garantir experiência fluida e observável em sessões longas: progresso contínuo
em Telegram/TUI durante stalls, timeout liveness-aware com origem preservada,
contexto controlado com continuidade, restart seguro sem perda silenciosa e
timeline explicável por run sem armazenar segredos.

## Requirements

### Requirement: A1 — Continuous long-run progress on both surfaces

O core MUST produzir estado de progresso contínuo e cada adapter MUST
apresentá-lo de forma adequada durante uma execução longa, inclusive quando não
houver evento produtivo do Bridge por vários minutos. O estado de progresso
MUST distinguir **ferramenta em execução** de **modelo silencioso**, e o
`detail` associado a um estado (incluindo compactação) MUST ser renderizado pelo
adapter, não descartado.

#### Scenario: Telegram progress during a stall

- **GIVEN** uma execução Telegram ativa com mais de 15s sem evento produtivo
- **WHEN** os marcos de stall são atingidos
- **THEN** o recibo existente é atualizado em até 90s
- **AND** o sistema não cria uma mensagem nova para cada heartbeat

#### Scenario: TUI progress during a stall

- **GIVEN** uma execução TUI ativa com mais de 15s sem evento produtivo
- **WHEN** os marcos de stall são atingidos
- **THEN** o indicador visual da sessão é atualizado em até 90s
- **AND** o heartbeat não é adicionado como mensagem normal no transcript

#### Scenario: progress during a long-running tool

- **GIVEN** uma execução ativa com uma ferramenta em execução
- **WHEN** a ferramenta permanece em execução por mais de 15s
- **THEN** o estado de progresso passa a `tool_running` com o label seguro da
  ferramenta e o tempo decorrido
- **AND** o recibo Telegram e o indicador TUI são atualizados em até 30s
- **AND** nenhum texto de stall ("demorando mais que o normal") é apresentado
  enquanto a ferramenta estiver em execução

#### Scenario: compaction detail is visible

- **GIVEN** uma execução ativa em que a compactação terminou
- **WHEN** o evento de compactação é processado
- **THEN** o `detail` com tokens antes/depois é renderizado no recibo Telegram
  e no indicador TUI uma única vez por run
- **AND** uma compactação regressiva é apresentada como aviso, não como sucesso
  silencioso

### Requirement: A2 — Liveness-aware timeout and recovery

O pipeline MUST distinguir ausência de atividade produtiva, ferramenta em
execução, Bridge vivo, process death, cancelamento do usuário e timeout de
segurança, preservando a origem do encerramento e um checkpoint retomável.
Atividade de ferramenta em execução MUST contar como atividade para a janela de
silêncio.

#### Scenario: silent but alive Bridge

- **GIVEN** o Bridge responde à sonda de liveness, mas não produz evento por
  um intervalo prolongado
- **WHEN** o watchdog avalia a execução
- **THEN** a execução não é cancelada imediatamente apenas por silêncio
- **AND** antes de um cancelamento de segurança o usuário recebe aviso
  escalonado
- **AND** qualquer encerramento persiste `timeout_origin`, duração e checkpoint

#### Scenario: long-running tool does not trigger idle escalation

- **GIVEN** uma ferramenta em execução com eventos `tool_running` periódicos
- **WHEN** o silêncio de eventos produtivos excede o idle configurado
- **THEN** o watchdog NÃO emite warning, urgent ou cancelamento por idle
- **AND** o cap duro de execução continua sendo o teto, com
  `timeout_origin=max_execution`

#### Scenario: dead bridge during a long-running tool

- **GIVEN** uma ferramenta em execução e o Bridge deixa de responder à sonda
- **WHEN** o watchdog avalia a execução
- **THEN** a execução é cancelada com `timeout_origin=process_death`
- **AND** o checkpoint preservado permite retomada

### Requirement: A3 — Bounded context with preserved continuity

O lifecycle de sessão MUST conter crescimento de contexto sem perder a
continuidade da tarefa nem substituir uma sessão válida por um resumo
incompatível. A decisão de contexto MUST usar o **tamanho do contexto atual
relativo à janela do modelo** — não o total cumulativo de tokens de input da
sessão — e MUST respeitar a gestão de contexto model-aware do PI SDK como
caminho primário. O uso de contexto/custo da sessão MUST ser observável e a
compactação MUST ser comunicada ao usuário.

#### Scenario: compaction or safe rotation

- **GIVEN** uma sessão próxima dos limites de contexto ou com compactação
  solicitada
- **WHEN** compactação ou rotação termina
- **THEN** tokens antes/depois, duração e resultado são observáveis
- **AND** o `session_file`/contexto válido é preservado ou a recuperação é
  explicitamente marcada como fria
- **AND** compactação sem redução não é tratada como sucesso operacional sem
  diagnóstico

#### Scenario: session usage is observable

- **GIVEN** uma sessão com `session_file` existente
- **WHEN** o usuário consulta o uso da sessão (comando ou indicador)
- **THEN** contexto atual, janela do modelo e percentual de uso são
  apresentados
- **AND** o uso acumulado de billing (input/output/custo) é apresentado
  separadamente
- **AND** o custo/tokens do último run ficam persistidos no runlog

#### Scenario: cumulative billing tokens do not drive context decisions

- **GIVEN** uma sessão cujo total cumulativo de input tokens ultrapassa um
  limiar absoluto histórico, mas cujo contexto atual é pequeno em relação à
  janela do modelo
- **WHEN** o lifecycle avalia a sessão
- **THEN** nenhuma compactação ou rotação é forçada por causa do cumulativo
- **AND** a decisão de contexto usa o percentual da janela do modelo

### Requirement: A4 — No silent loss during restart

Uma execução ativa MUST ser drenada ou recuperável de forma explícita quando o
daemon for reiniciado por deploy/shutdown.

#### Scenario: deploy with active run

- **GIVEN** existe um run ativo quando `make deploy` inicia o restart
- **WHEN** o daemon é encerrado
- **THEN** o run é concluído antes do restart ou é persistido como interrompido
  com notificação de retomada
- **AND** nenhum retry depende de um Bridge já morrendo
- **AND** a retomada usa o `session_file` correto em modo frio

### Requirement: A5 — Explainable long-run trace

Cada run MUST possuir uma timeline correlacionada por `run_id`/`request_id`
capaz de explicar stalls, duração de ferramentas, first feedback,
compactação/rotação, timeout, restart/process death e divergência entre stream
e resultado final, sem armazenar segredos. O terminal do run MUST persistir
tokens de entrada/saída, custo e contagem de ferramentas.

#### Scenario: diagnose a completed or failed long run

- **GIVEN** um run longo terminou com sucesso, falha, cancelamento ou timeout
- **WHEN** `aurelia debug run`/`debug metrics --json` consulta o runlog
- **THEN** a timeline contém as fases e durações necessárias para distinguir
  ferramenta lenta, provider silencioso, Bridge morto, timeout e restart
- **AND** divergência stream/result é registrada com metadados redigidos
- **AND** prompts, argumentos e resultados sensíveis permanecem truncados ou
  redigidos conforme o contrato atual

#### Scenario: usage is persisted atomically with the terminal state

- **GIVEN** um run terminou com um evento `result` contendo tokens e custo
- **WHEN** o terminal do run é persistido
- **THEN** `input_tokens`, `output_tokens`, `cost_usd` e `tool_count` são
  gravados na mesma transação da conclusão
- **AND** `debug metrics` deixa de reportar custo zero para runs que tiveram uso

### Requirement: A6 — Tool-in-flight feedback without false stall

O Bridge MUST detectar ferramenta em execução e MUST NOT classificar esse
período como stall do modelo. Enquanto houver ferramenta em voo, o Bridge MUST
emitir `tool_running` periódico e MUST NOT emitir `stall` nem `steer`. Uma
ferramenta que exceda o limite de aviso lento MUST gerar um aviso honesto de
ferramenta lenta, uma vez por ferramenta.

#### Scenario: long tool emits tool_running heartbeats

- **GIVEN** uma ferramenta em execução há mais de 15s
- **WHEN** o health monitor avalia o silêncio
- **THEN** um evento `tool_running` é emitido com label seguro e `elapsed_ms`
- **AND** nenhum evento `stall` é emitido

#### Scenario: no steer while a tool is in flight

- **GIVEN** uma ferramenta em execução há mais de 60s
- **WHEN** o health monitor avalia o silêncio
- **THEN** nenhum `steer` é enviado ao modelo
- **AND** a mensagem "pare o que está fazendo" nunca é injetada durante a
  execução de uma ferramenta

#### Scenario: slow tool warning is honest

- **GIVEN** uma ferramenta em execução além do limite de aviso lento
- **WHEN** o limite é atingido
- **THEN** um aviso de ferramenta lenta é emitido uma única vez para aquela
  ferramenta, com o label seguro e o tempo decorrido
- **AND** o aviso não usa o texto de "modelo com dificuldade de responder"

#### Scenario: tool label is bounded and redacted

- **GIVEN** um nome de ferramenta arbitrário vindo do SDK
- **WHEN** `tool_running` é emitido ou persistido
- **THEN** o nome é reduzido ao allowlist de labels
- **AND** comando, argumentos e resultado nunca são incluídos

### Requirement: A7 — Session usage visibility on both surfaces

O sistema MUST expor o uso da sessão (contexto, percentual do limite de
compactação e custo) no Telegram e na TUI, a partir de dados já fornecidos pelo
Bridge, sem exigir nova fonte de verdade.

#### Scenario: Telegram /usage reports real usage

- **GIVEN** uma sessão ativa com `session_file` existente
- **WHEN** o usuário envia `/usage`
- **THEN** a resposta apresenta tokens de contexto, percentual do limite de
  compactação, custo acumulado, turnos e mensagens
- **AND** falha na coleta de estatísticas degrada para uma mensagem explícita,
  não para silêncio ou erro cru

#### Scenario: TUI shows context and cost

- **GIVEN** a TUI conectada a uma sessão ativa
- **WHEN** o usuário abre o painel de estado ou o indicador de sessão
- **THEN** contexto (tokens/percentual) e custo são exibidos
- **AND** a coleta é on-demand, sem bloquear o envio de mensagens

### Requirement: A8 — Proactive long-session decision point

O sistema MUST avisar o usuário uma única vez quando a sessão ultrapassa o
limiar de atenção, oferecendo a decisão entre continuar e iniciar uma sessão
nova. O sistema MUST NOT iniciar uma sessão nova automaticamente nem repetir o
aviso a cada turno.

#### Scenario: nudge at the attention threshold

- **GIVEN** uma sessão ativa que cruza o limiar de atenção de contexto
- **WHEN** o próximo run é avaliado
- **THEN** um aviso com contexto/custo aproximados e a opção de `/new` é
  apresentado uma única vez
- **AND** nenhuma compactação ou rotação é forçada por causa do aviso

#### Scenario: nudge is not repeated

- **GIVEN** que o aviso de sessão longa já foi apresentado nesta sessão
- **WHEN** turnos seguintes continuam acima do limiar
- **THEN** o aviso não é repetido
- **AND** o `TokenGuard` permanece como único fallback de emergência

### Requirement: A9 — Model-aware context management

O sistema MUST decidir compactação/rotação com base no contexto atual relativo
à janela do modelo, deixando a compactação normal para o PI SDK (que compacta em
`contextWindow − reserveTokens`). Os limiares do Go MUST ser percentuais da
janela, e o Go MUST NOT impor um teto absoluto menor que a capacidade do modelo.

#### Scenario: large-window model is not rotated early

- **GIVEN** um modelo com janela de contexto grande e uma sessão cujo contexto
  atual está bem abaixo da janela
- **WHEN** o total cumulativo de tokens de input da sessão é alto
- **THEN** a sessão continua sem rotação
- **AND** a compactação normal é deixada ao PI SDK

#### Scenario: emergency rotation uses the window percentage

- **GIVEN** uma sessão cujo contexto atual atinge o percentual de emergência da
  janela
- **WHEN** o lifecycle avalia a sessão
- **THEN** a rotação é acionada como emergência
- **AND** o motivo registra o percentual do contexto, não um total cumulativo

#### Scenario: compaction reduction is detectable

- **GIVEN** que uma compactação reduziu o contexto atual
- **WHEN** o acompanhamento de contexto avalia a sessão seguinte
- **THEN** a redução é reconhecida e não dispara escalada por "turnos sem
  redução"
- **AND** uma compactação que não reduziu o contexto é sinalizada como
  regressiva

#### Scenario: unknown context size does not escalate

- **GIVEN** que o SDK não consegue estimar o contexto atual (percentual
  indisponível)
- **WHEN** o lifecycle avalia a sessão
- **THEN** nenhuma compactação/rotação é forçada por contexto
- **AND** o SDK e o provider permanecem responsáveis pelo limite da janela

#### Scenario: context size is exposed for the surfaces

- **GIVEN** uma sessão ativa com `session_file`
- **WHEN** as estatísticas da sessão são consultadas
- **THEN** tokens do contexto atual, janela do modelo e percentual são
  retornados
- **AND** Telegram e TUI apresentam esses valores
