# Library Default Keymap Collides with Application Shortcut

**Date**: 2026-06-28
**Change**: sidebar-delete-bug-fix
**Category**: anti-pattern

## What happened

The sidebar `d` key (delete session) was silently broken because the bubbles v2 table's `DefaultKeyMap()` binds `"d"` to `HalfPageDown`. When `d` was pressed, the table handled it first (moving the cursor half a page down, typically to the last row) before the application's `case "d"` handler read `m.sidebarCursor` — always targeting the wrong session.

The fix: customize the table keymap to remove `"d"` from `HalfPageDown`, keeping only `"ctrl+d"`. The sidebar table should not use `d` for navigation because `d` is a sidebar action key.

## How to avoid

When using any widget library with configurable keymaps (bubbles table, etc.), always check `DefaultKeyMap()` for collisions with application-defined shortcuts. If a library binding shares a key with an application action, override the binding preemptively — either remove the key entirely or move it to a modifier combination.

## Tags

#lesson #change-sidebar-delete-bug-fix #anti-pattern #tui #keymap #bubbles
