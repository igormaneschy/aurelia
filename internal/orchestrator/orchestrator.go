package orchestrator

import (
	"context"
	"log"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/security"
)

// BridgeExecutor is the interface the orchestrator needs from the bridge.
type BridgeExecutor interface {
	Execute(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error)
	ExecuteSync(ctx context.Context, req bridge.Request) (*bridge.Event, error)
}

// ExecutionContext carries run metadata through the execution lifecycle.
type ExecutionContext struct {
	RunID      string
	RepoRoot   string
	BaseBranch string
	ChatID     int64
	ThreadID   int
	MessageID  int
	UserID     int64
	Feature    string
	CreatePR   bool
	StartedAt  time.Time
}

// OrchestratorConfig holds configuration for the orchestrator.
type OrchestratorConfig struct {
	MaxConcurrentWorkers int           // default: 3
	DefaultMaxTurns      int           // default: 25
	MaxValidationRetries int           // default: 3
	RepoRoot             string        // project root path
	VerifyTimeout        time.Duration // default: 2m
	SecurityConfig       *security.SecurityConfig // nil → use DefaultConfig()
}

// Orchestrator coordinates the plan→execute→validate→consolidate cycle.
type Orchestrator struct {
	bridge   BridgeExecutor
	worktree *WorktreeManager
	config   OrchestratorConfig
}

// NewOrchestrator creates a new Orchestrator with the given dependencies.
func NewOrchestrator(bridge BridgeExecutor, config OrchestratorConfig) *Orchestrator {
	if config.MaxConcurrentWorkers <= 0 {
		config.MaxConcurrentWorkers = 3
	}
	if config.DefaultMaxTurns <= 0 {
		config.DefaultMaxTurns = 25
	}
	if config.MaxValidationRetries <= 0 {
		config.MaxValidationRetries = 3
	}
	if config.VerifyTimeout <= 0 {
		config.VerifyTimeout = 2 * time.Minute
	}

	var wm *WorktreeManager
	if config.RepoRoot != "" {
		wm = NewWorktreeManager(config.RepoRoot)
		// Clean up stale worktrees from previous runs on startup.
		// This is best-effort and does not prevent startup.
		count, cleanupErr := wm.CleanupAll()
		if count > 0 {
			log.Printf("orchestrator: cleaned up %d stale worktree(s) on startup", count)
		}
		if cleanupErr != nil {
			log.Printf("orchestrator: worktree cleanup on startup: %v", cleanupErr)
		}
	}

	return &Orchestrator{
		bridge:   bridge,
		worktree: wm,
		config:   config,
	}
}

// Config returns the orchestrator configuration.
func (o *Orchestrator) Config() OrchestratorConfig {
	return o.config
}

// WithRepoRoot returns a shallow copy of the Orchestrator with the given
// repoRoot, sharing the same bridge but using a new WorktreeManager that
// operates on repoRoot. The original orchestrator is not mutated.
//
// If repoRoot is empty or matches the current config, the same instance
// is returned (no-op).
func (o *Orchestrator) WithRepoRoot(repoRoot string) *Orchestrator {
	if repoRoot == "" || repoRoot == o.config.RepoRoot {
		return o
	}

	cfg := o.config
	cfg.RepoRoot = repoRoot

	return &Orchestrator{
		bridge:   o.bridge,
		worktree: NewWorktreeManager(repoRoot),
		config:   cfg,
	}
}

// Consolidate calls Aurelia to synthesize the final results from all workers.
func (o *Orchestrator) Consolidate(ctx context.Context, plan *Plan, results []TaskResult, systemPrompt string) (string, error) {
	req := bridge.Request{
		Command: "query",
		Prompt:  systemPrompt,
		Options: bridge.RequestOptions{
			NoUserSettings: true,
			Cwd:            o.config.RepoRoot,
		},
	}

	ev, err := o.bridge.ExecuteSync(ctx, req)
	if err != nil {
		return "", err
	}

	content := ev.Content
	if content == "" {
		content = ev.Text
	}
	return content, nil
}
