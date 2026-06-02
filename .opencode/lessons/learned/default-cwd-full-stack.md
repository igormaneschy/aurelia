# DefaultCWD Is a Full-Stack Resolution Concern

**Date**: 2026-06-02
**Change**: session-profile-operability-remediation
**Category**: anti-pattern

## What happened

DefaultCWD was added to prompt/CWD helpers but some execution paths still used context-free `effectiveCwd` / `currentCwd`, so private-chat fallback worked in prompts while preflight, plan execution, memory loading, or nudge paths could miss it.

## How to avoid

When adding a CWD source, audit every resolver call in prompt building, bridge request creation, preflight, orchestration, memory loading, and lifecycle hooks. Prefer context-aware helpers carrying `userID` and `isPrivateChat`.

## Tags

#lesson #change-session-profile-operability-remediation #anti-pattern #cwd #telegram
