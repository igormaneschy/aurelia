# Bridge Adapter Interface — Especificação

**Status:** Draft — Junho 2026

## Problem Statement

O Aurelia usa actualmente o PI SDK como único motor de raciocínio. O pipeline (`internal/pipeline/`) comunica directamente com `internal/bridge/`, usando os tipos concretos `bridge.Request` e `bridge.Event` em todos os call sites. Existem ~15 lugares no pipeline que constroem `bridge.Request{Command: "..."}` directamente.

O problema não é o PI SDK em si — é a ausência de costura. Não existe uma fronteira explícita entre "o que o pipeline precisa de um motor" e "como o PI implementa esse motor". Isto significa:

1. **Acoplamento de protocolo**: O protocolo NDJSON do PI (campos `session_file`, `resume`, `command`) vaza para fora de `internal/bridge/`. O pipeline sabe detalhes do transport.
2. **Testabilidade degradada**: Os testes do pipeline que usam `FakeBridge` têm de simular o protocolo PI completo. Um `MockEngine` com contrato mínimo seria muito mais simples.
3. **Bloqueio para multi-engine**: Quando se quiser usar um segundo SDK (ex: API nativa do Anthropic sem PI, um agente local, ou um stub de desenvolvimento), será necessário tocar em dezenas de call sites no pipeline em vez de criar um novo adapter.
4. **`input any` prolifera**: O campo `bridge.Event.Input` é `any` (deserializado como `map[string]interface{}`). O pipeline faz `json.Marshal(ev.Input)` em múltiplos sítios. Esse problema devia ser resolvido na fronteira, não espalhado.

O momento certo para criar esta abstracção é **agora**, enquanto o código tem um único implementador e o refactor ainda é cirúrgico. Após o Sprint de Agent Comms, cada agente vai querer o seu próprio motor — e nessa altura a abstracção será obrigatória, mas muito mais cara.

## Goals

- [ ] Criar `internal/engine/` com interface `Engine` e tipos `Request`, `Event`, `Command`, `Stats` independentes do PI
- [ ] Criar `internal/bridge/adapter.go` com `PIAdapter` que implementa `engine.Engine` sobre `*Bridge`
- [ ] `PIAdapter` é o único lugar onde `bridge.Request`/`bridge.Event` são construídos a partir de tipos `engine.*`
- [ ] O pipeline (`pipeline.go`, `resilient_bridge.go`, `service.go`) deixa de importar `internal/bridge` directamente para execução — usa `engine.Engine`
- [ ] `BridgeExecutor` em `resilient_bridge.go` é substituído por `engine.Engine`
- [ ] `MockEngine` em `internal/engine/noop.go` substitui `FakeBridge` nos testes do pipeline
- [ ] `go build ./...` e `go test ./... -short` passam sem regressões

## Out of Scope

- Implementar um segundo SDK/adapter concreto (este spec cria apenas a costura; o segundo adapter virá noutro sprint)
- Alterar o protocolo NDJSON do PI ou o `bundle.ts`
- Mover lógica de session lifecycle (rotate, compact) para fora do bridge
- Alterar `app.json` ou configuração de runtime
- Qualquer mudança na UI/Telegram

---

## User Stories

### P1: Engine Interface e Tipos — MVP

**User Story**: Como engenheiro, quero um contrato `engine.Engine` que expresse o que o pipeline precisa de qualquer motor, sem mencionar o PI.

**Why P1**: Sem a interface, nada do resto funciona. É a fundação.

**Acceptance Criteria**:

1. WHEN `internal/engine/engine.go` existe THEN ele SHALL exportar a interface `Engine` com exactamente 3 métodos: `Query`, `Command`, `Stats`
2. WHEN `Query` é chamado com um `engine.Request` THEN devolve `(<-chan engine.Event, error)` — canal fechado quando o motor termina
3. WHEN `Command` é chamado com um `engine.Command` THEN executa de forma síncrona e devolve `(engine.Event, error)` — adequado para abort, steer, compact, rotate
4. WHEN `Stats` é chamado com uma sessionKey THEN devolve `(engine.Stats, error)` com zero-value sem erro se a sessão não existir
5. WHEN `internal/engine/engine.go` é importado por qualquer package THEN ele SHALL NOT importar `internal/bridge` — dependência zero

**Independent Test**: `go vet ./internal/engine/...` passa. Import graph: `engine` não importa `bridge`.

---

### P1: PIAdapter implementa Engine — MVP

**User Story**: Como engenheiro, quero que `PIAdapter` traduza de forma fiel entre `engine.Request`/`engine.Event` e o protocolo NDJSON do PI, sem perder informação.

**Why P1**: Sem o adapter, o pipeline não pode usar a interface.

**Acceptance Criteria**:

1. WHEN `PIAdapter.Query` recebe um `engine.Request` THEN SHALL construir um `bridge.Request{Command: "query"}` com todos os campos mapeados correctamente (ver tabela de mapeamento em `design.md`)
2. WHEN `PIAdapter.Query` recebe imagens THEN SHALL popular `bridge.RequestOptions.Images` com o slice `[]ImageAttachment` correcto
3. WHEN `PIAdapter.Query` recebe `engine.Request.Security != nil` THEN SHALL construir `bridge.SecurityContext` a partir de `engine.SecurityPolicy`
4. WHEN `PIAdapter.toEngineEvent` recebe `bridge.Event{Type: "tool_use"}` THEN SHALL marshalar `ev.Input` (any) para JSON string e devolver `engine.Event{Type: EventTypeToolUse, Tool: &ToolEvent{Name: ev.Name, Input: <json_string>}}`
5. WHEN `PIAdapter.toEngineEvent` recebe `bridge.Event{Type: "result"}` THEN SHALL popular `engine.Event.Done` com InputTokens, OutputTokens, CostUSD, DurationMs, NumTurns
6. WHEN `PIAdapter.toEngineEvent` recebe `bridge.Event{Type: "error"}` THEN SHALL devolver `engine.Event{Type: EventTypeError, Err: fmt.Errorf("%s", ev.Message)}`
7. WHEN `PIAdapter.Command` é chamado com `cmd.Name == "abort"` THEN SHALL construir e enviar o `bridge.Request{Command: "abort"}` equivalente via `ExecuteSync`
8. WHEN `PIAdapter.Stats` é chamado THEN SHALL chamar `b.GetSessionStats` e mapear `SessionStats` para `engine.Stats` — devolver zero-value sem erro se `ss == nil`

**Independent Test**: Testes unitários em `bridge/adapter_test.go` com `*Bridge` mockado. Cobrem: mapeamento de cada tipo de evento, images, security, Stats nil-safe.

---

### P1: Pipeline usa engine.Engine — MVP

**User Story**: Como engenheiro, quero que o pipeline fale exclusivamente com `engine.Engine`, sem importar `internal/bridge` para execução.

**Why P1**: É a mudança que fecha o loop — sem ela o adapter existe mas não é usado.

**Acceptance Criteria**:

1. WHEN `resilient_bridge.go` é alterado THEN `BridgeExecutor` SHALL ser substituído por `engine.Engine` como tipo da dependência
2. WHEN `pipeline.go` e `service.go` constroem requests THEN SHALL usar `engine.Request` em vez de `bridge.Request`
3. WHEN o pipeline precisa de fazer abort/steer THEN SHALL chamar `engine.Engine.Command(ctx, engine.Command{Name: "abort", ...})` em vez de `bridge.Request{Command: "abort"}`
4. WHEN `pipeline_test.go` e `resilient_bridge_test.go` precisam de um bridge fake THEN SHALL usar `engine.MockEngine` (ou `engine.NoopEngine`) em vez de `FakeBridge`
5. WHEN `go build ./internal/pipeline/...` é executado THEN SHALL compilar sem referências directas a `bridge.Request` ou `bridge.Event` no código de produção do pipeline

**Independent Test**: `grep -r 'bridge\.Request' internal/pipeline/` devolve zero resultados em ficheiros não-test após a migração.

---

### P2: NoopEngine para testes — Should Have

**User Story**: Como engenheiro que escreve testes, quero um `MockEngine` configurável em `internal/engine/` que simule respostas sem processar nada.

**Why P2**: Reduz a fricção de escrever testes novos no pipeline. FakeBridge actual mistura protocolo PI com lógica de teste.

**Acceptance Criteria**:

1. WHEN `engine.MockEngine` é construído com `WithQueryResponse(events []engine.Event)` THEN `Query` devolve um canal pré-carregado com esses eventos
2. WHEN `engine.MockEngine` é construído com `WithCommandResponse(cmd string, ev engine.Event)` THEN `Command` devolve esse evento para o comando especificado
3. WHEN `engine.MockEngine.Query` é chamado THEN regista a chamada em `Calls []engine.RecordedCall` para assertions nos testes
4. WHEN `engine.NoopEngine` é usado (zero-value) THEN `Query` devolve canal fechado imediatamente, `Command` e `Stats` devolvem zero-value — útil para testes que não precisam de resposta

**Independent Test**: `go test ./internal/engine/...` cobre MockEngine e NoopEngine.

---

## Edge Cases

- WHEN `engine.Request.SessionKey` é string vazia THEN `PIAdapter` SHALL NOT popular `bridge.RequestOptions.Resume` (evitar resume acidental)
- WHEN `bridge.Event.Input` é nil THEN `json.Marshal(nil)` devolve `"null"` — o adapter SHALL devolver `Tool.Input = "null"` sem erro (comportamento correcto)
- WHEN `PIAdapter.Command` recebe `cmd.Name` desconhecido pelo PI THEN o PI devolve `bridge.Event{Type: "error"}` — o adapter SHALL devolvê-lo como `engine.Event{Type: EventTypeError}` sem panic
- WHEN o pipeline migra mas `ResilientBridge` ainda wrappa `engine.Engine` THEN o `ResilientBridge.bridge` field muda de tipo `BridgeExecutor` para `engine.Engine` — os métodos `validateChannel`, `proxyChannel` continuam a trabalhar com `bridge.Event` se o canal interno ainda for `<-chan bridge.Event`; se o canal migrar para `<-chan engine.Event` os helpers precisam de ser actualizados
- WHEN `engine.Stats.Turns` é mapeado a partir de `SessionStats.UserMessages` THEN o campo é semanticamente correcto (um turn = uma mensagem do user) mas o comentário deve documentar a proveniência

---

## Success Criteria

- [ ] `internal/engine/` existe com interface `Engine`, tipos completos, `MockEngine`, `NoopEngine`
- [ ] `internal/bridge/adapter.go` existe com `PIAdapter` que implementa `engine.Engine`
- [ ] Import graph: `engine` não importa `bridge`; `bridge` importa `engine` apenas em `adapter.go`
- [ ] Pipeline não importa `internal/bridge` para construção de requests (só `engine.*`)
- [ ] `grep -r 'bridge\.Request' internal/pipeline/*.go` devolve zero resultados
- [ ] `go build ./...` passa sem erros
- [ ] `go test ./... -short` passa sem regressões
- [ ] Testes de `adapter_test.go` cobrem todos os tipos de evento e os edge cases de nil
