# Prompt Profiles

Aurelia does not run independent worker agents. `/mode`, `/agents`, and `@profile`
select a **Prompt Profile** — complementary instructions (and optional execution
hints) injected into the request sent to the PI SDK harness.

## Resolution per message

```text
@profile (one-shot)  >  /mode (persistent default)  >  general
```

- `/mode developer` — sets your default profile until changed or cleared
- `@researcher compare SDKs` — uses `researcher` for this message only; default unchanged
- `/agents` — lists visible profiles; `/agents verbose` (owner DM) shows safe execution hints

Changing profile does **not** reset the PI session.

## Storage layout

| Location | Scope | Precedence |
|----------|-------|------------|
| `~/.aurelia/users/<id>/profiles/*.md` | Owner-private | Highest |
| `~/.aurelia/profiles/*.md` | Global (all users) | High |
| `~/.aurelia/agents/*.md` | Legacy global | Medium |
| Builtins (`general`, `developer`, `researcher`) | Embedded | Fallback |

Legacy mode overlays still work:

| Location | Behavior |
|----------|----------|
| `~/.aurelia/users/<id>/personas/mode_developer.md` | Merged into `developer` profile when no user-private `profiles/developer.md` |
| `~/.aurelia/users/<id>/personas/mode_researcher.md` | Merged into `researcher` profile |

User-private `profiles/<name>.md` replaces the profile entirely (no mode overlay merge).

## Canonical profile format

Path: `~/.aurelia/profiles/<name>.md`

```markdown
---
name: coder
description: Implementation and code changes
kind: prompt_profile
harness: pi
model: auto
capability_profile: execute_safe
allowed_tools: [Read, Write, Edit, Bash, Grep, Glob]
disallowed_tools: []
public: true
tags: [developer, code]
---

You package implementation requests for the harness. Be precise about scope,
tests, and validation steps.
```

### Frontmatter fields

| Field | Required | Notes |
|-------|----------|-------|
| `name` | Yes | Profile id; used by `/mode` and `@name` |
| `description` | Recommended | Shown in `/agents` |
| `public` | No | Default `true` (global), `false` (user-private dir) |
| `harness` | No | Default `pi`; unsupported values fail closed (Phase 3) |
| `model` | No | Execution hint for bridge when set |
| `cwd` | No | Execution hint; hidden from group `/agents` listings |
| `capability_profile` | No | Security guard-rails input |
| `allowed_tools` / `disallowed_tools` | No | Tool policy hints |
| `max_turns` / `tool_budget` | No | Monitoring hints |
| `tags` | No | Shown in `/agents verbose` |

Body markdown becomes the profile prompt (injected once as
`# Active Prompt Profile: <name>`).

## User-private profiles

Path: `~/.aurelia/users/<telegram_user_id>/profiles/<name>.md`

Same frontmatter as global profiles. Default `public: false` — only the owner
can list, explain, or invoke unless `public: true` is set explicitly.

Use for personal variants without affecting other authorized users in groups.

## Legacy `agents/` directory

`~/.aurelia/agents/*.md` remains supported. Fields map to `PromptProfile`
(`name`, `description`, `model`, `cwd`, `allowed_tools`, etc.). Global
`profiles/` overrides `agents/` when names collide.

Scheduled agents (`schedule` in frontmatter) are still owned by cron; profile
`cwd` on the job is persisted separately.

## Commands

| Command | Action |
|---------|--------|
| `/mode` | Show active default profile |
| `/mode <name>` | Set default profile |
| `/mode general` | Clear to general |
| `/mode explain <name>` | Safe summary (no prompt body) |
| `/agents` | List profiles + active marker |
| `/agents verbose` | Owner DM: model/capability/tags hints |
| `/agents explain <name>` | Same as `/mode explain` |
| `@<name> <text>` | One-shot profile for this message |

## Prompt assembly

Each turn injects **exactly one** effective profile section (plus persona,
memory, security). `@researcher` with `/mode developer` active injects only
`researcher`, not both.

Spec: `.specs/features/prompt-profiles/spec.md`