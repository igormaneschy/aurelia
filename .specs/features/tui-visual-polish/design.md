# TUI Visual Polish — Design

## Contexto

Documento de design para a fase **visual + affordances** da TUI Aurelia, após o sprint `tui-rich-components` (v0.33.0). Foco: percepção de produto, hierarquia, superfícies e interacção discoverable — sem alterar o contrato IPC nem a lógica de negócio.

**Screenshot de referência:** validação Junho 2026 — sessão Chat, chat mode, 2 mensagens, sidebar com Project/Daemon empilhado, grande void no centro.

---

## Princípios de design

| Princípio | Aplicação na Aurelia TUI |
|-----------|--------------------------|
| **Progressive disclosure** | Barra compacta + palette/modais para o resto |
| **Uma fonte de verdade por facto** | Cada estado (mode, daemon, model) num sítio canónico |
| **Affordance > legenda** | Botões, chips e hover — não “(click)” |
| **Superfícies empilhadas** | `surface-0` chat · `surface-1` chrome · modais por cima |
| **Ritmo** | harmonica para transições já existentes; estender ao chrome |

---

## Arquitectura de packages (delta)

```
internal/tui/
  theme.go           ← surface tokens, bubble styles, palette presets
  view.go            ← header chips, composer, status segments
  sidebar.go         ← painéis Sessions | Context | Actions
  transcript.go      ← message bubbles, empty state anchor
  autocomplete.go    ← dropdown overlay
  palette.go         ← NOVO — command palette (Fase C)
  status_bar.go      ← NOVO — render compact/expanded, hit regions
  composer.go        ← NOVO — input + chips + hints (extrair de view.go)
```

Composição mantém `Model` com embeds (`transcriptModel`, `inputModel`, `chromeModel`); novos renderers são funções puras + estilos em `themeStyles`.

---

## Paleta — tokens “Aurelia surface” (dark)

Reservar **205 (magenta)** só para marca/título; estados usam cores semânticas.

| Token | Lipgloss / cor | Uso |
|-------|----------------|-----|
| `surface-0` | bg `235` | Fundo implícito do chat (viewport) |
| `surface-1` | bg `237`, border `238` | Sidebar, input, modais |
| `accent-brand` | `205` bold | “Aurelia” no título da app |
| `accent-user` | `39` | Igor, prompt, bubble user |
| `accent-assistant` | `205` suave / `213` | Aurelia, bubble assistant |
| `accent-success` | `42` | ready, online |
| `accent-warn` | `214` | chat mode, pending, unread |
| `accent-error` | `196` | offline, erros |
| `text-muted` | `244` | hints, placeholders |
| `text-faint` | `238` | separadores, rules decorativas |

**Light theme:** espelhar com fundos `252`/`254` e bordas `250`; validar contraste em Terminal.app.

**Temas nomeados (Fase C):** mapas `preset → themeStyles` (`default`, `warm`, `high-contrast`).

---

## Layout — wireframes ASCII

### Sidebar (alvo Fase A)

```
╭─ Aurelia ───────────────╮
│ local terminal          │
├─ Sessions ──────────────┤
│ ● 💬 Chat         [2]   │
│ ○ 📁 Trade      deep…   │
│ ○ 💬 DM           —     │
├─ Context ───────────────┤
│ 📂 no project      ⌁    │  ← click → cwd form
│ 🤖 deepseek-v4-flash    │  ← click → model wizard
│ 🟢 ready · chat mode    │  ← chip composto (único sítio)
├─────────────────────────┤
│ ╭─────────────────────╮ │
│ │  ＋ New session      │ │  ← pill button
│ ╰─────────────────────╯ │
╰─────────────────────────╯
```

**Decisões:**

- `renderSidebarTable()` passa a `renderSidebarPanels()` — três blocos com `HeaderRuleStyle` entre eles.
- Remover bloco Project/Daemon duplicado do fluxo “hints” quando painel Context existe.
- `+ New session`: `lipgloss` border rounded + padding; hover = `SidebarCursorStyle`.
- Ícones sessão: mapa `chatID / binding → emoji` (fallback `💬`).

### Chat header (alvo Fase A)

```
Aurelia / Chat          deepseek-v4-flash · 🟢 ready · chat mode
▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓
```

- Rule decorativa: gradiente `░▒▓` em vez de `─` uniforme (opcional, toggle `--no-animations`).
- Meta string única; remover repetição na sidebar (sidebar Context mostra só cwd + model + health).

### Transcript — bubbles (Fase B)

```
                    ╭─ Igor · 16:38 ─────╮
                    │ oi                 │
                    ╰────────────────────╯

╭─ Aurelia · 16:38 ──────────────────────╮
│ Oi, Igor. Beleza? Em que posso ajudar?   │
╰──────────────────────────────────────────╯
```

**Implementação:**

```go
type bubbleAlign int
const (
    alignLeft bubbleAlign = iota
    alignRight // user, apenas se contentWidth >= 80
)

func renderMessageBubble(styles themeStyles, header, body string, role messageRole, width int) string
```

- User: `InputBoxStyle` derivado, `alignRight` quando `width >= 80`.
- Assistant: fundo `237`, borda `238`, glamour `body` interior com `width - padding`.
- Streaming: fade existente (`fadeStyle`) aplica-se à caixa inteira.
- Sistema: uma linha `ErrorStyle` / warn — sem bubble.

**Empty state ancorado:** se `len(messages) < 3` e `viewport.AtBottom()`, renderizar `renderEmptyState` centrado na **metade inferior** do viewport (não substituir mensagens).

### Composer (Fase A/B)

```
 ⏳ 1 pending   📎 2
╭────────────────────────────────────────────────╮
│ > │                                            │
╰────────────────────────────────────────────────╯
  /help · /cwd · /model              F1 · ↵ send
```

- Extrair `renderInput()` → `composer.go`.
- **Placeholder dinâmico:**
  - chat mode: `Chat mode — sem project. /cwd ou F1 help`
  - com cwd: `Mensagem para aurelia/…`
  - waiting: `Aurelia a pensar…`
- **Borda input:** reutilizar `InputBoxStyle` / `InputWaitingStyle`; novo `InputPendingStyle` (border `214`) quando `pendingQueue > 0`.
- Linha de hints: só quando `textarea.Value()==""` e sem modal aberto.

### Status bar — modos (Fase A)

**Compact** (`width < 100`):

```
│ 🟢 ready │ deepseek-v4-flash │ F1 help │ 🖱 mouse │
```

**Expanded** (`width >= 160`):

```
│ 🟢 ready │ model │ F1 help │ ↵ send │ ⌃P project │ ⌃S search │ ⌃C quit │
```

- `status_bar.go`: `renderStatusBarCompact()`, `renderStatusBarExpanded()`, thresholds em constantes.
- Separador `│` com padding fixo para hit tests (`status_mouse.go` generalizar `statusBarSegmentHit`).
- Hover: `Background(236)` no segmento sob o cursor (`sidebarHoverRow` pattern para status).

### Autocomplete dropdown (Fase B)

Em vez de `▶ /help  /status` na linha acima do input:

```
╭──────────────────╮
│ ▶ /help          │
│   /status        │
│   /cwd           │
╰──────────────────╯
```

- Posicionar acima do composer; largura `min(40, inputWidth)`.
- `tab` / `shift+tab` ciclam; `enter` aplica.

### Command palette (Fase C)

- Overlay modal reutilizando `renderModalOverlay`.
- `bubbles/textinput` ou textarea single-line + fuzzy (`sahilm/fuzzy` já no ecossistema charm ou match simples).
- Entradas: sessões, `/help`, `/cwd`, `/model`, `/new`, `/status`, “Toggle mouse”, “Search history”.
- Atalho: `Ctrl+P` (project panel migra para item da palette **ou** mantém `Ctrl+P` e palette em `Ctrl+Shift+P` — **decisão pendente Igor**; default proposto: palette `Ctrl+P`, project panel só via `/status` e chip header).

### Attention (Fase C, opcional)

- Quando `stream_end` e terminal blurred (detecção best-effort: focus events se disponíveis), emitir bell `\a` ou OSC notification.
- Flag `--attention` / config `tui.attention.enabled` (default false).

---

## Interacção — mapa de affordances

| Zona | Elemento | Acção | Já existe? | Delta |
|------|----------|-------|------------|-------|
| Sidebar | Session row | Abrir sessão | Sim (click) | + hover paint |
| Sidebar | Project line | cwd form | Sim | + painel Context |
| Sidebar | Model line | model wizard | Parcial (status) | + na sidebar |
| Sidebar | + New session | new form | Sim | + estilo botão |
| Header | Model chip | model wizard | Não | Novo |
| Header | Health chip | — | Não | Novo (display only) |
| Status | F1 help | toggle help | Sim | + hover |
| Status | Model | model wizard | Sim | manter |
| Composer | hints | — | Não | Novo |
| Palette | fuzzy | várias | Não | Fase C |

---

## Animações (harmonica)

| Evento | Animação | Pacote |
|--------|----------|--------|
| Troca de sessão | flash ícone sessão 300ms | `sessionFlashUntil` — já wired, falta paint na table |
| Nova mensagem unread | pulse badge 1× | `SidebarUnreadStyle` + `BadgeScale` |
| Modal open | opacity 0→1 painel 150ms | harmonica |
| Bubble stream | opacity assistant | já existe `ResponseOpacity` |

`--no-animations`: desliga todas; mantém layout estático.

---

## Testes

- `layout_test.go`: status bar uma linha em widths 80, 120, 175.
- `sidebar_test.go`: painéis renderizam separadores; model column nunca vazia.
- `transcript_test.go`: bubble wrap; user align right só em width largo.
- `status_mouse_test.go`: novos segmentos clicáveis.
- `palette_test.go` (Fase C): fuzzy ranking, esc fecha.

Screenshots golden opcionais: guardar em `internal/tui/testdata/` apenas se estável (evitar flake ANSI).

---

## Decisões em aberto (para Igor)

1. **Ctrl+P:** command palette vs project panel — qual prevalece?
2. **User bubbles à direita:** activar em todos os terminais ou só `width >= 80`?
3. **Temas nomeados:** quantos presets no MVP (`warm`, `high-contrast`)?
4. **Attention:** incluir na Fase C ou adiar?

---

## Referência cruzada

- Baseline funcional: `.specs/features/tui-rich-components/`
- Distribuição / polish anterior: `.specs/features/tui-polish-distribution/`
- Código actual: `internal/tui/theme.go`, `view.go`, `sidebar.go`, `transcript.go`, `modal.go`, `status_mouse.go`