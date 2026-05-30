# Cron CWD Extraction from Prompt

Date: 2026-05-29

## Problem

When a cron job has no `AgentName` configured, the runtime defaults to `observe` profile (no tools). Even if the prompt says "Set cwd to /project. Run: script.py", the LLM sees that as text but has no Bash/Read/Write to actually execute anything.

## Solution

In `internal/cron/runtime.go`, `ExecuteJob()`:

1. Added `extractCwdFromPrompt()` — parses "Set cwd to \<path\>" from the prompt text, looking for `. Run:` or newline as path delimiter
2. When `cwd` is empty after agent resolution, check if the prompt contains "Set cwd to \<path\>" 
3. If found, use that path as the actual `cwd` and set `opts.Cwd`
4. Elevate profile to `execute_safe` when cwd was extracted (the prompt implies execution)

## Key Files

- `internal/cron/runtime.go` — `extractCwdFromPrompt()`, modified `ExecuteJob()`
- `internal/cron/runtime_test.go` — `TestExtractCwdFromPrompt`, `TestBridgeCronRuntime_NoAgentWithCwdInPrompt`

## Edge Cases

- No "Set cwd to" prefix → returns "" (falls through to observe)
- Path at end of string without delimiter → takes full remainder
- Path with trailing spaces → trimmed
- Multiple "Set cwd to" → only first is used
