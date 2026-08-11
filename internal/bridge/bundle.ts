import { createInterface } from "node:readline";
import { appendFileSync, existsSync, mkdirSync, readFileSync, renameSync, statSync, truncateSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import {
  createAgentSession,
  DefaultResourceLoader,
  getAgentDir,
  ModelRuntime,
  SessionManager,
  SettingsManager,
} from "@earendil-works/pi-coding-agent";
import type { ImageContent, TextContent } from "@earendil-works/pi-ai";

// ── Types ────────────────────────────────────────────────────────────────────

interface ImageAttachment {
  path?: string;
  data?: string;
  media_type?: string;
}

// Mirrors SecurityContext in internal/bridge/protocol.go.
export interface SecurityContext {
  enabled: boolean;
  profile: "observe" | "read_only" | "edit_project" | "execute_safe" | "privileged";
  mode: "warn" | "block";
  cwd: string;
  sensitive_paths?: string[];
  allowed_outside_cwd?: string[];
  chat_id?: number;
  thread_id?: number;
  user_id?: number;
  agent_name?: string;
  request_id?: string;
}

// Mirrors RequestOptions in internal/bridge/protocol.go. The legacy Claude SDK
// fields (max_turns, permission_mode, mcp_servers, agents, disabled_tools)
// have no analogue in the PI SDK and were dropped.
interface RequestOptions {
  provider?: string;
  model?: string;
  cwd?: string;
  system_prompt?: string;
  resume?: string;
  allowed_tools?: string[];
  disallowed_tools?: string[];
  continue?: boolean;
  no_user_settings?: boolean;
  persist_session?: boolean;
  images?: ImageAttachment[];
  security?: SecurityContext;
  chat_id?: number;
  thread_id?: number;
  user_id?: number;
}

interface Request {
  command: string;
  prompt: string;
  request_id?: string;
  target_request_id?: string;
  refresh?: boolean;
  options?: RequestOptions;
}

interface OutEvent {
  event: string;
  request_id?: string;
  [key: string]: unknown;
}

interface SessionLookup {
  id: string;
  file?: string;
}

// Bounded LRU-ish cache of session id → session file. The bridge is long-lived
// in daemon mode, so an unbounded map would grow over time. Insertion-ordered
// Map iteration lets us evict the oldest entry once we hit the cap.
const MAX_SESSION_CACHE = 256;
const sessionByID = new Map<string, SessionLookup>();
let lastSessionID = "";

interface ActiveRequest {
  cancel(reason: string): Promise<void>;
}

type PiAgentSession = Awaited<ReturnType<typeof createAgentSession>>["session"];
type ChatSessionOwner = symbol;

// Public AgentSession members used by the bridge lifecycle adapter. Keeping
// this boundary structural also makes it possible to exercise teardown with a
// deterministic fake without depending on PI SDK internals.
export interface BridgeSession {
  bindExtensions(bindings: { mode: "print" }): Promise<void>;
  extensionRunner: {
    emit(event: { type: "session_shutdown"; reason: "quit" }): Promise<unknown>;
  };
  dispose(): void;
}

interface ChatSessionState {
  session: PiAgentSession;
  owner: ChatSessionOwner;
  sessionId: string;
  sessionFile: string | undefined;
  currentReqId: string;
  unsubPersistent: () => void;
  unsubHook?: () => void;
  idleTimer?: ReturnType<typeof setTimeout>;
  createdAt: number;
}

interface SessionHistoryMessage {
  sender: "Igor" | "Aurelia";
  text: string;
  timestamp?: string;
}

const activeRequests = new Map<string, ActiveRequest>();
const chatSessions = new Map<string, ChatSessionState>();

const chatSessionOwners = new Map<string, ChatSessionOwner>();

/**
 * Publish a session only while its request still owns the chat key.
 *
 * The bridge's query setup awaits extension binding. A newer query can claim
 * the same key during that await, so the ownership check must be adjacent to
 * the map insertion rather than performed only before the await.
 */
export function registerSessionIfOwner<T>(
  owners: ReadonlyMap<string, ChatSessionOwner>,
  sessions: Map<string, T>,
  key: string,
  owner: ChatSessionOwner,
  session: T,
): boolean {
  if (owners.get(key) !== owner) return false;
  sessions.set(key, session);
  return true;
}

/**
 * Remove a session only if the caller still owns the chat key.
 *
 * Returning the removed value lets the caller dispose exactly that session;
 * a stale caller gets no value and therefore cannot dispose the replacement.
 */
export function removeSessionIfOwner<T>(
  owners: ReadonlyMap<string, ChatSessionOwner>,
  sessions: Map<string, T>,
  key: string,
  owner: ChatSessionOwner,
): T | undefined {
  if (owners.get(key) !== owner) return undefined;
  const session = sessions.get(key);
  if (session === undefined) return undefined;
  sessions.delete(key);
  return session;
}

interface PendingChatCleanup {
  owner: ChatSessionOwner;
  promise: Promise<void>;
}

const pendingChatCleanups = new Map<string, PendingChatCleanup>();

function claimChatSessionOwner(key: string): ChatSessionOwner {
  const owner = Symbol(`chat-session:${key}`);
  chatSessionOwners.set(key, owner);
  return owner;
}

function ownsChatSession(key: string, owner: ChatSessionOwner): boolean {
  return chatSessionOwners.get(key) === owner;
}

interface BridgeSessionLifecycle {
  // Extension modules are loaded by resourceLoader.reload() before the
  // session's bindExtensions() call. Keep that fact separate from binding
  // completion so a canceled pre-bind session still gets session_shutdown.
  extensionRuntimeLoaded: boolean;
  extensionActivationStarted: boolean;
  bindingCompleted: boolean;
  teardownPromise?: Promise<void>;
}

const bridgeSessionLifecycles = new WeakMap<BridgeSession, BridgeSessionLifecycle>();

function bridgeSessionLifecycleFor(session: BridgeSession): BridgeSessionLifecycle {
  const existing = bridgeSessionLifecycles.get(session);
  if (existing) return existing;

  const lifecycle: BridgeSessionLifecycle = {
    extensionRuntimeLoaded: false,
    extensionActivationStarted: false,
    bindingCompleted: false,
  };
  bridgeSessionLifecycles.set(session, lifecycle);
  return lifecycle;
}

function markBridgeSessionExtensionRuntimeLoaded(session: BridgeSession): void {
  bridgeSessionLifecycleFor(session).extensionRuntimeLoaded = true;
}

/**
 * Tear down a session through the public PI SDK lifecycle contract.
 *
 * AgentSession.dispose() only removes session-local listeners/resources. The
 * SDK's print/RPC hosts use AgentSessionRuntime.dispose(), which emits the
 * async session_shutdown extension event first. The bridge owns AgentSession
 * directly, so it mirrors that public ordering through the documented
 * session.extensionRunner getter and ExtensionRunner.emit() method.
 *
 * A session returned by the SDK factory is marked as having a loaded
 * extension runtime because resourceLoader.reload() runs before the bridge
 * can observe the session. Therefore pre-bind cancellation is covered too.
 * Once activation starts, even a rejected/partially-completed bind must
 * receive shutdown.
 * Cleanup errors are logged and swallowed so teardown cannot replace the
 * existing user-visible request result or error.
 */
export async function disposeBridgeSession(session: BridgeSession): Promise<void> {
  const lifecycle = bridgeSessionLifecycleFor(session);
  if (!lifecycle.teardownPromise) {
    // Defer the body until after the promise is stored. This makes concurrent
    // callers share one shutdown/dispose sequence and prevents re-entrancy
    // from an extension shutdown handler from starting a second sequence.
    lifecycle.teardownPromise = Promise.resolve().then(async () => {
      if (lifecycle.extensionRuntimeLoaded || lifecycle.extensionActivationStarted) {
        try {
          await session.extensionRunner.emit({ type: "session_shutdown", reason: "quit" });
        } catch (err: unknown) {
          redactedLog(
            `session teardown: session_shutdown failed: ${err instanceof Error ? err.message : String(err)}`,
          );
        }
      }

      try {
        session.dispose();
      } catch (err: unknown) {
        redactedLog(
          `session teardown: dispose failed: ${err instanceof Error ? err.message : String(err)}`,
        );
      }
    });
  }
  await lifecycle.teardownPromise;
}

function chatKey(chatID: number, threadID: number, userID = 0): string {
  return `${chatID}:${threadID}:${userID}`;
}

function cleanupChatSession(key: string, expectedOwner?: ChatSessionOwner): Promise<void> {
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

  // Remove the entry before awaiting async extension cleanup. A failed query
  // must never leave its disposed/replaced session addressable by steer,
  // follow-up, or abort requests.
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
    try { cs.unsubPersistent(); } catch {}
    try { if (cs.unsubHook) cs.unsubHook(); } catch {}
    await disposeBridgeSession(cs.session);
  });
  pendingChatCleanups.set(key, { owner: cs.owner, promise: cleanup });
  void cleanup.then(
    () => {
      if (pendingChatCleanups.get(key)?.promise === cleanup) pendingChatCleanups.delete(key);
    },
    () => {
      if (pendingChatCleanups.get(key)?.promise === cleanup) pendingChatCleanups.delete(key);
    },
  );
  return cleanup;
}

function startIdleTimer(cs: ChatSessionState, key: string): void {
  clearTimeout(cs.idleTimer);
  cs.idleTimer = setTimeout(async () => {
    log(`idle timeout: cleaning up session ${cs.sessionId} for chat ${key}`);
    await cleanupChatSession(key, cs.owner);
  }, 30 * 60 * 1000); // 30 minutes
}

function rememberSession(id: string, lookup: SessionLookup): void {
  // Touch on update so re-resumed sessions stay warm.
  if (sessionByID.has(id)) sessionByID.delete(id);
  sessionByID.set(id, lookup);
  while (sessionByID.size > MAX_SESSION_CACHE) {
    const oldest = sessionByID.keys().next().value;
    if (oldest === undefined) break;
    sessionByID.delete(oldest);
  }
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function emit(obj: OutEvent): void {
  process.stdout.write(JSON.stringify(obj) + "\n");
}

function log(msg: string): void {
  process.stderr.write(`[bridge] ${msg}\n`);
}

// Redact common credential patterns from log/error messages to prevent leaking
// sensitive content (API keys, tokens, auth headers, private keys) through logs.
export function redactSDKError(msg: string): string {
  return msg
    // API keys: sk-*, pk-*, sk-ant-*, sk-proj-*
    .replace(/\bsk-[A-Za-z0-9]{20,}/g, "[API_KEY_REDACTED]")
    .replace(/\bpk-[A-Za-z0-9]{20,}/g, "[API_KEY_REDACTED]")
    .replace(/\bsk-ant-[A-Za-z0-9]{20,}/g, "[API_KEY_REDACTED]")
    .replace(/\bsk-proj-[A-Za-z0-9]{20,}/g, "[API_KEY_REDACTED]")
    // Stripe keys
    .replace(/\bsk_live_[A-Za-z0-9]+/g, "[STRIPE_KEY_REDACTED]")
    .replace(/\bsk_test_[A-Za-z0-9]+/g, "[STRIPE_KEY_REDACTED]")
    // AWS keys
    .replace(/\bAKIA[A-Z0-9]{16}/g, "[AWS_KEY_REDACTED]")
    // GCP keys
    .replace(/\bAIza[0-9A-Za-z_-]{35}/g, "[GCP_KEY_REDACTED]")
    // GitHub tokens
    .replace(/\bghp_[A-Za-z0-9]{36}/g, "[GH_TOKEN_REDACTED]")
    .replace(/\bgho_[A-Za-z0-9]{36}/g, "[GH_TOKEN_REDACTED]")
    .replace(/\bghu_[A-Za-z0-9]{36}/g, "[GH_TOKEN_REDACTED]")
    .replace(/\bghs_[A-Za-z0-9]{36}/g, "[GH_TOKEN_REDACTED]")
    .replace(/\bghr_[A-Za-z0-9]{36}/g, "[GH_TOKEN_REDACTED]")
    .replace(/\bgithub_pat_[0-9A-Za-z_-]+/g, "[GH_PAT_REDACTED]")
    // JWT tokens
    .replace(/\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+/g, "[JWT_REDACTED]")
    // Private key blocks (multiline)
    .replace(/-----BEGIN (OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----[\s\S]*?-----END (OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----/g, "[PRIVATE_KEY_BLOCK_REDACTED]")
    // Bearer/Basic auth tokens in headers
    .replace(/(Authorization:\s*(?:Bearer|Basic)\s+)\S+/gi, "$1[REDACTED]")
    // XAI keys
    .replace(/\bxai-[A-Za-z0-9]{20,}/g, "[XAI_KEY_REDACTED]")
    // GitLab tokens
    .replace(/\bglpat-[A-Za-z0-9_-]{20,}/g, "[GL_TOKEN_REDACTED]")
    // HuggingFace tokens
    .replace(/\bhf_[A-Za-z0-9]{20,}/g, "[HF_TOKEN_REDACTED]")
    // NPM tokens
    .replace(/\bnpm_[A-Za-z0-9]{36}/g, "[NPM_TOKEN_REDACTED]")
    // Slack tokens
    .replace(/\bxox[bpasa]-[A-Za-z0-9-]{20,}/g, "[SLACK_TOKEN_REDACTED]")
    .replace(/\bxapp-[A-Za-z0-9-]{20,}/g, "[SLACK_TOKEN_REDACTED]");
}

function redactedLog(msg: string): void {
  log(redactSDKError(msg));
}

// Detect billing/credit errors from the PI SDK so we can surface clear messages
// and avoid contaminating session suspect counters.
function isBillingError(msg: string): boolean {
  const lower = msg.toLowerCase();
  return lower.includes("insufficient balance")
    || lower.includes("insufficient credits")
    || (lower.includes("401") && lower.includes("billing"))
    || lower.includes("billing error");
}

// Redact then truncate: ensures secrets in the command excerpt are always
// redacted BEFORE slicing, so a secret straddling the truncation boundary
// isn't sliced in half and leaked (see redaction-before-truncation.md).
export function redactedCommandExcerpt(command: string, maxLen: number): string {
  const redacted = redactSDKError(command);
  if (redacted.length <= maxLen) return redacted;
  return redacted.slice(0, maxLen) + "...";
}

// Redact sensitive file/directory path references from audit text fields
// so that credential locations are not leaked in persisted output.
// Applied AFTER redactSDKError so token patterns are already removed.
export function redactAuditPath(text: string): string {
  return text
    // .env files (as path component, e.g. /path/.env, ~/.env, .env.local)
    .replace(/(?:^|[/~])\.env(?:\b|\/|$)/g, "[SENSITIVE_PATH_REDACTED]")
    // .ssh/ directory paths
    .replace(/(?:^|[/~])\.ssh(?:\/|$)/g, "[SENSITIVE_PATH_REDACTED]")
    // .pi/ directory paths
    .replace(/(?:^|[/~])\.pi(?:\/|$)/g, "[SENSITIVE_PATH_REDACTED]")
    // .aurelia/config/ directory paths
    .replace(/(?:^|[/~])\.aurelia\/config(?:\/|$)/g, "[SENSITIVE_PATH_REDACTED]")
    // .git/config file paths
    .replace(/(?:^|[/~])\.git\/config(?:\b|\/|$)/g, "[SENSITIVE_PATH_REDACTED]");
}

function escapeUntrustedSummary(text: string): string {
  return redactSDKError(text)
    .replace(/<\/previous_session_summary_untrusted>/gi, "&lt;/previous_session_summary_untrusted&gt;")
    .replace(/<previous_session_summary_untrusted>/gi, "&lt;previous_session_summary_untrusted&gt;");
}

function piAgentDir(): string {
  return process.env.PI_CODING_AGENT_DIR || join(homedir(), ".pi", "agent");
}

function mapProvider(provider: string | undefined): string | undefined {
  if (!provider) return undefined;
  const normalized = provider.trim().toLowerCase();
  const aliases: Record<string, string> = {
    kimi: "kimi-coding",
    kilo: "opencode-go",
    alibaba: "opencode-go",
    google: "google",
    anthropic: "anthropic",
    openrouter: "openrouter",
    zai: "zai",
    ollama: "ollama",
  };
  return aliases[normalized] ?? normalized;
}

function mapModelForProvider(provider: string | undefined, model: string): string {
  const normalized = model.trim();
  if (provider === "kimi-coding" && (normalized === "k2.5" || normalized === "kimi-k2.5")) {
    return "kimi-for-coding";
  }
  return normalized;
}

function translateToolName(name: string): string {
  const normalized = name.trim();
  const toolMap: Record<string, string> = {
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
    WebFetch: "web_search",
  };
  return toolMap[normalized] ?? normalized.toLowerCase();
}

// Full list of PI SDK built-in tools for denylist-only mode.
const allBuiltinTools = [
  "read", "write", "edit", "bash", "grep", "find", "ls",
  "web_search", "web_search_premium",
];

// Extension utility tools registered by pi-* packages under
// ~/.pi/agent/npm/ — these aren't in PI SDK's native tool set but the
// agent should still have access to them whenever the profile grants
// any non-empty allowlist. Each entry is gated by the security hook
// in evaluateToolPolicy, so the `mcp` proxy and web helpers never
// bypass the policy engine.
//
//   mcp                — pi-mcp-adapter proxy; lazy-connects MCP
//                        servers from ~/.pi/agent/mcp.json on demand
//   code_search        — pi-web-access: code-host search (GitHub, etc.)
//   fetch_content      — pi-web-access: HTTP fetch with parsing
//   get_search_content — pi-web-access: structured web search results
//
// Returning these as a const tuple keeps the set greppable and lets
// tests assert on the exact membership.
const EXTENSION_UTILITY_TOOLS: readonly string[] = [
  "mcp",
  "code_search",
  "fetch_content",
  "get_search_content",
];

export function translateAllowedTools(
  allowed: string[] | undefined,
  disallowed: string[] | undefined,
): string[] | undefined {
  const hasRestriction = (allowed && allowed.length > 0) || (disallowed && disallowed.length > 0);

  // Start with allowed list (or all built-ins if none specified)
  let result: string[] | undefined;
  if (allowed && allowed.length > 0) {
    result = [...new Set(allowed.map(translateToolName))];
  }

  // Apply denylist
  if (disallowed && disallowed.length > 0) {
    const denied = new Set(disallowed.map(translateToolName));
    if (result) {
      result = result.filter((t) => !denied.has(t));
    } else {
      // No allowlist: denylist means "all except these"
      result = allBuiltinTools.filter((t) => !denied.has(t));
    }
  }

  // Always merge in extension utility tools unless explicitly denied.
  // Without this, passing an allowlist to the PI SDK would filter out
  // the `mcp` proxy and the pi-web-access helpers, leaving the model
  // unable to call any MCP server or run a web search. Each call is
  // still gated by evaluateToolPolicy so security boundaries hold.
  if (result !== undefined) {
    const denied = new Set((disallowed ?? []).map(translateToolName));
    for (const ext of EXTENSION_UTILITY_TOOLS) {
      if (!denied.has(ext) && !result.includes(ext)) {
        result.push(ext);
      }
    }
  }

  if (!result || result.length === 0) {
    // If the user explicitly restricted tools and the result is empty,
    // return an empty array so the PI SDK receives no tools instead of
    // falling back to all defaults.
    return hasRestriction ? [] : undefined;
  }
  return result;
}

// ── Security Policy Evaluation ──────────────────────────────────────────────

export interface PolicyDecision {
  decision: "allow" | "block" | "rewrite";
  reason?: string;
  input?: Record<string, unknown>;
}

export interface AuditEntry {
  timestamp?: string;
  decision: string;
  tool_name: string;
  reason: string;
  chat_id?: number;
  thread_id?: number;
  agent_name?: string;
  profile: string;
  cwd: string;
  redacted: boolean;
}

export const DEFAULT_SENSITIVE_PATTERNS = [
  ".env", ".env.*", "*.pem", "*.key",
  "id_rsa", "id_ed25519",
  "config.json", "credentials.json", "*.credentials",
  "service-account*.json",
  ".ssh/*", ".pi/*", ".aurelia/config/*", ".git/config",
];

function matchesGlob(pattern: string, name: string): boolean {
  // Convert simple glob pattern to regex.
  const regexStr = "^" + pattern
    .replace(/\./g, "\\.")
    .replace(/\*/g, ".*")
    .replace(/\?/g, ".")
    + "$";
  try {
    return new RegExp(regexStr).test(name);
  } catch {
    return false;
  }
}

export function isSensitivePath(path: string, patterns: string[]): boolean {
  const clean = path.replace(/\\/g, "/");
  const parts = clean.split("/");
  const base = parts[parts.length - 1] || "";

  for (const pat of patterns) {
    if (matchesGlob(pat, base)) return true;
    if (matchesGlob(pat, clean)) return true;
  }

  // Check for known sensitive directories anywhere in the path.
  for (const dir of [".ssh", ".aurelia/config", ".pi"]) {
    if (clean.includes("/" + dir + "/") || clean.startsWith(dir + "/")) {
      return true;
    }
  }

  return false;
}

export function isDestructiveCommand(command: string): boolean {
  const lower = command.trim().toLowerCase();

  // rm -rf with absolute/system paths.
  if (/^rm\s+.*-rf/i.test(lower) || /^rm\s+.*-fr/i.test(lower)) {
    for (const bad of ["/ ", "/*", "/.", "~/", "/etc", "/usr", "/bin", "/lib", "/home", "/root", "/var"]) {
      if (lower.includes(bad)) return true;
    }
  }
  if (/rm\s+\//.test(lower) || /rm -rf \//.test(lower)) return true;

  // sudo (always blocked).
  if (/^sudo\s/.test(lower)) return true;

  // chmod -R on system paths.
  if (/chmod.*-r/i.test(lower)) {
    for (const bad of ["/ ", "/etc", "/usr", "/bin", "/lib"]) {
      if (lower.includes(bad)) return true;
    }
  }

  // chown -R.
  if (/chown.*-r/i.test(lower)) return true;

  // dd with of= (disk destroyer).
  if (/^dd\s/.test(lower) && /of=/.test(lower)) return true;

  // Fork bomb patterns.
  if (lower.includes(":(){") || lower.includes(":()")) return true;

  // mkfs, fdisk, parted.
  if (/^mkfs/.test(lower) || /^fdisk/.test(lower) || /^parted/.test(lower)) return true;

  return false;
}

export function isExfiltrationCommand(command: string): boolean {
  const lower = command.trim().toLowerCase();

  const hasNetworkTool = ["curl ", "wget ", "nc ", "ncat ", "scp ", "rsync "].some(
    (t) => lower.includes(t),
  );
  if (!hasNetworkTool) return false;

  const suspicious = [
    "$(cat ", "`cat `", "`env`", "$(env)",
    "<~", ".env", "id_rsa", "token", "secret", "password",
    "-d @", " --data @", "--data-raw", "--data-binary",
    "-F ", "--form ", "file=@",
    "| nc ", "| ncat ",
  ];
  return suspicious.some((s) => lower.includes(s));
}

export function matchesEnvAccess(command: string): boolean {
  const lower = command.trim().toLowerCase();
  if (/^env$/.test(lower) || /^printenv/.test(lower) || /^export($|\s)/.test(lower)) return true;
  if (lower.includes(".aurelia/config")) return true;
  if (/echo\s+\$/.test(lower) || /echo \${/.test(lower)) return true;
  if (lower.includes("cat ~/.aurelia")) return true;
  return false;
}

// hasShellComposition checks for shell control operators that compose
// multiple commands (&&, ||, ;, |, backticks, $(, >, <, newlines).
// This prevents safe-command prefix bypasses like "git status && whoami"
// or "go test ./... ; curl evil.com".  Must be called BEFORE safe git/build
// /test allow decisions so composed-command attacks are denied even when
// the prefix matches an allowlist entry.
function hasShellComposition(command: string): boolean {
  const lower = command.trim().toLowerCase();
  // Only check the portion after the first word — we don't want to flag
  // make targets themselves, since isSafeMakeCommand handles them.
  if (/^make(\s|$)/.test(lower)) return false; // handled by isSafeMakeCommand
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

function isDangerousGit(command: string): boolean {
  const lower = command.trim().toLowerCase();
  if (!lower.startsWith("git ")) return false;
  const dangerous = [
    "git push --force", "git push -f",
    "git remote add", "git remote set-url",
    "git reset --hard", "git clean -f",
    "git reflog delete", "git update-ref -d",
    "git credential", "git gc",
  ];
  return dangerous.some((d) => lower.includes(d));
}

// Safe make targets: only check-only/build/test operations that are
// low risk. Operational targets like install, deploy, run, dev, clean
// are excluded because they can side-effect the system or modify state
// beyond the workspace.
const SAFE_MAKE_TARGETS = new Set([
  "build", "test", "check",
  "lint", "vet", "typecheck", "generate",
]);

export function isSafeMakeCommand(command: string): boolean {
  const lower = command.trim().toLowerCase();

  // Reject variable assignments (make VAR=value) and shell metacharacters
  // that can inject arbitrary commands: ; && || ` $( < > | \n \r
  const hasMetachar = /[;&|`$]/.test(lower) || /[<>]/.test(lower) || lower.includes("\n") || lower.includes("\r");
  if (hasMetachar) return false;

  // "make" with no args runs the default target (typically build/test).
  if (/^make$/.test(lower)) return true;

  // Split into tokens after "make" and check EVERY token.
  // No flags (-f, --file, -C, etc.), no variable assignments, and
  // every target must be a known safe target or safe-X compound.
  const rest = lower.slice(5).trim();
  if (!rest) return true; // just "make "
  const tokens = rest.split(/\s+/);

  for (const tok of tokens) {
    // Reject flag-style tokens (-f, --file, -C, --directory, etc.)
    if (tok.startsWith("-")) return false;
    // Reject variable assignments (VAR=value)
    if (tok.includes("=")) return false;
    // Check if token is a safe target or compound variant.
    let ok = false;
    for (const safe of SAFE_MAKE_TARGETS) {
      if (tok === safe) { ok = true; break; }
      if (tok.startsWith(safe + "-") || tok.startsWith(safe + "_")) { ok = true; break; }
    }
    if (!ok) return false;
  }
  return true;
}

export function matchesBuildOrTest(command: string): boolean {
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
    /^tsc(\s|$)/,
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
    /^rails\s+test/,
  ];
  // Restrict make to safe targets only.
  if (/^make(\s|$)/.test(lower)) {
    if (!isSafeMakeCommand(command)) return false;
    return true; // safe make (including bare `make`) is allowed
  }
  return [...buildPatterns, ...testPatterns].some((p) => p.test(lower));
}

// gitHasSensitiveArgs checks whether a git command references sensitive
// file paths in its arguments (e.g. .env, .ssh/*, .pi/*, .aurelia/config/*,
// .git/config) or uses --no-index which can read arbitrary files.
// Must be called BEFORE matchesSafeGit so that even "git diff" or "git show"
// commands are denied when their arguments reference sensitive data.
export function gitHasSensitiveArgs(command: string): boolean {
  const lower = command.trim().toLowerCase();
  if (!lower.startsWith("git ")) return false;

  // git diff --no-index compares arbitrary files outside the repo.
  if (lower.includes("--no-index")) return true;

  // Check ref-based paths: HEAD:.env, HEAD:.git/config, etc.
  // These show file contents from git history which may contain secrets.
  if (/HEAD\s*:\s*\.(env|ssh|git\/config)\b/i.test(command)) return true;

  // Check explicit file arguments for sensitive path patterns.
  // Note: (?:^|\s) prefix so ".env" is matched after space separator, not
  // \b (word boundary) — because '.' is non-word, \b fails between space and ..
  const args = command.slice(3).trim(); // everything after "git"
  if (/(?:^|\s)\.env(?:\b|$)/.test(args)) return true;
  if (/\.ssh(?:\/|$)/.test(args)) return true;
  if (/\.pi(?:\/|$)/.test(args)) return true;
  if (/\.aurelia\/config(?:\/|$)/.test(args)) return true;
  if (/\.git\/config(?:\b|$)/.test(args)) return true;
  if (/~\/\./.test(args)) return true; // ~/. pattern indicates home-dir access

  return false;
}

export function matchesSafeGit(command: string): boolean {
  const lower = command.trim().toLowerCase();
  const safePrefixes = [
    "git status", "git diff", "git log", "git show",
    "git branch", "git checkout", "git stash list",
    "git describe", "git rev-parse", "git rev-list",
    // Intentionally excludes "git config": reveals remote tokens, credential
    // helpers, emails, and .git/config contents. Use /cwd or Read instead.
    "git ls-files", "git ls-tree",
    "git tag", "git blame", "git shortlog",
    "git cherry", "git cherry-pick --abort",
  ];
  return safePrefixes.some((p) => lower.startsWith(p));
}

function isPathInsideCwd(path: string, cwd: string, allowedOutside: string[]): boolean {
  if (!path || !cwd) return true;
  const clean = path.replace(/\\/g, "/");
  const cwdNorm = cwd.replace(/\\/g, "/").replace(/\/+$/, "");

  // Relative path starting with .. is outside.
  if (clean === ".." || clean.startsWith("../")) return false;

  // "." is the CWD itself — always allowed.
  if (clean === ".") return true;

  if (!clean.startsWith("/")) {
    // Relative path (not starting with ..): resolve against cwd, then apply
    // boundary comparison. This catches nested traversal like
    // "subdir/../../outside" that resolves outside cwd.
    const resolved = resolve(cwdNorm, clean);
    const cwdResolved = resolve(cwdNorm);
    if (resolved.startsWith(cwdResolved + "/") || resolved === cwdResolved) {
      return true;
    }
    // Check allowlist.
    for (const allowed of allowedOutside) {
      const allowedResolved = resolve(allowed.replace(/\\/g, "/"));
      if (resolved.startsWith(allowedResolved + "/") || resolved === allowedResolved) {
        return true;
      }
    }
    return false;
  }

  // Normalize with path.resolve to collapse "..", ".", and redundant segments
  // before boundary comparison. This prevents traversal via
  // /repo/subdir/../../secret from bypassing the check.
  const resolved = resolve(clean);
  const cwdResolved = resolve(cwdNorm);

  // Reject if resolved path falls outside cwd.
  if (!resolved.startsWith(cwdResolved + "/") && resolved !== cwdResolved) {
    // Not under cwd — check allowlist.
    for (const allowed of allowedOutside) {
      // Normalize allowed paths too to avoid /tmp/foo vs /tmp/foobar prefix bypass.
      const allowedResolved = resolve(allowed.replace(/\\/g, "/"));
      if (resolved.startsWith(allowedResolved + "/") || resolved === allowedResolved) {
        return true;
      }
    }
    return false;
  }

  return true;
}

// ── WebFetch URL policy (SSRF prevention) ────────────────────────────────────
//
// Checks a URL for known private/internal IP ranges to prevent Server-Side
// Request Forgery (SSRF) via the WebFetch tool. Uses native URL parsing and
// string/number checks — no external dependencies.
//
// Residual risk: DNS rebinding (a domain resolving to an internal IP after the
// URL-string check) and redirect-based SSRF (a public server redirecting to an
// internal IP after the initial check) are not mitigated here — those require
// runtime DNS resolution and redirect-following policy at the network layer.
function isBlockedWebFetchURL(
  urlString: string,
): { blocked: true; reason: string } | { blocked: false } {
  let url: URL;
  try {
    url = new URL(urlString);
  } catch {
    return { blocked: true, reason: "URL is invalid or unparseable" };
  }

  const hostname = url.hostname;
  if (!hostname) {
    return { blocked: true, reason: "URL has no hostname" };
  }

  // Well-known loopback hostnames.
  const lower = hostname.toLowerCase();
  if (
    lower === "localhost" ||
    lower === "localhost.localdomain" ||
    lower === "localhost6" ||
    lower === "localhost6.localdomain6"
  ) {
    return { blocked: true, reason: "URL targets loopback address" };
  }

  // Shared check for IPv4 octets: returns a blocked result if the address
  // is private/reserved, or null to let the caller continue.
  const checkIPv4 = (
    octets: number[],
  ): { blocked: true; reason: string } | null => {
    // 0.0.0.0/8 — "this network" (RFC 1122), non-routable.
    if (octets[0] === 0) {
      return { blocked: true, reason: "URL targets unspecified address" };
    }
    // 127.0.0.0/8 — loopback.
    if (octets[0] === 127) {
      return { blocked: true, reason: "URL targets loopback address" };
    }
    // 169.254.0.0/16 — link-local (includes cloud metadata 169.254.169.254).
    if (octets[0] === 169 && octets[1] === 254) {
      return { blocked: true, reason: "URL targets link-local address" };
    }
    // 10.0.0.0/8 — private.
    if (octets[0] === 10) {
      return { blocked: true, reason: "URL targets private IP address" };
    }
    // 172.16.0.0/12 — private.
    if (octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31) {
      return { blocked: true, reason: "URL targets private IP address" };
    }
    // 192.168.0.0/16 — private.
    if (octets[0] === 192 && octets[1] === 168) {
      return { blocked: true, reason: "URL targets private IP address" };
    }
    return null;
  };

  // IPv4 literal check.
  const ipv4Match = hostname.match(/^(\d+)\.(\d+)\.(\d+)\.(\d+)$/);
  if (ipv4Match) {
    const octets = ipv4Match.slice(1).map(Number);
    if (octets.some((o) => o > 255)) {
      return { blocked: true, reason: "URL has invalid IP address" };
    }
    const blocked = checkIPv4(octets);
    if (blocked) return blocked;
    // Public IPv4 — allow.
    return { blocked: false };
  }

  // IPv6 literal check.
  // Also catches IPv4-mapped IPv6 (e.g. [::ffff:127.0.0.1]).
  if (hostname.includes(":")) {
    const ipv6 = hostname.toLowerCase().replace(/^\[|\]$/g, "");

    // ::1 — loopback.
    if (ipv6 === "::1" || ipv6 === "0:0:0:0:0:0:0:1") {
      return { blocked: true, reason: "URL targets loopback address" };
    }

    // fc00::/7 — unique local address (ULA).
    if (ipv6.startsWith("fc") || ipv6.startsWith("fd")) {
      return { blocked: true, reason: "URL targets unique local address (ULA)" };
    }

    // IPv4-mapped IPv6 (e.g. [::ffff:127.0.0.1]).
    // Node's URL class serializes the embedded IPv4 as hex groups
    // (::ffff:7f00:1) rather than dotted-decimal (::ffff:127.0.0.1),
    // so we try both forms.
    let embeddedOctets: number[] | null = null;
    const ipv4MappedHex = ipv6.match(/^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/);
    if (ipv4MappedHex) {
      const hi = parseInt(ipv4MappedHex[1], 16);
      const lo = parseInt(ipv4MappedHex[2], 16);
      embeddedOctets = [
        (hi >> 8) & 0xff,
        hi & 0xff,
        (lo >> 8) & 0xff,
        lo & 0xff,
      ];
    } else {
      const ipv4MappedMixed = ipv6.match(
        /^::ffff:(\d+)\.(\d+)\.(\d+)\.(\d+)$/,
      );
      if (ipv4MappedMixed) {
        embeddedOctets = ipv4MappedMixed.slice(1).map(Number);
      }
    }
    if (embeddedOctets) {
      const blocked = checkIPv4(embeddedOctets);
      if (blocked) return blocked;
      // Embedded public IPv4 — allow via IPv6.
      return { blocked: false };
    }

    // Any other IPv6 — allow (public range).
    return { blocked: false };
  }

  // DNS hostname (not localhost, not an IP literal) — allow.
  return { blocked: false };
}

export function evaluateToolPolicy(
  toolName: string,
  input: Record<string, unknown>,
  security: SecurityContext,
): PolicyDecision {
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
      const path = (input.path as string) || "";
      if (!path) return { decision: "allow" };

      // Check sensitive paths.
      const patterns = cfg.sensitive_paths || DEFAULT_SENSITIVE_PATTERNS;
      if (isSensitivePath(path, patterns)) {
        const reason = `access to sensitive path blocked: ${path}`;
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      // Check cwd boundary for file/directory read-like tools. Grep/Glob/LS can
      // expose content or project structure, so they must not escape the bound cwd.
      if (cfg.cwd && !isPathInsideCwd(path, cfg.cwd, cfg.allowed_outside_cwd || [])) {
        const reason = `path outside working directory: ${path}`;
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      return { decision: "allow" };
    }

    case "Write":
    case "Edit": {
      const path = (input.path as string) || "";
      if (!path) return { decision: "allow" };

      // Check sensitive paths.
      const patterns = cfg.sensitive_paths || DEFAULT_SENSITIVE_PATTERNS;
      if (isSensitivePath(path, patterns)) {
        return { decision: "block", reason: `write to sensitive path blocked: ${path}` };
      }

      // Check cwd boundary.
      if (cfg.cwd && !isPathInsideCwd(path, cfg.cwd, cfg.allowed_outside_cwd || [])) {
        const reason = `write to path outside working directory: ${path}`;
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }
      return { decision: "allow" };
    }

    case "Bash": {
      const command = (input.command as string) || "";
      if (!command) return { decision: "allow" };

      // Security-critical checks run FIRST, before any broad allowlist,
      // so destructive/exfil patterns cannot hide behind build/test matches.
      //
      // 1. Destructive commands (rm -rf /, sudo, mkfs, etc.)
      if (isDestructiveCommand(command)) {
        const reason = `destructive command blocked: ${redactedCommandExcerpt(command, 80)}`;
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      // 2. Exfiltration (network tools reading sensitive files)
      if (isExfiltrationCommand(command)) {
        const reason = `exfiltration blocked: ${redactedCommandExcerpt(command, 80)}`;
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      // 3. Environment access (env, printenv, secret patterns)
      if (matchesEnvAccess(command)) {
        const reason = "environment access blocked: command reads env vars or secrets";
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      // 4. Dangerous git operations
      if (isDangerousGit(command)) {
        const reason = "dangerous git operation blocked";
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      // 5. Unsafe make commands — blocked explicitly so they don't fall
      // through to the default allow below. Safe make commands (check-only
      // build/test targets, no metacharacters) pass through to build/test.
      if (/^make(\s|$)/.test(command) && !isSafeMakeCommand(command)) {
        const reason = `unsafe make blocked: ${redactedCommandExcerpt(command, 80)}`;
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      // 6. Shell composition check — deny multi-command composition (&&, ||,
      //    ;, |, backticks, $(, >, <, \n) BEFORE safe git/build/test matching.
      //    This prevents bypasses like "git status && whoami" or "go test ./...; curl".
      if (hasShellComposition(command)) {
        const reason = `shell composition blocked: ${redactedCommandExcerpt(command, 120)}`;
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      // 7. Git sensitive args check — deny even safe git subcommands when
      //    their arguments reference sensitive paths (.env, .ssh, .pi,
      //    .aurelia/config, .git/config) or use --no-index. This prevents
      //    "git show HEAD:.env" and "git diff --no-index ~/.ssh/id_rsa /tmp/x".
      if (/^git\s/.test(command.trim()) && gitHasSensitiveArgs(command)) {
        const reason = `git sensitive args blocked: ${redactedCommandExcerpt(command, 120)}`;
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      // 8. Safe git commands allowed.
      if (matchesSafeGit(command)) {
        return { decision: "allow", reason: "safe git command allowed" };
      }

      // 8. Build and test commands — safe subset only.
      if (matchesBuildOrTest(command)) {
        return { decision: "allow", reason: "build/test command allowed" };
      }

      // 9. Fail-closed: execute_safe profile denies all non-allowlisted Bash.
      //    Shell composition (step 6) has already been checked, so any
      //    command reaching here is a single command with no shell chaining.
      if (cfg.profile === "execute_safe") {
        const reason = `command not on allowlist: ${redactedCommandExcerpt(command, 120)}`;
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      return { decision: "allow" };
    }

    case "WebFetch": {
      const url = (input.url as string) || "";
      if (!url) {
        const reason = "WebFetch blocked: no URL provided";
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      const check = isBlockedWebFetchURL(url);
      if (check.blocked) {
        const reason = `WebFetch blocked: ${check.reason}`;
        if (mode === "warn") return { decision: "allow", reason: "[WARN] " + reason };
        return { decision: "block", reason };
      }

      return { decision: "allow" };
    }

    default:
      return { decision: "allow" };
  }
}

const auditLogMaxBytes = 5 * 1024 * 1024;
const auditLogBackups = 3;

function auditLogPath(): string {
  const root = process.env.AURELIA_HOME?.trim() || join(homedir(), ".aurelia");
  return join(root, "audit.log");
}

function rotateAuditLogIfNeeded(path: string, incomingBytes: number): void {
  try {
    if (!existsSync(path)) return;
    const size = statSync(path).size;
    if (size + incomingBytes <= auditLogMaxBytes) return;
    if (auditLogBackups <= 0) {
      truncateSync(path, 0);
      return;
    }
    for (let i = auditLogBackups - 1; i >= 1; i--) {
      const oldPath = `${path}.${i}`;
      const newPath = `${path}.${i + 1}`;
      if (existsSync(oldPath)) renameSync(oldPath, newPath);
    }
    renameSync(path, `${path}.1`);
  } catch {
    // Audit logging must never block tool execution.
  }
}

function writeAuditFile(line: string): void {
  try {
    const path = auditLogPath();
    mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
    rotateAuditLogIfNeeded(path, Buffer.byteLength(line));
    appendFileSync(path, line, { encoding: "utf8", mode: 0o600 });
  } catch {
    // Audit logging must never block tool execution.
  }
}

function logAudit(entry: AuditEntry): void {
  entry.timestamp = new Date().toISOString();
  entry.redacted = true;
  // Redact BEFORE serialization so secrets in reason/cwd/agent_name
  // are caught (see redaction-before-truncation.md).
  // Token redaction first, then path redaction, so both are caught.
  entry.reason = redactAuditPath(redactSDKError(entry.reason));
  entry.cwd = redactAuditPath(redactSDKError(entry.cwd));
  if (entry.agent_name) entry.agent_name = redactAuditPath(redactSDKError(entry.agent_name));
  const line = "[security] " + JSON.stringify(entry) + "\n";
  process.stderr.write(line);
  writeAuditFile(line);
}

type BeforeToolCall = NonNullable<
  Awaited<ReturnType<typeof createAgentSession>>["session"]["agent"]["beforeToolCall"]
>;

// ── ai-memory MCP scope injection ────────────────────────────────────────
//
// The ai-memory MCP server (remote HTTP at 127.0.0.1:49374) auto-resolves
// the "active project" from the most recent lifecycle-hook activity it has
// seen — NOT from the current conversation. The Aurelia daemon is a single
// long-lived process (cwd fixed at ~/.aurelia) serving many chats across
// many projects, so its MCP calls resolve to whichever project last wrote
// hooks globally → queries return "stale" data or "not found".
//
// Fix: when the model calls an ai-memory tool through the unified `mcp`
// proxy without an explicit scope, inject `project` derived from the
// conversation's real cwd (SecurityContext.cwd), which the pipeline already
// resolves per chat via effectiveCwdForContext.

const AI_MEMORY_SERVER = "ai-memory";
const MEMORY_TOOL_PREFIX = "memory_";

// Walk up from `cwd` until a directory containing `.git` is found and return
// its basename — matches ai-memory's project naming (basename of repo_path).
export function deriveProjectName(cwd: string): string | undefined {
  if (!cwd) return undefined;
  let dir = resolve(cwd);
  for (;;) {
    if (existsSync(join(dir, ".git"))) {
      return basename(dir);
    }
    const parent = dirname(dir);
    if (parent === dir) return undefined;
    dir = parent;
  }
}

// Mutates `args` in place, returning true when a project was injected.
// Handles both the unified `mcp` proxy ({server, tool, args}) and
// direct memory_* tool names, plus string-JSON arguments.
//
// The pi-mcp-adapter `mcp` proxy registers a single tool with parameters
// { tool, args, server, ... } — the real tool arguments live under the
// `args` key (object or JSON string), which is what the model sees in the
// tool schema. There is no `arguments` key in the adapter envelope, so a
// legacy `arguments`-shaped call would be ignored by the adapter's execute
// (it only reads params.args) and injecting there would be pointless.
export function injectMcpProjectScope(
  toolName: string,
  args: unknown,
  cwd: string,
): boolean {
  if (typeof args !== "object" || args === null) return false;
  const a = args as Record<string, unknown>;

  let targetTool: string | undefined;
  let container: unknown;
  if (toolName === "mcp") {
    if (a.server !== AI_MEMORY_SERVER) return false;
    const t = a.tool;
    targetTool = typeof t === "string" ? t : undefined;
    container = a.args;
  } else if (toolName.startsWith(MEMORY_TOOL_PREFIX)) {
    targetTool = toolName;
    container = a;
  }
  if (!targetTool || !targetTool.startsWith(MEMORY_TOOL_PREFIX)) return false;

  // Resolve the arguments container (object or JSON string).
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
  const target = container as Record<string, unknown>;

  // Never override an explicit scope chosen by the model/user.
  if (
    target.project !== undefined ||
    target.workspace !== undefined ||
    target.scopes !== undefined ||
    target.global !== undefined
  ) {
    return false;
  }

  const project = deriveProjectName(cwd);
  if (!project) return false;
  target.project = project;
  if (toolName === "mcp") {
    a.args = wasString ? JSON.stringify(target) : target;
  }
  // Direct memory_* tool: `a` IS the target container, already mutated.
  return true;
}

// Install the policy wrapper after AgentSession has installed extension hooks.
// The returned teardown restores that original hook exactly, so extensions keep
// their own interception lifecycle when Aurelia releases a chat session.
export function installSecurityHook(
  agent: { beforeToolCall?: BeforeToolCall },
  security: SecurityContext,
  audit: (entry: AuditEntry) => void = logAudit,
): () => void {
  const origBeforeToolCall = agent.beforeToolCall;
  if (typeof origBeforeToolCall !== "function") {
    throw new Error("security hook not available: PI SDK version too old");
  }

  const { chat_id, agent_name, profile, cwd } = security;
  agent.beforeToolCall = async (ctx, signal) => {
    // Inject explicit ai-memory project scope from the conversation cwd so
    // the MCP server does not auto-resolve a stale/global project.
    if (cwd) {
      try {
        const injected = injectMcpProjectScope(ctx.toolCall.name, ctx.args, cwd);
        if (injected) {
          redactedLog(`mcp scope: injected project=${deriveProjectName(cwd)} for tool=${ctx.toolCall.name}`);
        }
      } catch (injectError) {
        redactedLog(`mcp scope: injection failed, continuing: ${injectError instanceof Error ? injectError.message : String(injectError)}`);
      }
    }

    const decision = evaluateToolPolicy(
      ctx.toolCall.name,
      ctx.args as Record<string, unknown>,
      security,
    );

    audit({
      decision: decision.decision,
      tool_name: ctx.toolCall.name,
      reason: decision.reason || "",
      chat_id,
      agent_name,
      profile,
      cwd,
      redacted: true,
    });

    if (decision.decision === "block") {
      redactedLog(`security block: tool=${ctx.toolCall.name} reason=${decision.reason}`);
      return { block: true, reason: decision.reason };
    }
    if (decision.decision === "rewrite" && decision.input) {
      if (typeof ctx.args === "object" && ctx.args !== null) {
        Object.assign(ctx.args, decision.input);
      }
    }

    // Extensions are invoked only after Aurelia's policy has allowed or
    // rewritten the shared args object. A throwing extension is logged but
    // cannot convert an allowed policy decision into an unexpected block.
    try {
      return await origBeforeToolCall(ctx, signal);
    } catch (hookError) {
      redactedLog(
        `security: tool=${ctx.toolCall.name} extension hook threw, allowing: ${
          hookError instanceof Error ? hookError.message : String(hookError)
        }`,
      );
      return undefined;
    }
  };

  return () => {
    agent.beforeToolCall = origBeforeToolCall;
  };
}

function textFromContent(content: unknown): string {
  if (!Array.isArray(content)) return "";
  return content
    .map((item) => {
      if (typeof item !== "object" || item === null) return "";
      const block = item as Record<string, unknown>;
      if (block.type !== "text") return "";
      return typeof block.text === "string" ? block.text : "";
    })
    .join("");
}

function textFromMessageContent(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) return textFromContent(content);
  if (typeof content !== "object" || content === null) return "";

  const obj = content as Record<string, unknown>;
  if (typeof obj.text === "string") return obj.text;
  if (obj.content !== undefined) return textFromMessageContent(obj.content);
  return "";
}

function sessionMessageTimestampISO(msg: Record<string, unknown>): string | undefined {
  const raw = msg.timestamp;
  if (typeof raw === "number" && Number.isFinite(raw)) {
    return new Date(raw).toISOString();
  }
  if (typeof raw === "string" && raw.trim() !== "") {
    const parsed = Date.parse(raw);
    if (Number.isFinite(parsed)) return new Date(parsed).toISOString();
  }
  return undefined;
}

function sessionHistoryFromMessages(messages: unknown[], limit = 100): SessionHistoryMessage[] {
  const history: SessionHistoryMessage[] = [];
  for (const raw of messages) {
    if (typeof raw !== "object" || raw === null) continue;

    const msg = raw as Record<string, unknown>;
    const role = typeof msg.role === "string" ? msg.role : "";
    const sender = role === "user" ? "Igor" : role === "assistant" ? "Aurelia" : "";
    if (!sender) continue;

    const text = textFromMessageContent(msg.content).trim();
    if (!text) continue;

    // PI stores message timestamps as Unix milliseconds; normalize to ISO for Go.
    const timestamp = sessionMessageTimestampISO(msg);
    history.push({ sender, text, ...(timestamp ? { timestamp } : {}) });
  }
  if (history.length <= limit) return history;
  return history.slice(history.length - limit);
}

export function resolveModel(
  modelRuntime: Pick<ModelRuntime, "getModel" | "getModels">,
  provider: string | undefined,
  modelID: string | undefined,
) {
  if (!modelID) return undefined;

  const mappedProvider = mapProvider(provider);
  const mappedModel = mapModelForProvider(mappedProvider, modelID);

  // Native PI SDK resolution
  if (mappedProvider) {
    const found = modelRuntime.getModel(mappedProvider, mappedModel);
    if (found) return found;
  }

  // Fallback: exact ID match among all models
  return modelRuntime.getModels().find((m) => m.id === mappedModel);
}

async function createModelRuntime(agentDir: string): Promise<ModelRuntime> {
  return ModelRuntime.create({
    authPath: join(agentDir, "auth.json"),
    modelsPath: join(agentDir, "models.json"),
    modelsStorePath: join(agentDir, "models-store.json"),
    allowModelNetwork: false,
  });
}

async function resolveSessionManager(opts: RequestOptions | undefined): Promise<SessionManager> {
  const cwd = opts?.cwd || process.cwd();
  if (opts?.persist_session === false) {
    return SessionManager.inMemory(cwd);
  }

  const target = opts?.resume || (opts?.continue ? lastSessionID : "");
  if (target) {
    const cached = sessionByID.get(target);
    if (cached?.file && existsSync(cached.file)) {
      return SessionManager.open(cached.file, undefined, cwd);
    }

    if (existsSync(target)) {
      return SessionManager.open(target, undefined, cwd);
    }

    const sessions = await SessionManager.listAll();
    const match = sessions.find((session) => session.id === target || session.id.startsWith(target));
    if (match) {
      return SessionManager.open(match.path, undefined, cwd);
    }

    redactedLog(`session not found for resume=${redactSDKError(target)}; starting a new session`);
  }

  return SessionManager.create(cwd);
}

// ── Create PI Session ───────────────────────────────────────────────────────

// Serialize createPiSession calls to prevent concurrent extension loading
// (resourceLoader.reload, SQLite open, etc.) from crashing the bridge when
// lifecycle get-session-stats runs while a query is in progress.
let piSessionLock: Promise<void> = Promise.resolve();

async function createPiSession(opts: RequestOptions | undefined) {
  // Wait for any previous createPiSession to finish.
  const prev = piSessionLock;
  let release: () => void;
  piSessionLock = new Promise<void>((resolve) => { release = resolve; });
  await prev;

  try {
    const created = await createPiSessionInner(opts);
    markBridgeSessionExtensionRuntimeLoaded(created.session);
    return created;
  } finally {
    release!();
  }
}

async function createPiSessionInner(opts: RequestOptions | undefined) {
  const cwd = opts?.cwd || process.cwd();
  const agentDir = piAgentDir() || getAgentDir();
  const settingsManager = opts?.no_user_settings
    ? SettingsManager.inMemory({
        compaction: { enabled: true },
        retry: { enabled: true, maxRetries: 2 },
      })
    : SettingsManager.create(cwd, agentDir);

  const modelRuntime = await createModelRuntime(agentDir);
  const model = resolveModel(modelRuntime, opts?.provider, opts?.model);
  if (opts?.model && !model) {
    throw new Error(`Modelo não encontrado no PI registry: provider=${opts.provider ?? ""} model=${opts.model}. Use /model para listar os disponíveis.`);
  }

  const resourceLoader = new DefaultResourceLoader({
    cwd,
    agentDir,
    settingsManager,
    noContextFiles: false,  // let PI discover CLAUDE.md/AGENTS.md
    noExtensions: opts?.no_user_settings ?? false,
    noSkills: opts?.no_user_settings ?? false,
    noPromptTemplates: opts?.no_user_settings ?? false,
    noThemes: true,
    systemPromptOverride: () => opts?.system_prompt || undefined,
  });
  await resourceLoader.reload();

  const sessionManager = await resolveSessionManager(opts);

  // Build the effective tool allowlist. The PI SDK's extension system
  // (pi-mcp-adapter, pi-web-access) registers utility tools like `mcp`
  // and `code_search`; translateAllowedTools merges them in whenever
  // the profile provides any allowlist. Returning `undefined` lets the
  // PI SDK fall back to its default discovery — built-ins plus all
  // extension-registered tools. See EXTENSION_UTILITY_TOOLS above for
  // the exact membership.
  const effectiveTools = translateAllowedTools(opts?.allowed_tools, opts?.disallowed_tools);

  return createAgentSession({
    cwd,
    agentDir,
    modelRuntime,
    model,
    resourceLoader,
    sessionManager,
    settingsManager,
    tools: effectiveTools,
  });
}

// The SDK's print and RPC modes explicitly bind extensions before their first
// turn. The bridge is headless, so print mode provides the same lifecycle
// event without inventing a UI context. In particular, pi-mcp-adapter starts
// its lazy server initialization from the session_start event.
export async function bindBridgeSessionExtensions(
  session: BridgeSession,
): Promise<void> {
  const lifecycle = bridgeSessionLifecycleFor(session);
  if (lifecycle.bindingCompleted) return;

  lifecycle.extensionActivationStarted = true;
  try {
    await session.bindExtensions({ mode: "print" });
    lifecycle.bindingCompleted = true;
  } catch (err: unknown) {
    // bindExtensions emits session_start before it finishes resource setup.
    // If a later initialization step fails, give already-started extensions
    // the matching shutdown event before preserving the original bind error.
    await disposeBridgeSession(session);
    throw err;
  }
}

interface InFlightBridgeSessionBind {
  session: BridgeSession;
  promise: Promise<void>;
}

/**
 * Coordinates one query's created session with cancellation.
 *
 * handleQuery cannot publish a ChatSessionState until extension binding has
 * completed, so key-based cleanup alone cannot find a session while binding
 * is gated. This controller keeps the exact session and bind promise owned by
 * the request; cancel() waits for creation and binding to settle, then tears
 * down that exact session. The controller is intentionally independent from
 * provider calls, timers, and the global chat-session map so the race is
 * deterministic to exercise.
 */
export interface BridgeSessionRequestLifecycle {
  createSession<T extends { session: BridgeSession }>(factory: () => Promise<T>): Promise<T>;
  trackSession(session: BridgeSession): void;
  markSessionCreationComplete(): void;
  isCanceled(): boolean;
  bindSession(session: BridgeSession): Promise<void>;
  cancel(): Promise<void>;
}

export function createBridgeSessionRequestLifecycle(): BridgeSessionRequestLifecycle {
  let trackedSession: BridgeSession | undefined;
  let sessionCreationComplete = false;
  let resolveSessionCreation!: () => void;
  const sessionCreated = new Promise<void>((resolve) => {
    resolveSessionCreation = resolve;
  });
  let canceled = false;
  let inFlightBind: InFlightBridgeSessionBind | undefined;
  let cancelPromise: Promise<void> | undefined;

  const trackSession = (session: BridgeSession): void => {
    if (trackedSession && trackedSession !== session) {
      throw new Error("bridge request cannot track more than one session");
    }
    trackedSession = session;
    markBridgeSessionExtensionRuntimeLoaded(session);
  };

  const markSessionCreationComplete = (): void => {
    if (sessionCreationComplete) return;
    sessionCreationComplete = true;
    resolveSessionCreation();
  };

  const createSession = async <T extends { session: BridgeSession }>(
    factory: () => Promise<T>,
  ): Promise<T> => {
    try {
      const created = await factory();
      trackSession(created.session);
      return created;
    } finally {
      // The SDK factory is not cancelable. Resolving this in a finally block
      // makes both success and rejection observable to cancel(), without
      // changing the factory's provider/error semantics.
      markSessionCreationComplete();
    }
  };

  const bindSession = async (session: BridgeSession): Promise<void> => {
    if (trackedSession !== session) {
      throw new Error("bridge request bind does not match its tracked session");
    }
    if (canceled) {
      await disposeBridgeSession(session);
      throw new Error("request canceled");
    }

    // Calling bindBridgeSessionExtensions() has no await before it returns;
    // install the exact-session record before any other request callback can
    // run. cancel() then serializes disposal after this promise settles.
    const bindPromise = bindBridgeSessionExtensions(session);
    const pending: InFlightBridgeSessionBind = { session, promise: bindPromise };
    inFlightBind = pending;
    try {
      await bindPromise;
    } finally {
      if (inFlightBind === pending) inFlightBind = undefined;
    }
  };

  const cancel = (): Promise<void> => {
    canceled = true;
    if (!cancelPromise) {
      cancelPromise = (async () => {
        // If cancellation wins while createPiSession is still loading
        // extensions, wait until the created session is observable before
        // deciding whether exact cleanup is needed.
        await sessionCreated;

        const pending = inFlightBind;
        if (pending) {
          try {
            await pending.promise;
          } catch {
            // bindBridgeSessionExtensions preserves and rethrows the bind
            // error to handleQuery; cancellation must still finish teardown.
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
    cancel,
  };
}

// ── Handle a single query command ────────────────────────────────────────────

async function handleQuery(req: Request): Promise<void> {
  const reqId = req.request_id || "";
  const opts = req.options;
  const chatID = opts?.chat_id || opts?.security?.chat_id || 0;
  const threadID = opts?.thread_id ?? opts?.security?.thread_id ?? 0;
  const userID = opts?.user_id ?? opts?.security?.user_id ?? 0;
  const cKey = chatKey(chatID, threadID, userID);
  const sessionOwner = claimChatSessionOwner(cKey);
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

  // Redact BEFORE truncation so secrets straddling the boundary aren't
  // sliced in half (see redaction-before-truncation.md).
  const redactedPrompt = redactSDKError(req.prompt);
  redactedLog(
    `query start — rid=${reqId} chat=${chatID} thread=${threadID} user=${userID} provider=${opts?.provider ?? "default"} model=${opts?.model ?? "default"} resume=${opts?.resume ?? "none"} prompt="${redactedPrompt.slice(0, 80)}..."`,
  );

  const timeoutMs = 30 * 60 * 1000;
  let timeout: ReturnType<typeof setTimeout> | undefined;
  let canceled = false;
  let terminalEmitted = false;
  let turnCount = 0;
  let session: Awaited<ReturnType<typeof createPiSession>>["session"] | undefined;
  let healthTimer: ReturnType<typeof setInterval> | undefined;
  const startedAt = Date.now();
  const sessionLifecycle = createBridgeSessionRequestLifecycle();

  const emitTerminalError = (message: string): void => {
    if (terminalEmitted) return;
    terminalEmitted = true;
    emitReq({ event: "error", message: redactSDKError(message) });
  };

  const cancelActive = async (reason: string): Promise<void> => {
    if (canceled) return;
    canceled = true;
    redactedLog(`query cancel — rid=${reqId} reason=${reason}`);
    const cleanup = cleanupChatSession(cKey, sessionOwner);
    emitTerminalError(reason);
    activeRequests.delete(reqId);
    await Promise.all([cleanup, sessionLifecycle.cancel()]);
  };

  activeRequests.set(reqId, { cancel: cancelActive });
  timeout = setTimeout(async () => {
    redactedLog(`query timeout — rid=${reqId} no result after 30 minutes`);
    await cancelActive("query timeout: no result after 30 minutes");
  }, timeoutMs);

  try {
    // Clean up any existing session for this chat
    await cleanupChatSession(cKey);
    if (canceled || !ownsChatSession(cKey, sessionOwner)) {
      throw new Error("request canceled");
    }

    // Compute effective tools before session creation so we can report
    // the full list (including extension tools like mcp, code_search, etc.)
    // even if the PI SDK's getActiveToolNames() returns a stale/incomplete
    // set (see lessons/pi-sdk-extension-tools-must-survive-allowlist.md).
    const effectiveToolNames = translateAllowedTools(
      opts?.allowed_tools,
      opts?.disallowed_tools,
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
      // This request became stale while extension binding was pending. It
      // never owned chatSessions, so dispose this exact session directly;
      // key-only cleanup could remove the replacement session.
      await disposeBridgeSession(liveSession);
      throw new Error("request canceled");
    }

    const sessionID = liveSession.sessionId;
    lastSessionID = sessionID;
    rememberSession(sessionID, { id: sessionID, file: liveSession.sessionFile });

    // Report tools from our computed allowlist (authoritative) rather than
    // getActiveToolNames() which may omit extension-registered tools due to
    // PI SDK internal timing (see _refreshToolRegistry / setActiveToolsByName).
    const piActive = liveSession.getActiveToolNames();
    const hasMcpProxy = effectiveToolNames.includes("mcp");
    const hasWebSearch = effectiveToolNames.includes("web_search");
    const profile = opts?.security?.profile ?? "none";
    redactedLog(
      `session tools — rid=${reqId} profile=${profile} ` +
      `active=[${effectiveToolNames.join(",")}] ` +
      `pi_active=[${piActive.join(",")}] ` +
      `mcp_proxy=${hasMcpProxy ? "on" : "off"} web_search=${hasWebSearch ? "on" : "off"}`,
    );

    emitReq({
      event: "system",
      session_id: sessionID,
      session_file: liveSession.sessionFile,
      tools: effectiveToolNames,
      model: liveSession.model ? `${liveSession.model.provider}/${liveSession.model.id}` : "",
    });

    // Set up persistent subscription for this session
    // Counts events for health diagnostics (logged after 30s of silence).
    let lastEventTime = Date.now();
    const unsubPersistent = liveSession.subscribe((event) => {
      if (terminalEmitted) return;
      const rid = chatSessions.get(cKey)?.currentReqId || reqId;
      const eReq = (obj: OutEvent) => emit({ ...obj, request_id: rid });

      switch (event.type) {
        // lastEventTime is only updated on content-producing events so lifecycle
        // events (turn_start/end, compactions, retries) don't mask real stalls.
        case "message_update": {
          lastEventTime = Date.now();
          const update = event.assistantMessageEvent;
          if (update.type === "text_delta") {
            eReq({ event: "assistant", text: update.delta });
          }
          break;
        }
        case "tool_execution_start": {
          lastEventTime = Date.now();
          redactedLog(
            `tool: ${event.toolName} id=${event.toolCallId.slice(0, 8)} rid=${rid}`,
          );
          eReq({
            event: "tool_use",
            id: event.toolCallId,
            name: event.toolName,
            input: event.args,
          });
          break;
        }
        case "tool_execution_end": {
          lastEventTime = Date.now();
          eReq({
            event: "tool_result",
            content: textFromContent(event.result?.content),
          });
          break;
        }
        case "agent_start": {
          eReq({ event: "agent_start" });
          break;
        }
        case "agent_end": {
          eReq({ event: "agent_end" });
          break;
        }
        case "turn_start": {
          eReq({ event: "turn_start" });
          break;
        }
        case "turn_end": {
          turnCount += 1;
          eReq({ event: "turn_end" });
          break;
        }
        case "auto_retry_start": {
          eReq({
            event: "auto_retry_start",
            attempt: event.attempt,
            max_attempts: event.maxAttempts,
            error: event.errorMessage,
          });
          break;
        }
        case "auto_retry_end": {
          eReq({
            event: "auto_retry_end",
            success: event.success,
            attempt: event.attempt,
            error: event.finalError,
          });
          break;
        }
        case "compaction_start": {
          eReq({
            event: "compaction_start",
            reason: event.reason,
          });
          break;
        }
        case "compaction_end": {
          eReq({
            event: "compaction_end",
            reason: event.reason,
            tokens_before: event.result?.tokensBefore ?? 0,
            success: !!event.result && !event.aborted,
            error: event.errorMessage,
          });
          break;
        }
        default:
          break;
      }
    });

    // Health check: monitor for stalls during active query.
    // At 30s: log warning (diagnostic).
    // At 60s: send a steer to nudge the model back to producing output.
    // At 120s: send a more urgent steer.
    let stallSteerSent = false;
    let stallUrgentSent = false;
    healthTimer = setInterval(() => {
      if (terminalEmitted || canceled) {
        clearInterval(healthTimer);
        return;
      }
      const silent = Date.now() - lastEventTime;
      // Reset steer flags when activity resumes (content events arrived).
      if (silent < 30_000) {
        stallSteerSent = false;
        stallUrgentSent = false;
      }
      if (silent >= 30_000) {
        redactedLog(
          `streaming stall: no PI SDK events for ${Math.round(silent / 1000)}s (rid=${reqId})`,
        );
      }
      if (silent >= 60_000 && !stallSteerSent) {
        stallSteerSent = true;
        try {
          liveSession.steer(
            "Continue please. You have been silent for over a minute. " +
            "If you have finished your current task, present your findings."
          ).then(() => {
            redactedLog(`stall steer sent at ${Math.round(silent / 1000)}s (rid=${reqId})`);
          }).catch((err: unknown) => {
            redactedLog(`stall steer failed: ${err instanceof Error ? err.message : String(err)}`);
          });
        } catch (err: unknown) {
          redactedLog(`stall steer failed (sync): ${err instanceof Error ? err.message : String(err)}`);
        }
      }
      if (silent >= 120_000 && !stallUrgentSent) {
        stallUrgentSent = true;
        try {
          liveSession.steer(
            "You have been silent for over 2 minutes. " +
            "Stop your current activity and present a summary of what you have done so far."
          ).then(() => {
            redactedLog(`stall urgent steer sent at ${Math.round(silent / 1000)}s (rid=${reqId})`);
          }).catch((err: unknown) => {
            redactedLog(`stall urgent steer failed: ${err instanceof Error ? err.message : String(err)}`);
          });
        } catch (err: unknown) {
          redactedLog(`stall urgent steer failed (sync): ${err instanceof Error ? err.message : String(err)}`);
        }
      }
    }, 15_000);

    // Register security tool_call hook if enabled.
    // Uses session.agent.beforeToolCall (PI SDK hook that can block tools) rather than
    // the non-existent session.on("tool_call") — the Agent class exposes beforeToolCall
    // as a direct property, and AgentSession._installAgentToolHooks() already sets it for
    // the extension runner. We wrap the existing hook to chain security before extensions.
    let unsubHook: (() => void) | undefined;
    if (opts?.security?.enabled) {
      unsubHook = installSecurityHook(liveSession.agent, opts.security!);
    }

    // Store the chat session
    const cs: ChatSessionState = {
      session: liveSession,
      owner: sessionOwner,
      sessionId: sessionID,
      sessionFile: liveSession.sessionFile,
      currentReqId: reqId,
      unsubPersistent,
      unsubHook,
      createdAt: Date.now(),
    };
    if (!registerSessionIfOwner(chatSessionOwners, chatSessions, cKey, sessionOwner, cs)) {
      await disposeBridgeSession(liveSession);
      throw new Error("request canceled");
    }

    // Re-check cancel after storing — guards race between session setup and cancel
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
        const contentBlocks: Array<TextContent | ImageContent> = [{ type: "text", text: req.prompt }];
        for (const img of images) {
          if (!img.data || !img.media_type) {
            redactedLog(`query image skipped: missing inline data or media type rid=${reqId}`);
            continue;
          }
          contentBlocks.push({
            type: "image",
            data: img.data,
            mimeType: img.media_type,
          });
        }
        await liveSession.sendUserMessage(contentBlocks);
      } else {
        await liveSession.prompt(req.prompt, { source: "rpc" });
      }
    } finally {
      // Keep subscription alive — session stays for steer/followUp/abort
      // Only clean up if canceled (disposed via cancelActive or canceled guard)
    }

    // Check for PI SDK error state after prompt completes.
    // The SDK clears state.errorMessage at run start, so we also check:
    // 1. state.errorMessage (set during current run failures)
    // 2. Last message stopReason (set on API errors)
    // 3. Heuristic: 0 tokens with work claimed = silent failure
    if (!terminalEmitted && !canceled) {
      const stats = liveSession.getSessionStats();
      const piError = liveSession.state.errorMessage;
      const lastMessages = liveSession.state.messages ?? [];
      let stopReason: string | undefined;
      let lastErrorMsg: string | undefined;
      if (lastMessages.length > 0) {
        const lastMsg = lastMessages[lastMessages.length - 1];
        if (lastMsg.role === "assistant") {
          stopReason = (lastMsg as any).stopReason;
          lastErrorMsg = (lastMsg as any).errorMessage;
        }
      }

      const hasExplicitError = piError || stopReason === "error" || lastErrorMsg;
      const zeroTokens = stats.tokens.input === 0
        && stats.tokens.output === 0
        && stats.cost === 0;
      const silentFailure = zeroTokens
        && (turnCount > 0 || stats.assistantMessages > 0);
      const noWorkDone = zeroTokens
        && stats.assistantMessages === 0
        && turnCount === 0;

      if (hasExplicitError || silentFailure || noWorkDone) {
        const errMsg = piError
          || lastErrorMsg
          || (stopReason === "error" ? `PI SDK error (stopReason=error, 0 tokens)` : '')
          || (silentFailure ? `PI SDK completed with 0 tokens — possible API error (check provider credits/auth)` : '')
          || (noWorkDone ? `PI SDK completed with no work done` : '')
          || `PI SDK returned error state`;
        redactedLog(`query PI SDK error: rid=${reqId} stopReason=${stopReason ?? "unknown"} ${errMsg}`);
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
        output_tokens: stats.tokens.output,
      });

      // Start idle timer — session stays alive for subsequent steer/followUp/abort
      const currentCs = chatSessions.get(cKey);
      if (currentCs?.owner === sessionOwner) {
        startIdleTimer(currentCs, cKey);
      }
    }
  } catch (err: unknown) {
    // Remove a failed session whether setup stopped before storage or failed
    // after it was stored. Never clean a newer replacement for this request.
    if (session) {
      const current = chatSessions.get(cKey);
      if (current?.session === session && current.owner === sessionOwner && ownsChatSession(cKey, sessionOwner)) {
        await cleanupChatSession(cKey, sessionOwner);
      } else {
        // The request may have been superseded after it lost ownership. Do
        // not use key-only cleanup here: the key may now address a newer
        // session, or no session may have been registered at all.
        await disposeBridgeSession(session);
      }
    }
    if (!terminalEmitted) {
      const rawErrMsg = err instanceof Error ? err.message : String(err);
      const isBilling = isBillingError(rawErrMsg);
      const userFriendlyMsg = isBilling
        ? "Provider sem créditos suficientes. Troque o modelo com /model ou adicione créditos."
        : rawErrMsg;
      redactedLog(`query error: rid=${reqId} ${rawErrMsg}`);
      emitTerminalError(userFriendlyMsg);
    }
  } finally {
    if (timeout) clearTimeout(timeout);
    if (healthTimer) clearInterval(healthTimer);
    activeRequests.delete(reqId);
    sessionLifecycle.markSessionCreationComplete();
    if (!chatSessions.has(cKey) && ownsChatSession(cKey, sessionOwner)) {
      chatSessionOwners.delete(cKey);
    }
    // Session is NOT disposed here when stored — it stays in chatSessions for steer/followUp/abort
  }
}

// ── Handle steer command ────────────────────────────────────
async function handleSteer(req: Request): Promise<void> {
  const reqId = req.request_id || "";
  const chatID = req.options?.chat_id || req.options?.security?.chat_id || 0;
  const threadID = req.options?.thread_id ?? req.options?.security?.thread_id ?? 0;
  const userID = req.options?.user_id ?? req.options?.security?.user_id ?? 0;
  const cKey = chatKey(chatID, threadID, userID);
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

  const cs = chatSessions.get(cKey);
  if (!cs) {
    emitReq({ event: "result", content: "no active session" });
    return;
  }

  clearTimeout(cs.idleTimer);
  redactedLog(`steer — rid=${reqId} chat=${chatID} thread=${threadID} user=${userID}`);

  try {
    await cs.session.steer(req.prompt);
    emitReq({ event: "result", content: "steer queued" });
  } catch (err: unknown) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog(`steer error: rid=${reqId} ${errMsg}`);
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    startIdleTimer(cs, cKey);
  }
}

// ── Handle follow-up command ────────────────────────────────
async function handleFollowUp(req: Request): Promise<void> {
  const reqId = req.request_id || "";
  const chatID = req.options?.chat_id || req.options?.security?.chat_id || 0;
  const threadID = req.options?.thread_id ?? req.options?.security?.thread_id ?? 0;
  const userID = req.options?.user_id ?? req.options?.security?.user_id ?? 0;
  const cKey = chatKey(chatID, threadID, userID);
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

  const cs = chatSessions.get(cKey);
  if (!cs) {
    emitReq({ event: "result", content: "no active session" });
    return;
  }

  clearTimeout(cs.idleTimer);
  redactedLog(`followUp — rid=${reqId} chat=${chatID} thread=${threadID} user=${userID}`);

  try {
    await cs.session.followUp(req.prompt);
    emitReq({ event: "result", content: "follow-up queued" });
  } catch (err: unknown) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog(`followUp error: rid=${reqId} ${errMsg}`);
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    startIdleTimer(cs, cKey);
  }
}

// ── Handle abort command ────────────────────────────────────
async function handleAbort(req: Request): Promise<void> {
  const reqId = req.request_id || "";
  const chatID = req.options?.chat_id || req.options?.security?.chat_id || 0;
  const threadID = req.options?.thread_id ?? req.options?.security?.thread_id ?? 0;
  const userID = req.options?.user_id ?? req.options?.security?.user_id ?? 0;
  const cKey = chatKey(chatID, threadID, userID);
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

  const cs = chatSessions.get(cKey);
  if (!cs) {
    emitReq({ event: "result", content: "no active session" });
    return;
  }

  redactedLog(`abort — rid=${reqId} chat=${chatID} thread=${threadID} user=${userID}`);

  try {
    await cs.session.abort();
    emitReq({ event: "result", content: "session aborted" });
  } catch (err: unknown) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog(`abort error: rid=${reqId} ${errMsg}`);
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    await cleanupChatSession(cKey, cs.owner);
  }
}

// ── Handle get-state command ────────────────────────────────
async function handleGetState(req: Request): Promise<void> {
  const reqId = req.request_id || "";
  const chatID = req.options?.chat_id || req.options?.security?.chat_id || 0;
  const threadID = req.options?.thread_id ?? req.options?.security?.thread_id ?? 0;
  const userID = req.options?.user_id ?? req.options?.security?.user_id ?? 0;
  const cKey = chatKey(chatID, threadID, userID);
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

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
      session_id: cs.sessionId,
    }),
  });
}

// ── Handle get-session-stats command ───────────────────────────────────────

async function handleGetSessionStats(req: Request): Promise<void> {
  const reqId = req.request_id || "";
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

  let piSession: Awaited<ReturnType<typeof createPiSession>> | undefined;
  try {
    // Open or resolve session temporarily; do not retain in chatSessions.
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
        context_usage_pct: usage?.percent ?? 0,
      }),
    });
  } catch (err: unknown) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog(`get-session-stats error: rid=${reqId} ${errMsg}`);
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    // Dispose of the temporary session even on error paths.
    if (piSession) {
      await disposeBridgeSession(piSession.session);
    }
  }
}

// ── Handle get-session-history command ─────────────────────────────────────

async function handleGetSessionHistory(req: Request): Promise<void> {
  const reqId = req.request_id || "";
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });
  const opts = req.options;

  if (!opts?.resume) {
    emitReq({ event: "result", content: "[]" });
    return;
  }
  if (!existsSync(opts.resume)) {
    redactedLog(`get-session-history skipped missing resume=${redactSDKError(opts.resume)}`);
    emitReq({ event: "result", content: "[]" });
    return;
  }

  let session: Awaited<ReturnType<typeof createPiSession>>["session"] | undefined;
  try {
    const piSession = await createPiSession({ ...opts, continue: false });
    session = piSession.session;
    await bindBridgeSessionExtensions(session);
    const messages = session.state.messages ?? [];
    const history = sessionHistoryFromMessages(messages, 100);
    emitReq({ event: "result", content: JSON.stringify(history) });
  } catch (err: unknown) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog(`get-session-history error: rid=${reqId} ${errMsg}`);
    emitReq({ event: "error", message: `get-session-history failed: ${redactSDKError(errMsg)}` });
  } finally {
    if (session) {
      await disposeBridgeSession(session);
    }
  }
}

// ── Handle compact-session command ────────────────────────────────────────

async function handleCompactSession(req: Request): Promise<void> {
  const reqId = req.request_id || "";
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

  let piSession: Awaited<ReturnType<typeof createPiSession>> | undefined;
  let unsub: (() => void) | undefined;
  try {
    piSession = await createPiSession(req.options);
    const session = piSession.session;
    await bindBridgeSessionExtensions(session);

    let terminalEmitted = false;

    // Subscribe to compaction events and forward them
    unsub = session.subscribe((event) => {
      if (terminalEmitted) return;
      if (event.type === "compaction_start") {
        emitReq({ event: "compaction_start", reason: event.reason });
      } else if (event.type === "compaction_end") {
        emitReq({
          event: "compaction_end",
          reason: event.reason,
          tokens_before: event.result?.tokensBefore ?? 0,
          success: !!event.result && !event.aborted,
          error: event.errorMessage,
        });
      }
    });

    // Signal start
    emitReq({ event: "compaction_start", reason: "manual" });

    const customInstructions = req.prompt || undefined;
    const result = await session.compact(customInstructions);

    if (!terminalEmitted) {
      terminalEmitted = true;
      emitReq({
        event: "compaction_end",
        reason: "manual",
        tokens_before: result?.tokensBefore ?? 0,
        success: !!result,
      });

      // Return result as terminal event
      emitReq({
        event: "result",
        content: JSON.stringify({
          success: !!result,
          tokens_before: result?.tokensBefore ?? 0,
          summary: result?.summary ?? "",
          session_id: session.sessionId,
          session_file: session.sessionFile,
        }),
      });
    }
  } catch (err: unknown) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog(`compact-session error: rid=${reqId} ${errMsg}`);
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    if (unsub) try { unsub(); } catch { /* ignore */ }
    if (piSession) {
      await disposeBridgeSession(piSession.session);
    }
  }
}

// ── Helpers ──────────────────────────────────────────────────────────────────

/** Poll pendingMessageCount until it drops to 0, capped at timeoutMs.
 *
 * Exported for testing. Not part of the protocol API.
 *
 * ponytail: ceiling — uses polling because the SDK provides no completion
 * event for session persistence. In practice sendUserMessage resolves after
 * the message is appended to state; this poll is a safety net. Upgrade to
 * an SDK-level persistence signal when one is available.
 */
export async function waitForPendingMessageCount(
  session: { readonly pendingMessageCount: number },
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (session.pendingMessageCount > 0) {
    if (Date.now() >= deadline) break;
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
}

// ── Handle rotate-session command ───────────────────────────────────────────

async function handleRotateSession(req: Request): Promise<void> {
  const reqId = req.request_id || "";
  const opts = req.options;
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

  let oldPiSession: Awaited<ReturnType<typeof createPiSession>> | undefined;
  let newPiSession: Awaited<ReturnType<typeof createPiSession>> | undefined;
  try {
    // 1. Open current session to get stats and generate summary
    oldPiSession = await createPiSession(opts);
    const oldSession = oldPiSession.session;
    await bindBridgeSessionExtensions(oldSession);

    // Get stats for summary context
    const stats = oldSession.getSessionStats();

    // Generate compact summary of the old session
    const compactionResult = await oldSession.compact(
      "Summarize the session's goal, current state, completed work, " +
      "in-progress items, key decisions, files read/modified, and next actions."
    );

    const summary = escapeUntrustedSummary(compactionResult?.summary ?? "");
    const sessionId = oldSession.sessionId;
    const oldFile = oldSession.sessionFile;

    redactedLog(`rotate: old session id=${sessionId} file=${oldFile} tokens=${stats.tokens.total}`);

    // Emit progress
    emitReq({ event: "system", message: "Generating session summary..." });

    // Dispose old session (don't need to keep it alive after compaction)
    await disposeBridgeSession(oldSession);
    oldPiSession = undefined; // Mark as disposed for the finally block

    // 2. Create a new fresh session with same options (cwd, model, tools, security)
    // Force no-resume by clearing resume/continue
    const newOpts: RequestOptions = { ...opts };
    newOpts.resume = undefined;
    newOpts.continue = false;
    newOpts.persist_session = true;

    newPiSession = await createPiSession(newOpts);
    const newSession = newPiSession.session;
    await bindBridgeSessionExtensions(newSession);
    const newFile = newSession.sessionFile;
    const newId = newSession.sessionId;

    redactedLog(`rotate: new session id=${newId} file=${newFile}`);

    // 3. Inject structured summary as untrusted context
    const summaryBlock = `<previous_session_summary_untrusted>
${summary}
</previous_session_summary_untrusted>`;

    // Send as initial user message so the agent has context.
    // sendUserMessage resolves after the message is appended to session state.
    await newSession.sendUserMessage([{ type: "text", text: summaryBlock }]);

    // Wait for pending message count to settle (safety net — should be 0 already).
    await waitForPendingMessageCount(newSession, 5000);

    emitReq({
      event: "result",
      content: JSON.stringify({
        success: true,
        old_session_file: oldFile,
        old_session_id: sessionId,
        new_session_file: newFile,
        new_session_id: newId,
        summary_length: summary.length,
        tokens_before: stats.tokens.total,
      }),
    });

    // Dispose the new session — the Go side will store the new file path
    // and the bridge will open it fresh on the next query.
    await disposeBridgeSession(newSession);
    newPiSession = undefined; // Mark as disposed for the finally block
  } catch (err: unknown) {
    const errMsg = err instanceof Error ? err.message : String(err);
    redactedLog(`rotate-session error: rid=${reqId} ${errMsg}`);
    emitReq({ event: "error", message: redactSDKError(errMsg) });
  } finally {
    // Clean up any session that wasn't disposed on the happy path.
    if (oldPiSession) {
      await disposeBridgeSession(oldPiSession.session);
    }
    if (newPiSession) {
      await disposeBridgeSession(newPiSession.session);
    }
  }
}

// ── Handle incoming request ──────────────────────────────────────────────────

async function handleRequest(line: string): Promise<void> {
  let req: Request;

  try {
    req = JSON.parse(line) as Request;
  } catch {
    emit({ event: "error", message: `invalid JSON: ${redactSDKError(line).slice(0, 200)}` });
    return;
  }

  if (!req.command) {
    emit({ event: "error", request_id: req.request_id || "", message: "missing 'command' field" });
    return;
  }

  const reqId = req.request_id || "";
  const emitReq = (obj: OutEvent) => emit({ ...obj, request_id: reqId });

  switch (req.command) {
    case "query": {
      if (!req.prompt) {
        emit({ event: "error", request_id: reqId, message: "missing 'prompt' field for query command" });
        return;
      }
      await handleQuery(req);
      break;
    }

    case "ping": {
      emit({ event: "pong", request_id: reqId });
      break;
    }

    case "cancel": {
      const target = req.target_request_id || "";
      if (!target) {
        emitReq({ event: "error", message: "missing 'target_request_id' for cancel command" });
        return;
      }
      const active = activeRequests.get(target);
      if (!active) {
        emitReq({ event: "result", content: `request ${target} is not active` });
        return;
      }
      await active.cancel("request canceled");
      emitReq({ event: "result", content: `request ${target} canceled` });
      break;
    }

    case "list-models": {
      try {
        const agentDir = piAgentDir() || getAgentDir();
        const modelRuntime = await createModelRuntime(agentDir);
        // When the caller explicitly asks for a refresh, force the PI SDK to
        // reload models.json and reset cached provider state. Without this,
        // edits to models.json or dynamic provider registrations may not be
        // reflected until the bridge process restarts.
        if (req.refresh) {
          await modelRuntime.refresh({ allowNetwork: true });
        }
        // Only show models with configured auth (checks auth.json, env vars, and models.json apiKey fallback)
        const available = await modelRuntime.getAvailable();
        // Filter: only expose models that resolveModel can find at query time.
        // This prevents the "model not found in PI registry" runtime error
        // when getAvailable() returns models that registry.find() cannot resolve
        // because of provider/model name mapping differences (see #220).
        const filtered = available.filter((m) => {
          const found = resolveModel(modelRuntime, m.provider, m.id);
          if (!found) {
            redactedLog(`list-models: excluding unresolvable model provider=${m.provider} model=${m.id}`);
          }
          return !!found;
        });
        const summary = filtered.map((m) => ({
          provider: m.provider,
          id: m.id,
          name: m.name ?? m.id,
          supportsImages: m.input?.includes("image") ?? false,
        }));
        emitReq({ event: "result", content: JSON.stringify(summary) });
      } catch (err: unknown) {
        const errMsg = err instanceof Error ? err.message : String(err);
        redactedLog(`list-models error: ${errMsg}`);
        emitReq({ event: "error", message: `list-models failed: ${redactSDKError(errMsg)}` });
      }
      break;
    }

    case "steer": {
      if (!req.prompt) {
        emit({ event: "error", request_id: reqId, message: "missing 'prompt' field for steer command" });
        return;
      }
      await handleSteer(req);
      break;
    }

    case "follow-up": {
      if (!req.prompt) {
        emit({ event: "error", request_id: reqId, message: "missing 'prompt' field for follow-up command" });
        return;
      }
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

    default: {
      emit({ event: "error", request_id: reqId, message: `unknown command: ${req.command}` });
    }
  }
}

// ── Main loop ────────────────────────────────────────────────────────────────

function main(): void {
  log("bridge started — waiting for commands on stdin");

  const rl = createInterface({
    input: process.stdin,
    terminal: false,
  });

  rl.on("line", (line: string) => {
    const trimmed = line.trim();
    if (!trimmed) return;

    handleRequest(trimmed).catch((err: unknown) => {
      const errMsg = err instanceof Error ? err.message : String(err);
      redactedLog(`unhandled error in request processing: ${errMsg}`);
      emit({ event: "error", message: `internal bridge error: ${redactSDKError(errMsg)}` });
    });
  });

  rl.on("close", () => {
    log("stdin closed — shutting down");
    process.exit(0);
  });

  process.on("unhandledRejection", (reason: unknown) => {
    const msg = reason instanceof Error ? reason.message : String(reason);
    redactedLog(`unhandled rejection: ${msg}`);
    emit({ event: "error", message: `unhandled rejection: ${redactSDKError(msg)}` });
  });

  process.on("uncaughtException", (err: Error) => {
    redactedLog(`uncaught exception: ${err.message}`);
    emit({ event: "error", message: `uncaught exception: ${redactSDKError(err.message)}` });
    process.exit(1);
  });
}

main();
