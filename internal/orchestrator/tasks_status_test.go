package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateTasksStatus_MarksCompleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.md")

	initial := `# Tasks

### T1: Create interface

**Done when**:
- [ ] Interface defined
- [ ] Types exported

### T2: Implement service

**Done when**:
- [ ] Implements interface
- [ ] Tests pass
`
	_ = os.WriteFile(path, []byte(initial), 0o644)

	results := []TaskResult{
		{TaskID: "T1", Success: true, Status: TaskApproved, DurationMs: 5000},
		{TaskID: "T2", Success: false, Status: TaskFailed, Error: "tests failed", DurationMs: 8000},
	}

	if err := UpdateTasksStatus(path, results); err != nil {
		t.Fatalf("UpdateTasksStatus: %v", err)
	}

	content, _ := os.ReadFile(path)
	s := string(content)

	// T1 checkboxes should be marked (TaskApproved)
	if !strings.Contains(s, "- [x] Interface defined") {
		t.Error("T1 checkbox not marked")
	}
	if !strings.Contains(s, "- [x] Types exported") {
		t.Error("T1 second checkbox not marked")
	}

	// T2 checkboxes should remain unchecked (TaskFailed)
	if strings.Contains(s, "- [x] Implements interface") {
		t.Error("T2 checkbox should not be marked (task failed)")
	}

	// Status summary should be appended
	if !strings.Contains(s, "Execution Status") {
		t.Error("missing execution status section")
	}
	if !strings.Contains(s, "✅ Approved") {
		t.Error("missing approved marker for T1")
	}
	if !strings.Contains(s, "❌ Failed") {
		t.Error("missing failed marker for T2")
	}
}

func TestUpdateTasksStatus_OnlyApproved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.md")

	initial := `# Tasks

### T1: Approved

- [ ] Checkbox

### T2: Failed

- [ ] Checkbox

### T3: Skipped

- [ ] Checkbox

### T4: Escalated

- [ ] Checkbox

### T5: Unverified

- [ ] Checkbox
`
	_ = os.WriteFile(path, []byte(initial), 0o644)

	results := []TaskResult{
		{TaskID: "T1", Status: TaskApproved},
		{TaskID: "T2", Status: TaskFailed},
		{TaskID: "T3", Status: TaskSkipped},
		{TaskID: "T4", Status: TaskEscalated},
		{TaskID: "T5", Status: TaskUnverified},
	}

	if err := UpdateTasksStatus(path, results); err != nil {
		t.Fatalf("UpdateTasksStatus: %v", err)
	}

	content, _ := os.ReadFile(path)
	s := string(content)

	// Only T1 should be checked
	if !strings.Contains(s, "- [x] Checkbox") {
		t.Error("expected at least one checked checkbox for T1")
	}
	// Count checked checkboxes — should be exactly 1
	checkedCount := strings.Count(s, "- [x]")
	if checkedCount != 1 {
		t.Errorf("expected 1 checked checkbox (T1), got %d", checkedCount)
	}

	// Verify status markers
	if !strings.Contains(s, "✅ Approved") {
		t.Error("missing approved marker")
	}
	if !strings.Contains(s, "❌ Failed") {
		t.Error("missing failed marker")
	}
	if !strings.Contains(s, "⏭️ Skipped") {
		t.Error("missing skipped marker")
	}
	if !strings.Contains(s, "🚨 Escalated") {
		t.Error("missing escalated marker")
	}
	if !strings.Contains(s, "⚠️ Unverified") {
		t.Error("missing unverified marker")
	}
}
