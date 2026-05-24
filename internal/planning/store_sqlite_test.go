package planning

import (
	"database/sql"
	"testing"
)

// NewTestDB creates an in-memory SQLite database with migrations applied.
func NewTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	return db
}

// tableExists reports whether the named table exists in the database.
func tableExists(db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
		name,
	).Scan(&count)
	return count > 0, err
}

// indexExists reports whether the named index exists in the database.
func indexExists(db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?",
		name,
	).Scan(&count)
	return count > 0, err
}

// TestMigrations_Idempotent verifies running migrations twice doesn't error.
func TestMigrations_Idempotent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("first RunMigrations: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("second RunMigrations (must be idempotent): %v", err)
	}
}

// TestMigrations_TablesExist verifies tables and indexes are created.
func TestMigrations_TablesExist(t *testing.T) {
	db := NewTestDB(t)

	tables := []string{"planning_state", "planning_offer"}
	for _, name := range tables {
		ok, err := tableExists(db, name)
		if err != nil {
			t.Fatalf("check table %q: %v", name, err)
		}
		if !ok {
			t.Errorf("table %q does not exist after migration", name)
		}
	}

	indexes := []string{
		"idx_planning_state_user",
		"idx_planning_state_updated",
		"idx_planning_offer_expires",
	}
	for _, name := range indexes {
		ok, err := indexExists(db, name)
		if err != nil {
			t.Fatalf("check index %q: %v", name, err)
		}
		if !ok {
			t.Errorf("index %q does not exist after migration", name)
		}
	}
}

// TestNewTestDB verifies NewTestDB creates a working in-memory DB.
func TestNewTestDB(t *testing.T) {
	db := NewTestDB(t)

	// Verify we can insert into planning_state
	_, err := db.Exec(`
		INSERT INTO planning_state
		(chat_id, thread_id, user_id, status, phase, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		1, 2, 3, "active", "specify", 100, 100)
	if err != nil {
		t.Fatalf("insert into planning_state: %v", err)
	}

	// Verify we can insert into planning_offer
	_, err = db.Exec(`
		INSERT INTO planning_offer
		(chat_id, thread_id, user_id, intent_hash, offered_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		1, 2, 3, "abc123", 100, 200)
	if err != nil {
		t.Fatalf("insert into planning_offer: %v", err)
	}
}
