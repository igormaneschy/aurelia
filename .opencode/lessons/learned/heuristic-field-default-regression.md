# Heuristic Based on Zero-Value Field Default Causes Silent Regression

**Date**: 2026-06-06
**Change**: prompt-profiles-regression-fix
**Category**: anti-pattern

## What happened

`DefaultProfileForContext` used `pp.CapabilityProfile == ""` as heuristic for "internal system process" (`isInternal=true`), returning `edit_project` (no Bash). All three builtin profiles (general, developer, researcher) and any profile loaded from disk without explicit `capability_profile` field were silently downgraded, losing Bash and web search access. Real internal processes (dream, nudge) set their profiles directly and never called this function — the heuristic served no purpose.

## How to avoid

Never use zero-value fields as proxies for semantic conditions. If a function takes an `isInternal bool`, the caller must determine this from explicit knowledge, not from the absence of optional fields. Validate heuristics against all callers — if a heuristic only matches unintended callers, remove it.

## Tags

#lesson #change-prompt-profiles-regression-fix #anti-pattern #heuristic #security
