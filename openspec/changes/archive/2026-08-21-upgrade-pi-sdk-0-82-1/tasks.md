# Upgrade PI SDK 0.79.2 → 0.82.1 — Tasks

**Change:** `upgrade-pi-sdk-0-82-1`
**Dependency graph:** `T0 → T1 → T2 → T3 → T4 → T5`
**Implementation branch:** `feature/upgrade-pi-sdk-0-82-1`
**Terminal boundary:** validação live no daemon; promoção/release ficam fora desta change até aprovação explícita.

## T0 — Baseline and preflight

- [x] Criar branch dedicada a partir do `main` atualizado; não trabalhar
      diretamente em `main`.
- [x] Confirmar Node `>=22.19.0` no desenvolvimento, serviço e CI.
- [x] Registrar baseline `0.79.2`: startup, `/model`, query, tool read-only,
      MCP/web extension, resume, compaction e erro de provider.
- [x] Capturar fixture JSONL redigida de sessão `0.79.2`.
- [x] Inspecionar tipos e runtime publicados de `0.82.1` para confirmar:
      `ModelRuntime`, `createAgentSession`, `beforeToolCall`,
      `SessionManager`, `SettingsManager` e eventos.
- [x] Confirmar symlinks de `auth.json` e `models.json` sem ler ou registrar
      segredos.
- [x] Parar e reportar blocker se Node, tipos, fixture ou baseline não forem
      obtidos.

**Validation:** baseline documentado, API surface confirmada em `dist/*.d.ts`
e blocker protocol satisfeito.

## T1 — Dependencies and package contract

- [x] Atualizar `bridge/package.json` para PI `0.82.1`.
- [x] Regenerar `bridge/package-lock.json` de forma reproduzível.
- [x] Confirmar `protobufjs >=7.6.5` no lockfile.
- [x] Atualizar as versões PI no template `bridgePackageJSON` em
      `internal/bridge/setup.go`.
- [x] Atualizar `internal/bridge/setup_test.go` para verificar versão exata,
      coerência entre os dois pacotes e engine Node.
- [x] Confirmar que `models-store.json` permanece isolado e não é apagado ou
      transformado em symlink automaticamente.

**Validation:** instalação limpa, inspeção do lockfile, typecheck e testes de
setup passam.

## T2 — Bridge migration to ModelRuntime

- [x] Remover import e criação de `AuthStorage`.
- [x] Remover criação de `ModelRegistry` para o caminho de sessão.
- [x] Criar `ModelRuntime` com `authPath`/`modelsPath` do agent dir isolado.
- [x] Passar `modelRuntime` para `createAgentSession`.
- [x] Migrar resolução para `getModel` + fallback por ID exato em `getModels`.
- [x] Preservar apenas aliases `mapProvider` e `mapModelForProvider` existentes.
- [x] Migrar `list-models` para runtime, `getAvailable()` e refresh explícito
      assíncrono.
- [x] Preservar formato NDJSON e erros claros de modelo inexistente.
- [x] Não introduzir fuzzy matching, singleton global ou mudança de protocolo.

**Validation:** `bridge/index.ts` e `bridge/bundle.ts` não contêm uso de
`AuthStorage`; typecheck, testes Bridge e build passam.

## T3 — Compatibility and regression tests

- [x] Testar resolução qualificada, aliases válidos e modelo inexistente.
- [x] Testar que listagem e query usam o mesmo catálogo/runtime.
- [x] Testar abertura da fixture de sessão `0.79.2` sem perda de mensagens,
      timestamps, model ou thinking level.
- [x] Testar compaction manual/automática, stats, histórico, abort, steer e
      follow-up.
- [x] Testar `stopReason=error`, error message, zero tokens com trabalho e
      zero trabalho sem `result` falso.
- [x] Testar `beforeToolCall`: block, allow, rewrite, chaining e restore.
- [x] Testar allowlist de tools, `pi-mcp-adapter` e `pi-web-access`.
- [x] Testar in-memory session e `no_user_settings`.

**Validation:** cada cenário de `openspec/changes/upgrade-pi-sdk-0-82-1/specs/pi-sdk-bridge/spec.md`
tem evidência em teste ou comando identificado.

## T4 — Bundle and documentation

- [x] Rebuild com `cd bridge && npm run build`.
- [x] Sincronizar `internal/bridge/bundle.ts` e
      `internal/bridge/bundle.js` pela receita do `Makefile`.
- [x] Confirmar que o banner `createRequire` permanece no bundle.
- [x] Atualizar `.specs/codebase/STACK.md` com versão real do PI e Node
      suportado.
- [x] Atualizar `.specs/codebase/INTEGRATIONS.md` para documentar
      `ModelRuntime` e `models-store.json` isolado.
- [x] Atualizar `internal/bridge/pi_boundary_test.go` para o contrato
      `0.82.1`, sem remover a verificação de que `session.on("tool_call")`
      não é usado.

**Validation:** testes de sincronização, bundle build, `git diff --check` e
revisão de diff passam.

## T5 — Full validation and live rollout gate

- [x] Executar `cd bridge && npm run typecheck`.
- [x] Executar `cd bridge && npm test`.
- [x] Executar `go test ./internal/bridge/... -count=1`.
- [x] Executar `go test ./internal/pipeline/... -count=1`.
- [x] Executar `go test ./... -short -count=1`.
- [x] Executar `go vet ./...`.
- [x] Executar `make deploy` após commit que altera Go/Bridge.
- [x] Validar Telegram/TUI, `/model`, query, tool, MCP/web, resume,
      compaction, retry e erros nos logs do daemon.
- [x] Executar soak operacional e registrar resultado.
- [x] Solicitar code review e security review antes de qualquer promoção.
- [x] Propor bump de versão e changelog ao Igor; não commitar release sem
      aprovação.
- [x] Criar `stable/upgrade-pi-sdk-0-82-1` somente após a validação live.

**Validation:** Evidence Matrix completa para A1–A5, sem `PASS` sem comando,
teste ou evidência live correspondente.

## Explicit non-goals checklist

- [x] Não atualizar para `0.83.x` nesta change.
- [x] Não migrar `internal/agents/`.
- [x] Não alterar persona, memória, cron, Telegram ou protocolo NDJSON.
- [x] Não symlinkar `models-store.json` sem uma change própria.
- [x] Não apagar sessões, credenciais ou catálogos durante rollback.
