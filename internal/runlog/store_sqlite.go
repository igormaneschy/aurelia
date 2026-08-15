package runlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/igormaneschy/aurelia/internal/observability"
	_ "modernc.org/sqlite"
)

// migratedColumns lists columns added by observability migration, keyed by
// their SQL type definition. Used for idempotent ALTER TABLE ADD.
type columnDef struct {
	name string
	typ  string
	def  string // DEFAULT value
}

var observabilityColumns = []columnDef{
	{name: "user_id", typ: "INTEGER", def: "0"},
	{name: "entrypoint", typ: "TEXT", def: "''"},
	{name: "agent_name", typ: "TEXT", def: "''"},
	{name: "provider", typ: "TEXT", def: "''"},
	{name: "model", typ: "TEXT", def: "''"},
	{name: "capability_profile", typ: "TEXT", def: "''"},
	{name: "duration_ms", typ: "INTEGER", def: "0"},
	{name: "input_tokens", typ: "INTEGER", def: "0"},
	{name: "output_tokens", typ: "INTEGER", def: "0"},
	{name: "cost_usd", typ: "REAL", def: "0"},
	{name: "tool_count", typ: "INTEGER", def: "0"},
	{name: "error_class", typ: "TEXT", def: "''"},
	{name: "timeout_origin", typ: "TEXT", def: "''"},
	{name: "used_fallback", typ: "INTEGER", def: "0"},
	{name: "session_file", typ: "TEXT", def: "''"},
	{name: "parent_run_id", typ: "TEXT", def: "''"},
	{name: "inbound_message_id", typ: "INTEGER", def: "0"},
	{name: "outbound_message_id", typ: "INTEGER", def: "0"},
	{name: "first_feedback_ms", typ: "INTEGER", def: "0"},
	{name: "max_silence_ms", typ: "INTEGER", def: "0"},
	{name: "stall_count", typ: "INTEGER", def: "0"},
	{name: "steer_count", typ: "INTEGER", def: "0"},
}

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens or creates the runlog database at dbPath.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// Create the file with restrictive permissions before sql.Open creates it,
	// to prevent world-readable database files (C3 in security review).
	// sql.Open will open the existing file without changing permissions.
	f, err := os.OpenFile(dbPath, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("create runlog db file: %w", err)
	}
	_ = f.Close()

	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open runlog sqlite store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &SQLiteStore{db: db}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Ensure existing runlog.db*, runlog.db-wal, runlog.db-shm have
	// owner-only permissions to prevent credential leakage from
	// persisted run data.
	chmod0600 := func(path string) {
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&077 != 0 {
			if chmodErr := os.Chmod(path, 0600); chmodErr != nil {
				log.Printf("Warning: failed to chmod runlog file %s: %v", path, chmodErr)
			}
		}
	}
	chmod0600(dbPath)
	chmod0600(dbPath + "-wal")
	chmod0600(dbPath + "-shm")

	return store, nil
}

func (s *SQLiteStore) initialize() error {
	// Base table (idempotent).
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS run_journal (
		run_id TEXT PRIMARY KEY,
		chat_id INTEGER NOT NULL,
		thread_id INTEGER NOT NULL,
		request_id TEXT NOT NULL,
		session_id TEXT DEFAULT '',
		cwd TEXT DEFAULT '',
		prompt TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'running',
		checkpoint TEXT DEFAULT '',
		tool_summary TEXT DEFAULT '',
		error TEXT DEFAULT '',
		started_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		completed_at INTEGER DEFAULT 0
	);`)
	if err != nil {
		return fmt.Errorf("create run_journal table: %w", err)
	}

	// Idempotent migration: add each observability column if it doesn't exist.
	// SQLite does not support IF NOT EXISTS for ALTER TABLE ADD COLUMN, so we
	// attempt each and ignore "duplicate column" errors.
	for _, col := range observabilityColumns {
		alterSQL := fmt.Sprintf("ALTER TABLE run_journal ADD COLUMN %s %s DEFAULT %s", col.name, col.typ, col.def)
		if _, err := s.db.Exec(alterSQL); err != nil {
			// Ignore "duplicate column" errors; surface other unexpected errors.
			errStr := err.Error()
			if !strings.Contains(errStr, "duplicate column") &&
				!strings.Contains(errStr, "already exists") {
				return fmt.Errorf("migrate run_journal add %s: %w", col.name, err)
			}
		}
	}

	// run_events timeline table.
	if _, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS run_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id TEXT NOT NULL,
		ts INTEGER NOT NULL,
		phase TEXT NOT NULL,
		level TEXT NOT NULL DEFAULT 'info',
		message TEXT DEFAULT '',
		metadata_json TEXT DEFAULT '{}'
	);`); err != nil {
		return fmt.Errorf("create run_events table: %w", err)
	}

	// Indexes.
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_run_journal_chat_thread ON run_journal(chat_id, thread_id, started_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_run_journal_started ON run_journal(started_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_run_journal_user_started ON run_journal(user_id, started_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_run_journal_status_started ON run_journal(status, started_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_run_events_run_ts ON run_events(run_id, ts, id)",
		"CREATE INDEX IF NOT EXISTS idx_run_events_phase_ts ON run_events(phase, ts DESC)",
		"CREATE INDEX IF NOT EXISTS idx_run_journal_session_started ON run_journal(session_file, started_at DESC)",
	}
	for _, idx := range indexes {
		if _, err := s.db.Exec(idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Base Store methods
// ---------------------------------------------------------------------------

// Start inserts a new run record with status=running.
// Uses record.StartedAt when non-zero, otherwise falls back to time.Now().
func (s *SQLiteStore) Start(ctx context.Context, record RunRecord) error {
	record.RunID = sanitizeRunlogText(record.RunID, 128)
	record.RequestID = sanitizeRunlogText(record.RequestID, 128)
	record.SessionID = sanitizeRunlogText(record.SessionID, 256)
	record.CWD = sanitizeRunlogText(record.CWD, 512)
	record.Prompt = sanitizeRunlogText(record.Prompt, 4096)
	record.Checkpoint = sanitizeRunlogText(record.Checkpoint, 4096)
	record.ToolSummary = sanitizeRunlogText(record.ToolSummary, 4096)
	record.Error = sanitizeRunlogText(record.Error, maxEventMessageBytes)
	record.EntryPoint = sanitizeRunlogText(record.EntryPoint, 64)
	record.AgentName = sanitizeRunlogText(record.AgentName, 128)
	record.Provider = sanitizeRunlogText(record.Provider, 128)
	record.Model = sanitizeRunlogText(record.Model, 256)
	record.CapabilityProfile = sanitizeRunlogText(record.CapabilityProfile, 128)
	record.SessionFile = sanitizeRunlogText(record.SessionFile, 512)
	record.ParentRunID = sanitizeRunlogText(record.ParentRunID, 128)
	now := unix(time.Now())
	startedAt := now
	if !record.StartedAt.IsZero() {
		startedAt = unix(record.StartedAt)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO run_journal
			(run_id, chat_id, thread_id, request_id, session_id, cwd, prompt,
			 status, checkpoint, tool_summary, error,
			 started_at, updated_at, completed_at,
			 user_id, entrypoint, agent_name, provider, model,
			 capability_profile, session_file, parent_run_id,
			 inbound_message_id, outbound_message_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		        ?, ?, ?, ?, ?,
		        ?, ?, ?,
		        ?, ?)`,
		record.RunID, record.ChatID, record.ThreadID,
		record.RequestID, record.SessionID, record.CWD, record.Prompt,
		RunRunning, record.Checkpoint, record.ToolSummary, record.Error,
		startedAt, now, 0,
		record.UserID, record.EntryPoint, record.AgentName, record.Provider, record.Model,
		record.CapabilityProfile, record.SessionFile, record.ParentRunID,
		record.InboundMessageID, record.OutboundMessageID)
	if err != nil {
		return fmt.Errorf("runlog start %s: %w", record.RunID, err)
	}
	return nil
}

// Update applies partial updates to an existing run.
func (s *SQLiteStore) Update(ctx context.Context, update RunUpdate) error {
	update.RunID = sanitizeRunlogText(update.RunID, 128)
	sanitizePtr := func(value *string, maxRunes int) *string {
		if value == nil {
			return nil
		}
		clean := sanitizeRunlogText(*value, maxRunes)
		return &clean
	}
	update.SessionID = sanitizePtr(update.SessionID, 256)
	update.Checkpoint = sanitizePtr(update.Checkpoint, 4096)
	update.ToolSummary = sanitizePtr(update.ToolSummary, 4096)
	update.Error = sanitizePtr(update.Error, maxEventMessageBytes)
	update.EntryPoint = sanitizePtr(update.EntryPoint, 64)
	update.AgentName = sanitizePtr(update.AgentName, 128)
	update.Provider = sanitizePtr(update.Provider, 128)
	update.Model = sanitizePtr(update.Model, 256)
	update.CapabilityProfile = sanitizePtr(update.CapabilityProfile, 128)
	update.ErrorClass = sanitizePtr(update.ErrorClass, 128)
	update.TimeoutOrigin = sanitizePtr(update.TimeoutOrigin, 128)
	update.SessionFile = sanitizePtr(update.SessionFile, 512)
	update.ParentRunID = sanitizePtr(update.ParentRunID, 128)
	now := unix(time.Now())

	sets := "updated_at = ?"
	args := []any{now}

	if update.SessionID != nil {
		sets += ", session_id = ?"
		args = append(args, *update.SessionID)
	}
	if update.Status != nil {
		sets += ", status = ?"
		args = append(args, string(*update.Status))
	}
	if update.Checkpoint != nil {
		sets += ", checkpoint = ?"
		args = append(args, *update.Checkpoint)
	}
	if update.ToolSummary != nil {
		sets += ", tool_summary = ?"
		args = append(args, *update.ToolSummary)
	}
	if update.Error != nil {
		sets += ", error = ?"
		args = append(args, *update.Error)
	}
	if update.CompletedAt != nil {
		sets += ", completed_at = ?"
		args = append(args, unix(*update.CompletedAt))
	}

	// Extended fields.
	if update.UserID != nil {
		sets += ", user_id = ?"
		args = append(args, *update.UserID)
	}
	if update.EntryPoint != nil {
		sets += ", entrypoint = ?"
		args = append(args, *update.EntryPoint)
	}
	if update.AgentName != nil {
		sets += ", agent_name = ?"
		args = append(args, *update.AgentName)
	}
	if update.Provider != nil {
		sets += ", provider = ?"
		args = append(args, *update.Provider)
	}
	if update.Model != nil {
		sets += ", model = ?"
		args = append(args, *update.Model)
	}
	if update.CapabilityProfile != nil {
		sets += ", capability_profile = ?"
		args = append(args, *update.CapabilityProfile)
	}
	if update.DurationMs != nil {
		sets += ", duration_ms = ?"
		args = append(args, *update.DurationMs)
	}
	if update.InputTokens != nil {
		sets += ", input_tokens = ?"
		args = append(args, *update.InputTokens)
	}
	if update.OutputTokens != nil {
		sets += ", output_tokens = ?"
		args = append(args, *update.OutputTokens)
	}
	if update.CostUSD != nil {
		sets += ", cost_usd = ?"
		args = append(args, *update.CostUSD)
	}
	if update.ToolCount != nil {
		sets += ", tool_count = ?"
		args = append(args, *update.ToolCount)
	}
	if update.ErrorClass != nil {
		sets += ", error_class = ?"
		args = append(args, *update.ErrorClass)
	}
	if update.TimeoutOrigin != nil {
		sets += ", timeout_origin = ?"
		args = append(args, *update.TimeoutOrigin)
	}
	if update.UsedFallback != nil {
		val := 0
		if *update.UsedFallback {
			val = 1
		}
		sets += ", used_fallback = ?"
		args = append(args, val)
	}
	if update.SessionFile != nil {
		sets += ", session_file = ?"
		args = append(args, *update.SessionFile)
	}
	if update.ParentRunID != nil {
		sets += ", parent_run_id = ?"
		args = append(args, *update.ParentRunID)
	}

	// Pi session ↔ Telegram message bridge.
	if update.InboundMessageID != nil {
		sets += ", inbound_message_id = ?"
		args = append(args, *update.InboundMessageID)
	}
	if update.OutboundMessageID != nil {
		sets += ", outbound_message_id = ?"
		args = append(args, *update.OutboundMessageID)
	}

	// Long-session aggregates.
	if update.FirstFeedbackMs != nil {
		sets += ", first_feedback_ms = ?"
		args = append(args, *update.FirstFeedbackMs)
	}
	if update.MaxSilenceMs != nil {
		sets += ", max_silence_ms = ?"
		args = append(args, *update.MaxSilenceMs)
	}
	if update.StallCount != nil {
		sets += ", stall_count = ?"
		args = append(args, *update.StallCount)
	}
	if update.SteerCount != nil {
		sets += ", steer_count = ?"
		args = append(args, *update.SteerCount)
	}

	args = append(args, update.RunID)
	q := fmt.Sprintf("UPDATE run_journal SET %s WHERE run_id = ?", sets)
	_, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("runlog update %s: %w", update.RunID, err)
	}
	return nil
}

// Complete marks a run with a terminal status and optional checkpoint/error/tool summary.
// Also sets completed_at and updates updated_at.
func (s *SQLiteStore) Complete(ctx context.Context, runID string, status RunStatus, checkpoint, errMsg, toolSummary string) error {
	runID = sanitizeRunlogText(runID, 128)
	checkpoint = sanitizeRunlogText(checkpoint, 4096)
	errMsg = sanitizeRunlogText(errMsg, maxEventMessageBytes)
	toolSummary = sanitizeRunlogText(toolSummary, 4096)
	now := unix(time.Now())
	_, err := s.db.ExecContext(ctx, `
		UPDATE run_journal
		SET status = ?, checkpoint = ?, error = ?, tool_summary = ?,
		    updated_at = ?, completed_at = ?
		WHERE run_id = ? AND status = 'running'`,
		string(status), checkpoint, errMsg, toolSummary, now, now, runID)
	if err != nil {
		return fmt.Errorf("runlog complete %s: %w", runID, err)
	}
	return nil
}

// MarkStaleRunsInterrupted transitions every row still in status=running to
// status=interrupted. Called at daemon startup: any row still running after a
// restart is stale by definition (the process that owned it is gone). The
// terminal status is interrupted — the run did not fail, it was cut off.
func (s *SQLiteStore) MarkStaleRunsInterrupted(ctx context.Context) (int64, error) {
	now := unix(time.Now())
	res, err := s.db.ExecContext(ctx, `
		UPDATE run_journal
		SET status = ?, error = 'daemon_restart',
		    updated_at = ?, completed_at = ?
		WHERE status = ?`,
		string(RunInterrupted), now, now, string(RunRunning))
	if err != nil {
		return 0, fmt.Errorf("runlog mark stale runs interrupted: %w", err)
	}
	return res.RowsAffected()
}

// CompleteWithEvents commits all pending timeline events and the terminal run
// row in one SQLite transaction. The terminal UPDATE is conditional on the
// row still being running, so a late callback cannot overwrite an earlier
// terminal status. If either the event insert or terminal UPDATE fails, the
// transaction is rolled back and neither half is reported as committed.
func (s *SQLiteStore) CompleteWithEvents(ctx context.Context, runID string, status RunStatus, checkpoint, errMsg, toolSummary string, agg CompletionAggregates, events []RunEvent) error {
	runID = sanitizeRunlogText(runID, 128)
	checkpoint = sanitizeRunlogText(checkpoint, 4096)
	errMsg = sanitizeRunlogText(errMsg, maxEventMessageBytes)
	toolSummary = sanitizeRunlogText(toolSummary, 4096)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("runlog complete transaction %s: %w", runID, err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO run_events (run_id, ts, phase, level, message, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("runlog complete prepare events %s: %w", runID, err)
	}
	for _, ev := range events {
		ev.RunID = sanitizeRunlogText(ev.RunID, 128)
		if ev.RunID == "" {
			ev.RunID = runID
		} else if ev.RunID != runID {
			_ = stmt.Close()
			return fmt.Errorf("runlog complete event %s/%s: foreign run_id rejected", runID, ev.RunID)
		}
		ev.Phase = sanitizeRunlogText(ev.Phase, 128)
		ev.Level = sanitizeRunlogText(ev.Level, 32)
		if ev.Level == "" {
			ev.Level = "info"
		}
		ts := ev.Timestamp
		if ts == 0 {
			ts = unix(time.Now())
		}
		if _, err := stmt.ExecContext(ctx, ev.RunID, ts, ev.Phase, ev.Level,
			sanitizeEventMessage(ev.Message), sanitizeMetadataJSON(ev.MetadataJSON)); err != nil {
			_ = stmt.Close()
			return fmt.Errorf("runlog complete event %s/%s: %w", runID, ev.Phase, err)
		}
	}
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("runlog complete close events %s: %w", runID, err)
	}

	now := unix(time.Now())
	result, err := tx.ExecContext(ctx, `
		UPDATE run_journal
		SET status = ?, checkpoint = ?, error = ?, tool_summary = ?,
		    updated_at = ?, completed_at = ?,
		    first_feedback_ms = ?, max_silence_ms = ?,
		    stall_count = ?, steer_count = ?
		WHERE run_id = ? AND status = 'running'`,
		string(status), checkpoint, errMsg, toolSummary, now, now,
		agg.FirstFeedbackMs, agg.MaxSilenceMs, agg.StallCount, agg.SteerCount, runID)
	if err != nil {
		return fmt.Errorf("runlog complete terminal %s: %w", runID, err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("runlog complete terminal rows %s: %w", runID, err)
	} else if rows != 1 {
		return fmt.Errorf("runlog complete terminal %s: expected one running row, affected %d", runID, rows)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("runlog complete commit %s: %w", runID, err)
	}
	return nil
}

// CompleteWithAggregates is retained as the concrete SQLite capability for
// callers that have no pending timeline events. It uses the same atomic
// terminal transaction and exactly-once running-row guard.
func (s *SQLiteStore) CompleteWithAggregates(ctx context.Context, runID string, status RunStatus, checkpoint, errMsg, toolSummary string, agg CompletionAggregates) error {
	return s.CompleteWithEvents(ctx, runID, status, checkpoint, errMsg, toolSummary, agg, nil)
}

// Latest returns the most recent run for a chat/thread, ordered by started_at.
// This uses the original SELECT list for backward compatibility with old rows.
func (s *SQLiteStore) Latest(ctx context.Context, chatID int64, threadID int) (*RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT run_id, chat_id, thread_id, request_id, session_id, cwd, prompt,
		       status, checkpoint, tool_summary, error,
		       started_at, updated_at, completed_at
		FROM run_journal
		WHERE chat_id = ? AND thread_id = ?
		ORDER BY started_at DESC, rowid DESC
		LIMIT 1`, chatID, threadID)

	rec, err := scanRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runlog latest chat=%d thread=%d: %w", chatID, threadID, err)
	}
	return rec, nil
}

// ---------------------------------------------------------------------------
// New Store methods (observability)
// ---------------------------------------------------------------------------

// maxEventMessageBytes caps the defensive message size at the sink. Metadata
// is capped by observability.MaxEventMetadataBytes via sanitizeMetadataJSON.
const maxEventMessageBytes = 2048

var (
	runlogAPIKeyRE     = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9]{20,}|pk-[A-Za-z0-9]{20,}|sk_live_[A-Za-z0-9]+|sk_test_[A-Za-z0-9]+|AKIA[A-Z0-9]{16}|AIza[0-9A-Za-z_-]{35}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[0-9A-Za-z_-]+|xai-[A-Za-z0-9]{20,}|glpat-[A-Za-z0-9_-]{20,}|hf_[A-Za-z0-9]{20,}|npm_[A-Za-z0-9]{20,})`)
	runlogJWTRE        = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+`)
	runlogPrivateRE    = regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----`)
	runlogAuthRE       = regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|Basic)\s+)\S+`)
	runlogJSONSecretRE = regexp.MustCompile(`(?i)((?:"|')?(?:password|secret|api[_-]?key|client[_-]?secret|access[_-]?token|refresh[_-]?token|token)(?:"|')?\s*[:=]\s*["']?)[^"'\s,}\]]+`)
)

func redactRunlogSecrets(s string) string {
	s = runlogPrivateRE.ReplaceAllString(s, "[PRIVATE_KEY_BLOCK_REDACTED]")
	s = runlogAuthRE.ReplaceAllString(s, "$1[REDACTED]")
	s = runlogAPIKeyRE.ReplaceAllString(s, "[CREDENTIAL_REDACTED]")
	s = runlogJWTRE.ReplaceAllString(s, "[JWT_REDACTED]")
	return runlogJSONSecretRE.ReplaceAllString(s, "$1[CREDENTIAL_REDACTED]")
}

// sanitizeRunlogText applies redaction before control cleanup and truncation.
// It is deliberately local to the SQLite sink so direct Store callers cannot
// bypass the telemetry-path safety guarantees provided by the pipeline.
func sanitizeRunlogText(s string, maxRunes int) string {
	s = redactRunlogSecrets(strings.ToValidUTF8(s, "\uFFFD"))
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return s
}

func sanitizeRunlogMetadataValue(value any, depth int) any {
	if depth >= 4 {
		return "[metadata_depth_limit]"
	}
	switch v := value.(type) {
	case string:
		return sanitizeRunlogText(v, maxEventMessageBytes)
	case []any:
		if len(v) > 64 {
			v = v[:64]
		}
		out := make([]any, len(v))
		for i := range v {
			out[i] = sanitizeRunlogMetadataValue(v[i], depth+1)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		count := 0
		for key, item := range v {
			if count >= 64 {
				break
			}
			out[sanitizeRunlogText(key, 128)] = sanitizeRunlogMetadataValue(item, depth+1)
			count++
		}
		return out
	default:
		return value
	}
}

// sanitizeEventMessage bounds a timeline message defensively at the sink:
// invalid UTF-8 is replaced (the persisted TEXT must always be valid UTF-8),
// CR/LF and C0/C1/control characters are removed, and the result is
// truncated to at most maxEventMessageBytes at a rune boundary. Terminal
// events are sanitized like any other message — never dropped.
func sanitizeEventMessage(s string) string {
	// 1. Redact complete secrets before any size-changing operation, then
	// replace invalid UTF-8 so the stored TEXT is always valid.
	s = sanitizeRunlogText(s, maxEventMessageBytes)
	// 2. Remove control characters (C0: 0x00-0x1F, DEL: 0x7F, C1: 0x80-0x9F).
	//    Rune iteration after ToValidUTF8 guarantees multi-byte sequences are
	//    never mistaken for C1 control bytes.
	cleaned := make([]rune, 0, len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		cleaned = append(cleaned, r)
	}
	// 3. Exact cap: at most maxEventMessageBytes, cut at a rune boundary.
	total := 0
	for i, r := range cleaned {
		sz := utf8.RuneLen(r)
		if total+sz > maxEventMessageBytes {
			return string(cleaned[:i])
		}
		total += sz
	}
	return string(cleaned)
}

// sanitizeMetadataJSON enforces the metadata cap defensively at the sink:
// the value is parsed and RE-MARSHALED (normalizing escaping and rejecting
// anything invalid, including literal control characters) and the re-marshaled
// size must respect exactly observability.MaxEventMetadataBytes. Oversized or
// invalid metadata is replaced with a small valid fallback so the timeline
// never stores broken or oversized JSON.
func sanitizeMetadataJSON(s string) string {
	if s == "" {
		return "{}"
	}
	// Redact before the size check so a credential that crosses the metadata
	// boundary cannot survive as a sliced prefix.
	s = redactRunlogSecrets(s)
	// Cheap pre-check: oversized input can never be legit (the pipeline
	// caps metadata at MaxEventMetadataBytes), so fail fast to the fallback
	// instead of parsing a huge blob.
	if len(s) > observability.MaxEventMetadataBytes {
		return `{"metadata_truncated":true}`
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return "{}"
	}
	v = sanitizeRunlogMetadataValue(v, 0)
	// Re-marshal normalizes escaping (and rejects nothing further — the
	// parse already validated). The cap check runs on the RE-MARSHALED
	// bytes because escaping can inflate the output past the input length.
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	if len(b) > observability.MaxEventMetadataBytes {
		return `{"metadata_truncated":true}`
	}
	return string(b)
}

// RecordEvents persists multiple timeline events in one transaction.
func (s *SQLiteStore) RecordEvents(ctx context.Context, events []RunEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("runlog record_events begin: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO run_events (run_id, ts, phase, level, message, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("runlog record_events prepare: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, ev := range events {
		ev.RunID = sanitizeRunlogText(ev.RunID, 128)
		ev.Phase = sanitizeRunlogText(ev.Phase, 128)
		ev.Level = sanitizeRunlogText(ev.Level, 32)
		ts := ev.Timestamp
		if ts == 0 {
			ts = unix(time.Now())
		}
		level := ev.Level
		if level == "" {
			level = "info"
		}
		message := sanitizeEventMessage(ev.Message)
		metadata := sanitizeMetadataJSON(ev.MetadataJSON)
		if _, err := stmt.ExecContext(ctx, ev.RunID, ts, ev.Phase, level, message, metadata); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("runlog record_events %s/%s: %w", ev.RunID, ev.Phase, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("runlog record_events commit: %w", err)
	}
	return nil
}

// RecordEvent persists a single run event to the timeline.
// Best-effort: errors are logged, never block the caller.
func (s *SQLiteStore) RecordEvent(ctx context.Context, ev RunEvent) error {
	ev.RunID = sanitizeRunlogText(ev.RunID, 128)
	ev.Phase = sanitizeRunlogText(ev.Phase, 128)
	ev.Level = sanitizeRunlogText(ev.Level, 32)
	ts := ev.Timestamp
	if ts == 0 {
		ts = unix(time.Now())
	}
	level := ev.Level
	if level == "" {
		level = "info"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO run_events (run_id, ts, phase, level, message, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?)`,
		ev.RunID, ts, ev.Phase, level,
		sanitizeEventMessage(ev.Message), sanitizeMetadataJSON(ev.MetadataJSON))
	if err != nil {
		return fmt.Errorf("runlog record_event %s/%s: %w", ev.RunID, ev.Phase, err)
	}
	return nil
}

// ListEvents returns all events for a run, ordered by timestamp ascending.
func (s *SQLiteStore) ListEvents(ctx context.Context, runID string) ([]RunEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, ts, phase, level, message, metadata_json
		FROM run_events
		WHERE run_id = ?
		ORDER BY ts ASC, id ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("runlog list_events %s: %w", runID, err)
	}
	defer func() { _ = rows.Close() }()

	var events []RunEvent
	for rows.Next() {
		var ev RunEvent
		if err := rows.Scan(&ev.ID, &ev.RunID, &ev.Timestamp, &ev.Phase,
			&ev.Level, &ev.Message, &ev.MetadataJSON); err != nil {
			return nil, fmt.Errorf("runlog scan event %s: %w", runID, err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runlog list_events rows %s: %w", runID, err)
	}
	return events, nil
}

// GetRun returns a single run by RunID, or nil if not found.
func (s *SQLiteStore) GetRun(ctx context.Context, runID string) (*RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT run_id, chat_id, thread_id, request_id, session_id, cwd, prompt,
		       status, checkpoint, tool_summary, error,
		       started_at, updated_at, completed_at,
		       COALESCE(user_id, 0), COALESCE(entrypoint, ''), COALESCE(agent_name, ''),
		       COALESCE(provider, ''), COALESCE(model, ''), COALESCE(capability_profile, ''),
		       COALESCE(duration_ms, 0), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
		       COALESCE(cost_usd, 0), COALESCE(tool_count, 0),
		       COALESCE(error_class, ''), COALESCE(timeout_origin, ''),
		       COALESCE(used_fallback, 0), COALESCE(session_file, ''),
		       COALESCE(parent_run_id, ''),
		       COALESCE(inbound_message_id, 0), COALESCE(outbound_message_id, 0),
		       COALESCE(first_feedback_ms, 0), COALESCE(max_silence_ms, 0),
		       COALESCE(stall_count, 0), COALESCE(steer_count, 0)
		FROM run_journal
		WHERE run_id = ?`, runID)

	rec, err := scanRecordFull(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runlog get_run %s: %w", runID, err)
	}
	return rec, nil
}

// ListRuns returns recent runs matching optional filters.
// Limit caps the result set (default 20). When chatID is non-zero,
// results are scoped to that chat. Results are ordered by started_at DESC.
func (s *SQLiteStore) ListRuns(ctx context.Context, chatID int64, limit int) ([]RunRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}

	var rows *sql.Rows
	var err error

	if chatID != 0 {
		rows, err = s.db.QueryContext(ctx, `
			SELECT run_id, chat_id, thread_id, request_id, session_id, cwd, prompt,
			       status, checkpoint, tool_summary, error,
			       started_at, updated_at, completed_at,
			       COALESCE(user_id, 0), COALESCE(entrypoint, ''), COALESCE(agent_name, ''),
			       COALESCE(provider, ''), COALESCE(model, ''), COALESCE(capability_profile, ''),
			       COALESCE(duration_ms, 0), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
			       COALESCE(cost_usd, 0), COALESCE(tool_count, 0),
			       COALESCE(error_class, ''), COALESCE(timeout_origin, ''),
		       COALESCE(used_fallback, 0), COALESCE(session_file, ''),
		       COALESCE(parent_run_id, ''),
		       COALESCE(inbound_message_id, 0), COALESCE(outbound_message_id, 0),
		       COALESCE(first_feedback_ms, 0), COALESCE(max_silence_ms, 0),
		       COALESCE(stall_count, 0), COALESCE(steer_count, 0)
			FROM run_journal
			WHERE chat_id = ?
			ORDER BY started_at DESC
			LIMIT ?`, chatID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT run_id, chat_id, thread_id, request_id, session_id, cwd, prompt,
			       status, checkpoint, tool_summary, error,
			       started_at, updated_at, completed_at,
			       COALESCE(user_id, 0), COALESCE(entrypoint, ''), COALESCE(agent_name, ''),
			       COALESCE(provider, ''), COALESCE(model, ''), COALESCE(capability_profile, ''),
			       COALESCE(duration_ms, 0), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
			       COALESCE(cost_usd, 0), COALESCE(tool_count, 0),
			       COALESCE(error_class, ''), COALESCE(timeout_origin, ''),
		       COALESCE(used_fallback, 0), COALESCE(session_file, ''),
		       COALESCE(parent_run_id, ''),
		       COALESCE(inbound_message_id, 0), COALESCE(outbound_message_id, 0),
		       COALESCE(first_feedback_ms, 0), COALESCE(max_silence_ms, 0),
		       COALESCE(stall_count, 0), COALESCE(steer_count, 0)
			FROM run_journal
			ORDER BY started_at DESC
			LIMIT ?`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("runlog list_runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []RunRecord
	for rows.Next() {
		rec, err := scanRecordFull(rows)
		if err != nil {
			return nil, fmt.Errorf("runlog scan list_runs: %w", err)
		}
		if rec != nil {
			records = append(records, *rec)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runlog list_runs rows: %w", err)
	}
	return records, nil
}

// Prune deletes terminal runs older than opts.OlderThan and their events.
// Running runs are never deleted.
func (s *SQLiteStore) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	cutoff := unix(opts.OlderThan)
	if cutoff <= 0 {
		return PruneResult{}, fmt.Errorf("runlog prune: invalid OlderThan %v", opts.OlderThan)
	}

	var result PruneResult
	countQuery := `
		SELECT
			(SELECT COUNT(*) FROM run_journal
			 WHERE started_at < ? AND status != ?) AS runs,
			(SELECT COUNT(*) FROM run_events
			 WHERE run_id IN (
				SELECT run_id FROM run_journal
				WHERE started_at < ? AND status != ?
			 )) AS events`
	if err := s.db.QueryRowContext(ctx, countQuery, cutoff, string(RunRunning), cutoff, string(RunRunning)).
		Scan(&result.RunsDeleted, &result.EventsDeleted); err != nil {
		return PruneResult{}, fmt.Errorf("runlog prune count: %w", err)
	}
	if opts.DryRun || result.RunsDeleted == 0 {
		return result, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PruneResult{}, fmt.Errorf("runlog prune begin: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM run_events
		WHERE run_id IN (
			SELECT run_id FROM run_journal
			WHERE started_at < ? AND status != ?
		)`, cutoff, string(RunRunning)); err != nil {
		_ = tx.Rollback()
		return PruneResult{}, fmt.Errorf("runlog prune delete events: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM run_journal
		WHERE started_at < ? AND status != ?`, cutoff, string(RunRunning)); err != nil {
		_ = tx.Rollback()
		return PruneResult{}, fmt.Errorf("runlog prune delete runs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return PruneResult{}, fmt.Errorf("runlog prune commit: %w", err)
	}
	return result, nil
}

// Close releases the database connection.
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// GetLastOutboundMessage returns the chat_id, thread_id, and outbound_message_id
// of the most recent run that has a non-zero outbound_message_id for the given
// session_file. Returns chatID=0, threadID=0, messageID=0 if not found.
func (s *SQLiteStore) GetLastOutboundMessage(ctx context.Context, sessionFile string) (int64, int, int64, error) {
	if sessionFile == "" {
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

// ---------------------------------------------------------------------------
// Scanner helpers
// ---------------------------------------------------------------------------

type recordScanner interface {
	Scan(dest ...any) error
}

// scanRecord reads the base (pre-migration) column set.
// Used by Latest for backward compatibility.
func scanRecord(row recordScanner) (*RunRecord, error) {
	var r RunRecord
	var status string
	var startedAt, updatedAt, completedAt int64
	err := row.Scan(&r.RunID, &r.ChatID, &r.ThreadID, &r.RequestID,
		&r.SessionID, &r.CWD, &r.Prompt,
		&status, &r.Checkpoint, &r.ToolSummary, &r.Error,
		&startedAt, &updatedAt, &completedAt)
	if err != nil {
		return nil, err
	}
	r.Status = RunStatus(status)
	r.StartedAt = fromUnix(startedAt)
	r.UpdatedAt = fromUnix(updatedAt)
	r.CompletedAt = fromUnix(completedAt)
	return &r, nil
}

// scanRecordFull reads the full column set including extended observability
// fields (with COALESCE defaults for old rows).
func scanRecordFull(row recordScanner) (*RunRecord, error) {
	var r RunRecord
	var status string
	var startedAt, updatedAt, completedAt int64
	var usedFallback int64
	err := row.Scan(
		&r.RunID, &r.ChatID, &r.ThreadID, &r.RequestID,
		&r.SessionID, &r.CWD, &r.Prompt,
		&status, &r.Checkpoint, &r.ToolSummary, &r.Error,
		&startedAt, &updatedAt, &completedAt,
		&r.UserID, &r.EntryPoint, &r.AgentName,
		&r.Provider, &r.Model, &r.CapabilityProfile,
		&r.DurationMs, &r.InputTokens, &r.OutputTokens,
		&r.CostUSD, &r.ToolCount,
		&r.ErrorClass, &r.TimeoutOrigin,
		&usedFallback, &r.SessionFile,
		&r.ParentRunID,
		&r.InboundMessageID, &r.OutboundMessageID,
		&r.FirstFeedbackMs, &r.MaxSilenceMs,
		&r.StallCount, &r.SteerCount)
	if err != nil {
		return nil, err
	}
	r.Status = RunStatus(status)
	r.UsedFallback = usedFallback != 0
	r.StartedAt = fromUnix(startedAt)
	r.UpdatedAt = fromUnix(updatedAt)
	r.CompletedAt = fromUnix(completedAt)
	return &r, nil
}

// ---------------------------------------------------------------------------
// Time helpers
// ---------------------------------------------------------------------------

func unix(t time.Time) int64 { return t.Unix() }

func fromUnix(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}
