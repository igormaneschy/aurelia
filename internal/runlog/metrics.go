package runlog

import (
	"context"
	"fmt"
	"time"
)

// MetricsResult holds aggregate operational metrics over a time window.
type MetricsResult struct {
	// Time window
	WindowStart time.Time
	WindowEnd   time.Time

	// Run counts
	RunsTotal       int
	RunsCompleted   int
	RunsFailed      int
	RunsTimedOut    int
	RunsCanceled    int
	RunsRunning     int
	SuccessRate     float64 // 0-100
	FallbackCount   int

	// Token and cost
	TokensInputTotal  int64
	TokensOutputTotal int64
	CostUSDTotal      float64

	// Duration (milliseconds)
	DurationP50Ms float64
	DurationP95Ms float64

	// Breakdowns
	ProviderBreakdown []BreakdownItem
	ModelBreakdown    []BreakdownItem
	EntrypointBreakdown []BreakdownItem

	// Cron (if available)
	CronRunsTotal    int
	CronSuccessRate  float64
}

// BreakdownItem is a metric breakdown key-value pair.
type BreakdownItem struct {
	Key   string
	Count int
	Pct   float64
}

// MetricsFilter controls the time window for the metrics query.
type MetricsFilter struct {
	Since time.Time
	Until time.Time
}

// Metrics queries aggregate metrics from run_journal over a time window.
// Returns a MetricsResult or an error.
func (s *SQLiteStore) Metrics(ctx context.Context, filter MetricsFilter) (*MetricsResult, error) {
	m := &MetricsResult{
		WindowStart: filter.Since,
		WindowEnd:   filter.Until,
	}

	// ── Run counts by status ──
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0) AS completed,
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) AS failed,
			COALESCE(SUM(CASE WHEN status = 'timed_out' THEN 1 ELSE 0 END), 0) AS timed_out,
			COALESCE(SUM(CASE WHEN status = 'canceled' THEN 1 ELSE 0 END), 0) AS canceled,
			COALESCE(SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END), 0) AS running,
			COALESCE(SUM(used_fallback), 0) AS fallbacks
		FROM run_journal
		WHERE started_at >= ? AND started_at < ?`,
		filter.Since.Unix(), filter.Until.Unix())

	if err := row.Scan(&m.RunsTotal, &m.RunsCompleted, &m.RunsFailed,
		&m.RunsTimedOut, &m.RunsCanceled, &m.RunsRunning, &m.FallbackCount); err != nil {
		return nil, fmt.Errorf("metrics run counts: %w", err)
	}

	// Success rate.
	if m.RunsTotal > 0 {
		m.SuccessRate = float64(m.RunsCompleted) / float64(m.RunsTotal) * 100
	}

	// ── Token and cost totals ──
	row = s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cost_usd), 0)
		FROM run_journal
		WHERE started_at >= ? AND started_at < ?`,
		filter.Since.Unix(), filter.Until.Unix())

	if err := row.Scan(&m.TokensInputTotal, &m.TokensOutputTotal, &m.CostUSDTotal); err != nil {
		return nil, fmt.Errorf("metrics token/cost totals: %w", err)
	}

	// ── Duration percentiles ──
	// SQLite percentile approximation: use subquery with completed runs
	// that have non-zero duration.
	durationRow := s.db.QueryRowContext(ctx, `
		SELECT
			AVG(duration_ms) AS avg_dur,
			COALESCE(
				(SELECT duration_ms FROM run_journal
				 WHERE started_at >= ? AND started_at < ?
				   AND duration_ms > 0 AND status IN ('completed', 'failed', 'timed_out')
				 ORDER BY duration_ms ASC
				 LIMIT 1 OFFSET (SELECT MAX(1, COUNT(*)/2 - 1) FROM run_journal
				   WHERE started_at >= ? AND started_at < ?
				     AND duration_ms > 0 AND status IN ('completed', 'failed', 'timed_out'))),
			0) AS p50,
			COALESCE(
				(SELECT duration_ms FROM run_journal
				 WHERE started_at >= ? AND started_at < ?
				   AND duration_ms > 0 AND status IN ('completed', 'failed', 'timed_out')
				 ORDER BY duration_ms ASC
				 LIMIT 1 OFFSET (SELECT MAX(1, (COUNT(*) * 95 / 100) - 1) FROM run_journal
				   WHERE started_at >= ? AND started_at < ?
				     AND duration_ms > 0 AND status IN ('completed', 'failed', 'timed_out'))),
			0) AS p95
		FROM run_journal
		WHERE started_at >= ? AND started_at < ?
		  AND duration_ms > 0 AND status IN ('completed', 'failed', 'timed_out')`,
		filter.Since.Unix(), filter.Until.Unix(),
		filter.Since.Unix(), filter.Until.Unix(),
		filter.Since.Unix(), filter.Until.Unix(),
		filter.Since.Unix(), filter.Until.Unix(),
		filter.Since.Unix(), filter.Until.Unix())

	var avgDuration float64
	if err := durationRow.Scan(&avgDuration, &m.DurationP50Ms, &m.DurationP95Ms); err != nil {
		return nil, fmt.Errorf("metrics duration percentiles: %w", err)
	}

	// ── Provider breakdown ──
	m.ProviderBreakdown = s.breakdown(ctx, "provider", filter)

	// ── Model breakdown ──
	m.ModelBreakdown = s.breakdown(ctx, "model", filter)

	// ── Entrypoint breakdown ──
	m.EntrypointBreakdown = s.breakdown(ctx, "entrypoint", filter)

	// ── Cron metrics ──
	// Check if cron_executions table exists; if so, query it.
	var hasCron int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cron_executions'`).Scan(&hasCron); err == nil && hasCron > 0 {
		cronRow := s.db.QueryRowContext(ctx, `
			SELECT
				COUNT(*) AS total,
				COALESCE(SUM(CASE WHEN status = 'completed' OR error = '' THEN 1 ELSE 0 END), 0) AS success,
				COALESCE(SUM(CASE WHEN status = 'failed' OR error != '' THEN 1 ELSE 0 END), 0) AS failed
			FROM cron_executions
			WHERE started_at >= ? AND started_at < ?`,
			filter.Since.Unix(), filter.Until.Unix())

		var cronSuccess, cronFailed int
		if err := cronRow.Scan(&m.CronRunsTotal, &cronSuccess, &cronFailed); err == nil {
			if m.CronRunsTotal > 0 {
				m.CronSuccessRate = float64(cronSuccess) / float64(m.CronRunsTotal) * 100
			}
		}
	}

	return m, nil
}

// breakdown runs a GROUP BY query for a given column and returns the results
// as BreakdownItem slices, ordered by count descending.
func (s *SQLiteStore) breakdown(ctx context.Context, column string, filter MetricsFilter) []BreakdownItem {
	// Use COALESCE to handle empty/null values
	colExpr := fmt.Sprintf("COALESCE(%s, 'unknown')", column)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s AS key, COUNT(*) AS count
		FROM run_journal
		WHERE started_at >= ? AND started_at < ?
		GROUP BY key
		ORDER BY count DESC`, colExpr),
		filter.Since.Unix(), filter.Until.Unix())
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	total := 0
	var items []BreakdownItem
	for rows.Next() {
		var item BreakdownItem
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			continue
		}
		total += item.Count
		items = append(items, item)
	}
	if total > 0 && len(items) > 0 {
		for i := range items {
			items[i].Pct = float64(items[i].Count) / float64(total) * 100
		}
	}
	return items
}
