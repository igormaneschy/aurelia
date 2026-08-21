# pi-sdk-bridge Specification

## Purpose
Contrato do bridge sobre o PI SDK: instalação reproduzível da versão pinada,
runtime unificado de modelos/autenticação via `ModelRuntime`, compatibilidade
de sessões e lifecycle entre versões, preservação de ferramentas/extensões e
segurança, e entrega operacional com bundle sincronizado à fonte.

## Requirements

### Requirement: A1 — Reproducible PI 0.82.1 runtime

O sistema MUST instalar e executar `@earendil-works/pi-ai` e
`@earendil-works/pi-coding-agent` em `0.82.1`, com lockfile e template do
daemon coerentes. O runtime Node efetivo MUST satisfazer o engine publicado
pelo pacote.

#### Scenario: clean dependency installation

- **GIVEN** um checkout da change sem `node_modules` do bridge
- **WHEN** as dependências são instaladas usando o `package.json` e o lockfile
- **THEN** ambos os pacotes PI resolvem para `0.82.1`
- **AND** `protobufjs` resolve para versão corrigida `>=7.6.5`
- **AND** o typecheck do bridge passa

#### Scenario: daemon installation uses the same contract

- **GIVEN** o daemon precisa preparar um bridge novo
- **WHEN** `EnsureBridge` grava o package template e instala as dependências
- **THEN** o template declara as mesmas versões PI `0.82.1`
- **AND** o bundle pode ser construído com o Node suportado

### Requirement: A2 — Unified model/auth runtime

O bridge MUST usar `ModelRuntime` para autenticação, resolução, catálogo e
criação de sessões. `AuthStorage` não pode ser importado pelo bridge, e a
factory `createAgentSession` não pode receber `authStorage` ou `modelRegistry`.

#### Scenario: qualified model resolution

- **GIVEN** provider e model ID válidos, incluindo alias Aurelia suportado
- **WHEN** uma query cria a sessão
- **THEN** o mesmo `ModelRuntime` resolve o modelo e é passado à
  `createAgentSession`
- **AND** a sessão informa o provider/model corretos no evento `system`

#### Scenario: unknown model fails closed

- **GIVEN** um provider/model ID que não existe no runtime
- **WHEN** uma query é recebida
- **THEN** o bridge emite `error` claro
- **AND** não chama `prompt()` nem emite `result` vazio de sucesso

#### Scenario: list and query use the same catalog

- **GIVEN** `list-models` retorna um modelo autenticado
- **WHEN** o mesmo provider/model ID é usado em uma query
- **THEN** a resolução da query encontra o modelo pelo runtime
- **AND** nenhum modelo é listado apenas por uma API que a query não consegue usar

### Requirement: A3 — Session and lifecycle compatibility

O bridge MUST preservar o contrato de sessões, compaction, retries, abort,
steer, follow-up, stats e histórico para arquivos criados na versão anterior.

#### Scenario: resume a 0.79.2 session

- **GIVEN** um fixture JSONL válido criado pelo PI `0.79.2`
- **WHEN** o bridge `0.82.1` abre a sessão pelo caminho salvo
- **THEN** o session ID e o arquivo original são preservados
- **AND** mensagens, timestamps, modelo e thinking level continuam disponíveis

#### Scenario: compaction remains observable

- **GIVEN** uma sessão que exige compaction manual ou automática
- **WHEN** a compaction é executada
- **THEN** `compaction_start` e `compaction_end` continuam sendo emitidos
- **AND** stats/tokens e o resumo persistido permanecem coerentes

#### Scenario: provider failure is not reported as success

- **GIVEN** o provider termina com `stopReason=error`, error message ou zero
  tokens após trabalho alegado
- **WHEN** o prompt termina
- **THEN** o bridge emite `error`
- **AND** não emite `result` como se a resposta fosse válida

### Requirement: A4 — Tool, security and extension compatibility

O upgrade MUST preservar as ferramentas built-in, as extensions compartilhadas
e o enforcement de segurança no hook `session.agent.beforeToolCall`.

#### Scenario: security hook blocks a destructive tool call

- **GIVEN** security está habilitada com perfil que bloqueia comando destrutivo
- **WHEN** o PI tenta executar a ferramenta Bash correspondente
- **THEN** a policy Aurelia retorna bloqueio antes da execução
- **AND** existe registro de auditoria redigido

#### Scenario: extension hook is chained

- **GIVEN** uma extension instalou o hook original do AgentSession
- **WHEN** a policy Aurelia permite a chamada
- **THEN** o hook original é chamado
- **AND** uma exceção do hook não remove o cleanup nem derruba o bridge

#### Scenario: MCP/web extension remains available

- **GIVEN** os symlinks de `pi-mcp-adapter` e `pi-web-access` estão presentes
- **WHEN** o bridge cria uma sessão com allowlist não vazia
- **THEN** as ferramentas utilitárias esperadas continuam disponíveis e
  sujeitas à policy
- **AND** o relatório de tools não depende de um snapshot prematuro de
  `getActiveToolNames()`

### Requirement: A5 — Operational delivery and live validation

O bundle instalado no daemon MUST ser derivado da fonte atual e funcionar no
caminho Telegram/TUI → Go → bridge → PI sem alteração do protocolo NDJSON.

#### Scenario: source and embedded bundle stay synchronized

- **GIVEN** `bridge/index.ts` foi alterado
- **WHEN** o build do bridge é concluído
- **THEN** `bridge/bundle.js`, `internal/bridge/bundle.ts` e
  `internal/bridge/bundle.js` refletem a mesma fonte/versão
- **AND** os testes de sincronização passam

#### Scenario: live Telegram smoke

- **GIVEN** o binário foi rebuilt e o daemon foi restarted por `make deploy`
- **WHEN** uma mensagem Telegram normal, uma consulta `/model`, uma chamada
  de ferramenta e um resume são executados
- **THEN** o usuário recebe resposta correta
- **AND** os logs não mostram import error, engine mismatch, modelo não
  resolvido ou sessão perdida

#### Scenario: explicit catalog refresh

- **GIVEN** o usuário solicita refresh de modelos
- **WHEN** `list-models` executa refresh explícito
- **THEN** o catálogo é atualizado de forma observável
- **AND** falha de rede não apaga silenciosamente um catálogo utilizável
