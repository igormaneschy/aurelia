# Model selection must fail closed before reset

**Date**: 2026-06-03
**Change**: ux-model-persistence-fixes
**Category**: pattern

## What happened

The callback path mutated in-memory model state and reset the current session before proving the new model was persisted. A save failure could leave the user with lost context and misleading success feedback.

## How to avoid

Persist the model first, then mutate runtime state, refresh provider env, invalidate caches, and reset the scoped session. On persistence failure, preserve session/config and tell the user what happened.

## Tags

#lesson #change-ux-model-persistence-fixes #pattern #models #ux #fail-closed
