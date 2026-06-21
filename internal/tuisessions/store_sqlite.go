package tuisessions

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore persists TUI session metadata in SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens dbPath and ensures the session schema exists.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=ON"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open tui sessions sqlite store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &SQLiteStore{db: db}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) initialize() error {
	query := `
	CREATE TABLE IF NOT EXISTS tui_sessions (
		chat_id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		last_used_at INTEGER NOT NULL
	);
	`
	if _, err := s.db.Exec(query); err != nil {
		return fmt.Errorf("initialize tui sessions schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) List(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT chat_id, name, created_at, last_used_at
		FROM tui_sessions
		ORDER BY last_used_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tui sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("list tui sessions: scan: %w", err)
		}
		result = append(result, *sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tui sessions: rows: %w", err)
	}
	return result, nil
}

func (s *SQLiteStore) Get(ctx context.Context, chatID int64) (*Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT chat_id, name, created_at, last_used_at
		FROM tui_sessions
		WHERE chat_id = ?`, chatID)
	sess, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tui session %d: %w", chatID, err)
	}
	return sess, nil
}

func (s *SQLiteStore) Create(ctx context.Context, chatID int64, name string) (*Session, error) {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tui_sessions (chat_id, name, created_at, last_used_at)
		VALUES (?, ?, ?, ?)`,
		chatID, name, now.UnixNano(), now.UnixNano())
	if err != nil {
		// SQLite returns "constraint failed: UNIQUE constraint" on duplicate PK.
		if isUniqueConstraintErr(err) {
			return nil, ErrSessionExists
		}
		return nil, fmt.Errorf("create tui session %d: %w", chatID, err)
	}
	return &Session{
		ChatID:     chatID,
		Name:       name,
		CreatedAt:  now,
		LastUsedAt: now,
	}, nil
}

func (s *SQLiteStore) Rename(ctx context.Context, chatID int64, name string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE tui_sessions SET name = ? WHERE chat_id = ?`,
		name, chatID)
	if err != nil {
		return fmt.Errorf("rename tui session %d: %w", chatID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *SQLiteStore) Touch(ctx context.Context, chatID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE tui_sessions SET last_used_at = ? WHERE chat_id = ?`,
		time.Now().UnixNano(), chatID)
	if err != nil {
		return fmt.Errorf("touch tui session %d: %w", chatID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *SQLiteStore) Delete(ctx context.Context, chatID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tui_sessions WHERE chat_id = ?`, chatID)
	if err != nil {
		return fmt.Errorf("delete tui session %d: %w", chatID, err)
	}
	return nil
}

// NextChatID returns the next available ChatID for a new session.
// It queries MIN(chat_id) from the database and returns one below it.
// Returns -1 when the table is empty (caller should use a default).
func (s *SQLiteStore) NextChatID(ctx context.Context) (int64, error) {
	var minID sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MIN(chat_id) FROM tui_sessions`).Scan(&minID)
	if err != nil {
		return 0, fmt.Errorf("next chat id query: %w", err)
	}
	if !minID.Valid {
		return -1, nil
	}
	return minID.Int64 - 1, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

type sessionScanner interface {
	Scan(dest ...any) error
}

func scanSession(row sessionScanner) (*Session, error) {
	var sess Session
	var createdAt, lastUsedAt int64
	if err := row.Scan(&sess.ChatID, &sess.Name, &createdAt, &lastUsedAt); err != nil {
		return nil, err
	}
	sess.CreatedAt = time.Unix(0, createdAt)
	sess.LastUsedAt = time.Unix(0, lastUsedAt)
	return &sess, nil
}

// isUniqueConstraintErr returns true if err is a SQLite UNIQUE constraint
// violation. modernc.org/sqlite returns an error whose message contains
// "UNIQUE constraint failed".
func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed: UNIQUE")
}
