import { describe, it } from "node:test";
import assert from "node:assert";
import {
  compactionEndPayload,
  compactionReason,
  formatLogLine,
  measuredElapsed,
  serializeOutEvent,
  StdoutEmissionBudget,
  stallTelemetryFor,
  ToolDurationTracker,
  validateBridgeRequest,
} from "./index.ts";

// ── long-session telemetry contract ────────────────────────────────────────

describe("serializeOutEvent", () => {
  it("stamps every NDJSON event with a valid ISO-8601 timestamp", () => {
    const line = serializeOutEvent({ event: "stall", request_id: "r1" });
    const parsed = JSON.parse(line) as { event: string; request_id: string; timestamp: string };
    assert.strictEqual(parsed.event, "stall");
    assert.strictEqual(parsed.request_id, "r1");
    assert.ok(!Number.isNaN(Date.parse(parsed.timestamp)), `expected ISO timestamp, got ${parsed.timestamp}`);
    assert.match(parsed.timestamp, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
  });

  it("preserves extra payload fields alongside the timestamp", () => {
    const line = serializeOutEvent({
      event: "tool_result",
      request_id: "r2",
      duration_ms: 1234,
      content: "redacted summary",
    });
    const parsed = JSON.parse(line) as Record<string, unknown>;
    assert.strictEqual(parsed.duration_ms, 1234);
    assert.strictEqual(parsed.content, "redacted summary");
    assert.strictEqual(typeof parsed.timestamp, "string");
  });

  it("bounds telemetry enums and avoids serializing untrusted object graphs", () => {
    const huge: Record<string, unknown> = {};
    for (let i = 0; i < 100; i++) huge[`key-${i}`] = "value";
    const parsed = JSON.parse(serializeOutEvent({
      event: "compaction_end",
      reason: "provider free text",
      source: "provider free text",
      severity: "provider free text",
      error_class: "provider free text",
      input: huge,
    })) as Record<string, unknown>;
    assert.strictEqual(parsed.reason, "unknown");
    assert.strictEqual(parsed.source, "unknown");
    assert.strictEqual(parsed.severity, "unknown");
    assert.strictEqual(parsed.error_class, "unknown");
    assert.ok(Object.keys(parsed.input as object).length <= 32);
  });

  it("keeps adversarial serialized output bounded while preserving terminal class", () => {
    const huge: Record<string, unknown> = {};
    for (let i = 0; i < 32; i++) huge[`field-${i}`] = "y".repeat(4_000);
    const line = serializeOutEvent({
      event: "result",
      request_id: "bounded-1",
      content: "x".repeat(200_000),
      nested: huge,
    });
    assert.ok(Buffer.byteLength(line, "utf8") <= 64 * 1024);
    const parsed = JSON.parse(line) as Record<string, unknown>;
    assert.strictEqual(parsed.event, "result");
    assert.strictEqual(parsed.request_id, "bounded-1");
    assert.strictEqual(parsed.payload_truncated, true);
  });
});

describe("validateBridgeRequest", () => {
  it("fails closed for missing or malformed correlation IDs before SDK work", () => {
    assert.strictEqual(validateBridgeRequest({ command: "ping" }).ok, false);
    assert.strictEqual(validateBridgeRequest({ command: "ping", request_id: "bad id" }).ok, false);
    assert.strictEqual(validateBridgeRequest({ command: "unknown", request_id: "r1" }).ok, false);
  });

  it("keeps the production command allowlist free of get-env", () => {
    assert.strictEqual(validateBridgeRequest({ command: "get-env", request_id: "r1" }).ok, false);
  });

  it("requires command-specific fields and accepts a valid compact request", () => {
    assert.strictEqual(validateBridgeRequest({ command: "query", request_id: "q1" }).ok, false);
    const valid = validateBridgeRequest({ command: "compact-session", request_id: "c1", options: {} });
    assert.strictEqual(valid.ok, true);
  });
});

describe("durable validation representation", () => {
  it("does not echo malformed request input", () => {
    // validateBridgeRequest is pure, so assert the only accepted durable error
    // representation is the bounded generic class at the request boundary.
    const invalid = validateBridgeRequest({ command: "query", request_id: "valid-1", prompt: 42 });
    assert.deepStrictEqual(invalid, { ok: false, error: "'prompt' must be a string" });
    assert.ok(!JSON.stringify(invalid).includes("42"));
  });
});

describe("StdoutEmissionBudget", () => {
  it("bounds active streams before any output is emitted", () => {
    const budget = new StdoutEmissionBudget(2);
    assert.strictEqual(budget.register("a"), true);
    assert.strictEqual(budget.register("b"), true);
    assert.strictEqual(budget.register("c"), false);
    budget.finish("a");
    assert.strictEqual(budget.register("c"), true);
  });
});

describe("formatLogLine", () => {
  it("prefixes operational logs with an ISO-8601 timestamp", () => {
    const line = formatLogLine("streaming stall: no PI SDK events");
    assert.match(line, /^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z\] \[bridge\] streaming stall: no PI SDK events$/);
  });
});

describe("ToolDurationTracker", () => {
  it("returns duration_ms only when a matching start was observed", () => {
    const t = new ToolDurationTracker();
    assert.strictEqual(t.end("missing"), undefined, "no start observed -> no duration");
    t.start("tool-1", 1_000);
    t.start("tool-2", 2_000);
    assert.strictEqual(t.end("tool-1", 3_500), 2_500);
    assert.strictEqual(t.end("tool-2", 5_000), 3_000);
    assert.strictEqual(t.end("tool-1", 6_000), undefined, "pair is single-use");
  });

  it("drops pairs when the clock moves backwards (no reliable duration)", () => {
    const t = new ToolDurationTracker();
    t.start("tool-x", 10_000);
    assert.strictEqual(t.end("tool-x", 9_000), undefined);
  });

  it("bounds growth: prunes oldest entries at the cap", () => {
    const t = new ToolDurationTracker(2, 60_000);
    t.start("a", 1_000);
    t.start("b", 2_000);
    t.start("c", 3_000); // evicts "a"
    assert.strictEqual(t.end("a", 4_000), undefined);
    assert.strictEqual(t.end("b", 5_000), 3_000);
    assert.strictEqual(t.end("c", 6_000), 3_000);
  });

  it("prunes leaked starts older than maxAgeMs", () => {
    const t = new ToolDurationTracker(8, 10_000);
    t.start("old", 1_000);
    t.prune(12_000);
    assert.strictEqual(t.end("old", 13_000), undefined);
  });

  it("clear() drops ALL starts so no state outlives the request", () => {
    const t = new ToolDurationTracker();
    t.start("a", 1_000);
    t.start("b", 2_000);
    t.clear();
    assert.strictEqual(t.end("a", 3_000), undefined);
    assert.strictEqual(t.end("b", 4_000), undefined);
    // Tracker stays usable after clear (no cross-request leakage).
    t.start("c", 5_000);
    assert.strictEqual(t.end("c", 6_000), 1_000);
  });

  it("does not pair missing opaque IDs across unrelated tool events", () => {
    const t = new ToolDurationTracker();
    t.start(undefined, 1_000);
    assert.strictEqual(t.end(undefined, 2_000), undefined);
  });
});

describe("compactionEndPayload", () => {
  it("emits duration_measured=true whenever duration_ms is present, including 0ms", () => {
    const zero = compactionEndPayload({
      reason: "manual",
      tokensBefore: 1000,
      success: true,
      errored: false,
      tokensAfter: 900,
      durationMs: 0,
    });
    assert.strictEqual(zero.duration_measured, true);
    assert.strictEqual(zero.duration_ms, 0);

    const withDur = compactionEndPayload({
      reason: "automatic",
      tokensBefore: 1000,
      success: true,
      errored: false,
      tokensAfter: 500,
      durationMs: 1234,
    });
    assert.strictEqual(withDur.duration_measured, true);
    assert.strictEqual(withDur.duration_ms, 1234);

    // No measured pair -> neither field is emitted.
    const unmeasured = compactionEndPayload({
      reason: "unknown",
      tokensBefore: 1000,
      success: true,
      errored: false,
    });
    assert.strictEqual(unmeasured.duration_measured, undefined);
    assert.strictEqual(unmeasured.duration_ms, undefined);
  });

  it("keeps delta_tokens signed: negative = reduction, positive = growth", () => {
    const reduced = compactionEndPayload({
      reason: "manual", tokensBefore: 1000, success: true, errored: false, tokensAfter: 800,
    });
    assert.strictEqual(reduced.delta_tokens, -200);
    const grew = compactionEndPayload({
      reason: "manual", tokensBefore: 1000, success: true, errored: false, tokensAfter: 1200,
    });
    assert.strictEqual(grew.delta_tokens, 200);
    // Explicit zero delta (neutral) is present, not hidden.
    const neutral = compactionEndPayload({
      reason: "manual", tokensBefore: 500, success: true, errored: false, tokensAfter: 500,
    });
    assert.strictEqual(neutral.delta_tokens, 0);
    // Unmeasured -> tokens_after/delta_tokens omitted entirely.
    const unmeasured = compactionEndPayload({
      reason: "manual", tokensBefore: 1000, success: true, errored: false,
    });
    assert.strictEqual(unmeasured.tokens_after, undefined);
    assert.strictEqual(unmeasured.delta_tokens, undefined);
  });

  it("emits only the static compaction_error enum, never raw text", () => {
    const errored = compactionEndPayload({
      reason: "manual", tokensBefore: 1000, success: false, errored: true,
    });
    assert.strictEqual(errored.error_class, "compaction_error");
    assert.ok(!("errorMessage" in errored), "raw error message must never be emitted");
    const ok = compactionEndPayload({
      reason: "manual", tokensBefore: 1000, success: true, errored: false,
    });
    assert.strictEqual(ok.error_class, undefined);
  });
});

describe("compactionReason", () => {
  it("maps known SDK reasons to the explicit enum", () => {
    assert.strictEqual(compactionReason("manual"), "manual");
    assert.strictEqual(compactionReason("user"), "manual");
    assert.strictEqual(compactionReason("auto"), "automatic");
    assert.strictEqual(compactionReason("automatic"), "automatic");
    assert.strictEqual(compactionReason("context_window"), "automatic");
    assert.strictEqual(compactionReason("CONTEXT"), "automatic");
  });

  it("falls back to unknown for empty, missing, or free-text reasons", () => {
    assert.strictEqual(compactionReason(undefined), "unknown");
    assert.strictEqual(compactionReason(null), "unknown");
    assert.strictEqual(compactionReason(""), "unknown");
    assert.strictEqual(compactionReason("raw SDK free text here"), "unknown");
  });
});

describe("measuredElapsed", () => {
  it("returns a non-negative duration only when the start marker exists", () => {
    assert.strictEqual(measuredElapsed(undefined, 5_000), undefined);
    assert.strictEqual(measuredElapsed(1_000, 3_500), 2_500);
    assert.strictEqual(measuredElapsed(1_000, 1_000), 0, "zero-duration pair is measurable");
  });

  it("never returns a negative duration when the clock moves backwards", () => {
    assert.strictEqual(measuredElapsed(10_000, 9_000), undefined);
  });
});

describe("stallTelemetryFor", () => {
  it("emits no telemetry below the warning threshold", () => {
    assert.deepStrictEqual(stallTelemetryFor(59_999, false, false), {});
    assert.deepStrictEqual(stallTelemetryFor(30_000, false, false), {});
  });

  it("emits stall+steer warning once at 60s and urgent once at 120s", () => {
    assert.deepStrictEqual(stallTelemetryFor(60_000, false, false), {
      stall: "warning",
      steer: "warning",
    });
    // Flags set: nothing new even deeper into the silence.
    assert.deepStrictEqual(stallTelemetryFor(90_000, true, false), {});
    assert.deepStrictEqual(stallTelemetryFor(120_000, true, false), {
      stall: "urgent",
      steer: "urgent",
    });
    assert.deepStrictEqual(stallTelemetryFor(300_000, true, true), {});
  });

  it("can re-emit after the flags are reset by resumed activity", () => {
    assert.deepStrictEqual(stallTelemetryFor(60_000, false, false), {
      stall: "warning",
      steer: "warning",
    });
    // Activity resumed -> caller resets flags -> a new transition can fire.
    assert.deepStrictEqual(stallTelemetryFor(70_000, false, false), {
      stall: "warning",
      steer: "warning",
    });
  });

  it("always reports positive silent_ms from the caller (>= 60s here)", () => {
    assert.ok(stallTelemetryFor(60_000, false, false).stall !== undefined);
  });
});
