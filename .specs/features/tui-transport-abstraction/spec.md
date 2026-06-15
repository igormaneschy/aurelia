# TUI — Transport Abstraction (Fase 0)

## Problem Statement

O Telegram é hoje o único ponto de entrada conversacional da Aurelia. O roadmap da TUI (Terminal User Interface) propõe uma interface alternativa que rode no terminal e comunique com o daemon já em execução.

Antes de construir qualquer UI nova, o pipeline precisa ser desacoplado do Telegram. Hoje a interface `pipeline.Output` e sua única implementação (`telegramPipelineOutput`) misturam comportamento genérico de envio de mensagens com comportamento específico do Telegram (reações com emoji, deleção de mensagens, progresso baseado em edição de mensagem, execução de planos via `BotController`).

Se a TUI for construída sobre o código atual, seria forçada a:
- aceitar um `Output` cheio de métodos Telegram-shaped;
- fazer no-op forçado em métodos como `ConfirmMessage`, `DeleteMessage` e `ExecuteApprovedPlan`;
- ou pior, vazar tipos `telebot` para dentro do pacote da TUI.

## Goals

- [ ] `pipeline.Output` usa apenas tipos transport-agnósticos.
- [ ] `transport.Transport` retorna um `MessageHandle` opaco e suporta capacidades opcionais (`DeletableTransport`, `ReactableTransport`).
- [ ] Existe uma implementação genérica `transportOutput` que funciona sobre qualquer `transport.Transport` sem conhecimento de Telegram.
- [ ] `telegramPipelineOutput` vira um thin adapter sobre `TelegramTransport`, mantendo o comportamento Telegram existente.
- [ ] Zero regressão no Telegram: respostas, reações, deleção de mensagens de reconexão e execução de planos continuam funcionando.
- [ ] Todos os fakes de teste de `Output` e `Transport` são atualizados para as novas assinaturas.

## Out of Scope

- Implementação da TUI em si (Fase 2).
- IPC layer / Unix socket (Fase 1).
- Novo binary `aurelia-tui`.
- Desacoplar o orchestrator/plan execution para a TUI — planos continuam Telegram-only por enquanto.
- Criar `internal/engine/` ou alterar `BridgeExecutor` (trabalho da feature bridge-adapter-interface).
- Alterar a lógica de negócio do pipeline (prompt builder, session lifecycle, tool monitoring, etc.).

---

## User Stories

### P1: Output Transport-Agnóstico

**User Story**: Como engenheiro, quero que `pipeline.Output` expresse apenas operações de chat genéricas, para que uma futura TUI possa implementá-la sem conhecer Telegram.

**Acceptance Criteria**:

1. WHEN o pipeline chama `Output.SendReply` THEN a mensagem é enviada como markdown/reply pelo transporte subjacente.
2. WHEN o pipeline chama `Output.SendText` THEN a mensagem é enviada como texto plano e retorna um `MessageHandle` opaco.
3. WHEN o pipeline chama `Output.DeleteMessage` com um `MessageHandle` THEN o transporte deleta a mensagem se suportar; caso contrário, é no-op seguro.
4. WHEN o pipeline chama `Output.ConfirmMessage` THEN o transporte confirma a mensagem se suportar (ex: reação emoji no Telegram); caso contrário, é no-op seguro.
5. WHEN o pipeline chama `Output.ExecuteApprovedPlan` THEN a implementação genérica não faz nada; a implementação Telegram continua executando o plano.

**Independent Test**: Criar um `MockTransport` e usar `transportOutput` para enviar uma mensagem — verificar que nenhum tipo `telebot` é necessário.

---

### P1: Capacidades Opcionais no Transporte

**User Story**: Como engenheiro, quero que o transporte declare opcionalmente se suporta deleção e reação, para que transportes simples não precisem implementar stubs vazios.

**Acceptance Criteria**:

1. WHEN `Transport.Send` é chamado THEN ele retorna `(MessageHandle, error)`.
2. WHEN um transporte implementa `DeletableTransport` THEN `DeleteMessage` chama `Delete(ctx, handle)`.
3. WHEN um transporte NÃO implementa `DeletableTransport` THEN `DeleteMessage` é no-op.
4. WHEN um transporte implementa `ReactableTransport` THEN `ConfirmMessage` chama `React(ctx, chatID, messageID)`.
5. WHEN um transporte NÃO implementa `ReactableTransport` THEN `ConfirmMessage` é no-op.

**Independent Test**: `go test ./internal/transport/...` cobre mock com e sem capacidades opcionais.

---

### P1: Telegram Continua Funcionando

**User Story**: Como usuário do Telegram, quero que nada mude no comportamento do bot após a refatoração.

**Acceptance Criteria**:

1. WHEN uma mensagem normal é respondida THEN a resposta chega formatada no Telegram.
2. WHEN o bridge morre e reconecta THEN a mensagem "Reconectando..." é enviada e depois deletada.
3. WHEN uma mensagem de sistema é confirmada THEN o emoji 🎉 continua sendo usado.
4. WHEN um plano de execução é aprovado THEN o orchestrator é acionado normalmente.

**Independent Test**: Teste ao vivo no Telegram após `make deploy`.

---

## Edge Cases

- WHEN `MessageHandle` é nil THEN `DeleteMessage` não faz nada.
- WHEN o transporte retorna erro em `Send` THEN o erro é logado e o pipeline trata como antes.
- WHEN um transporte antigo ainda retorna `error` em vez de `(MessageHandle, error)` THEN o código não compila — a mudança é deliberada e mecânica.
- WHEN `ConfirmMessage` é chamado com `messageID == 0` THEN é no-op.
- WHEN `ExecuteApprovedPlan` é chamado em um `Output` genérico THEN é no-op seguro.

---

## Success Criteria

- [ ] `pipeline.Output` não referencia `telebot` nem outros tipos Telegram-only.
- [ ] `go build ./...`, `go vet ./...` e `go test ./... -short` passam.
- [ ] `telegramPipelineOutput` continua usando `TelegramTransport` para todos os envios.
- [ ] Existe ao menos um teste unitário para `transportOutput` usando `MockTransport`.
- [ ] Teste ao vivo no Telegram confirma zero regressão.
