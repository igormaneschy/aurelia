# TUI Document Attachments — Especificação

**Status:** ✅ Validated — entregue em v0.35.0 (Fase 4.6, Sprint J)
**Sprint:** J (TUI) — sub-fase pós-Fase 4.5 (Image Input)
**Depende de:** Fase 1 (IPC Layer), Fase 2 (TUI MVP), Fase 4 (Project State Panel)
**Desbloqueia:** Enviar documentos (md, docx, ppt, pdf, etc.) do terminal para o projeto ativo sem sair da TUI

---

## Problem Statement

A TUI já suporta imagens via `/img`, `ctrl+v` e drag-and-drop. No entanto, documentos de texto (`.md`, `.docx`, `.ppt`, `.pdf`, etc.) ainda não têm um fluxo próprio. O utilizador precisa de sair da TUI para copiar ficheiros para o projeto ou mencionar paths absolutos na mensagem.

A abordagem acordada é diferente da de imagens:

- **CWD obrigatório** — só faz sentido anexar documentos quando há um projeto ativo.
- **Cópia para `<cwd>/uploads/`** — o ficheiro é materializado no projeto, não enviado como base64.
- **Qualquer formato permitido** — md, docx, ppt, pdf, txt, csv, etc.
- **Agente responsável por processar** — o Aurelia apenas copia o ficheiro e menciona a sua existência; o PI/Agente decide como ler/analisar.

---

## Goals

- [ ] O utilizador pode anexar um documento via `/attach <path>` no input da TUI
- [ ] O utilizador pode arrastar um ficheiro de documento para o terminal e o TUI detecta o path
- [ ] Múltiplos documentos podem ser anexados antes de enviar (badges `[📎 nome.pdf]` no input)
- [ ] O daemon rejeita `/attach` se não houver `/cwd` definido para a sessão ativa
- [ ] O daemon copia cada anexo para `<cwd>/uploads/<filename>` com segurança (path traversal, symlink, O_NOFOLLOW)
- [ ] O texto enviado ao agente inclui uma nota com a lista de ficheiros copiados para `./uploads/`
- [ ] O agente pode usar as suas ferramentas (Read, Glob, etc.) para processar os documentos
- [ ] Limite de tamanho configurável (default 25 MB por ficheiro, 100 MB total)
- [ ] Feedback visual: badges dos anexos pendentes, erro claro se inválido/maior que limite
- [ ] `go build ./... && go vet ./... && go test ./... -short` passam sem regressões

## Out of Scope

- Suporte a Windows (o daemon corre em macOS/Linux; Windows fica para futuro)
- Conversão/extração automática de conteúdo (PDF → texto, DOCX → markdown) — agente decide
- Edição/manipulação de documentos
- Cache de anexos ou histórico de anexos na sidebar
- Anexos via clipboard (diferente de imagens, o clipboard de documentos é não confiável cross-OS)
- Sincronização de anexos entre Telegram e TUI (superfícies independentes por design)

---

## User Stories

### P1: `/attach <path>` — Anexar documento por path

**User Story**: Como utilizador da TUI, quero escrever `/attach ~/docs/especificacao.pdf` para anexar um documento ao projeto ativo, para que o agente o possa analisar.

**Why P1**: Mais simples de implementar, sem dependência de drag-and-drop. Funciona em qualquer OS.

**Acceptance Criteria**:

1. WHEN o utilizador escreve `/attach <path>` no input THEN o TUI SHALL validar que o ficheiro existe, é regular, não é symlink e está dentro do limite de tamanho
2. WHEN não há `/cwd` definido na sessão THEN o TUI SHALL mostrar erro "Set a project with /cwd first" sem adicionar à lista
3. WHEN o ficheiro não existe THEN o TUI SHALL mostrar erro "File not found: <path>" sem adicionar à lista
4. WHEN o ficheiro é um symlink THEN o TUI SHALL mostrar erro "Symlinks are not allowed for attachments"
5. WHEN o ficheiro excede o limite de tamanho THEN o TUI SHALL mostrar erro "Attachment too large (X MB). Limit is Y MB" sem adicionar à lista
6. WHEN há documentos pendentes THEN o TUI SHALL mostrar badges `[📎 nome.pdf]` acima do input
7. WHEN o utilizador pressiona Enter com documentos pendentes THEN o TUI SHALL enviar a mensagem + attachments ao daemon
8. WHEN o utilizador pressiona `ctrl+x` THEN o TUI SHALL limpar todas as imagens e documentos pendentes
9. WHEN os documentos são enviados THEN o TUI SHALL limpar a lista de documentos pendentes

**Independent Test**: Iniciar TUI, definir `/cwd /tmp/project`, escrever `/attach /tmp/project/docs/spec.md`, verificar badge aparece, escrever pergunta, Enter, verificar IPC message contém `attachments` field.

---

### P1: IPC — Transporte de anexos pelo socket

**User Story**: Como engenheiro, quero que o protocolo IPC transporte paths de anexos do TUI para o daemon, para que o daemon os copie para o projeto.

**Why P1**: Sem isto, os anexos selecionados na TUI não chegam ao daemon.

**Acceptance Criteria**:

1. WHEN o TUI envia uma `IPCMessage` com `type:"send"` THEN ela SHALL poder incluir um campo `attachments []IPCAttachment`
2. WHEN `IPCAttachment` é recebida pelo daemon THEN o daemon SHALL validar que `path` está presente
3. WHEN o daemon recebe anexos THEN `handleTUISend` SHALL resolver o CWD da sessão e rejeitar se não houver `/cwd`
4. WHEN o CWD existe THEN o daemon SHALL criar `<cwd>/uploads/` se não existir
5. WHEN o daemon copia um anexo THEN SHALL usar `O_NOFOLLOW`, validar path traversal e renomear em caso de conflito de nome
6. WHEN o daemon copia os anexos com sucesso THEN SHALL anexar ao prompt uma nota listando os ficheiros em `./uploads/`
7. WHEN `validateMessage` recebe uma mensagem com attachments THEN SHALL validar contagem e tamanho total

**Independent Test**: Enviar IPC message com `attachments:[{path:"/tmp/spec.md"}]` via `nc -U`, verificar que o ficheiro foi copiado para `<cwd>/uploads/spec.md` e que o prompt enviado ao pipeline menciona o anexo.

---

### P2: Drag-and-drop — Detectar path de documento colado

**User Story**: Como utilizador da TUI, quero arrastar um ficheiro de documento para o terminal e o TUI detectar automaticamente o path, para não ter de escrever `/attach` manualmente.

**Why P2**: Conveniência — alguns terminais inserem o path do ficheiro arrastado como texto.

**Acceptance Criteria**:

1. WHEN o utilizador arrasta um ficheiro para o terminal THEN o terminal insere o path como texto e o TUI SHALL detectar que é um path de ficheiro válido
2. WHEN o texto colado (bracketed paste) é um path absoluto de ficheiro existente e não é imagem THEN o TUI SHALL adicionar à lista de documentos pendentes em vez de inserir o texto no textarea
3. WHEN o texto colado é um path mas é uma imagem suportada THEN o TUI SHALL tratar como imagem (fluxo existente)
4. WHEN o texto colado é um path mas não existe THEN o TUI SHALL inserir o texto no textarea normalmente
5. WHEN o texto colado é multi-linha THEN o TUI SHALL inserir no textarea normalmente

**Independent Test**: Arrastar ficheiro `.pdf` para o terminal, verificar badge aparece em vez de texto no input.

---

## Architecture

### Fluxo de dados

```
Utilizador na TUI
  │
  ├── /attach ~/docs/spec.md        (P1)
  ├── drag-and-drop de ficheiro     (P2)
  │
  ├── TUI: valida path (existe, regular, não symlink, tamanho)
  │   internal/tui/attachment.go (novo)
  │
  ├── TUI: mantém pendingAttachments []pendingAttachment no Model
  │   badges [📎 spec.md] no view
  │
  ├── Enter → IPCMessage{type:"send", text:..., attachments:[{path, name}]}
  │   attachments enviadas como paths (não conteúdo)
  │
  ├── Daemon: handleTUISend
  │   resolve CWD → rejeita se vazio
  │   copia cada attachment para <cwd>/uploads/<filename>
  │   anexa nota ao prompt
  │   pipeSvc.Process(chatID, threadID, 0, textWithAttachmentNote, images, userID, true)
  │
  ├── Pipeline: agente recebe mensagem com referência aos ficheiros
  │   Agente usa Read/Glob/etc para processar os documentos
  │
  └── Resposta volta por streaming
```

### Decisão: path no IPC, não conteúdo

O IPC tem `maxLineSize = 64KB` por linha JSON. Documentos podem ter dezenas de MB.

**Decisão**: O TUI envia **paths** de ficheiros no IPC, não conteúdo. O daemon copia o ficheiro do filesystem local para `<cwd>/uploads/`. Isto:

- Mantém o protocolo IPC leve
- Evita aumentar o buffer do scanner
- Materializa o documento no projeto, onde o agente já tem acesso via tools
- Permite qualquer formato sem validação de MIME específica

### Decisão: CWD obrigatório

Anexar documentos sem um projeto ativo é ambíguo: para onde copiar? A experiência do Telegram é diferente (ficheiro é descarregado para temp e injetado no prompt). Na TUI, o contexto é o projeto local.

**Decisão**: `/attach` requer `/cwd` definido. Sem CWD, o comando retorna erro imediato.

### Decisão: agente processa, não Aurelia

Para imagens, o Aurelia base64-encoda e envia ao modelo vision. Para documentos genéricos, o Aurelia não sabe qual é o formato nem como extrair o conteúdo de forma útil.

**Decisão**: Aurelia apenas copia o ficheiro para `./uploads/` e menciona a sua existência no prompt. O agente decide como processar (Read, ferramentas externas, etc.).

---

## Non-Functional Requirements

### Segurança

- O daemon valida que o path de destino (`<cwd>/uploads/<filename>`) não escapa do CWD.
- Não seguir symlinks na origem nem no destino (`O_NOFOLLOW`).
- Rejeitar ficheiros fora do home do utilizador ou `/tmp` como origem (defesa em profundidade).
- Não sobrescrever ficheiros existentes em `<cwd>/uploads/` — renomear com sufixo numérico.
- Limpar ficheiros temp criados pelo TUI em caso de erro.

### Performance

- A cópia acontece no daemon, não no TUI.
- Limite de 25 MB por ficheiro e 100 MB total previne anexos gigantes.
- Múltiplos anexos são enviados numa só IPC message.

### Compatibilidade

- macOS (darwin/arm64) prioritário.
- Linux secundário.
- Windows não suportado no MVP.

---

## Open Questions

1. **Limpeza de `uploads/`**: Deve o Aurelia apagar anexos antigos automaticamente? Recomendação: não no MVP — o utilizador gere o diretório.
2. **Conflitos de nome**: Se `<cwd>/uploads/spec.md` já existe, renomear para `spec_1.md`? Sim, para evitar sobrescritas acidentais.
3. **Mensagem ao agente**: Deve incluir o path relativo (`./uploads/spec.md`) e uma instrução curta? Sim.
