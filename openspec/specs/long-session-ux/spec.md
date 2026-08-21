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
houver evento produtivo do Bridge por vários minutos.

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

### Requirement: A2 — Liveness-aware timeout and recovery

O pipeline MUST distinguir ausência de atividade produtiva, Bridge vivo,
process death, cancelamento do usuário e timeout de segurança, preservando a
origem do encerramento e um checkpoint retomável.

#### Scenario: silent but alive Bridge

- **GIVEN** o Bridge responde à sonda de liveness, mas não produz evento por
  um intervalo prolongado
- **WHEN** o watchdog avalia a execução
- **THEN** a execução não é cancelada imediatamente apenas por silêncio
- **AND** antes de um cancelamento de segurança o usuário recebe aviso
  escalonado
- **AND** qualquer encerramento persiste `timeout_origin`, duração e checkpoint

### Requirement: A3 — Bounded context with preserved continuity

O lifecycle de sessão MUST conter crescimento de contexto sem perder a
continuidade da tarefa nem substituir uma sessão válida por um resumo
incompatível.

#### Scenario: compaction or safe rotation

- **GIVEN** uma sessão próxima dos limites de contexto ou com compactação
  solicitada
- **WHEN** compactação ou rotação termina
- **THEN** tokens antes/depois, duração e resultado são observáveis
- **AND** o `session_file`/contexto válido é preservado ou a recuperação é
  explicitamente marcada como fria
- **AND** compactação sem redução não é tratada como sucesso operacional sem
  diagnóstico

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
e resultado final, sem armazenar segredos.

#### Scenario: diagnose a completed or failed long run

- **GIVEN** um run longo terminou com sucesso, falha, cancelamento ou timeout
- **WHEN** `aurelia debug run`/`debug metrics --json` consulta o runlog
- **THEN** a timeline contém as fases e durações necessárias para distinguir
  ferramenta lenta, provider silencioso, Bridge morto, timeout e restart
- **AND** divergência stream/result é registrada com metadados redigidos
- **AND** prompts, argumentos e resultados sensíveis permanecem truncados ou
  redigidos conforme o contrato atual
