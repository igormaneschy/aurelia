# External Integrations

## Telegram Bot API

**Service:** Telegram Bot API via telebot.v3
**Purpose:** Primary user interface — receive messages, send responses
**Implementation:** `internal/telegram/` (25 files)
**Configuration:** `TelegramBotToken` + `TelegramAllowedUserIDs` in app config
**Authentication:** Bot token (long polling, 10s timeout)

**Capabilities:**
- Text, photo, document, voice/audio message handling
- Album buffering (900ms wait for grouped photos)
- Chunked output (3900 char limit) with Markdown→HTML conversion
- Typing indicators during LLM processing
- Emoji reactions
- Rate limit handling (FloodError detection + retry)
- User whitelist middleware

**Commands:** `/start`, `/help`, `/cwd`, `/reset`, `/cron`, `/agents`

## LLM Bridge (PI SDK)

**Service:** PI coding agent (`@earendil-works/pi-coding-agent`)
**Purpose:** LLM inference, tool use, agentic loops
**Implementation:** `internal/bridge/` (Go client) + `bridge/index.ts` (TS adapter)
**Configuration:** PI auth/models/settings in `~/.pi/agent`, with selected credentials exported from Aurelia config as env vars
**Authentication:** PI auth store (`auth.json`), provider env vars, or custom `models.json`

**Protocol:** NDJSON over stdin/stdout
- Request: `{command, prompt, request_id, options}`
- Events: `system` → `assistant`/`tool_use` → `result`/`error`
- Timeout: 30 minutes per query, with Go-side idle/watchdog handling for long active runs
- Multiplexed: Multiple concurrent requests via request_id

**Supported Providers:**
| Provider | Base URL | Default Model |
|----------|----------|---------------|
| Anthropic | (default) | `claude-sonnet-4-6` |
| Kimi | `api.kimi.com/coding/` | `kimi-k2-thinking` |
| OpenRouter | `openrouter.ai/api/v1` | (user-selected) |
| Zai | `api.z.ai/api/anthropic` | (user-selected) |
| Alibaba | `dashscope-intl.aliyuncs.com/apps/anthropic` | (user-selected) |

## PI Resources

**Service:** Local PI installation
**Purpose:** Reuse auth, model registry, skills, extensions, sessions, and settings
**Implementation:** `bridge/index.ts` via PI SDK
**Configuration:** `~/.pi/agent/auth.json`, `~/.pi/agent/models.json`, `~/.pi/agent/settings.json`
**Authentication:** PI `AuthStorage` plus provider env vars (`ANTHROPIC_API_KEY`, `KIMI_API_KEY`, `OPENROUTER_API_KEY`, etc.)

**Notes:**
1. Claude-specific Cloud MCP discovery was removed from the bridge during PI migration.
2. Agent-defined `mcp_servers` remain in the config schema but need a PI extension/adapter for parity.
3. PI built-in tools are mapped from Claude-style tool names (`Read` → `read`, `Glob` → `find`, etc.).
4. PI owns model resolution, sessions, compaction, context-file loading, skills/extensions and tool execution. Aurelia owns Telegram UX, identity/persona, scoped memory, scheduling, audit/policy context and workflows.

## Speech-to-Text (Groq)

**Service:** Groq Whisper API
**Purpose:** Transcribe voice messages and audio files
**Implementation:** `pkg/stt/groq.go`
**Configuration:** `groq` API key in provider config
**Authentication:** Bearer token

**Endpoint:** `POST https://api.groq.com/openai/v1/audio/transcriptions`
**Model:** `whisper-large-v3`
**Format:** Multipart form-data (file + model + response_format=json)

## SQLite Database

**Service:** Embedded SQLite (modernc.org/sqlite, pure Go)
**Purpose:** Persistent storage for cron jobs and execution history
**Implementation:** `internal/cron/store*.go`
**Configuration:** `~/.aurelia/data/cron.db` (configurable via `DBPath`)
**Authentication:** N/A (local file)

**Tables:**
- `cron_jobs` — Job definitions, schedule, status
- `cron_executions` — Execution history, cost tracking

**Features:** WAL mode, transactions via `WithTx()`, indexed queries

## File System (Persona & Config)

**Purpose:** Persistent identity, configuration, and agent definitions

**Runtime directory:** `~/.aurelia/` (overridable via `$AURELIA_HOME`)
```
~/.aurelia/
├── config/app.json              # Main configuration
├── config/mcp_servers.json      # MCP server definitions
├── data/cron.db                 # SQLite database
├── memory/personas/             # Global IDENTITY.md, SOUL.md
├── users/<id>/personas/USER.md  # Per-user persona
├── users/<id>/memory/           # Per-user global operational memory
├── topics/chat_<id>/thread_<id>/ # Topic memory and optional cwd_overlay/
├── projects/<slug>/team/        # Shared project/team operational memory
├── memory/OWNER_PLAYBOOK.md     # Optional owner instructions
├── agents/*.md                  # Agent definitions (YAML frontmatter)
└── bridge/                      # TypeScript runtime files
```

**Temporary files:** Media downloads to `os.TempDir()` (photos, documents, audio)

## Background Jobs

**System:** Custom polling scheduler (no external queue)
**Location:** `internal/cron/scheduler.go`
**Interval:** 15 seconds
**Capacity:** Up to 50 jobs per tick
**Deduplication:** `sync.Map` prevents concurrent runs of same job

**Job types:**
- Recurring: Cron expressions (e.g., `"0 9 * * MON"`)
- One-time: Absolute timestamp (`run_at`)
- Agent-scheduled: Auto-registered from agents with `schedule` field
