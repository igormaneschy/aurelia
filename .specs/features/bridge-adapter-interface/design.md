# Bridge Adapter Interface — Design

## Contexto

Este documento descreve as decisões de design, diagramas de estrutura, e a tabela de mapeamento de campos entre `engine.*` e `bridge.*`.

## Estrutura de Packages

```
internal/
  engine/                  ← NOVO — contrato puro, zero dependências internas
    engine.go              ← interface Engine + todos os tipos públicos
    noop.go                ← NoopEngine + MockEngine para testes
  bridge/                  ← existente — transport NDJSON para o PI SDK
    adapter.go             ← NOVO — PIAdapter implements engine.Engine
    bridge.go              ← sem mudanças de comportamento
    protocol.go            ← Request/Event ficam aqui; são tipos internos do adapter
    events.go              ← sem mudanças
  pipeline/                ← existente — usa engine.Engine, não bridge.*
    resilient_bridge.go    ← BridgeExecutor → engine.Engine
    pipeline.go            ← bridge.Request → engine.Request
    service.go             ← bridge.Request → engine.Request (onde aplicável)
```

## Regra de Dependência

```
  pipeline  →  engine  ←  bridge/adapter
                ↑
           (interface)

  pipeline NÃO importa bridge (excepto setup.go para construir o PIAdapter)
  bridge/adapter importa engine (para implementar a interface)
  engine NÃO importa nada interno
```

O único lugar onde `pipeline` ainda pode importar `bridge` é em `setup.go` (ou equivalente de wiring) para fazer `engine.Engine(bridge.NewPIAdapter(b))`. Em runtime, o pipeline fala apenas com `engine.Engine`.

## Interface Engine

```go
package engine

import "context"

type Engine interface {
    Query(ctx context.Context, req Request) (<-chan Event, error)
    Command(ctx context.Context, cmd Command) (Event, error)
    Stats(ctx context.Context, sessionKey string) (Stats, error)
}
```

### Decisão: porquê 3 métodos e não 1

Alternativa considerada: `Execute(ctx, Request) (<-chan Event, error)` com `Request.Command` para distinguir streaming de síncrono (como `bridge.Request` actual).

Rejeitada porque:
- O type system deixa de garantir que chamadas de lifecycle (`abort`, `steer`) não são passadas para o path de streaming — o compilador não ajuda
- `Command` ser síncrono (`Event`, não `<-chan Event`) é semanticamente correcto e simplifica o código do pipeline
- `Stats` com sessionKey explícito documenta a intenção melhor que `Request{Command: "get-session-stats", Options: {Resume: key}}`

## Tipos Públicos

```go
// engine.go

type EventType string
const (
    EventTypeText       EventType = "text"
    EventTypeToolUse    EventType = "tool_use"
    EventTypeToolResult EventType = "tool_result"
    EventTypeDone       EventType = "done"
    EventTypeError      EventType = "error"
    EventTypeSystem     EventType = "system"
)

type Event struct {
    Type    EventType
    Content string
    Tool    *ToolEvent  // preenchido se Type == ToolUse ou ToolResult
    Done    *DoneEvent  // preenchido se Type == Done
    Err     error       // preenchido se Type == Error
}

type ToolEvent struct {
    Name   string  // nome da tool (tool_use)
    Input  string  // JSON string do input (tool_use) — never any
    Output string  // conteúdo do resultado (tool_result)
}

type DoneEvent struct {
    InputTokens  int
    OutputTokens int
    Turns        int
    Cost         float64
    DurationMs   int64
}

type Request struct {
    Prompt       string
    SystemPrompt string
    SessionKey   string            // opaco — o adapter decide o mapeamento
    Provider     string
    Model        string
    Cwd          string
    AllowedTools []string
    Images       []Image
    Security     *SecurityPolicy
    ChatID       int64
    ThreadID     int
    UserID       int64
    Metadata     map[string]string // extensível sem quebrar a interface
}

type Command struct {
    Name     string  // "abort" | "steer" | "compact" | "rotate" | "get-stats"
    Payload  string  // steer prompt, session file, etc.
    ChatID   int64
    ThreadID int
    UserID   int64
}

type Image struct {
    Data      string
    MediaType string
    Path      string
}

type SecurityPolicy struct {
    Enabled        bool
    Profile        string
    Mode           string
    SensitivePaths []string
}

type Stats struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
    Turns        int     // mapeado de SessionStats.UserMessages
    Cost         float64
    ContextPct   float64
}
```

## Tabela de Mapeamento: engine.Request → bridge.Request

| engine.Request          | bridge.Request / RequestOptions       | Notas                                                    |
|-------------------------|---------------------------------------|----------------------------------------------------------|
| `Prompt`                | `Request.Prompt`                      | Directo                                                  |
| `SystemPrompt`          | `Options.SystemPrompt`                | Directo                                                  |
| `SessionKey`            | `Options.Resume`                      | PI usa path de ficheiro como session key                 |
| `Provider`              | `Options.Provider`                    | Directo                                                  |
| `Model`                 | `Options.Model`                       | Directo                                                  |
| `Cwd`                   | `Options.Cwd`                         | Directo                                                  |
| `AllowedTools`          | `Options.AllowedTools`                | Directo                                                  |
| `Images[*].Data`        | `Options.Images[*].Data`              | Directo                                                  |
| `Images[*].MediaType`   | `Options.Images[*].MediaType`         | Directo                                                  |
| `Images[*].Path`        | `Options.Images[*].Path`              | Directo                                                  |
| `Security.Enabled`      | `Options.Security.Enabled`            | Directo                                                  |
| `Security.Profile`      | `Options.Security.Profile`            | Directo                                                  |
| `Security.Mode`         | `Options.Security.Mode`               | Directo                                                  |
| `Security.SensitivePaths` | `Options.Security.SensitivePaths`   | Directo                                                  |
| `ChatID`                | `Options.Security.ChatID`             | Só populado se Security != nil                          |
| `ThreadID`              | `Options.Security.ThreadID`           | Só populado se Security != nil                          |
| `UserID`                | `Options.Security.UserID`             | Só populado se Security != nil                          |
| `Metadata["agent_name"]`| `Options.Security.AgentName`          | Extraído de Metadata; só se Security != nil             |
| *(implícito)*           | `Request.Command = "query"`           | O adapter sempre usa command query para Query()         |

## Tabela de Mapeamento: bridge.Event → engine.Event

| bridge.Event.Type | engine.Event.Type   | Campos mapeados                                                                 |
|-------------------|---------------------|---------------------------------------------------------------------------------|
| `assistant`       | `EventTypeText`     | `Content = ev.Content`                                                          |
| `tool_use`        | `EventTypeToolUse`  | `Tool.Name = ev.Name`, `Tool.Input = json.Marshal(ev.Input)`                  |
| `tool_result`     | `EventTypeToolResult` | `Tool.Output = ev.Content`                                                    |
| `result`          | `EventTypeDone`     | `Done.InputTokens`, `Done.OutputTokens`, `Done.Cost = ev.CostUSD`, `Done.DurationMs`, `Done.Turns = ev.NumTurns` |
| `error`           | `EventTypeError`    | `Err = fmt.Errorf("%s", ev.Message ?? ev.Content ?? "unknown bridge error")`  |
| `system`          | `EventTypeSystem`   | `Content = ev.SessionFile`, `Tool.Name = ev.SessionID` (session metadata)     |
| outros            | `EventTypeSystem`   | `Content = ev.Content` (compaction, pong, get_state — passthrough genérico)   |

### Decisão: porquê `Tool.Name = ev.SessionID` no system event

O pipeline precisa do `session_id` do event `system` para actualizar o estado da sessão. Reusar `ToolEvent.Name` evita adicionar um campo `SessionID string` ao topo de `engine.Event` apenas para este caso. A alternativa seria um campo `Metadata map[string]string` no `Event` — mais genérico mas mais pesado. Revisitar se surgir um terceiro uso de metadata no event.

## Migration Path — Strangler Fig

```
Passo 1  Criar internal/engine/engine.go + noop.go
         Zero impacto no código existente.

Passo 2  Criar internal/bridge/adapter.go (PIAdapter)
         Zero impacto no pipeline.

Passo 3  pipeline/resilient_bridge.go:
           - BridgeExecutor → engine.Engine
           - Todos os usos de bridge.Event no pipeline → engine.Event
         pipeline/pipeline.go:
           - Substituir bridge.Request{Command: ...} por engine.Command{Name: ...}
           - Substituir bridge.Request{...query fields} por engine.Request{...}

Passo 4  Migrar testes:
           - FakeBridge → engine.MockEngine
           - Remover imports de bridge nos _test.go do pipeline

Passo 5  Verificação:
           - go build ./...
           - go test ./... -short
           - grep -r 'bridge\.Request' internal/pipeline/*.go → zero hits
```

### Ficheiros afectados no Passo 3 (estimativa)

| Ficheiro                              | Tipo de mudança                         |
|---------------------------------------|-----------------------------------------|
| `pipeline/resilient_bridge.go`        | `BridgeExecutor` → `engine.Engine`; tipos de canal |
| `pipeline/pipeline.go`                | `bridge.Request` → `engine.Request`; `bridge.Event` → `engine.Event` |
| `pipeline/service.go`                 | Wiring: `NewPIAdapter(b)` passa o adapter |
| `pipeline/tool_monitoring.go`         | Se referencia `bridge.Event` → `engine.Event` |
| `pipeline/pipeline_test.go`           | `FakeBridge` → `engine.MockEngine`      |
| `pipeline/resilient_bridge_test.go`   | `FakeBridge` → `engine.MockEngine`      |

## Porquê não abstrair agora o session lifecycle (rotate, compact)

Os métodos `RotateSession`, `CompactSession`, `ListModels` em `bridge.go` são específicos do PI. Não existem no conceito genérico de `engine.Engine` porque:
- Um SDK nativo da Anthropic não tem `rotate-session` — tem context windows nativamente
- Forçar esses métodos na interface obrigaria qualquer implementador futuro a implementar stubs vazios
- O pipeline chama esses métodos via `Command(ctx, engine.Command{Name: "rotate-session", ...})` — o adapter traduz para `RotateSession`; um adapter diferente pode ignorar ou mapear diferente

Esta decisão mantém a interface mínima e o adapter como único lugar de decisões PI-específicas.
