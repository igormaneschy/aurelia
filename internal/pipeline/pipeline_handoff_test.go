package pipeline

import (
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/planning"
	"github.com/igormaneschy/aurelia/internal/session"
)

// panicOutput panics in ExecuteApprovedPlan to simulate execution failure.
type panicOutput struct {
	*fakeOutput
}

func (panicOutput) ExecuteApprovedPlan(_ int64, _ int, _ int, _ string, _ int64, _ *orchestrator.Plan) {
	panic("simulated execution failure")
}

func TestTryExecutePlan_AwaitingExec(t *testing.T) {
	// State is awaiting_exec — plan should execute normally.
	store := setupOfferTestStore(t)
	stateKey := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}

	state := &planning.State{
		Key:    stateKey,
		Status: planning.StatusAwaitingExec,
	}
	if err := store.Save(t.Context(), state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	sessions := session.NewStore()
	sessions.SetCwd(1, 0, "/repo/project")

	fo := &fakeOutput{}
	s := &Service{
		output:        fo,
		orchestrator:  orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:      sessions,
		planningStore: store,
	}

	planText := "Plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"T1\",\"description\":\"t1\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	handled, outcome := s.tryExecutePlan(1, 0, 100, planText, 100)

	if !handled {
		t.Fatal("expected handled=true")
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}

	// Wait for async goroutine
	for i := 0; i < 100 && !fo.planExecuted; i++ {
		time.Sleep(time.Millisecond)
	}
	if !fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should be called when state is awaiting_exec")
	}
}

func TestTryExecutePlan_NotApproved(t *testing.T) {
	// State is active but not awaiting_exec — plan should be blocked.
	store := setupOfferTestStore(t)
	stateKey := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}

	state := &planning.State{
		Key:    stateKey,
		Status: planning.StatusActive, // not awaiting_exec
	}
	if err := store.Save(t.Context(), state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	sessions := session.NewStore()
	sessions.SetCwd(1, 0, "/repo/project")

	fo := &fakeOutput{}
	s := &Service{
		output:        fo,
		orchestrator:  orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:      sessions,
		planningStore: store,
	}

	planText := "Plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"T1\",\"description\":\"t1\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	handled, outcome := s.tryExecutePlan(1, 0, 100, planText, 100)

	if !handled {
		t.Fatal("expected handled=true")
	}
	if outcome != OutcomePlanBlocked {
		t.Fatalf("expected OutcomePlanBlocked, got %v", outcome)
	}

	// ExecuteApprovedPlan must NOT be called when blocked
	time.Sleep(10 * time.Millisecond)
	if fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should NOT be called when plan is not approved")
	}
}

func TestTryExecutePlan_NoState(t *testing.T) {
	// No planning state — should fall through to legacy behavior.
	sessions := session.NewStore()
	sessions.SetCwd(1, 0, "/repo/project")

	fo := &fakeOutput{}
	s := &Service{
		output:        fo,
		orchestrator:  orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:      sessions,
		planningStore: nil, // no planning store
	}

	planText := "Plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"T1\",\"description\":\"t1\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	handled, outcome := s.tryExecutePlan(1, 0, 100, planText, 100)

	if !handled {
		t.Fatal("expected handled=true")
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}

	// Wait for async goroutine
	for i := 0; i < 100 && !fo.planExecuted; i++ {
		time.Sleep(time.Millisecond)
	}
	if !fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should be called in legacy mode")
	}
}

func TestTryExecutePlan_Cleanup(t *testing.T) {
	// Successful execution must delete planning state.
	store := setupOfferTestStore(t)
	stateKey := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}

	state := &planning.State{
		Key:    stateKey,
		Status: planning.StatusAwaitingExec,
	}
	if err := store.Save(t.Context(), state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	sessions := session.NewStore()
	sessions.SetCwd(1, 0, "/repo/project")

	fo := &fakeOutput{}
	s := &Service{
		output:        fo,
		orchestrator:  orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:      sessions,
		planningStore: store,
	}

	planText := "Plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"T1\",\"description\":\"t1\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	_, _ = s.tryExecutePlan(1, 0, 100, planText, 100)

	// Wait for async goroutine to complete (ExecuteApprovedPlan + cleanup)
	for i := 0; i < 200 && !fo.planExecuted; i++ {
		time.Sleep(time.Millisecond)
	}
	// Extra wait for cleanup to run after ExecuteApprovedPlan
	time.Sleep(50 * time.Millisecond)

	// Verify state was deleted
	saved, err := store.Get(t.Context(), stateKey)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if saved != nil {
		t.Fatal("expected state to be deleted after successful execution, but it still exists")
	}
}

func TestTryExecutePlan_PreserveOnFailure(t *testing.T) {
	// When execution fails (panics in this simulation),
	// planning state must be preserved (not deleted).
	store := setupOfferTestStore(t)
	stateKey := session.SessionKey{ChatID: 1, ThreadID: 0, UserID: 100}

	state := &planning.State{
		Key:    stateKey,
		Status: planning.StatusAwaitingExec,
	}
	if err := store.Save(t.Context(), state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	sessions := session.NewStore()
	sessions.SetCwd(1, 0, "/repo/project")

	po := &panicOutput{fakeOutput: &fakeOutput{}}
	s := &Service{
		output:        po,
		orchestrator:  orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:      sessions,
		planningStore: store,
	}

	planText := "Plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"T1\",\"description\":\"t1\",\"prompt\":\"do it\",\"needs_worktree\":false}]}\n```\n"
	handled, outcome := s.tryExecutePlan(1, 0, 100, planText, 100)

	if !handled {
		t.Fatal("expected handled=true (plan was extracted)")
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %v", outcome)
	}

	// Wait long enough for goroutine to panic and recover
	time.Sleep(100 * time.Millisecond)

	// Verify state was NOT deleted (preserved on failure)
	saved, err := store.Get(t.Context(), stateKey)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if saved == nil {
		t.Fatal("expected state to be preserved on failure, but it was deleted")
	}
	if saved.Status != planning.StatusAwaitingExec {
		t.Fatalf("expected status to remain awaiting_exec, got %v", saved.Status)
	}
}
