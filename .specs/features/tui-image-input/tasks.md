# TUI Image Input — Tasks

## Fase 1: Fundação (P1)

### Task 1: Extrair `encodeImageAttachment` para `pkg/images/`
- [ ] Criar `pkg/images/encode.go` com `Encode()`, `SupportedMIMEType()`, `MIMEFromPath()`, `TooLargeError`
- [ ] Criar `pkg/images/encode_test.go` com testes: encode válido, ficheiro inexistente, too large, MIME types
- [ ] Refactor `internal/telegram/input.go`: `encodeImageAttachment` → `images.Encode` + adapter
- [ ] Verificar `go test ./internal/telegram/... -short` passa sem regressões
- **Arquivos:** `pkg/images/encode.go`, `pkg/images/encode_test.go`, `internal/telegram/input.go`
- **Risco:** Baixo — refactor mecânico, comportamento idêntico

### Task 2: Adicionar `IPCImage` ao protocolo IPC
- [ ] Adicionar `IPCImage` struct e campo `Images` ao `IPCMessage` em `internal/ipc/types.go`
- [ ] Adicionar validação em `validateMessage` (MIME type, tamanho total ≤ 15MB)
- [ ] Testes: mensagem com images válida, MIME inválido, too large, path+data vazios
- **Arquivos:** `internal/ipc/types.go`, `internal/ipc/types_test.go`, `internal/ipc/server.go`
- **Risco:** Baixo — campo opcional, não quebra mensagens existentes

### Task 3: Handler do daemon — converter IPCImage → ImageAttachment
- [ ] Criar `cmd/aurelia/tui_image_handler.go` com `convertIPCImages()`
- [ ] Modificar `handleTUISend` em `tui_handler.go`: passar `images` ao pipeline em vez de `nil`
- [ ] Testes: conversão com path, conversão com data, erro de encode, imagens vazias
- **Arquivos:** `cmd/aurelia/tui_image_handler.go`, `cmd/aurelia/tui_handler.go`, `cmd/aurelia/tui_handler_test.go`
- **Risco:** Baixo — daemon já tem acesso ao FS, pipeline já aceita images

## Fase 2: TUI — `/img <path>` (P1)

### Task 4: TUI Model — pendingImages e comando `/img`
- [ ] Adicionar `pendingImages []pendingImage` ao Model em `internal/tui/model.go`
- [ ] Criar `internal/tui/image.go` com `attachImageFromPath()`, `clearPendingImages()`
- [ ] Em `update.go`: detectar `/img ` prefix no Enter, chamar `attachImageFromPath`
- [ ] Em `update.go`: `ctrl+x` limpa pendingImages
- [ ] Modificar `submitMessage` para incluir `Images` no IPCMessage quando há pendingImages
- [ ] Limpar pendingImages após envio
- **Arquivos:** `internal/tui/model.go`, `internal/tui/image.go`, `internal/tui/update.go`
- **Risco:** Médio — nova interação no input, mas isolada

### Task 5: TUI View — badges de imagens pendentes
- [ ] Adicionar `renderPendingImageBadges()` em `internal/tui/view.go`
- [ ] Integrar no `renderInput()` — badges acima do textarea
- [ ] Testes: badges com 1 imagem, com múltiplas imagens, sem imagens
- **Arquivos:** `internal/tui/view.go`
- **Risco:** Baixo — render only

### Task 6: Testes integrados `/img`
- [ ] Teste: `/img /tmp/test.png` adiciona à lista, não envia mensagem
- [ ] Teste: `/img` sem path → erro
- [ ] Teste: `/img /nonexistent.png` → erro "file not found"
- [ ] Teste: `/img file.txt` → erro "unsupported type"
- [ ] Teste: Enter com pendingImages → IPCMessage contém images
- [ ] Teste: `ctrl+x` limpa pendingImages
- **Arquivos:** `internal/tui/image_test.go`
- **Risco:** Baixo

## Fase 3: TUI — `ctrl+v` clipboard (P2)

### Task 7: Clipboard helper macOS
- [ ] Criar `internal/tui/clipboard.go` com `pasteFromClipboardMacOS()`
- [ ] Script osascript: ler clipboard como PNGf, gravar em temp file
- [ ] Tratar: clipboard sem imagem, osascript não disponível
- [ ] Testes: mock exec.Command, verificar temp file criado, erro sem imagem
- **Arquivos:** `internal/tui/clipboard.go`, `internal/tui/clipboard_test.go`
- **Risco:** Médio — subprocess, OS-specific

### Task 8: Clipboard helper Linux
- [ ] Adicionar `pasteFromClipboardLinux()` com xclip e wl-paste fallback
- [ ] Tratar: nenhum tool disponível, clipboard vazio
- [ ] Testes: mock exec.Command para xclip/wl-paste
- **Arquivos:** `internal/tui/clipboard.go`, `internal/tui/clipboard_test.go`
- **Risco:** Médio — multi-tool, OS-specific

### Task 9: TUI — integrar ctrl+v
- [ ] Em `update.go`: detectar `ctrl+v`, chamar `pasteFromClipboard()` (dispatch por OS)
- [ ] Adicionar imagem do clipboard a pendingImages como `[📎 clipboard.png]`
- [ ] Limpar temp file após envio da mensagem
- [ ] Testes: ctrl+v adiciona imagem, ctrl+v sem imagem mostra mensagem
- **Arquivos:** `internal/tui/update.go`, `internal/tui/image.go`
- **Risco:** Baixo — integração com helpers já testados

## Fase 4: TUI — Drag-and-drop (P3)

### Task 10: Detectar path de imagem em paste de texto
- [ ] Em `update.go`: quando `KeyMsg.Paste == true`, tentar `tryParseAsImagePath()`
- [ ] Se for path de imagem válido: adicionar a pendingImages, não inserir no textarea
- [ ] Se não for path de imagem: inserir no textarea normalmente
- [ ] Testes: paste de path .png → badge, paste de path .txt → textarea, paste de texto → textarea
- **Arquivos:** `internal/tui/update.go`, `internal/tui/image.go`, `internal/tui/image_test.go`
- **Risco:** Baixo — detecção não-intrusiva

## Fase 5: Validação e polish

### Task 11: Validação completa
- [ ] `go build ./... && go vet ./... && go test ./... -short` limpo
- [ ] Teste ao vivo: `/img` com screenshot real, verificar resposta do modelo
- [ ] Teste ao vivo: `ctrl+v` com screenshot, verificar resposta
- [ ] Teste ao vivo: drag-drop de ficheiro, verificar badge
- [ ] Verificar que `applyVisionFallback` troca modelo quando necessário
- **Risco:** — 

### Task 12: Documentação
- [ ] Actualizar `/help` do TUI com `/img`, `ctrl+v`, `ctrl+x`
- [ ] Actualizar `docs/aurelia-tui-roadmap.md` com nova sub-fase
- [ ] Actualizar CHANGELOG (quando promovido a stable)
- **Arquivos:** `cmd/aurelia/tui_handler.go`, `docs/aurelia-tui-roadmap.md`
- **Risco:** Baixo

---

## Ordem de Implementação

```
Task 1 (extrair images.Encode)
  → Task 2 (IPCImage no protocolo)
  → Task 3 (handler do daemon)
  → Task 4 (TUI /img)
  → Task 5 (TUI badges)
  → Task 6 (testes /img)
  → Task 7 (clipboard macOS)
  → Task 8 (clipboard Linux)
  → Task 9 (integrar ctrl+v)
  → Task 10 (drag-drop)
  → Task 11 (validação)
  → Task 12 (docs)
```

Tasks 1-3 são fundação (backend). Tasks 4-6 são P1 (`/img`). Tasks 7-9 são P2 (clipboard). Task 10 é P3 (drag-drop). Tasks 11-12 são fechamento.

Cada task é um commit atómico na branch `feature/tui-image-input`.
