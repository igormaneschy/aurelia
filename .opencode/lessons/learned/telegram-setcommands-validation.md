# Telegram SetCommands fails completely on invalid command names

**Date**: 2026-05-24
**Change**: plan-mode-commands-fix
**Category**: anti-pattern

## What happened

Added plan subcommands (`plan status`, `plan list`, `plan cancel`, `plan reset`) to `SetCommands()` menu registration. Telegram Bot API rejects commands containing spaces with `BOT_COMMAND_INVALID (400)`. This caused the entire `SetCommands` call to fail, meaning **no commands at all** appeared in the Telegram menu — not even the valid ones.

## How to avoid

Telegram commands must be: 1-32 chars, lowercase letters/digits/underscores only, starting with a letter. Never include spaces. When adding menu entries, validate command names against Telegram's format before calling `SetCommands`. Consider a unit test that validates all registered commands.

## Tags

#lesson #change-plan-mode-commands-fix #anti-pattern #telegram #bot-api
