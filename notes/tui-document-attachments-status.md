# TUI Document Attachments — Nota de Status

**Status:** Concluída, validada (build + vet + test + deploy) na branch `feature/tui-document-attachments`.
**Versão:** `v0.29.0+` (feature branch — aguarda merge em `main` e bump de versão)
**Branch:** `feature/tui-document-attachments` → `stable/tui-document-attachments` → `main`
**Data de conclusão:** 2026-06-19

**Spec:** `.specs/features/tui-document-attachments/spec.md`
**Design:** `.specs/features/tui-document-attachments/design.md`
**Tasks:** `.specs/features/tui-document-attachments/tasks.md`

---

## Decisão chave de design

- **Path no IPC, não conteúdo** — ao contrário das imagens (base64), o IPC transporta apenas o caminho do ficheiro. O daemon é responsável por copiar o ficheiro para o projecto activo.
- **CWD obrigatório** — o comando `/attach` só funciona quando há um `/cwd` definido para a sessão activa. Sem CWD, o daemon devolve erro.
- **Agente processa** — a Aurelia copia o ficheiro e menciona a sua existência no prompt; o PI/Agente decide como ler/analisar o conteúdo.
- **Cópia segura** — `O_NOFOLLOW`, rejeição de symlinks, path traversal defense (`filepath.Clean` + `strings.HasPrefix`), renomeação em caso de conflito de nome, limite de tamanho (`MaxAttachmentBytes`).

---

## Métodos de input

| Método | UX | Prioridade |
|--------|-----|------------|
| `/attach <path>` | Escreves `/attach ~/docs/spec.pdf` + pergunta | P1 |
| Drag-and-drop | Arrastas ficheiro para o terminal; TUI detecta path | P2 |

---

## Tasks

- [x] **T0** — IPC protocol extension: `IPCAttachment`, campo `Attachments`, constantes de validação
- [x] **T1** — TUI attachment model: `pendingAttachment`, `attachDocumentFromPath`, `clearPendingAttachments`
- [x] **T2** — TUI input handling: `/attach <path>` no `update.go`, drag-and-drop, `ctrl+x` limpa
- [x] **T3** — TUI view badges: `renderPendingAttachmentBadges()` com badges `[📎 nome.pdf]`
- [x] **T4** — Daemon copy handler: `copyAttachmentsToCWD`, `uniqueUploadPath`, `copyFileNoFollow`, `buildAttachmentNote`
- [x] **T5** — Daemon send integration: attachments em `handleTUISend` com resolução de CWD
- [x] **T6** — Documentação: esta nota de status + roadmap atualizado
- [x] **T7** — Validation: `go build ./...`, `go vet ./...`, `go test ./... -short`, `make deploy`, live test steps documentados

---

## Critério de saída

> Consegues anexar um documento ao projeto activo pela TUI e o agente responde com base no conteúdo do ficheiro.

---

## Live Test (para execução manual)

### Setup

```bash
# Criar projecto de teste com um documento
mkdir -p /tmp/aurelia-doc-test/docs
echo "## Test\nLorem ipsum dolor sit amet." > /tmp/aurelia-doc-test/docs/spec.md
```

### Passos

| # | Acção | Comando/Input | Esperado (sucesso) | Falha possível |
|---|-------|--------------|-------------------|----------------|
| 1 | Abrir TUI | `aurelia-tui` | TUI abre com input vazio | Binary não está em PATH → usar caminho absoluto `~/.aurelia/bin/aurelia-tui` |
| 2 | Definir CWD | `/cwd /tmp/aurelia-doc-test` | Badge `📁 /tmp/aurelia-doc-test` aparece no header | CWD inválido → daemon rejeita com erro |
| 3 | Anexar documento | `/attach /tmp/aurelia-doc-test/docs/spec.md` | Badge `[📎 spec.md]` aparece acima do input | Path não existe → "file not found"; Sem CWD → "no active CWD" |
| 4 | Enviar mensagem | "resume este documento" + Enter | Mensagem é enviada com badge do anexo | Badge desaparece → attachment não foi incluído no IPC |
| 5 | Verificar cópia | `ls -la /tmp/aurelia-doc-test/uploads/` | `spec.md` está na pasta `uploads/` | Pasta não existe ou ficheiro ausente → daemon não copiou |
| 6 | Verificar resposta | — | Agente menciona "Lorem ipsum" ou conteúdo do ficheiro | Agente ignora → attachment note não foi incluída no prompt |
| 7 | Verificar segurança | Repetir com symlink: `ln -s /etc/passwd /tmp/aurelia-doc-test/link.txt && /attach /tmp/aurelia-doc-test/link.txt` | Daemon rejeita com "symlink rejected" | Cópia acontece → falha de segurança (`O_NOFOLLOW`) |

### Limpeza

```bash
rm -rf /tmp/aurelia-doc-test
```

> **Nota:** O `make deploy` (ou o post-commit hook) já deve ter reiniciado o daemon com o novo binary. Se o daemon não estiver activo, verificar com `launchctl print gui/$(id -u)/com.aurelia.agent | grep -E "state|pid"`.
