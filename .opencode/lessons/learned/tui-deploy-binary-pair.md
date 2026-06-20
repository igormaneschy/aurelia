# Deploy Must Rebuild Both Daemon and TUI Binaries Atomically

**Date**: 2026-06-19
**Change**: tui-project-state-panel-fase4
**Category**: process

## What happened

O `make deploy` original só reconstruía e instalava o binário do daemon (`aurelia`). Depois de alterações no TUI, o binário `aurelia-tui` ficava desatualizado, e os testes ao vivo usavam uma versão antiga da interface. Foi necessário adicionar um target `install-tui` com o mesmo padrão atómico `.new -> mv` e fazer `deploy` depender de `install` + `install-tui`.

## How to avoid

Sempre que o projeto tiver múltiplos binários Go (daemon, TUI, CLI), o target de deploy deve reconstruir todos os binários afetados de forma atómica. Nunca atualizar só o daemon — testar com binários desfasados produz falsos negativos.

## Tags

#lesson #change-tui-project-state-panel-fase4 #process #deploy #tui #makefile
