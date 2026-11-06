# Aurelia Desktop

> **Status (2026-10-19): operacional.** O bloqueio de renderização foi
> resolvido — a causa raiz era um loop infinito de microtasks na UI
> (autoscroll do `Transcript.svelte`), não o WKWebView/protocolo customizado
> do Tauri. Dev e bundle de debug validados visualmente com o daemon real.
> Detalhes em [BLOCKED.md](BLOCKED.md).

Cliente desktop local para o daemon do Aurelia, construído com **Tauri 2 +
Svelte 5**. O app fala com o daemon existente pelo socket Unix
`~/.aurelia/aurelia.sock`; não há um segundo agente nem uma segunda base de
sessões.

## Arquitetura

```text
Aurelia.app (Tauri)
  ├─ Svelte 5  ── transcript, sidebar, composer, painel de projeto
  └─ Rust      ── cliente IPC, streaming, cancelamento, comandos
        │
        └─ ~/.aurelia/aurelia.sock  (JSON lines)
             └─ daemon Go + bridge TS/PI
```

- `crates/aurelia-ipc/` — cliente Rust independente do Tauri. Tipos do
  protocolo, conexão Unix, leitura de eventos e testes contra um socket falso.
- `src-tauri/` — comandos Tauri e estado do app (uma execução por vez).
- `src/` — UI Svelte.

As sessões são as mesmas da TUI (`~/.aurelia/data/tui_sessions.db`), então uma
conversa começada no terminal continua no app e vice-versa. O DM é o chat
`-9000001`; as sessões nomeadas usam o range reservado da TUI.

## Pré-requisitos

- macOS com Node.js 22+ e Rust 1.77+.
- Daemon do Aurelia em execução (`make deploy` no repositório ou o serviço
  launchd `com.aurelia.agent`).
- O app deve correr como o mesmo utilizador do daemon (autenticação por UID do
  socket).

## Desenvolvimento

```bash
cd desktop
npm install
npm run tauri dev
```

## Atalho de execução

```bash
make install-desktop   # instala o binário e o atalho de terminal
aurelia-desktop        # abre o cliente (terminal liberta-se de imediato)
```

O atalho é escrito em `~/.aurelia/env` ( sourced pelo shell rc, tal como o
`aurelia-tui`): na primeira execução constrói o cliente se ainda não existir
e depois lança-o. A saída da app vai para
`~/.aurelia/logs/aurelia-desktop.log`, por isso o prompt do terminal volta
imediatamente.

`make install-desktop` também copia o bundle `.app` (de um `make desktop-build`
anterior) para `~/Applications`, para o cliente aparecer no Dock, no Launchpad
e no ⌘Tab.

## Verificação estática

```bash
make desktop-check      # ou: cd desktop && npm run check
```

` svelte-check` type-checka os componentes Svelte. Um store usado sem o
respectivo `import` compila sem erro e só falha em runtime (o `send()` atirava
`ReferenceError` e o Enter não fazia nada) — o check apanha isso antes.

## Build

```bash
cd desktop
npm run tauri build
```

O bundle fica em `desktop/target/release/bundle/` (o workspace Cargo na raiz de
`desktop/` define o target dir). Para validar apenas
o binário sem gerar DMG:

```bash
npm run tauri build -- --debug --no-bundle
```

## O que está validado

O crate `crates/aurelia-ipc` fala com o daemon real e foi testado com:

```bash
cd desktop
cargo test -p aurelia-ipc
cargo run -p aurelia-ipc --example probe
```

O probe confirmou lista de sessões compartilhadas com a TUI, `project_state`,
catálogo de modelos, `/status` e histórico.

## O que está implementado

> Validado visualmente no macOS 27 (dev e bundle de debug) em 2026-10-19. A
> investigação do bloqueio anterior está em [BLOCKED.md](BLOCKED.md).

- listar, criar, abrir, renomear e eliminar sessões locais compartilhadas
  com a TUI;
- histórico da sessão PI;
- envio de mensagens com streaming em tempo real;
- markdown e blocos de código;
- cancelamento da resposta atual (fecha a conexão e o daemon aborta o turno);
- `/cwd` (definir/limpar projeto) e painel de estado do projeto;
- catálogo de modelos e troca de modelo (`/model`, `/model auto`);
- `/status`, `/agents`, `/help` e `/reset` via comandos;
- anexos por seletor de ficheiros e drag-and-drop (imagens e documentos).

## Limitações conhecidas

- **Assinatura/distribuição:** os bundles não estão assinados/notarizados; o
  app valida hoje como binário local (`--no-bundle`) e bundle de debug.
- **Sem concorrência:** o daemon tem um guard global de execução da TUI; o app
  também permite apenas uma resposta por vez.
- **Sem background:** fechar o app cancela o turno, porque o pipeline está preso
  à conexão IPC.
- **Sem notificações** e sem sincronização ao vivo com a TUI/Telegram.
- **Limite de sessões:** 8 sessões nomeadas + DM no namespace reservado. O app
  mostra uma mensagem clara quando o limite é atingido.
- **Comandos de texto:** `/status`, `/cwd`, `/agents`, `/mode` e `/help` são
  markdown, não JSON. O app exibe o texto; uma evolução do protocolo pode
  estruturar esses payloads.

## Próximos passos sugeridos

1. UI de renomear/apagar sessões.
2. Endpoints estruturados para status/perfis e cancelamento explícito.
3. Ampliar o namespace de sessões ou criar um namespace `desktop`.
4. Notificações e runs em background (requer mudança no daemon).
5. Notarização e targets de distribuição (`make desktop-build` + assinatura).
