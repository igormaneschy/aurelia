# Upgrade PI SDK 0.79.2 → 0.82.1 — Design

## 1. Current state

### Dependency surface

As versões `0.79.2` estão declaradas em três lugares de autoridade de
build/instalação:

| Local | Responsabilidade |
|---|---|
| `bridge/package.json` | desenvolvimento e build do bridge |
| `bridge/package-lock.json` | resolução reproduzível e integridade |
| `internal/bridge/setup.go` | `npm install` no ambiente do daemon |

O bundle duplicado precisa permanecer sincronizado:

```text
bridge/index.ts
    ├─ npm run build → bridge/bundle.js
    ├─ cp index.ts → internal/bridge/bundle.ts
    └─ cp bundle.js → internal/bridge/bundle.js
```

### Current PI calls

`bridge/index.ts` usa:

- `AuthStorage.create(...)`;
- `ModelRegistry.create(...)`;
- `registry.find()` e `registry.getAll()`;
- `registry.getAvailable()`;
- `registry.refresh()`;
- `createAgentSession({ authStorage, modelRegistry, ... })`;
- `DefaultResourceLoader`;
- `SessionManager`;
- `SettingsManager`;
- `session.agent.beforeToolCall`;
- eventos de streaming, retry e compaction.

## 2. Target architecture

### 2.1 Runtime creation

O bridge deverá criar o runtime com os arquivos isolados do Aurelia:

```typescript
const modelRuntime = await ModelRuntime.create({
  authPath: join(agentDir, "auth.json"),
  modelsPath: join(agentDir, "models.json"),
  allowModelNetwork: false,
});
```

O uso de rede de catálogo será explícito no comando `list-models` com
`refresh=true`, não durante cada query. O comportamento efetivo e as opções
exatas devem ser confirmados nos tipos publicados de `0.82.1` antes de editar
o bridge.

`models.json` continua sendo o symlink gerenciado pelo `EnsureBridge`.
`models-store.json`, introduzido pela linha nova do PI, permanece isolado em
`~/.aurelia/pi-agent/` nesta change para evitar escrita concorrente com o PI
CLI. A paridade com `pi --list-models` será medida após refresh explícito.

### 2.2 Model resolution

Preservar somente os aliases de produto já existentes:

- `mapProvider()`;
- `mapModelForProvider()`.

Depois do mapeamento:

1. usar `modelRuntime.getModel(mappedProvider, mappedModel)` quando provider
   estiver definido;
2. usar busca por ID exato em `modelRuntime.getModels()` como fallback;
3. não reintroduzir fuzzy matching por nome, label ou substring;
4. retornar erro claro antes de chamar `prompt()` quando o modelo não existir.

O mesmo runtime deve ser usado para listagem, resolução e criação da sessão.
Isso evita o bug histórico em que `/model` mostrava um modelo que o caminho
de query não conseguia resolver.

### 2.3 Session creation

O contrato alvo é:

```typescript
const { session } = await createAgentSession({
  cwd,
  agentDir,
  modelRuntime,
  resourceLoader,
  sessionManager,
  settingsManager,
  model,
  tools: effectiveTools,
});
```

Remover `AuthStorage` do import e remover `authStorage`/`modelRegistry` das
opções da factory. `SessionManager`, `SettingsManager`,
`DefaultResourceLoader`, `session.agent.beforeToolCall` e os limites do
bridge continuam sendo responsabilidades explícitas do Aurelia.

### 2.4 `list-models`

O fluxo será:

```text
list-models
  → ModelRuntime.create(...)
  → se refresh=true: await runtime.refresh({ allowNetwork: true })
  → await runtime.getAvailable()
  → validar IDs via runtime.getModel/getModels
  → emitir o mesmo JSON NDJSON atual
```

O formato externo não muda:

```json
{
  "provider": "...",
  "id": "...",
  "name": "...",
  "supportsImages": true
}
```

Falha de refresh deve ser observável nos logs e não pode produzir uma lista
silenciosamente vazia quando existe catálogo anterior utilizável.

### 2.5 Security and extensions

O hook de segurança permanece instalado depois da criação da sessão:

```text
PI AgentSession hook
        ↑ chained original extension hook
Bridge security wrapper
        ↓ block / rewrite / allow + audit
PI tool execution
```

Regras obrigatórias:

- verificar `typeof session.agent.beforeToolCall === "function"` na versão
  instalada;
- executar a policy Aurelia antes do hook de extension;
- retornar `{ block: true, reason }` para bloqueios;
- manter rewrite in-place de `ctx.args`;
- encadear o hook original;
- restaurar o hook no teardown;
- preservar fail-closed das policies de segurança;
- validar `pi-mcp-adapter` e `pi-web-access` com os symlinks atuais.

Não usar `session.on("tool_call")`: a lição histórica confirma que essa API
não existe no contrato PI usado pelo bridge.

### 2.6 Events and NDJSON

O upgrade não altera o protocolo Go ↔ bridge. Eventos novos do PI podem ser
ignorados de forma segura no `switch`, mas eventos já encaminhados devem
manter nomes e campos:

- `system`;
- `assistant`;
- `tool_use`;
- `tool_result`;
- `turn_start` / `turn_end`;
- `auto_retry_start` / `auto_retry_end`;
- `compaction_start` / `compaction_end`;
- `result` / `error`.

`result` não pode ser emitido quando o PI reportar `stopReason=error`,
`errorMessage`, zero tokens com trabalho alegado ou zero trabalho sem
resultado. Essa proteção já existe e deve ser preservada/revalidada.

## 3. Data and configuration compatibility

### Credentials and model files

- `~/.aurelia/pi-agent/auth.json` continua symlink para
  `~/.pi/agent/auth.json`;
- `~/.aurelia/pi-agent/models.json` continua symlink para
  `~/.pi/agent/models.json`;
- não copiar credenciais;
- não sobrescrever `models.json` durante a instalação;
- `models-store.json` pode ser criado no diretório isolado pelo runtime;
- nenhuma migração destrutiva de `auth.json` ou `models.json`.

### Sessions

Não há migração manual prevista. `SessionManager.open()` de `0.82.1` deve
abrir fixtures produzidas pelo `0.79.2`. A validação deve confirmar:

- mesmo `session_id` e `session_file`;
- mensagens e timestamps preservados;
- model/thinking level restaurados;
- compaction e stats funcionais;
- arquivos inválidos não são sobrescritos.

### Node

O pacote instalado declara Node `>=22.19.0`, enquanto a documentação local e
o bundle target ainda mencionam Node 20/18. Antes da implementação:

1. confirmar Node do shell de desenvolvimento;
2. confirmar Node usado pelo serviço;
3. confirmar Node disponível na CI;
4. alinhar `STACK.md`, scripts e target somente após essa evidência.

Se o daemon não possuir Node compatível, isso é blocker; não usar um target
transpilado para mascarar um runtime abaixo do engine exigido.

## 4. Implementation phases

### Phase 0 — Baseline and API preflight

- branch `feature/upgrade-pi-sdk-0-82-1` a partir de `main` atualizado;
- baseline live em `0.79.2`;
- fixture JSONL redigida;
- inspeção de `dist/*.d.ts` e runtime de `0.82.1`;
- confirmação de Node, extensions e symlinks.

### Phase 1 — Dependency and packaging

- atualizar `package.json`;
- regenerar lockfile;
- verificar `protobufjs >=7.6.5`;
- atualizar template em `setup.go`;
- reforçar testes de versão e engine.

### Phase 2 — Bridge API migration

- trocar imports e criação do runtime;
- migrar resolução/listagem/refresh de modelos;
- migrar `createAgentSession`;
- manter aliases e formato NDJSON;
- preservar segurança, erros, retries e compaction.

### Phase 3 — Compatibility tests

- model round-trip;
- session resume/compaction/stats;
- streaming/retry/error;
- security hook;
- extensions/MCP/web;
- no-user-settings/in-memory session.

### Phase 4 — Bundle, documentation and review

- gerar e sincronizar os bundles;
- atualizar specs de stack/integration;
- executar gates locais;
- code review com foco backend/bridge e security review para o hook;
- corrigir Critical/High antes do deploy.

### Phase 5 — Live validation

- `make deploy` após commit que altera Go/Bridge;
- testar Telegram/TUI e logs do daemon;
- soak operacional;
- preparar branch `stable/*` somente após validação.

## 5. Rollback

O rollback operacional é a reimplantação do último artefato/commit
conhecido como funcional, seguido de rebuild e restart do daemon conforme
`AGENTS.md`. Não remover ou substituir manualmente credenciais, sessões ou
catálogos do usuário.

Se `models-store.json` causar comportamento inesperado, preservar o arquivo
para diagnóstico e desabilitar apenas a nova estratégia de catálogo em uma
correção separada; não apagar dados automaticamente.

## 6. Review gates

1. **Preflight:** APIs instaladas, Node e baseline confirmados.
2. **Type/API:** `0.82.1` compila sem símbolos removidos.
3. **Behavior:** cinco contratos em `specs/pi-sdk-bridge/spec.md` comprovados.
4. **Security:** `beforeToolCall`, allowlist, blocking, rewrite e audit.
5. **Live:** daemon rebuilt/restarted, Telegram smoke e logs sem regressão.

## 7. Versioning boundary

Não alterar `internal/version/version.go` nem `CHANGELOG.md` nesta fase de
planejamento. Para a implementação, o bump e a entrada de changelog devem
ser propostos ao Igor e aprovados antes do commit de release/promoção.
