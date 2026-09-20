// Guards the PI SDK surface the bridge depends on.
//
// WHY: a PI SDK bump (see scripts/sync-pi-sdk.sh → make sync-pi-sdk) is only
// safe if the API the bridge imports still exists with the same shape. The
// typecheck catches signature changes; this test catches *removals and moves*
// that `--skipLibCheck` + lazy imports would otherwise turn into a runtime
// failure on the daemon, where it is expensive to diagnose (the bridge dies at
// import time and Telegram goes silent).
//
// Every entry point here is imported by bridge/index.ts. When a bump breaks one
// of these assertions, migrate the bridge — do not delete the assertion.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import {
  createAgentSession,
  DefaultResourceLoader,
  getAgentDir,
  ModelRuntime,
  SessionManager,
  SettingsManager,
} from "@earendil-works/pi-coding-agent";

describe("PI SDK entry-point surface", () => {
  const requiredExports: Record<string, unknown> = {
    createAgentSession,
    DefaultResourceLoader,
    getAgentDir,
    ModelRuntime,
    SessionManager,
    SettingsManager,
  };

  for (const [name, value] of Object.entries(requiredExports)) {
    it(`exports ${name}`, () => {
      assert.equal(typeof value, "function", `${name} is no longer exported as a callable`);
    });
  }
});

describe("PI SDK model runtime surface", () => {
  it("creates offline and exposes the lookup/refresh methods the bridge calls", async () => {
    const agentDir = mkdtempSync(join(tmpdir(), "aurelia-sdk-surface-"));
    try {
      const runtime = await ModelRuntime.create({
        authPath: join(agentDir, "auth.json"),
        modelsPath: join(agentDir, "models.json"),
        modelsStorePath: join(agentDir, "models-store.json"),
        allowModelNetwork: false,
      });
      for (const method of ["getModel", "getModels", "getAvailable", "refresh"] as const) {
        assert.equal(typeof runtime[method], "function", `ModelRuntime.${method} is missing`);
      }
    } finally {
      rmSync(agentDir, { recursive: true, force: true });
    }
  });
});

describe("PI SDK HTTP dispatcher", () => {
  // The bridge reaches this module by path because the package "exports" map
  // does not expose it. Importing the SDK without aligning the dispatcher
  // breaks compressed responses on Node 26 (stale remote model catalog).
  it("resolves core/http-dispatcher.js and exports configureHttpDispatcher", async () => {
    const entry = await import.meta.resolve("@earendil-works/pi-coding-agent");
    const url = pathToFileURL(join(dirname(fileURLToPath(entry)), "core/http-dispatcher.js")).href;
    const mod = (await import(url)) as { configureHttpDispatcher?: unknown };
    assert.equal(typeof mod.configureHttpDispatcher, "function", "configureHttpDispatcher moved or was removed");
  });
});
