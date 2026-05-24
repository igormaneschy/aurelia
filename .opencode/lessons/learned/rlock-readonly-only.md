# RLock is only safe for pure read operations

**Date**: 2026-05-24
**Change**: gap-remediation-2026-05-24
**Category**: anti-pattern

## What happened

Session `store.go` had 5 read methods (`Get`, `GetWithState`, `GetCwd`, `GetSession`, `GetSessionWithState`) changed from `Lock()` to `RLock()` to reduce contention. However, these methods mutate shared state (`e.lastSeen = time.Now()`, `s.cwdSeen[key] = time.Now()`) — a data race. The Go race detector would flag concurrent `lastSeen` writes, and map writes to `cwdSeen` under RLock race with GC/SetCwd under Lock.

## How to avoid

Never use `RLock` on methods that mutate any shared state, no matter how "trivial" the write seems. RLock is only for pure read operations. If the method needs to update a timestamp or marker, use full Lock or split into a separate write method.

## Tags

#lesson #change-gap-remediation-2026-05-24 #anti-pattern #concurrency #race-condition
