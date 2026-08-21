# Sincronizar indicador de modelo da TUI — Tasks

**Change:** `tui-model-indicator-sync-v1`  
**Dependency graph:** `T0 → T1 → T2 → T3`  
**Implementation branch:** `feature/tui-model-indicator-sync`  
**Terminal boundary:** validação live da TUI; promoção/release ficam fora até
aprovação explícita.

## T0 — Baseline e preflight

- [x] Criar branch dedicada a partir de `main` atualizado.
- [x] Reproduzir: selecionar modelo A, selecionar modelo B e verificar que a
      próxima pergunta usa B enquanto header/sidebar continuam em A.
- [x] Reproduzir pelo wizard, `/model auto`, fila e `pendingSessionModel`.
- [x] Confirmar que a mudança atual persiste `app.json` e limpa a sessão sem
      regressão.
- [x] Registrar baseline de `go test ./internal/tui/...` e
      `go test ./cmd/aurelia/...`.

**Validation:** reprodução confirmada e runtime/configuração separados do
estado visual.

## T1 — Refresh pós-comando

- [x] Adicionar classificação/helper para comandos `/model`.
- [x] Marcar refresh pendente nos caminhos textual, wizard, fila e sessão nova.
- [x] Após `stream_end` de comando de modelo, solicitar `fetchTUIStatus` da
      sessão ativa.
- [x] Limpar o marcador em erro, EOF inesperado e erro de stream.
- [x] Não disparar `/status` extra para mensagens normais ou comandos não
      relacionados a modelo.
- [x] Não substituir `activeModel` por vazio quando a resposta de status não
      contiver um modelo confirmado.

**Validation:** a resposta `tuiStatusMsg` atualiza `activeModel` e o mesmo
valor é usado por header e sidebar.

## T2 — Testes de comportamento

- [x] Adicionar teste para modelo textual: `model-a → model-b` após
      `stream_end` + status.
- [x] Adicionar teste para `/model auto` (`PI default`).
- [x] Adicionar teste para wizard, fila e `pendingSessionModel`.
- [x] Adicionar teste de falha: modelo anterior permanece confirmado.
- [x] Adicionar teste de que mensagem normal não solicita refresh de status.
- [x] Preservar testes existentes de parser de status, header e sidebar.

**Validation:** cada assertion A1–A3 possui teste identificável.

## T3 — Quality and live validation

- [x] Executar `go test ./internal/tui/... -count=1`.
- [x] Executar `go test ./cmd/aurelia/... -count=1`.
- [x] Executar `go test ./... -short -count=1`.
- [x] Executar `go vet ./...`.
- [x] Solicitar code review focado no loop Bubble Tea/IPC e concorrência de
      status.
- [x] Validar live: troca manual A/B, wizard, `/model auto`, troca de sessão e
      comando enfileirado.
- [x] Propor bump de versão/changelog ao Igor; não commitar release sem
      aprovação.

**Validation:** Evidence Matrix completa para A1–A3; nenhuma promoção nesta
change.

## Explicit non-goals checklist

- [x] Não alterar `cmd/aurelia/tui_model_handler.go` além do necessário para
      preservar o contrato existente.
- [x] Não alterar catálogo PI, provider/model resolution ou persistência.
- [x] Não alterar protocolo IPC.
- [x] Não compartilhar sessões TUI/Telegram.
