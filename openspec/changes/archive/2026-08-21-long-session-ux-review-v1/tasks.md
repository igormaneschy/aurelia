# Experiência fluida em sessões longas — Tasks

**Change:** `long-session-ux-review-v1`  
**Dependency graph:** `T0 → T1 → T2 → T3 → T4 → T5 → T6`  
**Implementation branch:** `feature/long-session-ux`  
**Terminal boundary:** validação live no daemon; promoção/release ficam fora
até aprovação explícita.

## T0 — Baseline e preflight

- [x] Criar branch dedicada a partir de `main` atualizado; não implementar em
      `main`.
- [x] Executar baseline local: `make check` ou registrar cada gate ausente.
- [x] Capturar métricas/runlogs de 7 dias, incluindo os casos de stall de
      1186s, contexto próximo de 1M tokens e restart em voo.
- [x] Criar fixtures redigidas para: stream silencioso, ferramenta longa,
      compaction sem redução e process death.
- [x] Confirmar o contrato atual de `run_id`, `request_id`, redaction e
      `session_file`.
- [x] Parar e reportar blocker se o baseline ou as fixtures não forem obtidos.

**Validation:** baseline e lacunas documentados; nenhuma mudança runtime.

## T1 — Instrumentação Bridge/pipeline/runlog

- [x] Adicionar timestamp ISO aos logs operacionais do Bridge.
- [x] Emitir evento NDJSON `stall` com `request_id`, severidade, `silent_ms` e
      fonte.
- [x] Medir duração de ferramentas sem registrar comando/argumentos brutos.
- [x] Persistir first feedback, max silence, stall/steer, compaction delta,
      process death e restart no runlog.
- [x] Expor os novos agregados em `aurelia debug metrics --json`.
- [x] Adicionar testes de redaction, correlação e compatibilidade NDJSON.

**Validation:** cada run fixture tem timeline explicável e nenhum segredo.

## T2 — Progresso comum e adapters Telegram/TUI

- [x] Evoluir `ProgressReporter` com estado/status surface-neutral.
- [x] Implementar heartbeat inicial e marcos escalonados no core.
- [x] Adaptar Telegram para atualizar um único recibo sem duplicar mensagens.
- [x] Adaptar `tuiOutput`/`tuiProgress` para atualizar o indicador visual sem
      inserir heartbeat técnico no transcript.
- [x] Adicionar `EventTypeProgress` e payload tipado no IPC TUI.
- [x] Mapear estados humanos: trabalhando, aguardando, stall warning, stall
      urgent, concluído, cancelado e falho.
- [x] Preservar throttle Telegram de 1,5s e o fluxo de progresso existente da
      TUI.
- [x] Testar término, cancelamento, erro de edição e ausência de bot.

**Validation:** fixture de silêncio produz atualização visível ≤90s em
Telegram e TUI; Telegram usa uma mensagem de progresso e TUI não polui o
transcript.

## T3 — Liveness-aware timeout

- [x] Separar atividade produtiva de sonda de liveness no watchdog.
- [x] Definir thresholds de aviso e segurança a partir do baseline, sem
      aumentar/remover timeout às cegas.
- [x] Persistir a origem exata: idle, max execution, bridge query,
      provider/PI, process death ou user cancel.
- [x] Preservar checkpoint e `/continuar` após timeout.
- [x] Testar Bridge vivo silencioso, Bridge morto e cancelamento explícito.

**Validation:** cada cenário termina com status/origin/checkpoint verificáveis.

## T4 — Context lifecycle

- [x] Registrar tokens/context usage antes e depois de compactação/rotação.
- [x] Classificar compactação eficaz, neutra ou regressiva.
- [x] Investigar o caso observado em que input cresceu após compaction.
- [x] Definir, com evidência, se threshold/rotação deve mudar; não alterar
      política de rotação summary-seeded sem teste de continuidade.
- [x] Notificar o usuário quando houver compactação/recuperação relevante.

**Validation:** fixture conserva continuidade e evidencia delta de tokens;
nenhuma compactação regressiva é marcada silenciosamente como sucesso.

## T5 — Restart seguro e revisão de stream

- [x] Obter aprovação explícita para alterar `Makefile`, hook ou scripts de
      serviço.
- [x] Implementar drain curto, adiamento ou modo force documentado para
      `make deploy` com run ativo.
- [x] Garantir cold resume/notificação para shutdown inevitável.
- [x] Remover retry silencioso em processo moribundo.
- [x] Classificar divergência stream/result e preservar `result` como fonte
      autoritativa.

**Validation:** cenário de deploy com run ativo não perde a mensagem nem o
`session_file`; divergência tem causa/metadado redigido.

## T6 — Quality, review e validação live

- [x] Executar `go test ./... -short -count=1`.
- [x] Executar `go vet ./...`.
- [x] Executar `make check` quando as ferramentas estiverem disponíveis.
- [x] Se `bridge/index.ts` mudar, executar `cd bridge && npm run build` e
      sincronizar os bundles conforme `AGENTS.md`/`Makefile`.
- [x] Solicitar code review focado em pipeline/Bridge/Telegram.
- [x] Solicitar security review para redaction, comandos e deploy.
- [x] Executar `make deploy` somente na branch de feature após commit e
      autorização operacional.
- [x] Validar Telegram: run normal, stall sintético, ferramenta longa,
-      compaction, timeout, `/stop`, restart e `continuar`.
- [x] Validar TUI: run normal, stall sintético, ferramenta longa, compaction,
      timeout, cancelamento e retomada.
- [x] Registrar Evidence Matrix para A1–A5.
- [x] Propor bump de versão e changelog ao Igor; não commitar release sem
      aprovação.

**Validation:** todos os assertions têm evidência; PASS sem teste/comando/
validação live é tratado como UNVERIFIED.

## Explicit non-goals checklist

- [x] Não trocar provider/modelo.
- [x] Não alterar o protocolo NDJSON além dos eventos/ campos documentados.
- [x] Não apagar sessões, checkpoints, credenciais ou memória.
- [x] Não auto-rotacionar por token sem validar continuidade.
- [x] Não alterar deploy sem aprovação explícita.
