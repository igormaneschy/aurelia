package orchestrator

import (
	"time"
)

// ExecutionManifest records the full lifecycle of a plan execution run.
// It is intentionally serializable so persistence/resume can be added later.
type ExecutionManifest struct {
	RunID      string
	RepoRoot   string
	BaseBranch string
	Feature    string
	StartedAt  time.Time
	FinishedAt time.Time
	Tasks      map[string]*TaskRecord
}

// TaskRecord captures the mutable execution state of a single task.
type TaskRecord struct {
	TaskID       string
	Status       TaskStatus
	Attempts     int
	ChangedFiles []string
	Verify       *VerifyResult
	CostUSD      float64
	DurationMs   int64
	Error        string
}

// NewExecutionManifest creates a manifest for a run with all tasks in pending state.
func NewExecutionManifest(runID, repoRoot, baseBranch, feature string, tasks []Task) *ExecutionManifest {
	m := &ExecutionManifest{
		RunID:      runID,
		RepoRoot:   repoRoot,
		BaseBranch:  baseBranch,
		Feature:    feature,
		StartedAt:  time.Now(),
		Tasks:      make(map[string]*TaskRecord, len(tasks)),
	}
	for _, t := range tasks {
		m.Tasks[t.ID] = &TaskRecord{
			TaskID: t.ID,
			Status: TaskPending,
		}
	}
	return m
}

// RecordResult updates the task record from a TaskResult.
func (m *ExecutionManifest) RecordResult(tr TaskResult) {
	rec, ok := m.Tasks[tr.TaskID]
	if !ok {
		return
	}
	rec.Status = tr.Status
	rec.Attempts = tr.Attempts
	rec.ChangedFiles = tr.ChangedFiles
	rec.Verify = tr.Verify
	rec.CostUSD = tr.CostUSD
	rec.DurationMs = tr.DurationMs
	rec.Error = tr.Error
}

// ApprovedResults returns only the task records whose status is TaskApproved.
func (m *ExecutionManifest) ApprovedResults() []*TaskRecord {
	var out []*TaskRecord
	for _, rec := range m.Tasks {
		if rec.Status == TaskApproved {
			out = append(out, rec)
		}
	}
	return out
}

// ApprovedChangedFiles returns the union of all changed files from approved tasks,
// deduplicated and sorted.
func (m *ExecutionManifest) ApprovedChangedFiles() []string {
	seen := make(map[string]bool)
	var files []string
	for _, rec := range m.ApprovedResults() {
		for _, f := range rec.ChangedFiles {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	return files
}

// TotalCost returns the sum of CostUSD across all tasks.
func (m *ExecutionManifest) TotalCost() float64 {
	var total float64
	for _, rec := range m.Tasks {
		total += rec.CostUSD
	}
	return total
}

// TotalDuration returns the sum of DurationMs across all tasks.
func (m *ExecutionManifest) TotalDuration() int64 {
	var total int64
	for _, rec := range m.Tasks {
		total += rec.DurationMs
	}
	return total
}

// TerminalStatuses returns the set of statuses that prevent a task from
// being approved later (failed, skipped, unverified, escalated).
func TerminalStatuses() map[TaskStatus]bool {
	return map[TaskStatus]bool{
		TaskFailed:     true,
		TaskSkipped:    true,
		TaskUnverified: true,
		TaskEscalated:  true,
	}
}
