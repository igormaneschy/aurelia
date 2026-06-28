import { describe, it, mock } from "node:test";
import assert from "node:assert";
import { waitForPendingMessageCount } from "./index.ts";

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
