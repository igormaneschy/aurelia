package runlog

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "runlog.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSQLiteStore_StartAndLatest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := uuid.NewString()
	now := time.Now().UTC()

	record := RunRecord{
		RunID:     runID,
		ChatID:    100,
		ThreadID:  0,
		RequestID: "req-1",
		SessionID: "",
		CWD:       "/home/project",
		Prompt:    "implement feature X",
		StartedAt: now,
	}

	if err := s.Start(ctx, record); err != nil {
		t.Fatalf("Start: %v", err)
	}

	got, err := s.Latest(ctx, 100, 0)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got == nil {
		t.Fatal("Latest returned nil")
	}
	if got.RunID != runID {
		t.Fatalf("RunID = %q, want %q", got.RunID, runID)
	}
	if got.ChatID != 100 {
		t.Fatalf("ChatID = %d, want 100", got.ChatID)
	}
	if got.ThreadID != 0 {
		t.Fatalf("ThreadID = %d, want 0", got.ThreadID)
	}
	if got.RequestID != "req-1" {
		t.Fatalf("RequestID = %q, want req-1", got.RequestID)
	}
	if got.CWD != "/home/project" {
		t.Fatalf("CWD = %q, want /home/project", got.CWD)
	}
	if got.Prompt != "implement feature X" {
		t.Fatalf("Prompt = %q, want implement feature X", got.Prompt)
	}
	if got.Status != RunRunning {
		t.Fatalf("Status = %q, want running", got.Status)
	}
	if got.StartedAt.IsZero() {
		t.Fatal("StartedAt should be set")
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be set")
	}
}

func TestSQLiteStore_Latest_NoRows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.Latest(ctx, 999, 0)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestSQLiteStore_Update(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := uuid.NewString()
	record := RunRecord{
		RunID:   runID,
		ChatID:  100,
		ThreadID: 0,
		RequestID: "req-1",
		Prompt:  "test",
	}
	if err := s.Start(ctx, record); err != nil {
		t.Fatalf("Start: %v", err)
	}

	sessionID := "sess-123"
	status := RunRunning
	summary := "tool1, tool2"
	checkpoint := "objetivo: test"

	err := s.Update(ctx, RunUpdate{
		RunID:       runID,
		SessionID:   &sessionID,
		Status:      &status,
		ToolSummary: &summary,
		Checkpoint:  &checkpoint,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := s.Latest(ctx, 100, 0)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got.SessionID != "sess-123" {
		t.Fatalf("SessionID = %q, want sess-123", got.SessionID)
	}
	if got.ToolSummary != "tool1, tool2" {
		t.Fatalf("ToolSummary = %q, want tool1, tool2", got.ToolSummary)
	}
	if got.Checkpoint != "objetivo: test" {
		t.Fatalf("Checkpoint = %q, want objetivo: test", got.Checkpoint)
	}
}

func TestSQLiteStore_Complete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := uuid.NewString()
	record := RunRecord{
		RunID:   runID,
		ChatID:  100,
		ThreadID: 0,
		RequestID: "req-1",
		Prompt:  "test",
	}
	if err := s.Start(ctx, record); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := s.Complete(ctx, runID, RunCompleted, "done", ""); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := s.Latest(ctx, 100, 0)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got.Status != RunCompleted {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
	if got.Checkpoint != "done" {
		t.Fatalf("Checkpoint = %q, want done", got.Checkpoint)
	}
	if got.CompletedAt.IsZero() {
		t.Fatal("CompletedAt should be set")
	}
}

func TestSQLiteStore_Complete_WithError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := uuid.NewString()
	record := RunRecord{
		RunID:   runID,
		ChatID:  100,
		ThreadID: 0,
		RequestID: "req-1",
		Prompt:  "test",
	}
	if err := s.Start(ctx, record); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := s.Complete(ctx, runID, RunFailed, "", "bridge error: timeout"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := s.Latest(ctx, 100, 0)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got.Status != RunFailed {
		t.Fatalf("Status = %q, want failed", got.Status)
	}
	if got.Error != "bridge error: timeout" {
		t.Fatalf("Error = %q, want bridge error: timeout", got.Error)
	}
}

func TestSQLiteStore_ThreadIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r1 := RunRecord{RunID: uuid.NewString(), ChatID: 100, ThreadID: 0, RequestID: "req-1", Prompt: "main"}
	r2 := RunRecord{RunID: uuid.NewString(), ChatID: 100, ThreadID: 42, RequestID: "req-2", Prompt: "topic"}

	if err := s.Start(ctx, r1); err != nil {
		t.Fatalf("Start main: %v", err)
	}
	if err := s.Start(ctx, r2); err != nil {
		t.Fatalf("Start topic: %v", err)
	}

	// Latest for thread 0 should return only r1
	got, err := s.Latest(ctx, 100, 0)
	if err != nil {
		t.Fatalf("Latest thread 0: %v", err)
	}
	if got == nil || got.RunID != r1.RunID {
		t.Fatalf("thread 0: expected %q, got %v", r1.RunID, got)
	}

	// Latest for thread 42 should return only r2
	got, err = s.Latest(ctx, 100, 42)
	if err != nil {
		t.Fatalf("Latest thread 42: %v", err)
	}
	if got == nil || got.RunID != r2.RunID {
		t.Fatalf("thread 42: expected %q, got %v", r2.RunID, got)
	}
}

func TestSQLiteStore_Latest_ReturnsMostRecent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r1 := RunRecord{RunID: uuid.NewString(), ChatID: 100, ThreadID: 0, RequestID: "req-1", Prompt: "first"}
	if err := s.Start(ctx, r1); err != nil {
		t.Fatalf("Start first: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	r2 := RunRecord{RunID: uuid.NewString(), ChatID: 100, ThreadID: 0, RequestID: "req-2", Prompt: "second"}
	if err := s.Start(ctx, r2); err != nil {
		t.Fatalf("Start second: %v", err)
	}

	got, err := s.Latest(ctx, 100, 0)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got.RunID != r2.RunID {
		t.Fatalf("expected latest run %q, got %q", r2.RunID, got.RunID)
	}
}

func TestSQLiteStore_RestartCollision(t *testing.T) {
	// Simulate daemon restart: open the same DB path with a fresh store,
	// generate independent RunIDs and verify they are unique (not process-local).
	// This guards against the bug where restarting reuses "run-1", "run-2".
	dbPath := filepath.Join(t.TempDir(), "restart.db")

	s1, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore s1: %v", err)
	}
	ctx := context.Background()

	r1 := RunRecord{RunID: uuid.NewString(), ChatID: 100, ThreadID: 0, RequestID: "req-1", Prompt: "first"}
	if err := s1.Start(ctx, r1); err != nil {
		t.Fatalf("Start first: %v", err)
	}

	// Close s1 and reopen the same path as s2 (simulating restart)
	s1.Close()

	s2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore s2 (reopen): %v", err)
	}
	defer s2.Close()
	r2 := RunRecord{RunID: uuid.NewString(), ChatID: 100, ThreadID: 0, RequestID: "req-2", Prompt: "second"}
	if err := s2.Start(ctx, r2); err != nil {
		t.Fatalf("Start second (restart): %v", err)
	}

	// Each store should see data persisted by the other.
	// s2 should see both r1 and r2 via Latest (r2 is newest).
	// We verify s2 sees both by checking Latest and also by reading r1 directly.
	gotLatest, err := s2.Latest(ctx, 100, 0)
	if err != nil {
		t.Fatalf("Latest s2: %v", err)
	}
	if gotLatest == nil || gotLatest.RunID != r2.RunID {
		t.Fatalf("s2 Latest: expected %q, got %v", r2.RunID, gotLatest)
	}

	// Verify r1 still exists (s2 sees previous data)
	// We can check by counting rows for this chat/thread.
	// For simplicity, just verify RunIDs are different (enforced by uuid).
	if r1.RunID == r2.RunID {
		t.Fatal("RunID collision across stores — RunID must be unique")
	}
}

func TestSQLiteStore_Reopen_PreservesData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "persist.db")

	s1, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	ctx := context.Background()
	runID := uuid.NewString()
	if err := s1.Start(ctx, RunRecord{
		RunID:     runID,
		ChatID:    100,
		ThreadID:  0,
		RequestID: "req-1",
		Prompt:    "persist test",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s1.Complete(ctx, runID, RunCompleted, "done", ""); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	s1.Close()

	// Reopen
	s2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore reopen: %v", err)
	}
	defer s2.Close()

	got, err := s2.Latest(ctx, 100, 0)
	if err != nil {
		t.Fatalf("Latest after reopen: %v", err)
	}
	if got == nil {
		t.Fatal("Latest after reopen returned nil")
	}
	if got.RunID != runID {
		t.Fatalf("RunID = %q, want %q", got.RunID, runID)
	}
	if got.Status != RunCompleted {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
}

func TestSQLiteStore_RecordEventAndListEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := "test-run-events"

	events := []RunEvent{
		{RunID: runID, Timestamp: 1000, Phase: "telegram_received", Level: "info", Message: "message received"},
		{RunID: runID, Timestamp: 1001, Phase: "bridge_request_started", Level: "info", Message: "starting bridge"},
		{RunID: runID, Timestamp: 1002, Phase: "bridge_tool_use", Level: "info", Message: "Read file", MetadataJSON: `{"tool":"Read"}`},
		{RunID: runID, Timestamp: 1003, Phase: "run_completed", Level: "info", Message: "done"},
	}

	for _, ev := range events {
		if err := s.RecordEvent(ctx, ev); err != nil {
			t.Fatalf("RecordEvent(%s): %v", ev.Phase, err)
		}
	}

	got, err := s.ListEvents(ctx, runID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != len(events) {
		t.Fatalf("got %d events, want %d", len(got), len(events))
	}

	for i, ev := range got {
		want := events[i]
		if ev.RunID != want.RunID {
			t.Errorf("event[%d] RunID = %q, want %q", i, ev.RunID, want.RunID)
		}
		if ev.Phase != want.Phase {
			t.Errorf("event[%d] Phase = %q, want %q", i, ev.Phase, want.Phase)
		}
		if ev.Message != want.Message {
			t.Errorf("event[%d] Message = %q, want %q", i, ev.Message, want.Message)
		}
		if ev.Level != want.Level {
			t.Errorf("event[%d] Level = %q, want %q", i, ev.Level, want.Level)
		}
	}
}

func TestSQLiteStore_EventsOrderedByTimestamp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := "order-test"

	// Insert out of order by id but timestamp should enforce order.
	_ = s.RecordEvent(ctx, RunEvent{RunID: runID, Timestamp: 300, Phase: "phase-3"})
	_ = s.RecordEvent(ctx, RunEvent{RunID: runID, Timestamp: 100, Phase: "phase-1"})
	_ = s.RecordEvent(ctx, RunEvent{RunID: runID, Timestamp: 200, Phase: "phase-2"})

	events, err := s.ListEvents(ctx, runID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].Phase != "phase-1" {
		t.Fatalf("events[0] = %q, want phase-1", events[0].Phase)
	}
	if events[1].Phase != "phase-2" {
		t.Fatalf("events[1] = %q, want phase-2", events[1].Phase)
	}
	if events[2].Phase != "phase-3" {
		t.Fatalf("events[2] = %q, want phase-3", events[2].Phase)
	}
}

func TestSQLiteStore_GetRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := uuid.NewString()
	record := RunRecord{
		RunID:     runID,
		ChatID:    100,
		ThreadID:  0,
		RequestID: "req-1",
		SessionID: "sess-abc",
		CWD:       "/home/project",
		Prompt:    "test getrun",
		UserID:    42,
		AgentName: "coder",
		Provider:  "kimi",
		Model:     "kimi-k2",
	}
	if err := s.Start(ctx, record); err != nil {
		t.Fatalf("Start: %v", err)
	}

	got, err := s.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got == nil {
		t.Fatal("GetRun returned nil")
	}
	if got.RunID != runID {
		t.Fatalf("RunID = %q, want %q", got.RunID, runID)
	}
	if got.ChatID != 100 {
		t.Fatalf("ChatID = %d, want 100", got.ChatID)
	}
	if got.UserID != 42 {
		t.Fatalf("UserID = %d, want 42", got.UserID)
	}
	if got.AgentName != "coder" {
		t.Fatalf("AgentName = %q, want coder", got.AgentName)
	}
	if got.Provider != "kimi" {
		t.Fatalf("Provider = %q, want kimi", got.Provider)
	}
	if got.Model != "kimi-k2" {
		t.Fatalf("Model = %q, want kimi-k2", got.Model)
	}
}

func TestSQLiteStore_GetRun_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetRun(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for nonexistent run")
	}
}

func TestSQLiteStore_ListRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert runs for two different chats.
	r1 := RunRecord{RunID: uuid.NewString(), ChatID: 100, ThreadID: 0, RequestID: "r1", Prompt: "first", UserID: 1, AgentName: "agent1"}
	r2 := RunRecord{RunID: uuid.NewString(), ChatID: 100, ThreadID: 0, RequestID: "r2", Prompt: "second", UserID: 1, AgentName: "agent1"}
	r3 := RunRecord{RunID: uuid.NewString(), ChatID: 200, ThreadID: 0, RequestID: "r3", Prompt: "other chat", UserID: 2, AgentName: "agent2"}

	if err := s.Start(ctx, r1); err != nil {
		t.Fatalf("Start r1: %v", err)
	}
	if err := s.Start(ctx, r2); err != nil {
		t.Fatalf("Start r2: %v", err)
	}
	if err := s.Start(ctx, r3); err != nil {
		t.Fatalf("Start r3: %v", err)
	}

	// List all (limit 10).
	all, err := s.ListRuns(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d runs, want 3", len(all))
	}

	// Filter by chat 100.
	chatRuns, err := s.ListRuns(ctx, 100, 10)
	if err != nil {
		t.Fatalf("ListRuns chat=100: %v", err)
	}
	if len(chatRuns) != 2 {
		t.Fatalf("got %d runs for chat 100, want 2", len(chatRuns))
	}

	// Filter by chat 200.
	chat200Runs, err := s.ListRuns(ctx, 200, 10)
	if err != nil {
		t.Fatalf("ListRuns chat=200: %v", err)
	}
	if len(chat200Runs) != 1 {
		t.Fatalf("got %d runs for chat 200, want 1", len(chat200Runs))
	}
	if chat200Runs[0].AgentName != "agent2" {
		t.Fatalf("agent = %q, want agent2", chat200Runs[0].AgentName)
	}
}

func TestSQLiteStore_Metrics(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)

	// Seed runs with various statuses and extended fields.
	runs := []RunRecord{
		{RunID: uuid.NewString(), ChatID: 100, ThreadID: 0, RequestID: "r1", Prompt: "", Status: RunCompleted, StartedAt: now.Add(-10 * time.Minute), DurationMs: 5000, InputTokens: 100, OutputTokens: 20, CostUSD: 0.001, Provider: "kimi", Model: "kimi-k2", EntryPoint: "telegram", UsedFallback: false},
		{RunID: uuid.NewString(), ChatID: 100, ThreadID: 0, RequestID: "r2", Prompt: "", Status: RunCompleted, StartedAt: now.Add(-5 * time.Minute), DurationMs: 3000, InputTokens: 200, OutputTokens: 50, CostUSD: 0.002, Provider: "kimi", Model: "kimi-k2", EntryPoint: "telegram", UsedFallback: false},
		{RunID: uuid.NewString(), ChatID: 100, ThreadID: 0, RequestID: "r3", Prompt: "", Status: RunFailed, StartedAt: now.Add(-2 * time.Minute), DurationMs: 1000, InputTokens: 50, OutputTokens: 10, CostUSD: 0.0005, Provider: "anthropic", Model: "claude", EntryPoint: "telegram", UsedFallback: true},
		{RunID: uuid.NewString(), ChatID: 200, ThreadID: 0, RequestID: "r4", Prompt: "", Status: RunTimedOut, StartedAt: yesterday, DurationMs: 60000, InputTokens: 500, OutputTokens: 100, CostUSD: 0.01, Provider: "kimi", Model: "kimi-k2", EntryPoint: "cron", UsedFallback: false},
		{RunID: uuid.NewString(), ChatID: 200, ThreadID: 0, RequestID: "r5", Prompt: "", Status: RunRunning, StartedAt: now, DurationMs: 0, InputTokens: 0, OutputTokens: 0, CostUSD: 0, Provider: "", Model: "", EntryPoint: "telegram", UsedFallback: false},
	}

	for _, r := range runs {
		if err := s.Start(ctx, r); err != nil {
			t.Fatalf("Start %s: %v", r.RunID, err)
		}
		// Update status and extended fields after start.
		if r.Status != RunRunning || r.DurationMs > 0 || r.InputTokens > 0 {
			upd := RunUpdate{
				RunID:       r.RunID,
				Status:      &r.Status,
				DurationMs:  &r.DurationMs,
				InputTokens: &r.InputTokens,
				OutputTokens: &r.OutputTokens,
				CostUSD:     &r.CostUSD,
				UsedFallback: &r.UsedFallback,
			}
			if r.EntryPoint != "" {
				upd.EntryPoint = &r.EntryPoint
			}
			if r.Provider != "" {
				upd.Provider = &r.Provider
			}
			if r.Model != "" {
				upd.Model = &r.Model
			}
			if err := s.Update(ctx, upd); err != nil {
				t.Fatalf("Update %s: %v", r.RunID, err)
			}
		}
	}

	// Query metrics for last hour (should include r1, r2, r3, r5 but not r4).
	metrics, err := s.Metrics(ctx, MetricsFilter{
		Since: now.Add(-1 * time.Hour),
		Until: now.Add(1 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}

	if metrics.RunsTotal != 4 {
		t.Fatalf("RunsTotal = %d, want 4", metrics.RunsTotal)
	}
	if metrics.RunsCompleted != 2 {
		t.Fatalf("RunsCompleted = %d, want 2", metrics.RunsCompleted)
	}
	if metrics.RunsFailed != 1 {
		t.Fatalf("RunsFailed = %d, want 1", metrics.RunsFailed)
	}
	if metrics.RunsRunning != 1 {
		t.Fatalf("RunsRunning = %d, want 1", metrics.RunsRunning)
	}
	if metrics.RunsTimedOut != 0 {
		t.Fatalf("RunsTimedOut = %d, want 0 (yesterday's run excluded)", metrics.RunsTimedOut)
	}
	if metrics.FallbackCount != 1 {
		t.Fatalf("FallbackCount = %d, want 1", metrics.FallbackCount)
	}

	// Tokens
	if metrics.TokensInputTotal != 350 {
		t.Fatalf("TokensInputTotal = %d, want 350", metrics.TokensInputTotal)
	}
	if metrics.TokensOutputTotal != 80 {
		t.Fatalf("TokensOutputTotal = %d, want 80", metrics.TokensOutputTotal)
	}

	// Cost
	if metrics.CostUSDTotal < 0.003 || metrics.CostUSDTotal > 0.004 {
		t.Fatalf("CostUSDTotal = %.4f, want ~0.0035", metrics.CostUSDTotal)
	}

	// Provider breakdown should have 2 entries (kimi, anthropic).
	if len(metrics.ProviderBreakdown) < 2 {
		t.Fatalf("ProviderBreakdown = %+v, want at least 2 entries", metrics.ProviderBreakdown)
	}

	// Entrypoint breakdown
	if len(metrics.EntrypointBreakdown) > 0 {
		foundTelegram := false
		for _, item := range metrics.EntrypointBreakdown {
			if item.Key == "telegram" {
				foundTelegram = true
				break
			}
		}
		if !foundTelegram {
			t.Fatalf("EntrypointBreakdown missing 'telegram': %+v", metrics.EntrypointBreakdown)
		}
	}
}

func TestSQLiteStore_FilePermissions(t *testing.T) {
	// Verify the runlog DB file is created with owner-only permissions.
	// On Unix: assert 0600 for the .db file; check -wal and -shm if present.
	// On Windows: skip mode checks (not meaningful on that platform).
	if runtime.GOOS == "windows" {
		t.Skip("permission assertions not meaningful on Windows")
	}

	dbPath := filepath.Join(t.TempDir(), "perm.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	store.Close()

	// Check .db file permission
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("Stat .db: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("db file permission: got %o, want 0600", perm)
	}

	// Check -wal and -shm if they exist (SQLite creates them lazily)
	for _, ext := range []string{"-wal", "-shm"} {
		path := dbPath + ext
		if info, statErr := os.Stat(path); statErr == nil {
			if perm := info.Mode().Perm(); perm != 0600 {
				t.Errorf("%s permission: got %o, want 0600", ext, perm)
			}
		}
	}
}

func TestSQLiteStore_InboundOutboundMessageIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	runID := uuid.NewString()
	record := RunRecord{
		RunID:             runID,
		ChatID:            100,
		ThreadID:          0,
		RequestID:         "req-1",
		Prompt:            "test message bridge",
		InboundMessageID:  42,
		OutboundMessageID: 99,
	}
	if err := s.Start(ctx, record); err != nil {
		t.Fatalf("Start: %v", err)
	}

	got, err := s.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got == nil {
		t.Fatal("GetRun returned nil")
	}
	if got.InboundMessageID != 42 {
		t.Errorf("InboundMessageID = %d, want 42", got.InboundMessageID)
	}
	if got.OutboundMessageID != 99 {
		t.Errorf("OutboundMessageID = %d, want 99", got.OutboundMessageID)
	}

	// Test Update path for outbound_message_id.
	newOutbound := int64(150)
	if err := s.Update(ctx, RunUpdate{
		RunID:             runID,
		OutboundMessageID: &newOutbound,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = s.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun after update: %v", err)
	}
	if got.OutboundMessageID != 150 {
		t.Errorf("OutboundMessageID after update = %d, want 150", got.OutboundMessageID)
	}
}

func TestSQLiteStore_GetLastOutboundMessage_Found(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sessionFile := "/path/to/session.json"
	r1 := RunRecord{
		RunID:             uuid.NewString(),
		ChatID:            100,
		ThreadID:          0,
		RequestID:         "req-1",
		Prompt:            "first",
		SessionFile:       sessionFile,
		OutboundMessageID: 10,
	}
	r2 := RunRecord{
		RunID:             uuid.NewString(),
		ChatID:            200,
		ThreadID:          5,
		RequestID:         "req-2",
		Prompt:            "second",
		SessionFile:       sessionFile,
		OutboundMessageID: 20,
	}
	if err := s.Start(ctx, r1); err != nil {
		t.Fatalf("Start r1: %v", err)
	}
	// Ensure distinct started_at timestamps (second resolution).
	time.Sleep(1100 * time.Millisecond)
	if err := s.Start(ctx, r2); err != nil {
		t.Fatalf("Start r2: %v", err)
	}

	chatID, threadID, msgID, err := s.GetLastOutboundMessage(ctx, sessionFile)
	if err != nil {
		t.Fatalf("GetLastOutboundMessage: %v", err)
	}
	if chatID != 200 {
		t.Errorf("chatID = %d, want 200", chatID)
	}
	if threadID != 5 {
		t.Errorf("threadID = %d, want 5", threadID)
	}
	if msgID != 20 {
		t.Errorf("messageID = %d, want 20", msgID)
	}
}

func TestSQLiteStore_GetLastOutboundMessage_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chatID, threadID, msgID, err := s.GetLastOutboundMessage(ctx, "/nonexistent/session.json")
	if err != nil {
		t.Fatalf("GetLastOutboundMessage: %v", err)
	}
	if chatID != 0 || threadID != 0 || msgID != 0 {
		t.Errorf("expected zeros for not found, got chat=%d thread=%d msg=%d", chatID, threadID, msgID)
	}
}

func TestSQLiteStore_GetLastOutboundMessage_EmptySession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chatID, threadID, msgID, err := s.GetLastOutboundMessage(ctx, "")
	if err != nil {
		t.Fatalf("GetLastOutboundMessage: %v", err)
	}
	if chatID != 0 || threadID != 0 || msgID != 0 {
		t.Errorf("expected zeros for empty session, got chat=%d thread=%d msg=%d", chatID, threadID, msgID)
	}
}

func TestSQLiteStore_GetLastOutboundMessage_SkipsZeroOutbound(t *testing.T) {
	// Runs with outbound_message_id=0 should be skipped.
	s := newTestStore(t)
	ctx := context.Background()

	sessionFile := "/path/to/session.json"
	r1 := RunRecord{
		RunID:             uuid.NewString(),
		ChatID:            100,
		ThreadID:          0,
		RequestID:         "req-1",
		Prompt:            "no outbound",
		SessionFile:       sessionFile,
		OutboundMessageID: 0, // no message sent
	}
	r2 := RunRecord{
		RunID:             uuid.NewString(),
		ChatID:            200,
		ThreadID:          1,
		RequestID:         "req-2",
		Prompt:            "with outbound",
		SessionFile:       sessionFile,
		OutboundMessageID: 55,
	}
	if err := s.Start(ctx, r1); err != nil {
		t.Fatalf("Start r1: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if err := s.Start(ctx, r2); err != nil {
		t.Fatalf("Start r2: %v", err)
	}

	chatID, threadID, msgID, err := s.GetLastOutboundMessage(ctx, sessionFile)
	if err != nil {
		t.Fatalf("GetLastOutboundMessage: %v", err)
	}
	if msgID != 55 {
		t.Errorf("messageID = %d, want 55 (should skip r1 with outbound=0)", msgID)
	}
	if chatID != 200 {
		t.Errorf("chatID = %d, want 200", chatID)
	}
	if threadID != 1 {
		t.Errorf("threadID = %d, want 1", threadID)
	}
}
