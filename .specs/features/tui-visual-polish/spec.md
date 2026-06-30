# TUI Visual Polish — Especificação

**Status:** ✅ Historical — entregue em v0.34.0; refinamentos finais em Sprint J (v0.35.0)
**Sprint:** TUI Visual Richness  
**Depende de:** `tui-rich-components` (v0.33.0 em `main`)  
**Desbloqueia:** TUI com identidade visual forte, affordances claras e densidade de informação sem ruído

---

## Problem Statement

A TUI do Aurelia (v0.33.0) ficou **funcionalmente rica** — forms huh, modais, histórico paginado, progresso de streaming, badges de não-lidas, rato e F1 help. O screenshot de validação (sessão *Chat*, chat mode, daemon ready) revela que o problema passou de *capabilities* para **percepção de produto**:

1. **Vazio dominante** — ~70% do ecrã é espaço negro sem significado; parece incompleto, não intencionalmente minimal.
2. **Redundância de contexto** — `chat mode`, `daemon ready` e `Aurelia` aparecem em header e sidebar.
3. **Sidebar híbrida** — tabela de sessões, bloco Project/Daemon e “+ New session (click)” empilhados sem hierarquia visual; parecem widgets colados.
4. **Hierarquia plana** — rosa/magenta (205) usado em título, sidebar, bordas e estados; tudo compete pelo mesmo peso.
5. **Affordances fracas** — interacção clicável indicada por texto “(click)” em vez de controles visuais.
6. **Transcript estilo log** — `▶ nome · hora` + linha `─` é legível mas não evoca conversa nem produto moderno.
7. **Coluna Model inconsistente** — só a sessão activa mostra modelo; outras linhas vazias quebram a tabela.
8. **Input genérico** — caixa “Type a message..” não comunica modo, project, fila, anexos ou comandos disponíveis.
9. **Status bar densa** — muitos atalhos em texto corrido; difícil de escanear e pouco “dashboard”.

### Baseline técnico (já entregue em v0.33.0)

- `bubbles/table` na sidebar, modais unificados (`modal.go`), glamour no transcript
- Paleta `theme.go` (dark/light), animações (`animation.go`), layout dinâmico (`layout.go`)
- Status bar com model, F1 help clicável, progress bars

Este sprint **não repete** rich components; **refina** superfície visual e interacção discoverable.

---

## Goals

### Fase A — Quick wins (hierarquia e affordances)

- [ ] Sidebar em **3 painéis** separados: Sessions · Context · Actions
- [ ] Header com **chips** (model, health, mode) — uma linha, zero duplicação
- [ ] Status bar **compacta** por defeito; expandida só em terminais largos
- [ ] Segmentos clicáveis com **hover** (não só underline com rato ligado)
- [ ] Input com **placeholder dinâmico** e borda por estado (idle / waiting / pending)
- [ ] Coluna Model na sidebar **nunca vazia** (`—` muted ou valor cacheado)

### Fase B — Visual richness (conversa e superfícies)

- [ ] **Message bubbles** — user e assistant com borda, padding e opcional alinhamento
- [ ] **Empty / welcome state** persistente no fundo do viewport quando histórico curto
- [ ] **Autocomplete dropdown** estilizado (lipgloss) em vez de badges inline
- [ ] Paleta **surface tokens** (`surface-0/1`, accents reservados) em `theme.go`
- [ ] **Hover highlight** na sidebar table (usar `sidebarHoverRow` visualmente)
- [ ] Ícones por **tipo de sessão** (DM, project, grupo) — substituir `○/●` puros

### Fase C — Interacção premium

- [ ] **Command palette** (`Ctrl+K`) — fuzzy: sessões, comandos, cwd, model (`Ctrl+P` mantém project panel)
- [ ] **`@` file references** — fuzzy paths do cwd no composer
- [ ] **Attention** — notificação/som quando resposta completa e TUI sem foco (**default ligado**, `--no-attention` para desligar)

### Stretch (pós-MVP — temas nomeados adiados)

- [ ] **Temas nomeados** (`warm`, `high-contrast`) — reavaliar após v0.34.0

---

## Out of Scope

- Syntax highlighting dedicado em blocos de código (glamour cobre o básico)
- Imagens inline no transcript (fora do escopo terminal)
- Sincronização visual TUI ↔ Telegram
- Reescrita completa para outro framework TUI (permanece bubbletea v2)
- Web UI ou GUI nativa

---

## Referências

| Fonte | O que absorver |
|-------|----------------|
| [OpenCode TUI](https://opencode.ai/docs/tui/) | Command palette, `@` refs, attention, temas nomeados |
| Charm (bubbletea / lipgloss / harmonica) | Bordas arredondadas, animações subtis, composição |
| lazygit | Painéis com foco, seleção de alto contraste, ícones |
| [clig.dev](https://clig.dev/) | Progressive disclosure — essencial visível, resto em modais |
| [Evil Martians CLI UX](https://evilmartians.com/chronicles/cli-ux-best-practices-3-patterns-for-improving-progress-displays) | Progresso e feedback de estado sempre visíveis |

---

## User Stories

### A.1 Sidebar em painéis

**Como** utilizador da TUI,  
**quero** ver sessões, contexto do projecto e acções em secções visualmente distintas,  
**para** perceber de relance onde clicar e o que está activo.

**Acceptance criteria:**

1. WHEN a sidebar está visível THEN existem separadores entre Sessions, Context e Actions.
2. WHEN passo o rato numa sessão THEN a linha ganha highlight distinto do estado activo.
3. WHEN uma sessão não tem model conhecido THEN a coluna Model mostra `—` (muted), nunca célula vazia.
4. WHEN clico em Project ou model no painel Context THEN abre o form correspondente (cwd / model).

### A.2 Header sem redundância

**Como** utilizador,  
**quero** um header de uma linha com chips clicáveis,  
**para** não ler a mesma informação três vezes.

**Acceptance criteria:**

1. WHEN em chat mode THEN um único chip `chat mode` aparece (não duplicado na sidebar).
2. WHEN o daemon está ready/waiting/offline THEN um chip de saúde único substitui “daemon ready · ready”.
3. WHEN clico no chip do model THEN abre o wizard `/model`.

### A.3 Status bar dashboard

**Como** utilizador,  
**quero** uma barra inferior escaneável com o essencial e atalhos opcionais,  
**para** orientar-me sem manual embutido.

**Acceptance criteria:**

1. WHEN `width < 100` THEN a barra mostra: health · model · F1 help · mouse.
2. WHEN `width >= 160` THEN atalhos adicionais aparecem (send, project, search, quit).
3. WHEN passo o rato em segmento clicável THEN feedback visual (invert ou underline) mesmo com mouse já ligado.

### B.1 Message bubbles

**Como** utilizador,  
**quero** mensagens com corpo visual (caixa),  
**para** distinguir turnos de conversa como num chat moderno.

**Acceptance criteria:**

1. WHEN mensagem é do utilizador THEN renderiza em caixa com borda accent-user.
2. WHEN mensagem é da Aurelia THEN glamour renderiza **dentro** da caixa assistant.
3. WHEN mensagem é sistema (📎, ⚠️) THEN estilo compacto sem caixa grande.

### C.1 Command palette

**Como** utilizador power,  
**quero** `Ctrl+K` para aceder a sessões e comandos por fuzzy search,  
**para** não memorizar slash commands sem perder `Ctrl+P` para o project panel.

**Acceptance criteria:**

1. WHEN pressiono `Ctrl+K` THEN abre modal palette com input fuzzy.
2. WHEN pressiono `Ctrl+P` THEN continua a abrir/fechar o project panel (sem regressão).
3. WHEN selecciono item na palette THEN executa acção (trocar sessão, abrir form, enviar comando).
4. WHEN `Esc` THEN fecha palette sem efeitos colaterais.

### C.2 Attention

**Como** utilizador,  
**quero** ser notificado quando a resposta termina e não estou a olhar para o terminal,  
**para** não ficar à espera sem perceber.

**Acceptance criteria:**

1. WHEN `stream_end` e TUI sem foco THEN bell ou notificação desktop (best-effort).
2. WHEN arranco com `--no-attention` THEN sem som/notificação.
3. WHEN default THEN attention ligado.

---

## Success Metrics

- Screenshot “antes/depois” na mesma resolução (120×30 mínimo): menos área vazia percebida, hierarquia clara em 5s de olhar.
- Nenhuma regressão: `go test ./internal/tui/... -short`, status bar continua numa linha em `width=120`.
- Igor valida live: sidebar clicável óbvia sem ler “(click)”; F1 help e palette discoverable.

---

## Branch & Release

```
feature/tui-visual-polish  →  stable/tui-visual-polish  →  main
```

Versão sugerida após merge: **v0.34.0** (minor — melhoria visual significativa, sem breaking API).