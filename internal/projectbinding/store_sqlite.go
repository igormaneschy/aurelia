package projectbinding

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore stores conversation project bindings in SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens dbPath and ensures the binding schema exists.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=ON"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open project binding sqlite store: %w", err)
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
	CREATE TABLE IF NOT EXISTS conversation_project_binding (
		chat_id INTEGER NOT NULL,
		thread_id INTEGER NOT NULL,
		cwd TEXT NOT NULL,
		project_slug TEXT NOT NULL,
		source TEXT NOT NULL,
		created_by INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		last_used_at INTEGER NOT NULL,
		PRIMARY KEY (chat_id, thread_id)
	);
	CREATE INDEX IF NOT EXISTS idx_project_binding_slug
	ON conversation_project_binding(project_slug);
	CREATE INDEX IF NOT EXISTS idx_project_binding_created_by
	ON conversation_project_binding(created_by);
	`
	if _, err := s.db.Exec(query); err != nil {
		return fmt.Errorf("initialize project binding schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Get(ctx context.Context, key ConversationKey) (*ProjectBinding, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT chat_id, thread_id, cwd, project_slug, source, created_by, created_at, updated_at, last_used_at
		FROM conversation_project_binding
		WHERE chat_id = ? AND thread_id = ?`, key.ChatID, key.ThreadID)
	binding, err := scanBinding(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project binding %s: %w", key, err)
	}
	return binding, nil
}

func (s *SQLiteStore) Resolve(ctx context.Context, key ConversationKey) (*ResolvedBinding, error) {
	if binding, err := s.Get(ctx, key); err != nil || binding != nil {
		if binding == nil {
			return nil, err
		}
		return &ResolvedBinding{Binding: binding, SourceKey: key}, err
	}
	if key.ThreadID == 0 {
		return nil, nil
	}
	groupKey := ConversationKey{ChatID: key.ChatID, ThreadID: 0}
	binding, err := s.Get(ctx, groupKey)
	if err != nil || binding == nil {
		return nil, err
	}
	return &ResolvedBinding{Binding: binding, Inherited: true, SourceKey: groupKey}, nil
}

func (s *SQLiteStore) Set(ctx context.Context, binding ProjectBinding) error {
	now := time.Now()
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	binding.UpdatedAt = now
	binding.LastUsedAt = now
	if binding.Source == "" {
		binding.Source = BindingManual
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conversation_project_binding
		(chat_id, thread_id, cwd, project_slug, source, created_by, created_at, updated_at, last_used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat_id, thread_id) DO UPDATE SET
			cwd = excluded.cwd,
			project_slug = excluded.project_slug,
			source = excluded.source,
			created_by = excluded.created_by,
			updated_at = excluded.updated_at,
			last_used_at = excluded.last_used_at`,
		binding.Key.ChatID, binding.Key.ThreadID, binding.CWD, binding.ProjectSlug,
		string(binding.Source), binding.CreatedBy, unix(binding.CreatedAt), unix(binding.UpdatedAt), unix(binding.LastUsedAt))
	if err != nil {
		return fmt.Errorf("set project binding %s cwd=%q: %w", binding.Key, binding.CWD, err)
	}
	return nil
}

func (s *SQLiteStore) Delete(ctx context.Context, key ConversationKey) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM conversation_project_binding WHERE chat_id = ? AND thread_id = ?`, key.ChatID, key.ThreadID); err != nil {
		return fmt.Errorf("delete project binding %s: %w", key, err)
	}
	return nil
}

func (s *SQLiteStore) Touch(ctx context.Context, key ConversationKey) error {
	_, err := s.db.ExecContext(ctx, `UPDATE conversation_project_binding SET last_used_at = ? WHERE chat_id = ? AND thread_id = ?`, unix(time.Now()), key.ChatID, key.ThreadID)
	if err != nil {
		return fmt.Errorf("touch project binding %s: %w", key, err)
	}
	return nil
}

// ListByUser returns the most recent unique project bindings created by userID,
// ordered by last_used_at DESC. Duplicate CWD paths are deduplicated — the most
// recently used entry for each path wins. Limit caps the returned slice.
func (s *SQLiteStore) ListByUser(ctx context.Context, userID int64, limit int) ([]ProjectBinding, error) {
	// Fetch a generous buffer to allow for deduplication
	rows, err := s.db.QueryContext(ctx, `
		SELECT chat_id, thread_id, cwd, project_slug, source, created_by, created_at, updated_at, last_used_at
		FROM conversation_project_binding
		WHERE created_by = ?
		ORDER BY last_used_at DESC
		LIMIT ?`, userID, max(limit*4, 50))
	if err != nil {
		return nil, fmt.Errorf("list project bindings by user %d: %w", userID, err)
	}
	defer func() { _ = rows.Close() }()

	var result []ProjectBinding
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("list project bindings by user %d: scan: %w", userID, err)
		}
		if _, ok := seen[b.CWD]; ok {
			continue
		}
		seen[b.CWD] = struct{}{}
		result = append(result, *b)
		if len(result) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list project bindings by user %d: rows: %w", userID, err)
	}
	return result, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

type bindingScanner interface {
	Scan(dest ...any) error
}

func scanBinding(row bindingScanner) (*ProjectBinding, error) {
	var b ProjectBinding
	var source string
	var createdAt, updatedAt, lastUsedAt int64
	if err := row.Scan(&b.Key.ChatID, &b.Key.ThreadID, &b.CWD, &b.ProjectSlug, &source, &b.CreatedBy, &createdAt, &updatedAt, &lastUsedAt); err != nil {
		return nil, err
	}
	b.Source = BindingSource(source)
	b.CreatedAt = fromUnix(createdAt)
	b.UpdatedAt = fromUnix(updatedAt)
	b.LastUsedAt = fromUnix(lastUsedAt)
	return &b, nil
}

func unix(t time.Time) int64 { return t.Unix() }

func fromUnix(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}
