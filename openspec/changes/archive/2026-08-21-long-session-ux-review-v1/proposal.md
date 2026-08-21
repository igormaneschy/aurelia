# Experiência fluida em sessões longas

**Status:** proposed  
**Created:** 2026-08-11  
**Owner:** Aurelia architecture  
**Change type:** reliability / UX / observability

## Why

A revisão dos logs recentes encontrou um problema concentrado no caminho
Telegram/TUI → pipeline → Bridge → PI SDK: sessões longas continuam vivas, mas
o usuário fica sem feedback, o contexto cresce além do limite operacional e
deploys interrompem execuções em voo.

### Evidência operacional

Fonte: `~/.aurelia/logs/aurelia.stderr.log`, linhas `31937–34025`.

- 346 eventos de stall em 9 runs; um run ficou 1186s (~20min) sem evento
  produtivo.
- O run mais longo recebeu no máximo um heartbeat antes do
  `idle_bridge_timeout` de 20min.
- Houve 10 reinícios do daemon, 4 quedas de requisições em voo e 2 rotações
  interrompidas.
- Uma sessão cresceu de aproximadamente 32k para 998k tokens, com turnos de
  até 21min e custo por turno de `$0,0085` para `$0,1824`.
- A compactação reportou sucesso sem reduzir efetivamente o input em pelo
  menos um ciclo observado.
- Foram registrados 17 avisos de divergência entre texto transmitido e
  resultado final, em 42 turnos.

### Qualidade da evidência

- A janela real disponível foi `2026-08-06 10:48` → `2026-08-11 10:07`.
- Não existem eventos entre 07 e 09/08; a causa do intervalo é desconhecida.
- O log mistura linhas Go com timestamp e linhas do Bridge sem timestamp.
- A maior parte da janela está na v0.40.1; a v0.40.2 aparece apenas nos
  minutos finais. A validação pós-mudança é obrigatória.

## Goal

Tornar sessões longas previsíveis e fluidas: o usuário deve saber que a sessão
continua ativa, receber uma recuperação clara quando algo realmente falhar,
não perder trabalho durante reinícios e não sofrer degradação silenciosa por
crescimento de contexto.

## In scope

- Instrumentação de stalls, duração de ferramentas, compactação, timeout,
  restart e divergência de streaming.
- Heartbeats escalonados através de um contrato de progresso comum, com
  apresentação própria para Telegram e TUI.
- Separação entre liveness do Bridge, atividade produtiva e timeout de
  segurança.
- Política mensurável para compactação/rotação, preservando continuidade.
- Drenagem ou recuperação explícita de runs durante `make deploy`/shutdown.
- Testes unitários, integração sintética, documentação operacional e
  validação live no daemon.

## Out of scope

- Troca de provider/modelo.
- Reescrita geral do pipeline ou do protocolo NDJSON.
- Alterações no ciclo MCP/segurança não necessárias para a observabilidade.
- Rotação automática baseada apenas em resumo sem prova de preservação de
  contexto.
- Bump de versão, `CHANGELOG.md`, promoção para `stable/*`, merge ou push.
- Alteração do hook/deploy sem aprovação explícita do Igor.

## Decision

Executar em fases, nesta ordem:

1. **Baseline e instrumentação** — fechar as lacunas de diagnóstico antes de
   ajustar thresholds.
2. **Feedback de progresso** — corrigir imediatamente a espera silenciosa,
   sem criar spam no Telegram.
3. **Liveness e timeout** — manter um limite de segurança, mas não confundir
   ausência temporária de eventos com falha sem sondar o estado do Bridge.
4. **Contexto** — medir compactação antes/depois; manter o PI SDK como caminho
   primário e só alterar política de rotação com evidência.
5. **Restart seguro** — escolher entre drain, adiamento do deploy ou replay
   explícito; nenhum retry deve depender de um processo já morrendo.

## Priorities

| Prioridade | Frente | Resultado esperado |
|---|---|---|
| P0 | Progresso durante stall | silêncio visível máximo de 90s |
| P0 | Liveness/timeout | origem de timeout clara e checkpoint preservado |
| P0 | Contexto | turns longos não escalam para centenas de milhares de tokens sem decisão explícita |
| P0 | Restart | zero perda silenciosa de run ativo |
| P1 | Observabilidade/stream | diagnóstico reproduzível e divergências classificadas |

## Prior lessons applied

- `incident-regression-from-daemon-logs.md` → valores observados nos logs serão
  transformados em cenários de regressão, não apenas em thresholds presumidos.
- `summary-seeded-session-rotation.md` → não usar rotação summary-seeded como
  correção cega para crescimento de tokens.
- `daemon-log-visibility.md` → toda validação live deve preservar startup e
  stderr visíveis.
- `post-impl-review-gaps.md` → a implementação exigirá revisão externa antes
  de qualquer operação mutável.

## Production path

```text
Telegram/TUI
  → internal/pipeline
  → internal/bridge (Go/NDJSON)
  → bridge/index.ts
  → AgentSession / PI SDK
  → provider e ferramentas
```

## Surface scope

Esta change é **core-first** e cobre as duas superfícies:

- **Core compartilhado:** `internal/pipeline`, `internal/bridge`, lifecycle de
  sessão, checkpoints, runlog e política de timeout/restart.
- **Telegram:** o estado de progresso vira um único recibo editável, sem spam
  de mensagens.
- **TUI:** o mesmo estado chega pelo `tuiOutput`/IPC e alimenta o progresso
  visual da sessão, sem aplicar semântica de edição de mensagem do Telegram.

O contrato de progresso será surface-neutral; somente o adapter de apresentação
varia. `tui-model-indicator-sync-v1` continua separado porque corrige a
sincronização do indicador de modelo, não a execução longa.

## Rollout boundary

Esta change termina com implementação em `feature/long-session-ux`, testes
locais, revisão e validação live documentada. A criação de `stable/*`, bump de
versão, changelog, merge para `main`, push e mudanças operacionais de deploy
exigem aprovação explícita posterior.

## References

- `internal/pipeline/pipeline.go` — heartbeat, timeout, run lifecycle e checkpoint.
- `internal/pipeline/idle_timeout.go` — cancelamento por silêncio.
- `internal/telegram/progress.go` — mensagem e throttle de progresso.
- `bridge/index.ts` — stall detection e lifecycle do AgentSession.
- `docs/OBSERVABILITY.md` / `docs/OPERATIONS.md` — contratos operacionais atuais.
