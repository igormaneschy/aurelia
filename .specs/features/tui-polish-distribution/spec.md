# TUI Polish & Distribution — Especificação

**Status:** Draft — Junho 2026
**Sprint:** J (TUI) — Fase 5
**Depende de:** Fase 1 (IPC Layer), Fase 2 (TUI MVP), Fase 3 (Multi-sessão)
**Desbloqueia:** Experiência de terminal madura, fácil de instalar e distribuir

---

## Problem Statement

A TUI funcional está implementada, mas a experiência de uso diário ainda tem fricções identificadas:

1. **Mouse habilitado bloqueia seleção nativa de texto** — o utilizador não consegue copiar texto das respostas da Aurelia.
2. **Segunda mensagem em sequência é ignorada** — enquanto a Aurelia responde, Enter não faz nada; não há fila nem feedback.
3. **Histórico de input é perdido ao sair** — não há persistência entre execuções.
4. **Comandos exigem memorização** — não há auto-complete ou help inline.
5. **Visual é estático** — não adapta a temas claros, não mostra modelo ativo, não tem help overlay.
6. **Distribuição é manual** — não há `make tui`, `--session`, ou builds cross-platform.

---

## Goals

### Fase 5.1 — Input & Interaction Polish

- [ ] Toggle de mouse (`ctrl+m`) para ligar/desligar captura de mouse.
- [ ] Fila de mensagens pendentes com badge visual.
- [ ] Histórico de input persistente em `~/.aurelia/tui_history.json`.
- [ ] Auto-complete de comandos (`tab` ou `?` quando input começa com `/`).

### Fase 5.2 — Visual Polish & Theming

- [ ] Tema claro/escuro com deteção automática e override.
- [ ] Status bar enriquecida (modelo, daemon, pending count, duração).
- [ ] Help overlay (`?`).
- [ ] Indicadores visuais de estado do daemon.
- [ ] Melhor separação visual entre mensagens.

### Fase 5.3 — Distribution & Build

- [ ] `--session <name>` flag para abrir sessão directamente.
- [ ] Build targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- [ ] `go install github.com/igormaneschy/aurelia/cmd/aurelia-tui@latest`.
- [ ] `make tui` target.
- [ ] CI artifact pipeline.

---

## Out of Scope

- Seleção e cópia de texto programática no viewport (futuro; mouse toggle resolve o caso de uso imediato).
- Syntax highlighting de blocos de código.
- Navegação por mensagens com `j`/`k`.
- Sincronização de estado entre TUI e Telegram.

---

## User Stories

### 5.1.1 Mouse toggle

**Como** utilizador da TUI,
**quero** pressionar `ctrl+m` para desligar o mouse,
**para** poder selecionar e copiar texto das respostas com o meu terminal nativo.

**Acceptance Criteria:**

1. WHEN `ctrl+m` is pressed THEN the TUI toggles mouse capture on/off.
2. WHEN mouse is off THEN terminal native text selection works.
3. WHEN mouse is on THEN viewport scroll via mouse wheel works.
4. WHEN the TUI starts THEN mouse is on by default (current behavior preserved).
5. WHEN mouse state changes THEN the status bar shows a mouse indicator (🖱️ / ✋).

### 5.1.2 Message queue

**Como** utilizador da TUI,
**quero** enviar uma segunda mensagem enquanto a primeira ainda está a ser processada,
**para** não perder o meu raciocínio enquanto espero pela resposta.

**Acceptance Criteria:**

1. WHEN Enter is pressed while `waiting==true` THEN the message is enqueued, not dropped.
2. WHEN messages are enqueued THEN a badge `⏳ N pending` appears above the input.
3. WHEN the current stream ends THEN the next enqueued message is sent automatically.
4. WHEN Esc is pressed THEN only the current turn is cancelled; queued messages remain.
5. WHEN there are no queued messages THEN the badge disappears.
6. WHEN the daemon returns an error for the current turn THEN the queue is still processed.

### 5.1.3 Persistent input history

**Como** utilizador da TUI,
**quero** que o histórico de input persista entre execuções,
**para** reutilizar comandos e perguntas anteriores.

**Acceptance Criteria:**

1. WHEN the TUI exits THEN the input history is saved to `~/.aurelia/tui_history.json`.
2. WHEN the TUI starts THEN it loads the saved history.
3. WHEN ↑/↓ are used THEN they navigate the persisted history.
4. WHEN history exceeds 1000 entries THEN oldest are dropped.

### 5.1.4 Command auto-complete

**Como** utilizador da TUI,
**quero** pressionar `tab` após `/` para ver comandos disponíveis,
**para** não memorizar todos os comandos.

**Acceptance Criteria:**

1. WHEN the input starts with `/` and Tab is pressed THEN a list of matching commands is shown.
2. WHEN a command is selected THEN it is inserted into the input.
3. WHEN `?` is pressed with empty input THEN the help overlay opens.
4. WHEN `?` is pressed with `/` prefix THEN it shows command suggestions inline.

### 5.2.1 Theme auto-detection

**Como** utilizador da TUI,
**quero** que a interface respeite o tema do meu terminal,
**para** não ficar com cores estranhas em terminais claros.

**Acceptance Criteria:**

1. WHEN the terminal reports light background THEN the TUI uses the light theme.
2. WHEN the terminal reports dark background THEN the TUI uses the dark theme.
3. WHEN `--theme light|dark|auto` is provided THEN the flag overrides detection.
4. WHEN the theme changes THEN all styled components (header, status bar, messages) update.

### 5.2.2 Rich status bar

**Como** utilizador da TUI,
**quero** ver na status bar o modelo ativo e o estado do daemon,
**para** saber em que contexto estou a trabalhar.

**Acceptance Criteria:**

1. WHEN a model is active THEN the status bar shows the model name.
2. WHEN the daemon is offline THEN the status bar shows a disconnected indicator.
3. WHEN messages are queued THEN the status bar shows the pending count.
4. WHEN a turn is running THEN the status bar shows elapsed time.

### 5.3.1 `--session` flag

**Como** utilizador da TUI,
**quero** correr `aurelia-tui --session work` para abrir uma sessão específica,
**para** não ter de navegar na sidebar sempre que inicio.

**Acceptance Criteria:**

1. WHEN `--session <name>` is provided THEN the TUI opens that local session.
2. WHEN the session does not exist THEN it is created before opening.
3. WHEN no `--session` is provided THEN the default DM session opens.
4. WHEN the name is invalid THEN the TUI exits with a clear error.

### 5.3.2 Cross-platform builds

**Como** mantenedor do Aurelia,
**quero** builds automatizados para macOS e Linux,
**para** distribuir o `aurelia-tui` facilmente.

**Acceptance Criteria:**

1. WHEN `make tui` runs THEN it builds `aurelia-tui` for the current platform.
2. WHEN CI runs THEN it produces binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
3. WHEN `go install ...@latest` runs THEN it installs the TUI binary.

---

## Non-Functional Requirements

### Segurança

- Histórico de input guardado com permissão `0600`.
- `--session` valida o nome contra caracteres de controlo e path traversal.

### Performance

- Histórico carregado de forma lazy no startup.
- Fila de mensagens processada sem polling adicional.

### Compatibilidade

- Bubble Tea v1 mantido (não migrar para v2 nesta fase).
- Temas light/dark usam paletas seguras para daltonismo.
