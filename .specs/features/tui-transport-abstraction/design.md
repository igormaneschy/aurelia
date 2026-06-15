# TUI — Transport Abstraction (Fase 0) — Design

**Spec**: `.specs/features/tui-transport-abstraction/spec.md`
**Status**: Approved

---

## Architecture Overview

O objetivo é transformar `pipeline.Output` em uma interface transport-agnóstica e introduzir uma implementação genérica (`transportOutput`) que funcione sobre qualquer `transport.Transport`. O `telegramPipelineOutput` vira um thin adapter que delega o máximo possível para `TelegramTransport`.

```text
┌─────────────────┐         ┌──────────────────────┐
│   pipeline.Service │──────▶│   pipeline.Output    │
│                 │         │  (transport-agnostic)│
└─────────────────┘         └──────────────────────┘
                                       │
            ┌──────────────────────────┼──────────────────────────┐
            │                          │                          │
            ▼                          ▼                          ▼
   ┌─────────────────┐      ┌─────────────────────┐      ┌─────────────────┐
   │  telegramPipeline│      │   transportOutput   │      │  future TUI     │
   │     Output       │      │      (new)          │      │     Output      │
   └────────┬─────────┘      └──────────┬──────────┘      └─────────────────┘
            │                           │
            ▼                           ▼
   ┌─────────────────┐      ┌─────────────────────┐
   │ TelegramTransport│      │  any Transport impl │
   │  (implements     │      │   (e.g. future TUI) │
   │   Transport +    │      └─────────────────────┘
   │ DeletableTransport│
   │ ReactableTransport)│
   └─────────────────┘
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
|-----------|----------|------------|
| `transport.Transport` | `internal/transport/transport.go` | Base interface; estender com `MessageHandle` e capacidades opcionais |
| `TelegramTransport` | `internal/telegram/transport.go` | Implementar `Send` com retorno de handle, `Delete` e `React` |
| `pipeline.Output` | `internal/pipeline/service.go` | Refatorar para tipos genéricos |
| `telegramPipelineOutput` | `internal/telegram/pipeline.go` | Vira thin adapter |
| `MockTransport` | `internal/transport/transport_test.go` | Atualizar para novas assinaturas |

### Integration Points

| System | Integration Method |
|--------|-------------------|
| `pipeline.Service` | Recebe `Output` via DI; nenhuma mudança de wiring inicial |
| `BotController.ensurePipeline` | Continua criando `telegramPipelineOutput` + `TelegramTransport` |
| Future TUI | Poderá criar `transportOutput` com `TUITransport` |

---

## Components

### `MessageHandle`

- **Purpose**: Referência opaca a uma mensagem enviada, usada para deleção posterior.
- **Definition**: `type MessageHandle any`
- **Notes**: O pipeline não inspeciona o conteúdo. Cada transporte define seu próprio tipo real (ex: `*telebot.Message` para Telegram).

### `DeletableTransport` (optional capability)

```go
type DeletableTransport interface {
    Transport
    Delete(ctx context.Context, handle MessageHandle) error
}
```

### `ReactableTransport` (optional capability)

```go
type ReactableTransport interface {
    Transport
    React(ctx context.Context, chatID int64, messageID int) error
}
```

### `Transport.Send` (extended)

```go
Send(ctx context.Context, msg OutgoingMessage) (MessageHandle, error)
```

### `transportOutput`

- **Purpose**: Implementação genérica de `pipeline.Output` sobre `transport.Transport`.
- **Location**: `internal/pipeline/transport_output.go`
- **Behavior**:
  - `StartTyping` → `tp.StartTyping`
  - `NewProgress` → retorna `noopPipelineProgress` (a TUI futura pode injetar seu próprio)
  - `SendError` → `tp.SendError`
  - `SendReply` → `tp.Send` com `Markdown: true`; retorna `0` como messageID (runlog não grava outbound para TUI genérica)
  - `SendText` → `tp.Send` com `Markdown: false`; retorna `MessageHandle`
  - `DeleteMessage` → type assertion para `DeletableTransport`; se não, no-op
  - `ConfirmMessage` → type assertion para `ReactableTransport`; se não, no-op
  - `ExecuteApprovedPlan` → no-op (logging opcional)

### `telegramPipelineOutput` (refatorado)

- **Purpose**: Thin adapter que reusa `TelegramTransport` para tudo que for genérico.
- **Location**: `internal/telegram/pipeline.go`
- **Behavior**:
  - Embute ou delega para `transportOutput`.
  - `NewProgress` cria `progressReporter` do Telegram (específico).
  - `ExecuteApprovedPlan` chama `BotController.executeApprovedPlan` (específico).

---

## Data Models

Nenhum modelo novo de persistência. Tipos alterados:

| Type | Location | Change |
|------|----------|--------|
| `MessageHandle` | `internal/transport/transport.go` | Novo: `any` |
| `Transport.Send` | `internal/transport/transport.go` | Retorna `(MessageHandle, error)` |
| `DeletableTransport` | `internal/transport/transport.go` | Nova interface opcional |
| `ReactableTransport` | `internal/transport/transport.go` | Nova interface opcional |
| `Output.SendText` | `internal/pipeline/service.go` | Retorna `(MessageHandle, error)` |
| `Output.DeleteMessage` | `internal/pipeline/service.go` | Recebe `MessageHandle` |

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
|---|---|---|
| `Transport.Send` retorna erro | Loga e continua; pipeline já trata assim hoje | Sem impacto novo |
| `DeleteMessage` com handle de tipo inesperado | Loga warning e ignora | Nenhum — deleção é best-effort |
| `ConfirmMessage` sem `ReactableTransport` | Silently no-op | TUI não terá confirmação visual; aceitável para MVP |
| `ExecuteApprovedPlan` em `transportOutput` | No-op com log | TUI não executa planos no MVP |

---

## Tech Decisions

| Decision | Choice | Rationale |
|---|---|---|
| `MessageHandle` como `any` | Sim | Mínimo de boilerplate; transportes definem seu próprio tipo real |
| Capabilities via type assertion | `DeletableTransport` / `ReactableTransport` | Evita forçar todos os transportes a implementarem stubs vazios |
| `transportOutput` genérico | Nova struct em `internal/pipeline` | Reuso direto pela futura TUI sem duplicar lógica |
| `telegramPipelineOutput` mantido | Thin adapter | Preserva comportamentos Telegram específicos sem poluir a interface base |
| `SendReply` retorna `int64` | Mantido | Runlog precisa do `outbound_message_id` no Telegram |
