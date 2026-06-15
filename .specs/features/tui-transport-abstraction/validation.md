# TUI — Transport Abstraction (Fase 0) — Validation

## Validation Checklist

### Build & Static Analysis

- [ ] `go build ./...` — compila todo o projeto.
- [ ] `go vet ./...` — sem warnings.
- [ ] `go test ./... -short` — todos os testes passam.

### Interface Contracts

- [ ] `transport.MessageHandle` é definido como `any`.
- [ ] `transport.Transport.Send` retorna `(MessageHandle, error)`.
- [ ] `transport.DeletableTransport` existe com método `Delete(context.Context, MessageHandle) error`.
- [ ] `transport.ReactableTransport` existe com método `React(context.Context, int64, int) error`.
- [ ] `TelegramTransport` implementa `Transport`, `DeletableTransport` e `ReactableTransport`.
- [ ] `MockTransport` implementa a interface `Transport` atualizada.

### Output Contracts

- [ ] `pipeline.Output.SendText` retorna `(MessageHandle, error)`.
- [ ] `pipeline.Output.DeleteMessage` recebe `MessageHandle`.
- [ ] `transportOutput` existe em `internal/pipeline/transport_output.go`.
- [ ] `NewTransportOutput(transport.Transport) pipeline.Output` funciona.
- [ ] `transportOutput` não importa `gopkg.in/telebot.v3`.

### Telegram Behavior Preservation

- [ ] `telegramPipelineOutput` ainda usa `TelegramTransport` para envio.
- [ ] Mensagens normais respondem formatadas no Telegram.
- [ ] Mensagens de erro usam o header "⚠️ Erro".
- [ ] Indicador de typing funciona durante processamento.
- [ ] Reconexão após process death envia e depois deleta "⚡ Reconectando...".
- [ ] `ConfirmMessage` continua reagindo com 🎉 no Telegram.
- [ ] Execução de plano (`ExecuteApprovedPlan`) continua funcionando no Telegram.

### Test Coverage

- [ ] `internal/transport/transport_test.go` cobre `MockTransport` atualizado.
- [ ] `internal/pipeline/transport_output_test.go` existe e cobre:
  - [ ] `SendReply` envia mensagem markdown.
  - [ ] `SendText` envia mensagem plain e retorna handle.
  - [ ] `DeleteMessage` no-op quando transporte não é `DeletableTransport`.
  - [ ] `ConfirmMessage` no-op quando transporte não é `ReactableTransport`.
  - [ ] `SendError` encaminha para `Transport.SendError`.
- [ ] Todos os fakes de `Output` nos testes do pipeline foram atualizados.

### Live Validation

- [ ] `make deploy` executa com sucesso.
- [ ] Enviar mensagem no Telegram → resposta chega.
- [ ] Enviar `/stop` durante execução → mensagem de interrupção e reação chegam.
- [ ] Simular process death do bridge → mensagem de reconexão aparece e é removida.

### Out-of-Scope Guard

- [ ] Nenhum arquivo em `internal/tui/` foi criado.
- [ ] Nenhum arquivo em `internal/ipc/` foi criado.
- [ ] Nenhum arquivo em `cmd/aurelia-tui/` foi criado.
- [ ] `internal/engine/` não foi criado.
- [ ] `BridgeExecutor` não foi alterado para `engine.Engine`.
