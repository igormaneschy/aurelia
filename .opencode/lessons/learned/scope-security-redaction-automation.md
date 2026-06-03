# Scope, Security, and Redaction Must Be Automated

## Context

Review remediation on 2026-06-02 found repeat failures already described in the changelog: user isolation gaps, redaction-before-truncation gaps, security policy bypasses outside the Bridge, async state races, and silent best-effort errors.

## Lesson

A lesson documented in the changelog is not enough. If a bug class repeats, encode it as one or more of:

- a type/signature that makes the unsafe shape impossible;
- a shared helper that owns the invariant;
- a lint/test/CI gate that fails on recurrence;
- a PR checklist item tied to the affected subsystem.

## Apply This

- Any state keyed by chat/thread must include `userID` unless a comment explains why it is intentionally shared.
- Any persisted/logged/prompt-injected text must use redaction before truncation.
- Any command execution path outside the Bridge must have its own allowlist or delegate to Bridge policy.
- Any async session/pipeline/Telegram change must pass targeted race tests.
- Any user-data deletion or observability/debug path must not silently ignore errors.

## Anti-pattern

Fixing the latest occurrence only in the touched function while leaving the invariant as tribal knowledge. That guarantees the next feature will reintroduce the same class in a new subsystem.
