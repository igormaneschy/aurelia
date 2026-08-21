# tui-model-indicator Specification

## Purpose
Manter o indicador de modelo da TUI (header e sidebar) sincronizado com o modelo
efetivamente confirmado pelo daemon, em todos os caminhos de seleção `/model`,
sem exibir estados não confirmados.

## Requirements

### Requirement: A1 — Successful model change refreshes the visual state

A troca de modelo concluída pelo comando `/model` MUST atualizar o modelo
exibido no header e na sidebar da TUI sem nova mensagem do usuário, troca de
sessão ou reinício do cliente.

#### Scenario: textual model change

- **GIVEN** a TUI mostra o modelo `model-a`
- **WHEN** o usuário executa `/model model-b` e o daemon confirma a troca
- **THEN** a TUI consulta o status canônico do daemon
- **AND** header e sidebar passam a mostrar `model-b`
- **AND** a próxima mensagem usa `model-b`

### Requirement: A2 — All model-selection paths share the same synchronization

Seleção pelo wizard, comando enfileirado, `/model auto` e modelo pendente após
criação de sessão MUST produzir o mesmo refresh pós-comando.

#### Scenario: wizard and queued selection

- **GIVEN** a seleção é feita pelo wizard ou está aguardando na fila
- **WHEN** o comando `/model` termina com sucesso
- **THEN** o status da sessão ativa é atualizado após `stream_end`
- **AND** o indicador visual reflete o modelo aplicado

### Requirement: A3 — Failed or stale refresh never claims a new model

Falha no comando ou no refresh de status MUST preservar o último modelo
confirmado, sem mostrar visualmente um modelo que não foi confirmado pelo
daemon.

#### Scenario: failed model change or status refresh

- **GIVEN** a TUI mostra `model-a`
- **WHEN** `/model model-b` falha ou o status pós-comando não pode ser lido
- **THEN** `model-a` permanece como último estado confirmado
- **AND** a TUI não exibe `model-b` como ativo
