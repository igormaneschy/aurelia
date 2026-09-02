import { createRequire as __piCreateRequire } from 'module';const require = __piCreateRequire(import.meta.url);

// index.ts
import { createInterface } from "node:readline";
import { appendFileSync, existsSync, mkdirSync, renameSync, statSync, truncateSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { createHash } from "node:crypto";
import {
  createAgentSession,
  DefaultResourceLoader,
  getAgentDir,
  ModelRuntime,
  SessionManager,
  SettingsManager
} from "@earendil-works/pi-coding-agent";
var MAX_SESSION_CACHE = 256;
var sessionByID = /* @__PURE__ */ new Map();
var lastSessionID = "";
var activeRequests = /* @__PURE__ */ new Map();
var chatSessions = /* @__PURE__ */ new Map();
var chatSessionOwners = /* @__PURE__ */ new Map();
function registerSessionIfOwner(owners, sessions, key, owner, session) {
  if (owners.get(key) !== owner) return false;
  sessions.set(key, session);
  return true;
}
function removeSessionIfOwner(owners, sessions, key, owner) {
  if (owners.get(key) !== owner) return void 0;
  const session = sessions.get(key);
  if (session === void 0) return void 0;
  sessions.delete(key);
  return session;
}
var pendingChatCleanups = /* @__PURE__ */ new Map();
function claimChatSessionOwner(key) {
  const owner = Symbol("chat-session:".concat(key));
  chatSessionOwners.set(key, owner);
  return owner;
}
function ownsChatSession(key, owner) {
  return chatSessionOwners.get(key) === owner;
}
var bridgeSessionLifecycles = /* @__PURE__ */ new WeakMap();
function bridgeSessionLifecycleFor(session) {
  const existing = bridgeSessionLifecycles.get(session);
  if (existing) return existing;
  const lifecycle = {
    extensionRuntimeLoaded: false,
    extensionActivationStarted: false,
    bindingCompleted: false
  };
  bridgeSessionLifecycles.set(session, lifecycle);
  return lifecycle;
}
function markBridgeSessionExtensionRuntimeLoaded(session) {
  bridgeSessionLifecycleFor(session).extensionRuntimeLoaded = true;
}
async function disposeBridgeSession(session) {
  const lifecycle = bridgeSessionLifecycleFor(session);
  if (!lifecycle.teardownPromise) {
    lifecycle.teardownPromise = Promise.resolve().then(async () => {
      if (lifecycle.extensionRuntimeLoaded || lifecycle.extensionActivationStarted) {
        try {
          await session.extensionRunner.emit({ type: "session_shutdown", reason: "quit" });
        } catch (err) {
          redactedLog(
            "session teardown: session_shutdown failed: ".concat(err instanceof Error ? err.message : String(err))
          );
        }
      }
      try {
        session.dispose();
      } catch (err) {
        redactedLog(
          "session teardown: dispose failed: ".concat(err instanceof Error ? err.message : String(err))
        );
      }
    });
  }
  await lifecycle.teardownPromise;
}
function chatKey(chatID, threadID, userID = 0) {
  return "".concat(chatID, ":").concat(threadID, ":").concat(userID);
}
function cleanupChatSession(key, expectedOwner) {
  const cs = chatSessions.get(key);
  if (expectedOwner && cs && cs.owner !== expectedOwner) return Promise.resolve();
  const pending = pendingChatCleanups.get(key);
  if (pending) {
    if (expectedOwner && pending.owner !== expectedOwner) {
      if (!cs) return Promise.resolve();
      return pending.promise.then(() => cleanupChatSession(key, expectedOwner));
    }
    if (!expectedOwner && cs && pending.owner !== cs.owner) {
      return pending.promise.then(() => cleanupChatSession(key));
    }
    return pending.promise;
  }
  if (!cs) return Promise.resolve();
  if (expectedOwner && cs.owner !== expectedOwner) return Promise.resolve();
  if (expectedOwner) {
    if (!removeSessionIfOwner(chatSessionOwners, chatSessions, key, expectedOwner)) {
      return Promise.resolve();
    }
  } else {
    chatSessions.delete(key);
  }
  if (chatSessionOwners.get(key) === cs.owner) chatSessionOwners.delete(key);
  const cleanup = Promise.resolve().then(async () => {
    clearTimeout(cs.idleTimer);
    try {
      cs.unsubPersistent();
    } catch {
    }
    try {
      if (cs.unsubHook) cs.unsubHook();
    } catch {
    }
    await disposeBridgeSession(cs.session);
  });
  pendingChatCleanups.set(key, { owner: cs.owner, promise: cleanup });
  void cleanup.then(
    () => {
      if (pendingChatCleanups.get(key)?.promise === cleanup) pendingChatCleanups.delete(key);
    },
    () => {
      if (pendingChatCleanups.get(key)?.promise === cleanup) pendingChatCleanups.delete(key);
    }
  );
  return cleanup;
}
function startIdleTimer(cs, key) {
  clearTimeout(cs.idleTimer);
  cs.idleTimer = setTimeout(async () => {
    log("idle timeout: cleaning up session ".concat(cs.sessionId, " for chat ").concat(key));
    await cleanupChatSession(key, cs.owner);
  }, 30 * 60 * 1e3);
}
function rememberSession(id, lookup) {
  if (sessionByID.has(id)) sessionByID.delete(id);
  sessionByID.set(id, lookup);
  while (sessionByID.size > MAX_SESSION_CACHE) {
    const oldest = sessionByID.keys().next().value;
    if (oldest === void 0) break;
    sessionByID.delete(oldest);
  }
}
var MAX_BRIDGE_LOG_RUNES = 2048;
var MAX_REQUEST_ID_RUNES = 128;
var MAX_TELEMETRY_LABEL_RUNES = 128;
var MAX_EVENT_TEXT_RUNES = 16 * 1024;
var MAX_RESULT_CONTENT_RUNES = 256 * 1024;
var MAX_EVENT_VALUE_RUNES = 2048;
var MAX_OUT_EVENT_BYTES = 320 * 1024;
var MAX_BRIDGE_REQUEST_BYTES = 256 * 1024;
var MAX_TOOL_INPUT_DEPTH = 3;
var MAX_TOOL_INPUT_KEYS = 32;
var MAX_TOOL_INPUT_ITEMS = 16;
var MAX_COUNTER_VALUE = 1e8;
var MAX_DURATION_MS = 24 * 60 * 60 * 1e3;
function truncateBridgeRunes(value, maxRunes) {
  if (maxRunes <= 0) return "";
  const runes = Array.from(value);
  return runes.length <= maxRunes ? value : runes.slice(0, maxRunes).join("");
}
function removeBridgeControls(value) {
  return Array.from(value).filter((r) => {
    const code = r.codePointAt(0) ?? 0;
    return code >= 32 && code !== 127 && (code < 128 || code > 159);
  }).join("");
}
function sanitizeBridgeText(value, maxRunes) {
  const raw = typeof value === "string" ? value : String(value ?? "");
  return truncateBridgeRunes(removeBridgeControls(redactSDKError(raw)), maxRunes);
}
function safeRequestID(value) {
  if (typeof value !== "string" || value.length === 0 || value.length > MAX_REQUEST_ID_RUNES) return "";
  return /^[A-Za-z0-9][A-Za-z0-9._:-]*$/.test(value) ? value : "";
}
var BRIDGE_COMMANDS = /* @__PURE__ */ new Set([
  "query",
  "ping",
  "cancel",
  "list-models",
  "steer",
  "follow-up",
  "abort",
  "get-state",
  "get-session-stats",
  "get-session-history",
  "compact-session",
  "rotate-session"
]);
function validateBridgeRequest(value) {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return { ok: false, error: "request must be an object" };
  }
  const raw = value;
  const requestID = safeRequestID(raw.request_id);
  if (!requestID) return { ok: false, error: "missing or invalid 'request_id'" };
  if (typeof raw.command !== "string" || !BRIDGE_COMMANDS.has(raw.command)) {
    return { ok: false, error: "missing or invalid 'command' field" };
  }
  if (raw.target_request_id !== void 0 && !safeRequestID(raw.target_request_id)) {
    return { ok: false, error: "invalid 'target_request_id'" };
  }
  if (raw.prompt !== void 0 && typeof raw.prompt !== "string") {
    return { ok: false, error: "'prompt' must be a string" };
  }
  if (raw.options !== void 0 && (typeof raw.options !== "object" || raw.options === null || Array.isArray(raw.options))) {
    return { ok: false, error: "'options' must be an object" };
  }
  if (raw.refresh !== void 0 && typeof raw.refresh !== "boolean") {
    return { ok: false, error: "'refresh' must be a boolean" };
  }
  const command = raw.command;
  if ((command === "query" || command === "steer" || command === "follow-up") && (typeof raw.prompt !== "string" || raw.prompt.length === 0)) {
    return { ok: false, error: "missing 'prompt' field for ".concat(command, " command") };
  }
  if (command === "cancel" && !safeRequestID(raw.target_request_id)) {
    return { ok: false, error: "missing or invalid 'target_request_id' for cancel command" };
  }
  return {
    ok: true,
    request: { ...raw, command, request_id: requestID }
  };
}
function toolCallKey(value) {
  if (value === null || value === void 0) return void 0;
  if (typeof value === "string") return "s:".concat(sanitizeBridgeText(value, MAX_TELEMETRY_LABEL_RUNES));
  if (typeof value === "number" || typeof value === "boolean" || typeof value === "bigint") {
    return "".concat(typeof value, ":").concat(String(value));
  }
  return "opaque_tool_id";
}
function safeToolCallID(value) {
  const digest = createHash("sha256").update(toolCallKey(value) ?? "missing_tool_id").digest("hex").slice(0, 20);
  return "tool-".concat(digest);
}
function safeLabel(value, fallback) {
  const label = sanitizeBridgeText(value, MAX_TELEMETRY_LABEL_RUNES).trim();
  return label || fallback;
}
function boundedCounter(value) {
  if (typeof value !== "number" || !Number.isFinite(value) || !Number.isSafeInteger(value)) return void 0;
  if (value < 0) return 0;
  return Math.min(value, MAX_COUNTER_VALUE);
}
function boundedDuration(value) {
  if (typeof value !== "number" || !Number.isFinite(value) || !Number.isSafeInteger(value)) return void 0;
  if (value < 0 || value > MAX_DURATION_MS) return void 0;
  return value;
}
function sanitizeToolInput(value, depth = 0) {
  if (depth >= MAX_TOOL_INPUT_DEPTH) return "[input_depth_limit]";
  if (value === null || typeof value === "boolean") return value;
  if (typeof value === "number") return Number.isFinite(value) ? value : "[invalid_number]";
  if (typeof value === "string") return sanitizeBridgeText(value, MAX_EVENT_VALUE_RUNES);
  if (Array.isArray(value)) {
    return value.slice(0, MAX_TOOL_INPUT_ITEMS).map((item) => sanitizeToolInput(item, depth + 1));
  }
  if (typeof value === "object") {
    const out = {};
    let count = 0;
    for (const key in value) {
      if (!Object.prototype.hasOwnProperty.call(value, key)) continue;
      if (count++ >= MAX_TOOL_INPUT_KEYS) break;
      const item = value[key];
      const safeKey = sanitizeBridgeText(key, MAX_TELEMETRY_LABEL_RUNES);
      if (safeKey) out[safeKey] = sanitizeToolInput(item, depth + 1);
    }
    return out;
  }
  return "[unsupported_input]";
}
function sanitizeOutValue(value, depth = 0) {
  if (depth >= 3) return "[value_depth_limit]";
  if (typeof value === "string") return sanitizeBridgeText(value, MAX_EVENT_VALUE_RUNES);
  if (value === null || typeof value === "boolean") return value;
  if (typeof value === "number") return Number.isFinite(value) ? value : 0;
  if (Array.isArray(value)) {
    return value.slice(0, MAX_TOOL_INPUT_ITEMS).map((item) => sanitizeOutValue(item, depth + 1));
  }
  if (typeof value === "object") {
    const out = {};
    let count = 0;
    for (const key in value) {
      if (!Object.prototype.hasOwnProperty.call(value, key)) continue;
      if (count++ >= MAX_TOOL_INPUT_KEYS) break;
      const item = value[key];
      const safeKey = sanitizeBridgeText(key, MAX_TELEMETRY_LABEL_RUNES);
      if (safeKey) out[safeKey] = sanitizeOutValue(item, depth + 1);
    }
    return out;
  }
  return "[unsupported_value]";
}
function sanitizeOutEvent(obj) {
  const event = safeLabel(obj.event, "unknown");
  const out = { event };
  const requestID = safeRequestID(obj.request_id);
  if (requestID) out.request_id = requestID;
  let fieldCount = 0;
  for (const key in obj) {
    if (!Object.prototype.hasOwnProperty.call(obj, key)) continue;
    const value = obj[key];
    if (key === "event" || key === "request_id" || key === "timestamp") continue;
    if (fieldCount++ >= MAX_TOOL_INPUT_KEYS) break;
    if (key === "content" || key === "text" || key === "message") {
      const limit = event === "result" && key === "content" ? MAX_RESULT_CONTENT_RUNES : MAX_EVENT_TEXT_RUNES;
      out[key] = sanitizeBridgeText(value, limit);
    } else if (key === "input") {
      out[key] = sanitizeToolInput(value);
    } else if (key === "name" || key === "tool_name") {
      out[key] = safeLabel(value, "tool");
    } else if (key === "id" || key === "tool_call_id") {
      out[key] = safeToolCallID(value);
    } else if (key === "error") {
      out[key] = safeLabel(value, "unknown");
    } else if (key === "reason") {
      out[key] = compactionReason(value);
    } else if (key === "source") {
      out[key] = value === "bridge_health" ? "bridge_health" : "unknown";
    } else if (key === "severity") {
      out[key] = value === "warning" || value === "urgent" ? value : "unknown";
    } else if (key === "error_class") {
      out[key] = value === "compaction_error" ? value : "unknown";
    } else {
      out[key] = sanitizeOutValue(value);
    }
  }
  return out;
}
function serializedByteLength(value) {
  return Buffer.byteLength(value, "utf8");
}
var StdoutEmissionBudget = class {
  constructor(maxActiveStreams = 32, maxAggregateBytes = 2 * 1024 * 1024, maxPerStreamBytes = 512 * 1024, reservedTerminalBytes = MAX_OUT_EVENT_BYTES) {
    this.maxActiveStreams = maxActiveStreams;
    this.maxAggregateBytes = maxAggregateBytes;
    this.maxPerStreamBytes = maxPerStreamBytes;
    this.reservedTerminalBytes = reservedTerminalBytes;
  }
  maxActiveStreams;
  maxAggregateBytes;
  maxPerStreamBytes;
  reservedTerminalBytes;
  streams = /* @__PURE__ */ new Map();
  aggregateBytes = 0;
  backpressured = false;
  pendingBytes = 0;
  drainAttached = false;
  register(requestID) {
    if (this.streams.has(requestID)) return true;
    if (this.streams.size >= this.maxActiveStreams) return false;
    this.streams.set(requestID, 0);
    return true;
  }
  release(requestID) {
    const bytes = this.streams.get(requestID);
    if (bytes === void 0) return;
    this.streams.delete(requestID);
    this.aggregateBytes = Math.max(0, this.aggregateBytes - bytes);
  }
  markBackpressure(bytes) {
    this.backpressured = true;
    this.pendingBytes += bytes;
    if (this.drainAttached) return;
    this.drainAttached = true;
    process.stdout.once("drain", () => {
      this.pendingBytes = 0;
      this.backpressured = false;
      this.drainAttached = false;
    });
  }
  /** Write one already-serialized line, returning false when a non-terminal
   * event was bounded/dropped. Terminal events always use the reserved path. */
  write(line, requestID, terminal) {
    const bytes = serializedByteLength(line);
    if (bytes > MAX_OUT_EVENT_BYTES) return false;
    if (!terminal) {
      const streamBytes = this.streams.get(requestID);
      if (streamBytes === void 0 || this.backpressured) return false;
      if (streamBytes + bytes > this.maxPerStreamBytes) return false;
      if (this.aggregateBytes + bytes > this.maxAggregateBytes - this.reservedTerminalBytes) return false;
      this.streams.set(requestID, streamBytes + bytes);
      this.aggregateBytes += bytes;
    }
    try {
      const accepted = process.stdout.write(line);
      if (!accepted) this.markBackpressure(bytes);
      if (terminal) this.release(requestID);
      return true;
    } catch {
      if (!terminal) {
        const current = this.streams.get(requestID) ?? 0;
        this.streams.set(requestID, Math.max(0, current - bytes));
        this.aggregateBytes = Math.max(0, this.aggregateBytes - bytes);
      }
      return false;
    }
  }
  finish(requestID) {
    this.release(requestID);
  }
  activeStreamCount() {
    return this.streams.size;
  }
};
var stdoutEmissionBudget = new StdoutEmissionBudget();
function serializeOutEvent(obj) {
  const sanitized = sanitizeOutEvent(obj);
  const candidate = JSON.stringify({ ...sanitized, timestamp: (/* @__PURE__ */ new Date()).toISOString() });
  if (serializedByteLength(candidate) <= MAX_OUT_EVENT_BYTES) return candidate;
  const fallback = {
    event: sanitized.event,
    ...sanitized.request_id ? { request_id: sanitized.request_id } : {},
    timestamp: (/* @__PURE__ */ new Date()).toISOString(),
    payload_truncated: true
  };
  if (sanitized.event === "result" && typeof sanitized.content === "string") {
    fallback.content = truncateBridgeRunes(sanitized.content, 4096);
  } else if (sanitized.event === "error" && typeof sanitized.message === "string") {
    fallback.message = truncateBridgeRunes(sanitized.message, 2048);
  }
  return JSON.stringify(fallback);
}
function emit(obj) {
  const line = serializeOutEvent(obj) + "\n";
  const requestID = safeRequestID(obj.request_id) || "__untracked__";
  const terminal = obj.event === "result" || obj.event === "error" || obj.event === "pong";
  if (!terminal && stdoutEmissionBudget.activeStreamCount() === 0) {
    return;
  }
  if (!stdoutEmissionBudget.write(line, requestID, terminal)) {
    if (!terminal) redactedLog("stdout emission budget dropped event=".concat(safeLabel(obj.event, "unknown")));
  }
}
function formatLogLine(msg) {
  return "[".concat((/* @__PURE__ */ new Date()).toISOString(), "] [bridge] ").concat(sanitizeBridgeText(msg, MAX_BRIDGE_LOG_RUNES));
}
function log(msg) {
  process.stderr.write(formatLogLine(msg) + "\n");
}
var ToolDurationTracker = class {
  constructor(maxEntries = 512, maxAgeMs = 2 * 60 * 60 * 1e3) {
    this.maxEntries = maxEntries;
    this.maxAgeMs = maxAgeMs;
  }
  maxEntries;
  maxAgeMs;
  starts = /* @__PURE__ */ new Map();
  start(toolCallId, now = Date.now()) {
    const key = toolCallKey(toolCallId);
    const safeID = safeToolCallID(toolCallId);
    if (key === void 0) return safeID;
    if (!this.starts.has(key) && this.starts.size >= this.maxEntries) {
      const oldest = this.starts.keys().next().value;
      if (oldest !== void 0) this.starts.delete(oldest);
    }
    this.starts.set(key, { safeID, startedAt: now });
    return safeID;
  }
  end(toolCallId, now = Date.now()) {
    return this.endWithID(toolCallId, now)?.durationMs;
  }
  endWithID(toolCallId, now = Date.now()) {
    const key = toolCallKey(toolCallId);
    if (key === void 0) return void 0;
    const started = this.starts.get(key);
    if (started === void 0) return void 0;
    this.starts.delete(key);
    if (now < started.startedAt) return void 0;
    return { toolCallID: started.safeID, durationMs: now - started.startedAt };
  }
  prune(now = Date.now()) {
    for (const [id, started] of this.starts) {
      if (now - started.startedAt > this.maxAgeMs) this.starts.delete(id);
    }
  }
  /**
   * Drops ALL tracked starts. Called at request teardown (finally) and when
   * the persistent subscription is unsubscribed so no tool-duration state is
   * retained by chatSessions after the request, and leaked starts can never
   * cross into a later request on the same session.
   */
  clear() {
    this.starts.clear();
  }
};
function compactionReason(raw) {
  if (typeof raw !== "string" || !raw) return "unknown";
  switch (raw.trim().toLowerCase()) {
    case "manual":
    case "user":
      return "manual";
    case "auto":
    case "automatic":
    case "system":
    case "context":
    case "context_window":
    case "threshold":
    case "overflow":
      return "automatic";
    default:
      return "unknown";
  }
}
function compactionEndPayload(args) {
  const tokensBefore = boundedCounter(args.tokensBefore) ?? 0;
  const tokensAfter = boundedCounter(args.tokensAfter);
  const durationMs = boundedDuration(args.durationMs);
  return {
    event: "compaction_end",
    reason: args.reason,
    tokens_before: tokensBefore,
    success: args.success,
    // Static enum value only — raw errorMessage never leaves the bridge.
    ...args.errored ? { error_class: "compaction_error" } : {},
    // tokens_after/delta_tokens are only present when the SDK measured
    // them; a negative delta means the context was reduced (effective) and
    // stays observable so a regression is never silently marked as success.
    ...tokensAfter !== void 0 ? { tokens_after: tokensAfter, delta_tokens: tokensAfter - tokensBefore } : {},
    // duration_measured=true is the authoritative presence marker — present
    // even when the measured duration is 0ms.
    ...durationMs !== void 0 ? { duration_measured: true, duration_ms: durationMs } : {}
  };
}
function measuredElapsed(startedAt, now = Date.now()) {
  if (startedAt === void 0) return void 0;
  const elapsed = now - startedAt;
  return elapsed >= 0 ? elapsed : void 0;
}
function stallTelemetryFor(silentMs, warningSent, urgentSent) {
  const out = {};
  if (silentMs >= 6e4 && !warningSent) {
    out.stall = "warning";
    out.steer = "warning";
  }
  if (silentMs >= 12e4 && !urgentSent) {
    out.stall = "urgent";
    out.steer = "urgent";
  }
  return out;
}
function redactSDKError(msg) {
  return msg.replace(/\bsk-[A-Za-z0-9]{20,}/g, "[API_KEY_REDACTED]").replace(/\bpk-[A-Za-z0-9]{20,}/g, "[API_KEY_REDACTED]").replace(/\bsk-ant-[A-Za-z0-9]{20,}/g, "[API_KEY_REDACTED]").replace(/\bsk-proj-[A-Za-z0-9]{20,}/g, "[API_KEY_REDACTED]").replace(/\bsk_live_[A-Za-z0-9]+/g, "[STRIPE_KEY_REDACTED]").replace(/\bsk_test_[A-Za-z0-9]+/g, "[STRIPE_KEY_REDACTED]").replace(/\bAKIA[A-Z0-9]{16}/g, "[AWS_KEY_REDACTED]").replace(/\bAIza[0-9A-Za-z_-]{35}/g, "[GCP_KEY_REDACTED]").replace(/\bghp_[A-Za-z0-9]{36}/g, "[GH_TOKEN_REDACTED]").replace(/\bgho_[A-Za-z0-9]{36}/g, "[GH_TOKEN_REDACTED]").replace(/\bghu_[A-Za-z0-9]{36}/g, "[GH_TOKEN_REDACTED]").replace(/\bghs_[A-Za-z0-9]{36}/g, "[GH_TOKEN_REDACTED]").replace(/\bghr_[A-Za-z0-9]{36}/g, "[GH_TOKEN_REDACTED]").replace(/\bgithub_pat_[0-9A-Za-z_-]+/g, "[GH_PAT_REDACTED]").replace(/\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+/g, "[JWT_REDACTED]").replace(/-----BEGIN (OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----[\s\S]*?-----END (OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----/g, "[PRIVATE_KEY_BLOCK_REDACTED]").replace(/(Authorization:\s*(?:Bearer|Basic)\s+)\S+/gi, "$1[REDACTED]").replace(/\bxai-[A-Za-z0-9]{20,}/g, "[XAI_KEY_REDACTED]").replace(/\bglpat-[A-Za-z0-9_-]{20,}/g, "[GL_TOKEN_REDACTED]").replace(/\bhf_[A-Za-z0-9]{20,}/g, "[HF_TOKEN_REDACTED]").replace(/\bnpm_[A-Za-z0-9]{36}/g, "[NPM_TOKEN_REDACTED]").replace(/\bxox[bpasa]-[A-Za-z0-9-]{20,}/g, "[SLACK_TOKEN_REDACTED]").replace(/\bxapp-[A-Za-z0-9-]{20,}/g, "[SLACK_TOKEN_REDACTED]");
}
function redactedLog(msg) {
  log(redactSDKError(msg));
}
function isBillingError(msg) {
  const lower = msg.toLowerCase();
  return lower.includes("insufficient balance") || lower.includes("insufficient credits") || lower.includes("401") && lower.includes("billing") || lower.includes("billing error");
}
function redactedCommandExcerpt(command, maxLen) {
  const redacted = redactSDKError(command);
  if (redacted.length <= maxLen) return redacted;
  return redacted.slice(0, maxLen) + "...";
}
function redactAuditPath(text) {
  return text.replace(/(?:^|[/~])\.env(?:\b|\/|$)/g, "[SENSITIVE_PATH_REDACTED]").replace(/(?:^|[/~])\.ssh(?:\/|$)/g, "[SENSITIVE_PATH_REDACTED]").replace(/(?:^|[/~])\.pi(?:\/|$)/g, "[SENSITIVE_PATH_REDACTED]").replace(/(?:^|[/~])\.aurelia\/config(?:\/|$)/g, "[SENSITIVE_PATH_REDACTED]").replace(/(?:^|[/~])\.git\/config(?:\b|\/|$)/g, "[SENSITIVE_PATH_REDACTED]");
}
function escapeUntrustedSummary(text) {
  return redactSDKError(text).replace(/<\/previous_session_summary_untrusted>/gi, "&lt;/previous_session_summary_untrusted&gt;").replace(/<previous_session_summary_untrusted>/gi, "&lt;previous_session_summary_untrusted&gt;");
}
function piAgentDir() {
  return process.env.PI_CODING_AGENT_DIR || join(homedir(), ".pi", "agent");
}
function mapProvider(provider) {
  if (!provider) return void 0;
  const normalized = provider.trim().toLowerCase();
  const aliases = {
    kimi: "kimi-coding",
    kilo: "opencode-go",
    alibaba: "opencode-go",
    google: "google",
    anthropic: "anthropic",
    openrouter: "openrouter",
    zai: "zai",
    ollama: "ollama"
  };
  return aliases[normalized] ?? normalized;
}
function mapModelForProvider(provider, model) {
  const normalized = model.trim();
  if (provider === "kimi-coding" && (normalized === "k2.5" || normalized === "kimi-k2.5")) {
    return "kimi-for-coding";
  }
  return normalized;
}
function translateToolName(name) {
  const normalized = name.trim();
  const toolMap = {
    Read: "read",
    Write: "write",
    Edit: "edit",
    Bash: "bash",
    Grep: "grep",
    Glob: "find",
    LS: "ls",
    List: "ls",
    WebSearch: "web_search",
    WebSearchPremium: "web_search_premium",
    WebFetch: "web_search"
  };
  return toolMap[normalized] ?? normalized.toLowerCase();
}
var allBuiltinTools = [
  "read",
  "write",
  "edit",
  "bash",
  "grep",
  "find",
  "ls",
  "web_search",
  "web_search_premium"
];
var EXTENSION_UTILITY_TOOLS = [
  "mcp",
  "code_search",
  "fetch_content",
  "get_search_content"
];
function translateAllowedTools(allowed, disallowed) {
  const hasRestriction = allowed && allowed.length > 0 || disallowed && disallowed.length > 0;
  let result;
  if (allowed && allowed.length > 0) {
    result = [...new Set(allowed.map(translateToolName))];
  }
  if (disallowed && disallowed.length > 0) {
    const denied = new Set(disallowed.map(translateToolName));
    if (result) {
      result = result.filter((t) => !denied.has(t));
    } else {
      result = allBuiltinTools.filter((t) => !denied.has(t));
    }
  }
  if (result !== void 0) {
    const denied = new Set((disallowed ?? []).map(translateToolName));
    for (const ext of EXTENSION_UTILITY_TOOLS) {
      if (!denied.has(ext) && !result.includes(ext)) {
        result.push(ext);
      }
    }
  }
  if (!result || result.length === 0) {
    return hasRestriction ? [] : void 0;
  }
  return result;
}
var DEFAULT_SENSITIVE_PATTERNS = [
  ".env",
  ".env.*",
  "*.pem",
  "*.key",
  "id_rsa",
  "id_ed25519",
  "config.json",
  "credentials.json",
  "*.credentials",
  "service-account*.json",
  ".ssh/*",
  ".pi/*",
  ".aurelia/config/*",
  ".git/config"
];
function matchesGlob(pattern, name) {
  const regexStr = "^" + pattern.replace(/\./g, "\\.").replace(/\*/g, ".*").replace(/\?/g, ".") + "$";
  try {
    return new RegExp(regexStr).test(name);
  } catch {
    return false;
  }
}
function isSensitivePath(path, patterns) {
  const clean = path.replace(/\\/g, "/");
  const parts = clean.split("/");
  const base = parts[parts.length - 1] || "";
  for (const pat of patterns) {
    if (matchesGlob(pat, base)) return true;
    if (matchesGlob(pat, clean)) return true;
  }
  for (const dir of [".ssh", ".aurelia/config", ".pi"]) {
    if (clean.includes("/" + dir + "/") || clean.startsWith(dir + "/")) {
      return true;
    }
  }
  return false;
}
function isDestructiveCommand(command) {
  const lower = command.trim().toLowerCase();
  if (/^rm\s+.*-rf/i.test(lower) || /^rm\s+.*-fr/i.test(lower)) {
    for (const bad of ["/ ", "/*", "/.", "~/", "/etc", "/usr", "/bin", "/lib", "/home", "/root", "/var"]) {
      if (lower.includes(bad)) return true;
    }
  }
  if (/rm\s+\//.test(lower) || /rm -rf \//.test(lower)) return true;
  if (/^sudo\s/.test(lower)) return true;
  if (/chmod.*-r/i.test(lower)) {
    for (const bad of ["/ ", "/etc", "/usr", "/bin", "/lib"]) {
      if (lower.includes(bad)) return true;
    }
  }
  if (/chown.*-r/i.test(lower)) return true;
  if (/^dd\s/.test(lower) && /of=/.test(lower)) return true;
  if (lower.includes(":(){") || lower.includes(":()")) return true;
  if (/^mkfs/.test(lower) || /^fdisk/.test(lower) || /^parted/.test(lower)) return true;
  return false;
}
function isExfiltrationCommand(command) {
  const lower = command.trim().toLowerCase();
  const hasNetworkTool = ["curl ", "wget ", "nc ", "ncat ", "scp ", "rsync "].some(
    (t) => lower.includes(t)
  );
  if (!hasNetworkTool) return false;
  const suspicious = [
    "$(cat ",
    "`cat `",
    "`env`",
    "$(env)",
    "<~",
    ".env",
    "id_rsa",
    "token",
    "secret",
    "password",
    "-d @",
    " --data @",
    "--data-raw",
    "--data-binary",
    "-F ",
    "--form ",
    "file=@",
    "| nc ",
    "| ncat "
  ];
  return suspicious.some((s) => lower.includes(s));
}
function matchesEnvAccess(command) {
  const lower = command.trim().toLowerCase();
  if (/^env$/.test(lower) || /^printenv/.test(lower) || /^export($|\s)/.test(lower)) return true;
  if (lower.includes(".aurelia/config")) return true;
  if (/echo\s+\$/.test(lower) || /echo \${/.test(lower)) return true;
  if (lower.includes("cat ~/.aurelia")) return true;
  return false;
}
function hasShellComposition(command) {
  const lower = command.trim().toLowerCase();
  if (/^make(\s|$)/.test(lower)) return false;
  if (lower.includes("&&")) return true;
  if (lower.includes("||")) return true;
  if (lower.includes(";")) return true;
  if (lower.includes("|")) return true;
  if (lower.includes("`")) return true;
  if (lower.includes("$(")) return true;
  if (/[<>]/.test(lower)) return true;
  if (lower.includes("\n")) return true;
  return false;
}
function isDangerousGit(command) {
  const lower = command.trim().toLowerCase();
  if (!lower.startsWith("git ")) return false;
  const dangerous = [
    "git push --force",
    "git push -f",
    "git remote add",
    "git remote set-url",
    "git reset --hard",
    "git clean -f",
    "git reflog delete",
    "git update-ref -d",
    "git credential",
    "git gc"
  ];
  return dangerous.some((d) => lower.includes(d));
}
var SAFE_MAKE_TARGETS = /* @__PURE__ */ new Set([
  "build",
  "test",
  "check",
  "lint",
  "vet",
  "typecheck",
  "generate"
]);
function isSafeMakeCommand(command) {
  const lower = command.trim().toLowerCase();
  const hasMetachar = /[;&|`$]/.test(lower) || /[<>]/.test(lower) || lower.includes("\n") || lower.includes("\r");
  if (hasMetachar) return false;
  if (/^make$/.test(lower)) return true;
  const rest = lower.slice(5).trim();
  if (!rest) return true;
  const tokens = rest.split(/\s+/);
  for (const tok of tokens) {
    if (tok.startsWith("-")) return false;
    if (tok.includes("=")) return false;
    let ok = false;
    for (const safe of SAFE_MAKE_TARGETS) {
      if (tok === safe) {
        ok = true;
        break;
      }
      if (tok.startsWith(safe + "-") || tok.startsWith(safe + "_")) {
        ok = true;
        break;
      }
    }
    if (!ok) return false;
  }
  return true;
}
function matchesBuildOrTest(command) {
  const lower = command.trim().toLowerCase();
  const buildPatterns = [
    /^go\s+(build|install|mod)/,
    /^npm\s+run\s+(build|prod|compile|typecheck|check)/,
    /^npx\s+(tsc|esbuild|webpack)/,
    /^cargo\s+(build|check)/,
    /^dotnet\s+(build|publish)/,
    /^(gradle\s+build|mvn\s+(compile|package))/,
    /^bun\s+run\s+build/,
    /^yarn(\s+run)?\s+build/,
    /^tsc(\s|$)/
  ];
  const testPatterns = [
    /^go\s+(test|vet|fmt)/,
    /^npm\s+(test|run\s+test)/,
    /^npx\s+(jest|mocha|vitest)/,
    /^yarn\s+(test|run\s+test)/,
    /^bun\s+test/,
    /^cargo\s+test/,
    /^dotnet\s+test/,
    /^gradle\s+test/,
    /^mvn\s+test/,
    /^pytest/,
    /^rspec/,
    /^rails\s+test/
  ];
  if (/^make(\s|$)/.test(lower)) {
    if (!isSafeMakeCommand(command)) return false;
    return true;
  }
  return [...buildPatterns, ...testPatterns].some((p) => p.test(lower));
}
function gitHasSensitiveArgs(command) {
  const lower = command.trim().toLowerCase();
  if (!lower.startsWith("git ")) return false;
  if (lower.includes("--no-index")) return true;
  if (/HEAD\s*:\s*\.(env|ssh|git\/config)\b/i.test(command)) return true;
  const args = command.slice(3).trim();
  if (/(?:^|\s)\.env(?:\b|$)/.test(args)) return true;
  if (/\.ssh(?:\/|$)/.test(args)) return true;
  if (/\.pi(?:\/|$)/.test(args)) return true;
  if (/\.aurelia\/config(?:\/|$)/.test(args)) return true;
  if (/\.git\/config(?:\b|$)/.test(args)) return true;
  if (/~\/\./.test(args)) return true;
  return false;
}
function matchesSafeGit(command) {
  const lower = command.trim().toLowerCase();
  const safePrefixes = [
    "git status",
    "git diff",
    "git log",
    "git show",
    "git branch",
    "git checkout",
    "git stash list",
    "git describe",
    "git rev-parse",
    "git rev-list",
    // Intentionally excludes "git config": reveals remote tokens, credential
    // helpers, emails, and .git/config contents. Use /cwd or Read instead.
    "git ls-files",
    "git ls-tree",
    "git tag",
    "git blame",
    "git shortlog",
    "git cherry",
    "git cherry-pick --abort"
  ];
  return safePrefixes.some((p) => lower.startsWith(p));
}
function isPathInsideCwd(path, cwd, allowedOutside) {
  if (!path || !cwd) return true;
  const clean = path.replace(/\\/g, "/");
  const cwdNorm = cwd.replace(/\\/g, "/").replace(/\/+$/, "");
  if (clean === ".." || clean.startsWith("../")) return false;
  if (clean === ".") return true;
  if (!clean.startsWith("/")) {
    const resolved2 = resolve(cwdNorm, clean);
    const cwdResolved2 = resolve(cwdNorm);
    if (resolved2.startsWith(cwdResolved2 + "/") || resolved2 === cwdResolved2) {
      return true;
    }
    for (const allowed of allowedOutside) {
      const allowedResolved = resolve(allowed.replace(/\\/g, "/"));
      if (resolved2.startsWith(allowedResolved + "/") || resolved2 === allowedResolved) {
        return true;
      }
    }
    return false;
  }
  const resolved = resolve(clean);
  const cwdResolved = resolve(cwdNorm);
  if (!resolved.startsWith(cwdResolved + "/") && resolved !== cwdResolved) {
    for (const allowed of allowedOutside) {
      const allowedResolved = resolve(allowed.replace(/\\/g, "/"));
      if (resolved.startsWith(allowedResolved + "/") || resolved === allowedResolved) {
        return true;
      }
    }
    return false;
  }
  return true;
}
function isBlockedWebFetchURL(urlString) {
  let url;
  try {
    url = new URL(urlString);
  } catch {
    return { blocked: true, reason: "URL is invalid or unparseable" };
  }
  const hostname = url.hostname;
  if (!hostname) {
    return { blocked: true, reason: "URL has no hostname" };
  }
  const lower = hostname.toLowerCase();
  if (lower === "localhost" || lower === "localhost.localdomain" || lower === "localhost6" || lower === "localhost6.localdomain6") {
    return { blocked: true, reason: "URL targets loopback address" };
  }
  const checkIPv4 = (octets) => {
    if (octets[0] === 0) {
      return { blocked: true, reason: "URL targets unspecified address" };
    }
    if (octets[0] === 127) {
      return { blocked: true, reason: "URL targets loopback address" };
    }
    if (octets[0] === 169 && octets[1] === 254) {
      return { blocked: true, reason: "URL targets link-local address" };
    }
    if (octets[0] === 10) {
      return { blocked: true, reason: "URL targets private IP address" };
    }
    if (octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31) {
      return { blocked: true, reason: "URL targets private IP address" };
    }
    if (octets[0] === 192 && octets[1] === 168) {
      return { blocked: true, reason: "URL targets private IP address" };
    }
    return null;
  };
  const ipv4Match = hostname.match(/^(\d+)\.(\d+)\.(\d+)\.(\d+)$/);
  if (ipv4Match) {
    const octets = ipv4Match.slice(1).map(Number);
    if (octets.some((o) => o > 255)) {
      return { blocked: true, reason: "URL has invalid IP address" };
    }
    const blocked = checkIPv4(octets);
    if (blocked) return blocked;
    return { blocked: false };
  }
  if (hostname.includes(":")) {
    const ipv6 = hostname.toLowerCase().replace(/^\[|\]$/g, "");
    if (ipv6 === "::1" || ipv6 === "0:0:0:0:0:0:0:1") {
      return { blocked: true, reason: "URL targets loopback address" };
    }
    if (ipv6.startsWith("fc") || ipv6.startsWith("fd")) {
      return { blocked: true, reason: "URL targets unique local address (ULA)" };
    }
    let embeddedOctets = null;
    const ipv4MappedHex = ipv6.match(/^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/);
    if (ipv4MappedHex) {
      const hi = parseInt(ipv4MappedHex[1], 16);
      const lo = parseInt(ipv4MappedHex[2], 16);
      embeddedOctets = [
        hi >> 8 & 255,
        hi & 255,
        lo >> 8 & 255,
        lo & 255
      ];
    } else {
      const ipv4MappedMixed = ipv6.match(
        /^::ffff:(\d+)\.(\d+)\.(\d+)\.(\d+)$/
      );
      if (ipv4MappedMixed) {
        embeddedOctets = ipv4MappedMixed.slice(1).map(Number);
      }
    }
    if (embeddedOctets) {
      const blocked = checkIPv4(embeddedOctets);
      if (blocked) return blocked;
      return { blocked: false };
    }
    return { blocked: false };
  }
  return { blocked: false };
}
function evaluateToolPolicy(toolName, input, security) {
  const cfg = security;
  const mode = cfg.mode || "block";
  if (cfg.profile === "privileged") {
    return { decision: "allow", reason: "privileged profile bypass" };
  }
  switch (toolName) {
    case "Read":
    case "Grep":
    case "Glob":
    case "LS": {
      const path = input.path || "";
      if (!path) return { decision: "allow" };
      const patterns = cfg.sensitive_paths || DEFAULT_SENSITIVE_PATTERNS;
      if (isSensitivePath(path, patterns)) {
        const reason = "access to sensitive path blocked: ".concat(path);
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      if (cfg.cwd && !isPathInsideCwd(path, cfg.cwd, cfg.allowed_outside_cwd || [])) {
        const reason = "path outside working directory: ".concat(path);
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      return { decision: "allow" };
    }
    case "Write":
    case "Edit": {
      const path = input.path || "";
      if (!path) return { decision: "allow" };
      const patterns = cfg.sensitive_paths || DEFAULT_SENSITIVE_PATTERNS;
      if (isSensitivePath(path, patterns)) {
        return { decision: "block", reason: "write to sensitive path blocked: ".concat(path) };
      }
      if (cfg.cwd && !isPathInsideCwd(path, cfg.cwd, cfg.allowed_outside_cwd || [])) {
        const reason = "write to path outside working directory: ".concat(path);
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      return { decision: "allow" };
    }
    case "Bash": {
      const command = input.command || "";
      if (!command) return { decision: "allow" };
      if (isDestructiveCommand(command)) {
        const reason = "destructive command blocked: ".concat(redactedCommandExcerpt(command, 80));
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      if (isExfiltrationCommand(command)) {
        const reason = "exfiltration blocked: ".concat(redactedCommandExcerpt(command, 80));
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      if (matchesEnvAccess(command)) {
        const reason = "environment access blocked: command reads env vars or secrets";
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      if (isDangerousGit(command)) {
        const reason = "dangerous git operation blocked";
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      if (/^make(\s|$)/.test(command) && !isSafeMakeCommand(command)) {
        const reason = "unsafe make blocked: ".concat(redactedCommandExcerpt(command, 80));
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      if (hasShellComposition(command)) {
        const reason = "shell composition blocked: ".concat(redactedCommandExcerpt(command, 120));
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      if (/^git\s/.test(command.trim()) && gitHasSensitiveArgs(command)) {
        const reason = "git sensitive args blocked: ".concat(redactedCommandExcerpt(command, 120));
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      if (matchesSafeGit(command)) {
        return { decision: "allow", reason: "safe git command allowed" };
      }
      if (matchesBuildOrTest(command)) {
        return { decision: "allow", reason: "build/test command allowed" };
      }
      if (cfg.profile === "execute_safe") {
        const reason = "command not on allowlist: ".concat(redactedCommandExcerpt(command, 120));
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      return { decision: "allow" };
    }
    case "WebFetch": {
      const url = input.url || "";
      if (!url) {
        const reason = "WebFetch blocked: no URL provided";
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      const check = isBlockedWebFetchURL(url);
      if (check.blocked) {
        const reason = "WebFetch blocked: ".concat(check.reason);
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      return { decision: "allow" };
    }
    default:
      return { decision: "allow" };
  }
}
var auditLogMaxBytes = 5 * 1024 * 1024;
var auditLogBackups = 3;
var MAX_AUDIT_LINE_BYTES = 16 * 1024;
function auditLogPath() {
  const root = process.env.AURELIA_HOME?.trim() || join(homedir(), ".aurelia");
  return join(root, "audit.log");
}
function rotateAuditLogIfNeeded(path, incomingBytes) {
  try {
    if (!existsSync(path)) return;
    const size = statSync(path).size;
    if (size + incomingBytes <= auditLogMaxBytes) return;
    if (auditLogBackups <= 0) {
      truncateSync(path, 0);
      return;
    }
    for (let i = auditLogBackups - 1; i >= 1; i--) {
      const oldPath = "".concat(path, ".").concat(i);
      const newPath = "".concat(path, ".").concat(i + 1);
      if (existsSync(oldPath)) renameSync(oldPath, newPath);
    }
    renameSync(path, "".concat(path, ".1"));
  } catch {
  }
}
function writeAuditFile(line) {
  try {
    if (serializedByteLength(line) > MAX_AUDIT_LINE_BYTES) return;
    const path = auditLogPath();
    mkdirSync(dirname(path), { recursive: true, mode: 448 });
    rotateAuditLogIfNeeded(path, Buffer.byteLength(line));
    appendFileSync(path, line, { encoding: "utf8", mode: 384 });
  } catch {
  }
}
function logAudit(entry) {
  entry.timestamp = (/* @__PURE__ */ new Date()).toISOString();
  entry.redacted = true;
  const sanitizeAuditText = (value, maxRunes) => {
    const redacted = redactAuditPath(redactSDKError(typeof value === "string" ? value : String(value ?? "")));
    return truncateBridgeRunes(removeBridgeControls(redacted), maxRunes);
  };
  entry.decision = sanitizeAuditText(entry.decision, 64);
  entry.tool_name = sanitizeAuditText(entry.tool_name, 128);
  entry.reason = sanitizeAuditText(entry.reason, MAX_BRIDGE_LOG_RUNES);
  entry.cwd = sanitizeAuditText(entry.cwd, MAX_EVENT_VALUE_RUNES);
  entry.profile = sanitizeAuditText(entry.profile, 64);
  if (entry.agent_name) entry.agent_name = sanitizeAuditText(entry.agent_name, 128);
  let serialized = JSON.stringify(entry);
  if (serializedByteLength(serialized) > MAX_AUDIT_LINE_BYTES) {
    serialized = JSON.stringify({
      timestamp: entry.timestamp,
      decision: entry.decision,
      tool_name: entry.tool_name,
      profile: entry.profile,
      redacted: true,
      audit_truncated: true
    });
  }
  const line = "[security] " + serialized + "\n";
  process.stderr.write(line);
  writeAuditFile(line);
}
var AI_MEMORY_SERVER = "ai-memory";
var MEMORY_TOOL_PREFIX = "memory_";
function deriveProjectName(cwd) {
  if (!cwd) return void 0;
  let dir = resolve(cwd);
  for (; ; ) {
    if (existsSync(join(dir, ".git"))) {
      return basename(dir);
    }
    const parent = dirname(dir);
    if (parent === dir) return void 0;
    dir = parent;
  }
}
function injectMcpProjectScope(toolName, args, cwd) {
  if (typeof args !== "object" || args === null) return false;
  const a = args;
  let targetTool;
  let container;
  if (toolName === "mcp") {
    if (a.server !== AI_MEMORY_SERVER) return false;
    const t = a.tool;
    targetTool = typeof t === "string" ? t : void 0;
    container = a.args;
  } else if (toolName.startsWith(MEMORY_TOOL_PREFIX)) {
    targetTool = toolName;
    container = a;
  }
  if (!targetTool || !targetTool.startsWith(MEMORY_TOOL_PREFIX)) return false;
  let wasString = false;
  if (typeof container === "string") {
    wasString = true;
    try {
      container = JSON.parse(container);
    } catch {
      return false;
    }
  }
  if (typeof container !== "object" || container === null) return false;
  const target = container;
  if (target.project !== void 0 || target.workspace !== void 0 || target.scopes !== void 0 || target.global !== void 0) {
    return false;
  }
  const project = deriveProjectName(cwd);
  if (!project) return false;
  target.project = project;
  if (toolName === "mcp") {
    a.args = wasString ? JSON.stringify(target) : target;
  }
  return true;
}
function installSecurityHook(agent, security, audit = logAudit) {
  const origBeforeToolCall = agent.beforeToolCall;
  if (typeof origBeforeToolCall !== "function") {
    throw new Error("security hook not available: PI SDK version too old");
  }
  const { chat_id, agent_name, profile, cwd } = security;
  agent.beforeToolCall = async (ctx, signal) => {
    if (cwd) {
      try {
        const injected = injectMcpProjectScope(ctx.toolCall.name, ctx.args, cwd);
        if (injected) {
          redactedLog("mcp scope: injected project=".concat(deriveProjectName(cwd), " for tool=").concat(ctx.toolCall.name));
        }
      } catch (injectError) {
        redactedLog("mcp scope: injection failed, continuing: ".concat(injectError instanceof Error ? injectError.message : String(injectError)));
      }
    }
    const decision = evaluateToolPolicy(
      ctx.toolCall.name,
      ctx.args,
      security
    );
    audit({
      decision: decision.decision,
      tool_name: ctx.toolCall.name,
      reason: decision.reason || "",
      chat_id,
      agent_name,
      profile,
      cwd,
      redacted: true
    });
    if (decision.decision === "block") {
      redactedLog("security block: tool=".concat(ctx.toolCall.name, " reason=").concat(decision.reason));
      return { block: true, reason: decision.reason };
    }
    if (decision.decision === "rewrite" && decision.input) {
      if (typeof ctx.args === "object" && ctx.args !== null) {
        Object.assign(ctx.args, decision.input);
      }
    }
    try {
      return await origBeforeToolCall(ctx, signal);
    } catch (hookError) {
      redactedLog(
        "security: tool=".concat(ctx.toolCall.name, " extension hook threw, allowing: ").concat(hookError instanceof Error ? hookError.message : String(hookError))
      );
      return void 0;
    }
  };
  return () => {
    agent.beforeToolCall = origBeforeToolCall;
  };
}
function textFromContent(content) {
  if (!Array.isArray(content)) return "";
  let text = "";
  let usedRunes = 0;
  for (const item of content.slice(0, MAX_TOOL_INPUT_ITEMS)) {
    if (typeof item !== "object" || item === null) continue;
    const block = item;
    if (block.type !== "text" || typeof block.text !== "string") continue;
    const safeBlock = sanitizeBridgeText(block.text, MAX_EVENT_TEXT_RUNES);
    const piece = truncateBridgeRunes(safeBlock, MAX_EVENT_TEXT_RUNES - usedRunes);
    text += piece;
    usedRunes += Array.from(piece).length;
    if (usedRunes >= MAX_EVENT_TEXT_RUNES) break;
  }
  return text;
}
function textFromMessageContent(content) {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) return textFromContent(content);
  if (typeof content !== "object" || content === null) return "";
  const obj = content;
  if (typeof obj.text === "string") return obj.text;
  if (obj.content !== void 0) return textFromMessageContent(obj.content);
  return "";
}
function sessionMessageTimestampISO(msg) {
  const raw = msg.timestamp;
  if (typeof raw === "number" && Number.isFinite(raw)) {
    return new Date(raw).toISOString();
  }
  if (typeof raw === "string" && raw.trim() !== "") {
    const parsed = Date.parse(raw);
    if (Number.isFinite(parsed)) return new Date(parsed).toISOString();
  }
  return void 0;
}
function sessionHistoryFromMessages(messages, limit = 100) {
  const history = [];
  for (const raw of messages) {
    if (typeof raw !== "object" || raw === null) continue;
    const msg = raw;
    const role = typeof msg.role === "string" ? msg.role : "";
    const sender = role === "user" ? "Igor" : role === "assistant" ? "Aurelia" : "";
    if (!sender) continue;
    const text = textFromMessageContent(msg.content).trim();
    if (!text) continue;
    const timestamp = sessionMessageTimestampISO(msg);
    history.push({ sender, text, ...timestamp ? { timestamp } : {} });
  }
  if (history.length <= limit) return history;
  return history.slice(history.length - limit);
}
function boundSessionHistoryPayload(history, maxTextRunes = 400, maxPayloadRunes = 12e3) {
  const bounded = [];
  let budget = maxPayloadRunes;
  for (let i = history.length - 1; i >= 0; i--) {
    const msg = history[i];
    const text = truncateBridgeRunes(msg.text, maxTextRunes);
    const entry = {
      sender: msg.sender,
      text,
      ...msg.timestamp ? { timestamp: msg.timestamp } : {}
    };
    const cost = Array.from(JSON.stringify(entry)).length;
    if (budget - cost < 0) break;
    budget -= cost;
    bounded.push(entry);
  }
  bounded.reverse();
  return bounded;
}
function resolveModel(modelRuntime, provider, modelID) {
  if (!modelID) return void 0;
  const mappedProvider = mapProvider(provider);
  const mappedModel = mapModelForProvider(mappedProvider, modelID);
  if (mappedProvider) {
    const found = modelRuntime.getModel(mappedProvider, mappedModel);
    if (found) return found;
  }
  return modelRuntime.getModels().find((m) => m.id === mappedModel);
}
async function createModelRuntime(agentDir) {
  return ModelRuntime.create({
    authPath: join(agentDir, "auth.json"),
    modelsPath: join(agentDir, "models.json"),
    modelsStorePath: join(agentDir, "models-store.json"),
    allowModelNetwork: false
  });
}
async function resolveSessionManager(opts) {
  const cwd = opts?.cwd || process.cwd();
  if (opts?.persist_session === false) {
    return SessionManager.inMemory(cwd);
  }
  const target = opts?.resume || (opts?.continue ? lastSessionID : "");
  if (target) {
    const cached = sessionByID.get(target);
    if (cached?.file && existsSync(cached.file)) {
      return SessionManager.open(cached.file, void 0, cwd);
    }
    if (existsSync(target)) {
      return SessionManager.open(target, void 0, cwd);
    }
    const sessions = await SessionManager.listAll();
    const match = sessions.find((session) => session.id === target || session.id.startsWith(target));
    if (match) {
      return SessionManager.open(match.path, void 0, cwd);
    }
    redactedLog("session not found for resume=".concat(redactSDKError(target), "; starting a new session"));
  }
  return SessionManager.create(cwd);
}
var piSessionLock = Promise.resolve();
async function createPiSession(opts) {
  const prev = piSessionLock;
  let release;
  piSessionLock = new Promise((resolve2) => {
    release = resolve2;
  });
  await prev;
  try {
    const created = await createPiSessionInner(opts);
    markBridgeSessionExtensionRuntimeLoaded(created.session);
    return created;
  } finally {
    release();
  }
}
async function createPiSessionInner(opts) {
  const cwd = opts?.cwd || process.cwd();
  const agentDir = piAgentDir() || getAgentDir();
  const settingsManager = opts?.no_user_settings ? SettingsManager.inMemory({
    compaction: { enabled: true },
    retry: { enabled: true, maxRetries: 2 }
  }) : SettingsManager.create(cwd, agentDir);
  const modelRuntime = await createModelRuntime(agentDir);
  const model = resolveModel(modelRuntime, opts?.provider, opts?.model);
  if (opts?.model && !model) {
    throw new Error("Modelo n\xE3o encontrado no PI registry: provider=".concat(opts.provider ?? "", " model=").concat(opts.model, ". Use /model para listar os dispon\xEDveis."));
  }
  const resourceLoader = new DefaultResourceLoader({
    cwd,
    agentDir,
    settingsManager,
    noContextFiles: false,
    // let PI discover CLAUDE.md/AGENTS.md
    noExtensions: opts?.no_user_settings ?? false,
    noSkills: opts?.no_user_settings ?? false,
    noPromptTemplates: opts?.no_user_settings ?? false,
    noThemes: true,
    systemPromptOverride: () => opts?.system_prompt || void 0
  });
  await resourceLoader.reload();
  const sessionManager = await resolveSessionManager(opts);
  const effectiveTools = translateAllowedTools(opts?.allowed_tools, opts?.disallowed_tools);
  return createAgentSession({
    cwd,
    agentDir,
    modelRuntime,
    model,
    resourceLoader,
    sessionManager,
    settingsManager,
    tools: effectiveTools
  });
}
async function bindBridgeSessionExtensions(session) {
  const lifecycle = bridgeSessionLifecycleFor(session);
  if (lifecycle.bindingCompleted) return;
  lifecycle.extensionActivationStarted = true;
  try {
    await session.bindExtensions({ mode: "print" });
    lifecycle.bindingCompleted = true;
  } catch (err) {
    await disposeBridgeSession(session);
    throw err;
  }
}
function createBridgeSessionRequestLifecycle() {
  let trackedSession;
  let sessionCreationComplete = false;
  let resolveSessionCreation;
  const sessionCreated = new Promise((resolve2) => {
    resolveSessionCreation = resolve2;
  });
  let canceled = false;
  let inFlightBind;
  let cancelPromise;
  const trackSession = (session) => {
    if (trackedSession && trackedSession !== session) {
      throw new Error("bridge request cannot track more than one session");
    }
    trackedSession = session;
    markBridgeSessionExtensionRuntimeLoaded(session);
  };
  const markSessionCreationComplete = () => {
    if (sessionCreationComplete) return;
    sessionCreationComplete = true;
    resolveSessionCreation();
  };
  const createSession = async (factory) => {
    try {
      const created = await factory();
      trackSession(created.session);
      return created;
    } finally {
      markSessionCreationComplete();
    }
  };
  const bindSession = async (session) => {
    if (trackedSession !== session) {
      throw new Error("bridge request bind does not match its tracked session");
    }
    if (canceled) {
      await disposeBridgeSession(session);
      throw new Error("request canceled");
    }
    const bindPromise = bindBridgeSessionExtensions(session);
    const pending = { session, promise: bindPromise };
    inFlightBind = pending;
    try {
      await bindPromise;
    } finally {
      if (inFlightBind === pending) inFlightBind = void 0;
    }
  };
  const cancel = () => {
    canceled = true;
    if (!cancelPromise) {
      cancelPromise = (async () => {
        await sessionCreated;
        const pending = inFlightBind;
        if (pending) {
          try {
            await pending.promise;
          } catch {
          }
        }
        if (trackedSession) {
          await disposeBridgeSession(trackedSession);
        }
      })();
    }
    return cancelPromise;
  };
  return {
    createSession,
    trackSession,
    markSessionCreationComplete,
    isCanceled: () => canceled,
    bindSession,
    cancel
  };
}
async function handleQuery(req) {
  const reqId = req.request_id || "";
  const opts = req.options;
  const chatID = opts?.chat_id || opts?.security?.chat_id || 0;
  const threadID = opts?.thread_id ?? opts?.security?.thread_id ?? 0;
  const userID = opts?.user_id ?? opts?.security?.user_id ?? 0;
  const cKey = chatKey(chatID, threadID, userID);
  const sessionOwner = claimChatSessionOwner(cKey);
  const emitReq = (obj) => emit({ ...obj, request_id: reqId });
  const toolDurations = new ToolDurationTracker();
  let compactionStartAt;
  let compactionEndObserved = false;
  redactedLog(
    "query start \u2014 rid=".concat(reqId, " chat=").concat(chatID, " thread=").concat(threadID, " user=").concat(userID, " provider=").concat(opts?.provider ?? "default", " model=").concat(opts?.model ?? "default", " resume=").concat(opts?.resume ?? "none")
  );
  const timeoutMs = 30 * 60 * 1e3;
  let timeout;
  let canceled = false;
  let terminalEmitted = false;
  let turnCount = 0;
  let session;
  let healthTimer;
  const startedAt = Date.now();
  const sessionLifecycle = createBridgeSessionRequestLifecycle();
  const emitTerminalError = (message) => {
    if (terminalEmitted) return;
    terminalEmitted = true;
    emitReq({ event: "error", message: redactSDKError(message) });
  };
  const cancelActive = async (reason) => {
    if (canceled) return;
    canceled = true;
    redactedLog("query cancel \u2014 rid=".concat(reqId, " reason=").concat(reason));
    const cleanup = cleanupChatSession(cKey, sessionOwner);
    emitTerminalError(reason);
    activeRequests.delete(reqId);
    await Promise.all([cleanup, sessionLifecycle.cancel()]);
  };
  activeRequests.set(reqId, { cancel: cancelActive });
  timeout = setTimeout(async () => {
    redactedLog("query timeout \u2014 rid=".concat(reqId, " no result after 30 minutes"));
    await cancelActive("query timeout: no result after 30 minutes");
  }, timeoutMs);
  try {
    await cleanupChatSession(cKey);
    if (canceled || !ownsChatSession(cKey, sessionOwner)) {
      throw new Error("request canceled");
    }
    const effectiveToolNames = translateAllowedTools(
      opts?.allowed_tools,
      opts?.disallowed_tools
    ) ?? [];
    const piSession = await sessionLifecycle.createSession(() => createPiSession(opts));
    const liveSession = piSession.session;
    session = liveSession;
    if (canceled || !ownsChatSession(cKey, sessionOwner)) {
      await disposeBridgeSession(liveSession);
      throw new Error("request canceled");
    }
    await sessionLifecycle.bindSession(liveSession);
    if (canceled || !ownsChatSession(cKey, sessionOwner)) {
      await disposeBridgeSession(liveSession);
      throw new Error("request canceled");
    }
    const sessionID = liveSession.sessionId;
    lastSessionID = sessionID;
    rememberSession(sessionID, { id: sessionID, file: liveSession.sessionFile });
    const piActive = liveSession.getActiveToolNames();
    const hasMcpProxy = effectiveToolNames.includes("mcp");
    const hasWebSearch = effectiveToolNames.includes("web_search");
    const profile = opts?.security?.profile ?? "none";
    redactedLog(
      "session tools \u2014 rid=".concat(reqId, " profile=").concat(profile, " ") + "active=[".concat(effectiveToolNames.join(","), "] ") + "pi_active=[".concat(piActive.join(","), "] ") + "mcp_proxy=".concat(hasMcpProxy ? "on" : "off", " web_search=").concat(hasWebSearch ? "on" : "off")
    );
    emitReq({
      event: "system",
      session_id: sessionID,
      session_file: liveSession.sessionFile,
      tools: effectiveToolNames,
      model: liveSession.model ? "".concat(liveSession.model.provider, "/").concat(liveSession.model.id) : ""
    });
    let lastEventTime = Date.now();
    const rawUnsubPersistent = liveSession.subscribe((event) => {
      try {
        if (terminalEmitted) return;
        const sdkEvent = event;
        const rid = safeRequestID(chatSessions.get(cKey)?.currentReqId || reqId);
        const eReq = (obj) => emit({ ...obj, request_id: rid });
        switch (sdkEvent?.type) {
          // lastEventTime is only updated on content-producing events so lifecycle
          // events (turn_start/end, compactions, retries) don't mask real stalls.
          case "message_update": {
            lastEventTime = Date.now();
            const update = sdkEvent.assistantMessageEvent;
            if (update?.type === "text_delta" && typeof update.delta === "string") {
              eReq({ event: "assistant", text: sanitizeBridgeText(update.delta, MAX_EVENT_TEXT_RUNES) });
            }
            break;
          }
          case "tool_execution_start": {
            lastEventTime = Date.now();
            const safeID = toolDurations.start(sdkEvent.toolCallId);
            const safeName = safeLabel(sdkEvent.toolName, "tool");
            redactedLog("tool: ".concat(safeName, " id=").concat(safeID, " rid=").concat(rid));
            eReq({
              event: "tool_use",
              // Keep the legacy id field while adding the explicit field used
              // by Go to pair the safe start/result identifiers.
              id: safeID,
              tool_call_id: safeID,
              name: safeName,
              input: sanitizeToolInput(sdkEvent.args)
            });
            break;
          }
          case "tool_execution_end": {
            lastEventTime = Date.now();
            const pair = toolDurations.endWithID(sdkEvent.toolCallId);
            const safeID = pair?.toolCallID ?? safeToolCallID(sdkEvent.toolCallId);
            eReq({
              event: "tool_result",
              content: textFromContent(sdkEvent.result?.content),
              tool_call_id: safeID,
              ...pair ? { duration_measured: true, duration_ms: pair.durationMs } : {}
            });
            break;
          }
          case "agent_start":
            eReq({ event: "agent_start" });
            break;
          case "agent_end":
            eReq({ event: "agent_end" });
            break;
          case "turn_start":
            eReq({ event: "turn_start" });
            break;
          case "turn_end":
            turnCount = Math.min(turnCount + 1, MAX_COUNTER_VALUE);
            eReq({ event: "turn_end" });
            break;
          case "auto_retry_start":
            eReq({
              event: "auto_retry_start",
              attempt: boundedCounter(sdkEvent.attempt),
              max_attempts: boundedCounter(sdkEvent.maxAttempts),
              error: safeLabel(sdkEvent.errorMessage, "unknown")
            });
            break;
          case "auto_retry_end":
            eReq({
              event: "auto_retry_end",
              success: sdkEvent.success === true,
              attempt: boundedCounter(sdkEvent.attempt),
              error: safeLabel(sdkEvent.finalError, "unknown")
            });
            break;
          case "compaction_start":
            if (compactionStartAt === void 0) {
              compactionStartAt = Date.now();
              compactionEndObserved = false;
              eReq({ event: "compaction_start", reason: compactionReason(sdkEvent.reason) });
            }
            break;
          case "compaction_end": {
            if (compactionEndObserved) break;
            compactionEndObserved = true;
            const result = sdkEvent.result;
            const tokensBefore = boundedCounter(result?.tokensBefore) ?? 0;
            const tokensAfter = boundedCounter(result?.estimatedTokensAfter);
            const durationMs = measuredElapsed(compactionStartAt);
            compactionStartAt = void 0;
            eReq(compactionEndPayload({
              reason: compactionReason(sdkEvent.reason),
              tokensBefore,
              success: !!result && sdkEvent.aborted !== true && typeof sdkEvent.errorMessage !== "string",
              errored: !result || sdkEvent.aborted === true || typeof sdkEvent.errorMessage === "string",
              tokensAfter,
              durationMs
            }));
            break;
          }
          default:
            break;
        }
      } catch (err) {
        redactedLog("malformed SDK event ignored: ".concat(err instanceof Error ? err.message : String(err)));
      }
    });
    const unsubPersistent = () => {
      toolDurations.clear();
      compactionStartAt = void 0;
      compactionEndObserved = false;
      rawUnsubPersistent();
    };
    let stallSteerSent = false;
    let stallUrgentSent = false;
    healthTimer = setInterval(() => {
      if (terminalEmitted || canceled) {
        clearInterval(healthTimer);
        return;
      }
      const silent = Date.now() - lastEventTime;
      if (silent < 3e4) {
        stallSteerSent = false;
        stallUrgentSent = false;
      }
      if (silent >= 3e4) {
        redactedLog(
          "streaming stall: no PI SDK events for ".concat(Math.round(silent / 1e3), "s (rid=").concat(reqId, ")")
        );
      }
      const telemetry = stallTelemetryFor(silent, stallSteerSent, stallUrgentSent);
      if (telemetry.stall === "warning") {
        stallSteerSent = true;
        emitReq({
          event: "stall",
          severity: "warning",
          silent_ms: silent,
          source: "bridge_health"
        });
      }
      if (telemetry.stall === "urgent") {
        stallUrgentSent = true;
        emitReq({
          event: "stall",
          severity: "urgent",
          silent_ms: silent,
          source: "bridge_health"
        });
      }
      if (telemetry.steer === "warning") {
        try {
          liveSession.steer(
            "Continue please. You have been silent for over a minute. If you have finished your current task, present your findings."
          ).then(() => {
            emitReq({
              event: "steer",
              severity: "warning",
              silent_ms: silent,
              source: "bridge_health"
            });
            redactedLog("stall steer sent at ".concat(Math.round(silent / 1e3), "s (rid=").concat(reqId, ")"));
          }).catch((err) => {
            redactedLog("stall steer failed: ".concat(err instanceof Error ? err.message : String(err)));
          });
        } catch (err) {
          redactedLog("stall steer failed (sync): ".concat(err instanceof Error ? err.message : String(err)));
        }
      }
      if (telemetry.steer === "urgent") {
        try {
          liveSession.steer(
            "You have been silent for over 2 minutes. Stop your current activity and present a summary of what you have done so far."
          ).then(() => {
            emitReq({
              event: "steer",
              severity: "urgent",
              silent_ms: silent,
              source: "bridge_health"
            });
            redactedLog("stall urgent steer sent at ".concat(Math.round(silent / 1e3), "s (rid=").concat(reqId, ")"));
          }).catch((err) => {
            redactedLog("stall urgent steer failed: ".concat(err instanceof Error ? err.message : String(err)));
          });
        } catch (err) {
          redactedLog("stall urgent steer failed (sync): ".concat(err instanceof Error ? err.message : String(err)));
        }
      }
    }, 15e3);
    let unsubHook;
    if (opts?.security?.enabled) {
      unsubHook = installSecurityHook(liveSession.agent, opts.security);
    }
    const cs = {
      session: liveSession,
      owner: sessionOwner,
      sessionId: sessionID,
      sessionFile: liveSession.sessionFile,
      currentReqId: reqId,
      unsubPersistent,
      unsubHook,
      createdAt: Date.now()
    };
    if (!registerSessionIfOwner(chatSessionOwners, chatSessions, cKey, sessionOwner, cs)) {
      await disposeBridgeSession(liveSession);
      throw new Error("request canceled");
    }
    if (canceled || !ownsChatSession(cKey, sessionOwner)) {
      const current = chatSessions.get(cKey);
      if (current?.session === liveSession && current.owner === sessionOwner) {
        await cleanupChatSession(cKey, sessionOwner);
      } else {
        await disposeBridgeSession(liveSession);
      }
      throw new Error("request canceled");
    }
    try {
      const images = opts?.images;
      if (images && images.length > 0) {
        const contentBlocks = [{ type: "text", text: req.prompt }];
        for (const img of images) {
          if (!img.data || !img.media_type) {
            redactedLog("query image skipped: missing inline data or media type rid=".concat(reqId));
            continue;
          }
          contentBlocks.push({
            type: "image",
            data: img.data,
            mimeType: img.media_type
          });
        }
        await liveSession.sendUserMessage(contentBlocks);
      } else {
        await liveSession.prompt(req.prompt, { source: "rpc" });
      }
    } finally {
    }
    if (!terminalEmitted && !canceled) {
      const stats = liveSession.getSessionStats();
      const piError = liveSession.state.errorMessage;
      const lastMessages = liveSession.state.messages ?? [];
      let stopReason;
      let lastErrorMsg;
      if (lastMessages.length > 0) {
        const lastMsg = lastMessages[lastMessages.length - 1];
        if (lastMsg.role === "assistant") {
          stopReason = lastMsg.stopReason;
          lastErrorMsg = lastMsg.errorMessage;
        }
      }
      const hasExplicitError = piError || stopReason === "error" || lastErrorMsg;
      const zeroTokens = stats.tokens.input === 0 && stats.tokens.output === 0 && stats.cost === 0;
      const silentFailure = zeroTokens && (turnCount > 0 || stats.assistantMessages > 0);
      const noWorkDone = zeroTokens && stats.assistantMessages === 0 && turnCount === 0;
      if (hasExplicitError || silentFailure || noWorkDone) {
        const errMsg = piError || lastErrorMsg || (stopReason === "error" ? "PI SDK error (stopReason=error, 0 tokens)" : "") || (silentFailure ? "PI SDK completed with 0 tokens \u2014 possible API error (check provider credits/auth)" : "") || (noWorkDone ? "PI SDK completed with no work done" : "") || "PI SDK returned error state";
        redactedLog("query PI SDK error: rid=".concat(reqId, " stopReason=").concat(stopReason ?? "unknown", " ").concat(errMsg));
        emitTerminalError(errMsg);
      }
    }
    if (!terminalEmitted && !canceled) {
      const stats = liveSession.getSessionStats();
      const content = liveSession.getLastAssistantText() ?? "";
      terminalEmitted = true;
      emitReq({
        event: "result",
        content,
        cost_usd: stats.cost,
        session_id: sessionID,
        session_file: liveSession.sessionFile,
        duration_ms: Date.now() - startedAt,
        num_turns: turnCount || stats.assistantMessages,
        input_tokens: stats.tokens.input,
        output_tokens: stats.tokens.output
      });
      const currentCs = chatSessions.get(cKey);
      if (currentCs?.owner === sessionOwner) {
        startIdleTimer(currentCs, cKey);
      }
    }
  } catch (err) {
    if (session) {
      const current = chatSessions.get(cKey);
      if (current?.session === session && current.owner === sessionOwner && ownsChatSession(cKey, sessionOwner)) {
        await cleanupChatSession(cKey, sessionOwner);
      } else {
        await disposeBridgeSession(session);
      }
    }
    if (!terminalEmitted) {
      const rawErrMsg = err instanceof Error ? err.message : String(err);
      const isBilling = isBillingError(rawErrMsg);
      const userFriendlyMsg = isBilling ? "Provider sem cr\xE9ditos suficientes. Troque o modelo com /model ou adicione cr\xE9ditos." : rawErrMsg;
      redactedLog("query error: rid=".concat(reqId, " ").concat(rawErrMsg));
      emitTerminalError(userFriendlyMsg);
    }
  } finally {
    if (timeout) clearTimeout(timeout);
    if (healthTimer) clearInterval(healthTimer);
    activeRequests.delete(reqId);
    toolDurations.clear();
    compactionStartAt = void 0;
    sessionLifecycle.markSessionCreationComplete();
    if (!chatSessions.has(cKey) && ownsChatSession(cKey, sessionOwner)) {
      chatSessionOwners.delete(cKey);
    }
  }
}
async function handleSteer(req) {
  const reqId = req.request_id || "";
  const chatID = req.options?.chat_id || req.options?.security?.chat_id || 0;
  const threadID = req.options?.thread_id ?? req.options?.security?.thread_id ?? 0;
  const userID = req.options?.user_id ?? req.options?.security?.user_id ?? 0;
  const cKey = chatKey(chatID, threadID, userID);
  const emitReq = (obj) => emit({ ...obj, request_id: reqId });
  const cs = chatSessions.get(cKey);
  if (!cs) {
    emitReq({ event: "result", content: "no active session" });
    return;
  }
  clearTimeout(cs.idleTimer);
  redactedLog("steer \u2014 rid=".concat(reqId, " chat=").concat(chatID, " thread=").concat(threadID, " user=").concat(userID));
  try {
    await cs.session.steer(req.prompt);
    emitReq({ event: "result", content: "steer queued" });
  } catch (err) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog("steer error: rid=".concat(reqId, " ").concat(errMsg));
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    startIdleTimer(cs, cKey);
  }
}
async function handleFollowUp(req) {
  const reqId = req.request_id || "";
  const chatID = req.options?.chat_id || req.options?.security?.chat_id || 0;
  const threadID = req.options?.thread_id ?? req.options?.security?.thread_id ?? 0;
  const userID = req.options?.user_id ?? req.options?.security?.user_id ?? 0;
  const cKey = chatKey(chatID, threadID, userID);
  const emitReq = (obj) => emit({ ...obj, request_id: reqId });
  const cs = chatSessions.get(cKey);
  if (!cs) {
    emitReq({ event: "result", content: "no active session" });
    return;
  }
  clearTimeout(cs.idleTimer);
  redactedLog("followUp \u2014 rid=".concat(reqId, " chat=").concat(chatID, " thread=").concat(threadID, " user=").concat(userID));
  try {
    await cs.session.followUp(req.prompt);
    emitReq({ event: "result", content: "follow-up queued" });
  } catch (err) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog("followUp error: rid=".concat(reqId, " ").concat(errMsg));
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    startIdleTimer(cs, cKey);
  }
}
async function handleAbort(req) {
  const reqId = req.request_id || "";
  const chatID = req.options?.chat_id || req.options?.security?.chat_id || 0;
  const threadID = req.options?.thread_id ?? req.options?.security?.thread_id ?? 0;
  const userID = req.options?.user_id ?? req.options?.security?.user_id ?? 0;
  const cKey = chatKey(chatID, threadID, userID);
  const emitReq = (obj) => emit({ ...obj, request_id: reqId });
  const cs = chatSessions.get(cKey);
  if (!cs) {
    emitReq({ event: "result", content: "no active session" });
    return;
  }
  redactedLog("abort \u2014 rid=".concat(reqId, " chat=").concat(chatID, " thread=").concat(threadID, " user=").concat(userID));
  try {
    await cs.session.abort();
    emitReq({ event: "result", content: "session aborted" });
  } catch (err) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog("abort error: rid=".concat(reqId, " ").concat(errMsg));
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    await cleanupChatSession(cKey, cs.owner);
  }
}
async function handleGetState(req) {
  const reqId = req.request_id || "";
  const chatID = req.options?.chat_id || req.options?.security?.chat_id || 0;
  const threadID = req.options?.thread_id ?? req.options?.security?.thread_id ?? 0;
  const userID = req.options?.user_id ?? req.options?.security?.user_id ?? 0;
  const cKey = chatKey(chatID, threadID, userID);
  const emitReq = (obj) => emit({ ...obj, request_id: reqId });
  const cs = chatSessions.get(cKey);
  if (!cs) {
    emitReq({ event: "result", content: JSON.stringify({ is_streaming: false, pending_count: 0, session_id: "" }) });
    return;
  }
  emitReq({
    event: "result",
    content: JSON.stringify({
      is_streaming: cs.session.isStreaming,
      pending_count: cs.session.pendingMessageCount,
      session_id: cs.sessionId
    })
  });
}
async function handleGetSessionStats(req) {
  const reqId = req.request_id || "";
  const emitReq = (obj) => emit({ ...obj, request_id: reqId });
  let piSession;
  try {
    piSession = await createPiSession(req.options);
    const session = piSession.session;
    await bindBridgeSessionExtensions(session);
    const stats = session.getSessionStats();
    const usage = stats.contextUsage;
    emitReq({
      event: "result",
      content: JSON.stringify({
        session_file: stats.sessionFile,
        session_id: stats.sessionId,
        user_messages: stats.userMessages,
        assistant_messages: stats.assistantMessages,
        tool_calls: stats.toolCalls,
        tool_results: stats.toolResults,
        total_messages: stats.totalMessages,
        input_tokens: stats.tokens.input,
        output_tokens: stats.tokens.output,
        cache_read_tokens: stats.tokens.cacheRead,
        cache_write_tokens: stats.tokens.cacheWrite,
        total_tokens: stats.tokens.total,
        cost: stats.cost,
        context_usage_pct: usage?.percent ?? 0
      })
    });
  } catch (err) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog("get-session-stats error: rid=".concat(reqId, " ").concat(errMsg));
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    if (piSession) {
      await disposeBridgeSession(piSession.session);
    }
  }
}
async function handleGetSessionHistory(req) {
  const reqId = req.request_id || "";
  const emitReq = (obj) => emit({ ...obj, request_id: reqId });
  const opts = req.options;
  if (!opts?.resume) {
    emitReq({ event: "result", content: "[]" });
    return;
  }
  if (!existsSync(opts.resume)) {
    redactedLog("get-session-history skipped missing resume=".concat(redactSDKError(opts.resume)));
    emitReq({ event: "result", content: "[]" });
    return;
  }
  let session;
  try {
    const piSession = await createPiSession({ ...opts, continue: false });
    session = piSession.session;
    await bindBridgeSessionExtensions(session);
    const messages = session.state.messages ?? [];
    const history = sessionHistoryFromMessages(messages, 100);
    emitReq({ event: "result", content: JSON.stringify(boundSessionHistoryPayload(history)) });
  } catch (err) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog("get-session-history error: rid=".concat(reqId, " ").concat(errMsg));
    emitReq({ event: "error", message: "get-session-history failed: ".concat(redactSDKError(errMsg)) });
  } finally {
    if (session) {
      await disposeBridgeSession(session);
    }
  }
}
async function handleCompactSession(req) {
  const reqId = req.request_id || "";
  const emitReq = (obj) => emit({ ...obj, request_id: reqId });
  let piSession;
  let session;
  let unsub;
  let canceled = false;
  let cancelPromise;
  let resolveCompactDone;
  const compactDone = new Promise((resolve2) => {
    resolveCompactDone = resolve2;
  });
  const sessionLifecycle = createBridgeSessionRequestLifecycle();
  const cancelActive = async (reason) => {
    if (cancelPromise) return cancelPromise;
    canceled = true;
    redactedLog("compact-session cancel \u2014 rid=".concat(reqId, " reason=").concat(reason));
    cancelPromise = (async () => {
      if (session) {
        try {
          session.abortCompaction();
        } catch (err) {
          redactedLog("compact-session abort failed: ".concat(err instanceof Error ? err.message : String(err)));
        }
      } else {
        await sessionLifecycle.cancel();
      }
      await compactDone;
    })();
    return cancelPromise;
  };
  activeRequests.set(reqId, { cancel: cancelActive });
  let compactionStartAt;
  let terminalEmitted = false;
  let sdkCompactionStartObserved = false;
  let sdkCompactionEndObserved = false;
  const emitCompactionEndIfNeeded = (result, success) => {
    if (!sdkCompactionStartObserved || sdkCompactionEndObserved) return;
    sdkCompactionEndObserved = true;
    const tokensBefore = boundedCounter(result?.tokensBefore) ?? 0;
    const tokensAfter = boundedCounter(result?.estimatedTokensAfter);
    const durationMs = measuredElapsed(compactionStartAt);
    compactionStartAt = void 0;
    emitReq(compactionEndPayload({
      reason: "manual",
      tokensBefore,
      success,
      errored: !success,
      tokensAfter,
      durationMs
    }));
  };
  try {
    piSession = await sessionLifecycle.createSession(() => createPiSession(req.options));
    session = piSession.session;
    if (canceled) throw new Error("compact-session canceled");
    await sessionLifecycle.bindSession(session);
    if (canceled) throw new Error("compact-session canceled");
    unsub = session.subscribe((event) => {
      try {
        if (terminalEmitted) return;
        const sdkEvent = event;
        if (sdkEvent?.type === "compaction_start") {
          if (sdkCompactionStartObserved) return;
          sdkCompactionStartObserved = true;
          compactionStartAt = Date.now();
          emitReq({ event: "compaction_start", reason: compactionReason(sdkEvent.reason) });
        } else if (sdkEvent?.type === "compaction_end") {
          if (sdkCompactionEndObserved) return;
          sdkCompactionEndObserved = true;
          const result2 = sdkEvent.result;
          const tokensBefore = boundedCounter(result2?.tokensBefore) ?? 0;
          const tokensAfter = boundedCounter(result2?.estimatedTokensAfter);
          const durationMs = measuredElapsed(compactionStartAt);
          compactionStartAt = void 0;
          emitReq(compactionEndPayload({
            reason: compactionReason(sdkEvent.reason),
            tokensBefore,
            success: !!result2 && sdkEvent.aborted !== true && typeof sdkEvent.errorMessage !== "string",
            errored: !result2 || sdkEvent.aborted === true || typeof sdkEvent.errorMessage === "string",
            tokensAfter,
            durationMs
          }));
        }
      } catch (err) {
        redactedLog("malformed compaction SDK event ignored: ".concat(err instanceof Error ? err.message : String(err)));
      }
    });
    const customInstructions = req.prompt || void 0;
    const result = await session.compact(customInstructions);
    if (canceled) throw new Error("compact-session canceled");
    if (!terminalEmitted) {
      const tokensBefore = boundedCounter(result?.tokensBefore) ?? 0;
      const tokensAfter = boundedCounter(result?.estimatedTokensAfter);
      if (!sdkCompactionEndObserved) {
        const durationMs = measuredElapsed(compactionStartAt);
        compactionStartAt = void 0;
        emitReq(compactionEndPayload({
          reason: "manual",
          tokensBefore,
          success: !!result,
          errored: !result,
          tokensAfter,
          durationMs
        }));
      }
      terminalEmitted = true;
      emitReq({
        event: "result",
        content: JSON.stringify({
          success: !!result,
          tokens_before: tokensBefore,
          summary: sanitizeBridgeText(result?.summary, MAX_EVENT_TEXT_RUNES),
          session_id: sanitizeBridgeText(session.sessionId, MAX_TELEMETRY_LABEL_RUNES),
          session_file: sanitizeBridgeText(session.sessionFile, MAX_EVENT_VALUE_RUNES)
        })
      });
    }
  } catch (err) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog("compact-session error: rid=".concat(reqId, " ").concat(errMsg));
    emitCompactionEndIfNeeded(void 0, false);
    if (!terminalEmitted) {
      terminalEmitted = true;
      emitReq({
        event: "error",
        message: canceled ? "compact-session canceled" : "compact-session failed"
      });
    }
  } finally {
    activeRequests.delete(reqId);
    if (unsub) try {
      unsub();
    } catch {
    }
    compactionStartAt = void 0;
    if (piSession) {
      await disposeBridgeSession(piSession.session);
    }
    sessionLifecycle.markSessionCreationComplete();
    resolveCompactDone();
  }
}
async function waitForPendingMessageCount(session, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (session.pendingMessageCount > 0) {
    if (Date.now() >= deadline) break;
    await new Promise((resolve2) => setTimeout(resolve2, 50));
  }
}
async function handleRotateSession(req) {
  const reqId = req.request_id || "";
  const opts = req.options;
  const emitReq = (obj) => emit({ ...obj, request_id: reqId });
  let oldPiSession;
  let newPiSession;
  try {
    oldPiSession = await createPiSession(opts);
    const oldSession = oldPiSession.session;
    await bindBridgeSessionExtensions(oldSession);
    const stats = oldSession.getSessionStats();
    const compactionResult = await oldSession.compact(
      "Summarize the session's goal, current state, completed work, in-progress items, key decisions, files read/modified, and next actions."
    );
    const summary = escapeUntrustedSummary(compactionResult?.summary ?? "");
    const sessionId = oldSession.sessionId;
    const oldFile = oldSession.sessionFile;
    redactedLog("rotate: old session id=".concat(sessionId, " file=").concat(oldFile, " tokens=").concat(stats.tokens.total));
    emitReq({ event: "system", message: "Generating session summary..." });
    await disposeBridgeSession(oldSession);
    oldPiSession = void 0;
    const newOpts = { ...opts };
    newOpts.resume = void 0;
    newOpts.continue = false;
    newOpts.persist_session = true;
    newPiSession = await createPiSession(newOpts);
    const newSession = newPiSession.session;
    await bindBridgeSessionExtensions(newSession);
    const newFile = newSession.sessionFile;
    const newId = newSession.sessionId;
    redactedLog("rotate: new session id=".concat(newId, " file=").concat(newFile));
    const summaryBlock = "<previous_session_summary_untrusted>\n".concat(summary, "\n</previous_session_summary_untrusted>");
    await newSession.sendUserMessage([{ type: "text", text: summaryBlock }]);
    await waitForPendingMessageCount(newSession, 5e3);
    emitReq({
      event: "result",
      content: JSON.stringify({
        success: true,
        old_session_file: oldFile,
        old_session_id: sessionId,
        new_session_file: newFile,
        new_session_id: newId,
        summary_length: summary.length,
        tokens_before: stats.tokens.total
      })
    });
    await disposeBridgeSession(newSession);
    newPiSession = void 0;
  } catch (err) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog("rotate-session error: rid=".concat(reqId, " ").concat(errMsg));
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    if (oldPiSession) {
      await disposeBridgeSession(oldPiSession.session);
    }
    if (newPiSession) {
      await disposeBridgeSession(newPiSession.session);
    }
  }
}
async function handleRequest(line) {
  if (serializedByteLength(line) > MAX_BRIDGE_REQUEST_BYTES) {
    const requestID = safeRequestIDFromLine(line);
    emit({ event: "error", ...requestID ? { request_id: requestID } : {}, message: "request exceeds maximum serialized size" });
    return;
  }
  let parsed;
  try {
    parsed = JSON.parse(line);
  } catch {
    emit({ event: "error", message: "invalid JSON" });
    return;
  }
  const validation = validateBridgeRequest(parsed);
  if (!validation.ok) {
    const candidate = typeof parsed === "object" && parsed !== null && !Array.isArray(parsed) ? parsed : void 0;
    const requestID = safeRequestID(candidate?.request_id);
    emit({
      event: "error",
      ...requestID ? { request_id: requestID } : {},
      message: "invalid bridge request"
    });
    return;
  }
  const req = validation.request;
  const reqId = req.request_id;
  const emitReq = (obj) => emit({ ...obj, request_id: reqId });
  if (activeRequests.has(reqId)) {
    emitReq({ event: "error", message: "duplicate request_id" });
    return;
  }
  if (req.command === "cancel") {
    const target = req.target_request_id;
    const active = activeRequests.get(target);
    if (!active) {
      emitReq({ event: "result", content: "request ".concat(target, " is not active") });
      return;
    }
    await active.cancel("request canceled");
    emitReq({ event: "result", content: "request ".concat(target, " canceled") });
    return;
  }
  if (!stdoutEmissionBudget.register(reqId)) {
    emitReq({ event: "error", message: "too many active bridge streams" });
    return;
  }
  try {
    switch (req.command) {
      case "query": {
        await handleQuery(req);
        break;
      }
      case "ping": {
        emit({ event: "pong", request_id: reqId });
        break;
      }
      case "list-models": {
        try {
          const agentDir = piAgentDir() || getAgentDir();
          const modelRuntime = await createModelRuntime(agentDir);
          if (req.refresh) {
            const controller = new AbortController();
            const timeout = setTimeout(() => controller.abort(), 15e3);
            try {
              const result = await modelRuntime.refresh({
                allowNetwork: true,
                force: true,
                signal: controller.signal
              });
              if (result.aborted) {
                redactedLog("list-models refresh aborted (timeout)");
              }
              if (result.errors.size > 0) {
                for (const [provider, err] of result.errors) {
                  redactedLog("list-models refresh error provider=".concat(provider, ": ").concat(err.message));
                }
              }
            } finally {
              clearTimeout(timeout);
            }
          }
          const available = await modelRuntime.getAvailable();
          const filtered = available.filter((m) => {
            const found = resolveModel(modelRuntime, m.provider, m.id);
            if (!found) {
              redactedLog("list-models: excluding unresolvable model provider=".concat(m.provider, " model=").concat(m.id));
            }
            return !!found;
          });
          const summary = filtered.map((m) => ({
            provider: m.provider,
            id: m.id,
            name: m.name ?? m.id,
            supportsImages: m.input?.includes("image") ?? false
          }));
          emitReq({ event: "result", content: JSON.stringify(summary) });
        } catch (err) {
          const errMsg = err instanceof Error ? err.message : String(err);
          redactedLog("list-models error: ".concat(errMsg));
          emitReq({ event: "error", message: "list-models failed: ".concat(redactSDKError(errMsg)) });
        }
        break;
      }
      case "steer": {
        await handleSteer(req);
        break;
      }
      case "follow-up": {
        await handleFollowUp(req);
        break;
      }
      case "abort": {
        await handleAbort(req);
        break;
      }
      case "get-state": {
        await handleGetState(req);
        break;
      }
      case "get-session-stats": {
        await handleGetSessionStats(req);
        break;
      }
      case "get-session-history": {
        await handleGetSessionHistory(req);
        break;
      }
      case "compact-session": {
        await handleCompactSession(req);
        break;
      }
      case "rotate-session": {
        await handleRotateSession(req);
        break;
      }
      default:
        emitReq({ event: "error", message: "invalid command" });
    }
  } finally {
    stdoutEmissionBudget.finish(reqId);
  }
}
function safeRequestIDFromLine(line) {
  try {
    const value = JSON.parse(line);
    if (typeof value !== "object" || value === null || Array.isArray(value)) return void 0;
    return safeRequestID(value.request_id) || void 0;
  } catch {
    return void 0;
  }
}
function main() {
  log("bridge started \u2014 waiting for commands on stdin");
  const rl = createInterface({
    input: process.stdin,
    terminal: false
  });
  rl.on("line", (line) => {
    const trimmed = line.trim();
    if (!trimmed) return;
    handleRequest(trimmed).catch((err) => {
      const errMsg = err instanceof Error ? err.message : String(err);
      redactedLog("unhandled error in request processing: ".concat(errMsg));
      const requestID = safeRequestIDFromLine(trimmed);
      emit({
        event: "error",
        ...requestID ? { request_id: requestID } : {},
        message: "internal bridge error: ".concat(redactSDKError(errMsg))
      });
    });
  });
  rl.on("close", () => {
    log("stdin closed \u2014 shutting down");
    process.exit(0);
  });
  process.on("unhandledRejection", (reason) => {
    const msg = reason instanceof Error ? reason.message : String(reason);
    redactedLog("unhandled rejection: ".concat(msg));
    emit({ event: "error", message: "unhandled rejection: ".concat(redactSDKError(msg)) });
  });
  process.on("uncaughtException", (err) => {
    redactedLog("uncaught exception: ".concat(err.message));
    emit({ event: "error", message: "uncaught exception: ".concat(redactSDKError(err.message)) });
    process.exit(1);
  });
}
main();
export {
  DEFAULT_SENSITIVE_PATTERNS,
  StdoutEmissionBudget,
  ToolDurationTracker,
  bindBridgeSessionExtensions,
  boundSessionHistoryPayload,
  compactionEndPayload,
  compactionReason,
  createBridgeSessionRequestLifecycle,
  deriveProjectName,
  disposeBridgeSession,
  evaluateToolPolicy,
  formatLogLine,
  gitHasSensitiveArgs,
  injectMcpProjectScope,
  installSecurityHook,
  isDestructiveCommand,
  isExfiltrationCommand,
  isSafeMakeCommand,
  isSensitivePath,
  matchesBuildOrTest,
  matchesEnvAccess,
  matchesSafeGit,
  measuredElapsed,
  redactAuditPath,
  redactSDKError,
  redactedCommandExcerpt,
  registerSessionIfOwner,
  removeSessionIfOwner,
  resolveModel,
  sanitizeBridgeText,
  serializeOutEvent,
  stallTelemetryFor,
  translateAllowedTools,
  validateBridgeRequest,
  waitForPendingMessageCount
};
