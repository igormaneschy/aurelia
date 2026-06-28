package pipeline

import (
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/session"
)

func TestPendingPlan_HappyPath(t *testing.T) {
	// Plan detected -> /execute -> ExecuteApprovedPlan called once
	fo := &fakeOutput{planDone: make(chan struct{})}
	s := &Service{
		output:       fo,
		pendingPlans: make(map[string]*pendingPlan),
	}

	plan := &orchestrator.Plan{
		Feature: "test-feature",
		Tasks: []orchestrator.Task{
			{ID: "1", Description: "task 1", Prompt: "do it"},
		},
	}

	// Store plan as pending
	s.storePendingPlan(1, 0, 100, "/repo/project", 42, plan)

	// Should be pending
	if !s.HasPendingPlan(1, 0, 42) {
		t.Fatal("HasPendingPlan should return true after storePendingPlan")
	}

	// Execute
	if s.ExecutePendingPlan(1, 0, 42) != PlanExecuted {
		t.Fatal("ExecutePendingPlan should return PlanExecuted when plan is pending")
	}

	// Wait for goroutine
	select {
	case <-fo.planDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ExecuteApprovedPlan should be called")
	}

	if !fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should have been called")
	}

	// Should no longer be pending
	if s.HasPendingPlan(1, 0, 42) {
		t.Fatal("HasPendingPlan should return false after ExecutePendingPlan")
	}
}

func TestPendingPlan_CancelDiscards(t *testing.T) {
	// Plan detected -> /cancel -> plan discarded, ExecuteApprovedPlan not called
	fo := &fakeOutput{}
	s := &Service{
		output:       fo,
		pendingPlans: make(map[string]*pendingPlan),
	}

	plan := &orchestrator.Plan{
		Tasks: []orchestrator.Task{
			{ID: "1", Description: "task 1", Prompt: "do it"},
		},
	}

	s.storePendingPlan(1, 0, 100, "/repo/project", 42, plan)

	// Cancel clears the pending plan
	if !s.ClearPendingPlan(1, 0, 42) {
		t.Fatal("ClearPendingPlan should return true when a plan was pending")
	}

	// Should no longer be pending
	if s.HasPendingPlan(1, 0, 42) {
		t.Fatal("HasPendingPlan should return false after ClearPendingPlan")
	}

	// Execute should do nothing
	if s.ExecutePendingPlan(1, 0, 42) != PlanNotFound {
		t.Fatal("ExecutePendingPlan should return PlanNotFound after plan was cancelled")
	}

	if fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should NOT have been called")
	}
}

func TestPendingPlan_UnrelatedMessageDiscards(t *testing.T) {
	// Plan detected -> user sends unrelated text -> plan discarded
	fo := &fakeOutput{}
	s := &Service{
		output:       fo,
		pendingPlans: make(map[string]*pendingPlan),
	}

	plan := &orchestrator.Plan{
		Tasks: []orchestrator.Task{
			{ID: "1", Description: "task 1", Prompt: "do it"},
		},
	}

	s.storePendingPlan(1, 0, 100, "/repo/project", 42, plan)

	// Simulate unrelated message clearing the pending plan
	if !s.ClearPendingPlan(1, 0, 42) {
		t.Fatal("ClearPendingPlan should return true when a plan was pending")
	}

	if s.HasPendingPlan(1, 0, 42) {
		t.Fatal("HasPendingPlan should return false after clearing")
	}

	if fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should NOT have been called")
	}
}

func TestPendingPlan_Expiry(t *testing.T) {
	// Plan detected -> 10 minutes pass -> /execute -> not executed
	fo := &fakeOutput{}
	s := &Service{
		output:       fo,
		pendingPlans: make(map[string]*pendingPlan),
	}

	plan := &orchestrator.Plan{
		Tasks: []orchestrator.Task{
			{ID: "1", Description: "task 1", Prompt: "do it"},
		},
	}

	s.storePendingPlan(1, 0, 100, "/repo/project", 42, plan)

	// Manually set created time to just past expiry
	key := sessionKey(1, 0, 42)
	s.pendingMu.Lock()
	s.pendingPlans[key].createdAt = time.Now().Add(-(pendingPlanExpiry + time.Minute))
	s.pendingMu.Unlock()

	// HasPendingPlan should return false for expired plans
	if s.HasPendingPlan(1, 0, 42) {
		t.Fatal("HasPendingPlan should return false for expired plans")
	}

	// ExecutePendingPlan should return PlanExpired for expired plans
	if s.ExecutePendingPlan(1, 0, 42) != PlanExpired {
		t.Fatal("ExecutePendingPlan should return PlanExpired for expired plans")
	}

	if fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should NOT be called for expired plans")
	}
}

func TestPendingPlan_NoPlanForDifferentSession(t *testing.T) {
	// Pending plan exists for session A; session B has no plan
	s := &Service{
		pendingPlans: make(map[string]*pendingPlan),
	}

	s.storePendingPlan(1, 0, 100, "/repo/project", 42, planWithOneTask())

	if s.HasPendingPlan(2, 0, 99) {
		t.Fatal("HasPendingPlan should return false for a different session")
	}
	if s.ExecutePendingPlan(2, 0, 99) != PlanNotFound {
		t.Fatal("ExecutePendingPlan should return PlanNotFound for a different session")
	}
	if s.ClearPendingPlan(2, 0, 99) {
		t.Fatal("ClearPendingPlan should return false for a different session")
	}

	// Original session should still have its plan
	if !s.HasPendingPlan(1, 0, 42) {
		t.Fatal("HasPendingPlan should still return true for original session")
	}
}

func TestPendingPlan_ClearNonExistent(t *testing.T) {
	// ClearPendingPlan on a session with no pending plan
	s := &Service{
		pendingPlans: make(map[string]*pendingPlan),
	}

	if s.ClearPendingPlan(1, 0, 42) {
		t.Fatal("ClearPendingPlan should return false when no plan exists")
	}
}

func TestPendingPlan_ExecuteNonExistent(t *testing.T) {
	// ExecutePendingPlan on a session with no pending plan
	s := &Service{
		pendingPlans: make(map[string]*pendingPlan),
	}

	if s.ExecutePendingPlan(1, 0, 42) != PlanNotFound {
		t.Fatal("ExecutePendingPlan should return PlanNotFound when no plan exists")
	}
}

func TestPendingPlan_ThreadScoped(t *testing.T) {
	// Pending plans are scoped by chat+thread+user, so different threads
	// on the same chat have independent plans.
	fo := &fakeOutput{planDone: make(chan struct{})}
	s := &Service{
		output:       fo,
		pendingPlans: make(map[string]*pendingPlan),
	}

	s.storePendingPlan(1, 5, 100, "/repo/project", 42, planWithOneTask())
	s.storePendingPlan(1, 6, 101, "/repo/other", 42, planWithOneTask())

	// Both should be pending independently
	if !s.HasPendingPlan(1, 5, 42) {
		t.Fatal("HasPendingPlan should return true for thread 5")
	}
	if !s.HasPendingPlan(1, 6, 42) {
		t.Fatal("HasPendingPlan should return true for thread 6")
	}

	// Clear thread 5 only
	s.ClearPendingPlan(1, 5, 42)
	if s.HasPendingPlan(1, 5, 42) {
		t.Fatal("HasPendingPlan should return false for cleared thread 5")
	}
	if !s.HasPendingPlan(1, 6, 42) {
		t.Fatal("HasPendingPlan should still return true for thread 6")
	}
}

func TestPendingPlan_SessionKey(t *testing.T) {
	// Verify sessionKey works the same for pending plans as for active sessions
	s := &Service{
		pendingPlans: make(map[string]*pendingPlan),
	}

	s.storePendingPlan(1, 0, 100, "/repo", 42, planWithOneTask())

	// Use sessionKey to verify map key format compatibility
	key := sessionKey(1, 0, 42)
	s.pendingMu.RLock()
	pp, ok := s.pendingPlans[key]
	s.pendingMu.RUnlock()

	if !ok {
		t.Fatal("pending plan should be stored with sessionKey format")
	}
	if pp.chatID != 1 {
		t.Errorf("chatID = %d, want 1", pp.chatID)
	}
	if pp.messageID != 100 {
		t.Errorf("messageID = %d, want 100", pp.messageID)
	}
	if pp.cwd != "/repo" {
		t.Errorf("cwd = %q, want /repo", pp.cwd)
	}
}

// planWithOneTask returns a minimal valid plan for testing.
func planWithOneTask() *orchestrator.Plan {
	return &orchestrator.Plan{
		Tasks: []orchestrator.Task{
			{ID: "1", Description: "test", Prompt: "do it"},
		},
	}
}

// Test that storePendingPlan tracks cwd from tryExecutePlan.
// The full integration: tryExecutePlan -> storePendingPlan -> ExecutePendingPlan.
func TestPendingPlan_IntegrationViaTryExecutePlan(t *testing.T) {
	sessions := session.NewStore()
	sessions.SetCwd(1, 0, "/repo/integration")

	fo := &fakeOutput{planDone: make(chan struct{})}
	s := &Service{
		output:        fo,
		orchestrator:  orchestrator.NewOrchestrator(nil, orchestrator.OrchestratorConfig{}),
		sessions:      sessions,
		pendingPlans:  make(map[string]*pendingPlan),
	}

	planText := "Plan:\n```aurelia-plan\n{\"tasks\":[{\"id\":\"1\",\"description\":\"task\",\"prompt\":\"do it\"}]}\n```\n"
	handled, _ := s.tryExecutePlan(1, 0, 100, planText, 42, false)
	if !handled {
		t.Fatal("tryExecutePlan should return handled=true when plan is valid")
	}
	if fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should NOT be called immediately")
	}

	// Plan should be pending with correct CWD
	if !s.HasPendingPlan(1, 0, 42) {
		t.Fatal("plan should be pending after tryExecutePlan")
	}

	// Now execute via ExecutePendingPlan
	if s.ExecutePendingPlan(1, 0, 42) != PlanExecuted {
		t.Fatal("ExecutePendingPlan should return PlanExecuted")
	}

	select {
	case <-fo.planDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ExecuteApprovedPlan should be called after ExecutePendingPlan")
	}
	if !fo.planExecuted {
		t.Fatal("ExecuteApprovedPlan should have been called")
	}
	if fo.planCwd != "/repo/integration" {
		t.Errorf("planCwd = %q, want /repo/integration", fo.planCwd)
	}
}
