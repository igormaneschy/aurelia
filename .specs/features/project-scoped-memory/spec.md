# Project-Scoped CWD Overlay — Specification

**Roadmap step:** 5.5 (pós Context-Scoped Memory)
**Status:** 📋 Draft
**Depende de:** `.specs/features/project-binding/` (binding persistente), `.specs/features/project-memory/` (camadas de memória)
**Altera:** `.specs/features/project-memory/spec.md` (cwd_overlay passa a ser project-scoped)

---

## Problem Statement

Hoje o `cwd_overlay` é escopado por `(chatID, threadID)`. Cada frontend — TUI, Telegram DM, grupo/thread — que ativa o mesmo `/cwd /projeto/X` escreve e lê de **diretórios diferentes**:

| Fronteira | chatID | threadID | Caminho cwd_overlay |
|---|---|---|---|
| Telegram DM | `50929027` | `0` | `topics/chat_50929027/cwd_overlay/` |
| TUI default DM | `-9000001` | `0` | `topics/chat_-9000001/cwd_overlay/` |
| TUI sessão nomeada | `-9000002` | `0` | `topics/chat_-9000002/cwd_overlay/` |
| Grupo thread 2 | `-1003748894639` | `2` | `topics/chat_-1003748894639/thread_2/cwd_overlay/` |

Isso fragmenta o knowledge base do projeto. Um fato salvo pelo nudge no Telegram DM sobre o projeto "aurelia" **não aparece** quando o usuário pergunta o mesmo projeto na TUI, mesmo com o mesmo `/cwd`.

### Evidência Atual

No disco do usuário existem **4 diretórios** `cwd_overlay` distintos (17 arquivos .md no total), que na prática referem-se a **2 projetos**:

| Projeto | Diretórios cwd_overlay |
|---|---|
| `aurelia` | `chat_50929027/cwd_overlay/` (Telegram DM) |
| `aurelia` | `chat_-9000001/cwd_overlay/` (TUI) ← *esperado, mas vazio* |
| `AutoTradersOMQS` | `chat_-1003748894639/thread_2/cwd_overlay/` (grupo thread) |
| `AutoTradersOMQS` | `chat_-9000002/cwd_overlay/` (TUI sessão) |

A camada `user_global`, por outro lado, **já é compartilhada** entre TUI e Telegram porque ambos usam o mesmo `userID` (`50929027` — configurado via `default_persona_user_id`). O gap é exclusivamente no `cwd_overlay`.

---

## Goals

- [ ] `cwd_overlay` passa a ser escopado por **slug do projeto** (CWD), independente de chat/thread
- [ ] TUI, Telegram DM e grupos/threads compartilham o mesmo `cwd_overlay` quando ativam o mesmo `/cwd`
- [ ] `topic` memory permanece isolada por `(chatID, threadID)` — conversas ainda têm espaço privado
- [ ] `user_global` permanece inalterada (já compartilhada)
- [ ] Migração única dos dados existentes de `topics/*/cwd_overlay/` para `projects/<slug>/cwd_overlay/`
- [ ] `BootstrapConversationProjectMemory` cria o novo diretório
- [ ] Cache de memória invalida por slug em vez de por path de tópico
- [ ] Instruções de prompt atualizadas para refletir o novo path

## Non-Goals

- Alterar o escopo de `topic` memory (continua por chat/thread)
- Alterar o escopo de `user_global` (já compartilhado)
- Implementar multi-projeto simultâneo no mesmo turno de conversa
- Remover `TopicCwdOverlayDir()` (mantido para compatibilidade, mas não usado internamente após migração)
- Resolver dados legados de `projects/<slug>/memory/conversations/` (jóia arquitetural, mantido como está)

---

## Core Model

### Novo layout de diretórios

```
~/.aurelia/projects/<project-slug>/
├── cwd_overlay/          ← NOVO: memory compartilhada do projeto
│   ├── MEMORY.md
│   ├── architecture_decisions.md
│   └── ...
└── team/                 ← LEGADO (v0.31.0-): não utilizado, mantido para read-only
```

O `project-slug` é o mesmo gerado por `runtime.ProjectSlug(cwd)` (ex: `-Users-igormaneschy-aurelia`).

### Comparativo antes/depois

| Aspecto | Antes | Depois |
|---|---|---|
| **Path base** | `topics/chat_X/thread_Y/cwd_overlay/` | `projects/<slug>/cwd_overlay/` |
| **Escopo** | `(chatID, threadID)` | `(projectSlug)` |
| **Compartilhamento TUI↔Telegram** | ❌ Isolado | ✅ Compartilhado |
| **topic memory** | `topics/chat_X/thread_Y/` | Inalterado |
| **user_global** | `users/<userID>/memory/` | Inalterado |

### Fluxo de resolução de memória

```
Turno LLM
  ├── user_global → ~/.aurelia/users/<id>/memory/         (sempre)
  ├── topic       → ~/.aurelia/topics/chat_X/thread_Y/    (se threadID > 0)
  └── cwd_overlay → ~/.aurelia/projects/<slug>/cwd_overlay/  (se /cwd ativo)
                                    ↕
                            independente de chat/thread!
```

---

## Dados Existentes e Migração

### Mapeamento concreto (produção do usuário)

| Projeto | Slug | Origem (tópico/disperso) | Total de arquivos |
|---|---|---|---|
| `aurelia` | `-Users-igormaneschy-aurelia` | `chat_50929027/cwd_overlay/` | 1 |
| `aurelia` | `-Users-igormaneschy-aurelia` | `chat_-9000001/cwd_overlay/` | 0 (vazio) |
| `AutoTradersOMQS` | `-Users-igormaneschy-Workspaces-AutoTradersOMQS-GO` | `thread_2/cwd_overlay/` | 12 |
| `FluencyWave` | `-Volumes-Dados-Workspaces-FluencyWave-NewFLuencyWave-fluencywave` | `thread_1787/cwd_overlay/` | 4 |
| TUI sessão 2 | `-Users-igormaneschy-Workspaces-AutotradersOMQS-GO` | `chat_-9000002/cwd_overlay/` | 1 |

**Estratégia:** migração única (`make migrate-memory-paths` ou comando ad-hoc) que:
1. Lê cada `topics/*/cwd_overlay/` existente
2. Calcula o slug do projeto via binding persistente (`conversation_project_binding`)
3. Copia fundindo arquivos com mesmo nome (concatena facts únicos)
4. Remove o diretório de origem (opcional, dry-run first)
5. Cria `MEMORY.md` consolidado no destino

### Projetos sem binding persistente

Para `cwd_overlay` órfãos (sem binding no SQLite), a migração pode:
- Pular o diretório (log avisando)
- Ou criar o slug a partir do nome do diretório pai (fallback heurístico)

---

## Comportamento por Modo

| Modo | CWD ativo? | user_global | topic | cwd_overlay |
|---|---|---|---|---|
| **Chat mode** (sem `/cwd`) | ❌ | ✅ | só se thread>0 | ❌ |
| **Chat mode** (com `/cwd`) | ✅ | ✅ | só se thread>0 | ✅ compartilhado |
| **TUI** (sem `/cwd`) | ❌ | ✅ | ❌ (thread=0) | ❌ |
| **TUI** (com `/cwd`) | ✅ | ✅ | ❌ (thread=0) | ✅ compartilhado |
