# PI SDK: uma versão pinada, um único caminho de instalação

**Date**: 2026-09-20
**Change**: pi-sdk-0-86-sync
**Category**: anti-pattern

## What happened

O PI SDK era declarado em dois lugares que ninguém comparava:

- `bridge/package.json` — o manifest que o repo instala, typechecka e builda;
- `internal/bridge/setup.go` (`bridgePackageJSON`) — o template que o daemon
  grava em `~/.aurelia/bridge/` e instala com npm.

Os dois divergiram: o repo foi para `0.84.4` enquanto o template continuou em
`0.82.1`. Pior: `EnsureBridge` só rodava `npm install` quando o diretório
`node_modules` **não existia**. Como ele existia (de um setup antigo), nenhum
bump de pin jamais chegava ao daemon — que seguiu rodando `0.82.1` por
semanas, silenciosamente, enquanto o PI CLI na mesma máquina estava em
`0.86.0`.

## How to avoid

1. **Um valor, não dois.** O pin do runtime vem de uma constante única
   (`piSDKVersion` em `internal/bridge/pi_sdk.go`) e o template do daemon é
   montado a partir dela. Um teste compara a constante com
   `bridge/package.json` e falha no primeiro sinal de divergência
   (`TestPiSDKVersionMatchesSourceManifest`).
2. **Existência de diretório não é versão instalada.** Checar
   `node_modules` existe e parar por aí transforma "instalado alguma vez" em
   "instalado na versão certa". Leia a versão real do manifest instalado e
   reinstale quando divergir.
3. **`npm install` confia no hidden lockfile.** Com
   `node_modules/.package-lock.json` presente, o npm considera a árvore correta
   sem relê-la — uma instalação interrompida fica invisível. No caminho de
   reparo, remova esse arquivo para forçar a reverificação contra o
   `package.json`.
4. **Falha de API tem que aparecer antes do deploy.** `skipLibCheck` + imports
   lazy transformam a remoção de um entry point em crash na hora do import, no
   daemon. `bridge/sdk-surface.test.ts` afirma os símbolos que o bridge usa e o
   caminho `core/http-dispatcher.js`.

Automação correspondente: `make sync-pi-sdk` (move os dois pins para a versão
do PI CLI e roda install, typecheck, testes do bridge, rebuild do bundle e
testes Go) e `make check-pi-sdk` (reporta drift sem escrever nada).

## Tags

#lesson #change-pi-sdk-0-86-sync #anti-pattern #pi-sdk #bridge #versioning
