# Sincronizar indicador de modelo da TUI — Design

## 1. Current state

O daemon já salva o modelo selecionado em `a.config` e em `app.json`, limpa a
sessão TUI e usa o novo modelo na próxima mensagem. A TUI, entretanto, mantém
`chromeModel.activeModel` como estado local separado.

O status é carregado em três situações existentes:

- conexão inicial;
- troca/abertura de sessão;
- resposta explícita ou periódica que chama `fetchTUIStatus`.

O término de um comando `/model` não está entre essas situações.

## 2. Target behavior

### 2.1 Canonical refresh

Adicionar um marcador de estado local para indicar que o stream atual requer
refresh de status ao terminar. O marcador deve ser ativado somente para:

- `/model <nome>`;
- `/model auto`;
- seleção equivalente no wizard;
- comandos `/model` que estejam na fila;
- modelo pendente aplicado imediatamente após `session_create`.

Quando o evento `stream_end` confirmar o término do comando:

1. limpar `waiting` como hoje;
2. limpar o marcador;
3. agendar `fetchTUIStatus(m.ipcClient, m.activeSession)`;
4. manter a mensagem de confirmação no transcript.

O `/status` retornado pelo daemon continua sendo a única fonte para
`activeModel`. Não parsear o texto `✅ Model changed...` para evitar duplicar
contratos de apresentação.

### 2.2 Failure behavior

- Se o comando `/model` falhar, não alterar `activeModel` e limpar o marcador.
- Se o refresh de status falhar, manter o último modelo conhecido e mostrar o
  estado de daemon já existente; não exibir o modelo solicitado como se tivesse
  sido aplicado.
- Uma resposta de status sem campo de modelo deve ser tratada como estado não
  confirmado: não sobrescrever `activeModel` com string vazia.
- `/model auto` deve refletir o valor canônico `PI default` retornado por
  `handleTUIStatus`.

### 2.3 Queue and session paths

O mesmo helper de início de comando deve ser usado pelos caminhos direto,
wizard, fila e `pendingSessionModel`. Isso evita que a correção funcione apenas
para a digitação manual e permaneça quebrada para seleção via formulário.

O refresh inicial disparado por troca de sessão pode ocorrer em paralelo com o
comando pendente; o refresh pós-comando é obrigatório e vence a resposta
intermediária antiga.

### 2.4 Rendering contract

Sem alteração de layout:

- `header.go` continua usando `activeModel`;
- `sidebar.go` continua usando `activeModel`;
- o valor novo aparece nos dois lugares após o mesmo `tuiStatusMsg`;
- modelo anterior permanece visível enquanto a troca está em andamento ou falha.

## 3. Implementation phases

### Phase 0 — Baseline

- Criar `feature/tui-model-indicator-sync` a partir de `main` atualizado.
- Registrar o comportamento atual com seleção manual, wizard, `/model auto` e
  comando enfileirado.
- Confirmar que a próxima mensagem já usa o modelo novo.

### Phase 1 — State synchronization

- Adicionar o marcador/helper de comando `/model`.
- Solicitar `fetchTUIStatus` após `stream_end` somente quando necessário.
- Limpar o marcador em `error`, `streamDone` e `streamErr`.
- Cobrir `pendingSessionModel` e `startQueuedMessage`.

### Phase 2 — Tests and visual validation

- Testar atualização de `activeModel` após stream final + `tuiStatusMsg`.
- Testar que mensagens normais não disparam refresh adicional.
- Testar falha de comando/status e `/model auto`.
- Validar header/sidebar na TUI real após seleção de dois modelos diferentes.

## 4. Data / API contract

Não há alteração no protocolo IPC nem no daemon. A change reutiliza:

- `ipc.MsgTypeCommand` para `/model`;
- `ipc.EventTypeStreamEnd` como término do comando;
- `fetchTUIStatus` existente;
- `tuiStatusMsg` existente.

## 5. Risks and rollback

Risco baixo: uma chamada local adicional de `/status` após comandos de modelo.
O risco principal é uma resposta de status antiga sobrescrever a nova em uma
troca de sessão; o marcador deve ser consumido somente no stream do comando e o
refresh deve usar `activeSession` atual.

Rollback: remover o refresh pós-comando sem alterar configuração persistida,
sessões ou histórico.

## 6. Validation gates

- `go test ./internal/tui/... -count=1`
- `go test ./cmd/aurelia/... -count=1`
- `go test ./... -short -count=1`
- `go vet ./...`
- validação live: `/model A` → header/sidebar A; `/model B` → header/sidebar B;
  `/model auto` → `PI default`; seleção via wizard e fila.
