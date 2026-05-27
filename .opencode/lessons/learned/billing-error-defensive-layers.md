# Billing Error: Defensive Detection at Every Layer

**Date**: 2026-05-27
**Change**: fix/billing-error-handling
**Category**: pattern

## What happened

Billing errors (401 Insufficient balance) from the PI SDK needed to be detected
and handled differently at 3 layers:
1. Bridge — emit clear user-facing message instead of raw SDK error
2. Pipeline — don't contaminate session suspect counters (skip MarkEmptyResult/MarkFailure)
3. Lifecycle — suppress misleading UX retry messages during rotation

Each layer needed its own `isBillingError` detection because error messages
arrive in different formats at each layer (raw SDK error, redacted, or
wrapped as Go error).

## How to avoid

When adding error classification that affects multiple layers:
- Add the detection helper at each layer (bridge TS, pipeline Go) with
  matching patterns
- Use the same exported/package-level function name for grepability
- Guard every session-state mutation (MarkEmptyResult, MarkFailure) — not
  just the main error handler path

## Tags

#lesson #change-fix-billing-error-handling #pattern #error-handling #defensive
