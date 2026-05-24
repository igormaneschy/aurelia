package planning

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// RunMigrations creates planning_state and planning_offer tables and indexes
// idempotently. Safe to call multiple times.
func RunMigrations(db *sql.DB) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS planning_state (
		chat_id INTEGER NOT NULL,
		thread_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		version INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL,
		phase TEXT NOT NULL,
		cwd TEXT,
		project_ctx TEXT,
		materialized TEXT,
		last_handoff_error TEXT,
		handoff_started_at INTEGER,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (chat_id, thread_id, user_id)
	);

	CREATE INDEX IF NOT EXISTS idx_planning_state_user ON planning_state(user_id);
	CREATE INDEX IF NOT EXISTS idx_planning_state_updated ON planning_state(updated_at);

	CREATE TABLE IF NOT EXISTS planning_offer (
		chat_id INTEGER NOT NULL,
		thread_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		intent_hash TEXT NOT NULL,
		offered_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		PRIMARY KEY (chat_id, thread_id, user_id, intent_hash)
	);

	CREATE INDEX IF NOT EXISTS idx_planning_offer_expires ON planning_offer(expires_at);
	`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("run planning migrations: %w", err)
	}
	return nil
}
