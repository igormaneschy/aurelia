package planning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/igormaneschy/aurelia/internal/session"
)

// ErrConflict is returned by Save when the state has been modified since it was last read.
var ErrConflict = errors.New("planning state conflict")

// SQLiteStore implements Store and OfferStore in SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a store backed by the given DB (migrations must already be applied).
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// Verify at compile time that SQLiteStore implements both interfaces.
var _ Store = (*SQLiteStore)(nil)
var _ OfferStore = (*SQLiteStore)(nil)

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...interface{}) error
}

// scanStateRow scans a planning_state row (without PK columns) into a State.
// Expected columns: version, status, phase, cwd, project_ctx, materialized,
// last_handoff_error, handoff_started_at, created_at, updated_at.
func scanStateRow(sc scanner, st *State) error {
	var projectCtx, materialized sql.NullString
	var handoffAt sql.NullInt64
	var createdUnix, updatedUnix int64

	err := sc.Scan(
		&st.Version, &st.Status, &st.Phase, &st.CWD,
		&projectCtx, &materialized, &st.LastHandoffError,
		&handoffAt, &createdUnix, &updatedUnix,
	)
	if err != nil {
		return err
	}

	if projectCtx.Valid {
		var pc ProjectContext
		if err := json.Unmarshal([]byte(projectCtx.String), &pc); err != nil {
			return fmt.Errorf("unmarshal project_ctx: %w", err)
		}
		st.ProjectCtx = &pc
	}
	if materialized.Valid {
		var arts []Artifact
		if err := json.Unmarshal([]byte(materialized.String), &arts); err != nil {
			return fmt.Errorf("unmarshal materialized: %w", err)
		}
		st.Materialized = arts
	}
	if handoffAt.Valid {
		t := time.Unix(handoffAt.Int64, 0)
		st.HandoffStartedAt = &t
	}
	st.CreatedAt = time.Unix(createdUnix, 0)
	st.UpdatedAt = time.Unix(updatedUnix, 0)
	return nil
}

// Get retrieves a planning state by session key.
// Returns nil, nil if the key does not exist.
func (s *SQLiteStore) Get(ctx context.Context, key session.SessionKey) (*State, error) {
	var state State
	state.Key = key

	row := s.db.QueryRowContext(ctx, `
		SELECT version, status, phase, cwd, project_ctx, materialized,
		       last_handoff_error, handoff_started_at, created_at, updated_at
		FROM planning_state
		WHERE chat_id = ? AND thread_id = ? AND user_id = ?`,
		key.ChatID, key.ThreadID, key.UserID,
	)
	if err := scanStateRow(row, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get planning state: %w", err)
	}
	return &state, nil
}

// Save persists a planning state with optimistic locking.
// Returns ErrConflict if the state has been modified since last read.
func (s *SQLiteStore) Save(ctx context.Context, state *State) error {
	now := time.Now()

	projectCtxJSON, err := json.Marshal(state.ProjectCtx)
	if err != nil {
		return fmt.Errorf("marshal project_ctx: %w", err)
	}
	materializedJSON, err := json.Marshal(state.Materialized)
	if err != nil {
		return fmt.Errorf("marshal materialized: %w", err)
	}
	handoffAt := marshalTimePtr(state.HandoffStartedAt)

	// Try optimistic UPDATE first.
	res, err := s.db.ExecContext(ctx, `
		UPDATE planning_state
		SET version = version + 1,
		    status = ?, phase = ?, cwd = ?,
		    project_ctx = ?, materialized = ?,
		    last_handoff_error = ?, handoff_started_at = ?,
		    updated_at = ?
		WHERE chat_id = ? AND thread_id = ? AND user_id = ? AND version = ?`,
		state.Status, state.Phase, state.CWD,
		string(projectCtxJSON), string(materializedJSON),
		state.LastHandoffError, handoffAt,
		now.Unix(),
		state.Key.ChatID, state.Key.ThreadID, state.Key.UserID,
		state.Version,
	)
	if err != nil {
		return fmt.Errorf("update planning state: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows > 0 {
		state.Version++
		state.UpdatedAt = now
		if state.CreatedAt.IsZero() {
			state.CreatedAt = now
		}
		return nil
	}

	// No rows — either conflict or new record.
	return s.saveInsert(ctx, state, now, projectCtxJSON, materializedJSON, handoffAt)
}

// saveInsert handles INSERT for a new planning state or returns ErrConflict.
func (s *SQLiteStore) saveInsert(ctx context.Context, state *State, now time.Time,
	projectCtxJSON, materializedJSON []byte, handoffAt *int64) error {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM planning_state
		WHERE chat_id=? AND thread_id=? AND user_id=?)`,
		state.Key.ChatID, state.Key.ThreadID, state.Key.UserID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check existing state: %w", err)
	}
	if exists {
		return ErrConflict
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO planning_state
		(chat_id, thread_id, user_id, version, status, phase, cwd,
		 project_ctx, materialized, last_handoff_error, handoff_started_at,
		 created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		state.Key.ChatID, state.Key.ThreadID, state.Key.UserID,
		state.Status, state.Phase, state.CWD,
		string(projectCtxJSON), string(materializedJSON),
		state.LastHandoffError, handoffAt,
		now.Unix(), now.Unix(),
	)
	if err != nil {
		return fmt.Errorf("insert planning state: %w", err)
	}

	state.Version = 1
	state.CreatedAt = now
	state.UpdatedAt = now
	return nil
}

// Delete removes a planning state by session key. Idempotent.
func (s *SQLiteStore) Delete(ctx context.Context, key session.SessionKey) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM planning_state
		WHERE chat_id=? AND thread_id=? AND user_id=?`,
		key.ChatID, key.ThreadID, key.UserID,
	)
	if err != nil {
		return fmt.Errorf("delete planning state: %w", err)
	}
	return nil
}

// ListByUser returns all planning states for a given user.
func (s *SQLiteStore) ListByUser(ctx context.Context, userID int64) ([]State, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT chat_id, thread_id, user_id,
		       version, status, phase, cwd,
		       project_ctx, materialized, last_handoff_error,
		       handoff_started_at, created_at, updated_at
		FROM planning_state
		WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("list planning states: %w", err)
	}
	defer rows.Close()

	var states []State
	for rows.Next() {
		var st State
		var projectCtx, materialized sql.NullString
		var handoffAt sql.NullInt64
		var createdUnix, updatedUnix int64

		err := rows.Scan(
			&st.Key.ChatID, &st.Key.ThreadID, &st.Key.UserID,
			&st.Version, &st.Status, &st.Phase, &st.CWD,
			&projectCtx, &materialized, &st.LastHandoffError,
			&handoffAt, &createdUnix, &updatedUnix,
		)
		if err != nil {
			return nil, fmt.Errorf("scan planning state: %w", err)
		}

		if projectCtx.Valid {
			var pc ProjectContext
			if err := json.Unmarshal([]byte(projectCtx.String), &pc); err != nil {
				return nil, fmt.Errorf("unmarshal project_ctx: %w", err)
			}
			st.ProjectCtx = &pc
		}
		if materialized.Valid {
			var arts []Artifact
			if err := json.Unmarshal([]byte(materialized.String), &arts); err != nil {
				return nil, fmt.Errorf("unmarshal materialized: %w", err)
			}
			st.Materialized = arts
		}
		if handoffAt.Valid {
			t := time.Unix(handoffAt.Int64, 0)
			st.HandoffStartedAt = &t
		}
		st.CreatedAt = time.Unix(createdUnix, 0)
		st.UpdatedAt = time.Unix(updatedUnix, 0)

		states = append(states, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate planning states: %w", err)
	}
	if states == nil {
		return []State{}, nil
	}
	return states, nil
}

// GC removes old planning states and expired offers.
func (s *SQLiteStore) GC(ctx context.Context, maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge).Unix()
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM planning_state WHERE updated_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("gc planning states: %w", err)
	}
	return s.GCOffers(ctx)
}

// RecordOffer records that an offer was made. Returns true if this is a new offer
// or the previous offer has expired.
func (s *SQLiteStore) RecordOffer(ctx context.Context, key session.SessionKey, intentHash string, ttl time.Duration) (bool, error) {
	now := time.Now()
	expiresAt := now.Add(ttl)

	_, err := s.db.ExecContext(ctx, `
		DELETE FROM planning_offer
		WHERE chat_id=? AND thread_id=? AND user_id=? AND intent_hash=? AND expires_at < ?`,
		key.ChatID, key.ThreadID, key.UserID, intentHash, now.Unix(),
	)
	if err != nil {
		return false, fmt.Errorf("delete expired offers: %w", err)
	}

	var count int
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM planning_offer
		WHERE chat_id=? AND thread_id=? AND user_id=? AND intent_hash=?`,
		key.ChatID, key.ThreadID, key.UserID, intentHash,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check existing offer: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO planning_offer (chat_id, thread_id, user_id, intent_hash, offered_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		key.ChatID, key.ThreadID, key.UserID, intentHash, now.Unix(), expiresAt.Unix(),
	)
	if err != nil {
		return false, fmt.Errorf("insert offer: %w", err)
	}
	return true, nil
}

// HasRecentOffer checks if there's an unexpired offer for this key+intent.
func (s *SQLiteStore) HasRecentOffer(ctx context.Context, key session.SessionKey, intentHash string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM planning_offer
		WHERE chat_id=? AND thread_id=? AND user_id=? AND intent_hash=? AND expires_at > ?`,
		key.ChatID, key.ThreadID, key.UserID, intentHash, time.Now().Unix(),
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check recent offer: %w", err)
	}
	return count > 0, nil
}

// GCOffers removes expired offers.
func (s *SQLiteStore) GCOffers(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM planning_offer WHERE expires_at < ?`, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("gc offers: %w", err)
	}
	return nil
}

// Close releases the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// marshalTimePtr converts a *time.Time to a nullable int64 for SQL storage.
func marshalTimePtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	unix := t.Unix()
	return &unix
}
