# TUI Rich Components — Design

## Contexto

Este documento descreve as decisões de design, integração de componentes bubbles/huh/harmonica e arquitetura para enriquecer a TUI do Aurelia.

## Estrutura de Packages

```
internal/
  tui/
    model.go              ← sidebarModel (table ou list), helpModel, progressBar, animState
    update.go             ← handlers para huh forms, filepicker, keybindings contextuais
    view.go               ← renderização dos novos componentes
    theme.go              ← estilos para table, list, progress, help
    sidebar.go            ← NOVO — sidebar com bubbles/table ou bubbles/list
    commands.go           ← NOVO — huh forms para /cwd, /model, /new
    progress.go           ← NOVO — streaming progress bar + timer
    history_nav.go        ← NOVO — paginação e busca no histórico
    keybindings.go        ← NOVO — keymap com bubbles/key + help overlay
    animation.go          ← NOVO — wrappers harmonica para transições
cmd/aurelia-tui/
  main.go                 ← flags adicionais (--no-animations)
```

## Componentes Bubbles Novos

### 1. `bubbles/table` — Sidebar de Sessões

**Alternativa avaliada:** `bubbles/list`
**Decisão:** `table` para sidebar (dados tabulares: ícone, nome, badges, modelo).

```go
type sidebarModel struct {
    table    table.Model
    sessions []sidebarRow
    width    int
    height   int
}

type sidebarRow struct {
    Icon      string // 💬 DM, 👥 grupo, 📁 projeto
    Name      string
    Unread    int    // badge count
    Model     string // modelo ativo na sessão
    ChatID    int64
    ThreadID  int
}
```

**Colunas da tabela:**
| Ícone | Sessão | Modelo |
|--------|--------|--------|
| 💬     | Igor (DM) | deepseek-v4-pro |
| 👥     | AutoTraders | gpt-5.5 |
| 📁     | aurelia/   | deepseek-v4-pro |

**Estilo:**
- Linha ativa: highlight com cor de accent
- Badge não-lidas: badge lipgloss com contagem (ex: `[3]`)
- Sessão sem atividade: cor muted

### 2. `huh` — Forms Interativos

**Integração:** huh forms são programas bubbletea separados? Ou integrados no Model?

**Decisão:** Integrados. huh.Form implementa `tea.Model`, podemos compor no Model principal com estado `formOpen huh.Model`.

```go
type Model struct {
    // ... existing fields ...
    activeForm      huh.Model // nil quando não há form aberto
    formOverlayOpen bool
}
```

**Fluxo `/cwd`:**
1. Usuário digita `/cwd` → `update()` detecta comando
2. `activeForm = newCwdForm()` (huh com filepicker ou input)
3. View renderiza form como overlay
4. Submit → fecha form, envia comando via IPC
5. Esc → fecha form sem ação

**Fluxo `/model`:**
1. Usuário digita `/model` → `update()` detecta
2. `activeForm = newModelSelect(models)` (huh.Select)
3. Submit → fecha, envia `/model <selecionado>`
4. Esc → fecha

**Fluxo `/new`:**
1. `activeForm = newSessionForm()` (huh.Form com inputs: nome, modelo, cwd)
2. Submit → fecha, envia comando de criar sessão

**Form de confirmação:**
- `/reset`, `/clear`, `/delete` → huh.Confirm com "Are you sure?"

### 3. `bubbles/progress` — Streaming Progress

**Estratégia:** Barra de progresso estimada com base em tokens recebidos vs max_tokens típico.

```go
type streamProgress struct {
    progress  progress.Model
    timer     stopwatch.Model
    active    bool
    tokenEst  int     // tokens recebidos até agora
    tokenMax  int     // estimativa (baseada no histórico da sessão)
}
```

**Regras de exibição:**
- Aparece após 2s de streaming (evita flicker em respostas curtas)
- Largura: mesma do input (contentWidth)
- Estilo: barra sutil (cores muted do tema)
- Ao completar: fade-out com harmonica (200ms)

### 4. `bubbles/paginator` — Navegação no Histórico

**Estratégia:** Manter o histórico completo em memória (já existe `messages []chatMessage`). Paginator controla janela visível.

```go
type historyNav struct {
    paginator  paginator.Model
    pageSize   int // 50 mensagens
    currentPage int
    totalPages  int
    searching   bool
    searchQuery string
}
```

**Estados:**
- **Normal:** scroll livre com viewport
- **Paginado:** `ctrl+f`/`ctrl+b` move páginas; viewport sync
- **Busca:** `ctrl+s` abre input; destaca matches; Enter vai para próxima ocorrência

**Indicador de novas mensagens:**
- Se usuário está em página N-1 e chega nova mensagem → "↓ New messages" no topo do viewport

### 5. `bubbles/help` — Help Overlay Nativo

**Substitui:** `helpOverlayOpen bool` + render manual em `view.go`.

```go
type helpModel struct {
    help  help.Model
    keys  keymap  // key.Binding agrupados por contexto
    open  bool
}

// Keymap contextual
type keymap struct {
    // Chat
    Send        key.Binding
    ScrollUp    key.Binding
    ScrollDown  key.Binding
    HistoryPrev key.Binding
    HistoryNext key.Binding

    // Sidebar
    SessionNext key.Binding
    SessionPrev key.Binding
    SessionNew  key.Binding
    SessionRename key.Binding

    // Global
    ToggleHelp  key.Binding
    ToggleMouse key.Binding
    Quit        key.Binding
}
```

### 6. `harmonica` — Animações

**Integração:** harmónica é uma lib de spring physics. Usamos para animar propriedades visuais.

**Animações planejadas:**

```go
type animState struct {
    enabled       bool // desligado se $TERM=dumb
    spinnerOpacity float64 // 1.0 → 0.0 no fade-out
    responseOpacity float64 // 0.0 → 1.0 no fade-in
    badgePulse    float64 // escala do badge
}
```

**Implementação:**
- `spinnerOpacity` decrementa de 1.0 → 0.0 em 300ms quando resposta chega
- `responseOpacity` incrementa de 0.0 → 1.0 em 200ms
- `badgePulse` oscila 1.0 → 1.3 → 1.0 em 500ms quando nova mensagem
- Cada frame: `tea.Tick(16ms)` (~60fps) para atualizar animações

**Fallback:** Se `$TERM` contém `dumb`, `vt100`, ou `--no-animations` flag → `animState.enabled = false`.

## Estrutura de Tipos e Estado

### Model extensions

```go
type Model struct {
    // ... existing fields ...

    // Sidebar enriquecida (F1)
    sidebarTable  table.Model
    sidebarRows   []sidebarRow

    // Forms huh (F2)
    activeForm      huh.Model
    formOverlayOpen bool

    // Progresso streaming (F3)
    streamProgress streamProgress

    // Histórico paginado (F4)
    historyNav historyNav

    // Help overlay nativo (F5)
    help helpModel

    // Animações (F6)
    anim animState
}
```

## Ciclo de Update

```
Update(msg) {
    // 1. Animações ativas?
    if anim.enabled && anim.hasActive() {
        return anim.update(msg)
    }

    // 2. Form huh aberto?
    if formOverlayOpen {
        return handleFormUpdate(msg)
    }

    // 3. Help overlay aberto?
    if help.open {
        return handleHelpUpdate(msg)
    }

    // 4. Busca no histórico?
    if historyNav.searching {
        return handleSearchUpdate(msg)
    }

    // 5. Sidebar focada?
    if sidebarFocused {
        return handleSidebarUpdate(msg)
    }

    // 6. Chat normal
    return handleChatUpdate(msg)
}
```

## Fase 7 — Mouse Interaction (lipgloss v2 Layers)

### Contexto

Bubbletea tem suporte completo a eventos de mouse desde v1 (`MouseMsg` com X, Y, Button, Action). O Aurelia já usa isso para scroll wheel no viewport. Mas para clique com hit-testing em regiões específicas da UI, há dois caminhos:

| Abordagem | API | Complexidade | Manutenção |
|-----------|-----|-------------|------------|
| **v1 manual** | `MouseMsg` + coordenadas hardcoded | Média | Frágil (muda layout = quebra) |
| **v2 Layers** | `lipgloss.Layer` + `Compositor.Hit()` | Baixa | Robusta (layout-aware) |

**Decisão: Upgrade para v2.** Bubbletea v2.0.7 (lançado 2026-06-01) e lipgloss v2 oferecem:
- `lipgloss.NewLayer(content)` — regiões da UI com ID
- `lipgloss.NewCompositor(root)` — composição de layers
- `compositor.Hit(x, y)` — devolve layer ID sob o cursor
- `tea.View.OnMouse(func(msg tea.MouseMsg) tea.Cmd)` — roteamento automático

### Padrão oficial (do exemplo `clickable` do bubbletea)

```go
// 1. Criar layers com IDs para cada região clicável
sidebarLayer := lipgloss.NewLayer(sidebarContent).ID("sidebar")
sessionRow1 := lipgloss.NewLayer(rowContent).ID("session_12345")
sessionRow2 := lipgloss.NewLayer(rowContent).ID("session_67890")
sidebarLayer.AddLayers(sessionRow1, sessionRow2)

// 2. Compositor faz hit-testing automático
root := lipgloss.NewLayer(bg)
root.AddLayers(sidebarLayer, chatLayer, statusLayer)
comp := lipgloss.NewCompositor(root)

// 3. OnMouse mapeia coordenadas → layer ID
v.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
    mouse := msg.Mouse()
    x, y := mouse.X, mouse.Y
    if id := comp.Hit(x, y).ID(); id != "" {
        return func() tea.Msg {
            return LayerHitMsg{ID: id, Mouse: msg}
        }
    }
    return nil
}

// 4. Update() trata LayerHitMsg como qualquer mensagem
case LayerHitMsg:
    switch msg.Mouse.(type) {
    case tea.MouseClickMsg:
        if strings.HasPrefix(msg.ID, "session_") {
            chatID := parseSessionID(msg.ID)
            m.switchToSession(chatID)
        }
    }
```

### Áreas clicáveis planejadas

| Região | ID pattern | Ação |
|--------|-----------|------|
| Sidebar row | `session_<chatID>` | Switch para sessão |
| Sidebar row hover | mesmo ID | Highlight visual |
| Botão nova sessão | `btn_new_session` | Abrir form /new |
| Botão renomear | `btn_rename_<chatID>` | Abrir rename |
| Status bar — modelo | `status_model` | Abrir select /model |
| Status bar — cwd | `status_cwd` | Abrir filepicker /cwd |

### Eventos de mouse suportados

| Evento | Tipo | Uso |
|--------|------|-----|
| Click | `MouseClickMsg` | Selecionar, ativar |
| Release | `MouseReleaseMsg` | Confirmar ação |
| Motion | `MouseMotionMsg` | Hover highlight |
| Wheel | `MouseWheelMsg` | Scroll (já existe) |

### Upgrade v1→v2

**Impacto:** Bubbletea + lipgloss precisam ser atualizados para v2.

**Riscos:**
- API changes: `tea.Model` → mesmo nome, mas pacote muda de `github.com/charmbracelet/bubbletea` para `charm.land/bubbletea/v2`
- `lipgloss.Style` → mesma API, pacote muda para `charm.land/lipgloss/v2`
- `tea.View` é novo conceito (v1 não tem; usávamos `Model.View() string`)

**Migração:**
1. Atualizar imports em `internal/tui/*.go`
2. Adaptar `View() string` → `View() tea.View`
3. Configurar `v.MouseMode = tea.MouseModeAllMotion`
4. Adicionar `v.OnMouse` handler
5. Testar — a API core (`Model`, `Update`, `Cmd`) é compatível

---

## Dependências Novas

```go
// go.mod
require (
    github.com/charmbracelet/bubbles v0.20.0  // já existe
    github.com/charmbracelet/huh v0.7.0       // NOVA
    github.com/charmbracelet/harmonica v0.2.0 // NOVA
    charm.land/bubbletea/v2 v2.0.7            // UPGRADE v1→v2
    charm.land/lipgloss/v2 v2.x.x             // UPGRADE v1→v2
)
```

**Nota:** `bubbles` já está em v0.20.0 e inclui todos os componentes listados. `huh` e `harmonica` são dependências novas. O upgrade bubbletea+lipgloss para v2 é pré-requisito para mouse interaction (Fase 7).

## Riscos

| Risco | Mitigação |
|-------|-----------|
| huh compõe mal com Model bubbletea existente | huh.Model é tea.Model; testar composição cedo |
| harmonica precisa de ticks frequentes | Verificar se 16ms tick não impacta performance |
| table vs list — qual é melhor pra sidebar? | Prototipar ambos; table parece melhor para dados tabulares |
| Progress bar depende de estimativa de tokens | Usar média móvel dos últimos 5 responses |
