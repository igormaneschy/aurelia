# TUI Document Attachments — Design

## Contexto

Este documento descreve as decisões de design, estrutura de packages, tabelas de mapeamento e detalhes de implementação para o suporte a anexos de documentos na TUI.

## Estrutura de Packages

```
internal/
  ipc/
    types.go                 ← IPCAttachment struct, campo Attachments em IPCMessage
    server.go                ← validateMessage aceita attachments
  tui/
    attachment.go            ← NOVO — /attach, drag-drop, pendingAttachments
    attachment_test.go       ← NOVO
    model.go                 ← pendingAttachments field
    update.go                ← /attach handler, Enter com attachments
    view.go                  ← badges [📎 nome.pdf]
  projectbinding/            ← já existe, usado para resolver CWD
cmd/aurelia/
  tui_attachment_handler.go  ← NOVO — copia segura para <cwd>/uploads/
  tui_handler.go             ← handleTUISend: attachments → copia + nota no prompt
```

## Regra de Dependência

```
  tui  →  internal/ipc (IPCAttachment)
  cmd/aurelia  →  internal/ipc, internal/bridge, internal/projectbinding
  cmd/aurelia  →  internal/runtime (para resolver/validar CWD)
```

## Tipos Novos

### IPCAttachment (IPC protocol)

```go
// internal/ipc/types.go

// IPCAttachment represents a document file attached to a TUI message.
// The TUI sends a filesystem path; the daemon copies the file into
// <cwd>/uploads/ before forwarding the message to the pipeline.
type IPCAttachment struct {
    Path string `json:"path"`            // absolute filesystem path
    Name string `json:"name,omitempty"`  // display name (defaults to basename of Path)
}
```

Adicionado ao `IPCMessage`:

```go
type IPCMessage struct {
    Type        string         `json:"type"`
    ChatID      int64          `json:"chat_id,omitempty"`
    ThreadID    int64          `json:"thread_id,omitempty"`
    UserID      int64          `json:"user_id,omitempty"`
    Text        string         `json:"text,omitempty"`
    Images      []IPCImage     `json:"images,omitempty"`
    Attachments []IPCAttachment `json:"attachments,omitempty"`  // ← NOVO
    RequestID   string         `json:"request_id,omitempty"`
}
```

### TUI Model — pendingAttachments

```go
// internal/tui/model.go — novos campos

type Model struct {
    // ... campos existentes ...

    // pendingAttachments are documents attached to the next message.
    // Populated by /attach or drag-and-drop. Cleared on send or ctrl+x.
    pendingAttachments []pendingAttachment
}

// pendingAttachment holds a document waiting to be sent.
type pendingAttachment struct {
    path string // filesystem path
    name string // display name (filename only)
}
```

## Tabela de Mapeamento

### IPCAttachment → ficheiro no projeto

| Campo IPCAttachment | Campo no projeto | Transformação |
|---------------------|------------------|---------------|
| `Path`              | `<cwd>/uploads/<name>` | daemon copia com O_NOFOLLOW |
| `Name`              | nome do ficheiro destino | `filepath.Base(Name)` sanitizado |

### Prompt enrichment

```text
<user text original>

[Attached files copied to ./uploads/]
- spec.md
- diagram.pdf
```

## Fluxo: `/attach <path>`

```
1. Utilizador escreve "/attach ~/docs/spec.md" + Enter
2. update.go: handleKeyMsg detecta "/attach " prefix
3. attachment.go: attachDocumentFromPath(path)
    a. Resolve path (expande ~, valida absoluto)
    b. Verifica se CWD está definido (via estado local ou daemon)
    c. Verifica se ficheiro existe, é regular, não é symlink
    d. Verifica tamanho (≤ maxAttachmentBytes)
    e. Adiciona a m.pendingAttachments
    f. Retorna sem enviar mensagem
4. view.go: renderPendingAttachmentBadges()
    Mostra "[📎 spec.md]" acima do input
5. Utilizador escreve pergunta normal + Enter
6. model.go: submitMessageWithAttachments()
    Constrói IPCMessage{type:"send", text:..., attachments:[{path, name}]}
7. IPC envia ao daemon
8. cmd/aurelia/tui_attachment_handler.go: copyAttachmentsToCWD()
    a. Resolve CWD via projectbinding.Resolve
    b. Rejeita se CWD vazio
    c. Cria <cwd>/uploads/ com permissão 0750
    d. Para cada attachment: copia com O_NOFOLLOW para <cwd>/uploads/<name>
    e. Renomeia em caso de conflito (spec_1.md, spec_2.md, ...)
    f. Retorna lista de nomes finais
9. tui_handler.go: handleTUISend()
    Anexa nota ao prompt com lista de ficheiros em ./uploads/
    pipeSvc.Process(chatID, threadID, 0, enrichedText, images, userID, true)
10. Pipeline: agente recebe mensagem e pode ler ficheiros via tools
```

## Fluxo: Drag-and-drop

```
1. Utilizador arrasta ficheiro .pdf para o terminal
2. Terminal insere o path como texto (bracketed paste)
3. update.go: handleKeyMsg recebe tea.KeyMsg com Paste=true
4. attachment.go: tryParseAsDocumentPath(pastedText)
    a. Se texto é path absoluto de ficheiro existente
    b. E não é uma imagem suportada
    c. Então: adiciona a pendingAttachments, não insere no textarea
5. Se não é path de documento válido: insere no textarea normalmente
```

```go
func tryParseAsDocumentPath(text string) (string, string, bool) {
    text = strings.TrimSpace(text)
    if !filepath.IsAbs(text) {
        return "", "", false
    }
    if isImagePath(text) {
        return "", "", false // let image flow handle it
    }
    fi, err := os.Lstat(text)
    if err != nil || !fi.Mode().IsRegular() {
        return "", "", false
    }
    return filepath.Base(text), text, true
}
```

## View — Badges de documentos pendentes

```
┌─────────────────────────────────────────────────────────┐
│  [📎 spec.md] [📎 diagram.pdf]                          │  ← badges
│  > resume estes documentos_                             │  ← input
└─────────────────────────────────────────────────────────┘
```

```go
// internal/tui/view.go

func (m Model) renderPendingAttachmentBadges() string {
    if len(m.pendingAttachments) == 0 {
        return ""
    }
    var badges []string
    for _, att := range m.pendingAttachments {
        badges = append(badges, fmt.Sprintf("[📎 %s]", att.name))
    }
    return lipgloss.NewStyle().
        Foreground(lipgloss.Color("226")).
        Render(strings.Join(badges, " "))
}
```

## Validação no IPC

```go
// internal/ipc/server.go — validateMessage

const (
    MaxAttachmentCount     = 10
    MaxAttachmentBytes     = 25 * 1024 * 1024  // per file
    MaxTotalAttachmentBytes = 100 * 1024 * 1024 // total
)

func validateMessage(msg IPCMessage) error {
    // ... validação existente ...

    if len(msg.Attachments) > MaxAttachmentCount {
        return fmt.Errorf("too many attachments (%d, max %d)", len(msg.Attachments), MaxAttachmentCount)
    }
    for i, att := range msg.Attachments {
        if att.Path == "" {
            return fmt.Errorf("attachment[%d]: path required", i)
        }
        if len(att.Path) > 4096 {
            return fmt.Errorf("attachment[%d]: path too long", i)
        }
    }
    return nil
}
```

## Daemon — Cópia segura para uploads/

```go
// cmd/aurelia/tui_attachment_handler.go

type copiedAttachment struct {
    OriginalName string
    FinalName    string
    Size         int64
}

// copyAttachmentsToCWD copies each attachment into cwd/uploads/ with
// path-traversal and symlink defenses. It returns the final filenames.
func copyAttachmentsToCWD(ctx context.Context, cwd string, attachments []ipc.IPCAttachment) ([]copiedAttachment, error) {
    uploadsDir := filepath.Join(cwd, "uploads")
    if err := os.MkdirAll(uploadsDir, 0o750); err != nil {
        return nil, fmt.Errorf("create uploads dir: %w", err)
    }

    var result []copiedAttachment
    for _, att := range attachments {
        baseName := filepath.Base(att.Name)
        if baseName == "" || baseName == "." || baseName == ".." {
            baseName = filepath.Base(att.Path)
        }
        if baseName == "" || baseName == "." || baseName == ".." {
            return nil, fmt.Errorf("invalid attachment name")
        }

        destPath, err := uniqueUploadPath(uploadsDir, baseName)
        if err != nil {
            return nil, err
        }

        size, err := copyFileNoFollow(att.Path, destPath, MaxAttachmentBytes)
        if err != nil {
            return nil, fmt.Errorf("copy %s: %w", baseName, err)
        }

        result = append(result, copiedAttachment{
            OriginalName: baseName,
            FinalName:    filepath.Base(destPath),
            Size:         size,
        })
    }
    return result, nil
}
```

### uniqueUploadPath

```go
func uniqueUploadPath(dir, name string) (string, error) {
    dest := filepath.Join(dir, name)
    if _, err := os.Stat(dest); os.IsNotExist(err) {
        return dest, nil
    }
    ext := filepath.Ext(name)
    stem := strings.TrimSuffix(name, ext)
    for i := 1; i < 1000; i++ {
        candidate := filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, i, ext))
        if _, err := os.Stat(candidate); os.IsNotExist(err) {
            return candidate, nil
        }
    }
    return "", fmt.Errorf("could not find unique name for %s", name)
}
```

### copyFileNoFollow

```go
func copyFileNoFollow(src, dst string, maxBytes int64) (int64, error) {
    srcFile, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
    if err != nil {
        return 0, fmt.Errorf("open source: %w", err)
    }
    defer srcFile.Close()

    fi, err := srcFile.Stat()
    if err != nil {
        return 0, fmt.Errorf("stat source: %w", err)
    }
    if !fi.Mode().IsRegular() {
        return 0, fmt.Errorf("not a regular file")
    }
    if fi.Size() > maxBytes {
        return 0, fmt.Errorf("file too large (%d bytes, max %d)", fi.Size(), maxBytes)
    }

    dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o640)
    if err != nil {
        return 0, fmt.Errorf("create destination: %w", err)
    }
    defer dstFile.Close()

    n, err := io.Copy(dstFile, io.LimitReader(srcFile, maxBytes+1))
    if err != nil {
        return 0, fmt.Errorf("copy: %w", err)
    }
    if n > maxBytes {
        return 0, fmt.Errorf("file too large")
    }
    return n, nil
}
```

### buildAttachmentNote

```go
func buildAttachmentNote(copied []copiedAttachment) string {
    if len(copied) == 0 {
        return ""
    }
    var b strings.Builder
    b.WriteString("\n\n[Attached files copied to ./uploads/]\n")
    for _, c := range copied {
        fmt.Fprintf(&b, "- %s\n", c.FinalName)
    }
    return b.String()
}
```

## Edge Cases

| Caso | Comportamento |
|------|---------------|
| `/attach` sem path | Erro: "Usage: /attach <path>" |
| `/attach` sem `/cwd` definido | Erro: "Set a project with /cwd first" |
| `/attach` com path inexistente | Erro: "File not found: <path>" |
| `/attach` com symlink | Erro: "Symlinks are not allowed for attachments" |
| `/attach` com diretório | Erro: "Not a regular file: <path>" |
| `/attach` com ficheiro > 25MB | Erro: "Attachment too large (X MB). Limit is Y MB." |
| Conflito de nome em `uploads/` | Renomeia para `nome_1.ext`, `nome_2.ext`, etc. |
| Path traversal no nome | `filepath.Base` + validação de saída do CWD |
| Enter sem texto mas com anexos | Envia nota dos anexos ao agente |
| `ctrl+x` sem anexos pendentes | No-op |
| Drag-and-drop de imagem | Trata como imagem (fluxo existente) |
| Drag-and-drop de path inexistente | Insere texto no textarea normalmente |

## Lições Aplicadas

- **filepath-base-traversal**: nunca confiar apenas em `filepath.Base` para sanitização. Usar validação explícita de `.`/`..` e `O_NOFOLLOW`.
- **goroutine-recovery**: nenhuma goroutine nova é introduzida nesta feature; o IPC já usa goroutines com `defer recover()`.
- **redaction-before-truncation**: logs de erros de attachment usam basename apenas, nunca path completo, e antes de qualquer truncamento.
