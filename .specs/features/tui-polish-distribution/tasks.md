# TUI Polish & Distribution — Tasks

**Sprint:** J (TUI) — Fase 5
**Branch:** `feature/tui-polish-distribution`
**Estimativa total:** 5-7 dias

---

## 5.1 — Input & Interaction Polish

### T5.1.0 — Refactor styles for theme support

**Objetivo:** Preparar os estilos para suportar light/dark antes de adicionar novas funcionalidades.

- [x] Criar `internal/tui/theme.go` com `themeStyles` struct e funções `newLightStyles()` / `newDarkStyles()`.
- [x] Mover todas as definições de estilo de `view.go` para `theme.go`.
- [x] Adicionar campo `styles themeStyles` ao `Model`.
- [x] Refatorar `view.go` para usar `m.styles.*` em vez de variáveis globais.
- [x] Garantir que `go build ./... && go test ./internal/tui/...` continua a passar.

**Critério de saída:** Nenhuma regressão visual; estilos centralizados.

---

### T5.1.1 — Mouse toggle

**Objetivo:** Permitir desligar/ligar o mouse para seleção nativa de texto.

- [ ] Alterar `cmd/aurelia-tui/main.go` para **não** passar `tea.WithMouseCellMotion()` por padrão.
- [ ] Adicionar `mouseEnabled bool` ao `Model` (default `false`).
- [ ] Adicionar handler `ctrl+m` em `internal/tui/update.go` que alterna `mouseEnabled`.
- [ ] Quando `mouseEnabled` muda, recriar o Bubble Tea program com a opção correcta.
  - Estratégia: emitir `tea.Quit` com código especial; wrapper em `main.go` detecta e reexecuta.
  - Alternativa: iniciar sempre sem mouse e documentar scroll via teclado.
- [ ] Atualizar status bar para mostrar indicador de mouse (🖱️/✋).
- [ ] Atualizar `buildTUIHelp` com `ctrl+m`.
- [ ] Adicionar testes em `internal/tui/model_test.go`.

**Critério de saída:** Utilizador consegue copiar texto do terminal com mouse off e fazer scroll com mouse on.

---

### T5.1.2 — Message queue

**Objetivo:** Permitir enviar mensagens em sequência sem esperar pela resposta anterior.

- [ ] Adicionar `pendingQueue []queuedMessage` ao `Model`.
- [ ] Criar `internal/tui/queue.go` com métodos:
  - `enqueueMessage(text, images, attachments)`
  - `dequeueMessage() (queuedMessage, bool)`
  - `pendingCount() int`
- [ ] Alterar `handleKeyMsg` em `update.go`: quando `waiting==true`, Enter não ignora — adiciona à fila.
- [ ] Alterar handlers de `stream_end`, `error`, `streamDoneMsg`, `streamErrMsg` para chamar `dequeueNext()`.
- [ ] Renderizar badge `⏳ N pending` acima do input em `view.go`.
- [ ] Garantir que `esc` cancela o turno atual mas não limpa a fila.
- [ ] Adicionar testes em `internal/tui/model_test.go`.

**Critério de saída:** Duas mensagens enviadas em sequência são processadas uma após a outra.

---

### T5.1.3 — Persistent input history

**Objetivo:** Guardar histórico de input entre execuções.

- [ ] Criar `internal/tui/history.go` com:
  - `loadInputHistory(path string) []string`
  - `saveInputHistory(path string, history []string) error`
- [ ] Carregar histórico no `NewModel`.
- [ ] Guardar histórico no shutdown (quit e sinal de interrupção).
- [ ] Limitar a 1000 entradas.
- [ ] Criar ficheiro com permissão `0600`.
- [ ] Adicionar testes em `internal/tui/history_test.go`.

**Critério de saída:** Histórico persiste após fechar e reabrir a TUI.

---

### T5.1.4 — Command auto-complete

**Objetivo:** Sugerir comandos quando o utilizador escreve `/`.

- [ ] Criar `internal/tui/autocomplete.go` com lista de comandos e lógica de prefix match.
- [ ] Adicionar estado `autocompleteOptions []string` e `autocompleteIndex int` ao `Model`.
- [ ] Handler de `tab` quando input começa com `/`: mostra/opera sugestões.
- [ ] Renderizar dropdown de sugestões em `view.go`.
- [ ] Handler de `?` com input vazio: abre help overlay (T5.2.3).
- [ ] Adicionar testes em `internal/tui/autocomplete_test.go`.

**Critério de saída:** `tab` após `/` mostra e completa comandos.

---

## 5.2 — Visual Polish & Theming

### T5.2.1 — Theme auto-detection and override

**Objetivo:** Suportar temas claro e escuro.

- [ ] Adicionar flag `--theme auto|light|dark` em `cmd/aurelia-tui/main.go`.
- [ ] Implementar `detectTerminalTheme()` em `internal/tui/theme.go`.
  - Verifica `$TERM_PROGRAM`, `$COLORFGBG`.
  - Default: `dark`.
- [ ] Aplicar tema no `NewModel`.
- [ ] Atualizar `glamour` renderer para usar estilo consistente (`dark`/`light`).
- [ ] Adicionar testes em `internal/tui/theme_test.go`.

**Critério de saída:** TUI adapta cores ao tema; `--theme light` força claro.

---

### T5.2.2 — Rich status bar

**Objetivo:** Mostrar mais informação útil na status bar.

- [ ] Adicionar campos ao `Model`: `activeModel string`, `turnStart time.Time`.
- [ ] Atualizar `renderStatusBar` para incluir:
  - Modelo ativo (se conhecido)
  - Pending count (se > 0)
  - Elapsed time (se waiting)
  - Estado do daemon
- [ ] Atualizar `fetchTUIStatus` ou `handleStreamEvent` para capturar modelo ativo.
- [ ] Adicionar testes de renderização em `internal/tui/view_test.go`.

**Critério de saída:** Status bar mostra modelo, pending count e tempo quando relevante.

---

### T5.2.3 — Help overlay

**Objetivo:** Mostrar keybindings e comandos sem sair da conversa.

- [ ] Adicionar `helpOverlayOpen bool` ao `Model`.
- [ ] Handler de `?`: toggle do overlay.
- [ ] Criar `renderHelpOverlay()` em `view.go`.
- [ ] Conteúdo: comandos, keybindings, dicas de mouse/attach.
- [ ] Fechar com `esc`, `?` ou `enter`.
- [ ] Adicionar testes.

**Critério de saída:** `?` abre overlay legível; `esc` fecha.

---

### T5.2.4 — Daemon state indicators

**Objetivo:** Tornar óbvio quando o daemon está offline.

- [ ] Alterar `renderChatHeader` para mostrar indicador offline quando `daemonLabel != "ready"`.
- [ ] Alterar `renderStatusBar` para mostrar `● offline` quando apropriado.
- [ ] Adicionar pequeno toast/linha quando a reconexão é bem-sucedida.
- [ ] Adicionar testes.

**Critério de saída:** Estado do daemon é imediatamente visível.

---

### T5.2.5 — Message visual separation

**Objetivo:** Facilitar leitura de conversas longas.

- [ ] Adicionar background alternado ou borda sutil entre mensagens.
- [ ] Aplicar via `themeStyles`.
- [ ] Garantir contraste suficiente em ambos os temas.

**Critério de saída:** Mensagens são visualmente distintas.

---

## 5.3 — Distribution & Build

### T5.3.1 — `--session` flag

**Objetivo:** Abrir sessão específica via CLI.

- [ ] Adicionar flag `--session` em `cmd/aurelia-tui/main.go`.
- [ ] Validar nome: rejeitar `.`, `..`, `/`, `\`, control chars.
- [ ] Se nome != "dm", enviar `MsgTypeSessionOpen` no `Init`;
  se não existir, enviar `MsgTypeSessionCreate` e depois `MsgTypeSessionOpen`.
- [ ] Se inválido, sair com mensagem clara.
- [ ] Adicionar testes.

**Critério de saída:** `aurelia-tui --session work` abre sessão `work`.

---

### T5.3.2 — Makefile targets

**Objetivo:** Compilar TUI via `make`.

- [ ] Adicionar `make tui` ao `Makefile`.
- [ ] Adicionar `make tui-all` para builds cross-platform.
- [ ] Documentar no `README.md` / `docs/aurelia-tui-roadmap.md`.

**Critério de saída:** `make tui` produz binary local.

---

### T5.3.3 — Cross-platform CI

**Objetivo:** Gerar artifacts para 4 arquiteturas.

- [ ] Adicionar job/matrix ao workflow de CI existente.
- [ ] Build para `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- [ ] Upload de artifacts.
- [ ] Verificar que `go install github.com/igormaneschy/aurelia/cmd/aurelia-tui@latest` funciona.

**Critério de saída:** CI produz 4 binaries e `go install` instala.

---

## T6 — Integration & validation

- [ ] `go build ./...` limpo
- [ ] `go vet ./...` limpo
- [ ] `go test ./... -short` passa sem regressões
- [ ] Teste ao vivo:
  1. Iniciar TUI: verificar mouse off por padrão e seleção nativa a funcionar
  2. `ctrl+m`: verificar scroll via mouse
  3. Enviar mensagem, enquanto streaming enviar segunda: verificar fila
  4. Fechar TUI, reabrir: verificar histórico de input
  5. `/` + `tab`: verificar auto-complete
  6. `--theme light`: verificar tema claro
  7. `?`: verificar help overlay
  8. `aurelia-tui --session work`: verificar abertura directa
  9. `make tui`: verificar build

**Critério de saída:** Todos os testes passam e a experiência ao vivo está fluida.

---

## Sequência de execução

```text
T5.1.0 → T5.1.1 → T5.1.2 → T5.1.3 → T5.1.4
  ↓
T5.2.1 → T5.2.2 → T5.2.3 → T5.2.4 → T5.2.5
  ↓
T5.3.1 → T5.3.2 → T5.3.3
  ↓
T6
```

T5.1.x e T5.2.x podem avançar em paralelo após T5.1.0. T5.3.x é independente.

---

## Notas operacionais

- Seguir a branch policy do projeto: `feature/tui-polish-distribution` → `stable/tui-polish-distribution` → `main`.
- Após cada commit, correr `make deploy` para rebuild + restart do daemon.
- A versão só é bumpada no merge para `main`, com aprovação do Igor.
