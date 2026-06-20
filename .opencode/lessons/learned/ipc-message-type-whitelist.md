# IPC Message Type Whitelist is a Common Regression Point

**Date**: 2026-06-19
**Change**: tui-project-state-panel-fase4
**Category**: anti-pattern

## What happened

Ao adicionar o novo tipo de mensagem `MsgTypeProjectState` para o painel de estado do projeto, o handler do daemon e o cliente TUI foram implementados, mas o tipo não foi adicionado à whitelist de `validateMessage` em `internal/ipc/server.go`. O servidor rejeitava silenciosamente os pedidos do TUI, e o painel ficava vazio ou a mostrar "Loading...". Só foi detetado durante os testes ao vivo.

## How to avoid

Sempre que adicionar um novo `MsgType` ou `EventType` IPC, atualizar imediatamente a whitelist de `validateMessage` e adicionar um teste de servidor que prove que o tipo chega ao handler. Tratar a validação como parte obrigatória da definição do tipo, não como passo opcional posterior.

## Tags

#lesson #change-tui-project-state-panel-fase4 #anti-pattern #ipc #validation #whitelist
