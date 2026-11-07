# Aurelia Desktop

> **Status (2026-11-07): operacional no Mac contra o daemon do NAS.** O modo
> pessoal atual usa **desktop + Telegram**; o cliente TUI está desativado. O
> desktop usa o namespace de sessões da TUI no NAS (`tui_sessions.db`), que
> deve ser preservado. O histórico do bloqueio de renderização de 2026-10-19
> está em [BLOCKED.md](BLOCKED.md).

Cliente desktop para o daemon do Aurelia, construído com **Tauri 2 + Svelte 5**.
O app fala com o daemon pelo socket Unix `~/.aurelia/aurelia.sock` (daemon
local) ou por `~/.aurelia/nas.sock` quando o túnel para o NAS está instalado;
não há um segundo agente nem uma segunda base de sessões.

## Arquitetura

```text
Aurelia.app (Tauri)
  ├─ Svelte 5  ── transcript, sidebar, composer, painel de projeto
  └─ Rust      ── cliente IPC, streaming, cancelamento, comandos
        │
        ├─ ~/.aurelia/aurelia.sock       (JSON lines, daemon local)
        │    └─ daemon Go + bridge TS/PI
        │
        └─ ~/.aurelia/nas.sock           (modo NAS, ver abaixo)
             └─ SSH streamlocal sobre Tailscale
                  └─ /home/<nas-user>/.aurelia/aurelia.sock no NAS
```

- `crates/aurelia-ipc/` — cliente Rust independente do Tauri. Tipos do
  protocolo, conexão Unix, leitura de eventos e testes contra um socket falso.
- `src-tauri/` — comandos Tauri e estado do app (uma execução por vez).
- `src/` — UI Svelte.

As sessões usam o namespace da TUI no daemon (`~/.aurelia/data/tui_sessions.db`
no NAS). O cliente TUI está desativado no modo pessoal, mas o banco é
compartilhado pelo desktop e **não deve ser apagado**. O DM é o chat
`-9000001`; as sessões nomeadas usam o range reservado da TUI.

## Pré-requisitos

- macOS com Node.js 22+ e Rust 1.77+.
- Um daemon do Aurelia: local (`make deploy` / launchd `com.aurelia.agent`) ou
  no NAS, alcançado pelo túnel descrito em **Modo NAS (Tailscale)**.
- No modo local, o app deve correr como o mesmo utilizador do daemon
  (autenticação por UID do socket). No modo NAS, o `user_id` é resolvido pelo
  daemon a partir da persona canónica.

## Modo NAS (Tailscale)

O cliente no Mac pode falar com o daemon no NAS sem abrir portas: um túnel SSH
de socket Unix sobre Tailscale expõe o socket remoto em `~/.aurelia/nas.sock`.
Funciona na rede de casa e fora dela, desde que o Tailscale esteja ligado.

No NAS, o `sshd` precisa de permitir forwarding local de sockets Unix (o
`/etc/ssh/sshd_config` do UGREEN vinha com `AllowTcpForwarding no`):

```text
AllowTcpForwarding local
AllowStreamLocalForwarding yes
```

No Mac, com a chave SSH já carregada na keychain (`ssh-add
--apple-use-keychain`):

```bash
AURELIA_NAS_HOST=<nas-host> AURELIA_NAS_USER=<nas-user> \
  make install-macos-nas-tunnel
```

Os defaults do repositório são genéricos (`aurelia-nas` / `aurelia`); o
instalador grava os valores reais em `~/.aurelia/nas-tunnel.env`. O instalador
cria `~/Library/LaunchAgents/com.aurelia.nas-tunnel.plist`, o wrapper
`~/.aurelia/bin/aurelia-nas-tunnel`, o marcador
`~/.aurelia/nas-tunnel.enabled` e o socket `~/.aurelia/nas.sock`. O launchd
mantém o túnel vivo e reconecta depois de sleep ou mudança de rede. Logs:
`~/.aurelia/logs/nas-tunnel.log`.

O app escolhe `nas.sock` quando o marcador existe; sem o marcador usa
`aurelia.sock` (daemon local). Para remover o modo NAS:

```bash
make uninstall-macos-nas-tunnel
```

O túnel usa a tua chave SSH existente via keychain; não cria nem copia chaves.
Se o daemon local estiver a correr, o wrapper recusa substituir o socket local
e regista o motivo no log; nesse caso remove o túnel antes de voltar ao daemon
local. `AURELIA_IPC_SOCKET=/caminho/do/socket` força um socket explícito
(independentemente do marcador). O app envia `user_id=0` e o daemon resolve a
persona canónica; `AURELIA_IPC_USER_ID` permite sobrepor a identidade em
configurações locais específicas.

### Memória entre desktop e Telegram

Uma sessão nova do desktop apontada para o cwd de um tópico do Telegram é uma
**conversa nova no mesmo projeto**, não uma continuação do thread. Ela
compartilha cwd, arquivos e o projeto ai-memory definido pelo
`.ai-memory.toml`. Não compartilha o session file/transcript, o binding de
`/cwd`, a lista de sessões nem o modelo. Novas sessões não herdam o cwd do DM;
use `/cwd` em cada uma. Em `ai-memory` mode, `/memory` mostra o projeto, páginas,
observações, sessões e handoffs pendentes; `/memory checkpoint` fica desativado
e as notas duráveis passam por páginas/handoff do ai-memory. Evite editar os
mesmos arquivos nos dois lados ao mesmo tempo; não há lock entre superfícies.

> **Memory consolidation rollout:** o daemon live usa
> `memory_provider=ai-memory` desde 2026-11-07. O painel do projeto mostra o
> projeto/páginas/handoffs do ai-memory em vez das camadas de ficheiro, e o
> prompt recebe um working set ai-memory limitado (3.126 caracteres no primeiro
> turno live). As 23 páginas legadas importadas têm a tag `legacy-import`.
> `cwd_overlay`/`topic`/`user_global` e continuity só são usados em
> `memory_provider=legacy` (rollback). `shadow` está reservado para a comparação
> pendente e, até ser implementado, mantém o prompt legacy.

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

## Troubleshooting (modo NAS)

```bash
launchctl print gui/$(id -u)/com.aurelia.nas-tunnel | sed -n '1,40p'
ls -l ~/.aurelia/nas.sock          # srw-------
tail -f ~/.aurelia/logs/nas-tunnel.log
```

- **App offline:** confirme que o tunnel está `running` e que
  `~/.aurelia/nas.sock` existe; depois use o refresh do app. O app escolhe
  `nas.sock` pelo marcador, não pelo estado momentâneo do socket.
- **SSH falha:** confirme Tailscale, o host em `~/.aurelia/nas-tunnel.env`, a
  chave na keychain (`ssh-add --apple-use-keychain`) e o host key em
  `~/.ssh/known_hosts`.
- **Socket local em uso:** se um daemon local `aurelia` estiver a correr, o
  wrapper recusa substituir o socket; remova o túnel antes de voltar ao modo
  local.
- **Logs do app:** `~/.aurelia/logs/aurelia-desktop.log` (atalho) ou o terminal
  de onde o binário foi lançado.

## O que está validado

O crate `crates/aurelia-ipc` fala com o daemon real e foi testado com:

```bash
cd desktop
cargo test -p aurelia-ipc
cargo run -p aurelia-ipc --example probe
```

O probe confirmou lista de sessões no namespace da TUI, `project_state`,
catálogo de modelos, `/status` e histórico.

Em 2026-11-07, no modo NAS:

- `cargo test -p aurelia-ipc`: 5/5;
- `cargo test -p aurelia-desktop --lib`: 1/1;
- build Tauri debug concluído e instalado em `~/.aurelia/bin/aurelia-desktop`;
- app online contra o NAS, com o projeto `Aurelia` e o DM no hub pessoal;
- túnel reconectado em ~1s após matar o `ssh`, e IPC `sessions`, `history` e
  `project_state` validado com `user_id=0`; `user_id=501` rejeitado (peer UID
  1000) sem alterar a autenticação do daemon.

## O que está implementado

> Validado visualmente no macOS 27 (dev e bundle de debug) em 2026-10-19. A
> investigação do bloqueio anterior está em [BLOCKED.md](BLOCKED.md).

- listar, criar, abrir, renomear e eliminar sessões locais no namespace da TUI
  (o cliente TUI está desativado; o banco é do desktop);
- histórico da sessão PI;
- envio de mensagens com streaming em tempo real;
- markdown e blocos de código;
- cancelamento da resposta atual (fecha a conexão e o daemon aborta o turno);
- `/cwd` (definir/limpar projeto) e painel de estado do projeto;
- catálogo de modelos e troca de modelo (`/model`, `/model auto`);
- `/status`, `/agents`, `/help` e `/reset` via comandos;
- anexos por seletor de ficheiros e drag-and-drop (imagens e documentos) no
  modo local; no modo NAS os caminhos dos ficheiros são do Mac e o daemon no
  NAS não os consegue ler (ver limitações).

## Limitações conhecidas

- **Assinatura/distribuição:** os bundles não estão assinados/notarizados; o
  app valida hoje como binário local (`--no-bundle`) e bundle de debug.
- **Sem concorrência:** o daemon tem um guard global de execução da TUI; o app
  também permite apenas uma resposta por vez.
- **Sem background:** fechar o app cancela o turno, porque o pipeline está preso
  à conexão IPC.
- **Sem notificações** e sem sincronização ao vivo com o Telegram.
- **TUI desativado:** o cliente TUI não é usado no modo pessoal, mas o
  namespace de sessões (`tui_sessions.db`, `-9000001…`) é do desktop e deve ser
  preservado.
- **Camada `topic` do Telegram:** não é carregada no desktop (thread 0); use
  `cwd_overlay`/ai-memory para memória entre superfícies.
- **Modelo:** o badge do app é uma seleção do cliente; o modelo do daemon pode
  diferir. Use `/model` para alinhar.
- **Anexos no modo NAS:** o protocolo atual envia caminhos locais; o daemon
  remoto lê os caminhos no NAS, não no Mac. Texto e comandos funcionam; para
  imagens/documentos, usar o Telegram ou aguardar o upload de bytes.
- **Limite de sessões:** 8 sessões nomeadas + DM no namespace reservado. O app
  mostra uma mensagem clara quando o limite é atingido.
- **Comandos de texto:** `/status`, `/cwd`, `/agents`, `/mode` e `/help` são
  markdown, não JSON. O app exibe o texto; uma evolução do protocolo pode
  estruturar esses payloads.

## Próximos passos sugeridos

1. Upload de bytes para imagens/documentos no modo NAS (hoje o protocolo envia
   caminhos locais do Mac).
2. UI de renomear/apagar sessões.
3. Endpoints estruturados para status/perfis e cancelamento explícito.
4. Ampliar o namespace de sessões ou criar um namespace `desktop`.
5. Notificações e runs em background (requer mudança no daemon).
6. Notarização e targets de distribuição (`make desktop-build` + assinatura).
