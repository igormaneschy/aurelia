# No Current Time for Restored History

**Date**: 2026-06-21
**Change**: fix-tui-message-timestamps
**Category**: anti-pattern

## What happened

History reload used `time.Now()`/current time when a message timestamp was
missing, so old messages could reappear with identical wrong times after TUI
restart.

## How to avoid

For restored transcripts, preserve missing timestamps as unknown and render no
time instead of fabricating one. Use current time only for new live messages.

## Tags

#lesson #change-fix-tui-message-timestamps #anti-pattern #tui #history #timestamps
