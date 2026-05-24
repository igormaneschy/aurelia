package pipeline

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/planning"
	"github.com/igormaneschy/aurelia/internal/session"
)

// TestE2E_PlanMode verifies the complete Plan Mode lifecycle end-to-end:
//
//	1. State creation (simulating /plan)
//	2. Tool use observation (simulating a normal message)
//	3. Artifact reconciliation
//	4. Status transition to awaiting_exec (simulating /execute)
//	5. Plan execution via tryExecutePlan
//	6. State cleanup after successful handoff
func TestE2E_PlanMode(t *testing.T) {
	// ── Setup ──────────────────────────────────────────────
	store := setupOfferTestStore(t)
	sessions := session.NewStore()
	cwd := t.TempDir()
	sessions.SetCwd(1, 0, cwd)

	fo := &fakeOutput{}
	orch := orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{})

	s := &Service{
		output:         fo,
		orchestrator:   orch,
		sessions:       sessions,
		planningStore:  store,
		planningStates: sync.Map{},
	}

	ctx := t.Context()
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}
	localKey := sessionKey(1, 0, 100)

	// ── Step 1: Simulate /plan — create state ─────────────
	state := &planning.State{
		Key:       key,
		Status:    planning.StatusActive,
		Phase:     planning.PhaseSpecify,
		CWD:       cwd,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save initial state: %v", err)
	}

	// Verify state was created with correct Status and Phase
	loaded, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after save: %v", err)
	}
	if loaded == nil {
		t.Fatal("state is nil after save")
	}
	if loaded.Status != planning.StatusActive {
		t.Fatalf("Status = %q, want %q", loaded.Status, planning.StatusActive)
	}
	if loaded.Phase != planning.PhaseSpecify {
		t.Fatalf("Phase = %q, want %q", loaded.Phase, planning.PhaseSpecify)
	}

	// Store in planningStates so observeToolUse can find it
	s.planningStates.Store(localKey, loaded)

	// ── Step 2: Simulate tool_use Write ────────────────────
	target := filepath.Join(cwd, "spec.md")
	ev := bridge.Event{
		Type: "tool_use",
		Name: "Write",
		Input: map[string]interface{}{
			"path":    target,
			"content": "# Specification",
		},
	}
	s.observeToolUse(1, 0, 100, ev)

	// Verify artifact was added to in-memory state (modification is synchronous)
	if len(loaded.Materialized) != 1 {
		t.Fatalf("expected 1 materialized artifact, got %d", len(loaded.Materialized))
	}
	if loaded.Materialized[0].Tool != "Write" {
		t.Errorf("Tool = %q, want %q", loaded.Materialized[0].Tool, "Write")
	}
	if loaded.Materialized[0].Path != target {
		t.Errorf("Path = %q, want %q", loaded.Materialized[0].Path, target)
	}
	if loaded.Materialized[0].Confirmed {
		t.Error("artifact should not be Confirmed before reconcile")
	}

	// Wait for async save goroutine to persist the state
	time.Sleep(50 * time.Millisecond)

	// Verify state was persisted to the store
	fromStore, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after observe: %v", err)
	}
	if fromStore == nil {
		t.Fatal("state is nil after observe")
	}
	if len(fromStore.Materialized) != 1 {
		t.Fatalf("expected 1 artifact in store, got %d", len(fromStore.Materialized))
	}

	// ── Step 3: Reconcile artifacts ────────────────────────
	// Create the file on disk so os.Stat succeeds
	if err := os.WriteFile(target, []byte("# Specification"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Reload to get latest version before reconcile
	s.planningStates.Store(localKey, fromStore)
	s.reconcileArtifacts(1, 0, 100)
	time.Sleep(50 * time.Millisecond) // wait for async save

	reconciled, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after reconcile: %v", err)
	}
	if reconciled == nil {
		t.Fatal("state is nil after reconcile")
	}
	if len(reconciled.Materialized) != 1 {
		t.Fatalf("expected 1 artifact after reconcile, got %d", len(reconciled.Materialized))
	}
	if !reconciled.Materialized[0].Confirmed {
		t.Error("artifact should be Confirmed after reconcile")
	}

	// ── Step 4: Simulate /execute ──────────────────────────
	reconciled.Status = planning.StatusAwaitingExec
	reconciled.UpdatedAt = time.Now()
	if err := store.Save(ctx, reconciled); err != nil {
		t.Fatalf("Save awaiting_exec: %v", err)
	}

	// Verify status was updated
	saved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after status change: %v", err)
	}
	if saved == nil {
		t.Fatal("state is nil after status change")
	}
	if saved.Status != planning.StatusAwaitingExec {
		t.Fatalf("Status = %q, want %q", saved.Status, planning.StatusAwaitingExec)
	}

	// ── Step 5: Simulate bridge emitting aurelia-plan ──────
	s.planningStates.Store(localKey, saved)

	planText := "Plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"T1\",\"description\":\"write spec\",\"prompt\":\"create spec.md\",\"needs_worktree\":false}]}\n```\n"
	handled, outcome := s.tryExecutePlan(1, 0, 100, planText, 100)
	if !handled {
		t.Fatal("expected handled=true")
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}

	// Wait for async goroutine: ExecuteApprovedPlan then cleanup
	for i := 0; i < 100 && !fo.planExecuted; i++ {
		time.Sleep(time.Millisecond)
	}
	if !fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan was not called")
	}

	// Wait for cleanup goroutine (delete after ExecuteApprovedPlan)
	time.Sleep(50 * time.Millisecond)

	// ── Step 6: Verify state deleted ───────────────────────
	deleted, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after cleanup: %v", err)
	}
	if deleted != nil {
		t.Fatal("expected state to be deleted after successful handoff")
	}
}

// TestE2E_PlanMode_ExecutorRefused verifies that when the executor
// fails (panics), the planning state is preserved and not deleted.
//
//	1-4: Same flow as happy path
//	5: Executor panics during ExecuteApprovedPlan
//	6: Verify state is preserved (not deleted)
func TestE2E_PlanMode_ExecutorRefused(t *testing.T) {
	// ── Setup ──────────────────────────────────────────────
	store := setupOfferTestStore(t)
	sessions := session.NewStore()
	cwd := t.TempDir()
	sessions.SetCwd(1, 0, cwd)

	po := &panicOutput{fakeOutput: &fakeOutput{}}
	orch := orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{})

	s := &Service{
		output:         po,
		orchestrator:   orch,
		sessions:       sessions,
		planningStore:  store,
		planningStates: sync.Map{},
	}

	ctx := t.Context()
	key := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}
	localKey := sessionKey(1, 0, 100)

	// ── Step 1: Simulate /plan ─────────────────────────────
	state := &planning.State{
		Key:       key,
		Status:    planning.StatusActive,
		Phase:     planning.PhaseSpecify,
		CWD:       cwd,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save initial state: %v", err)
	}

	loaded, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after save: %v", err)
	}
	if loaded == nil {
		t.Fatal("state is nil after save")
	}

	s.planningStates.Store(localKey, loaded)

	// ── Step 2: Simulate tool_use ──────────────────────────
	target := filepath.Join(cwd, "main.go")
	ev := bridge.Event{
		Type: "tool_use",
		Name: "Write",
		Input: map[string]interface{}{
			"path":    target,
			"content": "package main",
		},
	}
	s.observeToolUse(1, 0, 100, ev)

	if len(loaded.Materialized) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(loaded.Materialized))
	}

	// Wait for async save
	time.Sleep(50 * time.Millisecond)

	// Reload to get latest version
	fromStore, _ := store.Get(ctx, key)
	s.planningStates.Store(localKey, fromStore)

	// ── Step 3: Reconcile ──────────────────────────────────
	if err := os.WriteFile(target, []byte("package main"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s.reconcileArtifacts(1, 0, 100)
	time.Sleep(50 * time.Millisecond)

	reconciled, _ := store.Get(ctx, key)
	s.planningStates.Store(localKey, reconciled)

	// ── Step 4: Mark as awaiting_exec ──────────────────────
	reconciled.Status = planning.StatusAwaitingExec
	reconciled.UpdatedAt = time.Now()
	if err := store.Save(ctx, reconciled); err != nil {
		t.Fatalf("Save awaiting_exec: %v", err)
	}

	saved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after status change: %v", err)
	}
	if saved.Status != planning.StatusAwaitingExec {
		t.Fatalf("Status = %q, want %q", saved.Status, planning.StatusAwaitingExec)
	}

	s.planningStates.Store(localKey, saved)

	// ── Step 5: Execute with panicOutput (simulates failure) ──
	planText := "Plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"T1\",\"description\":\"do it\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	handled, outcome := s.tryExecutePlan(1, 0, 100, planText, 100)
	if !handled {
		t.Fatal("expected handled=true")
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}

	// Wait for async goroutine to panic and recover
	time.Sleep(100 * time.Millisecond)

	// ── Step 6: Verify state is preserved ─────────────────
	preserved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after executor failure: %v", err)
	}
	if preserved == nil {
		t.Fatal("expected state to be preserved on executor failure, but it was deleted")
	}
	if preserved.Status != planning.StatusAwaitingExec {
		t.Fatalf("expected status to remain awaiting_exec, got %v", preserved.Status)
	}
	if len(preserved.Materialized) != 1 {
		t.Fatalf("expected artifacts to be preserved, got %d", len(preserved.Materialized))
	}
}
