package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/planning"
	"github.com/igormaneschy/aurelia/internal/session"
)

// noopStore is a planning.Store stub that silently discards all operations.
// Used in tests that check synchronous state mutations without real persistence.
type noopStore struct {
	planning.Store
}

func (noopStore) Save(_ context.Context, _ *planning.State) error { return nil }
func (noopStore) Close() error                                    { return nil }

func newPlanningTestService(store planning.Store) *Service {
	return &Service{
		planningStore:  store,
		planningStates: sync.Map{},
	}
}

func TestProcessBridgeEvents_ObserverWrite(t *testing.T) {
	cwd := t.TempDir()

	state := &planning.State{
		Key:    session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100},
		Status: planning.StatusActive,
		CWD:    cwd,
	}
	localKey := sessionKey(1, 0, 100)

	s := newPlanningTestService(&noopStore{})
	s.planningStates.Store(localKey, state)

	target := filepath.Join(cwd, "file.go")
	ev := bridge.Event{
		Type: "tool_use",
		Name: "Write",
		Input: map[string]interface{}{
			"path":    target,
			"content": "package main",
		},
	}

	s.observeToolUse(1, 0, 100, ev)

	if len(state.Materialized) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(state.Materialized))
	}
	if state.Materialized[0].Tool != "Write" {
		t.Errorf("Tool = %q, want %q", state.Materialized[0].Tool, "Write")
	}
	if state.Materialized[0].Path != target {
		t.Errorf("Path = %q, want %q", state.Materialized[0].Path, target)
	}
	if !state.Materialized[0].InsideCWD {
		t.Error("InsideCWD = false, want true")
	}
	if state.Materialized[0].CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want non-zero")
	}
}

func TestProcessBridgeEvents_ObserverEdit(t *testing.T) {
	cwd := t.TempDir()

	state := &planning.State{
		Key:    session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100},
		Status: planning.StatusActive,
		CWD:    cwd,
	}
	localKey := sessionKey(1, 0, 100)

	s := newPlanningTestService(&noopStore{})
	s.planningStates.Store(localKey, state)

	target := filepath.Join(cwd, "main.go")
	ev := bridge.Event{
		Type: "tool_use",
		Name: "Edit",
		Input: map[string]interface{}{
			"path":      target,
			"oldString": "foo",
			"newString": "bar",
		},
	}

	s.observeToolUse(1, 0, 100, ev)

	if len(state.Materialized) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(state.Materialized))
	}
	if state.Materialized[0].Tool != "Edit" {
		t.Errorf("Tool = %q, want %q", state.Materialized[0].Tool, "Edit")
	}
	if state.Materialized[0].Path != target {
		t.Errorf("Path = %q, want %q", state.Materialized[0].Path, target)
	}
}

func TestProcessBridgeEvents_Reconcile(t *testing.T) {
	cwd := t.TempDir()

	state := &planning.State{
		Key:    session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100},
		Status: planning.StatusActive,
		CWD:    cwd,
	}
	localKey := sessionKey(1, 0, 100)

	s := newPlanningTestService(&noopStore{})
	s.planningStates.Store(localKey, state)

	// 1. Simulate a tool_use Write — artifact added, not yet confirmed
	target := filepath.Join(cwd, "exists.go")
	writeEv := bridge.Event{
		Type: "tool_use",
		Name: "Write",
		Input: map[string]interface{}{
			"path":    target,
			"content": "package main",
		},
	}
	s.observeToolUse(1, 0, 100, writeEv)

	if len(state.Materialized) != 1 {
		t.Fatalf("expected 1 artifact after observe, got %d", len(state.Materialized))
	}
	if state.Materialized[0].Confirmed {
		t.Error("artifact was Confirmed before reconcile, want false")
	}

	// 2. Create the file so os.Stat succeeds
	if err := os.WriteFile(target, []byte("data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// 3. Simulate tool_result — reconcile runs
	s.reconcileArtifacts(1, 0, 100)

	if len(state.Materialized) != 1 {
		t.Fatalf("expected 1 artifact after reconcile, got %d", len(state.Materialized))
	}
	if !state.Materialized[0].Confirmed {
		t.Error("existing file artifact: Confirmed = false, want true")
	}
}

func TestProcessBridgeEvents_NoPlanningStore(t *testing.T) {
	s := &Service{
		planningStore:  nil,
		planningStates: sync.Map{},
	}

	// These should not panic when planningStore is nil
	ev := bridge.Event{
		Type: "tool_use",
		Name: "Write",
		Input: map[string]interface{}{
			"path": "/tmp/file.go",
		},
	}

	s.observeToolUse(1, 0, 100, ev)
	s.reconcileArtifacts(1, 0, 100)
	s.savePlanningState(1, 0, 100)
}

func TestProcessBridgeEvents_NoActiveState(t *testing.T) {
	state := &planning.State{
		Key:    session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100},
		Status: planning.StatusCompleted,
		CWD:    t.TempDir(),
	}
	localKey := sessionKey(1, 0, 100)

	s := newPlanningTestService(&noopStore{})
	s.planningStates.Store(localKey, state)

	ev := bridge.Event{
		Type: "tool_use",
		Name: "Write",
		Input: map[string]interface{}{
			"path":    filepath.Join(t.TempDir(), "file.go"),
			"content": "package main",
		},
	}

	s.observeToolUse(1, 0, 100, ev)

	if len(state.Materialized) != 0 {
		t.Errorf("expected 0 artifacts for non-active state, got %d", len(state.Materialized))
	}
}

func TestSavePlanningState(t *testing.T) {
	cwd := t.TempDir()
	store := setupOfferTestStore(t)

	state := &planning.State{
		Key:    session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100},
		Status: planning.StatusActive,
		CWD:    cwd,
	}
	localKey := sessionKey(1, 0, 100)

	s := newPlanningTestService(store)
	s.planningStates.Store(localKey, state)

	// Add artifacts with confirmed=false
	state.Materialized = []planning.Artifact{
		{Path: filepath.Join(cwd, "exists.go"), Tool: "Write", CreatedAt: time.Now()},
	}

	// Create the file so ReconcileArtifacts marks it Confirmed
	if err := os.WriteFile(state.Materialized[0].Path, []byte("data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Final save should reconcile and persist
	s.savePlanningState(1, 0, 100)

	// Verify the state was saved to the store
	saved, err := store.Get(t.Context(), state.Key)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if saved == nil {
		t.Fatal("saved state is nil")
	}
	if len(saved.Materialized) != 1 {
		t.Fatalf("expected 1 artifact in store, got %d", len(saved.Materialized))
	}
	if !saved.Materialized[0].Confirmed {
		t.Error("artifact not Confirmed after savePlanningState")
	}
}

func TestSavePlanningState_NoState(t *testing.T) {
	s := newPlanningTestService(&noopStore{})
	s.savePlanningState(1, 0, 100)
}
