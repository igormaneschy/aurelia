# Pattern: Persist cron CWD instead of parsing prompts

## Context
Cron jobs can run project scripts long after the original chat context has changed. Encoding the working directory inside natural-language prompts such as `Set cwd to <path>. Run: ...` makes runtime behavior depend on fragile string parsing.

## Lesson
Store `cwd` as first-class cron job metadata and pass it to the bridge/security context at execution time. Keep prompt CWD extraction only as a backward-compatible migration path for old jobs.

## Why
- Prevents `ENAMETOOLONG` and malformed path failures when delimiters are missing.
- Preserves project isolation for Telegram topics and scheduled agents.
- Keeps prompts as action instructions, not configuration transport.

## Checklist
- Add schema fields for runtime binding (`cwd`, and `agent_name` when agent behavior depends on registry lookup).
- Include fields in INSERT/UPDATE/SELECT/scan paths.
- Backfill or refresh existing scheduled jobs on daemon startup.
- Validate CWD before enabling execute-capable tools.
- Keep a regression test for long/malformed prompt-derived CWD.
