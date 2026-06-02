# Bridge Adapter Interface — Validation

**Status:** Not yet validated

## Validation Checklist

Este documento será preenchido após implementação e PR review.

### Structural

- [ ] `internal/engine/` criado com `engine.go` e `noop.go`
- [ ] `internal/bridge/adapter.go` criado com `PIAdapter`
- [ ] Import graph correcto: `engine` não importa `bridge`
- [ ] `go build ./...` passa
- [ ] `go test ./... -short` passa

### Behavioural

- [ ] Todos os tipos de evento PI mapeados em `toEngineEvent` (text, tool_use, tool_result, result, error, system, outros)
- [ ] `ev.Input any` → `json.Marshal` → `string` sem panic para nil, map, string
- [ ] `Stats` nil-safe quando sessão não existe
- [ ] `Command` para abort, steer, compact, rotate funciona via `ExecuteSync`
- [ ] Images presentes no `engine.Request` chegam ao `bridge.Request` correctamente
- [ ] Security context ausente não gera nil pointer no adapter

### Pipeline

- [ ] `grep -r 'bridge\.Request' internal/pipeline/*.go` → zero resultados
- [ ] `grep -r 'bridge\.Event' internal/pipeline/*.go` → zero resultados
- [ ] `resilient_bridge_test.go` usa `engine.MockEngine`, não `FakeBridge`
- [ ] `pipeline_test.go` usa `engine.MockEngine`, não `FakeBridge`

### Regression

- [ ] Teste de abort cancela a execução correctamente
- [ ] Teste de steer injeta prompt no turn corrente
- [ ] Teste de fallback (ResilientBridge) funciona com MockEngine
- [ ] Teste de circuit breaker funciona com MockEngine
- [ ] Session lifecycle (rotate, compact) funciona via Command

## Notas de Validação

*(preenchido após implementação)*
