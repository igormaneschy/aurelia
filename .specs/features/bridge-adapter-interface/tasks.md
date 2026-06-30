# Bridge Adapter Interface — Tasks

**Track:** `.specs/features/multi-sdk/` Phase **B** — ver `multi-sdk/tasks.md` para DAG completo.

## Pré-requisitos

- [ ] Rever `internal/bridge/bridge.go`, `protocol.go`, `events.go` para confirmar todos os campos usados pelo pipeline
- [ ] Rever `internal/pipeline/pipeline.go` e `service.go` para listar todos os call sites de `bridge.Request`
- [ ] Confirmar import graph actual: `go mod graph` ou `goimports -v`

---

## Task 1 — Criar `internal/engine/engine.go`

**Ficheiro**: `internal/engine/engine.go`  
**Dependências**: nenhuma interna  
**Risco**: Zero — ficheiro novo

- [ ] Criar package `engine`
- [ ] Definir `type Engine interface` com métodos `Query`, `Command`, `Stats`
- [ ] Definir `EventType` e constantes: `EventTypeText`, `EventTypeToolUse`, `EventTypeToolResult`, `EventTypeDone`, `EventTypeError`, `EventTypeSystem`
- [ ] Definir structs: `Event`, `ToolEvent`, `DoneEvent`, `Request`, `Command`, `Image`, `SecurityPolicy`, `Stats`
- [ ] Adicionar comentários de documentação em todos os tipos exportados
- [ ] Verificar: `go vet ./internal/engine/...` passa
- [ ] Verificar: `grep -r 'internal/bridge' internal/engine/` → zero resultados

---

## Task 2 — Criar `internal/engine/noop.go`

**Ficheiro**: `internal/engine/noop.go`  
**Dependências**: `engine.go` (Task 1)  
**Risco**: Zero — ficheiro novo

- [ ] Definir `NoopEngine` que implementa `Engine`:
  - `Query` → devolve canal fechado imediatamente
  - `Command` → devolve zero-value Event sem erro
  - `Stats` → devolve zero-value Stats sem erro
- [ ] Definir `MockEngine` com campos configuráveis:
  - `QueryResponses []Event` — emitidos em sequência por `Query`
  - `CommandResponses map[string]Event` — por nome de comando
  - `Calls []RecordedCall` — registo de chamadas para assertions
- [ ] Definir `RecordedCall` com campos `Method string`, `Request *Request`, `Command *Command`
- [ ] Escrever `engine_test.go` com testes de `MockEngine` e `NoopEngine`

---

## Task 3 — Criar `internal/bridge/adapter.go`

**Ficheiro**: `internal/bridge/adapter.go`  
**Dependências**: `internal/engine` (Task 1), `internal/bridge` (existente)  
**Risco**: Baixo — ficheiro novo, sem alterar código existente

- [ ] Definir `type PIAdapter struct { b *Bridge }`
- [ ] Implementar `NewPIAdapter(b *Bridge) *PIAdapter`
- [ ] Implementar `PIAdapter.Query` — `toRequest` + `b.Execute` + goroutine de tradução `toEngineEvent`
- [ ] Implementar `PIAdapter.Command` — construir `bridge.Request` directo + `b.ExecuteSync` + `toEngineEvent`
- [ ] Implementar `PIAdapter.Stats` — `b.GetSessionStats` + mapear para `engine.Stats`
- [ ] Implementar `toRequest(engine.Request) bridge.Request` (ver tabela de mapeamento em `design.md`)
- [ ] Implementar `toEngineEvent(bridge.Event) engine.Event` (ver tabela de mapeamento em `design.md`)
- [ ] Tratar nil-safety: `ev.Input == nil` → `Tool.Input = "null"` sem panic
- [ ] Tratar ev.Message vazio em error event → fallback para `ev.Content` → fallback para `"unknown bridge error"`
- [ ] Escrever `bridge/adapter_test.go` com:
  - Teste de mapeamento de cada tipo de evento (text, tool_use, tool_result, result, error, system)
  - Teste de images populadas correctamente
  - Teste de security context: presente e ausente
  - Teste de `Stats` com `ss == nil` (nil-safe)
  - Teste de `Command` para abort e steer

---

## Task 4 — Migrar `pipeline/resilient_bridge.go`

**Ficheiro**: `internal/pipeline/resilient_bridge.go`  
**Dependências**: Task 3  
**Risco**: Médio — altera interface usada por toda a pipeline

- [ ] Substituir `type BridgeExecutor interface { Execute(...) }` por `engine.Engine`
- [ ] Alterar campo `bridge engine.Engine` no `ResilientBridge`
- [ ] Se `validateChannel` / `proxyChannel` usam `bridge.Event` → migrar para `engine.Event`
- [ ] Actualizar `executeWithRetry` e `tryFallback` para `engine.Event`
- [ ] Actualizar `resilient_bridge_test.go`: `FakeBridge` → `engine.MockEngine`
- [ ] Verificar: `go test ./internal/pipeline/... -run TestResilient -short` passa

---

## Task 5 — Migrar `pipeline/pipeline.go`

**Ficheiro**: `internal/pipeline/pipeline.go`  
**Dependências**: Task 4  
**Risco**: Médio-alto — maior número de call sites

- [ ] Listar todos os `bridge.Request{...}` construídos no ficheiro
- [ ] Substituir construção de query requests por `engine.Request{...}`
- [ ] Substituir `bridge.Request{Command: "abort"}` por `engine.Command{Name: "abort"}`
- [ ] Substituir `bridge.Request{Command: "steer", Prompt: ...}` por `engine.Command{Name: "steer", Payload: ...}`
- [ ] Verificar: todos os `bridge.Event` no código de produção substituídos por `engine.Event`
- [ ] Verificar: `grep 'bridge\.Request' internal/pipeline/pipeline.go` → zero hits

---

## Task 6 — Migrar wiring em `service.go`

**Ficheiro**: `internal/pipeline/service.go` (ou equivalente de construção)  
**Dependências**: Task 5  
**Risco**: Baixo — só wiring

- [ ] Localizar onde `*bridge.Bridge` é criado e passado para o pipeline
- [ ] Envolver com `bridge.NewPIAdapter(b)` antes de passar
- [ ] Garantir que o tipo passado para `NewResilientBridge` é `engine.Engine`
- [ ] O import de `internal/bridge` no service.go é permitido apenas para instanciar o adapter

---

## Task 7 — Verificação final

- [ ] `go build ./...` — zero erros
- [ ] `go test ./... -short` — zero falhas
- [ ] `grep -r 'bridge\.Request' internal/pipeline/*.go` → zero resultados (excluir _test.go)
- [ ] `grep -r 'bridge\.Event' internal/pipeline/*.go` → zero resultados (excluir _test.go)
- [ ] `go mod tidy` — sem mudanças em `go.mod`/`go.sum` (nenhuma nova dependência)
- [ ] Actualizar `ARCHITECTURE.md` com secção sobre `engine.Engine` e a regra de dependência

---

## Ordem de execução recomendada

```
Task 1 → Task 2 → Task 3 → Task 4 → Task 5 → Task 6 → Task 7
```

Cada task compila e testa antes de avançar. Tasks 1–3 são de risco zero (ficheiros novos). O risco concentra-se em Tasks 4–5 e deve ser feito com testes a verde antes de começar.

## Branch e PR

- Branch: `refactor/bridge-adapter-interface`
- Title: `refactor(engine): introduce Engine interface and PIAdapter — decouple pipeline from PI protocol`
- Base: `main`
- PR body: uma secção por task com ficheiros alterados e resultado dos testes
