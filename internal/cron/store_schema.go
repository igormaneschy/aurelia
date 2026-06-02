package cron

import (
	"fmt"
	"strings"
)

func (s *SQLiteCronStore) initialize() error {
	query := `
	CREATE TABLE IF NOT EXISTS cron_jobs (
		id TEXT PRIMARY KEY,
		owner_user_id TEXT NOT NULL,
		target_chat_id INTEGER NOT NULL,
		target_thread_id INTEGER NOT NULL DEFAULT 0,
		cwd TEXT NOT NULL DEFAULT '',
		agent_name TEXT NOT NULL DEFAULT '',
		timezone TEXT DEFAULT '',
		schedule_type TEXT NOT NULL,
		cron_expr TEXT NOT NULL DEFAULT '',
		run_at DATETIME,
		prompt TEXT NOT NULL,
		active INTEGER NOT NULL DEFAULT 1,
		last_run_at DATETIME,
		next_run_at DATETIME,
		last_status TEXT NOT NULL DEFAULT 'idle',
		last_error TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS cron_executions (
		id TEXT PRIMARY KEY,
		job_id TEXT NOT NULL,
		started_at DATETIME NOT NULL,
		finished_at DATETIME,
		status TEXT NOT NULL,
		output_summary TEXT NOT NULL DEFAULT '',
		error_message TEXT NOT NULL DEFAULT ''
	);
	`
	_, err := s.db.Exec(query)
	if err != nil {
		return fmt.Errorf("initialize cron schema: %w", err)
	}

	// Migration: add columns if missing (safe to re-run).
	// On fresh installs, CREATE TABLE already includes these columns and the
	// ALTER will fail with "duplicate column" — which is caught and ignored.
	// This pattern avoids duplicating column definitions across two code paths
	// (CREATE vs ALTER) and keeps the full schema visible in the CREATE TABLE
	// while remaining compatible with existing databases.
	for _, col := range []string{
		"ALTER TABLE cron_executions ADD COLUMN session_id TEXT DEFAULT ''",
		"ALTER TABLE cron_executions ADD COLUMN cost_usd REAL DEFAULT 0",
		"ALTER TABLE cron_executions ADD COLUMN tokens_used INTEGER DEFAULT 0",
		"ALTER TABLE cron_jobs ADD COLUMN target_thread_id INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE cron_jobs ADD COLUMN cwd TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE cron_jobs ADD COLUMN agent_name TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE cron_jobs ADD COLUMN timezone TEXT DEFAULT ''",
	} {
		_, err := s.db.Exec(col)
		if err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migration: %w", err)
		}
	}

	// Index for ListDueJobs query
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cron_jobs_due ON cron_jobs(active, next_run_at)`)
	if err != nil {
		return fmt.Errorf("create due jobs index: %w", err)
	}

	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cron_jobs_chat ON cron_jobs(target_chat_id)`)
	if err != nil {
		return fmt.Errorf("create chat jobs index: %w", err)
	}

	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cron_jobs_owner ON cron_jobs(owner_user_id)`)
	if err != nil {
		return fmt.Errorf("create owner jobs index: %w", err)
	}

	// Backfill cwd from prompt text for existing jobs.
	// New jobs created via CreateJob already have cwd populated by
	// extractCwdFromPrompt at creation time, so the WHERE cwd = ''
	// filter naturally skips them — no risk of double-extraction.
	return s.backfillCronJobCwd()
}

func (s *SQLiteCronStore) backfillCronJobCwd() error {
	rows, err := s.db.Query(`SELECT id, prompt FROM cron_jobs WHERE cwd = ''`)
	if err != nil {
		return fmt.Errorf("select cron jobs for cwd backfill: %w", err)
	}
	defer func() { _ = rows.Close() }()

	updates := make(map[string]string)
	for rows.Next() {
		var id string
		var prompt string
		if err := rows.Scan(&id, &prompt); err != nil {
			return fmt.Errorf("scan cron job for cwd backfill: %w", err)
		}
		if cwd := extractCwdFromPrompt(prompt); cwd != "" {
			updates[id] = cwd
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate cron jobs for cwd backfill: %w", err)
	}

	for id, cwd := range updates {
		if _, err := s.db.Exec(`UPDATE cron_jobs SET cwd = ? WHERE id = ?`, cwd, id); err != nil {
			return fmt.Errorf("backfill cwd for cron job %q: %w", id, err)
		}
	}
	return nil
}
