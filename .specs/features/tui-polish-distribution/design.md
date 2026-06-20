# TUI Polish & Distribution — Design

## Contexto

Este documento descreve as decisões de design, estrutura de packages, tabelas de mapeamento e detalhes de implementação para a Fase 5 do TUI.

## Estrutura de Packages

```
internal/
  tui/
    model.go                 ← mouseEnabled, pendingQueue, theme, historyFile
    update.go                ← ctrl+m, queue submit, Esc behavior
    view.go                  ← theme styles, status bar, help overlay
    history.go               ← NOVO — persistência de input history
    autocomplete.go          ← NOVO — sugestões de comandos
cmd/aurelia-tui/
  main.go                  ← --session, --theme flags
cmd/aurelia/
  tui_session_handler.go   ← reutilizado para abrir/criar sessão via --session
Makefile                   ← target `make tui`
.github/workflows/         ← build matrix cross-platform
```

## Tipos e Estado

### Model extensions

```go
// internal/tui/model.go

type Model struct {
    // ... existing fields ...

    // Mouse capture state.
    mouseEnabled bool

    // Pending message queue.
    pendingQueue []queuedMessage

    // Theme: "auto" | "light" | "dark".
    theme string

    // History file path.
    historyPath string
}

type queuedMessage struct {
    Text        string
    Images      []ipc.IPCImage
    Attachments []ipc.IPCAttachment
}
```

## Decisões de Design

### 5.1.1 Mouse toggle

O Bubble Tea `tea.WithMouseCellMotion()` captura todos os eventos de mouse, o que é necessário para scroll no viewport mas impede a seleção nativa de texto do terminal.

**Decisão:** Implementar toggle `ctrl+m` que recria o programa com ou sem mouse.

```go
func (m Model) toggleMouseCmd() tea.Cmd {
    return func() tea.Msg {
        return toggleMouseMsg{enabled: !m.mouseEnabled}
    }
}
```

No `main.go`, o programa é criado com mouse on. Quando o toggle ocorre, o programa emite `tea.Quit` e o wrapper externo recria o programa com a opção correcta. Alternativa: guardar o estado e apenas alterar o comportamento interno (scroll) — mas isso não devolve a seleção nativa. A recriação é necessária.

Simplificação: ao invés de recriar o programa, podemos iniciar **sem** `tea.WithMouseCellMotion()` e implementar scroll via teclado como primário, ativando o mouse apenas quando o utilizador pressiona `ctrl+m`. O default fica sem mouse, resolvendo o problema de seleção nativa imediatamente. O scroll via mouse é opt-in.

**Decisão final:** Default = mouse **off**. `ctrl+m` liga o mouse (scroll). Estado mostrado na status bar. Isto resolve a seleção nativa por padrão.

### 5.1.2 Message queue

O daemon mantém uma run de cada vez (`tuiRunGuard`). A fila é implementada **no cliente TUI**, não no daemon.

**Decisão:**

- Quando `waiting==true` e Enter é pressionado, a mensagem vai para `pendingQueue`.
- Badge `⏳ N pending` é renderizado acima do input.
- No handler de `stream_end`/`error`, após limpar o estado `waiting`, verifica-se `pendingQueue`. Se houver mensagens, envia-se a primeira.
- `esc` cancela o turno atual (`pipeSvc.Cancel`) mas não limpa a fila.

```go
func (m Model) dequeueNext() tea.Cmd {
    if len(m.pendingQueue) == 0 {
        return nil
    }
    next := m.pendingQueue[0]
    m.pendingQueue = m.pendingQueue[1:]
    return m.submitQueuedMessage(next)
}
```

### 5.1.3 Persistent input history

O histórico é guardado em `~/.aurelia/tui_history.json` (array de strings). Limite de 1000 entradas.

**Decisão:** Guardar no shutdown (quit ou sinal), carregar no `NewModel`. Usar `sync.Once` para evitar escritas concorrentes.

### 5.1.4 Command auto-complete

Lista fixa de comandos: `/help`, `/status`, `/cwd`, `/model`, `/img`, `/attach`.

**Decisão:** Quando o input começa com `/` e o utilizador pressiona `tab`, mostra dropdown inline com comandos que fazem prefix match. `enter` ou `tab` novamente insere o comando completo. Se houver apenas uma match, preenche automaticamente.

### 5.2.1 Theme

O Bubble Tea v1 não tem theme system nativo. Implementamos um mapa de estilos por tema.

```go
type themeStyles struct {
    UserStyle           lipgloss.Style
    AssistantStyle      lipgloss.Style
    ErrorStyle          lipgloss.Style
    InputBoxStyle       lipgloss.Style
    InputWaitingStyle   lipgloss.Style
    StatusBarStyle      lipgloss.Style
    SidebarStyle        lipgloss.Style
    HeaderTitleStyle    lipgloss.Style
    HeaderMetaStyle     lipgloss.Style
    MessageSeparator    lipgloss.Style
    StatusReadyStyle    lipgloss.Style
    StatusBusyStyle     lipgloss.Style
    StatusErrorStyle    lipgloss.Style
    ChatModeStyle       lipgloss.Style
}
```

**Deteção:**
- `$TERM_PROGRAM` pode indicar terminal conhecido.
- `$COLORFGBG` pode indicar light/dark em alguns terminais.
- `$COLORTERM=truecolor` não indica tema, apenas capability.
- Como deteção fiável é difícil, o default é `dark` e `--theme auto` tenta inferir; `--theme light`/`dark` força.

### 5.2.2 Rich status bar

A status bar passa a incluir:

```text
● ready   ·   gpt-4o   ·   ⏳ 1   ·   0:12   ·   ↵ send   ·   alt+enter newline
```

Campos (truncados conforme largura):
1. Estado (ready/waiting/error/offline)
2. Modelo ativo
3. Pending count (se > 0)
4. Elapsed time do turno atual (se waiting)
5. Keybindings

### 5.3.1 `--session` flag

Parsing: `--session <name>` onde `<name>` pode ser `dm` ou um nome sem prefixo `tui:`. O TUI resolve para ChatID.

**Fluxo:**
1. `main.go` parseia flag.
2. Cria modelo com `activeSession` pré-definido.
3. No `Init`, se `--session` foi fornecido, envia `MsgTypeSessionOpen` ou `MsgTypeSessionCreate`.
4. Se a sessão não existir, cria antes de abrir.

### 5.3.2 Cross-platform builds

**Makefile:**

```makefile
.PHONY: tui
TUI_BINARY := aurelia-tui

tui:
	go build -o $(TUI_BINARY) ./cmd/aurelia-tui

tui-all:
	GOOS=linux GOARCH=amd64 go build -o dist/$(TUI_BINARY)-linux-amd64 ./cmd/aurelia-tui
	GOOS=linux GOARCH=arm64 go build -o dist/$(TUI_BINARY)-linux-arm64 ./cmd/aurelia-tui
	GOOS=darwin GOARCH=amd64 go build -o dist/$(TUI_BINARY)-darwin-amd64 ./cmd/aurelia-tui
	GOOS=darwin GOARCH=arm64 go build -o dist/$(TUI_BINARY)-darwin-arm64 ./cmd/aurelia-tui
```

**CI:** Matrix de GOOS/GOARCH no workflow existente, upload de artifacts.

## Edge Cases

| Caso | Comportamento |
|------|---------------|
| Mouse toggle durante streaming | Aplica-se na próxima renderização; não interrompe stream |
| Fila cheia (limite arbitrário 10) | Novas mensagens mostram erro "Queue full" |
| Histórico corrompido | Ignora e começa vazio; loga warning |
| `--session` com nome inválido | Sai com erro claro |
| Tema claro em terminal dark | Utilizador usa `--theme dark` |
| Largura insuficiente para status bar | Campos de menor prioridade são omitidos |

## Lições Aplicadas

- **filepath-base-traversal**: validação de `--session` rejeita `.`, `..`, separadores e control chars.
- **goroutine-recovery**: recriação do programa no mouse toggle usa `defer recover()` se houver goroutines próprias.
- **redaction-before-truncation**: histórico guardado sem truncamento; conteúdo sensível não é persistido.
