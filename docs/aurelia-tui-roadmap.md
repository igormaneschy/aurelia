# Aurelia TUI — Plano de Implementação

**Sprint:** J (pós Auto-Skills)
**Status:** 🔴 Proposta — aguarda aprovação para roadmap
**Depende de:** Sprint E (Project Memory), Sprint F (Wiki Memory Gateway)
**Desbloqueia:** Interface alternativa ao Telegram, onboarding sem bot, uso em contexto de terminal/IDE

---

## Problema a Resolver

O Telegram é hoje o único ponto de entrada conversacional da Aurelia. Isso cria três fricções reais:

1. **Contexto de terminal** — trabalhar num projecto no terminal e ter de trocar para o telemóvel (ou Telegram Desktop) para falar com a Aurelia quebra o fluxo de trabalho
2. **Sessões não retomáveis cross-surface** — iniciar uma sessão de trabalho no Telegram e querer retomá-la num terminal (ou vice-versa) não é possível hoje
3. **Dependência de conectividade** — o Telegram requer ligação à internet e autenticação externa; uma TUI local comunica directamente com o daemon

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
| **Framework TUI** | `charmbracelet/bubbletea` v2 | Go nativo, mesmo stack do Aurelia, arquitectura Elm (Model/Update/View), produção comprovada |
| **Styling & Layout** | `charmbracelet/lipgloss` | CSS-like para terminal — borders, cores, padding, flex layout |
| **Componentes** | `charmbracelet/bubbles` | Lista, viewport, textarea, spinner, progress bar — não reinventar |
| **Markdown rendering** | `charmbracelet/glamour` | Render de markdown no terminal com temas — respostas da Aurelia ficam formatadas |
| **IPC com daemon** | Unix socket ou gRPC local | Comunicação com o processo `aureliad` já em execução |
| **Config** | Ficheiro existente do Aurelia | Reutiliza `~/.aurelia/config.yaml` |

---

## Layout Visual da TUI

```
┌─────────────────────────────────────────────────────────┐
│ ● aurelia  [project: aurelia @ main]                    │  ← Header bar
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
- **Header bar** — projecto activo (`/cwd`), agent activo
- **Status bar** — keybindings contextuais

---

## Mapeamento Telegram → TUI

| Conceito Telegram | Conceito TUI | Implementação |
|---|---|---|
| Chat directo com o bot | Sessão DM | `ConversationKey{chat_id: localDM, thread_id: 0}` |
| Grupo com tópicos | Chat com sidebar de tópicos | `ConversationKey{chat_id: groupID, thread_id: topicID}` |
| Mensagem de texto | Input da TUI | `IncomingMessage` via `TUITransport` |
| Resposta do bot | Viewport renderizado | Stream de `OutgoingMessage` com glamour |
| `/cwd`, `/status`, etc. | Comandos idênticos no input | Parser de comandos reutilizado do pipeline |
| Projecto activo | Indicador no header | Lê cwd do daemon |
| Sessão persistente | Retoma por `ConversationKey` | Mesmas chaves, mesmo SQLite |

---

## Fases de Implementação

### Fase 0 — Transport Abstraction (1-2 dias)
*Pré-requisito para tudo. Sem esta fase, a TUI não é possível.*

**Tasks:**
- [ ] Definir `Transport` interface em `internal/transport/`
- [ ] Refactorizar `internal/telegram/` para implementar `TelegramTransport`
- [ ] Injectar transport no pipeline via DI (sem mudar comportamento)
- [ ] Testes: `MockTransport` para testes de pipeline existentes
- [ ] `go build ./... && go test ./...` limpo

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
- [ ] Auth local: verificação de `user_id` por UID do processo Unix (sem token extra) — pendente, usa permissão 0600 como barrier inicial

---

### Fase 2 — TUI MVP (1 semana)
*Interface mínima funcional: uma conversa, comandos, streaming.*

**Tasks:**
- [ ] `cmd/aurelia-tui/main.go` — binary separado `aurelia-tui`
- [ ] `TUITransport` implementando a interface de transport
- [ ] Model principal com 3 estados: `loading | chat | error`
- [ ] Viewport com glamour para rendering de markdown
- [ ] Textarea com `shift+enter` para multi-linha
- [ ] Streaming de respostas em tempo real (chunks aparecem à medida que chegam)
- [ ] Keybindings: `ctrl+c` sair, `ctrl+l` limpar, `tab` alternar sidebar
- [ ] Status bar com `cwd` activo e agent activo
- [ ] Graceful degradation: se daemon não estiver a correr, mensagem clara

**Critério de saída:** consegues ter uma conversa completa com a Aurelia pela TUI, incluindo `/cwd` e respostas em markdown

---

### Fase 3 — Multi-sessão e Sidebar (3-4 dias)
*Reflectir a estrutura de chats do Telegram.*

**Conceito de sessões locais:**

A TUI cria `ConversationKey` locais que coexistem com as do Telegram. Um `chat_id` local (e.g. `-9000001`) é reservado para sessions TUI. Um utilizador pode ter:
- `tui:dm` — conversa directa local (equivale ao DM do Telegram)
- `tui:work` — sessão de trabalho com nome personalizado
- `telegram:grupo-x/topico-dev` — sessão importada do Telegram (read + write)

**Tasks:**
- [ ] Sidebar com lista de sessões (locais + Telegram se autenticado)
- [ ] Navegação: `↑↓` na sidebar, `enter` para abrir sessão
- [ ] Criar nova sessão local: `n` na sidebar
- [ ] Persistência de sessões TUI no SQLite do daemon
- [ ] Header com nome da sessão, `cwd`, projecto activo
- [ ] Histórico de mensagens por sessão (carregado do daemon ao abrir)

---

### Fase 4 — Painel de Estado do Projeto (3 dias)
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
- [ ] Overlay de estado (`ctrl+p` para abrir/fechar)
- [ ] Informação do projecto activo (cwd, binding source)
- [ ] Resumo de memória (camadas activas, contagem de facts)
- [ ] Último checkpoint da conversa
- [ ] Sincronização em tempo real com o daemon (poll ou subscribe)

---

### Fase 5 — Polish e Distribuição (2-3 dias)

**Tasks:**
- [ ] Tema claro/escuro (detecta `$TERM_PROGRAM` e `$COLORTERM`)
- [ ] Mouse support (`tea.WithMouseCellMotion()`)
- [ ] Resize handling (terminal window resize)
- [ ] `--session` flag para abrir directamente numa sessão: `aurelia-tui --session tui:work`
- [ ] `--attach` flag para retomar sessão Telegram: `aurelia-tui --attach telegram:chat_id/thread_id`
- [ ] Build targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`
- [ ] Instalação via `go install`: `go install github.com/igormaneschy/aurelia/cmd/aurelia-tui@latest`
- [ ] Makefile target: `make tui`

---

## Posicionamento no Roadmap

```
Sprint D: ~~Plan Mode~~ 🗑️ Removido 2026-05-24
Sprint E: User-Scoped Project Memory         📐 spec completa
Sprint F: Wiki Memory Gateway                📐 spec
Sprint G: Nudge                              🔴 draft
Sprint H: Agent Orchestration               🔴 draft
Sprint I: Auto-Skills                        🔴 draft
Sprint J: TUI ← AQUI                        🔴 proposta
  ├─ Fase 0: Transport Abstraction (2d)
  ├─ Fase 1: IPC Layer (3d)
  ├─ Fase 2: TUI MVP (5d)
  ├─ Fase 3: Multi-sessão (4d)
  ├─ Fase 4: Painel de Estado do Projeto (3d)
  └─ Fase 5: Polish + Distribuição (3d)
     Total estimado: ~2.5 semanas
```

**Nota:** A Fase 0 (Transport Abstraction) pode e deve ser feita **antes** do Sprint J — idealmente junto com o Sprint D0 ou E, porque não tem risco de regressão e a refactorização vai ser necessária de qualquer forma.

---

## Riscos e Mitigações

| Risco | Probabilidade | Mitigação |
|---|---|---|
| Bubble Tea ELM architecture difícil de manter em apps grandes | Médio | Dividir em sub-modelos por componente (sidebar model, chat model, project state model) com `tea.Cmd` para comunicação |
| IPC unix socket não disponível em Windows | Baixo | Aurelia corre em macOS/Linux; named pipe como fallback futuro |
| Divergência de estado entre TUI e Telegram | Médio | Toda a escrita passa pelo daemon — TUI nunca escreve directamente no SQLite |
| Sessões TUI com `chat_id` local conflituarem com IDs Telegram | Baixo | Namespace separado: IDs locais negativos < -9000000, Telegram usa range diferente |
| Streaming lento no terminal com mensagens longas | Baixo | Glamour renderiza por chunks; viewport só renderiza o visível (`content-visibility` equivalente) |

---

## Critérios de Sucesso do Sprint J

- [ ] `aurelia-tui` corre como binary independente sem configuração extra
- [ ] Conversa completa com streaming funciona pela TUI
- [ ] Sidebar mostra sessões locais e sessões Telegram (se autenticado)
- [ ] Retomar sessão iniciada no Telegram funciona com `--attach`
- [ ] Painel de estado mostra cwd, binding source, resumo de memória
- [ ] Nenhuma regressão no Telegram transport
- [ ] `go build ./... && go vet ./... && go test ./...` limpo
- [ ] Funciona em macOS (darwin/arm64) e Linux (amd64/arm64)
