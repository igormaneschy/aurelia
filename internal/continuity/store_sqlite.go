package continuity

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens or creates the continuity database at dbPath.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// Create the file with restrictive permissions before sql.Open creates it,
	// preventing world-readable database files (matching runlog pattern).
	f, err := os.OpenFile(dbPath, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("create continuity db file: %w", err)
	}
	_ = f.Close()

	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open continuity sqlite store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &SQLiteStore{db: db}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Ensure db/wal/shm have owner-only permissions (0600).
	chmod0600 := func(path string) {
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&077 != 0 {
			if chmodErr := os.Chmod(path, 0600); chmodErr != nil {
				log.Printf("Warning: failed to chmod continuity file %s: %v", path, chmodErr)
			}
		}
	}
	chmod0600(dbPath)
	chmod0600(dbPath + "-wal")
	chmod0600(dbPath + "-shm")

	return store, nil
}

func (s *SQLiteStore) initialize() error {
	// Create the table with the new 3-column primary key.
	// The migration from (chat_id, thread_id) to (chat_id, thread_id, user_id)
	// is handled by ensureUserIDColumn below.
	query := `
	CREATE TABLE IF NOT EXISTS conversation_state (
		chat_id INTEGER NOT NULL,
		thread_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL DEFAULT 0,
		cwd TEXT DEFAULT '',
		active_goal TEXT DEFAULT '',
		last_user_intent TEXT DEFAULT '',
		last_assistant_summary TEXT DEFAULT '',
		last_checkpoint TEXT DEFAULT '',
		last_run_id TEXT DEFAULT '',
		last_run_status TEXT DEFAULT '',
		last_tools TEXT DEFAULT '',
		session_id TEXT DEFAULT '',
		session_cold INTEGER NOT NULL DEFAULT 0,
		reset_reason TEXT DEFAULT '',
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (chat_id, thread_id, user_id)
	);
	CREATE INDEX IF NOT EXISTS idx_conversation_state_cwd
	ON conversation_state(cwd);
	`
	if _, err := s.db.Exec(query); err != nil {
		return fmt.Errorf("initialize continuity schema: %w", err)
	}
	return s.ensureUserIDColumn()
}

// ensureUserIDColumn migrates from the old (chat_id, thread_id) primary key
// to the new (chat_id, thread_id, user_id) primary key. This is safe to run
// on every startup — it detects whether the migration is needed.
// The migration runs inside a single transaction for atomicity: if the daemon
// crashes mid-migration, SQLite rolls back and the old table is untouched.
func (s *SQLiteStore) ensureUserIDColumn() error {
	// Check if user_id is already part of the PK using pragma_table_info.pk.
	// pk=0 means the column is NOT in the primary key; pk>0 means it is.
	// This correctly handles the edge case where user_id exists as a regular
	// column but was never promoted to the PK (e.g., manual ALTER TABLE).
	var pkPosition int
	err := s.db.QueryRow(`
		SELECT pk FROM pragma_table_info('conversation_state')
		WHERE name = 'user_id'`).Scan(&pkPosition)
	if err != nil {
		// Column doesn't exist or query failed — proceed to migration.
		// sql.ErrNoRows means the column is absent, which is fine.
		if err.Error() != "sql: no rows in result set" {
			return fmt.Errorf("check user_id PK: %w", err)
		}
	}
	if pkPosition > 0 {
		// Already migrated — user_id is part of the PK.
		return nil
	}

	// Migration needed: recreate the table with the new PK inside a transaction.
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback() // no-op after successful Commit

	// Step 1: Create new table.
	_, err = tx.Exec(`
	CREATE TABLE IF NOT EXISTS conversation_state_new (
		chat_id INTEGER NOT NULL,
		thread_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL DEFAULT 0,
		cwd TEXT DEFAULT '',
		active_goal TEXT DEFAULT '',
		last_user_intent TEXT DEFAULT '',
		last_assistant_summary TEXT DEFAULT '',
		last_checkpoint TEXT DEFAULT '',
		last_run_id TEXT DEFAULT '',
		last_run_status TEXT DEFAULT '',
		last_tools TEXT DEFAULT '',
		session_id TEXT DEFAULT '',
		session_cold INTEGER NOT NULL DEFAULT 0,
		reset_reason TEXT DEFAULT '',
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (chat_id, thread_id, user_id)
	)`)
	if err != nil {
		return fmt.Errorf("create conversation_state_new: %w", err)
	}

	// Step 2: Copy data (user_id defaults to 0 for legacy rows).
	_, err = tx.Exec(`
	INSERT OR IGNORE INTO conversation_state_new
		(chat_id, thread_id, user_id, cwd, active_goal, last_user_intent,
		 last_assistant_summary, last_checkpoint, last_run_id,
		 last_run_status, last_tools, session_id, session_cold,
		 reset_reason, updated_at)
	SELECT chat_id, thread_id, 0, cwd, active_goal, last_user_intent,
		 last_assistant_summary, last_checkpoint, last_run_id,
		 last_run_status, last_tools, session_id, session_cold,
		 reset_reason, updated_at
	FROM conversation_state`)
	if err != nil {
		return fmt.Errorf("copy conversation_state data: %w", err)
	}

	// Step 3: Drop old table.
	_, err = tx.Exec(`DROP TABLE conversation_state`)
	if err != nil {
		return fmt.Errorf("drop old conversation_state: %w", err)
	}

	// Step 4: Rename new table to replace the old one.
	_, err = tx.Exec(`ALTER TABLE conversation_state_new RENAME TO conversation_state`)
	if err != nil {
		return fmt.Errorf("rename conversation_state_new: %w", err)
	}

	// Step 5: Recreate index.
	_, err = tx.Exec(`
	CREATE INDEX IF NOT EXISTS idx_conversation_state_cwd
	ON conversation_state(cwd)`)
	if err != nil {
		return fmt.Errorf("recreate cwd index: %w", err)
	}

	return tx.Commit()
}

// Get retrieves the current state for a chat/thread, or nil if absent.
func (s *SQLiteStore) Get(ctx context.Context, chatID int64, threadID int, userID int64) (*ConversationState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT chat_id, thread_id, user_id, cwd,
		       active_goal, last_user_intent, last_assistant_summary,
		       last_checkpoint, last_run_id, last_run_status,
		       last_tools, session_id, session_cold,
		       reset_reason, updated_at
		FROM conversation_state
		WHERE chat_id = ? AND thread_id = ? AND user_id = ?`, chatID, threadID, userID)

	state, err := scanState(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("continuity get chat=%d thread=%d: %w", chatID, threadID, err)
	}
	return state, nil
}

// Upsert fully replaces the state for a chat/thread/user.
// All text fields are sanitized (redacted + capped) before storage.
func (s *SQLiteStore) Upsert(ctx context.Context, state ConversationState) error {
	now := unix(state.UpdatedAt)
	sessionCold := boolToInt(state.SessionCold)

	state.CWD = sanitize(state.CWD, 200)
	state.ActiveGoal = sanitize(state.ActiveGoal, MaxActiveGoal)
	state.LastUserIntent = sanitize(state.LastUserIntent, MaxUserIntent)
	state.LastAssistantSummary = sanitize(state.LastAssistantSummary, MaxAssistantSummary)
	state.LastCheckpoint = sanitize(state.LastCheckpoint, MaxCheckpoint)
	state.LastRunID = sanitize(state.LastRunID, 200)
	state.LastRunStatus = sanitize(state.LastRunStatus, 100)
	state.LastTools = sanitize(state.LastTools, MaxTools)
	state.SessionID = sanitize(state.SessionID, 200)
	state.ResetReason = sanitize(state.ResetReason, 300)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conversation_state
			(chat_id, thread_id, user_id, cwd, active_goal, last_user_intent,
			 last_assistant_summary, last_checkpoint, last_run_id,
			 last_run_status, last_tools, session_id, session_cold,
			 reset_reason, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat_id, thread_id, user_id) DO UPDATE SET
			cwd = excluded.cwd,
			active_goal = excluded.active_goal,
			last_user_intent = excluded.last_user_intent,
			last_assistant_summary = excluded.last_assistant_summary,
			last_checkpoint = excluded.last_checkpoint,
			last_run_id = excluded.last_run_id,
			last_run_status = excluded.last_run_status,
			last_tools = excluded.last_tools,
			session_id = excluded.session_id,
			session_cold = excluded.session_cold,
			reset_reason = excluded.reset_reason,
			updated_at = excluded.updated_at`,
		state.ChatID, state.ThreadID, state.UserID, state.CWD,
		state.ActiveGoal, state.LastUserIntent,
		state.LastAssistantSummary, state.LastCheckpoint,
		state.LastRunID, state.LastRunStatus,
		state.LastTools, state.SessionID, sessionCold,
		state.ResetReason, now)
	if err != nil {
		return fmt.Errorf("continuity upsert chat=%d thread=%d: %w", state.ChatID, state.ThreadID, err)
	}
	return nil
}

// Patch applies partial updates without overwriting unset fields.
// Uses a single atomic INSERT ... ON CONFLICT DO UPDATE.
// All values args come first, then SET args. For unset fields (nil pointer),
// the SET clause uses COALESCE(NULL, col) to preserve the existing value.
// All text fields are sanitized (redacted + capped) before storage.
func (s *SQLiteStore) Patch(ctx context.Context, key ConversationKey, patch StatePatch) error {
	now := unix(patch.UpdatedAt)

	cols := "chat_id, thread_id, user_id, updated_at"
	vals := "?, ?, ?, ?"
	var valueArgs []any // VALUES arguments in column order
	var setClauses []string
	var setArgs []any // SET arguments

	valueArgs = append(valueArgs, key.ChatID, key.ThreadID, key.UserID, now)
	setClauses = append(setClauses, "updated_at = ?")
	setArgs = append(setArgs, now)

	appendField := func(col string, val *string, cap int, def string) {
		cols += ", " + col
		vals += ", ?"
		valueArgs = append(valueArgs, sanitizePtr(val, cap, def))
		if val != nil {
			sanitized := sanitize(*val, cap)
			setClauses = append(setClauses, col+" = ?")
			setArgs = append(setArgs, sanitized)
		} else {
			setClauses = append(setClauses, col+" = COALESCE(NULL, "+col+")")
		}
	}
	appendIntField := func(col string, val *bool) {
		cols += ", " + col
		vals += ", ?"
		if val != nil {
			valueArgs = append(valueArgs, boolToInt(*val))
			setClauses = append(setClauses, col+" = ?")
			setArgs = append(setArgs, boolToInt(*val))
		} else {
			valueArgs = append(valueArgs, 0)
			setClauses = append(setClauses, col+" = COALESCE(NULL, "+col+")")
		}
	}

	appendField("cwd", patch.CWD, 200, "")
	appendField("active_goal", patch.ActiveGoal, MaxActiveGoal, "")
	appendField("last_user_intent", patch.LastUserIntent, MaxUserIntent, "")
	appendField("last_assistant_summary", patch.LastAssistantSummary, MaxAssistantSummary, "")
	appendField("last_checkpoint", patch.LastCheckpoint, MaxCheckpoint, "")
	appendField("last_run_id", patch.LastRunID, 200, "")
	appendField("last_run_status", patch.LastRunStatus, 100, "")
	appendField("last_tools", patch.LastTools, MaxTools, "")
	appendField("session_id", patch.SessionID, 200, "")
	appendIntField("session_cold", patch.SessionCold)
	appendField("reset_reason", patch.ResetReason, 300, "")

	// Concatenate valueArgs and setArgs in the correct order:
	// VALUES args first, then SET args.
	setStr := strings.Join(setClauses, ", ")
	allArgs := append(valueArgs, setArgs...)

	q := fmt.Sprintf(`
		INSERT INTO conversation_state (%s)
		VALUES (%s)
		ON CONFLICT(chat_id, thread_id, user_id) DO UPDATE SET %s`, cols, vals, setStr)

	_, err := s.db.ExecContext(ctx, q, allArgs...)
	if err != nil {
		return fmt.Errorf("continuity patch chat=%d thread=%d: %w", key.ChatID, key.ThreadID, err)
	}
	return nil
}

// MarkColdForSessions sets session_cold=1 and reset_reason for all rows
// that have a non-empty session_id. Used when the bridge process dies.
func (s *SQLiteStore) MarkColdForSessions(ctx context.Context, reason string) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		UPDATE conversation_state
		SET session_cold = 1, reset_reason = ?, updated_at = ?
		WHERE session_id != '' AND session_id IS NOT NULL`,
		reason, now)
	if err != nil {
		return fmt.Errorf("continuity mark cold for sessions: %w", err)
	}
	return nil
}

// Close releases the database connection.
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// --- sanitization ---

// sanitize redacts secrets from s and caps it to at most max runes.
func sanitize(s string, max int) string {
	if s == "" {
		return ""
	}
	s = redactSecrets(s)
	return capString(s, max)
}

// sanitizePtr calls sanitize on *val if non-nil, or returns def.
func sanitizePtr(val *string, max int, def string) string {
	if val == nil {
		return def
	}
	return sanitize(*val, max)
}

// escapeUntrusted escapes < and > to prevent delimiter injection in untrusted
// content blocks like <continuity_state_untrusted> and <checkpoint_untrusted>.
func escapeUntrusted(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// EscapeUntrusted is the exported wrapper for escapeUntrusted, used by
// pipeline to escape checkpoint content before prompt injection.
func EscapeUntrusted(s string) string { return escapeUntrusted(s) }

// redactSecrets replaces known credential patterns with [REDACTED] markers.
// This is a defense-in-depth sanitization — callers should also redact before
// calling store methods. Patterns are a subset of pipeline.RedactSecrets.
func redactSecrets(s string) string {
	result := s
	for i, re := range redactPrefixREs {
		result = re.ReplaceAllString(result, redactPrefixLabels[i])
	}
	result = redactPrivateKeyRE.ReplaceAllString(result, "[PRIVATE_KEY_BLOCK_REDACTED]")
	result = redactAuthRE.ReplaceAllString(result, "$1[REDACTED]")

	lines := strings.Split(result, "\n")
	var filtered []string
	for _, line := range lines {
		lower := strings.ToLower(line)
		if redactLineRE.MatchString(lower) || redactJSONSecretRE.MatchString(line) {
			filtered = append(filtered, "[CREDENTIAL_REDACTED]")
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

// Pre-compiled redaction regexes (subset of pipeline's patterns).
var (
	redactPrefixREs    []*regexp.Regexp
	redactPrefixLabels []string
	redactPrivateKeyRE *regexp.Regexp
	redactAuthRE       *regexp.Regexp
	redactLineRE       *regexp.Regexp
	redactJSONSecretRE *regexp.Regexp
)

func init() {
	type pattern struct {
		repl  string
		label string
	}
	prefixPatterns := []pattern{
		{`\bsk-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bpk-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk-ant-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk-proj-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk_live_[A-Za-z0-9]+`, "[STRIPE_KEY_REDACTED]"},
		{`\bsk_test_[A-Za-z0-9]+`, "[STRIPE_KEY_REDACTED]"},
		{`\bAKIA[A-Z0-9]{16}`, "[AWS_KEY_REDACTED]"},
		{`\bAIza[0-9A-Za-z_-]{35}`, "[GCP_KEY_REDACTED]"},
		{`\bghp_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bgho_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghu_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghs_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghr_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bgithub_pat_[0-9A-Za-z_-]+`, "[GH_PAT_REDACTED]"},
		{`\bglpat-[A-Za-z0-9_-]{20,}`, "[GL_TOKEN_REDACTED]"},
		{`\bhf_[A-Za-z0-9]{20,}`, "[HF_TOKEN_REDACTED]"},
		{`\bnpm_[A-Za-z0-9]{36}`, "[NPM_TOKEN_REDACTED]"},
		{`\bxox[bpasa]-[A-Za-z0-9-]{20,}`, "[SLACK_TOKEN_REDACTED]"},
		{`\bxapp-[A-Za-z0-9-]{20,}`, "[SLACK_TOKEN_REDACTED]"},
		{`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+`, "[JWT_REDACTED]"},
		{`\bxai-[A-Za-z0-9]{20,}`, "[XAI_KEY_REDACTED]"},
	}

	redactPrefixREs = make([]*regexp.Regexp, len(prefixPatterns))
	redactPrefixLabels = make([]string, len(prefixPatterns))
	for i, p := range prefixPatterns {
		redactPrefixREs[i] = regexp.MustCompile(p.repl)
		redactPrefixLabels[i] = p.label
	}

	redactPrivateKeyRE = regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----`)
	redactAuthRE = regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|Basic)\s+)\S+`)
	redactLineRE = regexp.MustCompile(`(password|secret|api_key|api-key|api\.key|apikey|clientsecret|client_secret|access_token|refresh_token|token)\s*[=:]\s*\S+`)
	redactJSONSecretRE = regexp.MustCompile(`"(?:apiKey|api_key|api-key|api\.key|clientSecret|client_secret|client-secret|client\.secret|accessToken|access_token|access-token|access\.token|refreshToken|refresh_token|refresh-token|refresh\.token|token)"\s*:\s*"[^"]{4,}"`)
}

// --- helpers ---

type stateScanner interface {
	Scan(dest ...any) error
}

func scanState(row stateScanner) (*ConversationState, error) {
	var s ConversationState
	var sessionCold int
	var updatedAt int64
	err := row.Scan(&s.ChatID, &s.ThreadID, &s.UserID, &s.CWD,
		&s.ActiveGoal, &s.LastUserIntent, &s.LastAssistantSummary,
		&s.LastCheckpoint, &s.LastRunID, &s.LastRunStatus,
		&s.LastTools, &s.SessionID, &sessionCold,
		&s.ResetReason, &updatedAt)
	if err != nil {
		return nil, err
	}
	s.SessionCold = sessionCold != 0
	s.UpdatedAt = fromUnix(updatedAt)
	return &s, nil
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return time.Now().Unix()
	}
	return t.Unix()
}

func fromUnix(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// capString truncates s to at most max runes, respecting UTF-8 boundaries.
func capString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	// Truncate by bytes, walking back to a valid rune boundary.
	trimmed := s
	for utf8.RuneCountInString(trimmed) > max {
		trimmed = trimmed[:len(trimmed)-1]
	}
	// Ensure valid UTF-8 at boundary.
	for len(trimmed) > 0 && trimmed[len(trimmed)-1]&0xC0 == 0x80 {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed
}
