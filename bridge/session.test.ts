import { describe, it, mock } from "node:test";
import assert from "node:assert";
import { join } from "node:path";
import { SessionManager } from "@earendil-works/pi-coding-agent";
import {
  bindBridgeSessionExtensions,
  boundSessionHistoryPayload,
  createBridgeSessionRequestLifecycle,
  disposeBridgeSession,
  installSecurityHook,
  removeSessionIfOwner,
  registerSessionIfOwner,
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

describe("bridge extension lifecycle binding", () => {
  it("runs an adapter-style session_start handler before the first prompt", async () => {
    const order: string[] = [];
    let adapterInitialized = false;
    let bindingCalls = 0;
    const sessionStartHandlers = [async () => {
      order.push("session_start");
      adapterInitialized = true;
    }];
    const session = {
      async bindExtensions(bindings: { mode: "print" }) {
        bindingCalls += 1;
        assert.deepStrictEqual(bindings, { mode: "print" });
        order.push("bind");
        for (const handler of sessionStartHandlers) await handler();
      },
      extensionRunner: { async emit() {} },
      dispose() {},
      async prompt(_text: string) {
        assert.strictEqual(adapterInitialized, true, "session_start must initialize the adapter before prompt");
        order.push("prompt");
      },
    };

    await bindBridgeSessionExtensions(session);
    await session.prompt("first turn");

    assert.strictEqual(bindingCalls, 1);
    assert.deepStrictEqual(order, ["bind", "session_start", "prompt"]);
  });

  it("propagates extension initialization failures", async () => {
    const failure = new Error("MCP initialization failed");
    const session = {
      async bindExtensions(_bindings: { mode: "print" }): Promise<void> {
        throw failure;
      },
      extensionRunner: { async emit() {} },
      dispose() {},
    };

    await assert.rejects(bindBridgeSessionExtensions(session), failure);
  });

  it("awaits session_shutdown before disposing a bound session", async () => {
    const order: string[] = [];
    let releaseShutdown!: () => void;
    let markShutdownStarted!: () => void;
    const shutdownStarted = new Promise<void>((resolve) => { markShutdownStarted = resolve; });
    const shutdownGate = new Promise<void>((resolve) => { releaseShutdown = resolve; });
    let disposeCalls = 0;
    const session = {
      async bindExtensions(_bindings: { mode: "print" }): Promise<void> {
        order.push("session_start");
      },
      extensionRunner: {
        async emit(event: { type: "session_shutdown"; reason: "quit" }): Promise<void> {
          assert.deepStrictEqual(event, { type: "session_shutdown", reason: "quit" });
          order.push("session_shutdown:start");
          markShutdownStarted();
          await shutdownGate;
          order.push("session_shutdown:end");
        },
      },
      dispose(): void {
        disposeCalls += 1;
        order.push("dispose");
      },
    };

    await bindBridgeSessionExtensions(session);
    const teardown = disposeBridgeSession(session);
    await shutdownStarted;

    assert.strictEqual(disposeCalls, 0, "dispose must wait for the async shutdown event");
    releaseShutdown();
    await teardown;
    assert.deepStrictEqual(order, [
      "session_start",
      "session_shutdown:start",
      "session_shutdown:end",
      "dispose",
    ]);
  });

  it("shares one awaited teardown across repeated calls", async () => {
    let shutdownCalls = 0;
    let disposeCalls = 0;
    const session = {
      async bindExtensions(_bindings: { mode: "print" }): Promise<void> {},
      extensionRunner: {
        async emit(_event: { type: "session_shutdown"; reason: "quit" }): Promise<void> {
          shutdownCalls += 1;
        },
      },
      dispose(): void {
        disposeCalls += 1;
      },
    };

    const lifecycle = createBridgeSessionRequestLifecycle();
    await lifecycle.createSession(async () => ({ session }));
    await lifecycle.bindSession(session);
    await Promise.all([lifecycle.cancel(), lifecycle.cancel(), lifecycle.cancel()]);

    assert.strictEqual(shutdownCalls, 1);
    assert.strictEqual(disposeCalls, 1);
  });

  it("cleans up a partially bound session while preserving the bind error", async () => {
    const failure = new Error("MCP initialization failed after session_start");
    const order: string[] = [];
    let disposeCalls = 0;
    const session = {
      async bindExtensions(_bindings: { mode: "print" }): Promise<void> {
        order.push("session_start");
        throw failure;
      },
      extensionRunner: {
        async emit(event: { type: "session_shutdown"; reason: "quit" }): Promise<void> {
          assert.deepStrictEqual(event, { type: "session_shutdown", reason: "quit" });
          order.push("session_shutdown");
        },
      },
      dispose(): void {
        disposeCalls += 1;
        order.push("dispose");
      },
    };

    const lifecycle = createBridgeSessionRequestLifecycle();
    await lifecycle.createSession(async () => ({ session }));
    await assert.rejects(lifecycle.bindSession(session), failure);
    await lifecycle.cancel();

    assert.deepStrictEqual(order, ["session_start", "session_shutdown", "dispose"]);
    assert.strictEqual(disposeCalls, 1);
  });

  it("shuts down a created extension runtime canceled before binding starts", async () => {
    const order: string[] = [];
    let bindCalls = 0;
    let disposeCalls = 0;
    const session = {
      async bindExtensions(_bindings: { mode: "print" }): Promise<void> {
        bindCalls += 1;
      },
      extensionRunner: {
        async emit(event: { type: "session_shutdown"; reason: "quit" }): Promise<void> {
          assert.deepStrictEqual(event, { type: "session_shutdown", reason: "quit" });
          order.push("session_shutdown");
        },
      },
      dispose(): void {
        disposeCalls += 1;
        order.push("dispose");
      },
    };

    const lifecycle = createBridgeSessionRequestLifecycle();
    await lifecycle.createSession(async () => ({ session }));
    await lifecycle.cancel();

    assert.strictEqual(bindCalls, 0, "pre-bind cancellation must not activate the session");
    assert.deepStrictEqual(order, ["session_shutdown", "dispose"]);
    assert.strictEqual(disposeCalls, 1);

    await assert.rejects(lifecycle.bindSession(session), /request canceled/);
    assert.strictEqual(bindCalls, 0, "a canceled request must never start a late bind");
    assert.deepStrictEqual(order, ["session_shutdown", "dispose"]);
  });

  it("resolves cancellation when the non-cancelable session factory rejects", async () => {
    const failure = new Error("provider setup failed");
    let releaseFactory!: () => void;
    const factoryGate = new Promise<void>((resolve) => { releaseFactory = resolve; });
    const lifecycle = createBridgeSessionRequestLifecycle();

    const creation = lifecycle.createSession(async () => {
      await factoryGate;
      throw failure;
    });
    const cancellation = lifecycle.cancel();

    let cancellationSettled = false;
    const observedCancellation = cancellation.then(() => { cancellationSettled = true; });
    await Promise.resolve();
    assert.strictEqual(cancellationSettled, false, "cancel must wait for the factory outcome");

    const observedCreation = assert.rejects(creation, (err: unknown) => err === failure);
    releaseFactory();
    await observedCreation;
    await observedCancellation;
    assert.strictEqual(cancellationSettled, true);
  });

  it("waits for a gated bind before exact-session cancellation cleanup", async () => {
    const order: string[] = [];
    let releaseBind!: () => void;
    let markBindStarted!: () => void;
    const bindStarted = new Promise<void>((resolve) => { markBindStarted = resolve; });
    const bindGate = new Promise<void>((resolve) => { releaseBind = resolve; });
    let disposeCalls = 0;
    const session = {
      async bindExtensions(_bindings: { mode: "print" }): Promise<void> {
        order.push("bind:start");
        markBindStarted();
        await bindGate;
        order.push("bind:end");
      },
      extensionRunner: {
        async emit(_event: { type: "session_shutdown"; reason: "quit" }): Promise<void> {
          order.push("session_shutdown");
        },
      },
      dispose(): void {
        disposeCalls += 1;
        order.push("dispose");
      },
    };

    const lifecycle = createBridgeSessionRequestLifecycle();
    await lifecycle.createSession(async () => ({ session }));
    const bind = lifecycle.bindSession(session);
    await bindStarted;

    let cancelSettled = false;
    const cancel = lifecycle.cancel().then(() => { cancelSettled = true; });
    assert.strictEqual(cancelSettled, false, "cancellation must remain pending while bind is gated");
    assert.strictEqual(disposeCalls, 0, "session must not dispose before bind settles");

    releaseBind();
    await Promise.all([bind, cancel]);

    assert.strictEqual(cancelSettled, true);
    assert.deepStrictEqual(order, ["bind:start", "bind:end", "session_shutdown", "dispose"]);
    assert.strictEqual(disposeCalls, 1);
  });

  it("keeps a replacement registered while cancellation settles a stale gated bind", async () => {
    const key = "chat:thread:user";
    const owners = new Map<string, symbol>();
    const sessions = new Map<string, object>();
    const staleOwner = Symbol("stale");
    const newerOwner = Symbol("newer");
    owners.set(key, staleOwner);

    let releaseBind!: () => void;
    let markBindStarted!: () => void;
    const bindStarted = new Promise<void>((resolve) => { markBindStarted = resolve; });
    const bindGate = new Promise<void>((resolve) => { releaseBind = resolve; });
    let staleDisposeCalls = 0;
    let newerDisposeCalls = 0;

    const staleSession = {
      async bindExtensions(_bindings: { mode: "print" }): Promise<void> {
        markBindStarted();
        await bindGate;
      },
      extensionRunner: { async emit() {} },
      dispose(): void { staleDisposeCalls += 1; },
    };
    const newerSession = {
      async bindExtensions(_bindings: { mode: "print" }): Promise<void> {},
      extensionRunner: { async emit() {} },
      dispose(): void { newerDisposeCalls += 1; },
    };

    const staleLifecycle = createBridgeSessionRequestLifecycle();
    await staleLifecycle.createSession(async () => ({ session: staleSession }));
    const staleBind = staleLifecycle.bindSession(staleSession);
    await bindStarted;

    owners.set(key, newerOwner);
    const newerLifecycle = createBridgeSessionRequestLifecycle();
    await newerLifecycle.createSession(async () => ({ session: newerSession }));
    await newerLifecycle.bindSession(newerSession);
    assert.strictEqual(registerSessionIfOwner(owners, sessions, key, newerOwner, newerSession), true);

    let cancelSettled = false;
    const staleCancel = staleLifecycle.cancel().then(() => { cancelSettled = true; });
    assert.strictEqual(cancelSettled, false);
    assert.strictEqual(sessions.get(key), newerSession);

    releaseBind();
    await Promise.all([staleBind, staleCancel]);

    // This is the publication step used by handleQuery after its awaited
    // bind. A canceled stale request must fail it even after the gate opens.
    assert.strictEqual(registerSessionIfOwner(owners, sessions, key, staleOwner, staleSession), false);
    assert.strictEqual(sessions.get(key), newerSession);
    assert.strictEqual(staleDisposeCalls, 1);
    assert.strictEqual(newerDisposeCalls, 0);

    await disposeBridgeSession(newerSession);
    assert.strictEqual(newerDisposeCalls, 1);
  });

  it("does not publish a stale gated bind over a newer chat session", async () => {
    const key = "chat:thread:user";
    const owners = new Map<string, symbol>();
    const sessions = new Map<string, object>();
    const staleOwner = Symbol("stale");
    const newerOwner = Symbol("newer");
    owners.set(key, staleOwner);

    let releaseBind!: () => void;
    let markBindStarted!: () => void;
    const bindStarted = new Promise<void>((resolve) => { markBindStarted = resolve; });
    const bindGate = new Promise<void>((resolve) => { releaseBind = resolve; });
    let staleDisposeCalls = 0;
    let newerDisposeCalls = 0;

    const staleSession = {
      async bindExtensions(_bindings: { mode: "print" }): Promise<void> {
        markBindStarted();
        await bindGate;
      },
      extensionRunner: { async emit() {} },
      dispose(): void { staleDisposeCalls += 1; },
    };
    const newerSession = {
      async bindExtensions(_bindings: { mode: "print" }): Promise<void> {},
      extensionRunner: { async emit() {} },
      dispose(): void { newerDisposeCalls += 1; },
    };

    const staleLifecycle = createBridgeSessionRequestLifecycle();
    await staleLifecycle.createSession(async () => ({ session: staleSession }));
    const staleBind = staleLifecycle.bindSession(staleSession);

    await bindStarted;
    owners.set(key, newerOwner);
    const newerLifecycle = createBridgeSessionRequestLifecycle();
    await newerLifecycle.createSession(async () => ({ session: newerSession }));
    await newerLifecycle.bindSession(newerSession);
    assert.strictEqual(registerSessionIfOwner(owners, sessions, key, newerOwner, newerSession), true);
    assert.strictEqual(removeSessionIfOwner(owners, sessions, key, staleOwner), undefined);
    assert.strictEqual(sessions.get(key), newerSession, "stale cleanup must not remove the newer session");
    assert.strictEqual(newerDisposeCalls, 0, "stale cleanup must not dispose the newer session");

    releaseBind();
    await staleBind;
    assert.strictEqual(registerSessionIfOwner(owners, sessions, key, staleOwner, staleSession), false);
    await staleLifecycle.cancel();
    assert.strictEqual(sessions.get(key), newerSession, "newer ownership must remain registered");
    assert.strictEqual(staleDisposeCalls, 1, "stale session must be disposed once");
    assert.strictEqual(newerDisposeCalls, 0, "newer session must remain live");

    await staleLifecycle.cancel();
    await newerLifecycle.cancel();
    await newerLifecycle.cancel();
    assert.strictEqual(staleDisposeCalls, 1, "stale teardown must be idempotent");
    assert.strictEqual(newerDisposeCalls, 1, "newer teardown must be idempotent");
  });
});

describe("boundSessionHistoryPayload", () => {
  it("keeps small histories intact and preserves ordering", () => {
    const history = [
      { sender: "Igor" as const, text: "hello" },
      { sender: "Aurelia" as const, text: "hi there", timestamp: "2026-01-01T00:00:00.000Z" },
    ];
    const bounded = boundSessionHistoryPayload(history);
    assert.deepStrictEqual(bounded, history);
    // Serialized payload stays valid JSON under the sanitizer cap.
    const serialized = JSON.stringify(bounded);
    assert.ok(Buffer.byteLength(serialized, "utf8") < 16 * 1024);
  });

  it("truncates oversized messages so the serialized payload always parses", () => {
    const big = "x".repeat(20_000);
    const history = [
      { sender: "Igor" as const, text: big },
      { sender: "Aurelia" as const, text: "recent reply" },
    ];
    const bounded = boundSessionHistoryPayload(history);
    assert.strictEqual(bounded.length, 2, "both messages must survive (each truncated)");
    assert.ok(bounded[0].text.length <= 400, "oversized text must be truncated per message");
    assert.strictEqual(bounded[1].text, "recent reply");
    // Round-trip must succeed: the whole point of the bound is valid JSON.
    const serialized = JSON.stringify(bounded);
    assert.doesNotThrow(() => JSON.parse(serialized));
    assert.ok(Buffer.byteLength(serialized, "utf8") < 16 * 1024);
  });

  it("drops oldest messages when the payload budget is exhausted", () => {
    const history = Array.from({ length: 200 }, (_, i) => ({
      sender: ("Igor" as const),
      text: `message ${i} ` + "y".repeat(300),
    }));
    const bounded = boundSessionHistoryPayload(history);
    assert.ok(bounded.length < 200, "budget must drop old messages");
    assert.ok(bounded.length > 0, "newest message must always survive");
    // Newest message is the last one in the original list.
    assert.ok(bounded[bounded.length - 1].text.startsWith("message 199"));
    const serialized = JSON.stringify(bounded);
    assert.doesNotThrow(() => JSON.parse(serialized));
    assert.ok(Buffer.byteLength(serialized, "utf8") < 16 * 1024);
  });

  it("always emits valid JSON even for a single oversized message", () => {
    const history = [
      { sender: "Aurelia" as const, text: "z".repeat(50_000) },
    ];
    const bounded = boundSessionHistoryPayload(history);
    assert.strictEqual(bounded.length, 1);
    assert.ok(bounded[0].text.length <= 400);
    assert.doesNotThrow(() => JSON.parse(JSON.stringify(bounded)));
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
