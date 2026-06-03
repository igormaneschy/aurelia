# UX Polish — Tasks

**Design**: `.specs/features/ux-polish/design.md`
**Status**: Draft

---

## Execution Plan

### Phase 1: Foundation (Parallel)

```
T1 ──┐
T2 ──┼──→ (independentes)
T3 ──┘
```

### Phase 2: Core UX (Parallel)

```
     ┌→ T4 ─┐
T1 ──┤      ├──→ T7
     ├→ T5 ─┤
T2 ──┤      ├──→ T7
     └→ T6 ─┘
```

### Phase 3: Integration & Polish (Sequential)

```
T7 → T8 → T9
```

---

## Task Breakdown

### T1: Ack de Recebimento (👀 reaction)

**What**: Adicionar reação 👀 na mensagem do usuário imediatamente ao receber, e ✅ ao finalizar.
**Where**: `internal/telegram/bot.go` (novos métodos), todos os handlers em `bot_middleware.go` e `input.go`
**Depends on**: None
**Reuses**: `telebot.ReactionOptions` existente

**Implementation details**:
- Novo método `ackMessage(msg *telebot.Message)` no BotController
- Novo método `confirmMessage(msg *telebot.Message)` no BotController
- Em cada handler (`handleText`, `handlePhoto`, `handleDocument`, `handleVoice`, `handleHelpCommand`, `handleCwdCommand`, `handleResetCommand`, `handleUsageCommand`, `handleCronCommand`, `handleModelCommand`): chamar `bc.ackMessage(c.Message())` antes de qualquer processamento
- No pipeline, após `output.SendReply` ou `output.SendError`, chamar `bc.confirmMessage(originalMsg)` (pode ser feito via hook no `telegramPipelineOutput`)
- Para comandos locais (que não vão pro pipeline), chamar `confirmMessage` antes de enviar a resposta

**Done when**:
- [ ] `ackMessage()` implementado
- [ ] `confirmMessage()` implementado
- [ ] Todos os handlers chamam `ackMessage()`
- [ ] Pipeline confirma mensagem ao finalizar
- [ ] Teste: mensagem recebida → 👀 aparece
- [ ] Teste: resposta enviada → 👀 some (ou vira ✅)
- [ ] Tests pass: `go test ./internal/telegram/...`

**Verify:**
```bash
go test ./internal/telegram/ -run TestAck -v
```

---

### T2: Unidades Humanas (`humanBytes`)

**What**: Converter bytes para MB/KB/B legíveis; aplicar em mensagens de imagem grande.
**Where**: `internal/telegram/input.go` (helper + `imageTooLargeError`)
**Depends on**: None
**Reuses**: Apenas formatação

**Implementation details**:
- Função `humanBytes(n int) string` com regras: <1KB → B, <1MB → KB, else → MB
- Modificar `imageTooLargeError.UserMessage()` para usar `humanBytes()`
- Adicionar testes para `humanBytes()` cobrindo: 0, 512, 1024, 1048576, 15728640

**Done when**:
- [ ] `humanBytes()` implementado
- [ ] `imageTooLargeError.UserMessage()` atualizado
- [ ] Teste: 15728640 → "15.0 MB"
- [ ] Teste: 1024 → "1.0 KB"
- [ ] Teste: 512 → "512 B"
- [ ] Tests pass: `go test ./internal/telegram/...`

**Verify:**
```bash
go test ./internal/telegram/ -run TestHumanBytes -v
```

---

### T3: Model Switch Local

**What**: Trocar `ClearAll(chatID)` por `Clear(chatID, threadID)` na troca de modelo.
**Where**: `internal/telegram/bot_middleware.go:setModelFromCallback`, `internal/telegram/commands.go:cmdSetModel`
**Depends on**: None
**Reuses**: `session.Store.Clear()` existente

**Implementation details**:
- Em `setModelFromCallback`: usar `c.Callback().Message.ThreadID` para obter threadID; chamar `bc.sessions.Clear(chatID, threadID)`
- Em `cmdSetModel`: adicionar parâmetro `threadID int` (ou usar 0 se não disponível); chamar `bc.sessions.Clear(chatID, threadID)`
- Atualizar mensagem de confirmação para mencionar o escopo

**Done when**:
- [ ] `setModelFromCallback` usa `Clear(chatID, threadID)`
- [ ] `cmdSetModel` usa `Clear(chatID, threadID)`
- [ ] Mensagem de confirmação atualizada ("Sessão deste tópico foi resetada")
- [ ] Teste: troca de modelo em chat privado → session limpa
- [ ] Teste (manual): troca de modelo em fórum → outro tópico preservado
- [ ] Tests pass: `go test ./internal/telegram/...`

**Verify:**
```bash
go test ./internal/telegram/ -run TestModelSwitch -v
```

---

### T4: Progresso Rico (timer + 8 ferramentas)

**What**: Adicionar timer e aumentar limite de ferramentas no progress reporter.
**Where**: `internal/telegram/progress.go`
**Depends on**: None
**Reuses**: Estrutura existente `progressReporter`

**Implementation details**:
- Campo `startTime time.Time` no struct
- Inicializar `startTime: time.Now()` nos construtores
- `ReportTool`: calcular `time.Since(p.startTime)`, formatar com `formatDuration()`, prefixar texto
- Limite de display: 5 → 8
- `formatDuration(d time.Duration) string`: <60s → "Xs", else → "Xm Xs"

**Done when**:
- [ ] Campo `startTime` adicionado
- [ ] `formatDuration()` implementado
- [ ] Limite de display alterado para 8
- [ ] Timer aparece no topo da mensagem de progresso
- [ ] Teste: 6 ferramentas → todas exibidas
- [ ] Teste: timer incrementa corretamente
- [ ] Tests pass: `go test ./internal/telegram/...`

**Verify:**
```bash
go test ./internal/telegram/ -run TestProgress -v
```

---

### T5: Status para Humanos

**What**: Refatorar `cmdStatus` para remover jargão e adicionar info útil.
**Where**: `internal/telegram/commands.go:cmdStatus`
**Depends on**: None
**Reuses**: `session.Tracker.Get()`, `session.Store.GetCwd()`

**Implementation details**:
- Remover linha de session ID (`sid=... (warm)`)
- Adicionar: diretório atual (CWD), resumo da sessão (mensagens, tokens)
- Manter: bridge status, modelo, agendamentos
- Usar emojis para tornar visualmente escaneável

**Done when**:
- [ ] Session ID removido da saída
- [ ] CWD adicionado quando disponível
- [ ] Resumo de sessão (turns + tokens) adicionado
- [ ] Emojis aplicados a cada linha
- [ ] Teste: saída não contém "sid=" nem "warm" nem "cold"
- [ ] Teste: saída contém "mensagens" quando há uso
- [ ] Tests pass: `go test ./internal/telegram/...`

**Verify:**
```bash
go test ./internal/telegram/ -run TestStatus -v
```

---

### T6: Reset com Memória

**What**: Mostrar resumo da sessão ao resetar.
**Where**: `internal/telegram/commands.go:cmdSessionReset`, `internal/telegram/bot_middleware.go:handleResetCommand`
**Depends on**: None
**Reuses**: `session.Tracker.Get()`

**Implementation details**:
- Antes de `Clear()`, capturar `usage := bc.tracker.Get(chatID)`
- Se `usage.NumTurns > 0`, retornar mensagem com resumo
- Se vazio, manter mensagem original
- `handleResetCommand` também deve usar o resumo (reutilizar `cmdSessionReset` ou duplicar lógica)

**Done when**:
- [ ] `cmdSessionReset` captura usage antes de limpar
- [ ] Mensagem com resumo quando aplicável
- [ ] `handleResetCommand` mostra resumo
- [ ] Teste: 3 mensagens → reset mostra "3 mensagens"
- [ ] Teste: 0 mensagens → reset mostra mensagem simples
- [ ] Tests pass: `go test ./internal/telegram/...`

**Verify:**
```bash
go test ./internal/telegram/ -run TestResetSummary -v
```

---

### T7: Erros Actionable + Help Rica + Documentos com Dica

**What**: Atualizar todas as mensagens de erro com dicas; refatorar /help; atualizar mensagem de formato não suportado.
**Where**: `internal/telegram/messages.go`, `internal/telegram/commands.go`, `internal/telegram/bot_middleware.go`, `internal/pipeline/pipeline.go`
**Depends on**: None (pode rodar em paralelo com T4/T5/T6)
**Reuses**: Constantes de mensagens existentes

**Implementation details**:
- `messages.go`: atualizar `unsupportedDocumentMessage`; adicionar novas constantes de erro com dicas
- `commands.go`: `cmdCronCreate` → usar mensagem com exemplo; `cmdStatus` → já feito em T5
- `pipeline.go`: mensagens de erro do bridge com dicas; mensagem de timeout com sugestão
- `bot_middleware.go`: `handleHelpCommand` com exemplos naturais

**Mensagens a atualizar**:
1. Bridge execute error → "Falha ao conectar... Dica: /new"
2. Bridge cooldown → "⏳ Processador em recuperação... ~%d segundos"
3. Cron parse error → com exemplo
4. Timeout → "Tente dividir em partes menores"
5. `/help` → com exemplos naturais
6. Documento não suportado → com dica de conversão

**Done when**:
- [ ] Todas as 6 mensagens atualizadas
- [ ] Teste: cada mensagem contém uma dica ou exemplo
- [ ] `/help` contém pelo menos 3 exemplos naturais
- [ ] Tests pass: `go test ./internal/telegram/...`

**Verify:**
```bash
go test ./internal/telegram/ -run TestMessages -v
go test ./internal/pipeline/ -run TestErrorHints -v
```

---

### T8: Concorrência Delegada ao PI SDK

**What**: Evitar que Aurelia prometa posição/ordem de fila quando quem gerencia mensagens subsequentes é o PI SDK.
**Where**: `internal/pipeline/pipeline.go` e mensagens de status/concorrência
**Depends on**: T1
**Reuses**: estado observável do PI SDK quando disponível

**Implementation details**:
- Não criar fila paralela em Aurelia para mensagens que o PI SDK já recebe.
- Evitar texto como "1 mensagem à frente" se Aurelia não controla essa posição.
- Manter ack visual imediato e mensagens curtas de recebido/processando.
- Se `/status` consultar o PI SDK, mostrar apenas estado observável e acionável.

**Done when**:
- [ ] Mensagens de concorrência não prometem posição de fila própria
- [ ] `/status` não mistura fila inventada com execução do PI SDK
- [ ] Tests pass: `go test ./internal/pipeline/...`

**Verify:**
```bash
go test ./internal/pipeline/ -short
```

---

### T9: Integration & Regression Tests

**What**: Rodar suite completa e verificar que nada quebrou.
**Where**: Todos os pacotes
**Depends on**: T1–T8
**Reuses**: Testes existentes

**Implementation details**:
- `go test ./... -short`
- `go build ./cmd/aurelia/`
- Verificar visualmente (Telegram) as 10 melhorias

**Done when**:
- [ ] `go test ./... -short` passa
- [ ] `go build ./cmd/aurelia/` compila
- [ ] Checklist visual verificado (ver Validation)

**Verify:**
```bash
go test ./... -short
go build ./cmd/aurelia/
```

---

## Parallel Execution Map

```
Phase 1 (Parallel, independentes):
  T1 ── Ack de Recebimento
  T2 ── Unidades Humanas
  T3 ── Model Switch Local

Phase 2 (Parallel, independentes entre si):
  T4 ── Progresso Rico
  T5 ── Status para Humanos
  T6 ── Reset com Memória
  T7 ── Erros + Help + Documentos

Phase 3 (Sequential, após T1–T7):
  T8 ── Concorrência Delegada ao PI SDK
  T9 ── Integration & Regression
```

**Ordem real de execução:**
```
T1 ────────────────────────────────────────────┐
T2 ────────────────────────────────────────────┤
T3 ────────────────────────────────────────────┤
T4 ────────────────────────────────────────────┼→ T8 → T9
T5 ────────────────────────────────────────────┤
T6 ────────────────────────────────────────────┤
T7 ────────────────────────────────────────────┘
```

---

## Task Granularity Check

| Task | Scope | Status |
|------|-------|--------|
| T1: Ack | 2 métodos + calls em ~8 handlers | Granular |
| T2: humanBytes | 1 função + 1 método + testes | Granular |
| T3: Model Switch Local | 2 lugares + mensagem | Granular |
| T4: Progresso Rico | 1 struct + 2 métodos + testes | Granular |
| T5: Status | 1 função refatorada + testes | Granular |
| T6: Reset com Memória | 1 função refatorada + 1 handler | Granular |
| T7: Mensagens | 6 constantes/mensagens atualizadas | Granular (mas coeso) |
| T8: Concorrência delegada | ajuste de mensagens/requisitos, sem fila paralela | Granular |
| T9: Integration | Testes e build | Granular |
