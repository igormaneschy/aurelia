# [RESOLVIDO] Renderização do app desktop no macOS 27

**Data:** 2026-10-19
**Branch:** `feature/desktop-app`
**Ambiente:** macOS 27.0.1 (build 26A434), Apple Silicon, Rust 1.96.1, Node 26.0.0, Tauri 2.12.4, wry 0.57.0.

## Resolução

**O bloqueio não era o Tauri/WKWebView.** Era um loop infinito de microtasks no
JS da própria UI: o autoscroll do `Transcript.svelte` combinava
`afterUpdate(async () => { await tick(); viewport.scrollTop = … })`. O `tick()`
empurra a escrita do `scrollTop` para depois do flush atual; como `viewport`
(via `bind:this`) e `stickToBottom` são *reactive sources* no Svelte 5 legacy,
a escrita re-invalida o componente e agenda um novo flush — que roda
`afterUpdate` de novo — para sempre. A main thread do processo `WebContent`
fica permanentemente drenando microtasks (~100% CPU) e **nada mais executa**:
módulos pendentes nunca avaliam, `DOMContentLoaded` nunca dispara, `fetch` e
`invoke` nunca resolvem, `eval`/`evaluateJavaScript` nunca rodam. Era isso que
parecia "o WKWebView não renderiza".

### Correção

`desktop/src/lib/components/Transcript.svelte` — autoscroll síncrono dentro do
mesmo ciclo de update, sem `await tick()`:

```svelte
afterUpdate(() => {
  if (stickToBottom && viewport) {
    viewport.scrollTop = viewport.scrollHeight;
  }
});
```

Com a mutação acontecendo *durante* o flush, ela é absorvida (a identidade do
elemento não muda, nenhum effect re-roda) e o ciclo termina. O `await tick()`
do `Composer.svelte` está em handler de submissão (one-shot) e permanece.

### Como foi provado

1. `sample` no processo `WebContent` (PID do app) mostrou 1.286/1.286 amostras
   em `MicrotaskQueue::performMicrotaskCheckpoint → drainWithoutUseCallOnEachMicrotask`
   com frames JIT (`operationEnumeratorNextUpdateAndMode`, alocações, GC).
2. Probe temporário em `src/probe.ts` contava microtasks e fazia
   `fetch('/microtask-beacon?…')` com o stack do agendador a cada 2.000
   agendamentos; o logger do Vite registrou **~18.000 beacons em 12s** com três
   stacks dominantes formando o ping-pong: `flush → afterUpdate (Transcript:54)
   → await tick() → $.mutate(viewport) (Transcript:57) → queue flush → flush → …`.
3. Após a correção: zero beacons, `WebContent` a ~5% CPU, UI montada e
   interativa (validação visual por screenshot da janela, 22:28 de 2026-10-19).
4. O Safari com a mesma página dev ficava a 0% CPU porque sem
   `__TAURI_INTERNALS__` o `boot()` sai cedo e o Transcript nunca populava —
   o loop só ativava no contexto Tauri, o que camuflou o bug como "problema de
   plataforma".

### Efeito colateral documentado

Enquanto o WebContent ficava preso, o app **saía silenciosamente** (exit 0, sem
crash report) alguns minutos depois do spin começar. Com o loop corrigido o app
fica estável (alive > 150s, sem reinícios). Mantemos a nota como referência:
um `WebContent` a 100% de CPU constante em apps wry pode terminar com o
processo do app sem relatório de crash.

## Histórico da investigação (hipótese anterior — incorreta para este caso)

O registro abaixo foi a hipótese de trabalho antes do `sample`. As regressões
WebKit/wry citadas existem de verdade no macOS 26/27 e continuam relevantes
como watch-outs (em especial #1822: responder a um `WKURLSchemeTask` parado
aborta o processo — observamos isso ao vivo quando reloads do Vite atingiram o
app durante o spin), mas **não eram a causa do app não renderizar**.

- Issue wry #1775 — respostas de protocolo customizado bloqueiam a thread via
  `callOnMainRunLoopAndWait` quando fora da main thread (PR aberto #1856).
- Issue wry #1778 — macOS 26.6 bloqueia subrecursos cross-scheme (`asset://`).
- Issue wry #1848 — macOS 26: frames obsoletos/parciais no compositor.
- WebKit bug 318996 — `_doAfterNextPresentationUpdate` pendurado para sempre.

### Sintomas originais (todos explicados pelo loop)

1. HTML/CSS estático renderizava (splash) — nada de JS em loop ainda.
2. JS inline executava — antes do mount disparar IPC.
3. Asset do bundle "pendente" — a main thread do WebContent nunca ficava livre.
4. `invoke`/`listen` nunca resolviam — o contexto JS não consumia respostas.
5. `webview.eval` retornava `Ok(())` sem executar — script enfileirado,
   thread ocupada para sempre.
6. `WebContent` a ~100% CPU com IPC ativo — o próprio loop.
7. Bundle single-file também falhava — o mesmo JS da app rodava inline.

### Ambiente validado

O crate `desktop/crates/aurelia-ipc` fala com o daemon real:

```bash
cd desktop
cargo test -p aurelia-ipc
cargo run -p aurelia-ipc --example probe
```

## Como reproduzir (o sintoma original)

```bash
cd desktop
git stash -- src/lib/components/Transcript.svelte  # volta o afterUpdate com tick
npm install
npm run tauri dev
```

O app abre, o splash aparece e a UI nunca monta; `WebContent` gira a ~100%.
