# Undefined identifiers in Svelte components fail silently at runtime

**Date**: 2026-10-19
**Change**: desktop-app-render-unblock
**Category**: anti-pattern

## What happened

Pressing Enter in the desktop composer did nothing at all: no message, no
error, no log. `send()` in `App.svelte` called `get(pendingAttachments)` but
the store was never imported from `./lib/stores`. Svelte compiles an unknown
identifier as a plain global reference, so the build was green and the failure
only appeared as a runtime `ReferenceError: Can't find variable:
pendingAttachments` — swallowed by `void submit()`, which returns a promise
nobody awaits.

The symptom was indistinguishable from a broken webview/IPC layer, and it cost
a full diagnostic round to localise.

## How to avoid

- Type-check the Svelte side: `svelte-check` (added as `npm run check` /
  `make desktop-check`). Plain `tsc` does not see `.svelte` files, and the
  Vite build does not fail on undefined identifiers in legacy-mode components.
- Never leave a fire-and-forget promise without a rejection handler when the
  user is waiting on it (`void submit()` swallows errors): attach
  `.catch()` and surface the message.
- Debug a silent UI failure with beacons instead of guesses: a temporary
  `fetch('/probe?…')` from the component to the Vite dev server (whose
  middleware logs every request) reveals which steps ran, and a
  `window.addEventListener("unhandledrejection", …)` beacon names the actual
  exception. Both were decisive here.

## Tags

#lesson #change-desktop-app-render-unblock #anti-pattern #svelte #svelte-check #debugging
