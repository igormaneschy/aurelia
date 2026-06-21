# PI Session Message Timestamps

**Date**: 2026-06-21
**Change**: fix-tui-message-timestamps
**Category**: pattern

## What happened

PI session history messages expose `timestamp` as Unix milliseconds. Treating
only string timestamps as valid made restored TUI messages lose their original
time.

## How to avoid

Before mapping PI SDK session fields into Aurelia protocol fields, verify the
SDK representation and add a boundary/source-contract test for it.

## Tags

#lesson #change-fix-tui-message-timestamps #pattern #pi-sdk #tui #timestamps
