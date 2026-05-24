# SendContextText does not auto-extract ThreadID from context

**Date**: 2026-05-24
**Change**: plan-mode-commands-fix
**Category**: anti-pattern

## What happened

All 6 Plan Mode command handlers used `SendContextText(c, msg)` without passing `&telebot.SendOptions{ThreadID: c.Message().ThreadID}`. The `SendContextText` helper only sets `ParseMode: ModeHTML` by default — it does not inspect the `telebot.Context` for thread/topic ID. This caused all plan command replies to be sent to the general chat (ThreadID=0) instead of the topic where the command was issued.

## How to avoid

When sending replies in topic-enabled groups, always explicitly pass ThreadID:
- Use `SendTextWithThread(bot, chat, msg, threadID)` for direct bot sends
- Or pass `&telebot.SendOptions{ThreadID: c.Message().ThreadID}` as third arg to `SendContextText(c, msg, opts...)`

Add a linter or code review check for `SendContextText` calls in handlers that might run in topics.

## Tags

#lesson #change-plan-mode-commands-fix #anti-pattern #telegram #thread-routing
