import { describe, it } from "node:test";
import assert from "node:assert";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { deriveProjectName, injectMcpProjectScope } from "./index.ts";

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
      const args = { server: "ai-memory", tool: "memory_query", arguments: { query: "handoff" } };
      const changed = injectMcpProjectScope("mcp", args, cwd);
      assert.strictEqual(changed, true);
      assert.strictEqual(args.arguments.project, "AutoTradersOMQS-GO");
      assert.strictEqual(args.arguments.query, "handoff");
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("injects into memory_read_page and memory_handoff_accept", () => {
    const cwd = withGitRepo("aurelia");
    try {
      for (const tool of ["memory_read_page", "memory_handoff_accept", "memory_explore", "memory_recent"]) {
        const args = { server: "ai-memory", tool, arguments: { query: "x" } };
        assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), true);
        assert.strictEqual(args.arguments.project, "aurelia");
      }
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("does NOT override an explicit project scope", () => {
    const cwd = withGitRepo("aurelia");
    try {
      const args = { server: "ai-memory", tool: "memory_query", arguments: { query: "x", project: "codegraph" } };
      assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), false);
      assert.strictEqual(args.arguments.project, "codegraph");
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("does NOT override workspace / scopes / global", () => {
    const cwd = withGitRepo("aurelia");
    try {
      const w = { server: "ai-memory", tool: "memory_query", arguments: { query: "x", workspace: "hermes", project: "trading" } };
      assert.strictEqual(injectMcpProjectScope("mcp", w, cwd), false);
      const s = { server: "ai-memory", tool: "memory_query", arguments: { query: "x", scopes: [{ workspace: "default", project: "a" }] } };
      assert.strictEqual(injectMcpProjectScope("mcp", s, cwd), false);
      const g = { server: "ai-memory", tool: "memory_query", arguments: { query: "x", global: true } };
      assert.strictEqual(injectMcpProjectScope("mcp", g, cwd), false);
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("ignores non-ai-memory servers", () => {
    const cwd = withGitRepo("aurelia");
    try {
      const args = { server: "context7", tool: "memory_query", arguments: { query: "x" } };
      assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), false);
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("ignores non-memory tools on ai-memory server", () => {
    const cwd = withGitRepo("aurelia");
    try {
      const args = { server: "ai-memory", tool: "some_other_tool", arguments: { query: "x" } };
      assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), false);
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("handles string-JSON arguments", () => {
    const cwd = withGitRepo("codegraph");
    try {
      const args = { server: "ai-memory", tool: "memory_query", arguments: JSON.stringify({ query: "x" }) };
      assert.strictEqual(injectMcpProjectScope("mcp", args, cwd), true);
      assert.strictEqual(JSON.parse(args.arguments as string).project, "codegraph");
    } finally {
      rmSync(cwd, { recursive: true, force: true });
    }
  });

  it("ignores malformed JSON-string arguments", () => {
    const cwd = withGitRepo("codegraph");
    try {
      const args = { server: "ai-memory", tool: "memory_query", arguments: "{not-json" };
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
