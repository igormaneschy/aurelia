# Long-session UX and Reliability

## Purpose

Garantir experiência fluida e observável em sessões longas: progresso contínuo
em Telegram/TUI durante stalls, timeout liveness-aware com origem preservada,
contexto controlado com continuidade, restart seguro sem perda silenciosa e
timeline explicável por run sem armazenar segredos.

## MODIFIED Requirements

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

## ADDED Requirements

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
