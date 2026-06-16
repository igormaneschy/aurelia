# TUI — Implementation Changelog

## 2026-06-16 — Fase 2 MVP (`feature/tui-mvp`)

### Added
- Novo binário `cmd/aurelia-tui` para interface local em terminal.
- Pacote `internal/tui` com Model/Update/View Bubble Tea, viewport de chat,
  textarea, status bar, sidebar mínima e renderização markdown via Glamour.
- Handler TUI no daemon (`cmd/aurelia/tui_handler.go`) usando o IPC Unix socket
  existente e `ipc.ReservedTUIChatID` para sessões locais.
- Suporte a streaming de resposta por eventos IPC (`ack`, `stream_chunk`,
  `message`, `stream_end`, `error`).
- Comandos `/cwd` e `/status` funcionam pela TUI e atualizam a sidebar.

### UX hardening
- `enter` envia mensagem; `alt+enter` insere nova linha; `ctrl+j` é fallback
  portátil para newline.
- `tab`/`ctrl+i` alternam sidebar sem escrever caracteres no input.
- Atalhos `alt+<rune>` desconhecidos são ignorados em vez de inseridos como
  texto literal; atalhos próprios do textarea continuam funcionando.
- Resíduos de resposta OSC de cor do terminal (`1;rgb:.../.../...`) são
  filtrados quando vazam como input.
- Markdown usa tema fixo `dark` em vez de auto style para evitar queries de
  background do terminal que poluíam o input em alguns emuladores.
- Layout recebeu uma linha limpa de respiro no topo, status bar compacta e
  sidebar escondida em terminais estreitos/baixos.

### Validation
- `go test ./internal/tui`
- `go build ./...`
- `go test ./... -short`
- `go vet ./...`
- `go build -o "$HOME/.aurelia/bin/aurelia-tui" ./cmd/aurelia-tui`
- `make deploy` após commit para rebuild/restart do daemon.

### Deferred
- Multi-sessão e navegação real de sidebar continuam na Fase 3.
- Painel de projeto/memória continua na Fase 4.
- Tema claro/escuro adaptativo, mouse support e distribuição formal continuam
  na Fase 5.
