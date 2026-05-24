package orchestrator

import (
	"strings"
	"testing"
)

func TestBuildOrchestratorPrompt_ContainsTLC(t *testing.T) {
	prompt := BuildOrchestratorPrompt("You are Aurelia.", []AgentSummary{
		{Name: "qa", Description: "test specialist", Tools: []string{"Read", "Bash"}, ReadOnly: false},
	})

	checks := []string{
		"Spec-Driven Development",
		"SPECIFY",
		"DESIGN",
		"TASKS",
		"IMPLEMENT",
		"Anti-Overengineering",
		"3 months",
		"aurelia-plan",
		"qa",
		"test specialist",
		"You are Aurelia.",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}
}

func TestBuildOrchestratorPrompt_NoAgents(t *testing.T) {
	prompt := BuildOrchestratorPrompt("persona", nil)
	if !strings.Contains(prompt, "No specialized agents") {
		t.Error("should indicate no agents available")
	}
}

func TestBuildExecutionPrompt_ContainsTasksAndSchema(t *testing.T) {
	prompt := BuildExecutionPrompt("## T1: Implement endpoint\n## T2: Write tests", []AgentSummary{
		{Name: "worker", Description: "default", Tools: []string{"Read", "Write"}},
	})

	if !strings.Contains(prompt, "T1: Implement endpoint") {
		t.Error("missing tasks content")
	}
	if !strings.Contains(prompt, "aurelia-plan") {
		t.Error("missing output format")
	}
	if !strings.Contains(prompt, "worker") {
		t.Error("missing agent list")
	}
	if !strings.Contains(prompt, `"feature"`) {
		t.Error("missing feature field in schema")
	}
	if !strings.Contains(prompt, `"create_pr"`) {
		t.Error("missing create_pr field in schema")
	}
	if !strings.Contains(prompt, `"verify"`) {
		t.Error("missing verify field in schema")
	}
}

func TestBuildWorkerPrompt_AllLayers(t *testing.T) {
	prompt := BuildWorkerPrompt(
		"You are a worker.",
		"## Dev Commands\ngo build ./...",
		"## Squad\n| worker | default |",
		"## Problem\nNeed /health endpoint",
		"## Architecture\nREST API",
		Task{ID: "T1", Description: "Implement /health", Prompt: "Create GET /health returning 200 OK"},
		[]Task{
			{ID: "T1", Agent: "worker", Description: "Implement /health"},
			{ID: "T2", Agent: "qa", Description: "Write tests"},
		},
	)

	checks := []string{
		"You are a worker.",
		"CLAUDE.md",
		"go build",
		"AGENTS.md",
		"Feature Specification",
		"Feature Design",
		"Your Task",
		"T1",
		"Implement /health",
		"Focus ONLY",
		"Other Workers",
		"T2",
		"qa",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("worker prompt missing %q", check)
		}
	}

	// The full task body must NOT be in the system prompt (it goes in user message)
	if strings.Contains(prompt, "Create GET /health returning 200 OK") {
		t.Error("worker prompt should NOT embed the full task.Prompt body")
	}
}

func TestBuildWorkerPrompt_NoSiblings(t *testing.T) {
	prompt := BuildWorkerPrompt("agent", "", "", "", "",
		Task{ID: "T1", Description: "solo task", Prompt: "do it"},
		nil,
	)
	if strings.Contains(prompt, "Other Workers") {
		t.Error("should not show sibling section when no siblings")
	}
	if strings.Contains(prompt, "do it") {
		t.Error("worker prompt should NOT embed task.Prompt even when no siblings")
	}
}

func TestBuildWorkerPrompt_DoesNotEmbedTaskBody(t *testing.T) {
	prompt := BuildWorkerPrompt(
		"agent",
		"", "", "", "",
		Task{ID: "T1", Description: "task one", Prompt: "This is the full long prompt with many details and file paths that should not appear in the system prompt."},
		[]Task{
			{ID: "T2", Agent: "qa", Description: "task two", Prompt: "Another long prompt that should not leak."},
		},
	)
	if strings.Contains(prompt, "This is the full long prompt") {
		t.Error("worker prompt embedded current task body")
	}
	if strings.Contains(prompt, "Another long prompt") {
		t.Error("worker prompt embedded sibling task body")
	}
}

func TestBuildValidationPrompt_ContainsChecks(t *testing.T) {
	prompt := BuildValidationPrompt("spec content here", "design content here")

	checks := []string{
		"Quality Gate",
		"Task requirements",
		"Scope discipline",
		"overengineering",
		"approved",
		"issues",
		"should_retry",
		"spec content here",
		"design content here",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("validation prompt missing %q", check)
		}
	}
}

func TestBuildConsolidationPrompt_IncludesResults(t *testing.T) {
	plan := &Plan{Tasks: []Task{
		{ID: "T1", Description: "implement"},
		{ID: "T2", Description: "test"},
	}}
	results := []TaskResult{
		{TaskID: "T1", Success: true, Content: "endpoint created"},
		{TaskID: "T2", Success: false, Error: "tests failed", Content: "2 failures"},
	}

	prompt := BuildConsolidationPrompt("You are Aurelia.", plan, results)

	if !strings.Contains(prompt, "endpoint created") {
		t.Error("missing T1 result content")
	}
	if !strings.Contains(prompt, "tests failed") {
		t.Error("missing T2 error")
	}
	if !strings.Contains(prompt, "✅ Success") {
		t.Error("missing success marker")
	}
	if !strings.Contains(prompt, "❌ Failed") {
		t.Error("missing failure marker")
	}
}
