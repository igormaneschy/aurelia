import { describe, it, mock } from "node:test";
import assert from "node:assert";
import { join } from "node:path";
import { SessionManager } from "@earendil-works/pi-coding-agent";
import {
  installSecurityHook,
  resolveModel,
  SecurityContext,
  waitForPendingMessageCount,
} from "./index.ts";

// ── waitForPendingMessageCount ──────────────────────────────────────────────

describe("waitForPendingMessageCount", () => {
  it("returns immediately when pending count is already 0", async () => {
    const session = { pendingMessageCount: 0 };
    const start = Date.now();
    await waitForPendingMessageCount(session, 5000);
    const elapsed = Date.now() - start;
    assert.ok(elapsed < 100, `expected fast return, got ${elapsed}ms`);
  });

  it("waits for pending count to drop to 0", async () => {
    const session = { pendingMessageCount: 1 };
    // Schedule pendingMessageCount to drop after a short delay
    setTimeout(() => { session.pendingMessageCount = 0; }, 30);
    const start = Date.now();
    await waitForPendingMessageCount(session, 5000);
    const elapsed = Date.now() - start;
    assert.ok(elapsed >= 20, `expected to wait, got ${elapsed}ms`);
    assert.ok(elapsed < 1000, `expected completion within 1s, got ${elapsed}ms`);
  });

  it("times out when pending count stays above 0", async () => {
    const session = { pendingMessageCount: 1 };
    const start = Date.now();
    await waitForPendingMessageCount(session, 50);
    const elapsed = Date.now() - start;
    assert.ok(elapsed >= 40, `expected near-timeout, got ${elapsed}ms`);
    // Should not exceed timeout by too much (50ms + poll overhead)
    assert.ok(elapsed < 500, `expected timeout-bound, got ${elapsed}ms`);
  });
});

describe("PI 0.82.1 model runtime boundary", () => {
  it("uses qualified lookup first, then only an exact ID fallback", () => {
    const direct = { provider: "kimi-coding", id: "kimi-for-coding" };
    const fallback = { provider: "other", id: "kimi-for-coding" };
    const runtime = {
      getModel(provider: string, model: string) {
        return provider === "kimi-coding" && model === "kimi-for-coding" ? direct : undefined;
      },
      getModels() {
        return [fallback, { provider: "other", id: "kimi-for-coding-preview" }];
      },
    } as Parameters<typeof resolveModel>[0];

    assert.strictEqual(resolveModel(runtime, "kimi", "k2.5"), direct);
    assert.strictEqual(resolveModel(runtime, undefined, "kimi-for-coding"), fallback);
    assert.strictEqual(resolveModel(runtime, undefined, "kimi-for-cod"), undefined);
  });
});

describe("PI 0.79.2 JSONL compatibility", () => {
  it("opens the captured 0.79.2 session without losing identity, messages, model, or thinking", () => {
    const fixture = join(process.cwd(), "testdata", "pi-0.79.2-session.jsonl");
    const session = SessionManager.open(fixture, undefined, "/workspace/legacy");
    const context = session.buildSessionContext();

    assert.strictEqual(session.getSessionId(), "legacy-079-session");
    assert.strictEqual(session.getSessionFile(), fixture);
    assert.strictEqual(context.messages.length, 2);
    assert.strictEqual(context.messages[0].timestamp, 1710000000000);
    assert.strictEqual(context.messages[1].timestamp, 1710000001000);
    assert.deepStrictEqual(context.model, {
      provider: "anthropic",
      modelId: "claude-sonnet-4-5",
    });
    assert.strictEqual(context.thinkingLevel, "high");
  });
});

describe("security beforeToolCall lifecycle", () => {
  const security: SecurityContext = {
    enabled: true,
    profile: "execute_safe",
    mode: "block",
    cwd: "/workspace/legacy",
  };

  it("chains allowed calls, blocks before extensions, and restores the original hook", async () => {
    type HookAgent = Parameters<typeof installSecurityHook>[0];
    let extensionCalls = 0;
    const original: NonNullable<HookAgent["beforeToolCall"]> = async () => {
      extensionCalls += 1;
      return undefined;
    };
    const agent: HookAgent = { beforeToolCall: original };
    const restore = installSecurityHook(agent, security, () => {});
    const hook = agent.beforeToolCall!;
    const signal = new AbortController().signal;

    await hook(
      { toolCall: { name: "Bash" }, args: { command: "go test ./..." } } as Parameters<typeof hook>[0],
      signal,
    );
    assert.strictEqual(extensionCalls, 1);

    const blocked = await hook(
      { toolCall: { name: "Bash" }, args: { command: "whoami" } } as Parameters<typeof hook>[0],
      signal,
    );
    assert.deepStrictEqual(blocked, { block: true, reason: "command not on allowlist: whoami" });
    assert.strictEqual(extensionCalls, 1);

    restore();
    assert.strictEqual(agent.beforeToolCall, original);
  });
});
