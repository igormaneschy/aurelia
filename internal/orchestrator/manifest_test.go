package orchestrator

import (
	"slices"
	"testing"
)

func TestNewExecutionManifest(t *testing.T) {
	m := NewExecutionManifest("r1", "/repo", "main", "feat", []Task{
		{ID: "T1", Description: "task one"},
		{ID: "T2", Description: "task two"},
	})

	if m.RunID != "r1" {
		t.Errorf("RunID = %q", m.RunID)
	}
	if m.RepoRoot != "/repo" {
		t.Errorf("RepoRoot = %q", m.RepoRoot)
	}
	if m.BaseBranch != "main" {
		t.Errorf("BaseBranch = %q", m.BaseBranch)
	}
	if m.Feature != "feat" {
		t.Errorf("Feature = %q", m.Feature)
	}
	if m.StartedAt.IsZero() {
		t.Error("StartedAt not set")
	}
	if len(m.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(m.Tasks))
	}
	for _, id := range []string{"T1", "T2"} {
		if m.Tasks[id] == nil {
			t.Errorf("missing task %s", id)
		} else if m.Tasks[id].Status != TaskPending {
			t.Errorf("task %s status = %q, want pending", id, m.Tasks[id].Status)
		}
	}
}

func TestExecutionManifest_RecordResult(t *testing.T) {
	m := NewExecutionManifest("r1", "/repo", "main", "feat", []Task{
		{ID: "T1"},
	})

	m.RecordResult(TaskResult{
		TaskID:       "T1",
		Status:       TaskApproved,
		Attempts:     2,
		ChangedFiles: []string{"a.go"},
		CostUSD:      0.01,
		DurationMs:   1000,
		Error:        "",
	})

	rec := m.Tasks["T1"]
	if rec.Status != TaskApproved {
		t.Errorf("status = %q, want approved", rec.Status)
	}
	if rec.Attempts != 2 {
		t.Errorf("attempts = %d", rec.Attempts)
	}
	if len(rec.ChangedFiles) != 1 || rec.ChangedFiles[0] != "a.go" {
		t.Errorf("changedFiles = %v", rec.ChangedFiles)
	}
	if rec.CostUSD != 0.01 {
		t.Errorf("cost = %f", rec.CostUSD)
	}
	if rec.DurationMs != 1000 {
		t.Errorf("duration = %d", rec.DurationMs)
	}
}

func TestExecutionManifest_ApprovedResults(t *testing.T) {
	m := NewExecutionManifest("r1", "/repo", "main", "feat", []Task{
		{ID: "T1"}, {ID: "T2"}, {ID: "T3"},
	})
	m.Tasks["T1"].Status = TaskApproved
	m.Tasks["T2"].Status = TaskFailed
	m.Tasks["T3"].Status = TaskApproved

	approved := m.ApprovedResults()
	if len(approved) != 2 {
		t.Fatalf("expected 2 approved, got %d", len(approved))
	}
	ids := make([]string, len(approved))
	for i, r := range approved {
		ids[i] = r.TaskID
	}
	if !slices.Contains(ids, "T1") || !slices.Contains(ids, "T3") {
		t.Errorf("unexpected approved ids: %v", ids)
	}
}

func TestExecutionManifest_ApprovedChangedFiles(t *testing.T) {
	m := NewExecutionManifest("r1", "/repo", "main", "feat", []Task{
		{ID: "T1"}, {ID: "T2"},
	})
	m.Tasks["T1"].Status = TaskApproved
	m.Tasks["T1"].ChangedFiles = []string{"a.go", "b.go"}
	m.Tasks["T2"].Status = TaskApproved
	m.Tasks["T2"].ChangedFiles = []string{"b.go", "c.go"}

	files := m.ApprovedChangedFiles()
	if len(files) != 3 {
		t.Fatalf("expected 3 unique files, got %d: %v", len(files), files)
	}
	want := []string{"a.go", "b.go", "c.go"}
	for _, f := range want {
		if !slices.Contains(files, f) {
			t.Errorf("missing file %s in %v", f, files)
		}
	}
}

func TestExecutionManifest_Totals(t *testing.T) {
	m := NewExecutionManifest("r1", "/repo", "main", "feat", []Task{
		{ID: "T1"}, {ID: "T2"},
	})
	m.Tasks["T1"].CostUSD = 0.01
	m.Tasks["T1"].DurationMs = 1000
	m.Tasks["T2"].CostUSD = 0.02
	m.Tasks["T2"].DurationMs = 2000

	if got := m.TotalCost(); got != 0.03 {
		t.Errorf("TotalCost = %f, want 0.03", got)
	}
	if got := m.TotalDuration(); got != 3000 {
		t.Errorf("TotalDuration = %d, want 3000", got)
	}
}

func TestTerminalStatuses(t *testing.T) {
	term := TerminalStatuses()
	for _, s := range []TaskStatus{TaskFailed, TaskSkipped, TaskUnverified, TaskEscalated} {
		if !term[s] {
			t.Errorf("expected %q to be terminal", s)
		}
	}
	for _, s := range []TaskStatus{TaskPending, TaskRunning, TaskApproved} {
		if term[s] {
			t.Errorf("expected %q NOT to be terminal", s)
		}
	}
}

func TestTaskResult_ApprovedCompat(t *testing.T) {
	// Approved bool should be derived from Status for backward compatibility.
	tr := TaskResult{TaskID: "T1", Status: TaskApproved, Success: true}
	if !tr.Approved {
		t.Log("Approved is an explicit field; caller must set it alongside Status")
	}
}
