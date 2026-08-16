# Sincronizar indicador de modelo da TUI — Tasks

**Change:** `tui-model-indicator-sync-v1`  
**Dependency graph:** `T0 → T1 → T2 → T3`  
**Implementation branch:** `feature/tui-model-indicator-sync`  
**Terminal boundary:** validação live da TUI; promoção/release ficam fora até
aprovação explícita.

## T0 — Baseline e preflight

- [ ] Criar branch dedicada a partir de `main` atualizado.
- [ ] Reproduzir: selecionar modelo A, selecionar modelo B e verificar que a
      próxima pergunta usa B enquanto header/sidebar continuam em A.
- [ ] Reproduzir pelo wizard, `/model auto`, fila e `pendingSessionModel`.
- [ ] Confirmar que a mudança atual persiste `app.json` e limpa a sessão sem
      regressão.
- [ ] Registrar baseline de `go test ./internal/tui/...` e
      `go test ./cmd/aurelia/...`.

**Validation:** reprodução confirmada e runtime/configuração separados do
estado visual.

## T1 — Refresh pós-comando

- [ ] Adicionar classificação/helper para comandos `/model`.
- [ ] Marcar refresh pendente nos caminhos textual, wizard, fila e sessão nova.
- [ ] Após `stream_end` de comando de modelo, solicitar `fetchTUIStatus` da
      sessão ativa.
- [ ] Limpar o marcador em erro, EOF inesperado e erro de stream.
- [ ] Não disparar `/status` extra para mensagens normais ou comandos não
      relacionados a modelo.
- [ ] Não substituir `activeModel` por vazio quando a resposta de status não
      contiver um modelo confirmado.

**Validation:** a resposta `tuiStatusMsg` atualiza `activeModel` e o mesmo
valor é usado por header e sidebar.

## T2 — Testes de comportamento

- [ ] Adicionar teste para modelo textual: `model-a → model-b` após
      `stream_end` + status.
- [ ] Adicionar teste para `/model auto` (`PI default`).
- [ ] Adicionar teste para wizard, fila e `pendingSessionModel`.
- [ ] Adicionar teste de falha: modelo anterior permanece confirmado.
- [ ] Adicionar teste de que mensagem normal não solicita refresh de status.
- [ ] Preservar testes existentes de parser de status, header e sidebar.

**Validation:** cada assertion A1–A3 possui teste identificável.

## T3 — Quality and live validation

- [ ] Executar `go test ./internal/tui/... -count=1`.
- [ ] Executar `go test ./cmd/aurelia/... -count=1`.
- [ ] Executar `go test ./... -short -count=1`.
- [ ] Executar `go vet ./...`.
- [ ] Solicitar code review focado no loop Bubble Tea/IPC e concorrência de
      status.
- [ ] Validar live: troca manual A/B, wizard, `/model auto`, troca de sessão e
      comando enfileirado.
- [ ] Propor bump de versão/changelog ao Igor; não commitar release sem
      aprovação.

**Validation:** Evidence Matrix completa para A1–A3; nenhuma promoção nesta
change.

## Explicit non-goals checklist

- [ ] Não alterar `cmd/aurelia/tui_model_handler.go` além do necessário para
      preservar o contrato existente.
- [ ] Não alterar catálogo PI, provider/model resolution ou persistência.
- [ ] Não alterar protocolo IPC.
- [ ] Não compartilhar sessões TUI/Telegram.
