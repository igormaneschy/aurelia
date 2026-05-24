# Telegram handler registration must include bot wiring

**Date**: 2026-05-24
**Change**: plan-mode-commands-fix
**Category**: anti-pattern

## What happened

Implemented 6 Plan Mode command handlers (`handlePlan`, `handlePlanStatus`, etc.) and added them to `commandRules` for natural-language matching, but forgot to register them with `bc.bot.Handle()` in `registerContentRoutes()`. The handlers existed but were never reachable via Telegram slash commands.

## How to avoid

When adding new Telegram commands, always verify three wiring points:
1. Handler function exists and is tested
2. `bc.bot.Handle("/command", bc.handler)` registered in `registerContentRoutes()`
3. Command added to `registerSlashMenu()` for Telegram menu discovery
4. Command added to `helpMessage()` for user documentation

Create a checklist in the task template for new commands.

## Tags

#lesson #change-plan-mode-commands-fix #anti-pattern #telegram #wiring
