import { describe, it } from "node:test";
import assert from "node:assert";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { deriveProjectName, injectMcpProjectScope, installSecurityHook, SecurityContext } from "./index.ts";

// ── deriveProjectName ────────────────────────────────────────────────────

describe("deriveProjectName", () => {
  it("returns basename of the nearest .git directory", () => {
    const root = mkdtempSync(join(tmpdir(), "scope-test-"));
    try {
      mkdirSync(join(root, "AutoTradersOMQS-GO", "nested", "deep"), { recursive: true });
      mkdirSync(join(root, "AutoTradersOMQS-GO", ".git"), { recursive: true });
      assert.strictEqual(
        deriveProjectName(join(root, "AutoTradersOMQS-GO", "nested", "deep")),
        "AutoTradersOMQS-GO",
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("returns the exact cwd basename when cwd itself is a git repo", () => {
    const root = mkdtempSync(join(tmpdir(), "scope-test-"));
    try {
      mkdirSync(join(root, "aurelia", ".git"), { recursive: true });
      assert.strictEqual(deriveProjectName(join(root, "aurelia")), "aurelia");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("returns undefined when no .git is found up the tree", () => {
    const root = mkdtempSync(join(tmpdir(), "scope-test-"));
    try {
      assert.strictEqual(deriveProjectName(join(root, "no-git-here")), undefined);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("returns undefined for empty cwd", () => {
    assert.strictEqual(deriveProjectName(""), undefined);
  });
});

// ── injectMcpProjectScope ────────────────────────────────────────────────

function withGitRepo(name: string): string {
  const root = mkdtempSync(join(tmpdir(), "scope-test-"));
  mkdirSync(join(root, name, ".git"), { recursive: true });
  return join(root, name);
}

describe("injectMcpProjectScope (unified mcp proxy)", () => {
  it("injects project into ai-memory memory_query without explicit scope", () => {
    const cwd = withGitRepo("AutoTradersOMQS-GO");
    try {
      // Real pi-mcp-adapter envelope: { server, tool, args } — the actual
      // tool arguments live under the `args` key, not `arguments`.
      const args = { server: "ai-memory", tool: "memory_query", args: { query: "handoff" } };
      const changed = injectMcpProjectScope("mcp", args, cwd);
      assert.strictEqual(changed, true);
      assert.strictEqual(args.args.project, "AutoTradersOMQS-GO");
      assert.strictEqual(args.args.query, "handoff");
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("injects into memory_read_page and memory_handoff_accept", () => {
    const cwd = withGitRepo("aurelia");
    try {
      for (const tool of ["memory_read_page", "memory_handoff_accept", "memory_explore", "memory_recent"]) {
        const args = { server: "ai-memory", tool, args: { query: "x" } };
        assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), true);
        assert.strictEqual(args.args.project, "aurelia");
      }
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("does NOT override an explicit project scope", () => {
    const cwd = withGitRepo("aurelia");
    try {
      const args = { server: "ai-memory", tool: "memory_query", args: { query: "x", project: "codegraph" } };
      assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), false);
      assert.strictEqual(args.args.project, "codegraph");
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("does NOT override workspace / scopes / global", () => {
    const cwd = withGitRepo("aurelia");
    try {
      const w = { server: "ai-memory", tool: "memory_query", args: { query: "x", workspace: "hermes", project: "trading" } };
      assert.strictEqual(injectMcpProjectScope("mcp", w, cwd), false);
      const s = { server: "ai-memory", tool: "memory_query", args: { query: "x", scopes: [{ workspace: "default", project: "a" }] } };
      assert.strictEqual(injectMcpProjectScope("mcp", s, cwd), false);
      const g = { server: "ai-memory", tool: "memory_query", args: { query: "x", global: true } };
      assert.strictEqual(injectMcpProjectScope("mcp", g, cwd), false);
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("ignores non-ai-memory servers", () => {
    const cwd = withGitRepo("aurelia");
    try {
      const args = { server: "context7", tool: "memory_query", args: { query: "x" } };
      assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), false);
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("ignores non-memory tools on ai-memory server", () => {
    const cwd = withGitRepo("aurelia");
    try {
      const args = { server: "ai-memory", tool: "some_other_tool", args: { query: "x" } };
      assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), false);
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("handles string-JSON args", () => {
    const cwd = withGitRepo("codegraph");
    try {
      const args = { server: "ai-memory", tool: "memory_query", args: JSON.stringify({ query: "x" }) };
      assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), true);
      assert.strictEqual(JSON.parse(args.args as string).project, "codegraph");
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("ignores malformed JSON-string args", () => {
    const cwd = withGitRepo("codegraph");
    try {
      const args = { server: "ai-memory", tool: "memory_query", args: "{not-json" };
      assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), false);
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("no-ops when the mcp envelope uses the legacy `arguments` key", () => {
    const cwd = withGitRepo("aurelia");
    try {
      // The installed pi-mcp-adapter schema has no `arguments` property, so a
      // call shaped like this is ignored by the adapter's execute (it only
      // reads params.args). The interceptor must not half-inject into a key
      // the adapter never reads.
      const args = { server: "ai-memory", tool: "memory_query", arguments: { query: "x" } };
      assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), false);
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });
});

describe("injectMcpProjectScope (direct memory_* tool names)", () => {
  it("injects project for direct memory_query calls", () => {
    const cwd = withGitRepo("AutoTradersOMQS-GO");
    try {
      const args = { query: "handoff" };
      const changed = injectMcpProjectScope("memory_query", args, cwd);
      assert.strictEqual(changed, true);
      assert.strictEqual(args.project, "AutoTradersOMQS-GO");
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("no-ops when cwd has no .git", () => {
    const cwd = mkdtempSync(join(tmpdir(), "scope-test-"));
    try {
      const args = { query: "handoff" };
      assert.strictEqual(injectMcpProjectScope("memory_query", args, cwd), false);
      assert.strictEqual(args.project, undefined);
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("no-ops on non-memory direct tools", () => {
    const cwd = withGitRepo("aurelia");
    try {
      const args = { query: "x" };
      assert.strictEqual(injectMcpProjectScope("bash", args, cwd), false);
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });
});

// ── installSecurityHook: real envelope through the beforeToolCall hook ─────
//
// The pi-mcp-adapter registers a single `mcp` tool with parameters
// { tool, args, server, ... }. The model's call arrives at the hook as
// ctx.args = { server, tool, args } — the real memory_* tool arguments are
// ctx.args.args. These tests exercise the full hook path, not just the
// pure injectMcpProjectScope helper.

describe("installSecurityHook ai-memory scope injection", () => {
  function hookAgent() {
    type HookAgent = Parameters<typeof installSecurityHook>[0];
    const seen: Array<{ name: string; args: Record<string, unknown> }> = [];
    const original: NonNullable<HookAgent["beforeToolCall"]> = async (ctx) => {
      seen.push({ name: ctx.toolCall.name, args: ctx.args as Record<string, unknown> });
      return undefined;
    };
    const agent: HookAgent = { beforeToolCall: original };
    return { agent, original, seen };
  }

  function securityFor(cwd: string): SecurityContext {
    return {
      enabled: true,
      profile: "execute_safe",
      mode: "block",
      cwd,
    };
  }

  it("injects project into ctx.args.args for mcp ai-memory calls", async () => {
    const cwd = withGitRepo("aurelia");
    try {
      const { agent, original, seen } = hookAgent();
      const restore = installSecurityHook(agent, securityFor(cwd), () => {});
      const hook = agent.beforeToolCall!;
      const signal = new AbortController().signal;
      try {
        const ctx = {
          toolCall: { name: "mcp" },
          args: { server: "ai-memory", tool: "memory_query", args: { query: "handoff" } },
        } as Parameters<typeof hook>[0];

        await hook(ctx, signal);

        // The original extension hook saw the mutated envelope.
        assert.strictEqual(seen.length, 1);
        assert.strictEqual(seen[0].name, "mcp");
        const inner = seen[0].args.args as Record<string, unknown>;
        assert.strictEqual(inner.project, "aurelia");
        assert.strictEqual(inner.query, "handoff");
      } finally {
        restore();
        assert.strictEqual(agent.beforeToolCall, original);
      }
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("injects into string-JSON ctx.args.args without double-encoding", async () => {
    const cwd = withGitRepo("codegraph");
    try {
      const { agent, seen } = hookAgent();
      const restore = installSecurityHook(agent, securityFor(cwd), () => {});
      const hook = agent.beforeToolCall!;
      try {
        const ctx = {
          toolCall: { name: "mcp" },
          args: { server: "ai-memory", tool: "memory_query", args: JSON.stringify({ query: "x" }) },
        } as Parameters<typeof hook>[0];

        await hook(ctx, new AbortController().signal);

        assert.strictEqual(seen.length, 1);
        const inner = seen[0].args.args as string;
        assert.strictEqual(JSON.parse(inner).project, "codegraph");
        assert.strictEqual(JSON.parse(inner).query, "x");
      } finally {
        restore();
      }
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("does not inject when the model already passed an explicit scope", async () => {
    const cwd = withGitRepo("aurelia");
    try {
      const { agent, seen } = hookAgent();
      const restore = installSecurityHook(agent, securityFor(cwd), () => {});
      const hook = agent.beforeToolCall!;
      try {
        const ctx = {
          toolCall: { name: "mcp" },
          args: { server: "ai-memory", tool: "memory_query", args: { query: "x", project: "codegraph" } },
        } as Parameters<typeof hook>[0];

        await hook(ctx, new AbortController().signal);

        assert.strictEqual(seen.length, 1);
        const inner = seen[0].args.args as Record<string, unknown>;
        assert.strictEqual(inner.project, "codegraph");
      } finally {
        restore();
      }
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("leaves non-ai-memory mcp calls untouched", async () => {
    const cwd = withGitRepo("aurelia");
    try {
      const { agent, seen } = hookAgent();
      const restore = installSecurityHook(agent, securityFor(cwd), () => {});
      const hook = agent.beforeToolCall!;
      try {
        const ctx = {
          toolCall: { name: "mcp" },
          args: { server: "context7", tool: "resolve-library-id", args: { query: "x" } },
        } as Parameters<typeof hook>[0];

        await hook(ctx, new AbortController().signal);

        assert.strictEqual(seen.length, 1);
        assert.strictEqual(seen[0].args.server, "context7");
      } finally {
        restore();
      }
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });
});
