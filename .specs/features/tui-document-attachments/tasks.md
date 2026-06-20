# TUI Document Attachments — Tasks

**Sprint:** J (TUI)
**Branch:** `feature/tui-document-attachments`
**Estimativa total:** 3-4 dias

---

## T0 — IPC protocol extension

**Objetivo:** Adicionar `IPCAttachment` e campo `Attachments` ao `IPCMessage`.

- [ ] Adicionar `IPCAttachment` em `internal/ipc/types.go`
- [ ] Adicionar campo `Attachments []IPCAttachment` em `IPCMessage`
- [ ] Adicionar constantes `MaxAttachmentCount`, `MaxAttachmentBytes`, `MaxTotalAttachmentBytes`
- [ ] Atualizar `validateMessage` em `internal/ipc/server.go` para validar attachments
- [ ] Adicionar testes em `internal/ipc/types_test.go` e `internal/ipc/server_test.go`

**Critério de saída:** `go test ./internal/ipc/...` passa; mensagens com attachments são aceites.

---

## T1 — TUI attachment model

**Objetivo:** Adicionar estado e lógica de anexos ao TUI.

- [ ] Criar `internal/tui/attachment.go` com:
  - `pendingAttachment` struct
  - `attachDocumentFromPath(path)`
  - `clearPendingAttachments()`
  - `pendingAttachmentBadges()`
  - `toIPCAttachments()`
  - `tryParseAsDocumentPath(text)` para drag-and-drop
- [ ] Adicionar `pendingAttachments []pendingAttachment` ao `Model` em `internal/tui/model.go`
- [ ] Adicionar testes em `internal/tui/attachment_test.go`

**Critério de saída:** Testes de unidade para attach, validação e conversão IPC passam.

---

## T2 — TUI input handling

**Objetivo:** Integrar `/attach` e drag-and-drop no loop de input.

- [ ] Em `internal/tui/update.go`, adicionar handler para `/attach <path>` (similar a `/img`)
- [ ] Adicionar handler para `/attach` sem path
- [ ] Atualizar `delegateKeyToTextarea` para detectar drag-and-drop de documento (após verificar imagem)
- [ ] Limpar `pendingAttachments` no `ctrl+x` (juntamente com imagens)
- [ ] Atualizar `submitMessage` em `internal/tui/model.go` para popular `Attachments`
- [ ] Atualizar `buildTUIHelp` em `cmd/aurelia/tui_handler.go`

**Critério de saída:** TUI reage a `/attach`, mostra badges e envia attachments no IPC.

---

## T3 — TUI view badges

**Objetivo:** Mostrar badges de anexos pendentes acima do input.

- [ ] Em `internal/tui/view.go`, adicionar `renderPendingAttachmentBadges()`
- [ ] Integrar com a renderização do input (juntamente com badges de imagem)
- [ ] Garantir que badges de imagem e documento aparecem lado a lado quando ambos existem

**Critério de saída:** Visualmente os badges aparecem e são distintos dos de imagem.

---

## T4 — Daemon copy handler

**Objetivo:** Implementar cópia segura de anexos para `<cwd>/uploads/`.

- [ ] Criar `cmd/aurelia/tui_attachment_handler.go` com:
  - `copyAttachmentsToCWD(ctx, cwd, attachments)`
  - `uniqueUploadPath(dir, name)`
  - `copyFileNoFollow(src, dst, maxBytes)`
  - `buildAttachmentNote(copied)`
- [ ] Adicionar testes em `cmd/aurelia/tui_attachment_handler_test.go`
- [ ] Cobrir casos: sucesso, symlink rejeitado, path traversal, conflito de nome, ficheiro grande

**Critério de saída:** Testes de unidade passam; cópia segura validada.

---

## T5 — Daemon send integration

**Objetivo:** Integrar attachments em `handleTUISend`.

- [ ] Em `cmd/aurelia/tui_handler.go`, no `handleTUISend`:
  - Resolver CWD via `a.bindings.Resolve`
  - Se CWD vazio e houver attachments, retornar erro
  - Se houver attachments, chamar `copyAttachmentsToCWD`
  - Anexar nota ao prompt com `buildAttachmentNote`
  - Chamar `pipeSvc.Process` com o prompt enriquecido
- [ ] Adicionar testes em `cmd/aurelia/tui_handler_test.go`

**Critério de saída:** Testes de integração passam; anexos chegam ao pipeline.

---

## T6 — Documentation and roadmap update

**Objetivo:** Atualizar documentação do projeto.

- [ ] Atualizar `docs/aurelia-tui-roadmap.md` com nova Fase 4.6 — Document Attachments
- [ ] Atualizar `cmd/aurelia/tui_handler.go` `buildTUIHelp` com `/attach`
- [ ] Revisar `.specs/features/tui-document-attachments/spec.md` e `design.md`

**Critério de saída:** Documentação reflete o novo comportamento.

---

## T7 — Validation and live test

**Objetivo:** Validar a implementação end-to-end.

- [ ] `go build ./...` limpo
- [ ] `go vet ./...` limpo
- [ ] `go test ./... -short` passa sem regressões
- [ ] Teste ao vivo:
  1. Iniciar daemon
  2. Abrir TUI
  3. `/cwd /tmp/project`
  4. `/attach /tmp/project/docs/spec.md`
  5. Perguntar "resume este documento"
  6. Verificar que `spec.md` foi copiado para `/tmp/project/uploads/spec.md`
  7. Verificar que o agente respondeu com base no documento

**Critério de saída:** Feature funciona ao vivo; pronta para merge em `stable/`.

---

## Sequência de execução

```text
T0 → T1 → T2 → T3 → T4 → T5 → T6 → T7
```

T0 e T1 podem ser feitos em paralelo. T4 e T5 são dependentes de T0/T1.

---

## Notas operacionais

- Seguir a branch policy do projeto: `feature/tui-document-attachments` → `stable/tui-document-attachments` → `main`.
- Após cada commit, correr `make deploy` para rebuild + restart do daemon.
- A versão só é bumpada no merge para `main`, com aprovação do Igor.
