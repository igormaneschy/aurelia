package runlog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/pkg/idgen"
)

// TestSQLiteStore_LongSessionAggregates covers A4 persistence: the four
// aggregates survive Update -> GetRun, zero for old rows, and the migration is
// idempotent when reopening the same database file.
func TestSQLiteStore_LongSessionAggregates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: runID, ChatID: 1, ThreadID: 0, RequestID: "req-agg",
		Prompt: "test", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ff, ms := int64(1500), int64(1186000)
	sc, sr := 2, 1
	if err := s.Update(ctx, RunUpdate{
		RunID:           runID,
		FirstFeedbackMs: &ff,
		MaxSilenceMs:    &ms,
		StallCount:      &sc,
		SteerCount:      &sr,
	}); err != nil {
		t.Fatalf("Update aggregates: %v", err)
	}

	got, err := s.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got == nil {
		t.Fatal("GetRun returned nil")
	}
	if got.FirstFeedbackMs != 1500 || got.MaxSilenceMs != 1186000 {
		t.Fatalf("aggregate ms = %d/%d, want 1500/1186000",
			got.FirstFeedbackMs, got.MaxSilenceMs)
	}
	if got.StallCount != 2 || got.SteerCount != 1 {
		t.Fatalf("counts = %d/%d, want 2/1", got.StallCount, got.SteerCount)
	}

	// Old rows (started before the migration) scan as zero via COALESCE.
	oldRun := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: oldRun, ChatID: 1, ThreadID: 0, RequestID: "req-old",
		Prompt: "old", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Start old: %v", err)
	}
	oldGot, err := s.GetRun(ctx, oldRun)
	if err != nil {
		t.Fatalf("GetRun old: %v", err)
	}
	if oldGot.FirstFeedbackMs != 0 || oldGot.MaxSilenceMs != 0 ||
		oldGot.StallCount != 0 || oldGot.SteerCount != 0 {
		t.Fatalf("old row aggregates = %+v, want all zero", oldGot)
	}
}

// TestSQLiteStore_Metrics_LongSessionAggregates covers A5: debug metrics
// exposes the new aggregates without touching the existing fields.
func TestSQLiteStore_Metrics_LongSessionAggregates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Run A: first_feedback 1s, max_silence 10s, 2 stalls, 1 steer.
	runA := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: runA, ChatID: 1, ThreadID: 0, RequestID: "req-a",
		Prompt: "a", StartedAt: now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("Start A: %v", err)
	}
	ffA, msA := int64(1000), int64(10000)
	scA, srA := 2, 1
	durA := int64(60000)
	if err := s.Update(ctx, RunUpdate{
		RunID: runA, FirstFeedbackMs: &ffA, MaxSilenceMs: &msA,
		StallCount: &scA, SteerCount: &srA, DurationMs: &durA,
	}); err != nil {
		t.Fatalf("Update A: %v", err)
	}
	if err := s.Complete(ctx, runA, RunCompleted, "", "", ""); err != nil {
		t.Fatalf("Complete A: %v", err)
	}

	// Run B: first_feedback 3s, max_silence 30s, 1 stall, 0 steers.
	runB := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: runB, ChatID: 1, ThreadID: 0, RequestID: "req-b",
		Prompt: "b", StartedAt: now.Add(-3 * time.Minute),
	}); err != nil {
		t.Fatalf("Start B: %v", err)
	}
	ffB, msB := int64(3000), int64(30000)
	scB, srB := 1, 0
	durB := int64(120000)
	if err := s.Update(ctx, RunUpdate{
		RunID: runB, FirstFeedbackMs: &ffB, MaxSilenceMs: &msB,
		StallCount: &scB, SteerCount: &srB, DurationMs: &durB,
	}); err != nil {
		t.Fatalf("Update B: %v", err)
	}
	if err := s.Complete(ctx, runB, RunCompleted, "", "", ""); err != nil {
		t.Fatalf("Complete B: %v", err)
	}

	m, err := s.Metrics(ctx, MetricsFilter{Since: now.Add(-time.Hour), Until: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if m.RunsTotal != 2 || m.RunsCompleted != 2 {
		t.Fatalf("existing counts broken: total=%d completed=%d", m.RunsTotal, m.RunsCompleted)
	}
	if m.StallsTotal != 3 || m.SteersTotal != 1 {
		t.Fatalf("StallsTotal/SteersTotal = %d/%d, want 3/1", m.StallsTotal, m.SteersTotal)
	}
	if m.AvgFirstFeedbackMs != 2000 {
		t.Fatalf("AvgFirstFeedbackMs = %v, want 2000", m.AvgFirstFeedbackMs)
	}
	if m.AvgMaxSilenceMs != 20000 {
		t.Fatalf("AvgMaxSilenceMs = %v, want 20000", m.AvgMaxSilenceMs)
	}
}

// TestSQLiteStore_Metrics_EmptyWindowIsZero ensures Metrics succeeds with no
// runs in the window: AVG over zero rows yields NULL and must be coalesced to
// zero instead of failing the scan (regression: debug metrics crashed with
// "converting NULL to float64 is unsupported" on a fresh store).
func TestSQLiteStore_Metrics_EmptyWindowIsZero(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	m, err := s.Metrics(ctx, MetricsFilter{Since: now.Add(-time.Hour), Until: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("Metrics on empty window: %v", err)
	}
	if m.RunsTotal != 0 || m.DurationP50Ms != 0 || m.DurationP95Ms != 0 {
		t.Fatalf("empty-window metrics = %+v, want all zero", m)
	}
	if m.StallsTotal != 0 || m.SteersTotal != 0 || m.AvgFirstFeedbackMs != 0 || m.AvgMaxSilenceMs != 0 {
		t.Fatalf("empty-window long-session aggregates = %+v, want all zero", m)
	}
}

// TestSQLiteStore_MarkStaleRunsInterrupted covers T5: rows still running
// after a daemon restart are marked interrupted (error=daemon_restart) while
// terminal rows are untouched.
func TestSQLiteStore_MarkStaleRunsInterrupted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	runA := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: runA, ChatID: 1, ThreadID: 0, RequestID: "req-stale",
		Prompt: "stale", StartedAt: now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("Start A: %v", err)
	}
	runB := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: runB, ChatID: 2, ThreadID: 0, RequestID: "req-done",
		Prompt: "done", StartedAt: now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("Start B: %v", err)
	}
	if err := s.Complete(ctx, runB, RunCompleted, "", "", ""); err != nil {
		t.Fatalf("Complete B: %v", err)
	}

	marked, err := s.MarkStaleRunsInterrupted(ctx, now)
	if err != nil {
		t.Fatalf("MarkStaleRunsInterrupted: %v", err)
	}
	if marked != 1 {
		t.Fatalf("marked = %d, want 1", marked)
	}

	got, err := s.GetRun(ctx, runA)
	if err != nil || got == nil {
		t.Fatalf("GetRun A: %v", err)
	}
	if got.Status != RunInterrupted || got.Error != "daemon_restart" {
		t.Fatalf("stale run status = %q error = %q, want interrupted/daemon_restart", got.Status, got.Error)
	}

	gotB, err := s.GetRun(ctx, runB)
	if err != nil || gotB == nil {
		t.Fatalf("GetRun B: %v", err)
	}
	if gotB.Status != RunCompleted {
		t.Fatalf("completed run status = %q, want completed (untouched)", gotB.Status)
	}

	// Idempotent: a second pass marks nothing.
	marked2, err := s.MarkStaleRunsInterrupted(ctx, now)
	if err != nil {
		t.Fatalf("MarkStaleRunsInterrupted second pass: %v", err)
	}
	if marked2 != 0 {
		t.Fatalf("marked2 = %d, want 0 (idempotent)", marked2)
	}
}

// TestSQLiteStore_MarkStaleRunsInterrupted_CutoffProtectsFreshRuns covers the
// startup race (H2): a run started AFTER the reconcile cutoff (e.g. a cron
// job racing boot) must never be marked interrupted.
func TestSQLiteStore_MarkStaleRunsInterrupted_CutoffProtectsFreshRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	runA := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: runA, ChatID: 1, ThreadID: 0, RequestID: "req-before",
		Prompt: "before", StartedAt: now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("Start A: %v", err)
	}
	runB := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: runB, ChatID: 2, ThreadID: 0, RequestID: "req-after",
		Prompt: "after", StartedAt: now.Add(30 * time.Second),
	}); err != nil {
		t.Fatalf("Start B: %v", err)
	}

	marked, err := s.MarkStaleRunsInterrupted(ctx, now)
	if err != nil {
		t.Fatalf("MarkStaleRunsInterrupted: %v", err)
	}
	if marked != 1 {
		t.Fatalf("marked = %d, want 1 (only the pre-cutoff run)", marked)
	}

	got, err := s.GetRun(ctx, runA)
	if err != nil || got == nil {
		t.Fatalf("GetRun A: %v", err)
	}
	if got.Status != RunInterrupted {
		t.Fatalf("stale run status = %q, want interrupted", got.Status)
	}

	gotB, err := s.GetRun(ctx, runB)
	if err != nil || gotB == nil {
		t.Fatalf("GetRun B: %v", err)
	}
	if gotB.Status != RunRunning {
		t.Fatalf("fresh run status = %q, want running (protected by cutoff)", gotB.Status)
	}
}

// TestSQLiteStore_Metrics_SingleRunPercentiles covers the regression where
// MAX(1, COUNT/2-1) resolved the p50/p95 offset to 1 for a single qualifying
// run, skipping the only row and yielding NULL (percentiles stuck at 0).
func TestSQLiteStore_Metrics_SingleRunPercentiles(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	runID := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: runID, ChatID: 1, ThreadID: 0, RequestID: "req-solo",
		Prompt: "solo", StartedAt: now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	dur := int64(3312)
	if err := s.Update(ctx, RunUpdate{RunID: runID, DurationMs: &dur}); err != nil {
		t.Fatalf("Update duration: %v", err)
	}
	if err := s.Complete(ctx, runID, RunCompleted, "", "", ""); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	m, err := s.Metrics(ctx, MetricsFilter{Since: now.Add(-time.Hour), Until: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if m.DurationP50Ms != 3312 || m.DurationP95Ms != 3312 {
		t.Fatalf("percentiles = p50:%v p95:%v, want both 3312 (single row must resolve)", m.DurationP50Ms, m.DurationP95Ms)
	}
}

// TestSQLiteStore_CompleteWithAggregates verifies the atomic terminal write:
// status + long-session aggregates persist in the same operation and are
// readable back together (never split between Complete and a later Update).
func TestSQLiteStore_CompleteWithAggregates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: runID, ChatID: 1, ThreadID: 0, RequestID: "req-atomic",
		Prompt: "test", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	agg := CompletionAggregates{
		DurationMs:      60000,
		FirstFeedbackMs: 1200,
		MaxSilenceMs:    45000,
		StallCount:      3,
		SteerCount:      2,
	}
	if err := s.CompleteWithAggregates(ctx, runID, RunFailed, "cp", "err", "summary", agg); err != nil {
		t.Fatalf("CompleteWithAggregates: %v", err)
	}

	got, err := s.GetRun(ctx, runID)
	if err != nil || got == nil {
		t.Fatalf("GetRun: %v (nil=%v)", err, got == nil)
	}
	if got.Status != RunFailed {
		t.Fatalf("status = %s, want failed (atomic terminal write)", got.Status)
	}
	if got.DurationMs != 60000 {
		t.Fatalf("duration_ms = %d, want 60000 (terminal write must persist duration)", got.DurationMs)
	}
	if got.FirstFeedbackMs != 1200 || got.MaxSilenceMs != 45000 {
		t.Fatalf("aggregate ms = %d/%d, want 1200/45000", got.FirstFeedbackMs, got.MaxSilenceMs)
	}
	if got.StallCount != 3 || got.SteerCount != 2 {
		t.Fatalf("counts = %d/%d, want 3/2", got.StallCount, got.SteerCount)
	}
	if got.CompletedAt.IsZero() {
		t.Fatal("completed_at not set by atomic completion")
	}
}

func TestSQLiteStore_CompleteWithEvents_CommitsTimelineAndTerminalTogether(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	runID := idgen.New()
	if err := s.Start(ctx, RunRecord{RunID: runID, ChatID: 1, RequestID: "req-tx", Prompt: "test"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.CompleteWithEvents(ctx, runID, RunCompleted, "checkpoint", "", "Read", CompletionAggregates{StallCount: 1}, []RunEvent{
		{RunID: runID, Phase: "bridge_stall", Level: "warn", Message: "event=stall", MetadataJSON: `{"source":"bridge_health"}`},
	}); err != nil {
		t.Fatalf("CompleteWithEvents: %v", err)
	}
	got, err := s.GetRun(ctx, runID)
	if err != nil || got == nil || got.Status != RunCompleted || got.StallCount != 1 {
		t.Fatalf("terminal row = %#v, err=%v", got, err)
	}
	events, err := s.ListEvents(ctx, runID)
	if err != nil || len(events) != 1 || events[0].Phase != "bridge_stall" {
		t.Fatalf("timeline = %#v, err=%v", events, err)
	}
}

func TestSQLiteStore_CompleteWithEvents_RejectsForeignEventID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	runID := idgen.New()
	foreignID := idgen.New()
	if err := s.Start(ctx, RunRecord{RunID: runID, ChatID: 1, RequestID: "req-foreign", Prompt: "test"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	err := s.CompleteWithEvents(ctx, runID, RunCompleted, "done", "", "", CompletionAggregates{}, []RunEvent{
		{RunID: foreignID, Phase: "foreign", Message: "must not persist"},
	})
	if err == nil {
		t.Fatal("expected foreign event run_id rejection")
	}
	got, getErr := s.GetRun(ctx, runID)
	if getErr != nil || got == nil || got.Status != RunRunning {
		t.Fatalf("terminal row changed after foreign event rejection: %#v, err=%v", got, getErr)
	}
	events, listErr := s.ListEvents(ctx, runID)
	if listErr != nil {
		t.Fatalf("ListEvents: %v", listErr)
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v, want none after rollback", events)
	}
}

func TestSQLiteStore_CompleteWithEvents_RollsBackTimelineWhenTerminalUpdateFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	runID := idgen.New()
	if err := s.Start(ctx, RunRecord{RunID: runID, ChatID: 1, RequestID: "req-rollback", Prompt: "test"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Complete(ctx, runID, RunCompleted, "done", "", ""); err != nil {
		t.Fatalf("initial Complete: %v", err)
	}
	err := s.CompleteWithEvents(ctx, runID, RunFailed, "overwrite", "error", "", CompletionAggregates{}, []RunEvent{
		{RunID: runID, Phase: "should_rollback", Message: "must not persist"},
	})
	if err == nil {
		t.Fatal("expected terminal conditional update failure")
	}
	events, listErr := s.ListEvents(ctx, runID)
	if listErr != nil {
		t.Fatalf("ListEvents: %v", listErr)
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v, want rollback of timeline insert", events)
	}
	got, getErr := s.GetRun(ctx, runID)
	if getErr != nil || got == nil || got.Status != RunCompleted || got.Checkpoint != "done" {
		t.Fatalf("terminal row changed after rollback: %#v, err=%v", got, getErr)
	}
}

// TestSQLiteStore_RecordEvents_DefensiveLimits verifies the sink-level caps:
// oversized or invalid metadata is replaced with a valid fallback object and
// oversized messages are truncated, so the timeline never stores broken JSON
// or unbounded blobs.
func TestSQLiteStore_RecordEvents_DefensiveLimits(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := idgen.New()
	if err := s.Start(ctx, RunRecord{
		RunID: runID, ChatID: 1, ThreadID: 0, RequestID: "req-limits",
		Prompt: "test", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	oversized := `{"pad":"` + strings.Repeat("x", observability.MaxEventMetadataBytes+100) + `"}`
	events := []RunEvent{
		{RunID: runID, Timestamp: unix(time.Now()), Phase: "bridge_stall",
			Message: strings.Repeat("m", 10_000), MetadataJSON: oversized},
		{RunID: runID, Timestamp: unix(time.Now()), Phase: "bridge_steer",
			Message: "ok", MetadataJSON: "not-json"},
		{RunID: runID, Timestamp: unix(time.Now()), Phase: "bridge_system",
			Message: "ok", MetadataJSON: ""},
	}
	if err := s.RecordEvents(ctx, events); err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}

	got, err := s.ListEvents(ctx, runID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("events = %d, want 3", len(got))
	}
	if got[0].MetadataJSON != `{"metadata_truncated":true}` {
		t.Fatalf("oversized metadata = %q, want fallback", got[0].MetadataJSON)
	}
	if len(got[0].Message) > maxEventMessageBytes {
		t.Fatalf("message length = %d, want <= %d", len(got[0].Message), maxEventMessageBytes)
	}
	if got[1].MetadataJSON != "{}" {
		t.Fatalf("invalid metadata = %q, want {}", got[1].MetadataJSON)
	}
	if got[2].MetadataJSON != "{}" {
		t.Fatalf("empty metadata = %q, want {}", got[2].MetadataJSON)
	}

	// RecordEvent (single) applies the same limits.
	if err := s.RecordEvent(ctx, RunEvent{
		RunID: runID, Timestamp: unix(time.Now()), Phase: "bridge_stall",
		Message: "single", MetadataJSON: strings.Repeat("j", observability.MaxEventMetadataBytes+1),
	}); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	got, err = s.ListEvents(ctx, runID)
	if err != nil {
		t.Fatalf("ListEvents after single: %v", err)
	}
	if got[3].MetadataJSON != `{"metadata_truncated":true}` {
		t.Fatalf("single oversized metadata = %q, want fallback", got[3].MetadataJSON)
	}
}

// TestSQLiteStore_ReopenMigrationIdempotent verifies the long-session column
// migration is idempotent across store reopens on the same database file.
func TestSQLiteStore_ReopenMigrationIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "runlog.db")
	first, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore first: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}

	second, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore second (idempotent migration): %v", err)
	}
	defer func() { _ = second.Close() }()

	ctx := context.Background()
	runID := idgen.New()
	if err := second.Start(ctx, RunRecord{
		RunID: runID, ChatID: 1, ThreadID: 0, RequestID: "req-reopen",
		Prompt: "test", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Start after reopen: %v", err)
	}
	got, err := second.GetRun(ctx, runID)
	if err != nil || got == nil {
		t.Fatalf("GetRun after reopen: %v (nil=%v)", err, got == nil)
	}
}

// TestSanitizeEventMessage_UTF8AndControlChars covers correction 7: invalid
// UTF-8 is replaced (never stored raw), CR/LF and C0/C1/control characters
// are removed, and multi-byte runes survive untouched.
func TestSanitizeEventMessage_UTF8AndControlChars(t *testing.T) {
	// CR/LF + C0 + DEL removed; text preserved.
	got := sanitizeEventMessage("line1\r\nline2\x00\x1f\x7fend")
	if strings.ContainsAny(got, "\r\n\x00\x1f\x7f") {
		t.Fatalf("control characters survived sanitize: %q", got)
	}
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") || !strings.Contains(got, "end") {
		t.Fatalf("text lost during control-char removal: %q", got)
	}

	// Invalid UTF-8 bytes are replaced with U+FFFD — the stored TEXT is
	// always valid UTF-8.
	invalid := "ok\xff\xfe\x80bad"
	got = sanitizeEventMessage(invalid)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitized message is not valid UTF-8: %q", got)
	}
	if !strings.Contains(got, "\uFFFD") {
		t.Fatalf("invalid bytes not replaced with U+FFFD: %q", got)
	}
	if !strings.Contains(got, "ok") || !strings.Contains(got, "bad") {
		t.Fatalf("surrounding text lost: %q", got)
	}

	// Multi-byte runes survive untouched (never mistaken for C1 controls).
	multi := "café —日本語"
	got = sanitizeEventMessage(multi)
	if got != multi {
		t.Fatalf("multi-byte runes corrupted: %q != %q", got, multi)
	}
}

// TestSanitizeEventMessage_ExactCapBoundary covers the exact cap+1 boundary:
// a message of exactly maxEventMessageBytes passes through unchanged, cap+1
// is truncated to exactly the cap, and the truncation never splits a rune.
func TestSanitizeEventMessage_ExactCapBoundary(t *testing.T) {
	// Exactly at the cap: unchanged.
	exact := strings.Repeat("a", maxEventMessageBytes)
	if got := sanitizeEventMessage(exact); got != exact {
		t.Fatalf("exact-cap message changed: len=%d want %d", len(got), len(exact))
	}

	// cap+1 ASCII: truncated to exactly the cap (never cap+1).
	over := strings.Repeat("a", maxEventMessageBytes+1)
	got := sanitizeEventMessage(over)
	if len(got) != maxEventMessageBytes {
		t.Fatalf("cap+1 message len = %d, want exactly %d", len(got), maxEventMessageBytes)
	}

	// cap+1 with a multi-byte rune straddling the boundary: the cut lands at
	// a rune boundary so the result is valid UTF-8 and never exceeds the cap.
	straddle := strings.Repeat("a", maxEventMessageBytes-2) + "éé" // 2 runes span the cap
	got = sanitizeEventMessage(straddle)
	if len(got) > maxEventMessageBytes {
		t.Fatalf("straddling message len = %d, want <= %d", len(got), maxEventMessageBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("straddling truncation split a rune: %q", got)
	}
}

// TestSanitizeMetadataJSON_RemarshalAndCap covers correction 7: metadata is
// parsed and RE-MARSHALED (normalizing escaping, rejecting literal control
// characters), and the re-marshaled size must respect exactly the cap.
func TestSanitizeMetadataJSON_RemarshalAndCap(t *testing.T) {
	// Empty -> valid empty object.
	if got := sanitizeMetadataJSON(""); got != "{}" {
		t.Fatalf("empty metadata = %q, want {}", got)
	}
	// Invalid JSON -> fallback, never broken JSON.
	if got := sanitizeMetadataJSON("not-json"); got != "{}" {
		t.Fatalf("invalid metadata = %q, want {}", got)
	}
	// Literal control characters inside a JSON string are rejected.
	if got := sanitizeMetadataJSON(`{"a":"x` + "\n" + `y"}`); got != "{}" {
		t.Fatalf("control-char metadata = %q, want {}", got)
	}
	// Valid metadata round-trips through re-marshal.
	if got := sanitizeMetadataJSON(`{"a":1,"b":"x"}`); got != `{"a":1,"b":"x"}` {
		t.Fatalf("valid metadata = %q, want normalized re-marshal", got)
	}
	// Re-marshal inflation is caught: html-escaped output can exceed the
	// input length, so the cap check runs on the re-marshaled bytes.
	if got := sanitizeMetadataJSON(`{"pad":"` + strings.Repeat("<", observability.MaxEventMetadataBytes-20) + `"}`); got != `{"metadata_truncated":true}` {
		t.Fatalf("re-marshal-inflated metadata = %q, want fallback", got)
	}
	// Oversized input also degrades to the fallback.
	if got := sanitizeMetadataJSON(`{"pad":"` + strings.Repeat("x", observability.MaxEventMetadataBytes+100) + `"}`); got != `{"metadata_truncated":true}` {
		t.Fatalf("oversized metadata = %q, want fallback", got)
	}
}
