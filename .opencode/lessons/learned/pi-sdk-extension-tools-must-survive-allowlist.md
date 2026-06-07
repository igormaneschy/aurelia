# PI SDK: extension utility tools must survive the allowlist filter

**Date**: 2026-06-06
**Change**: feature/pi-sdk-tool-availability-audit
**Category**: anti-pattern

## What happened

After enabling MCP proxy mode (`pi-mcp-adapter`) and web access
(`pi-web-access`) extensions, the model could not call any MCP server or
run a web search even though the extensions were installed and
functional. Logs showed `mcp`, `code_search`, `fetch_content`, and
`get_search_content` were present in the **default** PI SDK tool list
but disappeared from the **profile-derived** `allowed_tools` passed to
`createAgentSession({ tools: ... })`.

Root cause: the bridge was passing a `toolPolicy.allowedTools` array
derived from a profile like `["Read", "Write", "Edit", "Bash", "Grep",
"Glob", "LS", "WebSearch"]` straight to the SDK. PI's session factory
treats that list as the **complete** set of tools the model can see —
extension-registered tools (`mcp` from `pi-mcp-adapter`,
`code_search`/`fetch_content`/`get_search_content` from `pi-web-access`)
were not in the list, so the SDK filtered them out. The model then
either:
- Did not know the tools existed (filtered out at system-prompt time)
- Or tried to call them and got a synthetic 401 from the chat-model
  provider (because it routed the call to a non-tool endpoint)

A legacy `discoverMCPTools()` helper tried to read `~/.pi/agent/mcp.json`
and synthesize per-server tool names like `notebooklm__query`, but PI's
MCP proxy is a **single** tool named `mcp` with a JSON-schema envelope
— the synthesized names were never reachable.

## How to avoid

1. When `toolPolicy.allowedTools` is set, **always merge in the
   extension-registered utility tool names** (`mcp`, `code_search`,
   `fetch_content`, `get_search_content`) subject to the explicit
   `disallowedTools` set. The fix is in `bridge/index.ts`
   `translateAllowedTools()` and the constant `EXTENSION_UTILITY_TOOLS`.

2. Do not invent tool names. PI's MCP proxy is a single `mcp` tool
   that takes `{search, tool, args, list}`; one tool, one schema.
   `discoverMCPTools()` was a wrong abstraction — removed it.

3. After every `system` event from the bridge, log the **active tool
   set** (`profile=... active=[...] mcp_proxy=on/off web_search=on/off`).
   Tool-filter regressions are otherwise invisible in production logs.

4. Verify with a probe that spawns the bridge directly:
   ```
   node --experimental-strip-types /tmp/probe_verify.js
   ```
   The probe's `system` event must list `mcp` in the tool names.

## Tags

#lesson #change-feature-pi-sdk-tool-availability-audit #anti-pattern #pi-sdk #bridge #mcp #tool-policy
