# TUI Rich Components — Tasks

**Sprint:** TUI Rich UX
**Branch:** `feature/tui-rich-components`
**Estimativa total:** 8-12 dias

---

## Fase 1 — Sidebar & Navegação

### T1.1 — Sidebar com bubbles/table

**Objetivo:** Substituir renderização manual da sidebar por `bubbles/table` com colunas enriquecidas.

- [ ] Criar `internal/tui/sidebar.go` com `sidebarModel` struct.
- [ ] Definir `sidebarRow` struct: Icon, Name, Unread, Model, ChatID, ThreadID.
- [ ] Implementar `newSidebarTable()` que cria `table.Model` com colunas.
- [ ] Mapear `sessions []tuiSessionInfo` → `[]sidebarRow`.
- [ ] Atualizar `Model` com `sidebarTable table.Model` e `sidebarRows []sidebarRow`.
- [ ] Atualizar `renderSidebar()` em `view.go` para usar `sidebarTable.View()`.
- [ ] Adicionar estilos da tabela em `theme.go` (dark + light).
- [ ] Atualizar `update.go` com handlers de teclas na sidebar (`j`/`k`, `enter`).
- [ ] Adicionar badge de mensagens não-lidas (contador).
- [ ] Adicionar indicador visual de sessão ativa (highlight row).
- [ ] Criar `internal/tui/sidebar_test.go` com testes de navegação.
- [ ] Garantir `go build ./... && go test ./internal/tui/... -short` passa.
- [ ] Deploy + teste manual (navegar entre sessões).

**Critério de saída:** Sidebar funcional com table, navegação `j`/`k`, badges.

---

### T1.2 — Sidebar com bubbles/list (prototipar)

**Objetivo:** Prototipar `bubbles/list` como alternativa à table para comparação.

- [ ] Criar branch `prototype/tui-sidebar-list`.
- [ ] Implementar sidebar com `list.Model` (título + descrição).
- [ ] Comparar UX: table vs list (espaço, legibilidade, navegação).
- [ ] Decidir com Igor qual abordagem seguir.
- [ ] Merge ou descartar protótipo.

**Critério de saída:** Decisão table vs list documentada.

---

## Fase 2 — Comandos Interativos (huh)

### T2.1 — Integração huh no Model

**Objetivo:** Adicionar suporte a forms huh como overlay no TUI.

- [ ] Adicionar `github.com/charmbracelet/huh` ao `go.mod`.
- [ ] Adicionar `activeForm huh.Model` e `formOverlayOpen bool` ao `Model`.
- [ ] Atualizar `Update()` para rotear mensagens para `activeForm` quando aberto.
- [ ] Atualizar `View()` para renderizar form como overlay central.
- [ ] Implementar `Esc` para fechar form sem ação.
- [ ] Testar composição huh + bubbletea (não quebrar estado do chat).
- [ ] `go build ./...` limpo.

**Critério de saída:** Forms huh podem ser abertos/fechados sem quebrar chat.

---

### T2.2 — Comando /cwd com filepicker

**Objetivo:** `/cwd` abre filepicker huh para selecionar diretório.

- [ ] Criar `newCwdForm(currentPath string) huh.Model` em `internal/tui/commands.go`.
- [ ] Form: `huh.NewFilePicker()` com `CurrentDirectory` = home ou cwd atual.
- [ ] Submit → chamar IPC para atualizar cwd.
- [ ] Atualizar `update.go`: detectar `/cwd` (sem args) → abrir form.
- [ ] Manter compatibilidade: `/cwd /path/to/dir` ainda funciona direto.
- [ ] Atualizar status bar após confirmação.
- [ ] Adicionar testes em `internal/tui/commands_test.go`.
- [ ] Deploy + teste manual.

**Critério de saída:** Usuário seleciona diretório visualmente.

---

### T2.3 — Comando /model com select huh

**Objetivo:** `/model` abre select huh com modelos disponíveis.

- [ ] Criar `newModelSelect(models []string, current string) huh.Model` em `commands.go`.
- [ ] Obter lista de modelos via IPC (já disponível no status).
- [ ] Form: `huh.NewSelect[string]()` com opções da lista.
- [ ] Submit → chamar IPC para atualizar modelo.
- [ ] Atualizar status bar com novo modelo.
- [ ] Deploy + teste manual.

**Critério de saída:** Usuário seleciona modelo de uma lista.

---

### T2.4 — Comando /new com form huh

**Objetivo:** `/new` abre form huh para criar nova sessão.

- [ ] Criar `newSessionForm() huh.Model` em `commands.go`.
- [ ] Campos: nome (input), chat_id (input, opcional), modelo (select).
- [ ] Submit → chamar IPC para criar sessão.
- [ ] Sidebar atualiza automaticamente com nova sessão.
- [ ] Deploy + teste manual.

**Critério de saída:** Nova sessão criada via form.

---

### T2.5 — Confirmações huh

**Objetivo:** Ações destrutivas pedem confirmação visual.

- [ ] `/reset` → `huh.NewConfirm()` com "Reset session? This cannot be undone."
- [ ] `/clear` → `huh.NewConfirm()` com "Clear chat history?"
- [ ] Confirm → executar comando; Cancel → voltar ao chat.
- [ ] Deploy + teste manual.

**Critério de saída:** Ações destrutivas têm confirmação huh.

---

## Fase 3 — Progresso & Feedback

### T3.1 — Barra de progresso no streaming

**Objetivo:** Mostrar progress bar durante respostas longas.

- [ ] Criar `internal/tui/progress.go` com `streamProgress` struct.
- [ ] `streamProgress` contém `progress.Model` + `stopwatch.Model`.
- [ ] Iniciar progress bar após 2s de streaming (`tea.Tick(2*time.Second)`).
- [ ] Estimar `tokenMax` com média móvel dos últimos 5 responses.
- [ ] Atualizar `tokenEst` a cada chunk de streaming.
- [ ] Renderizar abaixo do input no `chatView()`.
- [ ] Esconder com fade-out ao completar.
- [ ] Adicionar estilos em `theme.go`.
- [ ] Não mostrar se resposta < 2s.
- [ ] Desligar se `--no-animations`.
- [ ] Deploy + teste manual (perguntar algo que gere resposta longa).

**Critério de saída:** Barra de progresso visível em respostas > 2s.

---

### T3.2 — Timer visível na status bar

**Objetivo:** Mostrar duração da resposta atual na status bar.

- [ ] Usar `stopwatch.Model` do `streamProgress` (T3.1).
- [ ] Atualizar `renderStatusBar()`: `⏱ 3.2s` enquanto resposta ativa.
- [ ] Remover ao completar.
- [ ] Testar visibilidade com diferentes durações.

**Critério de saída:** Timer visível durante respostas.

---

## Fase 4 — Histórico & Paginação

### T4.1 — Paginação do histórico

**Objetivo:** Navegar histórico de chat por páginas.

- [ ] Criar `internal/tui/history_nav.go` com `historyNav` struct.
- [ ] `historyNav` contém `paginator.Model` com `pageSize = 50`.
- [ ] `ctrl+f` → próxima página (se disponível).
- [ ] `ctrl+b` → página anterior (se disponível).
- [ ] Sincronizar viewport com página atual.
- [ ] Indicador "↓ New messages" quando usuário não está na última página.
- [ ] Indicador de página na status bar: `[2/5]`.
- [ ] Atualizar keybindings com novos atalhos.
- [ ] Deploy + teste manual.

**Critério de saída:** Navegação paginada funcional.

---

### T4.2 — Busca no histórico

**Objetivo:** Buscar texto no histórico de chat.

- [ ] `ctrl+s` → abre input de busca inline.
- [ ] Digitação filtra/scrolla para primeira ocorrência.
- [ ] Enter → próxima ocorrência.
- [ ] Esc → sai da busca.
- [ ] Destacar texto encontrado com cor de accent.
- [ ] Deploy + teste manual.

**Critério de saída:** Busca funcional no histórico.

---

## Fase 5 — Help & Keybindings

### T5.1 — Keymap com bubbles/key

**Objetivo:** Definir atalhos com `key.Binding` para help overlay nativo.

- [ ] Criar `internal/tui/keybindings.go` com `keymap` struct.
- [ ] Definir bindings por contexto: chat, sidebar, form, global.
- [ ] Mapear atalhos existentes para `key.Binding`.
- [ ] `?` → toggle help overlay.
- [ ] Atualizar `update.go` para usar `keymap` em vez de strings hardcoded.

**Critério de saída:** Todos os atalhos definidos como `key.Binding`.

---

### T5.2 — Help overlay com bubbles/help

**Objetivo:** Substituir help manual por `bubbles/help`.

- [ ] Adicionar `help help.Model` ao Model.
- [ ] `newHelpModel()` cria help com keymap da fase 5.1.
- [ ] `?` abre/fecha overlay.
- [ ] Help é contextual: mostra atalhos do contexto atual (chat, sidebar, form).
- [ ] Estilos em `theme.go` para help (dark/light).
- [ ] Remover `helpOverlayOpen bool` e render manual antigo.
- [ ] Deploy + teste manual (`?` em diferentes contextos).

**Critério de saída:** Help overlay nativo funcional.

---

## Fase 6 — Animações

### T6.1 — Integração harmonica

**Objetivo:** Adicionar animações suaves com harmónica.

- [ ] Adicionar `github.com/charmbracelet/harmonica` ao `go.mod`.
- [ ] Criar `internal/tui/animation.go` com `animState` struct.
- [ ] `animState` gerencia springs: spinnerOpacity, responseOpacity, badgePulse.
- [ ] Tick loop: `tea.Tick(16ms)` quando animações ativas.
- [ ] Detectar `$TERM` para desligar animações (dumb, vt100).
- [ ] Flag `--no-animations` para override manual.
- [ ] `go build ./...` limpo.

**Critério de saída:** Infraestrutura de animação pronta.

---

### T6.2 — Transições spinner → resposta

**Objetivo:** Spinner fade-out + resposta fade-in.

- [ ] Spinner fade-out: opacity 1.0 → 0.0 em 300ms.
- [ ] Resposta fade-in: opacity 0.0 → 1.0 em 200ms.
- [ ] Gatilho: evento `stream_end` do IPC.
- [ ] Aplicar opacity via lipgloss estilo (foreground color com alpha).
- [ ] Deploy + teste manual.

**Critério de saída:** Transição suave spinner → resposta.

---

### T6.3 — Pulse no badge de mensagens

**Objetivo:** Badge de não-lidas pulsa quando chega nova mensagem.

- [ ] Badge pulse: escala 1.0 → 1.3 → 1.0 em 500ms.
- [ ] Gatilho: nova mensagem em sessão não-ativa.
- [ ] Aplicar escala via padding/lipgloss.
- [ ] Deploy + teste manual.

**Critério de saída:** Badge animado.

---

## Fase 7 — Mouse Interaction

### T7.0 — Upgrade bubbletea + lipgloss para v2

**Objetivo:** Atualizar dependências para v2 (pré-requisito para mouse com Layers).

- [ ] Atualizar `go.mod`: `bubbletea v1.3.10` → `charm.land/bubbletea/v2 v2.0.7`.
- [ ] Atualizar `go.mod`: `lipgloss v1.1.1` → `charm.land/lipgloss/v2`.
- [ ] Atualizar imports em `internal/tui/*.go`.
- [ ] Adaptar `View() string` → `View() tea.View` em `model.go`, `view.go`.
- [ ] Configurar `MouseMode` no `tea.View`.
- [ ] Corrigir quebras de API (checkar `go build ./...`).
- [ ] Rodar `go test ./internal/tui/... -short` — garantir que nada quebrou.
- [ ] Deploy + teste manual (TUI abre, scroll funciona, sidebar navega).

**Critério de saída:** TUI funcional com bubbletea v2 + lipgloss v2, sem regressões.

**Risco:** 🔴 Mudança de API pode quebrar comportamentos sutis. Testar exaustivamente.

---

### T7.1 — Sidebar com Layers clicáveis

**Objetivo:** Implementar clique na sidebar usando lipgloss v2 Layers + Compositor.

- [ ] Criar `Layer` para sidebar container.
- [ ] Criar `Layer` por sessão com ID `session_<chatID>`.
- [ ] Criar `Compositor` com todas as layers (sidebar, chat, status).
- [ ] Implementar `OnMouse` handler que faz `comp.Hit(x, y)`.
- [ ] Criar `LayerHitMsg` custom message.
- [ ] Tratar `MouseClickMsg` em `Update()`: switch session por ID.
- [ ] Implementar hover highlight com `MouseMotionMsg`.
- [ ] Garantir que navegação por teclado (`j`/`k`) continua funcional.
- [ ] Atualizar `keybindings.go` com atalhos de mouse documentados.
- [ ] Deploy + teste manual (clicar em sessões, hover visível).

**Critério de saída:** Clique na sidebar troca de sessão; hover destaca linha.

---

### T7.2 — Áreas clicáveis adicionais

**Objetivo:** Expandir mouse para outras regiões da UI.

- [ ] Status bar clicável: clique no modelo → abre select; clique no cwd → abre filepicker.
- [ ] Botão "Nova Sessão" na sidebar (Layer com ID `btn_new_session`).
- [ ] Scroll wheel no viewport (já funciona, garantir com v2).
- [ ] Desabilitar mouse em terminais não-suportados (`$TERM=dumb`).
- [ ] Flag `--no-mouse` para desabilitar.
- [ ] Atualizar help overlay com atalhos de mouse.
- [ ] Deploy + teste manual.

**Critério de saída:** Status bar e botões respondem a clique.

---

## Ordem de Execução

```
F7.0 (upgrade v2) ── F1 (sidebar table) ──┬── F2 (huh forms) ── F3 (progress) ── F4 (history) ── F5 (help) ── F6 (animations)
                                           └── F7.1-F7.2 (mouse interaction com v2 Layers)
```

**F7.0 é o primeiro passo** — o upgrade para v2 desbloqueia mouse interaction.  
Fases 1-3 são as de maior impacto na UX diária. Fases 4-7 são polish.

## Dependências Externas

| Dependência | Versão | Status |
|-------------|--------|--------|
| `bubbles` (table, progress, paginator, help, key) | v0.20.0 | ✅ Já no go.mod |
| `bubbletea` | v2.0.7 | ❗ Upgrade v1→v2 |
| `lipgloss` | v2.x | ❗ Upgrade v1→v2 |
| `huh` | v0.7.0 | ❗ Nova |
| `harmonica` | v0.2.0 | ❗ Nova |

**Estimativa total:** 10-14 dias (17 tasks, 7 fases).
