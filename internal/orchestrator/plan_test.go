package orchestrator

import (
	"testing"
)

func TestExecutionOrder_NoDependencies(t *testing.T) {
	p := &Plan{Tasks: []Task{
		{ID: "1", Description: "task 1"},
		{ID: "2", Description: "task 2"},
		{ID: "3", Description: "task 3"},
	}}

	waves, err := p.ExecutionOrder()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(waves) != 1 {
		t.Fatalf("expected 1 wave, got %d", len(waves))
	}
	if len(waves[0]) != 3 {
		t.Errorf("expected 3 tasks in wave 1, got %d", len(waves[0]))
	}
}

func TestExecutionOrder_LinearDependencies(t *testing.T) {
	p := &Plan{Tasks: []Task{
		{ID: "1", Description: "first"},
		{ID: "2", Description: "second", DependsOn: []string{"1"}},
		{ID: "3", Description: "third", DependsOn: []string{"2"}},
	}}

	waves, err := p.ExecutionOrder()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(waves) != 3 {
		t.Fatalf("expected 3 waves, got %d", len(waves))
	}
	if waves[0][0].ID != "1" {
		t.Errorf("wave 1 should be task 1, got %s", waves[0][0].ID)
	}
	if waves[1][0].ID != "2" {
		t.Errorf("wave 2 should be task 2, got %s", waves[1][0].ID)
	}
	if waves[2][0].ID != "3" {
		t.Errorf("wave 3 should be task 3, got %s", waves[2][0].ID)
	}
}

func TestExecutionOrder_ParallelAfterFoundation(t *testing.T) {
	p := &Plan{Tasks: []Task{
		{ID: "1", Description: "foundation"},
		{ID: "2", Description: "parallel A", DependsOn: []string{"1"}},
		{ID: "3", Description: "parallel B", DependsOn: []string{"1"}},
		{ID: "4", Description: "final", DependsOn: []string{"2", "3"}},
	}}

	waves, err := p.ExecutionOrder()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(waves) != 3 {
		t.Fatalf("expected 3 waves, got %d", len(waves))
	}
	// Wave 1: task 1
	if len(waves[0]) != 1 || waves[0][0].ID != "1" {
		t.Errorf("wave 1 should be [1], got %v", taskIDs(waves[0]))
	}
	// Wave 2: tasks 2 and 3 (parallel)
	if len(waves[1]) != 2 {
		t.Errorf("wave 2 should have 2 tasks, got %d", len(waves[1]))
	}
	// Wave 3: task 4
	if len(waves[2]) != 1 || waves[2][0].ID != "4" {
		t.Errorf("wave 3 should be [4], got %v", taskIDs(waves[2]))
	}
}

func TestExecutionOrder_CircularDependency(t *testing.T) {
	p := &Plan{Tasks: []Task{
		{ID: "1", Description: "A", DependsOn: []string{"2"}},
		{ID: "2", Description: "B", DependsOn: []string{"1"}},
	}}

	_, err := p.ExecutionOrder()
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}
}

func TestParsePlan_Valid(t *testing.T) {
	data := []byte(`{"tasks":[{"id":"1","description":"test","agent":"worker","prompt":"do it","needs_worktree":true}]}`)
	p, err := ParsePlan(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(p.Tasks))
	}
	if p.Tasks[0].Agent != "worker" {
		t.Errorf("agent = %q, want worker", p.Tasks[0].Agent)
	}
	if !p.Tasks[0].NeedsWorktree {
		t.Error("expected NeedsWorktree = true")
	}
}

func TestParsePlan_Empty(t *testing.T) {
	data := []byte(`{"tasks":[]}`)
	_, err := ParsePlan(data)
	if err == nil {
		t.Fatal("expected error for empty tasks")
	}
}

func TestParsePlan_InvalidJSON(t *testing.T) {
	_, err := ParsePlan([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParsePlan_BackwardCompatibility(t *testing.T) {
	// Old plans without Feature/CreatePR/Verify/Task.Verify should still parse
	data := []byte(`{"tasks":[{"id":"1","description":"test","agent":"worker","prompt":"do it","needs_worktree":true}]}`)
	p, err := ParsePlan(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Feature != "" {
		t.Errorf("unexpected feature: %q", p.Feature)
	}
	if p.CreatePR {
		t.Error("unexpected create_pr = true")
	}
	if p.Verify != "" {
		t.Errorf("unexpected verify: %q", p.Verify)
	}
	if len(p.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(p.Tasks))
	}
	if p.Tasks[0].Verify != "" {
		t.Errorf("unexpected task verify: %q", p.Tasks[0].Verify)
	}
}

func taskIDs(tasks []Task) []string {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return ids
}
