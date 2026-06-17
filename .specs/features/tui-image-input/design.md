# TUI Image Input — Design

## Contexto

Este documento descreve as decisões de design, estrutura de packages,
tabelas de mapeamento, e detalhes de implementação para o suporte a
imagens na TUI.

## Estrutura de Packages

```
pkg/
  images/                    ← NOVO — encode partilhado, sem deps internas
    encode.go                ← ReadFile + base64 + size validation
    encode_test.go

internal/
  ipc/
    types.go                 ← IPCImage struct, validação
    server.go                ← validateMessage aceita images
  tui/
    image.go                 ← NOVO — clipboard, /img, drag-drop, pendingImages
    image_test.go
    model.go                 ← pendingImages field, /img handler
    update.go                ← ctrl+v, ctrl+x, Enter com imagens
    view.go                  ← badges [📎 nome.png]
  bridge/
    protocol.go              ← sem mudanças (ImageAttachment já existe)
  telegram/
    input.go                 ← encodeImageAttachment → pkg/images/Encode

cmd/aurelia/
  tui_handler.go             ← handleTUISend: images → pipeline
  tui_image_handler.go       ← NOVO — converte IPCImage[] → ImageAttachment[]
```

## Regra de Dependência

```
  tui  →  pkg/images  (encode)
  tui  →  internal/ipc (IPCImage)
  telegram  →  pkg/images  (encode, refactor)
  cmd/aurelia  →  pkg/images  (encode no daemon)
  cmd/aurelia  →  internal/bridge  (ImageAttachment)

  pkg/images NÃO importa nada interno — pacote puro
```

## Tipos Novos

### IPCImage (IPC protocol)

```go
// internal/ipc/types.go

// IPCImage represents an image attached to a TUI message.
// Either Path or Data should be populated:
//   - Path: filesystem path — daemon reads and base64-encodes
//   - Data: pre-base64-encoded image data (future use, not MVP)
type IPCImage struct {
    Path      string `json:"path,omitempty"`
    Data      string `json:"data,omitempty"`
    MediaType string `json:"media_type,omitempty"`
}
```

Adicionado ao `IPCMessage`:

```go
type IPCMessage struct {
    Type      string      `json:"type"`
    ChatID    int64       `json:"chat_id,omitempty"`
    ThreadID  int64       `json:"thread_id,omitempty"`
    UserID    int64       `json:"user_id,omitempty"`
    Text      string      `json:"text,omitempty"`
    RequestID string      `json:"request_id,omitempty"`
    Images    []IPCImage  `json:"images,omitempty"`  ← NOVO
}
```

### pkg/images.Encode (partilhado)

```go
// pkg/images/encode.go

// ImageAttachment is a base64-encoded image ready for the bridge protocol.
// Mirrors bridge.ImageAttachment but without the bridge dependency.
type ImageAttachment struct {
    Path      string
    Data      string // base64-encoded
    MediaType string
}

// Encode reads an image file, validates its size, base64-encodes it,
// and returns an ImageAttachment. Returns an error if the file is too
// large or cannot be read.
func Encode(filePath, defaultMIME string, maxBytes int) (ImageAttachment, error)

// SupportedMIMEType returns true if the MIME type is a supported image format.
func SupportedMIMEType(mimeType string) bool

// MIMEFromPath guesses the MIME type from a file extension.
func MIMEFromPath(filePath string) string

// MaxImageBytes is the default size limit (10 MB), matching Telegram.
const MaxImageBytes = 10 * 1024 * 1024
```

### TUI Model — pendingImages

```go
// internal/tui/model.go — novos campos

type Model struct {
    // ... campos existentes ...

    // pendingImages are images attached to the next message.
    // Populated by /img, ctrl+v, or drag-drop. Cleared on send or ctrl+x.
    pendingImages []pendingImage
}

// pendingImage holds an image waiting to be sent.
type pendingImage struct {
    DisplayName string // shown in badge (e.g. "screenshot.png")
    Path        string // filesystem path
    MediaType   string // MIME type
}
```

## Tabela de Mapeamento

### IPC → Bridge (no daemon)

| IPCImage field | bridge.ImageAttachment field | Transformação |
|----------------|------------------------------|---------------|
| `Path` | `Path` | directo |
| `Data` | `Data` | directo (se preenchido) |
| `Path` (sem Data) | `Data` | daemon lê ficheiro, base64-encoda via `images.Encode` |
| `MediaType` | `MediaType` | directo |

### bridge.ImageAttachment → PI SDK (no bridge TS, já existe)

| bridge.ImageAttachment | PI SDK ImageContent | Transformação |
|------------------------|---------------------|---------------|
| `Data` (base64) | `data` | directo |
| `MediaType` | `mimeType` | directo |
| `Path` | — | não enviado ao PI (apenas metadata) |

## Fluxo: `/img <path>`

```
1. Utilizador escreve "/img ~/screenshots/erro.png" + Enter
2. update.go: handleKeyMsg detecta "/img " prefix
3. image.go: attachImageFromPath(path)
   a. Resolve path (expande ~, valida absoluto)
   b. Verifica extensão suportada (png/jpg/jpeg/gif/webp)
   c. Verifica tamanho (≤ maxImageBytes)
   d. Adiciona a m.pendingImages
   e. Retorna sem enviar mensagem
4. view.go: renderPendingImageBadges()
   Mostra "[📎 erro.png]" acima do input
5. Utilizador escreve pergunta normal + Enter
6. model.go: submitMessageWithImages()
   Constrói IPCMessage{type:"send", text:..., images:[{path, media_type}]}
7. IPC envia ao daemon
8. tui_image_handler.go: convertIPCImages()
   Para cada IPCImage com Path: images.Encode(path, mime, maxBytes)
   Retorna []bridge.ImageAttachment
9. tui_handler.go: handleTUISend()
   pipeSvc.Process(chatID, threadID, 0, text, images, userID, true)
10. pipeline.go: req.Options.Images = images
    applyVisionFallback(&req, images) — troca modelo se necessário
11. bridge/index.ts: contentBlocks = [{text}, {image, data, mimeType}]
    PI SDK envia ao modelo
```

## Fluxo: `ctrl+v` (clipboard)

```
1. Utilizador pressiona ctrl+v
2. update.go: handleKeyMsg detecta "ctrl+v"
3. image.go: pasteImageFromClipboard()
   a. Detecta OS (runtime.GOOS)
   b. macOS: exec.Command("osascript", "-e", clipboardScript)
      Script: grava clipboard PNG para $TMPDIR/aurelia-clip-<timestamp>.png
   c. Linux: tenta "xclip -selection clipboard -t image/png -o > tmpfile"
      Depois "wl-paste -t image/png > tmpfile"
   d. Se nenhum tool disponível: retorna erro "clipboard not supported"
   e. Se clipboard vazio (sem imagem): retorna erro "no image in clipboard"
   f. Adiciona tmpfile a m.pendingImages como [📎 clipboard.png]
4. Resto igual ao fluxo /img (badges, Enter, envio)
5. Após envio: os.Remove(tmpfile) — limpa ficheiro temp
```

### Script osascript (macOS)

```applescript
set tmpPath to "/tmp/aurelia-clip-<timestamp>.png"
set theImage to the clipboard as «class PNGf»
set theFile to open for access POSIX file tmpPath with write permission
write theImage to theFile
close access theFile
return tmpPath
```

Implementação Go:

```go
func pasteFromClipboardMacOS() (string, error) {
    tmpFile, err := os.CreateTemp("", "aurelia-clip-*.png")
    if err != nil {
        return "", fmt.Errorf("create temp file: %w", err)
    }
    tmpPath := tmpFile.Name()
    _ = tmpFile.Close()

    script := fmt.Sprintf(`
        set theImage to the clipboard as «class PNGf»
        set theFile to open for access POSIX file "%s" with write permission
        write theImage to theFile
        close access theFile
    `, tmpPath)

    cmd := exec.Command("osascript", "-e", script)
    var stderr bytes.Buffer
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        os.Remove(tmpPath)
        if strings.Contains(stderr.String(), "can't make") {
            return "", errNoClipboardImage
        }
        return "", fmt.Errorf("osascript: %w (stderr: %s)", err, stderr.String())
    }
    return tmpPath, nil
}
```

### Linux: xclip / wl-paste

```go
func pasteFromClipboardLinux() (string, error) {
    tmpFile, err := os.CreateTemp("", "aurelia-clip-*.png")
    if err != nil {
        return "", fmt.Errorf("create temp file: %w", err)
    }
    tmpPath := tmpFile.Name()
    _ = tmpFile.Close()

    // Try xclip first, then wl-paste (Wayland).
    for _, cmd := range [][]string{
        {"xclip", "-selection", "clipboard", "-t", "image/png", "-o"},
        {"wl-paste", "-t", "image/png"},
    } {
        if _, err := exec.LookPath(cmd[0]); err != nil {
            continue
        }
        c := exec.Command(cmd[0], cmd[1:]...)
        out, err := os.Create(tmpPath)
        if err != nil {
            os.Remove(tmpPath)
            return "", err
        }
        c.Stdout = out
        err = c.Run()
        out.Close()
        if err != nil {
            os.Remove(tmpPath)
            continue
        }
        // Verify the file is non-empty (clipboard had no image).
        info, _ := os.Stat(tmpPath)
        if info == nil || info.Size() == 0 {
            os.Remove(tmpPath)
            return "", errNoClipboardImage
        }
        return tmpPath, nil
    }
    os.Remove(tmpPath)
    return "", errClipboardToolNotFound
}
```

## Fluxo: Drag-and-drop

```
1. Utilizador arrasta ficheiro .png para o terminal
2. Terminal insere o path como texto (bracketed paste)
3. update.go: handleKeyMsg recebe tea.KeyMsg com Paste=true
4. image.go: tryParseAsImagePath(pastedText)
   a. Se texto é path absoluto com extensão de imagem suportada
   b. E ficheiro existe
   c. Então: adiciona a pendingImages, não insere no textarea
5. Se não é path de imagem: insere no textarea normalmente
```

```go
func tryParseAsImagePath(text string) (string, string, bool) {
    text = strings.TrimSpace(text)
    if !filepath.IsAbs(text) {
        return "", "", false
    }
    ext := strings.ToLower(filepath.Ext(text))
    mime, ok := extensionMIME[ext]
    if !ok {
        return "", "", false
    }
    if _, err := os.Stat(text); err != nil {
        return "", "", false
    }
    return filepath.Base(text), mime, true
}
```

## View — Badges de imagens pendentes

```
┌─────────────────────────────────────────────────────────┐
│  [📎 erro.png] [📎 diagram.png]                          │  ← badges
│  > o que está errado com estas telas?_                  │  ← input
└─────────────────────────────────────────────────────────┘
```

```go
// internal/tui/view.go

func (m Model) renderPendingImageBadges() string {
    if len(m.pendingImages) == 0 {
        return ""
    }
    var badges []string
    for _, img := range m.pendingImages {
        badges = append(badges, fmt.Sprintf("[📎 %s]", img.DisplayName))
    }
    return lipgloss.NewStyle().
        Foreground(lipgloss.Color("226")).
        Render(strings.Join(badges, " "))
}
```

Integrado no `renderInput()` — badges aparecem acima do textarea quando
há imagens pendentes.

## Validação no IPC

```go
// internal/ipc/server.go — validateMessage

// maxTotalImageBytes limits the total size of all images in one message.
const maxTotalImageBytes = 15 * 1024 * 1024 // 15 MB

func validateMessage(msg IPCMessage) error {
    // ... validação existente ...

    if len(msg.Images) > 0 {
        totalSize := 0
        for _, img := range msg.Images {
            if img.Path == "" && img.Data == "" {
                return fmt.Errorf("image missing path and data")
            }
            if img.MediaType == "" {
                return fmt.Errorf("image missing media_type")
            }
            if !isSupportedImageMIME(img.MediaType) {
                return fmt.Errorf("unsupported image media_type: %s", img.MediaType)
            }
            totalSize += len(img.Data) // base64 size
            if img.Path != "" {
                totalSize += len(img.Path)
            }
        }
        if totalSize > maxTotalImageBytes {
            return fmt.Errorf("images too large (%d bytes, max %d)", totalSize, maxTotalImageBytes)
        }
    }
    return nil
}
```

## Daemon — Conversão IPCImage → ImageAttachment

```go
// cmd/aurelia/tui_image_handler.go

func convertIPCImages(ctx context.Context, ipcImages []ipc.IPCImage) ([]bridge.ImageAttachment, error) {
    if len(ipcImages) == 0 {
        return nil, nil
    }
    attachments := make([]bridge.ImageAttachment, 0, len(ipcImages))
    for _, img := range ipcImages {
        if img.Data != "" {
            // Pre-encoded (future use) — use directly.
            attachments = append(attachments, bridge.ImageAttachment{
                Data:      img.Data,
                MediaType: img.MediaType,
                Path:      img.Path,
            })
            continue
        }
        // Read from path and encode.
        att, err := images.Encode(img.Path, img.MediaType, images.MaxImageBytes)
        if err != nil {
            return nil, fmt.Errorf("encode image %s: %w", img.Path, err)
        }
        attachments = append(attachments, bridge.ImageAttachment{
            Path:      att.Path,
            Data:      att.Data,
            MediaType: att.MediaType,
        })
    }
    return attachments, nil
}
```

## Refactor: extrair encodeImageAttachment

### Antes (internal/telegram/input.go)

```go
func encodeImageAttachment(filePath, defaultMIME string, maxImageBytes int) (bridge.ImageAttachment, error) {
    data, err := os.ReadFile(filePath)
    // ... validação + base64 ...
}
```

### Depois (pkg/images/encode.go)

```go
func Encode(filePath, defaultMIME string, maxBytes int) (ImageAttachment, error) {
    data, err := os.ReadFile(filePath)
    if err != nil {
        return ImageAttachment{}, fmt.Errorf("read image %q: %w", filePath, err)
    }
    if maxBytes <= 0 {
        maxBytes = MaxImageBytes
    }
    if len(data) > maxBytes {
        return ImageAttachment{}, TooLargeError{Path: filePath, Size: len(data), Limit: maxBytes}
    }
    encoded := base64.StdEncoding.EncodeToString(data)
    return ImageAttachment{
        Path:      filePath,
        Data:      encoded,
        MediaType: defaultMIME,
    }, nil
}
```

### Telegram usa o pacote partilhado

```go
// internal/telegram/input.go — refactor
func (bc *BotController) encodeImage(filePath, defaultMIME string) (bridge.ImageAttachment, error) {
    att, err := images.Encode(filePath, defaultMIME, bc.maxImageBytes())
    if err != nil {
        var tooLarge images.TooLargeError
        if errors.As(err, &tooLarge) {
            return bridge.ImageAttachment{}, imageTooLargeError{
                path: tooLarge.Path, size: tooLarge.Size, limit: tooLarge.Limit,
            }
        }
        return bridge.ImageAttachment{}, err
    }
    return bridge.ImageAttachment{
        Path:      att.Path,
        Data:      att.Data,
        MediaType: att.MediaType,
    }, nil
}
```

## Edge Cases

| Caso | Comportamento |
|------|---------------|
| `/img` sem path | Erro: "Usage: /img <path>" |
| `/img` com path inexistente | Erro: "File not found: <path>" |
| `/img` com extensão não-imagem | Erro: "Unsupported image type: .txt. Supported: png, jpg, jpeg, gif, webp" |
| `/img` com ficheiro > 10MB | Erro: "Image too large (15.0 MB). Limit is 10.0 MB." |
| `ctrl+v` sem imagem no clipboard | Mensagem: "No image in clipboard" |
| `ctrl+v` sem osascript/xclip | Mensagem: "Clipboard image paste not supported. Use /img <path>." |
| Enter sem texto mas com imagens | Envia apenas as imagens com texto vazio (modelo recebe só imagens) |
| Enter sem imagens nem texto | No-op (comportamento existente) |
| `ctrl+x` sem imagens pendentes | No-op |
| Modelo activo não suporta visão | `applyVisionFallback` troca para vision_fallback se configurado; senão erro do PI |
| Sessão TUI sem /cwd | Imagens funcionam independentemente de /cwd |
| Drag-drop de ficheiro não-imagem | Texto inserido no textarea normalmente |
| Drag-drop de múltiplos paths | Apenas o primeiro é processado como imagem; resto inserido como texto |
