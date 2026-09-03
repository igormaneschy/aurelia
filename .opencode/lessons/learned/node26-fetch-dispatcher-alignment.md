# Node 26 Fetch Poisoning — Bridge Must Align HTTP Dispatcher

## Context
2026-09-03. Telegram/TUI `/model` refresh could never fetch newly launched
remote models (`muse-spark-1.3-contributor` on `opencode-go`): bridge showed
33 models vs 34 in `pi --list-models`, even with forced refresh.

## Root cause
Importing `@earendil-works/pi-coding-agent` mutates process-global HTTP state
in a way that breaks compressed (br/gzip) responses on Node 26: `pi.dev`
catalog fetches return **empty headers + raw compressed bytes**, so
`response.json()` throws `Unexpected token` and the `models-store.json`
overlay goes stale per-provider (error swallowed into `result.errors`, only
`checkedAt` would bump — actually not even that: on parse throw, the provider
entry is not republished at all).

The PI SDK knows about this — `dist/core/http-dispatcher.js` says:
"Node 26.0's bundled fetch can otherwise consume compressed responses
through npm undici's dispatcher without decompressing them, causing
response.json() failures." The PI CLI calls `configureHttpDispatcher()` at
startup; the bridge never did.

## Symptoms (how to recognize)
- `list-models` refresh logs `Unexpected token '�', "�\x13..." is not valid JSON`
  for several providers at once (google, opencode-go, huggingface, nvidia, opencode).
- Response spy shows `status 200` with **zero headers** and a small binary body
  (~1.4KB brotli) instead of JSON.
- `pi --list-models` (CLI) sees models the bridge doesn't.
- Repro: `await import("@earendil-works/pi-coding-agent")` alone is enough to
  break subsequent plain `fetch()` calls in the process (verified with
  `core/sdk.js` import + `example.com` fetch).

## Fix pattern
Call the SDK's own `configureHttpDispatcher()` before serving requests
(`bridge/index.ts`: `ensureHttpDispatcherAligned()`, top-level await before
`main()`). It is NOT exported via the package `exports` map — resolve the
package entry with `import.meta.resolve` and import
`dist/core/http-dispatcher.js` by file URL. Wrap in try/catch (best-effort):
a future SDK rename must not break boot. Pinned in
`TestBridgeModelRuntimeBoundary` via `configureHttpDispatcher` assertion.

Do NOT `require.resolve()` the SDK: its `exports` map has no `require`
condition (`ERR_PACKAGE_PATH_NOT_EXPORTED`); ESM `import.meta.resolve` works.

## Unrelated pre-existing issue seen during diagnosis
`openai-codex` refresh fails with 401 `refresh_token_reused` — the user's
Codex OAuth token needs re-login (`pi auth`). Not a catalog bug.
