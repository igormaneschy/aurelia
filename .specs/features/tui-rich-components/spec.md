# TUI Rich Components — Especificação

**Status:** Draft — Junho 2026
**Sprint:** TUI Rich UX
**Depende de:** `tui-polish-distribution` (fase 5 completa), `tui-image-input`, `tui-document-attachments`
**Desbloqueia:** Experiência de terminal rica, comparável a clients desktop leves

---

## Problem Statement

A TUI do Aurelia está funcional mas usa apenas 4 dos 14 componentes da biblioteca `bubbles`. Há oportunidades claras de enriquecimento:

1. **Sidebar é texto puro** — sem ícones, badges de mensagens não-lidas, ou indicadores de estado.
2. **Comandos são texto livre** — `/cwd`, `/model`, `/new` exigem digitação completa e memorização.
3. **Streaming não tem indicador de progresso** — só spinner genérico, sem noção de quanto falta.
4. **Histórico de chat é linear** — sem paginação, busca, ou navegação rápida.
5. **Help overlay é manual** — não usa o componente nativo `bubbles/help` com keybindings.
6. **Sem animações** — transições são bruscas (spinner → resposta, sessão switch).

---

## Goals

### Fase 1 — Sidebar & Navegação (bubbles/table, bubbles/list)

- [ ] Sidebar com `bubbles/table`: colunas (ícone, nome, msgs não-lidas, modelo).
- [ ] Ou `bubbles/list`: itens com título + descrição, navegação com `j`/`k`.
- [ ] Indicador visual de sessão ativa + sessões com mensagens pendentes.
- [ ] Atalho `ctrl+n` para nova sessão, `ctrl+d` para renomear.

### Fase 2 — Comandos Interativos (huh)

- [ ] `/cwd` abre form huh com filepicker ou input de path.
- [ ] `/model` abre select huh com lista de modelos disponíveis.
- [ ] `/new` abre form huh (nome da sessão, modelo, cwd opcional).
- [ ] Confirmações huh para ações destrutivas (`/reset`, `/clear`).

### Fase 3 — Progresso & Feedback (bubbles/progress, bubbles/timer)

- [ ] Barra de progresso durante streaming (token count estimado).
- [ ] Timer/stopwatch visível na status bar durante respostas.
- [ ] Indicador de typing (dots animados) quando daemon está processando.
- [ ] Progress bar para upload de imagens/anexos.

### Fase 4 — Histórico & Paginação (bubbles/paginator)

- [ ] Paginação do chat history (50 msgs por página).
- [ ] Atalhos `ctrl+f` / `ctrl+b` para forward/back.
- [ ] Busca inline no histórico (`ctrl+s`).

### Fase 5 — Help & Keybindings (bubbles/help, bubbles/key)

- [ ] Substituir help overlay manual por `bubbles/help` com keymap.
- [ ] Keybindings contextuais (chat vs sidebar vs form).
- [ ] Atalhos discoverable via `?` com lista formatada.

### Fase 6 — Animações (harmonica)

- [ ] Transição spinner → resposta com fade.
- [ ] Scroll suave no viewport (nova mensagem aparece).
- [ ] Pulse animation no badge de mensagens não-lidas.

### Fase 7 — Mouse Interaction (lipgloss v2 Layers)

- [ ] Clique na sidebar para trocar de sessão.
- [ ] Hover highlight nas linhas da sidebar.
- [ ] Clique em botões (nova sessão, fechar, etc.).
- [ ] Clique na status bar para ações rápidas.
- [ ] Scroll wheel no viewport (já existe).
- [ ] Upgrade bubbletea v1→v2 + lipgloss v1→v2 (pré-requisito).

---

## Out of Scope

- SSH/Wish access (médio prazo, spec separada).
- Syntax highlighting de blocos de código (depende de glamour upstream).
- Gravação/playback de sessões (vhs).
- Multi-painel (split view tipo tmux).
- Temas customizáveis pelo usuário (só light/dark por enquanto).

---

## User Stories

### F7.1 — Clique na sidebar para trocar de sessão

**Como** utilizador da TUI,
**quero** clicar numa sessão da sidebar para selecioná-la,
**para** alternar entre conversas sem usar o teclado.

**Acceptance Criteria:**

1. WHEN clico numa linha da sidebar THEN essa sessão é selecionada.
2. WHEN movo o mouse sobre a sidebar THEN a linha sob o cursor tem highlight.
3. WHEN clico fora da sidebar THEN o clique é ignorado (não troca de sessão).
4. WHEN sidebar não está visível (ecrã estreito) THEN mouse não tem efeito.
5. Navegação por teclado (`j`/`k`/Enter) continua a funcionar independentemente.

### F1.1 — Sidebar enriquecida com table

**Como** utilizador da TUI,
**quero** ver a lista de sessões com ícones, nomes, e badges de mensagens não-lidas,
**para** navegar rapidamente entre conversas e saber onde há atividade pendente.

**Acceptance Criteria:**

1. WHEN TUI inicia THEN sidebar mostra tabela com colunas: ícone, nome da sessão, badge de não-lidas.
2. WHEN uma nova mensagem chega numa sessão não-ativa THEN badge incrementa.
3. WHEN seleciono uma sessão e pressiono Enter THEN o chat carrega e o badge zera.
4. WHEN uso `j`/`k` THEN cursor move na sidebar.
5. WHEN pressiono `ctrl+n` THEN abre form huh para nova sessão.
6. WHEN pressiono `ctrl+d` numa sessão THEN abre confirmação huh para renomear.

### F2.1 — Comando /cwd com filepicker visual

**Como** utilizador da TUI,
**quero** digitar `/cwd` e ver um filepicker visual,
**para** selecionar o diretório do projeto sem digitar o caminho completo.

**Acceptance Criteria:**

1. WHEN digito `/cwd` e pressiono Enter THEN abre filepicker huh.
2. WHEN navego no filepicker com `j`/`k` e Enter THEN cwd é atualizado.
3. WHEN pressiono Esc no filepicker THEN volta ao chat sem alterar cwd.
4. WHEN o daemon confirma o cwd THEN status bar atualiza.

### F2.2 — Comando /model com select huh

**Como** utilizador da TUI,
**quero** digitar `/model` e ver uma lista selecionável de modelos,
**para** trocar de modelo sem digitar nomes complexos.

**Acceptance Criteria:**

1. WHEN digito `/model` e pressiono Enter THEN abre select huh com modelos disponíveis.
2. WHEN seleciono um modelo e pressiono Enter THEN o modelo é atualizado.
3. WHEN pressiono Esc THEN volta ao chat sem alterar.
4. WHEN modelo muda THEN status bar reflete o novo modelo ativo.

### F3.1 — Barra de progresso no streaming

**Como** utilizador da TUI,
**quero** ver uma barra de progresso durante respostas longas,
**para** ter noção de quanto tempo falta para a resposta completar.

**Acceptance Criteria:**

1. WHEN streaming começa THEN uma barra de progresso aparece abaixo do input.
2. WHEN tokens são recebidos THEN a barra avança proporcionalmente.
3. WHEN streaming termina THEN a barra desaparece com fade.
4. WHEN streaming é curto (< 2s) THEN barra não aparece (evita flicker).

### F4.1 — Histórico paginado

**Como** utilizador da TUI,
**quero** navegar o histórico de chat por páginas,
**para** revisar conversas antigas sem scroll infinito.

**Acceptance Criteria:**

1. WHEN pressiono `ctrl+f` THEN chat avança uma página (50 msgs).
2. WHEN pressiono `ctrl+b` THEN chat retrocede uma página.
3. WHEN pressiono `ctrl+s` THEN abre input de busca no histórico.
4. WHEN estou numa página antiga E chega nova mensagem THEN indicador "↓ novas mensagens" aparece.

### F5.1 — Help overlay nativo

**Como** utilizador da TUI,
**quero** pressionar `?` e ver todos os atalhos formatados,
**para** descobrir funcionalidades sem ler documentação.

**Acceptance Criteria:**

1. WHEN pressiono `?` THEN overlay bubbles/help aparece com keybindings.
2. WHEN pressiono `?` ou Esc no overlay THEN fecha.
3. Keybindings são contextuais (chat vs sidebar vs form).
4. Overlay usa o tema atual (light/dark).

### F6.1 — Transições suaves

**Como** utilizador da TUI,
**quero** transições visuais suaves entre estados,
**para** a experiência parecer mais polida e profissional.

**Acceptance Criteria:**

1. WHEN resposta chega THEN spinner faz fade-out e resposta faz fade-in.
2. WHEN mudo de sessão THEN sidebar destaca com animação.
3. WHEN nova mensagem chega em sessão inativa THEN badge pulsa.
4. Animações são desligadas automaticamente se `$TERM` não suportar (dumb, vt100).

---

## Métricas de Sucesso

| Métrica | Atual | Alvo |
|---------|-------|------|
| Componentes bubbles usados | 4/14 | 8+/14 |
| Tempo para trocar /cwd | ~10s (digitar path) | ~3s (filepicker) |
| Tempo para trocar /model | ~8s (digitar nome) | ~2s (select) |
| Tempo para trocar sessão | ~2s (teclado) | ~0.5s (clique mouse) |
| Descoberta de atalhos | Manual (ler docs) | Help overlay integrado |
| Percepção de velocidade | Spinner indeterminado | Barra de progresso + timer |
| Interação | Só teclado | Teclado + mouse (clique, hover) |

