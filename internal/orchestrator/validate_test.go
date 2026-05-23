package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

func TestValidate_FailedWorker(t *testing.T) {
	o := NewOrchestrator(newFakeBridge(), OrchestratorConfig{})

	vr, err := o.Validate(
		context.Background(),
		Task{ID: "1", Description: "test"},
		TaskResult{TaskID: "1", Success: false, Error: "timeout"},
		nil,
		"validate prompt",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vr.Approved {
		t.Error("should not approve failed worker")
	}
	if !vr.ShouldRetry {
		t.Error("should suggest retry for failed worker")
	}
}

func TestValidate_SetsCwd(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{
		Type:    "result",
		Content: `{"approved": true, "issues": [], "should_retry": false}`,
	})
	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: "/repo/test"})

	vr, err := o.Validate(
		context.Background(),
		Task{ID: "1", Prompt: "implement X"},
		TaskResult{TaskID: "1", Success: true, Content: "done"},
		nil,
		"validate prompt",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vr.Approved {
		t.Error("expected approved")
	}

	req := fb.LastRequest()
	if req.Options.Cwd != "/repo/test" {
		t.Errorf("Validate bridge request Cwd = %q, want %q", req.Options.Cwd, "/repo/test")
	}
}

func TestValidate_ApprovedByBridge(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{
		Type:    "result",
		Content: `{"approved": true, "issues": [], "should_retry": false}`,
	})
	o := NewOrchestrator(fb, OrchestratorConfig{})

	vr, err := o.Validate(
		context.Background(),
		Task{ID: "1", Description: "test", Prompt: "implement X"},
		TaskResult{TaskID: "1", Success: true, Content: "implemented X"},
		nil,
		"validate prompt",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vr.Approved {
		t.Error("expected approved")
	}
}

func TestValidate_RejectedByBridge(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{
		Type:    "result",
		Content: `{"approved": false, "issues": ["missing error handling", "no tests"], "should_retry": true}`,
	})
	o := NewOrchestrator(fb, OrchestratorConfig{})

	vr, err := o.Validate(
		context.Background(),
		Task{ID: "1"},
		TaskResult{TaskID: "1", Success: true, Content: "did stuff"},
		nil,
		"validate",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vr.Approved {
		t.Error("should not approve")
	}
	if len(vr.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(vr.Issues))
	}
	if !vr.ShouldRetry {
		t.Error("should suggest retry")
	}
}

func TestValidateBridgeFailure_IsNotApproved(t *testing.T) {
	fb := newFakeBridge()
	fb.SetSyncErr(fmt.Errorf("bridge timeout"))
	o := NewOrchestrator(fb, OrchestratorConfig{})

	vr, err := o.Validate(
		context.Background(),
		Task{ID: "1"},
		TaskResult{TaskID: "1", Success: true, Content: "done"},
		nil,
		"validate prompt",
	)
	if err == nil {
		t.Fatal("expected error for bridge failure, got nil")
	}
	if vr != nil {
		t.Error("expected nil ValidationResult on bridge failure")
	}
}

func TestValidate_ReceivesDiffAndVerifyOutput(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{
		Type:    "result",
		Content: `{"approved": true, "issues": [], "should_retry": false}`,
	})
	o := NewOrchestrator(fb, OrchestratorConfig{})

	artifacts := &ArtifactSnapshot{
		ChangedFiles: []string{"main.go"},
		DiffStat:     " main.go | 1 +\n",
		Diff:         "diff --git a/main.go b/main.go\n+func main() {}",
		Verify: &VerifyResult{
			Command:  "go test ./...",
			ExitCode: 0,
			Stdout:   "ok",
		},
	}

	vr, err := o.Validate(
		context.Background(),
		Task{ID: "1", Description: "implement main", Prompt: "add main func"},
		TaskResult{TaskID: "1", Success: true, Content: "done"},
		artifacts,
		"validate prompt",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vr.Approved {
		t.Error("expected approved")
	}

	req := fb.LastRequest()
	prompt := req.Prompt
	if !strings.Contains(prompt, "Changed Files") {
		t.Error("validation prompt should include changed files")
	}
	if !strings.Contains(prompt, "main.go") {
		t.Error("validation prompt should include file name")
	}
	if !strings.Contains(prompt, "Diff Stat") {
		t.Error("validation prompt should include diffstat")
	}
	if !strings.Contains(prompt, "Diff") {
		t.Error("validation prompt should include diff")
	}
	if !strings.Contains(prompt, "Verify Result") {
		t.Error("validation prompt should include verify result")
	}
	if !strings.Contains(prompt, "go test ./...") {
		t.Error("validation prompt should include verify command")
	}
}

func TestValidate_EmptyDiffRejectedForWriteTask(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{
		Type:    "result",
		Content: `{"approved": true, "issues": [], "should_retry": false}`,
	})
	o := NewOrchestrator(fb, OrchestratorConfig{})

	// Write task with empty artifacts (no changes)
	artifacts := &ArtifactSnapshot{ChangedFiles: []string{}}

	vr, err := o.Validate(
		context.Background(),
		Task{ID: "1", Description: "write file", Prompt: "create foo.go", NeedsWorktree: true},
		TaskResult{TaskID: "1", Success: true, Content: "done"},
		artifacts,
		"validate prompt",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The bridge approved, but we check that the prompt included the warning
	req := fb.LastRequest()
	if !strings.Contains(req.Prompt, "no changes were detected") {
		t.Error("validation prompt should warn about empty diff for write task")
	}
	if !vr.Approved {
		t.Error("expected approved (bridge said yes, prompt warning is advisory)")
	}
}

func TestParseValidationResponse_JSON(t *testing.T) {
	input := `{"approved": true, "issues": [], "should_retry": false}`
	vr := parseValidationResponse(input)
	if !vr.Approved {
		t.Error("expected approved")
	}
}

func TestParseValidationResponse_JSONInText(t *testing.T) {
	input := "Here is the result:\n\n" + `{"approved": false, "issues": ["bad code"], "should_retry": true}` + "\n\nEnd."
	vr := parseValidationResponse(input)
	if vr.Approved {
		t.Error("expected not approved")
	}
	if len(vr.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(vr.Issues))
	}
}

func TestParseValidationResponse_HeuristicApproved(t *testing.T) {
	input := "The work looks great. Approved!"
	vr := parseValidationResponse(input)
	if !vr.Approved {
		t.Error("expected approved via heuristic")
	}
}

func TestParseValidationResponse_Unparseable(t *testing.T) {
	input := "Some random text without clear signal."
	vr := parseValidationResponse(input)
	if vr.Approved {
		t.Error("expected not approved for unparseable response")
	}
	if !vr.ShouldRetry {
		t.Error("should suggest retry for unparseable")
	}
}

func TestConsolidate_SetsCwd(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{Type: "result", Content: "consolidated"})

	o := NewOrchestrator(fb, OrchestratorConfig{RepoRoot: "/repo/consolidate"})

	text, err := o.Consolidate(context.Background(), &Plan{}, nil, "consolidate prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "consolidated" {
		t.Errorf("Consolidate returned %q, want %q", text, "consolidated")
	}

	req := fb.LastRequest()
	if req.Options.Cwd != "/repo/consolidate" {
		t.Errorf("Consolidate bridge request Cwd = %q, want %q", req.Options.Cwd, "/repo/consolidate")
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`{"a": 1}`, `{"a": 1}`},
		{`text {"a": {"b": 2}} more`, `{"a": {"b": 2}}`},
		{`no json here`, ""},
		{`{unclosed`, ""},
	}
	for _, tt := range tests {
		got := extractJSON(tt.input)
		if got != tt.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
