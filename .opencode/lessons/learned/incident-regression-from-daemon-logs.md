# Turn production logs into regression tests

**Date**: 2026-06-01
**Change**: pi-session-continuity-review
**Category**: process

## What happened

The daemon log exposed the exact failure shape: active topic session, `input_tokens=371682`, `rotate_after=250000`, then a new summary-seeded session. Capturing those numbers as a unit test made the fix independent of implementation details.

## How to avoid

When a live daemon bug has structured logs, preserve the observed decision inputs in a regression test before generalizing the policy change.

## Tags

#lesson #change-pi-session-continuity-review #process #logs #regression
