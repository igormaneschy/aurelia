# TUI — Transport Abstraction (Fase 0) — Tasks

**Design**: `.specs/features/tui-transport-abstraction/design.md`
**Status**: Validated

---

## Execution Plan

### Phase 1: Extender a interface de transporte

```
T1 → T2
```

### Phase 2: Implementar Output genérico

```
T2 ──→ T3
```

### Phase 3: Refatorar adapter Telegram

```
T3 ──→ T4
```

### Phase 4: Atualizar testes e validar

```
T4 ──→ T5 ──→ T6
```

---

## Task Breakdown

### T1: Extender `internal/transport/transport.go`

**What**: Adicionar `MessageHandle`, alterar `Transport.Send` para retornar `(MessageHandle, error)`, e adicionar interfaces opcionais `DeletableTransport` e `ReactableTransport`.

**Where**: `internal/transport/transport.go`

**Depends on**: Nenhuma

**Reuses**: Interface `Transport` existente

**Done when**:
- [ ] `type MessageHandle any` definido.
- [ ] `Transport.Send` retorna `(MessageHandle, error)`.
- [ ] `DeletableTransport` interface definida com `Delete(ctx, MessageHandle) error`.
- [ ] `ReactableTransport` interface definida com `React(ctx, chatID int64, messageID int) error`.
- [ ] `OutgoingMessage` e `IncomingMessage` inalterados exceto se necessário.
- [ ] `go build ./internal/transport/...` compila (quebra esperada em `TelegramTransport` e testes).

**Verify**:
```
go build ./internal/transport/...
```

---

### T2: Atualizar `TelegramTransport` e `MockTransport`

**What**: Implementar as novas assinaturas em `TelegramTransport` e ajustar `MockTransport` nos testes.

**Where**:
- `internal/telegram/transport.go`
- `internal/transport/transport_test.go`

**Depends on**: T1

**Reuses**: Helpers existentes `sendTextWithSender`, `sendErrorWithSender`, `startChatActionLoop`

**Done when**:
- [ ] `TelegramTransport.Send` retorna `(*telebot.Message, error)`.
- [ ] `TelegramTransport.Delete` implementa `DeletableTransport`.
- [ ] `TelegramTransport.React` implementa `ReactableTransport`.
- [ ] `MockTransport` ajustado para `(MessageHandle, error)`.
- [ ] Testes de `MockTransport` atualizados e passando.

**Verify**:
```
go test ./internal/transport/...
```

---

### T3: Criar `internal/pipeline/transport_output.go`

**What**: Implementação genérica de `pipeline.Output` sobre `transport.Transport`.

**Where**: `internal/pipeline/transport_output.go` (novo)

**Depends on**: T1, T2

**Reuses**: `transport.Transport`, `pipeline.ProgressReporter`, `noopPipelineProgress`

**Done when**:
- [ ] Struct `transportOutput` criada com campo `tp transport.Transport`.
- [ ] Construtor `NewTransportOutput(tp transport.Transport) Output`.
- [ ] Todos os métodos de `Output` implementados de forma genérica.
- [ ] `DeleteMessage` faz type assertion seguro para `DeletableTransport`.
- [ ] `ConfirmMessage` faz type assertion seguro para `ReactableTransport`.
- [ ] `ExecuteApprovedPlan` é no-op.

**Verify**:
```
go test ./internal/pipeline/ -run TestTransportOutput -v
```

---

### T4: Refatorar `telegramPipelineOutput`

**What**: Transformar `telegramPipelineOutput` em thin adapter que reusa `TelegramTransport` para envio genérico e mantém só o específico.

**Where**: `internal/telegram/pipeline.go`

**Depends on**: T3

**Reuses**: `TelegramTransport`, `progressReporter`, `BotController.executeApprovedPlan`

**Done when**:
- [ ] `telegramPipelineOutput` delega `StartTyping`, `SendError`, `SendReply`, `SendText`, `DeleteMessage`, `ConfirmMessage` para `TelegramTransport` ou `transportOutput`.
- [ ] `NewProgress` continua criando `progressReporter` do Telegram.
- [ ] `ExecuteApprovedPlan` continua chamando `BotController.executeApprovedPlan`.
- [ ] Nenhum `*telebot.Chat` ou `*telebot.Message` é criado diretamente em `telegramPipelineOutput` — tudo vai para `TelegramTransport`.
- [ ] `go build ./...` compila.

**Verify**:
```
go build ./... && go vet ./...
```

---

### T5: Atualizar fakes de teste do pipeline

**What**: Ajustar todos os `fakeOutput`, `recordingOutput`, `captureOutput`, `testOutputRecorder` para as novas assinaturas.

**Where**:
- `internal/pipeline/result_event_test.go`
- `internal/pipeline/session_lifecycle_test.go`
- `internal/pipeline/ux_messages_test.go`
- `internal/pipeline/project_preflight_test.go`
- Outros `_test.go` que implementam `Output`

**Depends on**: T4

**Reuses**: Fakes existentes

**Done when**:
- [ ] Todos os fakes implementam `Output` com `SendText` retornando `MessageHandle`.
- [ ] Todos os fakes implementam `DeleteMessage(MessageHandle)`.
- [ ] `go test ./internal/pipeline/...` passa.

**Verify**:
```
go test ./internal/pipeline/... -v
```

---

### T6: Validar regressão no Telegram

**What**: Build, deploy e teste ao vivo no Telegram.

**Where**: N/A — validação

**Depends on**: T5

**Done when**:
- [ ] `go test ./... -short` passa.
- [ ] `go vet ./...` passa.
- [ ] `make deploy` atualiza o daemon.
- [ ] Mensagem normal recebe resposta formatada.
- [ ] `/stop` durante execução envia mensagem de interrupção e reação.
- [ ] Reconexão após process death deleta a mensagem "Reconectando...".

**Verify**:
```
go test ./... -short && go vet ./... && make deploy
```

---

## Task Summary

| ID | Task | File(s) | Priority | Depends |
|----|------|---------|----------|---------|
| T1 | Extender interface de transporte | `internal/transport/transport.go` | P0 | — |
| T2 | Atualizar TelegramTransport + MockTransport | `internal/telegram/transport.go`, `internal/transport/transport_test.go` | P0 | T1 |
| T3 | Criar transportOutput genérico | `internal/pipeline/transport_output.go` | P0 | T1, T2 |
| T4 | Refatorar telegramPipelineOutput | `internal/telegram/pipeline.go` | P0 | T3 |
| T5 | Atualizar fakes de teste | `internal/pipeline/*_test.go` | P0 | T4 |
| T6 | Validar regressão Telegram | N/A (live) | P0 | T5 |
