# Defense-in-Depth: Explicit Fields on Builtin Objects

**Date**: 2026-06-06
**Change**: prompt-profiles-regression-fix
**Category**: pattern

## What happened

After fixing the `isInternal` heuristic regression, we added explicit `CapabilityProfile: "execute_safe"` to all three builtin profiles (general, developer, researcher) as defense-in-depth. Without explicit fields, builtins rely on default resolution logic that can silently change. With explicit fields, the intent is declarative and survives future heuristic changes.

## How to avoid

Builtin/config objects with security-sensitive fields should declare them explicitly rather than relying on defaults. A future developer changing `DefaultProfileForContext` or adding a new heuristic won't accidentally break builtins that declared their intent upfront.

## Tags

#lesson #change-prompt-profiles-regression-fix #pattern #defense-in-depth #security
