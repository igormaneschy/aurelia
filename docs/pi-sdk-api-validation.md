# PI SDK API Validation

**Date:** 2026-09-20
**PI SDK Version:** `@earendil-works/pi-ai` and `@earendil-works/pi-coding-agent` v0.86.0
**Validated by:** installed `dist/*.d.ts` + `tsc --noEmit`, `bridge/sdk-surface.test.ts`
(entry-points, offline `ModelRuntime.create()`, HTTP dispatcher), the bridge test
suite, and the Go boundary tests.

## Keeping the pin current

The pin lives in exactly two files that must agree, and one command moves both:

| File | Role |
|---|---|
| `bridge/package.json` | repo manifest — what npm typechecks, tests and bundles against |
| `internal/bridge/pi_sdk.go` (`piSDKVersion`) | runtime pin — what the daemon installs |

```bash
make sync-pi-sdk        # pin both to the installed PI CLI, then verify
make check-pi-sdk       # report drift only (exit 1 when stale, no writes)
```

`make sync-pi-sdk` reinstalls, typechecks, runs the bridge tests, rebuilds the
bundle and runs the Go bridge tests. It never commits and never touches
`CHANGELOG.md` or `internal/version` — the release bump stays a reviewed step.

Before promoting a bump that changes the package template, also exercise the
*daemon's* install path against the real registry (fresh install, drift repair,
stale-hash rebuild):

```bash
AURELIA_BRIDGE_INSTALL_PROBE=1 go test ./internal/bridge/ \
  -run TestEnsureBridgeInstallProbe -v -timeout 15m
```

Two independent guards keep the pin honest:

1. `TestPiSDKVersionMatchesSourceManifest` (Go) fails if the two declarations
   above disagree. This is the drift that put the daemon on 0.82.1 while the
   repo pinned 0.84.4.
2. `bridge/sdk-surface.test.ts` fails when a bump removes or moves an SDK
   entry point the bridge imports. `skipLibCheck` plus lazy imports would
   otherwise turn that into an import-time crash on the daemon.

The daemon converges on its own: `bridgePackageJSON` embeds `piSDKVersion`, so
a bump changes the template hash and `EnsureBridge` reinstalls `node_modules`
and rebuilds the bundle on the next start. `sdkVersionDrift` additionally
catches any tree whose installed version differs for another reason.

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

The installed v0.86.0 declarations confirm:

- `ModelRuntime.create()` is asynchronous;
- `getModel(providerId, modelId)` is the qualified lookup;
- `getModels()` is used only for exact-ID fallback;
- `getAvailable()` and `refresh()` are asynchronous;
- `createAgentSession()` accepts `modelRuntime`, not `authStorage` or `modelRegistry`.

Catalog refresh is network-enabled only for an explicit `list-models` request with `refresh=true`. `models-store.json` is a local file in Aurelia's isolated PI-agent directory; `auth.json` and `models.json` remain daemon-managed symlinks to the PI CLI files.

## Session and event boundary

`SessionManager.open()` continues to own JSONL resume. The bridge preserves
`session_id`, `session_file`, message timestamps, selected model and thinking
level across sessions written by earlier SDK versions (the 0.79.2 fixture still
opens). NDJSON event names and fields remain a Go ↔ bridge protocol contract;
session files are forward-compatible, and the 0.86.0 changelog lists no session
format break.

Provider failures remain terminal `error` events, never a successful empty
`result`: the bridge checks `state.errorMessage`, the final assistant
`stopReason`/`errorMessage`, and zero-token/no-work states.

## Tool security boundary

`beforeToolCall` is still not a `createAgentSession` option. After session creation, the bridge wraps `session.agent.beforeToolCall`, evaluates Aurelia policy first, forwards allowed or rewritten calls to PI's original extension hook, and restores that original hook on cleanup. `session.on("tool_call")` is not a PI SDK API and must not be used.

## Upgrade notes: 0.84.4 → 0.86.0

0.85.x carried no breaking changes. The three published for 0.86.0 do not cross
the bridge boundary:

- custom-provider stream inputs (`Context` → `TranscriptContext`) — Aurelia
  registers no custom providers;
- `ToolCall.arguments` / `ToolResultMessage.details` restricted to JSON values —
  the bridge already serializes tool input/output as JSON strings;
- `user_bash` failing closed — the bridge installs no `user_bash` handler.

The HTTP dispatcher path (`dist/core/http-dispatcher.js`,
`configureHttpDispatcher`) still resolves; `sdk-surface.test.ts` asserts it.

## Live validation: 2026-09-20

Daemon `v0.47.0` + `feature/pi-sdk-0-86-sync`, deployed by the post-commit hook.
On the first start `EnsureBridge` logged the drift for both packages
(`installed=0.82.1 pinned=0.86.0`), dropped npm's hidden lockfile, reinstalled
and rebuilt the bundle from source in ~6s. The next restart changed nothing, so
the repair converges and is idempotent.

| Check | Evidence |
|---|---|
| Telegram query → reply | run `18829387`, `status=completed`, `entrypoint=telegram` |
| Session resume | run `a7e81ceb` resumed `01a0be7a-…jsonl`, `session_lifecycle state=healthy action=continue`, 64 365 input tokens (history restored) |
| Tool calls + security audit | `mcp` and `memory_status` executed; `[security] decision=allow` logged for both |
| Extensions/MCP + ai-memory | ai-memory answered 221 latest pages / 1717 versions / 422 sessions / 186 804 observations |
| Model catalog | bridge `list-models` = 247 models, `pi --list-models` = 247 (parity) |
| Telegram `/model` (network refresh) | `models-store.json` rewritten at 12:08:41 local (the moment of the command) — 645 models across 11 providers, per-provider `checkedAt`/`etag`, `muse-spark` present on `opencode-go`. Proves `ModelRuntime.refresh({allowNetwork: true, force: true})` completed under Node 26 with the HTTP dispatcher aligned |
| Errors | `aurelia debug errors` → none; no import failure, panic, "model not found in PI registry", `list-models` refresh error or empty-catalog warning after the deploy |
| Prefill telemetry | `provider wait … after 30s` then `provider first chunk after 40s` — no false stall/steer on the local model |
| Daemon SDK version | `~/.aurelia/bridge/node_modules/@earendil-works/pi-coding-agent/package.json` = `0.86.0` |

All checklist items were exercised on the deployed daemon. The catalog refresh
row is the strongest single signal: it requires the SDK import, auth resolution,
network fetch with compressed responses, the model store overlay, and the
extension/MCP surface to all work together.
