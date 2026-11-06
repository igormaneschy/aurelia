# Autoscroll hooks must not re-enter the update pipeline

**Date**: 2026-10-19
**Change**: desktop-app-render-unblock
**Category**: anti-pattern

## What happened

The desktop app (Tauri 2 + Svelte 5) built fine but the UI never painted on
macOS 27.0.1. `WebContent` spun at ~100% CPU and every symptom pointed at the
webview platform layer: module scripts stayed pending, `DOMContentLoaded` never
fired, `fetch`/`invoke` never resolved, and `eval`/`evaluateJavaScript`
returned success without executing. The documented hypothesis blamed the
Tauri/wry custom protocol (wkwebview), and the recommended next step was a
full UI rewrite (egui/iced).

Sampling the `WebContent` process (`sample <pid> 3`) showed the real picture:
1.286/1.286 samples inside `JSC::MicrotaskQueue::drainWithoutUseCallOnEachMicrotask`
with JIT frames doing `for...in` enumeration and allocations — an infinite
microtask loop, not a stuck scheme handler. A probe hooking `queueMicrotask`
and beaconning scheduling stacks to the Vite dev server (~18k beacons in 12s)
named the exact cycle: `flush → afterUpdate (Transcript.svelte) → await
tick() → $.mutate(viewport, scrollTop = …) → queue flush → flush → …`.

The autoscroll wrote the bound element one hop too late (`await tick()`),
guaranteeing a new full update cycle every time; because `bind:this`/legacy
`let` are reactive mutable sources in Svelte 5, the DOM write re-invalidated
the component forever. Safari with the same dev page idled at 0% CPU only
because without `__TAURI_INTERNALS__` the app's boot fails before the
transcript ever populates — the loop was reachable only inside Tauri, which
camouflaged the bug as a platform regression.

## How to avoid

- Autoscroll (and any DOM write to a bound element) runs synchronously inside
  the same update cycle: `afterUpdate(() => { viewport.scrollTop = … })` — no
  `await tick()` inside `afterUpdate`/`onMount` callbacks.
- Before pivoting a stack for "platform bug" symptoms, sample the actual
  process: `sample <pid> 3` on the WebContent XPC process names the real
  hot path in seconds.
- A wedged webview (permanent microtask drain) can also make the whole app
  exit silently (exit 0, no crash report); treat 100%-CPU WebContent as a
  correctness bug, not a perf annoyance.

## Tags

#lesson #change-desktop-app-render-unblock #anti-pattern #svelte #tauri #wkwebview #autoscroll
