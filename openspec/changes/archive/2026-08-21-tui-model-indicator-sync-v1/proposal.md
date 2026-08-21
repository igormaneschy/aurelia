# Sincronizar indicador de modelo da TUI

**Status:** proposed  
**Created:** 2026-08-11  
**Owner:** Aurelia architecture  
**Change type:** TUI correctness / state synchronization

## Why

O comando `/model` na TUI altera corretamente a configuração do daemon e
reinicia a sessão: a próxima pergunta usa o novo modelo. Porém, o indicador
visual do header/sidebar continua mostrando o modelo anterior até uma nova
consulta de status ou troca de sessão.

## Root cause confirmed

- `cmd/aurelia/tui_model_handler.go:217-227` persiste o modelo e limpa a sessão
  corretamente.
- `internal/tui/update.go:228-239` atualiza `activeModel` somente quando recebe
  `tuiStatusMsg`.
- `internal/tui/update.go:1404-1412` processa a mensagem final do comando, mas
  só atualiza CWD; não atualiza modelo nem solicita `/status`.
- `internal/tui/update.go:1414-1430` encerra o stream sem refresh de status.
- Header e sidebar renderizam exclusivamente `activeModel` em
  `internal/tui/header.go` e `internal/tui/sidebar.go`.

Portanto, o runtime está correto e a falha é de sincronização do estado visual
local da TUI.

## Goal

Depois de um `/model <nome>` ou `/model auto` concluído com sucesso, o header e
a sidebar devem refletir o modelo canônico retornado pelo daemon, sem exigir
uma nova pergunta, troca de sessão ou reinício da TUI.

## In scope

- Refresh de `/status` após conclusão bem-sucedida de comandos `/model`.
- Cobertura de seleção pelo wizard, comando textual e comando enfileirado.
- Cobertura de `/model auto` e troca de sessão com modelo pendente.
- Testes de header/sidebar e falhas de comando/status.

## Out of scope

- Alterar a persistência de `default_provider/default_model`.
- Alterar a resolução de modelos do Bridge ou o catálogo PI.
- Compartilhar sessões entre TUI e Telegram.
- Alterar o layout visual além de corrigir o valor exibido.
- Bump de versão, changelog, deploy ou promoção nesta change.

## Decision

Usar o `/status` já existente como fonte de verdade após o término do comando,
em vez de interpretar texto de confirmação (`Model changed to ...`). O refresh
deve ser marcado por tipo de comando para não gerar uma chamada `/status` após
cada mensagem normal.

## Production path

```text
TUI wizard/text/queue
  → IPC MsgTypeCommand (/model ...)
  → cmd/aurelia/tui_model_handler.go
  → config + session reset
  → IPC message/stream_end
  → TUI fetchTUIStatus
  → activeModel
  → header + sidebar
```

## Rollout boundary

Esta change termina em uma branch `feature/tui-model-indicator-sync` com testes
locais e validação live da TUI. Stable branch, bump de versão, changelog,
merge, push e deploy exigem aprovação explícita posterior.

## References

- `internal/tui/update.go` — ciclo de comando, stream e status.
- `internal/tui/model.go` — `fetchTUIStatus` e `statusFromEvents`.
- `internal/tui/header.go` / `internal/tui/sidebar.go` — renderização do modelo.
- `cmd/aurelia/tui_model_handler.go` — fonte de verdade da troca de modelo.
- `decisions/tui-telegram-independent-surfaces.md` — TUI permanece local e independente.
