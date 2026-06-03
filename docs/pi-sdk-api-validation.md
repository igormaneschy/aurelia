# PI SDK API Validation

**Date:** 2026-05-20; updated 2026-05-21
**PI SDK Version:** `@earendil-works/pi-coding-agent` v0.74.0
**Validated by:** Static analysis of installed types + runtime usage in `bridge/index.ts`

---

## Summary

All APIs required by the `delegate-to-pi-sdk` spec are available and match expectations. One API (`beforeToolCall` as a `createAgentSession` option) is **not exposed**. The correct integration is to create the session first, then wrap `session.agent.beforeToolCall`. The older fallback idea (`session.on("tool_call")`) is invalid for the current PI SDK and was removed from the live Bridge.

---

## API-by-API Validation

### 1. `ModelRegistry.find(provider, modelId)`

**Status:** ✅ CONFIRMED

```typescript
// dist/core/model-registry.d.ts
find(provider: string, modelId: string): Model<Api> | undefined;
```

**Evidence:** Already used in `bridge/index.ts:616`:
```typescript
const direct = registry.find(mappedProvider, mappedModel);
```

**Conclusion:** Native resolution works. The custom `resolveModelFromRegistry()` can be simplified to use `registry.find()` + `registry.getAll().find()` as a fallback.

---

### 2. `ModelRegistry.getAll()`

**Status:** ✅ CONFIRMED

```typescript
// dist/core/model-registry.d.ts
getAll(): Model<Api>[];
```

**Evidence:** Already used in `bridge/index.ts:611` and `bridge/index.ts:1101`.

**Conclusion:** Works as expected. Can be used for fallback exact-ID matching.

---

### 3. `ModelRegistry.hasConfiguredAuth(model)`

**Status:** ✅ CONFIRMED

```typescript
// dist/core/model-registry.d.ts
hasConfiguredAuth(model: Model<Api>): boolean;
```

**Evidence:** Already used in `bridge/index.ts:627` and `bridge/index.ts:1103`.

**Conclusion:** Used for filtering available models in `list-models` command.

---

### 4. `SessionManager.create(cwd)`

**Status:** ✅ CONFIRMED

```typescript
// dist/core/session-manager.d.ts
static create(cwd: string, sessionDir?: string): SessionManager;
```

**Evidence:** Already used in `bridge/index.ts:673`.

**Conclusion:** Works as expected for creating new sessions.

---

### 5. `SessionManager.open(path, sessionDir?, cwdOverride?)`

**Status:** ✅ CONFIRMED

```typescript
// dist/core/session-manager.d.ts
static open(path: string, sessionDir?: string, cwdOverride?: string): SessionManager;
```

**Evidence:** Already used in `bridge/index.ts:657`, `661`, `667`.

**Conclusion:** Accepts both file paths and partial IDs. Compatible with the simplified session store that tracks `sessionFile` instead of `sessionID`.

---

### 6. `SessionManager.listAll()`

**Status:** ✅ CONFIRMED

```typescript
// dist/core/session-manager.d.ts (inferred from usage)
```

**Evidence:** Already used in `bridge/index.ts:664`:
```typescript
const sessions = await SessionManager.listAll();
```

**Conclusion:** Returns sessions that can be matched by ID prefix.

---

### 7. `SettingsManager.inMemory({ compaction: { enabled: true } })`

**Status:** ✅ CONFIRMED

```typescript
// dist/core/settings-manager.d.ts
static inMemory(settings?: Partial<Settings>): SettingsManager;

interface CompactionSettings {
    enabled?: boolean;
    reserveTokens?: number;
    keepRecentTokens?: number;
}

interface Settings {
    // ...
    compaction?: CompactionSettings;
    // ...
}
```

**Evidence:** Currently used with `compaction: { enabled: true }` in `bridge/index.ts`.

**Conclusion:** Compaction is enabled and PI SDK is the source of truth for context pruning.

---

### 8. `DefaultResourceLoader` with `noContextFiles: false`

**Status:** ✅ CONFIRMED

```typescript
// dist/core/resource-loader.d.ts
interface DefaultResourceLoaderOptions {
    // ...
    noContextFiles?: boolean;
    // ...
    agentsFilesOverride?: (base: { agentsFiles: Array<{ path: string; content: string }> }) => { agentsFiles: Array<{ path: string; content: string }> };
    // ...
}
```

**Evidence:** Currently used with `noContextFiles: false` in `bridge/index.ts`.

**Conclusion:** PI now auto-discovers `CLAUDE.md` and `AGENTS.md` in the project directory. The `agentsFilesOverride` option remains a future adapter path if Aurelia wants an SDK-native representation of Prompt Profiles while keeping user-facing files under Aurelia-managed paths. Product semantics should remain stable: Aurelia profiles inject personality/context; PI executes.

---

### 9. `beforeToolCall` hook

**Status:** ✅ AVAILABLE ON `session.agent`; ❌ NOT EXPOSED AS `createAgentSession` OPTION

```typescript
// dist/core/sdk.d.ts — CreateAgentSessionOptions
// No beforeToolCall field present.
// Fields: cwd, agentDir, authStorage, modelRegistry, model, thinkingLevel,
//         scopedModels, noTools, tools, customTools, resourceLoader,
//         sessionManager, settingsManager, sessionStartEvent
```

**Evidence:** `CreateAgentSessionOptions` (sdk.d.ts lines 11-55) does not contain `beforeToolCall`.

**Conclusion:** The `beforeToolCall` hook is available on the `Agent` instance behind `session.agent`, but `createAgentSession` does not expose it as a construction option. The Bridge wraps `session.agent.beforeToolCall` after session creation and chains to the original hook so PI extensions still run.

**Impact:** Security hooks stay in the Bridge for now. The Go `internal/security/policy.go` can still be removed because the Bridge is the single source of truth for policy evaluation. Do not use `session.on("tool_call")`; that API does not exist in the current SDK.

---

### 10. `loadProjectContextFiles` export

**Status:** ✅ CONFIRMED

```typescript
// dist/core/resource-loader.d.ts
export declare function loadProjectContextFiles(options: {
    cwd: string;
    agentDir: string;
}): Array<{ path: string; content: string }>;
```

**Evidence:** Exported from `dist/index.d.ts` line 14.

**Conclusion:** Available for direct use if needed, though `DefaultResourceLoader` with `noContextFiles: false` is the preferred approach.

---

## Breaking Changes Check

| API | Version 0.74.0 | Risk |
|-----|----------------|------|
| `ModelRegistry.find()` | Stable, unchanged signature | Low |
| `SessionManager.open()` | Stable, unchanged signature | Low |
| `SettingsManager.inMemory()` | Stable, `compaction` field present | Low |
| `DefaultResourceLoader` | Stable, `noContextFiles` present | Low |
| `createAgentSession` options | No `beforeToolCall` — expected | None (wrap `session.agent.beforeToolCall`) |

**Overall risk:** Low. All APIs are stable and already in use by the Bridge.

---

## Blockers for Delegate-to-PI-SDK

**None.** All required APIs are confirmed. The only "gap" (`beforeToolCall` in `createAgentSession`) is handled by wrapping `session.agent.beforeToolCall` after creation.

---

## Notes

- The PI SDK uses `"latest"` in `bridge/package.json`. At time of validation, the resolved version is `0.74.0`. If PI SDK is updated, re-run this validation.
- `DefaultResourceLoader` auto-discovers context files when `noContextFiles: false`. The discovery logic is internal to PI SDK and not configurable — it looks for `CLAUDE.md`, `AGENTS.md`, and other context files in the project directory.
- `agentsFilesOverride` allows redirecting agent discovery to a custom directory without moving files. This enables the "Option B" migration strategy.
