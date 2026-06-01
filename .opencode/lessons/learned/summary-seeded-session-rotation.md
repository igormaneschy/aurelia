# Summary-seeded rotation can corrupt topic continuity

**Date**: 2026-06-01
**Change**: pi-session-continuity-review
**Category**: anti-pattern

## What happened

Automatic rotation replaced a PI session with a new session seeded by a generated summary. The summary carried stale Aurelia/cron context, so the next turn in an AutoTraders topic answered from the wrong context despite the correct CWD.

## How to avoid

Do not auto-rotate sessions for token size. Preserve the original PI `session_file` and let PI SDK compaction manage long context; use summary-seeded sessions only for explicit/emergency recovery.

## Tags

#lesson #change-pi-session-continuity-review #anti-pattern #session #continuity #pi-sdk
