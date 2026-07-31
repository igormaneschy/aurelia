# Upgrade PI SDK 0.79.2 → 0.82.1 — Tasks

**Change:** `upgrade-pi-sdk-0-82-1`
**Dependency graph:** `T0 → T1 → T2 → T3 → T4 → T5`
**Implementation branch:** `feature/upgrade-pi-sdk-0-82-1`
**Terminal boundary:** validação live no daemon; promoção/release ficam fora desta change até aprovação explícita.

## T0 — Baseline and preflight

- [ ] Criar branch dedicada a partir do `main` atualizado; não trabalhar
      diretamente em `main`.
- [ ] Confirmar Node `>=22.19.0` no desenvolvimento, serviço e CI.
- [ ] Registrar baseline `0.79.2`: startup, `/model`, query, tool read-only,
      MCP/web extension, resume, compaction e erro de provider.
- [ ] Capturar fixture JSONL redigida de sessão `0.79.2`.
- [ ] Inspecionar tipos e runtime publicados de `0.82.1` para confirmar:
      `ModelRuntime`, `createAgentSession`, `beforeToolCall`,
      `SessionManager`, `SettingsManager` e eventos.
- [ ] Confirmar symlinks de `auth.json` e `models.json` sem ler ou registrar
      segredos.
- [ ] Parar e reportar blocker se Node, tipos, fixture ou baseline não forem
      obtidos.

**Validation:** baseline documentado, API surface confirmada em `dist/*.d.ts`
e blocker protocol satisfeito.

## T1 — Dependencies and package contract

- [ ] Atualizar `bridge/package.json` para PI `0.82.1`.
- [ ] Regenerar `bridge/package-lock.json` de forma reproduzível.
- [ ] Confirmar `protobufjs >=7.6.5` no lockfile.
- [ ] Atualizar as versões PI no template `bridgePackageJSON` em
      `internal/bridge/setup.go`.
- [ ] Atualizar `internal/bridge/setup_test.go` para verificar versão exata,
      coerência entre os dois pacotes e engine Node.
- [ ] Confirmar que `models-store.json` permanece isolado e não é apagado ou
      transformado em symlink automaticamente.

**Validation:** instalação limpa, inspeção do lockfile, typecheck e testes de
setup passam.

## T2 — Bridge migration to ModelRuntime

- [ ] Remover import e criação de `AuthStorage`.
- [ ] Remover criação de `ModelRegistry` para o caminho de sessão.
- [ ] Criar `ModelRuntime` com `authPath`/`modelsPath` do agent dir isolado.
- [ ] Passar `modelRuntime` para `createAgentSession`.
- [ ] Migrar resolução para `getModel` + fallback por ID exato em `getModels`.
- [ ] Preservar apenas aliases `mapProvider` e `mapModelForProvider` existentes.
- [ ] Migrar `list-models` para runtime, `getAvailable()` e refresh explícito
      assíncrono.
- [ ] Preservar formato NDJSON e erros claros de modelo inexistente.
- [ ] Não introduzir fuzzy matching, singleton global ou mudança de protocolo.

**Validation:** `bridge/index.ts` e `bridge/bundle.ts` não contêm uso de
`AuthStorage`; typecheck, testes Bridge e build passam.

## T3 — Compatibility and regression tests

- [ ] Testar resolução qualificada, aliases válidos e modelo inexistente.
- [ ] Testar que listagem e query usam o mesmo catálogo/runtime.
- [ ] Testar abertura da fixture de sessão `0.79.2` sem perda de mensagens,
      timestamps, model ou thinking level.
- [ ] Testar compaction manual/automática, stats, histórico, abort, steer e
      follow-up.
- [ ] Testar `stopReason=error`, error message, zero tokens com trabalho e
      zero trabalho sem `result` falso.
- [ ] Testar `beforeToolCall`: block, allow, rewrite, chaining e restore.
- [ ] Testar allowlist de tools, `pi-mcp-adapter` e `pi-web-access`.
- [ ] Testar in-memory session e `no_user_settings`.

**Validation:** cada cenário de `openspec/changes/upgrade-pi-sdk-0-82-1/specs/pi-sdk-bridge/spec.md`
tem evidência em teste ou comando identificado.

## T4 — Bundle and documentation

- [ ] Rebuild com `cd bridge && npm run build`.
- [ ] Sincronizar `internal/bridge/bundle.ts` e
      `internal/bridge/bundle.js` pela receita do `Makefile`.
- [ ] Confirmar que o banner `createRequire` permanece no bundle.
- [ ] Atualizar `.specs/codebase/STACK.md` com versão real do PI e Node
      suportado.
- [ ] Atualizar `.specs/codebase/INTEGRATIONS.md` para documentar
      `ModelRuntime` e `models-store.json` isolado.
- [ ] Atualizar `internal/bridge/pi_boundary_test.go` para o contrato
      `0.82.1`, sem remover a verificação de que `session.on("tool_call")`
      não é usado.

**Validation:** testes de sincronização, bundle build, `git diff --check` e
revisão de diff passam.

## T5 — Full validation and live rollout gate

- [ ] Executar `cd bridge && npm run typecheck`.
- [ ] Executar `cd bridge && npm test`.
- [ ] Executar `go test ./internal/bridge/... -count=1`.
- [ ] Executar `go test ./internal/pipeline/... -count=1`.
- [ ] Executar `go test ./... -short -count=1`.
- [ ] Executar `go vet ./...`.
- [ ] Executar `make deploy` após commit que altera Go/Bridge.
- [ ] Validar Telegram/TUI, `/model`, query, tool, MCP/web, resume,
      compaction, retry e erros nos logs do daemon.
- [ ] Executar soak operacional e registrar resultado.
- [ ] Solicitar code review e security review antes de qualquer promoção.
- [ ] Propor bump de versão e changelog ao Igor; não commitar release sem
      aprovação.
- [ ] Criar `stable/upgrade-pi-sdk-0-82-1` somente após a validação live.

**Validation:** Evidence Matrix completa para A1–A5, sem `PASS` sem comando,
teste ou evidência live correspondente.

## Explicit non-goals checklist

- [ ] Não atualizar para `0.83.x` nesta change.
- [ ] Não migrar `internal/agents/`.
- [ ] Não alterar persona, memória, cron, Telegram ou protocolo NDJSON.
- [ ] Não symlinkar `models-store.json` sem uma change própria.
- [ ] Não apagar sessões, credenciais ou catálogos durante rollback.
