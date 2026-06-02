package runlog

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

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
		"CREATE INDEX IF NOT EXISTS idx_run_journal_session_outbound ON run_journal(session_file, outbound_message_id DESC)",
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

	args = append(args, update.RunID)
	q := fmt.Sprintf("UPDATE run_journal SET %s WHERE run_id = ?", sets)
	_, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("runlog update %s: %w", update.RunID, err)
	}
	return nil
}

// Complete marks a run with a terminal status and optional checkpoint/error.
// Also sets completed_at and updates updated_at.
func (s *SQLiteStore) Complete(ctx context.Context, runID string, status RunStatus, checkpoint, errMsg string) error {
	now := unix(time.Now())
	_, err := s.db.ExecContext(ctx, `
		UPDATE run_journal
		SET status = ?, checkpoint = ?, error = ?,
		    updated_at = ?, completed_at = ?
		WHERE run_id = ?`,
		string(status), checkpoint, errMsg, now, now, runID)
	if err != nil {
		return fmt.Errorf("runlog complete %s: %w", runID, err)
	}
	return nil
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

// RecordEvent persists a single run event to the timeline.
// Best-effort: errors are logged, never block the caller.
func (s *SQLiteStore) RecordEvent(ctx context.Context, ev RunEvent) error {
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
		ev.RunID, ts, ev.Phase, level, ev.Message, ev.MetadataJSON)
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
		       COALESCE(inbound_message_id, 0), COALESCE(outbound_message_id, 0)
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
			       COALESCE(inbound_message_id, 0), COALESCE(outbound_message_id, 0)
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
			       COALESCE(inbound_message_id, 0), COALESCE(outbound_message_id, 0)
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
		ORDER BY started_at DESC
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
		&r.InboundMessageID, &r.OutboundMessageID)
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
