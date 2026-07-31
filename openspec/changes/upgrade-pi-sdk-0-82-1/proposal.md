# Upgrade PI SDK 0.79.2 → 0.82.1

**Status:** proposed
**Created:** 2026-07-31
**Owner:** Aurelia architecture
**Change type:** dependency/API migration

## Why

O Aurelia ainda fixa `@earendil-works/pi-ai` e
`@earendil-works/pi-coding-agent` em `0.79.2`, embora o PI SDK já tenha
mudado o contrato de autenticação e resolução de modelos em `0.80.8`.

O bridge atual usa diretamente `AuthStorage` e `ModelRegistry` em
`bridge/index.ts` e replica as dependências em `internal/bridge/setup.go`.
Essas APIs foram removidas ou deixaram de ser o caminho canônico no
`0.82.1`; portanto, não é uma atualização apenas de versões.

O upgrade também incorpora correções relevantes para o caminho de produção:

- `ModelRuntime` unifica autenticação, configuração e catálogo de modelos;
- retries mais resilientes para compaction, branch summaries, DNS e streams;
- correções de persistência/carregamento de sessões;
- correções de credenciais provider-scoped e catálogos dinâmicos;
- `protobufjs` `7.6.5`, corrigindo o intervalo afetado por
  `GHSA-j3f2-48v5-ccww` / `CVE-2026-59877`.

## Goal

Atualizar o PI SDK integrado do Aurelia de `0.79.2` para `0.82.1`, migrar o
bridge para `ModelRuntime`, preservar o protocolo NDJSON e demonstrar por
testes e validação live que modelos, sessões, compaction, ferramentas,
extensions, segurança e fluxo Telegram continuam funcionando.

## In scope

- Fixar ambos os pacotes PI em `0.82.1` e regenerar o lockfile.
- Migrar `AuthStorage`/`ModelRegistry` para `ModelRuntime`.
- Atualizar resolução e listagem de modelos para APIs assíncronas do runtime.
- Validar compatibilidade de sessões JSONL existentes.
- Validar `beforeToolCall`, `pi-mcp-adapter` e `pi-web-access`.
- Atualizar o template de instalação em `internal/bridge/setup.go`.
- Regenerar e sincronizar os bundles do bridge.
- Corrigir a documentação de runtime Node e da integração PI.
- Fazer validação live no daemon antes da promoção.

## Out of scope

- Upgrade para `0.83.x`.
- Adoção de constrained sampling.
- Migração de `internal/agents/` para o PI SDK.
- Mudança do protocolo Go ↔ TypeScript/NDJSON.
- Reescrita da política de segurança.
- Novos providers ou mudanças na UX Telegram.
- Symlink imediato de `models-store.json`; isso só poderá ser reavaliado
  como mudança separada após evidência de necessidade e segurança de locking.

## Decision

Usar `ModelRuntime` como única fachada de autenticação e catálogo no bridge:

```text
auth.json + models.json
        ↓
ModelRuntime.create(...)
        ↓
getModel/getModels/getAvailable/refresh
        ↓
createAgentSession({ modelRuntime, ... })
```

O `ModelRuntime` será criado por sessão durante esta change. Não será
introduzido singleton ou cache global antes de medir custo e isolamento de
extensions.

`0.82.1` permanece o alvo desta change, mesmo com `0.83.0` já publicado:
`0.83.0` adiciona uma quebra independente na camada TypeBox e deve ser
avaliado separadamente.

## Impact

### Production path

```text
Telegram/TUI
  → internal/pipeline
  → internal/bridge (NDJSON)
  → bridge/index.ts
  → ModelRuntime + AgentSession
  → provider / tools / extensions
```

### Risk

O risco principal é regressão em resolução de modelos, retomada de sessões,
extensions ou enforcement de `beforeToolCall`. A implementação só pode ser
promovida após baseline `0.79.2`, testes de fixture e validação live.

### Rollout boundary

Esta change termina em uma branch `feature/` validada localmente e no daemon.
A promoção para `stable/*`, bump de versão, alteração de `CHANGELOG.md`,
merge para `main` e push exigem aprovação explícita posterior.

## References

- `bridge/index.ts` — uso atual de `AuthStorage`, `ModelRegistry` e `createAgentSession`.
- `bridge/package.json` — versões e target do bridge.
- `internal/bridge/setup.go` — template de dependências instalado pelo daemon.
- `internal/bridge/setup_test.go` — invariantes de instalação e bundle.
- `internal/bridge/pi_boundary_test.go` — contratos da fronteira PI.
- `AGENTS.md` — branch, deploy, symlink e validação obrigatórios.
- `.opencode/lessons/learned/pi-sdk-api-verification.md` — APIs devem ser verificadas nos tipos instalados.
- `.opencode/lessons/learned/pi-model-registry-roundtrip.md` — catálogo precisa de validação live.
- `.opencode/lessons/learned/runtime-baseline-before-fix.md` — baseline deve preceder a correção.
- https://github.com/earendil-works/pi/releases/tag/v0.82.1
- https://github.com/advisories/GHSA-j3f2-48v5-ccww
