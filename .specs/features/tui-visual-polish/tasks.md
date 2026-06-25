# TUI Visual Polish — Tasks

**Sprint:** TUI Visual Richness  
**Branch:** `feature/tui-visual-polish`  
**Estimativa total:** 6–10 dias (Fases A+B); +3–5 dias se Fase C completa  
**Release alvo:** v0.34.0

---

## Fase A — Quick wins (hierarquia e affordances)

### T-A.1 — Surface tokens em `theme.go`

**Objetivo:** Introduzir tokens reutilizáveis sem quebrar temas existentes.

- [ ] Adicionar campos a `themeStyles`: `Surface0`, `Surface1`, `ChipStyle`, `ButtonMutedStyle`, `InputPendingStyle`.
- [ ] Preencher em `newDarkStyles()` / `newLightStyles()` conforme design.md.
- [ ] Reservar `205` para `accent-brand` (título); migrar usos excessivos de rosa em hints.
- [ ] Teste: `theme_test.go` — estilos renderizam sem string vazia.
- [ ] `go test ./internal/tui/... -short`

**Critério de saída:** tokens disponíveis; visual ainda igual (só refactor de estilos).

---

### T-A.2 — Sidebar em três painéis

**Objetivo:** Separar Sessions · Context · Actions com hierarquia clara.

- [ ] Renomear/refactor `renderSidebarTable()` → compor `renderSidebarPanels()`.
- [ ] Painel **Sessions:** título + table (sem Project/Daemon abaixo).
- [ ] Painel **Context:** cwd (click), model (click), chip health+mode único.
- [ ] Painel **Actions:** botão `+ New session` (lipgloss pill).
- [ ] Separadores horizontais entre painéis (`HeaderRuleStyle` ou `MessageSeparatorStyle`).
- [ ] Remover duplicação de `chat mode` / `daemon` do bloco hints antigo.
- [ ] Coluna Model: `—` muted quando vazio; cache per-session se IPC disponível (stretch).
- [ ] Ícones sessão: 💬 DM, 📁 project-bound, ○ default.
- [ ] `sidebar_test.go`: contém `Sessions`, `Context`, `+ New session`; model nunca string vazia.
- [ ] Deploy + validação live sidebar.

**Critério de saída:** screenshot sidebar com 3 secções óbvias; cliques cwd/model/new funcionam.

---

### T-A.3 — Header chips (uma linha)

**Objetivo:** Eliminar redundância header ↔ sidebar.

- [ ] `renderChatHeader()`: meta = `modelChip · healthChip · modeChip`.
- [ ] `healthChip`: 🟢 ready / 🟡 waiting / 🔴 offline (reutilizar `chromeState()`).
- [ ] `modeChip`: só se `isChatMode()`; omitir se redundante com Context (escolher **um** sítio — preferência header).
- [ ] Clicável: model chip → `openModelSelect()`.
- [ ] Rule decorativa opcional (`░▒▓`) atrás do header.
- [ ] Testes: header não contém “daemon ready” e “ready” duplicados.
- [ ] Deploy + validação live.

**Critério de saída:** uma linha de meta escaneável; sem triplicação de chat mode.

---

### T-A.4 — Status bar compacta / expandida

**Objetivo:** Dashboard escaneável; atalhos em terminais largos.

- [ ] Criar `status_bar.go` com `renderStatusBar()`, thresholds documentados.
- [ ] Modo compacto: health · model · F1 help · mouse.
- [ ] Modo expandido: atalhos actuais (rebalancear `min` widths).
- [ ] Separador `│`; borda top `238` na barra.
- [ ] Hover segment: background highlight em `MouseMotionMsg` (generalizar hit map).
- [ ] Actualizar `status_mouse_test.go` e `layout_test.go` (uma linha @ 120 cols).
- [ ] Deploy + validação live.

**Critério de saída:** barra legível em 80 cols; F1 help óbvio e clicável.

---

### T-A.5 — Composer com placeholder e borda por estado

**Objetivo:** Input comunica contexto e estado da fila.

- [ ] Extrair `composer.go` de `renderInput()` / lógica de badges.
- [ ] Placeholder dinâmico (chat mode / cwd / waiting).
- [ ] `InputPendingStyle` quando `len(pendingQueue) > 0`.
- [ ] Linha hints sob input (vazio, sem modal): `/help · /cwd · F1`.
- [ ] Testes: placeholder muda com `cwdPath` e `waiting`.
- [ ] Deploy + validação live.

**Critério de saída:** composer auto-explicativo sem abrir help.

---

### T-A.6 — Hover na sidebar table

**Objetivo:** Tornar óbvio que sessões são clicáveis.

- [ ] Em `syncSidebarRows()`, aplicar estilo hover quando `i == sidebarHoverRow && !sidebarFocused`.
- [ ] Remover texto “(click)” do hint — substituído por hover + botão pill.
- [ ] `sidebar_test.go` ou mouse test: hover altera render.
- [ ] Deploy + validação live com mouse on.

**Critério de saída:** hover visível; sem “(click)” na UI.

---

## Fase B — Visual richness

### T-B.1 — Message bubbles

**Objetivo:** Transcript estilo conversa, não log.

- [ ] `renderMessageBubble()` em `transcript.go`.
- [ ] User bubble: borda `accent-user`; **align right só se `width >= 80`** (decisão Igor); esquerda em narrow.
- [ ] Assistant bubble: fundo `surface-1`, glamour interior.
- [ ] Sistema: manter linha simples.
- [ ] Integrar search highlight (plain text path preservado).
- [ ] Testes: bubble contém body; markdown assistant dentro da caixa.
- [ ] Deploy + validação live com resposta longa markdown.

**Critério de saída:** duas mensagens do screenshot ficam claramente “cartões”.

---

### T-B.2 — Empty state ancorado

**Objetivo:** Reduzir void quando histórico curto.

- [ ] Se `len(messages) < 3` e última página: compor viewport com mensagens + `renderEmptyState` no espaço inferior.
- [ ] Empty state: título, `/help`, `/cwd`, `Ctrl+N`, `F1`.
- [ ] Teste: viewport vazio mostra welcome; com 2 msgs mostra welcome no fundo.
- [ ] Deploy + validação live após `/clear` ou sessão nova.

**Critério de saída:** ecrã não parece “vazio” com conversa curta.

---

### T-B.3 — Autocomplete dropdown

**Objetivo:** Substituir badges inline por menu.

- [ ] `renderAutocompleteDropdown()` com borda lipgloss.
- [ ] Posicionar acima do composer; não empurrar layout (overlay no footer stack).
- [ ] Manter ciclo tab / apply enter.
- [ ] Testes: dropdown lista opções; apply preenche textarea.
- [ ] Deploy + validação live com `/c`.

**Critério de saída:** autocomplete parece menu, não texto solto.

---

### T-B.4 — Animações de chrome

**Objetivo:** Ligar animações existentes ao visual novo.

- [ ] Flash sessão activa na table row (`sessionFlashUntil`).
- [ ] Pulse unread badge ao incrementar (`session_unread.go` → anim trigger).
- [ ] Modal fade-in respeitando `--no-animations`.
- [ ] Testes: anim state não quebra com `no-animations`.
- [ ] Deploy + validação live.

**Critério de saída:** troca de sessão e unread têm feedback subtil.

---

## Fase C — Interacção premium (opcional / stretch)

### T-C.1 — Command palette (`Ctrl+K`)

- [ ] `palette.go` + estado `paletteOpen` no chrome.
- [ ] Fuzzy sobre sessões e comandos.
- [ ] Modal overlay; Esc fecha.
- [ ] Atalho **`Ctrl+K`**; **`Ctrl+P` inalterado** (project panel).
- [ ] Registar `Ctrl+K` no help overlay e status bar expandida.
- [ ] Testes: `Ctrl+P` ainda abre project panel; `Ctrl+K` abre palette.
- [ ] Deploy.

**Critério de saída:** `Ctrl+K` abre palette; `/cwd` na palette abre form; `Ctrl+P` sem regressão.

---

### T-C.2 — `@` file references

- [ ] Detectar `@` no composer; fuzzy paths relativos ao cwd.
- [ ] Inserir path no texto ou anexo conforme tipo.
- [ ] Testes com temp dir.
- [ ] Deploy.

**Critério de saída:** `@READ` sugere `README.md` no cwd.

---

### T-C.3 — Temas nomeados (STRETCH — adiado)

> Igor: indeciso no MVP. Não bloqueia v0.34.0. Reabrir após validação visual das Fases A+B.

- [ ] `ParseTheme` aceita `warm`, `high-contrast`.
- [ ] `/theme` ou flag `--theme warm`.
- [ ] Documentar em help overlay.

---

### T-C.4 — Attention (aprovado — default on)

- [ ] Config `attention.enabled` default **true**; flag `--no-attention`.
- [ ] Bell / OSC notification em `stream_end` quando TUI sem foco (best-effort).
- [ ] Teste unitário do gating; sem spam em foco activo.
- [ ] Documentar em help overlay e `CHANGELOG`.
- [ ] Deploy + validação live (blur terminal, esperar resposta).

---

## Release checklist

- [ ] Todas as tasks da Fase A + B completas; Fase C sem T-C.3 (temas adiados).
- [ ] `go test ./internal/tui/... -short` verde.
- [ ] `golangci-lint run ./...` verde.
- [ ] `make deploy` + validação Telegram/TUI live.
- [ ] `stable/tui-visual-polish` → PR → `main`.
- [ ] `CHANGELOG.md` + bump **v0.34.0** (aprovação Igor).
- [ ] Screenshot antes/depois anexado à PR.

---

## Ordem de execução recomendada

```
T-A.1 → T-A.2 → T-A.3 → T-A.4 → T-A.5 → T-A.6
         ↓
T-B.1 → T-B.2 → T-B.3 → T-B.4
         ↓
T-C.* (paralelo após B estável)
```

**Primeiro PR stack sugerido:** A.1+A.2 (sidebar + tokens), depois A.3+A.4 (header + status), depois A.5+A.6+B.1 (composer + bubbles).