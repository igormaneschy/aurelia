# Verify build before investigating review false positives

**Date**: 2026-05-24
**Change**: gap-remediation-2026-05-24
**Category**: process

## What happened

The code review flagged a "syntax error" in `pipeline.go:1011` — a `case` statement allegedly indented inside another `case` body. The claim was that the code wouldn't compile. However, `go build ./...` already passed. The indentation was correct; the reviewer misread leading whitespace and a blank line between case blocks. Time was lost investigating a nonexistent bug.

## How to avoid

Before investigating a reviewer's build/syntax claim, run the actual build command (`go build ./...`, `tsc`, etc.) to establish ground truth. If the build passes, the claim is likely a false positive. Always verify against the compiler before assuming the reviewer is right.

## Tags

#lesson #change-gap-remediation-2026-05-24 #process #review #quality-gates
