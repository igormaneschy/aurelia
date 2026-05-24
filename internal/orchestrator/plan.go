package orchestrator

import (
	"encoding/json"
	"fmt"
)

// Plan is the structured execution plan returned by Aurelia.
type Plan struct {
	Feature  string `json:"feature,omitempty"`   // e.g. "agent-orchestration-execution"
	CreatePR bool   `json:"create_pr,omitempty"` // true when user asked to open a PR
	Verify   string `json:"verify,omitempty"`    // optional default verify command
	Tasks    []Task `json:"tasks"`
}

// Task is an atomic subtask in the execution plan.
type Task struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	Agent         string   `json:"agent"`
	Prompt        string   `json:"prompt"`
	DependsOn     []string `json:"depends_on,omitempty"`
	NeedsWorktree bool     `json:"needs_worktree"`
	Verify        string   `json:"verify,omitempty"` // optional per-task verify command (overrides plan.Verify)
}

// TaskStatus represents the lifecycle state of a task in the execution manifest.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskRunning    TaskStatus = "running"
	TaskApproved   TaskStatus = "approved"
	TaskFailed     TaskStatus = "failed"
	TaskSkipped    TaskStatus = "skipped"
	TaskUnverified TaskStatus = "unverified"
	TaskEscalated  TaskStatus = "escalated"
)

// TaskResult holds the outcome of a single worker execution.
type TaskResult struct {
	TaskID       string
	Content      string
	Success      bool       // bridge call succeeded (compat)
	Status       TaskStatus // approved, failed, skipped, unverified, escalated
	Approved     bool       // validation passed (derived from Status)
	Skipped      bool       // dependency/merge/preflight skip
	Attempts     int        // number of execution attempts
	ChangedFiles []string
	Verify       *VerifyResult
	DurationMs   int64
	CostUSD      float64
	Error        string
}

// WorkerEvent is emitted during execution for visual feedback.
type WorkerEvent struct {
	TaskID   string
	Type     string // "start", "progress", "done", "error"
	ToolName string
	Message  string
}

// WorkerConfig holds resolved configuration for a worker.
type WorkerConfig struct {
	Model             string
	MaxTurns          int
	Tools             []string
	DisallowedTools   []string
	CapabilityProfile string
	Prompt            string
}

// ParsePlan parses a JSON plan from bytes.
func ParsePlan(data []byte) (*Plan, error) {
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing plan JSON: %w", err)
	}
	if len(p.Tasks) == 0 {
		return nil, fmt.Errorf("plan has no tasks")
	}
	return &p, nil
}

// ExecutionOrder returns tasks grouped by wave for parallel execution.
// Tasks within a wave have all dependencies satisfied by previous waves.
// Returns error if there are circular dependencies.
func (p *Plan) ExecutionOrder() ([][]Task, error) {
	taskByID := make(map[string]Task, len(p.Tasks))
	for _, t := range p.Tasks {
		taskByID[t.ID] = t
	}

	completed := make(map[string]bool)
	remaining := make(map[string]Task, len(p.Tasks))
	for _, t := range p.Tasks {
		remaining[t.ID] = t
	}

	var waves [][]Task

	for len(remaining) > 0 {
		var wave []Task
		for id, t := range remaining {
			ready := true
			for _, dep := range t.DependsOn {
				if !completed[dep] {
					ready = false
					break
				}
			}
			if ready {
				wave = append(wave, t)
				_ = id // used in loop
			}
		}

		if len(wave) == 0 {
			// Remaining tasks all have unsatisfied deps → circular
			ids := make([]string, 0, len(remaining))
			for id := range remaining {
				ids = append(ids, id)
			}
			return nil, fmt.Errorf("circular dependency detected among tasks: %v", ids)
		}

		for _, t := range wave {
			completed[t.ID] = true
			delete(remaining, t.ID)
		}
		waves = append(waves, wave)
	}

	return waves, nil
}
