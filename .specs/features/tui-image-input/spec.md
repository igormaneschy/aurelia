# TUI Image Input — Especificação

**Status:** Draft — Junho 2026
**Sprint:** J (TUI) — sub-fase pós-Fase 3
**Depende de:** Fase 1 (IPC Layer), Fase 2 (TUI MVP)
**Desbloqueia:** Análise de imagens (screenshots, diagramas, fotos) directamente no terminal sem trocar para o Telegram

---

## Problem Statement

O Aurelia já suporta imagens vision no Telegram: o utilizador envia uma foto, o bot faz download, base64-encoda, e o pipeline envia ao PI SDK como `ImageContent` blocks. O modelo vision analisa a imagem e responde.

A TUI não tem esta capacidade. O `handleTUISend` em `cmd/aurelia/tui_handler.go:317` passa `nil` no parâmetro `images` do pipeline:

```go
pipeErr := pipeSvc.Process(chatID, threadID, 0, text, nil, userID, true)
//                                                       ^^^^
```

Toda a infraestrutura backend já existe e está testada:
- `bridge.ImageAttachment{Path, Data, MediaType}` — protocolo
- `bridge/index.ts:1403` — converte para `ImageContent` do PI SDK
- `pipeline.go:368` — `req.Options.Images = input.images`
- `pipeline.go:443` — `applyVisionFallback` troca para modelo vision se configurado
- `telegram/input.go:200` — `encodeImageAttachment()` lê ficheiro, base64-encoda, valida tamanho

O que falta é a camada TUI: capturar imagens no terminal e enviá-las via IPC até ao pipeline.

### O problema do terminal com imagens

Terminais **não conseguem receber bytes binários de imagens** via paste. Isto é uma limitação fundamental do protocolo de terminal, não do Bubble Tea. O `KeyMsg.Paste` do Bubble Tea detecta bracketed paste de texto, mas imagens no clipboard não chegam ao terminal como bytes — o terminal ignora-as ou insere o path do ficheiro se o utilizador arrastou um ficheiro.

A indústria (Hermes Agent, Claude Code, oh-my-pi) resolve isto com três padrões complementares:

1. **Clipboard via subprocess OS-level** — chamar ferramentas do SO directamente (`osascript` no macOS, `xclip`/`wl-paste` no Linux) para ler a imagem do clipboard e gravar num ficheiro temp
2. **Path de ficheiro no input** — o utilizador escreve `/img /caminho/para/imagem.png` ou arrasta um ficheiro para o terminal (alguns terminais inserem o path como texto)
3. **`file://` URI** — o utilizador cola `file:///Users/user/screenshot.png` e o TUI detecta o esquema

---

## Goals

- [ ] O utilizador pode enviar uma imagem via `/img <path>` no input da TUI
- [ ] O utilizador pode colar uma imagem do clipboard com `ctrl+v` (macOS prioritário, Linux secundário)
- [ ] O utilizador pode arrastar um ficheiro de imagem para o terminal e o TUI detecta o path
- [ ] Múltiplas imagens podem ser anexadas antes de enviar (badges `[📎 nome.png]` no input)
- [ ] O daemon recebe as imagens via IPC e passa-as ao pipeline
- [ ] `applyVisionFallback` troca automaticamente para modelo vision se o modelo activo não suportar imagens
- [ ] Limite de tamanho configurável (default 10MB, igual ao Telegram)
- [ ] Feedback visual: badges das imagens anexadas, erro claro se imagem inválida/maior que limite
- [ ] `go build ./... && go vet ./... && go test ./... -short` passam sem regressões

## Out of Scope

- Suporte a Windows (o daemon corre em macOS/Linux; Windows fica para futuro)
- Edição/manipulação de imagens (crop, resize) — o modelo recebe a imagem original
- Paste de imagem via protocolo de terminal (OSC 52, iTerm2 inline images) — não é suportado universalmente
- Cache de imagens — cada envio lê o ficheiro/clipboard de novo
- Histórico de imagens na sidebar — as imagens fazem parte do histórico de mensagens, não da sidebar

---

## User Stories

### P1: `/img <path>` — Anexar imagem por path de ficheiro

**User Story**: Como utilizador da TUI, quero escrever `/img ~/screenshots/erro.png` para anexar uma imagem do filesystem à próxima mensagem, para que o modelo vision a analise.

**Why P1**: Mais simples de implementar, sem dependência de clipboard. Funciona em qualquer OS. Base para os outros métodos.

**Acceptance Criteria**:

1. WHEN o utilizador escreve `/img <path>` no input THEN o TUI SHALL ler o ficheiro, validar que é uma imagem suportada (png, jpg, jpeg, gif, webp), e adicionar à lista de imagens pendentes
2. WHEN o ficheiro não existe THEN o TUI SHALL mostrar erro "File not found: <path>" sem adicionar à lista
3. WHEN o ficheiro excede o limite de tamanho THEN o TUI SHALL mostrar erro "Image too large (X MB). Limit is Y MB" sem adicionar à lista
4. WHEN o ficheiro tem extensão não suportada THEN o TUI SHALL mostrar erro "Unsupported image type: <ext>. Supported: png, jpg, jpeg, gif, webp"
5. WHEN há imagens pendentes THEN o TUI SHALL mostrar badges `[📎 nome.png]` acima do input
6. WHEN o utilizador pressiona Enter com imagens pendentes THEN o TUI SHALL enviar a mensagem + imagens ao daemon
7. WHEN o utilizador pressiona `ctrl+x` THEN o TUI SHALL limpar todas as imagens pendentes
8. WHEN as imagens são enviadas THEN o TUI SHALL limpar a lista de imagens pendentes

**Independent Test**: Iniciar TUI, escrever `/img /tmp/test.png`, verificar badge aparece, escrever pergunta, Enter, verificar IPC message contém `images` field.

---

### P1: IPC — Transporte de imagens pelo socket

**User Story**: Como engenheiro, quero que o protocolo IPC transporte imagens do TUI para o daemon, para que o pipeline as receba.

**Why P1**: Sem isto, as imagens capturadas na TUI não chegam ao pipeline.

**Acceptance Criteria**:

1. WHEN o TUI envia uma `IPCMessage` com `type:"send"` THEN ela SHALL poder incluir um campo `images []IPCImage`
2. WHEN `IPCImage` é recebida pelo daemon THEN o daemon SHALL validar que `data` (base64) ou `path` está presente, e que `media_type` é um MIME type suportado
3. WHEN o tamanho total das imagens excede 15MB THEN o daemon SHALL rejeitar com erro "images too large"
4. WHEN o daemon recebe imagens THEN `handleTUISend` SHALL passá-las ao pipeline como `[]bridge.ImageAttachment`
5. WHEN o daemon recebe imagens com `path` preenchido (sem `data`) THEN o daemon SHALL ler o ficheiro, base64-encodar, e popular `Data` antes de enviar ao pipeline
6. WHEN o daemon recebe imagens com `data` preenchida (base64) THEN o daemon SHALL usá-la directamente sem ler ficheiro
7. WHEN `validateMessage` recebe uma mensagem com imagens THEN SHALL validar MIME types e tamanho total

**Independent Test**: Enviar IPC message com `images:[{path:"/tmp/test.png", media_type:"image/png"}]` via `nc -U`, verificar pipeline recebe `[]bridge.ImageAttachment` não-vazio.

---

### P2: `ctrl+v` — Colar imagem do clipboard

**User Story**: Como utilizador da TUI no macOS, quero pressionar `ctrl+v` para colar a imagem que está no meu clipboard (ex: screenshot), para não ter de gravar num ficheiro primeiro.

**Why P2**: Melhora muito o fluxo de trabalho — screenshot (Cmd+Shift+Ctrl+4) → ctrl+v na TUI → pergunta. Mas depende de subprocess OS-level.

**Acceptance Criteria**:

1. WHEN o utilizador pressiona `ctrl+v` no macOS THEN o TUI SHALL executar `osascript` para ler o clipboard como PNG e gravar num ficheiro temp
2. WHEN o clipboard não tem imagem THEN o TUI SHALL mostrar "No image in clipboard" sem erro
3. WHEN `osascript` não está disponível THEN o TUI SHALL fazer fallback para paste de texto normal (não bloquear)
4. WHEN a imagem do clipboard é lida com sucesso THEN o TUI SHALL adicionar à lista de imagens pendentes como `[📎 clipboard.png]`
5. WHEN o utilizador pressiona `ctrl+v` no Linux THEN o TUI SHALL tentar `xclip -selection clipboard -t image/png -o` e depois `wl-paste -t image/png`
6. WHEN nenhum tool de clipboard está disponível no Linux THEN o TUI SHALL mostrar "Clipboard image paste not supported. Use /img <path> instead."

**Independent Test**: Copiar screenshot para clipboard, pressionar ctrl+v na TUI, verificar badge `[📎 clipboard.png]` aparece.

---

### P3: Drag-and-drop — Detectar path de ficheiro colado

**User Story**: Como utilizador da TUI, quero arrastar um ficheiro de imagem para o terminal e o TUI detectar automaticamente o path, para não ter de escrever `/img` manualmente.

**Why P3**: Conveniência — alguns terminais (iTerm2, Terminal.app) inserem o path do ficheiro arrastado como texto. O TUI pode detectar se o texto colado é um path de imagem válido.

**Acceptance Criteria**:

1. WHEN o utilizador arrasta um ficheiro .png/.jpg para o terminal THEN o terminal insere o path como texto e o TUI SHALL detectar que é um path de imagem válido
2. WHEN o texto colado (bracketed paste) é um path absoluto com extensão de imagem THEN o TUI SHALL adicionar à lista de imagens pendentes em vez de inserir o texto no textarea
3. WHEN o texto colado é um path mas não é uma imagem THEN o TUI SHALL inserir o texto no textarea normalmente (não interceptar)
4. WHEN o texto colado é multi-linha THEN o TUI SHALL inserir no textarea normalmente (não é um path único)

**Independent Test**: Arrastar ficheiro .png para o terminal, verificar badge aparece em vez de texto no input.

---

## Architecture

### Fluxo de dados

```
Utilizador na TUI
  │
  ├── /img ~/screenshots/erro.png     (P1)
  ├── ctrl+v (clipboard)              (P2)
  ├── drag-and-drop de ficheiro       (P3)
  │
  ├── TUI: lê ficheiro / clipboard
  │   internal/tui/image.go (novo)
  │   reutiliza encodeImageAttachment() extraído para pkg/images/
  │
  ├── TUI: mantém pendingImages []bridge.ImageAttachment no Model
  │   badges [📎 nome.png] no view
  │
  ├── Enter → IPCMessage{type:"send", text:..., images:[...]}
  │   images enviadas como paths (não base64) — daemon lê do FS
  │
  ├── Daemon: handleTUISend
  │   converte IPCImage[] → bridge.ImageAttachment[]
  │   lê ficheiros se path preenchido, base64-encoda
  │   pipeSvc.Process(chatID, threadID, 0, text, images, userID, true)
  │
  ├── Pipeline: req.Options.Images = images
  │   applyVisionFallback → troca modelo se necessário
  │
  └── Bridge: contentBlocks = [{type:"text"}, {type:"image", data, mimeType}]
      PI SDK envia ao modelo → resposta volta por streaming
```

### Decisão: path vs base64 no IPC

O IPC tem `maxLineSize = 64KB` por linha JSON. Uma imagem base64 de 10MB = ~13MB — excede o limite.

**Decisão**: O TUI envia **paths** de ficheiros temp no IPC, não base64. O daemon lê o ficheiro do filesystem (TUI e daemon correm na mesma máquina), base64-encoda, e envia ao bridge. Isto:

- Mantém o protocolo IPC leve (paths são strings curtas)
- Evita aumentar o buffer do scanner para 15MB+
- Reutiliza o padrão do Telegram (daemon lê ficheiro → base64 → bridge)
- O ficheiro temp é criado pelo TUI (clipboard) ou é o path original do utilizador (`/img`)

Para o caso de o TUI e daemon poderem correr em máquinas diferentes no futuro (não planeado), o base64 inline pode ser adicionado como fallback — mas não no MVP.

### Decisão: extrair `encodeImageAttachment` para pacote partilhado

A função `encodeImageAttachment` em `internal/telegram/input.go:200` lê um ficheiro, valida tamanho, base64-encoda, e devolve `bridge.ImageAttachment`. O TUI precisa da mesma lógica.

**Decisão**: Extrair para `pkg/images/encode.go` — pacote partilhado sem dependência de `internal/telegram`. Tanto o Telegram como o TUI importam.

### Decisão: clipboard por subprocess, não por protocolo de terminal

O protocolo de terminal não transporta bytes binários de imagens. O Bubble Tea `KeyMsg.Paste` detecta bracketed paste de **texto**, não bytes de imagem.

**Decisão**: Usar subprocess OS-level para ler o clipboard:
- **macOS**: `osascript -e 'get the clipboard as «class PNGf»'` → escreve para ficheiro temp
- **Linux**: `xclip -selection clipboard -t image/png -o > /tmp/aurelia-clip.png` ou `wl-paste -t image/png`
- **Fallback**: se nenhuma ferramenta está disponível, mostrar mensagem a sugerir `/img <path>`

### Decisão: MIME types suportados

Alinhado com o que o PI SDK e os modelos vision suportam:

| Extensão | MIME type |
|----------|-----------|
| `.png` | `image/png` |
| `.jpg`, `.jpeg` | `image/jpeg` |
| `.gif` | `image/gif` |
| `.webp` | `image/webp` |

---

## Non-Functional Requirements

### Segurança

- O daemon valida que os paths de imagens estão dentro do `$TMPDIR` ou do home do utilizador — não permite ler ficheiros arbitrários do sistema
- O `forceTUIIDs` continua a aplicar-se — imagens vão para a sessão TUI activa, não para chats Telegram
- As imagens temp do clipboard são criadas com `os.CreateTemp` (permissão 0600) e apagadas após envio

### Performance

- O base64 encoding acontece no daemon, não no TUI — o TUI envia apenas o path
- O limite de 10MB por imagem (igual ao Telegram) previne imagens gigantes que demoram a encodar
- Múltiplas imagens são enviadas numa só IPC message — não há round-trips por imagem

### Compatibilidade

- macOS (darwin/arm64) é prioritário — é onde o daemon corre
- Linux (xclip/wl-paste) é secundário
- Windows não suportado no MVP

---

## Open Questions

1. **`ctrl+v` vs `ctrl+shift+v`**: `ctrl+v` pode conflitar com paste de texto nalguns terminais. Alternativa: usar `ctrl+shift+v` para imagem e deixar `ctrl+v` para texto. Verificar comportamento em iTerm2 e Terminal.app.
2. **Vision fallback automático**: Se o modelo activo não suporta visão e não há `VisionFallback` configurado, mostrar erro ou enviar sem imagem? Recomendação: mostrar erro "Active model does not support images. Configure vision_fallback in app.json."
3. **Imagens no histórico**: As imagens enviadas devem aparecer no viewport como `[📎 image.png]` ou o modelo deve descrever o que recebeu? Recomendação: mostrar badge no histórico, não tentar renderizar a imagem no terminal.
