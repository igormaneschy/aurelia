# Tests must isolate writable app config

**Date**: 2026-06-03
**Change**: ux-model-persistence-fixes
**Category**: anti-pattern

## What happened

A model-selection test assumed `saveDefaultModel()` would fail without a real config file, but it ran against `~/.aurelia/config/app.json` and persisted the fake `newpi/new-model` into the live daemon config.

## How to avoid

Any test that exercises config persistence must set `AURELIA_HOME` to `t.TempDir()` and create a temp `app.json`; never rely on the developer machine not having real config.

## Tags

#lesson #change-ux-model-persistence-fixes #anti-pattern #testing #config #models
