# Experiência fluida em sessões longas — Design

## 1. Current state

O checkout está limpo em `main`, v0.40.2. A investigação operacional foi
majoritariamente feita sobre v0.40.1.

### Limites atuais

- `bridgeExecutionTimeout`: 30min em `internal/pipeline/pipeline.go`.
- `idle_timeout_minutes`: default 20min em `internal/config/config.go`.
- `heartbeatMonitor`: ticker de 10s, primeiro heartbeat após 15s, mas
  `beatSent` só é resetado quando chega `tool_use`.
- Bridge: registra stall a partir de 30s e envia steer em 60s/120s, mas esses
  sinais ficam no stderr e não chegam ao runlog/pipeline como eventos.
- Telegram `progressReporter`: throttle de 1,5s, porém heartbeats e alertas
  usam novas mensagens em vez de atualizar o recibo de progresso.
- TUI `tuiOutput` já usa `tuiProgress` para enviar progresso por IPC; a change
  deve preservar esse adapter e impedir que heartbeat técnico polua o
  transcript como texto comum.
- Runlog possui duração, tokens, custo, tool count e timeout origin, mas não
  possui métricas de maior intervalo silencioso, first feedback, duração de
  ferramenta ou duração da compactação.

## 2. Target architecture

### 2.1 Bridge event contract

O Bridge continuará emitindo NDJSON e preservará eventos existentes. Serão
adicionados campos/eventos mínimos, redigidos e correlacionados por
`request_id`:

```json
{
  "event": "stall",
  "request_id": "...",
  "severity": "warning|urgent",
  "silent_ms": 120000,
  "source": "bridge_health"
}
```

O `tool_result` deverá carregar `duration_ms` quando o Bridge conseguir medir
com segurança o intervalo entre início e fim. Nenhum comando bruto, argumento
ou resultado completo será incluído nesse evento.

O Bridge deverá acrescentar timestamp ISO às próprias linhas operacionais. O
timestamp do evento NDJSON continua sendo a fonte de correlação entre Bridge,
pipeline e runlog.

### 2.2 Surface-neutral progress contract

O pipeline produzirá um estado de progresso comum por run:

```text
running → waiting/working → stalled-warning → stalled-urgent
        → completed | canceled | timed_out | failed
```

O contrato comum deve carregar, no mínimo, `phase`, `elapsed`, `severity` e
`summary` redigido. Cada superfície terá um adapter:

- **Telegram:** uma única mensagem/recibo editável por run. Heartbeats,
  ferramentas e avisos usam o mesmo throttle e não criam mensagens ilimitadas.
- **TUI:** um novo evento IPC tipado `progress` atualiza o indicador da sessão.
  O heartbeat não é injetado como mensagem normal no transcript; o payload é
  renderizado no footer/header da TUI.

Contrato mínimo do evento IPC TUI:

```json
{
  "type": "progress",
  "body": {
    "phase": "working|waiting|stall_warning|stall_urgent|terminal",
    "elapsed_ms": 120000,
    "severity": "info|warning|urgent",
    "summary": "Ainda estou processando."
  }
}
```

Regras comuns:

1. Primeiro feedback visível até 15s.
2. Durante silêncio, cada adapter atualiza sua representação em intervalos que
   mantenham silêncio visível abaixo de 90s.
3. A mensagem deve distinguir `trabalhando`, `aguardando ferramenta/provedor`
   e `recuperando`; não expor detalhes técnicos ou prompts.
4. `ReportText`, `ReportTool`, `ReportStatus` e heartbeat devem compartilhar o
   mesmo estado e regras de encerramento.
5. Ao terminar, cada adapter encerra seu indicador sem deixar estado visual
   órfão.

### 2.3 Liveness and timeout policy

Silêncio será tratado em três camadas:

1. **Produtividade:** eventos `assistant`, `tool_execution_start/end` e
   `message_update` atualizam a atividade produtiva.
2. **Liveness:** `ping`/`get-state` e estado do processo distinguem Bridge vivo
   de processo morto ou sem resposta.
3. **Safety timeout:** após o limite configurado, com checkpoint e origem
   persistidos, a execução pode ser cancelada de forma explícita.

O primeiro ciclo não deve simplesmente aumentar ou remover o timeout. Deve
registrar a fase, sondar o Bridge e apresentar aviso escalonado antes do
cancelamento. Cancelamento explícito do usuário continua imediato.

Origens mínimas preservadas:

```text
max_execution_timeout
idle_bridge_timeout
bridge_query_timeout
provider/pi_timeout
process_death
user_cancel
```

### 2.4 Context lifecycle

O PI SDK permanece responsável pela compactação normal. Antes de mudar os
thresholds:

1. registrar tokens/context usage antes e depois de cada compactação;
2. classificar compactação eficaz, neutra ou regressiva;
3. medir custo e duração do turno seguinte;
4. só então decidir se a rotação deve ser antecipada;
5. executar rotação somente em boundary seguro de turno ou em recuperação
   explícita, preservando `session_file` e contexto operacional.

Uma rotação interrompida por restart não pode ser tratada como resultado válido
nem deixar o retry dependente do processo moribundo.

### 2.5 Restart safety

A política operacional proposta é:

1. `make deploy` consulta se há runs ativos antes de `launchctl kickstart -k`;
2. se houver run, aguarda uma janela curta de drain ou exige modo force
   explícito;
3. se o shutdown ocorrer mesmo assim, o runlog/continuity deve produzir uma
   notificação de retomada, sem retry silencioso;
4. o próximo turno retoma em modo frio usando o `session_file` persistido.

Esta parte exige decisão/aprovação do Igor antes de editar `Makefile`, hooks ou
scripts de serviço.

### 2.6 Observability and stream consistency

O runlog deverá permitir responder, para qualquer run longo:

- quando o primeiro feedback chegou;
- qual foi o maior intervalo sem evento produtivo;
- qual ferramenta estava ativa e por quanto tempo;
- quantos stalls/steers ocorreram e se houve recuperação;
- quando compactação/rotação iniciou, terminou e qual foi o delta de tokens;
- se houve restart, timeout ou process death;
- quanto o texto transmitido divergiu do `result` final.

O `result` final continua autoritativo para a resposta. A divergência deve ser
classificada antes de qualquer mudança de UX: truncamento, reescrita do SDK,
consolidação ou perda de evento.

## 3. Implementation phases

### Phase 0 — Baseline and preflight

- Criar `feature/long-session-ux` a partir de `main` atualizado.
- Capturar baseline com `make check` e métricas dos últimos 7 dias.
- Fixar uma fixture redigida de run com stall, uma com compaction e uma com
  process death.
- Confirmar comportamento de `make deploy` e cold resume sem alterar operação.

### Phase 1 — Instrumentation

- Adicionar timestamps/eventos Bridge.
- Persistir tool duration, stall, first feedback, max silence e compaction
  delta.
- Expor métricas em `aurelia debug metrics --json` sem armazenar segredos.

### Phase 2 — Progress UX: Telegram + TUI

- Implementar contrato surface-neutral e heartbeat escalonado.
- Adaptar Telegram para recibo único editável.
- Adaptar TUI para `tuiProgress`/IPC sem poluir transcript.
- Validar throttle, cancelamento e término sem mensagens órfãs.

### Phase 3 — Liveness and context

- Implementar política de sondagem e aviso escalonado.
- Instrumentar e corrigir compactação ineficaz.
- Só depois propor alteração de threshold/rotação, com evidência antes/depois.

### Phase 4 — Restart and stream review

- Após aprovação operacional, implementar drain/recovery de deploy.
- Classificar divergência stream/result e corrigir somente se a evidência indicar
  perda de UX ou conteúdo.

### Phase 5 — Review and live validation

- Code review focado em pipeline/bridge/Telegram.
- Security review para eventos redigidos e superfícies de comando/deploy.
- `make deploy`, smoke Telegram, cenário de stall sintético, cenário de
  compaction e cenário de restart.

## 4. Rollback

Rollback é reimplantar o último artefato conhecido como funcional, sem apagar
sessões, checkpoints, credenciais ou arquivos de memória. Mudanças de deploy
devem poder ser desativadas sem invalidar o runlog existente.

## 5. Review gates

1. Instrumentação não registra segredo, prompt bruto ou comando Bash.
2. Cada assertion A1–A5 possui teste/comando/evidência live.
3. Nenhum Critical/High permanece aberto.
4. O bundle do Bridge permanece sincronizado quando `bridge/index.ts` mudar.
5. Bump de versão e changelog ficam fora desta fase até aprovação explícita.
