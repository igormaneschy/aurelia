# Session/Profile Operability Improvements — Specification

**Status:** 🔴 Draft — implementation guide  
**Priority:** P0/P1 candidate  
**Scope:** Runlog ↔ Telegram message correlation, per-user mode profiles, profile timezone/default CWD enrichment  
**Decision boundary:** Aurelia remains a coordination/product layer; Pi SDK/Ares remains the reasoning/session-history engine.

---

## 1. Overview

This spec defines three independently deployable improvements that make Aurelia more reliable as a Telegram-native personal assistant: (1) a runlog message bridge that records Telegram inbound/outbound `message_id`s alongside Pi session metadata, enabling crash-safe threading and nudge replies; (2) per-user mode profiles that inject small contextual prompt overlays such as developer/researcher/general without resetting Pi sessions; and (3) profile enrichment with timezone and default CWD so cron parsing and private-chat project context behave like a personal assistant rather than a server-local daemon. Together these features strengthen Aurelia’s coordination layer while preserving the core architectural rule: Pi owns reasoning/session history, Aurelia owns Telegram UX, durable coordination state, persona, continuity, memory UX, runlog and policy.

---

## 2. Architecture Diagram

```text
Telegram message
  inbound message_id, chat_id, thread_id, user_id, chat_type
        │
        ▼
internal/telegram
  ├─ command router
  │   ├─ /mode ───────────────┐
  │   ├─ /memory checkpoint ──┼─ reads Profile.ActiveMode
  │   └─ cron create ─────────┼─ reads Profile.Timezone
  │                           │
  ├─ currentCwd resolver ◄────┼─ Profile.DefaultCWD fallback for private chats
  │                           │
  └─ pipeline entry ──────────┼─ passes inbound message_id + chat_type
        │                     │
        ▼                     │
internal/pipeline             │
  ├─ BuildSystemPrompt        │
  │   └─ internal/persona ────┘
  │       ├─ IDENTITY.md + SOUL.md + USER.md
  │       └─ mode_<name>.md overlay appended last
  │
  ├─ runlog.Start(RunRecord{InboundMessageID, SessionFile, ...})
  ├─ Pi SDK/Ares bridge Execute
  └─ final Telegram send returns outbound message_id
        │
        ▼
internal/runlog SQLite
  run_journal.inbound_message_id
  run_journal.outbound_message_id
        │
        ▼
internal/dream/nudge
  GetLastOutboundMessage(session_file)
        │
        ▼
Telegram threaded nudge reply
  ReplyToMessageID = last outbound message_id
  fallback: normal send if Telegram rejects missing/deleted reply target
```

---

## 3. Feature 1 Spec — Runlog Message Bridge

### 3.1 Problem

Aurelia records runs and Pi session metadata, but it needs a durable, structured bridge to Telegram message IDs. Without this, nudge/dream output cannot reliably thread under the last conversation message after restart, cold resume cannot identify the last Telegram message touched by a Pi session, and group-topic routing loses precision after crash/restart.

### 3.2 Architecture Decision

Store only Telegram message identifiers in `internal/runlog`, not message content. The runlog is the correct durable correlation store because it already represents pipeline runs, session files, chat/thread/user metadata and completion status. Do **not** expand `internal/session/store.go`; it must remain a lightweight operational cache.

> Current-state note: some runlog fields/methods may already exist in the repository. Implementation must verify whether each item is present and complete the missing wiring/tests rather than duplicating definitions.

### 3.3 Data Model

#### `internal/runlog/types.go`

Add to `RunRecord` if not already present:

```go
type RunRecord struct {
    // existing fields...

    // Pi session ↔ Telegram message bridge.
    InboundMessageID  int64 // Telegram message_id that triggered this run; 0 if unavailable
    OutboundMessageID int64 // Telegram message_id of final Aurelia response; 0 until sent
}
```

Add to `RunUpdate` using pointer semantics:

```go
type RunUpdate struct {
    // existing optional fields...

    InboundMessageID  *int64
    OutboundMessageID *int64
}
```

**Public interface impact:** yes — `runlog.RunRecord`, `runlog.RunUpdate` and `runlog.Store` are package APIs consumed by pipeline, Telegram tests and fake stores.

**Content privacy:** message text, captions, document names, prompt fragments and Telegram sender text SHALL NOT be stored in these fields.

### 3.4 SQLite Migration

Add idempotent nullable integer columns with zero default:

```sql
ALTER TABLE run_journal ADD COLUMN inbound_message_id INTEGER DEFAULT 0;
ALTER TABLE run_journal ADD COLUMN outbound_message_id INTEGER DEFAULT 0;
```

Migration rules:

- run during `SQLiteStore.initialize()` on every startup;
- ignore duplicate-column / already-exists errors only;
- surface all other migration errors with `fmt.Errorf("migrate run_journal add %s: %w", col, err)`;
- old rows read as zero via `COALESCE(inbound_message_id, 0)` and `COALESCE(outbound_message_id, 0)`;
- no content backfill is attempted.

Recommended index:

```sql
CREATE INDEX IF NOT EXISTS idx_run_journal_session_outbound
ON run_journal(session_file, outbound_message_id, started_at DESC);
```

If index growth is a concern, `idx_run_journal_session_started` is sufficient for MVP and `GetLastOutboundMessage` may filter `outbound_message_id != 0`.

### 3.5 Store Method

Add to `internal/runlog/store.go`:

```go
type Store interface {
    // existing methods...

    // GetLastOutboundMessage returns the chat_id, thread_id and Telegram
    // outbound message_id for the most recent run associated with sessionFile.
    // Returns zero values and nil error when sessionFile is empty or no row exists.
    GetLastOutboundMessage(ctx context.Context, sessionFile string) (chatID int64, threadID int, messageID int64, err error)
}
```

SQLite implementation:

```go
func (s *SQLiteStore) GetLastOutboundMessage(ctx context.Context, sessionFile string) (int64, int, int64, error) {
    if strings.TrimSpace(sessionFile) == "" {
        return 0, 0, 0, nil
    }

    var chatID, messageID int64
    var threadID int
    err := s.db.QueryRowContext(ctx, `
        SELECT chat_id, thread_id, outbound_message_id
        FROM run_journal
        WHERE session_file = ? AND outbound_message_id != 0
        ORDER BY started_at DESC, rowid DESC
        LIMIT 1`, sessionFile).Scan(&chatID, &threadID, &messageID)
    if err == sql.ErrNoRows {
        return 0, 0, 0, nil
    }
    if err != nil {
        return 0, 0, 0, fmt.Errorf("runlog get_last_outbound session=%s: %w", sessionFile, err)
    }
    return chatID, threadID, messageID, nil
}
```

### 3.6 Pipeline Integration

#### Inbound message ID

At pipeline entry, include the Telegram inbound message ID in the `RunRecord` passed to `runlog.Start`:

```go
record := runlog.RunRecord{
    RunID:            runID,
    ChatID:           chatID,
    ThreadID:         threadID,
    UserID:           userID,
    EntryPoint:       "telegram",
    SessionFile:      sessionFile,
    InboundMessageID: int64(input.MessageID),
    // existing fields...
}
```

If the entrypoint is cron, CLI or internal nudge, use `0`.

#### Outbound message ID

At final response send, capture the `telebot.Message.ID` returned by Telegram and update runlog:

```go
sent, err := output.Send(...)
if err != nil {
    // existing error path
}
if sent != nil && sent.ID != 0 {
    outboundID := int64(sent.ID)
    if err := runLog.Update(ctx, runlog.RunUpdate{
        RunID:             runID,
        OutboundMessageID: &outboundID,
    }); err != nil {
        log.Printf("runlog: update outbound_message_id run=%s: %v", runID, err)
    }
}
```

The update is best-effort: a failed ID update must not fail user delivery.

### 3.7 Nudge Integration

In `internal/dream/nudge.go`, when the nudge has a `session_file` and `runlog.Store`, resolve the last outbound message:

1. `GetLastOutboundMessage(ctx, sessionFile)`;
2. use returned `chatID/threadID/messageID` only if all are non-zero and match the intended session/chat where applicable;
3. send nudge using `ReplyToMessageID: messageID` and explicit `ThreadID: threadID`;
4. if Telegram returns a reply-target error because the original message was deleted/unavailable, retry once as a normal send with `ReplyToMessageID=0` while preserving `ThreadID`.

Telegram fallback detection should be conservative. Match known messages such as `message to be replied not found` or `reply message not found`; all other send errors should be surfaced/logged as existing behavior.

### 3.8 Constraints

- Do not modify `internal/session/store.go` for message ID persistence.
- Do not store message content.
- Treat `OutboundMessageID=0` as unknown, not failure.
- Always pass explicit `ThreadID` when sending to topics.
- Runlog write failures are observability failures, not user-delivery failures.

---

## 4. Feature 2 Spec — Mode Profiles

### 4.1 Problem

Aurelia has rich persona assembly, but no safe, durable way for an authorized user to switch behavioral focus without editing files. A mode should be a lightweight per-user prompt overlay that changes the next request’s behavior while leaving Pi session history intact.

### 4.2 Architecture Decision

Store the selected mode in `users.Profile.ActiveMode`, and store prompt overlays as optional markdown files in the per-user persona directory. Mode injection is appended **last** in `BuildPromptForUser`, after IDENTITY, SOUL, USER.md, owner playbook and project playbook. Missing overlay files are silently ignored, so a user can set a mode before creating its overlay file.

Modes are not agents and do not change model/tool configuration. They are prompt overlays only.

### 4.3 Data Model — `Profile.ActiveMode`

Modify `internal/users/profile.go`:

```go
type Profile struct {
    UserID      int64     `json:"user_id"`
    Name        string    `json:"name"`
    Language    string    `json:"language"`
    IsOwner     bool      `json:"is_owner"`
    ActiveMode  string    `json:"active_mode"` // developer | researcher | general | ""
    OnboardedAt time.Time `json:"onboarded_at"`
    LastSeenAt  time.Time `json:"last_seen_at"`
}
```

Valid values:

- `"developer"`
- `"researcher"`
- `"general"`
- `""` — default/general

Validation helper:

```go
func NormalizeMode(raw string) (string, error) {
    switch strings.TrimSpace(strings.ToLower(raw)) {
    case "", "auto", "general", "geral":
        return "", nil
    case "dev", "developer", "desenvolvedor":
        return "developer", nil
    case "researcher", "research", "pesquisa", "pesquisador":
        return "researcher", nil
    default:
        return "", fmt.Errorf("modo inválido %q", raw)
    }
}
```

`NormalizeMode` may live in `internal/users` if reused by persona and Telegram, or in `internal/telegram` if only command parsing needs aliases. The canonical stored value must be one of the valid values above.

### 4.4 Mode Overlay Files

Path format:

```text
~/.aurelia/users/<userID>/personas/mode_<name>.md
```

> The request named `~/.config/aurelia/persona/users/<userID>/mode_<name>.md`; current Aurelia user-profile paths are under `~/.aurelia/users/<id>/personas/`. Implement against the existing resolver root to preserve current architecture. If a config-root migration is desired, it must be a separate spec.

Resolver method:

```go
func (r *Resolver) UserModePath(userID int64, mode string) string {
    return filepath.Join(r.PersonasDir(userID), "mode_"+mode+".md")
}
```

File behavior:

- optional; missing file returns no overlay and no user-visible error;
- expected size: 100–300 words, but implementation does not enforce word count;
- read with the existing optional-file pattern (`readOptionalFile`);
- mode name must be normalized before path construction; do not pass raw user text into a path.

Prompt injection header:

```markdown
## ACTIVE MODE: DEVELOPER

<overlay content>
```

Injection order:

1. IDENTITY + SOUL + USER.md;
2. owner playbook, only when `isOwner`;
3. project playbook;
4. active mode overlay last.

### 4.5 Persona Integration

Current signature:

```go
func (s *CanonicalIdentityService) BuildPromptForUser(
    userID int64,
    resolver interface{ UserMdPath(userID int64) string },
    isOwner bool,
) (string, error)
```

Required after-signature:

```go
type UserPromptResolver interface {
    UserMdPath(userID int64) string
    UserModePath(userID int64, mode string) string
}

func (s *CanonicalIdentityService) BuildPromptForUser(
    userID int64,
    resolver UserPromptResolver,
    isOwner bool,
    activeMode string,
) (string, error)
```

Implementation detail:

```go
func (s *CanonicalIdentityService) buildModeBlock(userID int64, resolver UserPromptResolver, mode string) string {
    mode, err := users.NormalizeMode(mode)
    if err != nil || mode == "" {
        return ""
    }
    content, err := readOptionalFile(resolver.UserModePath(userID, mode))
    if err != nil {
        log.Printf("persona: failed to read mode overlay user=%d mode=%s: %v", userID, mode, err)
        return ""
    }
    if strings.TrimSpace(content) == "" {
        return ""
    }
    return "## ACTIVE MODE: " + strings.ToUpper(mode) + "\n\n" + strings.TrimSpace(content)
}
```

Pipeline prompt builder must load the profile and pass `profile.ActiveMode` when available. If profile load fails, log and proceed with no mode overlay.

### 4.6 Telegram Command — `/mode`

Add enum value:

```go
const (
    // existing...
    CmdSetMode
)
```

Command matching:

- substring match, accent-insensitive:
  - `/mode developer`, `/mode researcher`, `/mode general`, `/mode auto`
  - `modo dev`, `modo desenvolvedor`, `modo pesquisa`, `modo geral`
- exact match for current-mode query:
  - `qual meu modo`
  - `meu modo atual`
- `/mode` with no argument shows current mode.

Handler signature:

```go
func (bc *BotController) cmdSetMode(c telebot.Context, text string) (string, error)
```

Handler behavior:

1. get `userID := safeSenderID(c.Sender())`;
2. require `bc.userStore != nil`; if unavailable return `Sistema de usuários não disponível.`;
3. load profile via `bc.userStore.Get(userID)`;
4. if profile is nil, create a minimal profile only if user is authorized and existing onboarding flow allows it; otherwise return a clear setup message;
5. parse target mode:
   - no target/query phrase → show current mode;
   - `auto`, `general`, `geral` → store `""` or `"general"` per normalization decision; recommended storage for default is `""`;
   - `developer`, `researcher` → store canonical value;
6. save profile using existing atomic JSON write path;
7. do **not** reset Pi session;
8. return:

```text
✅ Modo alterado para **developer**. Próxima mensagem usará o novo perfil.
```

Current mode response:

```text
Modo atual: **general**.
```

Access: every authorized user; not owner-only.

### 4.7 Mode Listing in `CmdListAgents`

Change handler from:

```go
func (bc *BotController) cmdListAgents() (string, error)
```

to:

```go
func (bc *BotController) cmdListAgents(userID int64) (string, error)
```

Append a mode section after agents:

```markdown

**Modos disponíveis**
- **general** (● ativo)
- **developer**
- **researcher**
```

Rules:

- `""` displays as `general`;
- add `(● ativo)` suffix next to active mode;
- do not require mode overlay files to list modes;
- if profile load fails, show `general` active and log the error.

### 4.8 Memory Checkpoint Tag

Change handler from:

```go
func (bc *BotController) cmdMemoryCheckpoint(chatID int64, threadID int, text string) (string, error)
```

to:

```go
func (bc *BotController) cmdMemoryCheckpoint(chatID int64, threadID int, userID int64, text string) (string, error)
```

Before `WriteCheckpoint`, append active mode metadata:

```go
func appendModeTag(note, mode string) string {
    mode, _ = users.NormalizeMode(mode)
    if mode == "" {
        return note
    }
    tag := "[mode:" + mode + "]"
    if strings.TrimSpace(note) == "" {
        return tag
    }
    return strings.TrimSpace(note) + " " + tag
}
```

Examples:

- `note=""`, `mode="developer"` → `[mode:developer]`
- `note="before refactor"`, `mode="developer"` → `before refactor [mode:developer]`

This is metadata in the note only; do not change checkpoint file format.

---

## 5. Feature 3 Spec — Profile Enrichment: Timezone + DefaultCWD

### 5.1 Problem

Personal assistants must interpret time and workspace relative to the user. Aurelia currently risks interpreting cron schedules in server time and leaves private chats without a working directory if no binding/session CWD exists.

### 5.2 Data Model

Modify `internal/users/profile.go`:

```go
type Profile struct {
    UserID      int64     `json:"user_id"`
    Name        string    `json:"name"`
    Language    string    `json:"language"`
    IsOwner     bool      `json:"is_owner"`
    ActiveMode  string    `json:"active_mode"`
    Timezone    string    `json:"timezone"`    // IANA tz; empty = UTC
    DefaultCWD  string    `json:"default_cwd"` // fallback CWD for private chats only
    OnboardedAt time.Time `json:"onboarded_at"`
    LastSeenAt  time.Time `json:"last_seen_at"`
}
```

Backward compatibility:

- old JSON profiles decode with empty `ActiveMode`, `Timezone`, `DefaultCWD`;
- empty timezone means UTC;
- empty default CWD means no fallback.

Validation helpers:

```go
func NormalizeTimezone(raw string) (string, *time.Location, error) {
    tz := strings.TrimSpace(raw)
    if tz == "" {
        return "", time.UTC, nil
    }
    loc, err := time.LoadLocation(tz)
    if err != nil {
        return "", nil, fmt.Errorf("invalid timezone %q: %w", raw, err)
    }
    return tz, loc, nil
}
```

`DefaultCWD` must be validated using the same path safety rules as `/cwd` (`runtime.ResolveProjectCwd`) before saving through any future command/config path. Onboarding does not collect it.

### 5.3 Timezone in Cron

#### Fast parser

Change:

```go
func cronFastParse(text string, now time.Time) *cronCreateParsed
```

to keep the same signature but call with a location-aware `now`:

```go
loc := bc.userLocation(userID)
now := time.Now().In(loc)
parsed := cronFastParse(text, now)
```

Because `cronFastParse` already accepts `now`, its internals should preserve `now.Location()` for `today`, `tomorrow` and relative schedules.

#### LLM parser prompt

Replace hardcoded prompt constant behavior with a prompt builder:

```go
func cronParseSystemPromptForLocation(now time.Time, tzName string) string {
    _, offset := now.Zone()
    offsetText := formatUTCOffset(offset) // e.g. +00:00, -03:00
    return fmt.Sprintf(`You are a scheduling assistant. Extract scheduling parameters from the user's message.

Respond with ONLY a JSON object (no markdown, no explanation):

For recurring schedules:
{"type":"cron","cron_expr":"<cron expression>","prompt":"<what to do>"}

For one-time schedules:
{"type":"once","run_at":"<ISO 8601 timestamp>","prompt":"<what to do>"}

Rules:
- User timezone: %s (%s)
- Current user-local time: %s
- cron_expr uses standard 5-field cron in the user's local timezone: minute hour day month weekday
- run_at must be ISO 8601 with timezone offset %s unless the user explicitly specifies another timezone
- prompt is the ACTION to perform, not the scheduling part
- If the user says "amanhã" or relative dates, calculate from current user-local time
- If no time specified, default to 09:00 user-local time`, tzName, offsetText, now.Format(time.RFC3339), offsetText)
}
```

Change:

```go
func (bc *BotController) parseCronWithLLM(text string) (*cronCreateParsed, error)
```

to:

```go
func (bc *BotController) parseCronWithLLM(text string, now time.Time, tzName string) (*cronCreateParsed, error)
```

#### Cron create command

`cmdCronCreate` must resolve user timezone before both fast and LLM parsing:

```go
userID := safeSenderID(c.Sender())
tzName, loc := bc.userTimezone(userID) // empty/invalid => UTC, log invalid
now := time.Now().In(loc)
parsed := cronFastParse(text, now)
if parsed == nil {
    parsed, err = bc.parseCronWithLLM(text, now, displayTimezoneName(tzName))
}
```

Invalid stored timezone behavior:

- log `telegram: invalid profile timezone user=<id> timezone=<value>`;
- fall back to UTC;
- do not block cron creation.

### 5.4 DefaultCWD Fallback

Add final fallback to CWD resolution chain:

```text
agent override
→ projectbinding.Resolve(chat_id, thread_id)
→ session.Store.GetCwd(chat_id, thread_id)
→ Profile.DefaultCWD only for private chats with ThreadID == 0
```

This affects both Telegram command-side `currentCwd` and pipeline-side `effectiveCwd`.

Because `pipeline.Service.effectiveCwd` currently receives only `chatID/threadID` and not Telegram chat type, the implementation must introduce an explicit private-chat signal rather than infer from `chatID` sign alone.

Recommended minimal model:

```go
type Input struct {
    // existing fields...
    IsPrivateChat bool
}
```

or equivalent existing pipeline input field if already present.

Then:

```go
func (s *Service) effectiveCwdForContext(agent *agents.Agent, chatID int64, threadID int, userID int64, isPrivateChat bool) string
```

Rules:

- fallback applies only when `isPrivateChat == true` and `threadID == 0`;
- fallback must not apply to groups, supergroups or topics;
- fallback must not override explicit `/cwd` binding or session CWD;
- if `DefaultCWD` is set but invalid/unreadable, log and ignore;
- do not write fallback into `session.Store`; it is a read-time fallback, not a binding.

Telegram command-side helper:

```go
func (bc *BotController) currentCwdForContext(chatID int64, threadID int, userID int64, isPrivateChat bool) string
```

Keep existing `currentCwd(chatID, threadID)` only if needed by tests; route new feature paths through the context-aware helper.

### 5.5 Onboarding Timezone

Update `internal/telegram/bootstrap_profile.go` to collect timezone after the profile text step and before success.

Recommended bootstrap state expansion:

```go
type bootstrapStep string

const (
    bootstrapStepAssistant bootstrapStep = "assistant"
    bootstrapStepProfile   bootstrapStep = "profile"
    bootstrapStepTimezone  bootstrapStep = "timezone"
)
```

Inline choices:

- `Europe/Lisbon`
- `America/Sao_Paulo`
- `America/New_York`
- `UTC`
- `Other (type manually)`

Behavior:

1. after profile content is generated/written, set pending bootstrap to timezone step;
2. send inline keyboard asking for timezone;
3. if user picks one of the predefined options, validate with `time.LoadLocation` and save `Profile.Timezone`;
4. if user picks Other, ask them to type an IANA timezone string;
5. invalid manual timezone returns a short error and keeps pending state at timezone step;
6. `DefaultCWD` remains empty and is not collected.

Profile persistence must use `users.Store.Save` / `users.Save` atomic write pattern, not ad-hoc JSON writes.

---

## 6. Implementation Order

Features are independently deployable. Recommended order:

1. **Feature 1 — Runlog Message Bridge**
   - Highest operational value for nudge threading and crash-safe correlation.
   - Mostly additive SQLite/runlog/pipeline wiring.
   - Does not depend on profile changes.

2. **Feature 3a/3b — Profile timezone + cron timezone**
   - Adds profile fields and fixes personal-assistant time semantics.
   - Can ship before onboarding updates; existing profiles default to UTC.

3. **Feature 2 — Mode Profiles**
   - Adds user-controlled prompt overlays and command UX.
   - Independent of timezone/default CWD, but shares profile persistence.

4. **Feature 3c — DefaultCWD fallback**
   - Requires careful chat-type propagation into pipeline and command helpers.
   - Should be implemented after profile fields exist.

5. **Feature 3d — Onboarding timezone**
   - UX polish and data collection.
   - Can ship after profile timezone support; does not block cron fallback to UTC.

---

## 7. Interface Changes

### 7.1 `runlog.Store` — public package interface

Before:

```go
type Store interface {
    Start(ctx context.Context, record RunRecord) error
    Update(ctx context.Context, update RunUpdate) error
    // ...
}
```

After:

```go
type Store interface {
    Start(ctx context.Context, record RunRecord) error
    Update(ctx context.Context, update RunUpdate) error
    // ...
    GetLastOutboundMessage(ctx context.Context, sessionFile string) (chatID int64, threadID int, messageID int64, err error)
}
```

All fake stores in tests must implement the new method.

### 7.2 `users.Profile` — public package struct

Before:

```go
type Profile struct {
    UserID      int64     `json:"user_id"`
    Name        string    `json:"name"`
    Language    string    `json:"language"`
    IsOwner     bool      `json:"is_owner"`
    OnboardedAt time.Time `json:"onboarded_at"`
    LastSeenAt  time.Time `json:"last_seen_at"`
}
```

After:

```go
type Profile struct {
    UserID      int64     `json:"user_id"`
    Name        string    `json:"name"`
    Language    string    `json:"language"`
    IsOwner     bool      `json:"is_owner"`
    ActiveMode  string    `json:"active_mode"`
    Timezone    string    `json:"timezone"`
    DefaultCWD  string    `json:"default_cwd"`
    OnboardedAt time.Time `json:"onboarded_at"`
    LastSeenAt  time.Time `json:"last_seen_at"`
}
```

### 7.3 `users.Resolver` — public package methods

Add:

```go
func (r *Resolver) UserModePath(userID int64, mode string) string
```

### 7.4 `persona.BuildPromptForUser` — public package method

Before:

```go
func (s *CanonicalIdentityService) BuildPromptForUser(userID int64, resolver interface{ UserMdPath(userID int64) string }, isOwner bool) (string, error)
```

After:

```go
type UserPromptResolver interface {
    UserMdPath(userID int64) string
    UserModePath(userID int64, mode string) string
}

func (s *CanonicalIdentityService) BuildPromptForUser(userID int64, resolver UserPromptResolver, isOwner bool, activeMode string) (string, error)
```

### 7.5 Telegram command handlers — internal implementation detail

Before:

```go
func (bc *BotController) cmdListAgents() (string, error)
func (bc *BotController) cmdMemoryCheckpoint(chatID int64, threadID int, text string) (string, error)
func (bc *BotController) parseCronWithLLM(text string) (*cronCreateParsed, error)
func (bc *BotController) currentCwd(chatID int64, threadID int) string
```

After:

```go
func (bc *BotController) cmdSetMode(c telebot.Context, text string) (string, error)
func (bc *BotController) cmdListAgents(userID int64) (string, error)
func (bc *BotController) cmdMemoryCheckpoint(chatID int64, threadID int, userID int64, text string) (string, error)
func (bc *BotController) parseCronWithLLM(text string, now time.Time, tzName string) (*cronCreateParsed, error)
func (bc *BotController) currentCwdForContext(chatID int64, threadID int, userID int64, isPrivateChat bool) string
```

### 7.6 Pipeline input/CWD context — internal implementation detail

Add or reuse an input field that explicitly carries private-chat status:

```go
IsPrivateChat bool
```

Do not infer private chat from `chatID` alone.

---

## 8. File Change Index

### `internal/runlog/`

- `types.go` — add/verify message ID fields in `RunRecord` and `RunUpdate`.
- `store.go` — add/verify `GetLastOutboundMessage` interface method.
- `store_sqlite.go` — add/verify idempotent columns, scan/insert/update support and query method.
- `store_test.go` — add/extend tests for migration, scan, update and last outbound lookup.

### `internal/pipeline/`

- `pipeline.go` — wire inbound message ID at run start and outbound message ID after final send.
- `service.go` / input type file — add private-chat signal if not already present.
- `prompt_builder.go` — pass `Profile.ActiveMode` to persona; apply DefaultCWD fallback through context-aware CWD resolver.
- relevant tests — update fake `runlog.Store`, prompt mode tests, default CWD tests.

### `internal/dream/`

- `nudge.go` — use `GetLastOutboundMessage` for threaded nudge send and fallback on missing reply target.
- tests — fake runlog and transport behavior for threaded/fallback sends.

### `internal/users/`

- `profile.go` — add `ActiveMode`, `Timezone`, `DefaultCWD`; add validation helpers if placed here.
- `resolver.go` — add `UserModePath`.
- `store_test.go` or new profile tests — JSON backward compatibility and atomic save behavior.

### `internal/persona/`

- `canonical_service.go` — update `BuildPromptForUser` signature and append mode overlay last.
- `canonical_service_prompt.go` — add `buildModeBlock` helper or equivalent.
- `optional_file.go` — no required change; use existing loader.
- tests — missing overlay ignored, overlay appended last, invalid/empty mode ignored.

### `internal/telegram/`

- `commands.go` — add `CmdSetMode`, matching rules, handler dispatch, `/mode` handler, mode listing, checkpoint mode tag, timezone-aware cron parsing, context-aware CWD helper.
- `cron_fast_parse.go` — likely no signature change; ensure location-aware `now` is used in tests.
- `bootstrap_profile.go` — collect timezone during onboarding.
- route/bootstrap files — handle timezone inline callback/manual typing if route definitions are separated.
- tests — command matching, mode handler, list agents active mode, checkpoint tag, cron timezone prompt, onboarding timezone.

### `internal/cron/`

- No schema change required for this spec unless current cron runtime stores/executes schedules in UTC without preserving intended user-local semantics.
- If cron job execution currently assumes server-local time for cron expressions, add a separate follow-up to store timezone per recurring job. This spec only requires parsing creation in the user timezone.

---

## 9. Test Requirements

### Feature 1 — Runlog Message Bridge

Example tests:

- `TestSQLiteStore_InboundOutboundMessageIDs`
- `TestSQLiteStore_GetLastOutboundMessage_ReturnsNewestNonZeroForSessionFile`
- `TestSQLiteStore_GetLastOutboundMessage_EmptySessionReturnsZero`
- `TestSQLiteStore_MessageIDMigration_IsIdempotent`
- `TestPipeline_RunlogStartIncludesInboundMessageID`
- `TestPipeline_RunlogUpdateIncludesOutboundMessageID`
- `TestNudge_ThreadsUnderLastOutboundMessage`
- `TestNudge_ReplyTargetMissingFallsBackToNormalSend`

Assertions:

- message IDs persist and scan through `GetRun/ListRuns`;
- no message text is persisted;
- old DB rows return zero message IDs;
- outbound update is best-effort and does not fail delivery;
- nudge send always preserves thread ID in group topics.

### Feature 2 — Mode Profiles

Example tests:

- `TestNormalizeMode_AcceptsAliases`
- `TestProfile_JSONBackwardCompatibility_EmptyActiveMode`
- `TestResolver_UserModePath`
- `TestBuildPromptForUser_AppendsActiveModeLast`
- `TestBuildPromptForUser_MissingModeOverlayIgnored`
- `TestMatchCommand_ModePhrases`
- `TestCmdSetMode_UpdatesProfileWithoutSessionReset`
- `TestCmdSetMode_QueryCurrentMode`
- `TestCmdListAgents_ShowsActiveModeSuffix`
- `TestMemoryCheckpoint_AppendsModeTag`

Assertions:

- invalid mode returns a clear user-facing error;
- `/mode auto` or `/mode general` returns default/general behavior;
- mode change affects next prompt build without resetting Pi session;
- owner-only restrictions are not applied to `/mode`.

### Feature 3 — Profile Enrichment

Example tests:

- `TestProfile_JSONBackwardCompatibility_TimezoneDefaultCWD`
- `TestNormalizeTimezone_EmptyReturnsUTC`
- `TestNormalizeTimezone_InvalidErrors`
- `TestCmdCronCreate_UsesProfileTimezoneForFastParseTomorrow`
- `TestParseCronWithLLM_SystemPromptUsesUserTimezoneOffset`
- `TestCurrentCwdForContext_UsesDefaultCWDOnlyPrivateNoBinding`
- `TestCurrentCwdForContext_BindingBeatsDefaultCWD`
- `TestCurrentCwdForContext_DoesNotUseDefaultCWDInGroupOrTopic`
- `TestBootstrapTimezone_PredefinedChoiceSavesProfileTimezone`
- `TestBootstrapTimezone_InvalidManualKeepsPendingStep`

Assertions:

- old profiles default to UTC and no default CWD;
- invalid timezone does not crash cron creation;
- private-chat fallback does not leak into groups/topics;
- onboarding saves timezone via atomic profile save.

### Validation Commands

Use project commands from `AGENTS.md`:

```bash
go build ./...
go test ./... -short
go vet ./...
```

For final validation before merge:

```bash
go test ./... -v
```

---

## 10. Migration Safety

### SQLite

- `run_journal` migrations are additive only.
- New columns are nullable `INTEGER DEFAULT 0`.
- Migration runs idempotently on startup.
- Existing rows remain valid and scan with zero message IDs.
- No destructive schema changes.
- No change to `run_events` required.

### JSON Profiles

- Adding `ActiveMode`, `Timezone`, and `DefaultCWD` is backward-compatible with Go JSON decoding.
- Missing fields decode as empty strings.
- Empty `ActiveMode` means general/default mode.
- Empty `Timezone` means UTC.
- Empty `DefaultCWD` means no fallback.
- Saves must continue to use the existing atomic temp-file + rename pattern.

### Prompt and Session Behavior

- Mode overlay changes do not reset Pi sessions.
- Because Aurelia passes a fresh system prompt per request, the mode takes effect on the next message.
- Missing overlay files are ignored, not fatal.
- DefaultCWD is read-time fallback only and must not create or mutate `/cwd` bindings.

### Telegram Compatibility

- Message IDs are stored as `int64` even though Telegram message IDs are currently within `int` range in telebot.
- Reply threading degrades to normal send if Telegram rejects the reply target.
- All topic sends must include explicit `ThreadID` to avoid leaking replies into the general group chat.

### Independent Deployment Matrix

| Feature | Can ship alone? | Backward-compatible default |
|---|---:|---|
| Runlog Message Bridge | Yes | message IDs default to `0`; nudge can skip threading |
| Mode Profiles | Yes | empty mode = general; missing overlay ignored |
| Timezone | Yes | empty/invalid timezone falls back to UTC |
| DefaultCWD | Yes | empty default CWD means no fallback |
| Onboarding Timezone | Yes, after profile field exists | existing onboarded users unchanged |

---

## Prior Lessons Applied

- `.opencode/lessons/learned/cwd-binding-source-of-truth.md` → DefaultCWD must be a final fallback in one CWD resolution chain, never another competing source that bypasses bindings.
- `.opencode/lessons/learned/telegram-threadid-explicit.md` → nudge fallback/reply sends must always pass explicit `ThreadID` for topic-enabled groups.
- `.opencode/lessons/learned/redaction-before-truncation.md` → this spec forbids storing message text/content in runlog; only IDs are persisted.
- `.opencode/lessons/learned/delegate-to-dependency.md` → no Pi conversation-history management is added; Aurelia records coordination metadata only.
