# PI SDK API Validation

**Date:** 2026-07-31
**PI SDK Version:** `@earendil-works/pi-ai` and `@earendil-works/pi-coding-agent` v0.82.1
**Validated by:** installed `dist/*.d.ts`, a no-network `ModelRuntime.create()` runtime probe, and bridge compilation.

## Required runtime

- Node.js `>=22.19.0` is required by the published coding-agent package.
- The bridge package declares that engine and pins `protobufjs` to `7.6.5` through an npm override.
- The build remains ESM and preserves the `createRequire` banner for PI dependencies with dynamic `require()`.

## Model runtime boundary

`ModelRuntime` is the sole bridge source for credentials and model catalog state.

```typescript
const modelRuntime = await ModelRuntime.create({
  authPath: join(agentDir, "auth.json"),
  modelsPath: join(agentDir, "models.json"),
  modelsStorePath: join(agentDir, "models-store.json"),
  allowModelNetwork: false,
});
```

The installed v0.82.1 declarations confirm:

- `ModelRuntime.create()` is asynchronous;
- `getModel(providerId, modelId)` is the qualified lookup;
- `getModels()` is used only for exact-ID fallback;
- `getAvailable()` and `refresh()` are asynchronous;
- `createAgentSession()` accepts `modelRuntime`, not `authStorage` or `modelRegistry`.

Catalog refresh is network-enabled only for an explicit `list-models` request with `refresh=true`. `models-store.json` is a local file in Aurelia's isolated PI-agent directory; `auth.json` and `models.json` remain daemon-managed symlinks to the PI CLI files.

## Session and event boundary

`SessionManager.open()` continues to own JSONL resume. The bridge preserves `session_id`, `session_file`, message timestamps, selected model and thinking level across the supported v0.79.2 fixture. NDJSON event names and fields remain a Go ↔ bridge protocol contract.

Provider failures remain terminal `error` events, never a successful empty `result`: the bridge checks `state.errorMessage`, the final assistant `stopReason`/`errorMessage`, and zero-token/no-work states.

## Tool security boundary

`beforeToolCall` is still not a `createAgentSession` option. After session creation, the bridge wraps `session.agent.beforeToolCall`, evaluates Aurelia policy first, forwards allowed or rewritten calls to PI's original extension hook, and restores that original hook on cleanup. `session.on("tool_call")` is not a PI SDK API and must not be used.
