# Background Receipts Must Validate Chat and Thread

**Date**: 2026-06-02
**Change**: session-profile-operability-remediation
**Category**: pattern

## What happened

Nudge receipt threading initially validated only `chat_id` from runlog before replying to a stored Telegram message. In topic-enabled groups, same-chat/different-thread mismatches can leak receipt messages across topics.

## How to avoid

Any background sender using stored Telegram message IDs must validate both intended `chat_id` and `thread_id`, then fall back to a non-reply send in the intended thread when the stored target does not match.

## Tags

#lesson #change-session-profile-operability-remediation #pattern #telegram #threading #nudge
