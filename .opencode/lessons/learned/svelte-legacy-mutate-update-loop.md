# Svelte 5 legacy mode: mutating component state re-schedules the component

**Date**: 2026-10-20
**Change**: desktop-app-render-unblock
**Category**: anti-pattern

## What happened

Switching sessions in the desktop client froze the window on "A carregar
histórico…". The process sample showed the WebContent main thread in an
endless update loop (`performMicrotaskCheckpoint → drainWithoutUseCallOnEachMicrotask`
with a deep recursion), and the daemon answered every request in milliseconds —
so the stall was entirely in the frontend.

Two independent instances of the same trap:

- `bind:this` creates a **reactive source**. Writing a property on the bound
  element (`viewport.scrollTop = …`) compiles to `mutate(source, …)`, and
  `mutate` re-schedules the component. Inside `afterUpdate` that means:
  mutate → re-render → afterUpdate → mutate → … The window never became
  interactive again.
- The throttle bookkeeping in the Markdown renderer was an object literal
  assigned to a component variable. Svelte 5 legacy mode turns it into a
  source, so every `state.pending = value` compiled to `mutate(source, …)` and
  re-ran the reactive statement that called it — an endless loop. A class
  instance does not help: the compiler wraps it the same way.

## How to avoid

- **Copy the element into a local before writing to it**:
  `const node = viewport; node.scrollTop = node.scrollHeight;` — a local is
  plain JavaScript, so the write never touches the reactive graph.
- **Keep mutable bookkeeping in a closure**, not in component scope: locals of
  a nested function are not instrumented by the compiler, so they neither
  invalidate nor are tracked. A factory function returning an object is not
  enough — the compiler still wraps the returned object.
- After any change to a Svelte component, read the **compiled output** and
  look for `mutate(...)` on the values the component writes. Zero mutates on
  hot paths is the invariant that keeps the update loop closed.
- When a UI freeze is suspected, `sample` the WebContent process: a stack in
  `drainWithoutUseCallOnEachMicrotask` with deep recursion means a reactive
  loop, not a slow render.

## Tags

#lesson #change-desktop-app-render-unblock #anti-pattern #svelte #reactivity #performance
