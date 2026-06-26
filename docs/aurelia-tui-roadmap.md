# Aurelia TUI — Plano de Implementação

**Sprint:** J (pós Auto-Skills)
**Status:** ✅ Sprint J concluído — Fases 0–5 mergeadas em `main`.
**Versão actual:** `v0.35.0` (Sprint J completo + IPC peer UID auth)
**Depende de:** Sprint E (Project Memory), Sprint F (Wiki Memory Gateway)
**Desbloqueia:** Interface alternativa ao Telegram, onboarding sem bot, uso em contexto de terminal/IDE

---

## Problema a Resolver

O Telegram é hoje o único ponto de entrada conversacional da Aurelia. Isso cria três fricções reais:

1. **Contexto de terminal** — trabalhar num projecto no terminal e ter de trocar para o telemóvel (ou Telegram Desktop) para falar com a Aurelia quebra o fluxo de trabalho
2. **Sessões isoladas** — no Telegram, os tópicos foram uma workaround para ter contextos separados (cada tópico com /cwd próprio). A TUI resolve isto nativamente com sessões locais nomeadas, sem precisar de importar nada do Telegram
3. **Dependência de conectividade** — o Telegram requer ligação à internet e autenticação externa; uma TUI local comunica directamente com o daemon

> **Decisão 2026-06-17:** Telegram e TUI são superfícies independentes
> por design. O `--attach` (continuar no terminal uma conversa iniciada
> no Telegram) foi removido do roadmap. Ver detalhes na Fase 5.

---

## Decisão de Arquitectura — Transport Interface

Antes de construir qualquer UI, o Aurelia precisa de uma abstracção de transport. Hoje o código do Telegram está misturado com lógica de pipeline. A refactorização central é extrair uma interface:

```go
// internal/transport/transport.go
type Transport interface {
    Receive() <-chan IncomingMessage
    Send(ctx context.Context, msg OutgoingMessage) error
    Name() string
}

type IncomingMessage struct {
    ChatID    int64
    ThreadID  int64
    UserID    int64
    Text      string
    Source    string // "telegram" | "tui" | "cli"
}

type OutgoingMessage struct {
    ChatID   int64
    ThreadID int64
    Body     string
    Markdown bool
}
```

O `TelegramTransport` implementa esta interface (sem mudança de comportamento). O `TUITransport` implementa a mesma interface. O pipeline recebe um `Transport` e não sabe a diferença.

Esta refactorização é **Fase 0** — pequena, localizada, sem risco de regressão, e desbloqueia tudo o resto.

---

## Stack de Tecnologia

| Componente | Biblioteca | Razão |
|---|---|---|
| **Framework TUI** | `charmbracelet/bubbletea` v1 (v1.3.10) | Go nativo, mesmo stack do Aurelia, arquitectura Elm (Model/Update/View), produção comprovada. V2 usa module path `charm.land/bubbletea/v2` — migração adiada para Fase 5 (Polish) para evitar risco de regressão cross-package |
| **Styling & Layout** | `charmbracelet/lipgloss` v1 | CSS-like para terminal — borders, cores, padding, flex layout |
| **Componentes** | `charmbracelet/bubbles` v0.20 | Lista, viewport, textarea, spinner, progress bar — não reinventar |
| **Markdown rendering** | `charmbracelet/glamour` v0.8 | Render de markdown no terminal com temas — respostas da Aurelia ficam formatadas |
| **IPC com daemon** | Unix socket | Comunicação com o processo `aureliad` já em execução |
| **Config** | Ficheiro existente do Aurelia | Reutiliza `~/.aurelia/config.yaml` |

> **Nota sobre Bubble Tea v1 vs v2 (2026-06-15):** O roadmap original previa Bubble Tea v2. Na data da implementação, as bibliotecas Charm v2 usam module paths diferentes (`charm.land/bubbletea/v2`, `github.com/charmbracelet/{bubbles,lipgloss,glamour}/v2`). A migração exige alterar todos os imports e verificar compatibilidade cross-package. Para minimizar risco no MVP, mantivemos as versões estáveis v1; a migração para v2 está agendada para a Fase 5 (Polish).

> **Nota sobre multiline (2026-06-15):** O `/cwd` no input usa `enter` para submeter (consistente com o roadmap). `alt+enter` insere nova linha; `ctrl+j` é fallback portável para terminais que não distinguem `shift+enter`. O status bar exibe a tecla activa: `alt+enter:newline`.

> **Nota UX hardening (2026-06-16):** A Fase 2 inclui uma sub-etapa 2.1 para reduzir fricção dos primeiros testes: a UI sobe sem pré-checar o socket no `main`, o ping inicial tem timeout curto, status/sidebar exibem estado de daemon e projecto, e a sidebar esconde-se automaticamente em terminais estreitos/baixos. Multi-sessão continua reservado para a Fase 3.

> **Nota Fase 2.2 polish (2026-06-16):** O MVP foi fechado com correções de input (`tab`/`ctrl+i`, `alt+<rune>` desconhecido e resíduos OSC `rgb:`), markdown com tema fixo para evitar queries de cor do terminal, uma linha limpa de respiro no topo, status bar compacta e sidebar mínima menos técnica. O polish visual avançado fica para Fase 5.

---

## Layout Visual da TUI

```
┌─────────────────────────────────────────────────────────┐
│                                                        │  ← top margin
├──────────────┬──────────────────────────────────────────┤
│              │                                          │
│  CHATS       │  #aurelia-dev / General                  │  ← Thread title
│  ──────────  │  ────────────────────────────────────    │
│  ● DM        │                                          │
│  ○ aurelia   │  Igor: preciso de um plano para o        │
│    ├ General │  sprint E                                │
│    ├ Dev     │                                          │
│    └ Infra   │  Aurelia: Vou analisar o estado actual   │
│              │  do roadmap. Tens 3 dependências         │
│  [+ novo]    │  pendentes...                            │
│              │                                          │
│              │  ──────────────────────────────────────  │
│              │  > _                                     │  ← Input
└──────────────┴──────────────────────────────────────────┘
│ q:quit  tab:chats  ctrl+p:project  ctrl+m:memory  ?:help  │  ← Status bar
└─────────────────────────────────────────────────────────┘
```

**Painéis:**
- **Sidebar esquerda** — lista de "chats" mapeados para `ConversationKey` (DM = chat directo, grupos = chat com lista de tópicos)
- **Painel principal** — histórico da conversa com markdown rendering, streaming de respostas em tempo real
- **Input bar** — textarea com suporte a multi-linha (`shift+enter`)
- **Top margin** — respiro visual; estado detalhado fica na sidebar/status
- **Status bar** — keybindings contextuais

---

## Mapeamento Telegram → TUI

Telegram e TUI são **superfícies independentes** por design (decisão
2026-06-17). Não há importação de sessões entre as duas. O mapeamento
abaixo mostra como conceitos equivalentes se traduzem, não que são
partilhados.

| Conceito Telegram | Conceito TUI | Implementação |
|---|---|---|
| Chat directo com o bot | Sessão DM local | `ConversationKey{chat_id: ReservedTUIChatID, thread_id: 0}` |
| Grupo com tópicos | Sessões locais nomeadas | Cada sessão = um `ChatID` no range reservado, com /cwd próprio |
| Mensagem de texto | Input da TUI | `IncomingMessage` via `TUITransport` |
| Resposta do bot | Viewport renderizado | Stream de `OutgoingMessage` com glamour |
| `/cwd`, `/status`, etc. | Comandos idênticos no input | Parser de comandos reutilizado do pipeline |
| Projecto activo | Indicador no header | Lê cwd do daemon |
| Sessão persistente | Retoma por `ConversationKey` | Mesmas chaves, mesmo SQLite — mas namespaces separados |

---

## Fases de Implementação

### Fase 0 — Transport Abstraction (1-2 dias)
*Pré-requisito para tudo. Sem esta fase, a TUI não é possível.*

**Tasks:**
- [x] Definir `Transport` interface em `internal/transport/`
- [x] Refactorizar `internal/telegram/` para implementar `TelegramTransport`
- [x] Injectar transport no pipeline via DI (sem mudar comportamento)
- [x] Testes: `MockTransport` para testes de pipeline existentes
- [x] `go build ./... && go test ./...` limpo

**Critério de saída:** pipeline funciona identicamente ao actual, Telegram ainda funciona, mas agora aceita qualquer `Transport`

---

### Fase 1 — IPC Layer (2-3 dias)
*Como a TUI fala com o daemon `aureliad` já em execução.*

**Opções avaliadas:**

| Opção | Pros | Contras |
|---|---|---|
| **Unix socket + JSON** | Simples, zero deps extra, Go nativo | Protocolo manual |
| **gRPC local** | Strongly typed, streaming nativo, protobuf | Adiciona dependência, mais setup |
| **Shared SQLite** | Zero infra, lê estado directamente | Acoplamento, sem streaming |

**Escolha recomendada:** Unix socket com JSON lines no MVP. gRPC em P2 quando houver mais surfaces (app desktop, VS Code extension).

```go
// internal/ipc/server.go — exposto pelo daemon
type IPCMessage struct {
    Type    string          `json:"type"`    // "send" | "subscribe" | "command"
    ChatID  int64           `json:"chat_id"`
    ThreadID int64          `json:"thread_id"`
    UserID  int64           `json:"user_id"`
    Text    string          `json:"text"`
}

type IPCEvent struct {
    Type    string `json:"type"`   // "message" | "stream_chunk" | "stream_end" | "error"
    Body    string `json:"body"`
    Done    bool   `json:"done"`
}
```

**Tasks:**
- [x] `internal/ipc/server.go` — Unix socket listener no daemon (`~/.aurelia/aurelia.sock`)
- [x] `internal/ipc/client.go` — cliente para a TUI usar
- [x] Streaming de respostas (chunks SSE-like via JSON lines)
- [x] Auth local: verificação de `user_id` por UID do processo Unix (`SO_PEERCRED` / `LOCAL_PEERCRED`) + socket `0600`

---

### Fase 2 — TUI MVP (1 semana) ✅
*Interface mínima funcional: uma conversa, comandos, streaming.*

**Tasks:**
- [x] `cmd/aurelia-tui/main.go` — binary separado `aurelia-tui`
- [x] Cliente IPC local para conversar com o daemon via Unix socket
- [x] Handler TUI no daemon com `ReservedTUIChatID`
- [x] Model principal com 3 estados: `loading | chat | error`
- [x] Viewport com glamour para rendering de markdown
- [x] Textarea com `alt+enter` para multi-linha (`ctrl+j` fallback)
- [x] Streaming de respostas em tempo real (chunks aparecem à medida que chegam)
- [x] Keybindings: `ctrl+c` sair, `ctrl+l` limpar, `tab` alternar sidebar
- [x] Status bar compacta com estado e keybindings
- [x] Sidebar mínima com sessão, projecto (`/cwd`) e estado do daemon
- [x] Graceful degradation: se daemon não estiver a correr, mensagem clara
- [x] UX hardening: filtros para atalhos/resíduos de terminal não poluírem input
- [x] `/help` local com comandos e atalhos do TUI
- [x] `/model` local para listar, refresh, auto e troca validada de modelo
- [x] Polish visual: blocos de mensagem, estado vazio, header/status responsivos
- [x] Cancelamento com `Esc`, spinner durante streaming e health check periódico
- [x] Scroll por teclado (`pgup`/`pgdown`) e mouse wheel

**Critério de saída:** ✅ consegues ter uma conversa completa com a Aurelia pela TUI, incluindo `/help`, `/cwd`, `/status`, `/model`, streaming e respostas em markdown.

---

### Fase 3 — Multi-sessão e Sidebar (3-4 dias) ✅
*Sessões locais isoladas, cada uma com /cwd e session file PI próprios.*

**Status:** Concluída, validada ao vivo e mergeada em `main` (via `stable/review-gap-fixes`, PR #17).
**Versão:** `v0.27.1`
**Branch:** `feature/tui-multi-session` → `stable/review-gap-fixes` → `main`

**Conceito de sessões locais:**

A TUI cria `ConversationKey` locais no namespace reservado [-9000009,
-9000001]. Cada sessão é um compartimento estanque com /cwd, session
file PI, e histórico independentes. Um utilizador pode ter:
- `tui:dm` — conversa directa local (sessão default, ChatID=-9000001)
- `tui:work` — sessão de trabalho com nome personalizado
- `tui:research` — outra sessão nomeada para um contexto diferente

> **Nota 2026-06-17:** Sessões importadas do Telegram (`telegram:...`)
> foram removidas do roadmap. Ver decisão na Fase 5. Telegram e TUI são
> superfícies independentes por design.

**Tasks:**
- [x] Sidebar com lista de sessões (locais + Telegram se autenticado)
- [x] Navegação: `↑↓` na sidebar, `enter` para abrir sessão
- [x] Criar nova sessão local: `n` na sidebar
- [x] Persistência de sessões TUI no SQLite do daemon
- [x] Header com nome da sessão, `cwd`, projecto activo
- [x] Histórico de mensagens por sessão (carregado do daemon ao abrir)

---

### Fase 4 — Painel de Estado do Projeto (3 dias) ✅
*Painel informativo para o projecto activo: cwd, memória, contexto.*

O Plan Mode explícito foi removido (decisão 2026-05-24). Planejamento permanece conversacional.
Em vez de um painel de plan mode, a TUI oferece um painel de estado do projecto activo sobrepondo-se
ao chat, útil para ver contexto rápido sem sair da conversa.

**Painel de Estado (overlay, `ctrl+p` para abrir/fechar):**

```
┌─ PROJECTO ───────────────────────────────────┐
│ Projecto: ~/dev/aurelia                      │
│ Binding: grupo → tópico Dev                  │
│ Agente activo: @coder                        │
│                                              │
│ Memória:                                     │
│  🟢 Global: 14 facts                         │
│  🟢 Projecto: 8 facts                        │
│                                              │
│ Último checkpoint:                           │
│  "Refactoring do pipeline concluído"         │
│   - 3 ficheiros alterados                    │
│                                              │
│ [/cwd] [/status] [/memory]                   │
└──────────────────────────────────────────────┘
```

**Tasks:**
- [x] Overlay de estado (`ctrl+p` para abrir/fechar)
- [x] Informação do projecto activo (cwd, binding source)
- [x] Resumo de memória (camadas activas, contagem de facts)
- [x] Último checkpoint da conversa
- [x] Sincronização em tempo real com o daemon (poll 30s)

---

### Fase 4.5 — Image Input / Vision (3-4 dias) ✅

*Enviar imagens ao modelo vision directamente do terminal — screenshots,
diagramas, fotos — sem trocar para o Telegram.*

**Status:** Concluída, validada ao vivo e mergeada em `main`.
**Versão:** `v0.27.x` → `v0.28.0` (smart vision fallback com model capability catalog)
**Branch:** `feature/tui-image-input` → `stable/tui-image-input` → `main`

**Spec:** `.specs/features/tui-image-input/spec.md`
**Design:** `.specs/features/tui-image-input/design.md`
**Tasks:** `.specs/features/tui-image-input/tasks.md`

Toda a infraestrutura backend já existia (bridge protocol, pipeline,
`applyVisionFallback`). Esta fase adicionou apenas a camada TUI.

**Métodos de input:**

| Método | UX | Prioridade |
|--------|-----|------------|
| `/img <path>` | Escreves `/img ~/screenshots/erro.png` + pergunta | P1 |
| `ctrl+v` (clipboard) | Screenshot → ctrl+v na TUI → pergunta | P2 |
| Drag-and-drop | Arrastas ficheiro para o terminal | P3 |

**Tasks:**
- [x] Extrair `encodeImageAttachment` para `pkg/images/` (partilhado Telegram + TUI)
- [x] Adicionar `IPCImage` ao protocolo IPC + validação
- [x] Handler do daemon: converter IPCImage → ImageAttachment, passar ao pipeline
- [x] TUI: comando `/img <path>` com badges `[📎 nome.png]`
- [x] TUI: `ctrl+v` clipboard (osascript macOS, xclip/wl-paste Linux)
- [x] TUI: drag-and-drop — detectar path de imagem em paste de texto
- [x] `ctrl+x` limpa imagens pendentes
- [x] Testes + validação ao vivo com modelo vision

**Critério de saída:** consegues enviar um screenshot à Aurelia pela TUI
e receber uma análise do modelo vision, com `applyVisionFallback`
trocando automaticamente se o modelo activo não suportar imagens.

---

### Fase 4.6 — Document Attachments (3-4 dias) ✅

*Anexar documentos (md, docx, ppt, pdf, etc.) ao projeto ativo diretamente da TUI.*

**Status:** Concluída, validada ao vivo e mergeada em `main`.
**Versão:** `v0.29.0`
**Branch:** `feature/tui-document-attachments` → `stable/tui-document-attachments` → `main`

**Status note:** `notes/tui-document-attachments-status.md`

**Conceito:**

Ao contrário das imagens, que são base64-encodadas e enviadas ao modelo vision,
documentos genéricos são **copiados para `<cwd>/uploads/`** e mencionados no
prompt. O agente é responsável por processá-los com as suas ferramentas.

**Decisões:**

- **CWD obrigatório** — `/attach` só funciona quando há um projeto ativo.
- **Qualquer formato permitido** — md, docx, ppt, pdf, txt, csv, etc.
- **Cópia segura** — `O_NOFOLLOW`, rejeição de symlinks, path traversal defense,
  renomeação em caso de conflito de nome.
- **Agente processa** — Aurelia não extrai/converte conteúdo.

**Métodos de input:**

| Método | UX | Prioridade |
|--------|-----|------------|
| `/attach <path>` | Escreves `/attach ~/docs/spec.pdf` + pergunta | P1 |
| Drag-and-drop | Arrastas ficheiro para o terminal | P2 |

**Tasks:**

- [x] `IPCAttachment` no protocolo IPC + validação (T0)
- [x] TUI: `/attach <path>`, drag-and-drop, badges `[📎 nome.pdf]` (T1-T3)
- [x] Daemon: resolver CWD, copiar para `<cwd>/uploads/`, anexar nota ao prompt (T4-T5)
- [x] Testes de unidade + integração
- [x] Documentação e roadmap (T6)
- [x] Validation: build, vet, test, deploy, passos de live test (T7)

**Critério de saída:** consegues anexar um documento ao projeto ativo pela TUI e
o agente responde com base no conteúdo do ficheiro.

**Spec:** `.specs/features/tui-document-attachments/spec.md`
**Design:** `.specs/features/tui-document-attachments/design.md`
**Tasks:** `.specs/features/tui-document-attachments/tasks.md`

---

### Fase 5 — Polish e Distribuição (5-7 dias)

A Fase 5 foi reestruturada em três sub-fases para separar interação,
visual e distribuição. Os pontos críticos identificados são: (1) o mouse
está habilitado mas bloqueia a seleção nativa de texto do terminal;
(2) enviar uma segunda mensagem enquanto a primeira está a ser
processada é silenciosamente ignorada; (3) a experiência visual pode
ser enriquecida sem adicionar complexidade ao core.

**Decisão 2026-06-17 — `--attach` removido do roadmap.**
O `--attach telegram:chat_id/thread_id` foi originalmente planeado para
permitir retomar no terminal uma conversa iniciada no Telegram. Com a
Fase 3 (multi-sessão local), cada sessão TUI tem o seu próprio /cwd,
session file PI, e histórico — o isolamento é nativo e não precisa de
importar nada do Telegram.

Análise de trade-offs:
- **A favor de remover**: complexidade de segurança (permitir ChatID
  positivo no IPC quebra o namespace local), races cross-surface (TUI
  e Telegram a enviar turnos concorrentes no mesmo session file),
  modelo mental confuso ("esta sessão é local ou espelho?"), caso de
  uso raro (se estás no computador, usas a TUI; se não estás, usas o
  Telegram — a sobreposição é marginal).
- **O contexto real é o /cwd**, não a conversa. O PI lê os ficheiros
  do projecto independentemente de onde a conversa aconteceu.
- **A infraestrutura para `--attach` já existe** (session.Store indexa
  por ChatID) — se a necessidade surgir no futuro, é uma camada fina
  por cima. A decisão não é irreversível.

As sessões Telegram e TUI são agora **compartimentos estanques** por
design. Telegram = comunicação assíncrona fora do computador. TUI =
trabalho focado no terminal.

---

#### Fase 5.1 — Input & Interaction Polish (2-3 dias)

Melhorias na forma como o utilizador interage com a TUI.

**Tasks:**
- [x] **Mouse toggle (`ctrl+o`)**: ligar/desligar captura de mouse (opt-in por defeito).
  - Quando ligado: scroll no viewport funciona.
  - Quando desligado: seleção nativa de texto do terminal funciona.
  - Estado guardado na sessão atual (não persistente).
- [x] **Fila de mensagens**: permitir enviar uma segunda mensagem
  enquanto a primeira está em streaming.
  - Mensagens pendentes são enfileiradas no modelo da TUI.
  - Badge visual `⏳ N pending` acima do input.
  - Próxima mensagem é enviada automaticamente após `stream_end`.
  - `esc` cancela apenas o turno atual; a fila continua.
- [x] **Histórico persistente de input**: guardar input history em
  `~/.aurelia/tui_history.json` e carregar no startup.
- [x] **Auto-complete de comandos**: `tab` mostra sugestões
  quando o input começa com `/`.

**Critério de saída:** consegues desligar o mouse para copiar texto,
enviar 2 mensagens em sequência sem perder a segunda, e reutilizar
histórico de input entre execuções.

---

#### Fase 5.2 — Visual Polish & Theming (2-3 dias)

Melhorias visuais e de descoberta de funcionalidades.

**Tasks:**
- [x] **Tema claro/escuro**: detectar `$TERM_PROGRAM`, `$COLORTERM` e
  preferência do sistema; permitir override via `--theme` ou config.
- [x] **Status bar enriquecida**: mostrar modelo ativo, estado do
  daemon, contagem de mensagens pendentes, e duração do turno.
- [x] **Help overlay (F1)**: painel flutuante com keybindings e
  comandos, sem sair da conversa.
- [x] **Indicadores de estado do daemon**: transição visual clara entre
  online/offline/reconnecting no header e na status bar.
- [x] **Separação visual mais forte entre mensagens**: divisores subtis
  sutil de background ou bordas para facilitar leitura longa.

**Critério de saída:** a TUI adapta-se ao tema do terminal, mostra
estado rico na status bar, e o utilizador pode descobrir todos os
atalhos com `?`.

---

#### Fase 5.3 — Distribution & Build (1-2 dias)

Tornar o `aurelia-tui` distribuível e fácil de instalar.

**Tasks:**
- [x] Mouse support (`tea.WithMouseCellMotion()` + scroll no viewport)
- [x] Resize handling básico (terminal window resize)
- [x] **`--session` flag**: `aurelia-tui --session tui:work` abre
  directamente na sessão indicada, criando-a se não existir.
- [x] **Build targets**: `linux/amd64`, `linux/arm64`, `darwin/amd64`,
  `darwin/arm64` (`make tui-all`).
- [x] **`go install`**: `go install github.com/igormaneschy/aurelia/cmd/aurelia-tui@latest`.
- [x] **`make tui`**: target no Makefile para build do binary TUI.
- [x] **Release pipeline**: CI matrix produz artifacts para as 4 arquiteturas.

**Critério de saída:** `make tui` compila o binary; `go install`
instala; `--session tui:work` abre a sessão correcta; CI gera
artifacts para as 4 plataformas.

**Spec:** `.specs/features/tui-polish-distribution/spec.md`
**Design:** `.specs/features/tui-polish-distribution/design.md`
**Tasks:** `.specs/features/tui-polish-distribution/tasks.md`

---

## Posicionamento no Roadmap

```
Sprint D: ~~Plan Mode~~ 🗑️ Removido 2026-05-24
Sprint E: User-Scoped Project Memory         ✅ implementado
Sprint F: Wiki Memory Gateway                📐 spec
Sprint G: Nudge                              🔴 draft
Sprint H: Agent Orchestration               🔴 draft
Sprint I: Auto-Skills                        🔴 draft
Sprint J: TUI ← AQUI                        🟢 em progresso
  ├─ Fase 0: Transport Abstraction (2d)      ✅ v0.23.6
  ├─ Fase 1: IPC Layer (3d)                  ✅ v0.24.0
  ├─ Fase 2: TUI MVP (5d)                    ✅ v0.25.x
  ├─ Fase 3: Multi-sessão (4d)               ✅ v0.27.1
  ├─ Fase 4.5: Image Input / Vision (3-4d)   ✅ v0.27.x → v0.28.0
  ├─ Fase 4: Painel de Estado do Projeto (3d) ✅ (este commit)
  ├─ Fase 4.6: Document Attachments (3-4d)   ✅ v0.29.0+ (feature branch)
  └─ Fase 5: Polish + Distribuição (5-7d)    ✅ v0.34.0
       ├─ 5.1: Input & Interaction Polish     ✅
       ├─ 5.2: Visual Polish & Theming          ✅
       └─ 5.3: Distribution & Build           ✅
```

**Nota:** A Fase 0 (Transport Abstraction) pode e deve ser feita **antes** do Sprint J — idealmente junto com o Sprint D0 ou E, porque não tem risco de regressão e a refactorização vai ser necessária de qualquer forma.

---

## Riscos e Mitigações

| Risco | Probabilidade | Mitigação |
|---|---|---|
| Bubble Tea ELM architecture difícil de manter em apps grandes | Médio | Dividir em sub-modelos por componente (sidebar model, chat model, project state model) com `tea.Cmd` para comunicação |
| IPC unix socket não disponível em Windows | Baixo | Aurelia corre em macOS/Linux; named pipe como fallback futuro |
| Sessões TUI com `chat_id` local conflituarem com IDs Telegram | Baixo | Namespace separado: IDs locais negativos [-9000009,-9000001], Telegram usa range positivo. Daemon força IDs no range (`forceTUIIDs`) |
| Streaming lento no terminal com mensagens longas | Baixo | Glamour renderiza por chunks; viewport só renderiza o visível (`content-visibility` equivalente) |

---

## Critérios de Sucesso do Sprint J

- [x] `aurelia-tui` corre como binary independente sem configuração extra
- [x] Conversa completa com streaming funciona pela TUI
- [x] Sidebar mostra sessões locais com navegação e gestão (criar/abrir/apagar)
- [x] Painel de estado mostra cwd, binding source, resumo de memória
- [x] Envio de imagens funciona (`/img`, `ctrl+v` clipboard, drag-drop) com modelo vision
- [x] Nenhuma regressão no Telegram transport
- [x] `go build ./... && go vet ./... && go test ./...` limpo
- [x] Funciona em macOS (darwin/arm64) e Linux (amd64/arm64)
