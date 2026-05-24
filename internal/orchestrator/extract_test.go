package orchestrator

import "testing"

func TestExtractPlan_ValidPlan(t *testing.T) {
	response := `Vou executar o plano aprovado:

` + "```aurelia-plan" + `
{"tasks":[{"id":"1","description":"Implement /health","agent":"worker","prompt":"Create GET /health endpoint","needs_worktree":true}]}
` + "```" + `

Vou começar agora.`

	plan, err := ExtractPlanFromText(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan == nil {
		t.Fatal("expected plan, got nil")
		return
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].ID != "1" {
		t.Errorf("task ID = %q, want 1", plan.Tasks[0].ID)
	}
}

func TestExtractPlan_NoMarker(t *testing.T) {
	response := "Just a normal response without any plan."
	plan, err := ExtractPlanFromText(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan != nil {
		t.Error("expected nil plan for response without marker")
	}
}

func TestContainsPlanMarker(t *testing.T) {
	response := "intro\n```aurelia-plan\n{not closed"
	if !ContainsPlanMarker(response) {
		t.Fatal("expected marker to be detected")
	}
	if ContainsPlanMarker("normal response") {
		t.Fatal("did not expect marker in normal response")
	}
}

func TestStripPlanBlock_IncompleteBlock(t *testing.T) {
	response := "Vou executar.\n\n```aurelia-plan\n{\"tasks\":["
	got := StripPlanBlock(response)
	if got != "Vou executar." {
		t.Fatalf("StripPlanBlock() = %q, want %q", got, "Vou executar.")
	}
}

func TestExtractPlan_InvalidJSON(t *testing.T) {
	response := "```aurelia-plan\n{not valid json}\n```"
	_, err := ExtractPlanFromText(response)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExtractPlan_EmptyBlock(t *testing.T) {
	response := "```aurelia-plan\n\n```"
	plan, err := ExtractPlanFromText(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan != nil {
		t.Error("expected nil plan for empty block")
	}
}

func TestExtractPlan_MultipleTasks(t *testing.T) {
	response := "```aurelia-plan\n" + `{"tasks":[
		{"id":"1","description":"implement","agent":"worker","prompt":"do it","needs_worktree":true},
		{"id":"2","description":"test","agent":"qa","prompt":"test it","depends_on":["1"],"needs_worktree":true},
		{"id":"3","description":"review","agent":"code-reviewer","prompt":"review it","depends_on":["2"],"needs_worktree":false}
	]}` + "\n```"

	plan, err := ExtractPlanFromText(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(plan.Tasks))
	}
	if plan.Tasks[1].Agent != "qa" {
		t.Errorf("task 2 agent = %q, want qa", plan.Tasks[1].Agent)
	}
	if len(plan.Tasks[2].DependsOn) != 1 || plan.Tasks[2].DependsOn[0] != "2" {
		t.Errorf("task 3 depends_on = %v", plan.Tasks[2].DependsOn)
	}
}

func TestExtractPlan_WithFeatureCreatePRVerify(t *testing.T) {
	response := "```aurelia-plan\n" + `{"feature":"my-feature","create_pr":true,"verify":"go test ./...","tasks":[{"id":"1","description":"do it","agent":"worker","prompt":"write code","needs_worktree":true,"verify":"go vet ./..."}]}` + "\n```"

	plan, err := ExtractPlanFromText(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan == nil {
		t.Fatal("expected plan")
	}
	if plan.Feature != "my-feature" {
		t.Errorf("feature = %q, want my-feature", plan.Feature)
	}
	if !plan.CreatePR {
		t.Error("expected create_pr = true")
	}
	if plan.Verify != "go test ./..." {
		t.Errorf("verify = %q, want go test ./...", plan.Verify)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].Verify != "go vet ./..." {
		t.Errorf("task verify = %q, want go vet ./...", plan.Tasks[0].Verify)
	}
}

func TestStripPlanBlock(t *testing.T) {
	response := "Here is my plan:\n\n```aurelia-plan\n{\"tasks\":[]}\n```\n\nStarting now."
	got := StripPlanBlock(response)
	want := "Here is my plan:\n\nStarting now."
	if got != want {
		t.Errorf("StripPlanBlock = %q, want %q", got, want)
	}
}

func TestStripPlanBlock_NoMarker(t *testing.T) {
	response := "Normal response."
	got := StripPlanBlock(response)
	if got != response {
		t.Errorf("StripPlanBlock should return original, got %q", got)
	}
}
