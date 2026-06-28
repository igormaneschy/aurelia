package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/profiles"

	"gopkg.in/telebot.v3"
)

const orchestrationTimeout = 30 * time.Minute

// executeApprovedPlan runs the full TLC Implement+Validate cycle:
// ensure docs → spawn workers per wave → validate → merge → consolidate.
// repoRoot is the chat's effective working directory (from handoff cwd).
// All user-facing errors are sent to the original thread.
func (bc *BotController) executeApprovedPlan(chat *telebot.Chat, threadID int, messageID int, repoRoot string, userID int64, plan *orchestrator.Plan) {
	ctx, cancel := context.WithTimeout(context.Background(), orchestrationTimeout)
	defer cancel()

	// Safety gate: run preflight before any docs or worker operations.
	// Preflight checks the handoff repoRoot, not the daemon-configured RepoRoot.
	var runOrch *orchestrator.Orchestrator
	var baseBranch string
	if bc.orchestrator != nil {
		result, err := bc.orchestrator.PreflightExecution(ctx, repoRoot, plan.CreatePR)
		if err != nil {
			log.Printf("PreflightExecution for chat=%d thread=%d: %v", chat.ID, threadID, err)
			_ = SendErrorWithThread(bc.bot, chat, orchestrator.PreflightUserMessage(err), threadID)
			return
		}
		baseBranch = result.BaseBranch

		// Build a run-scoped orchestrator that uses the handoff cwd for all
		// subsequent operations (worktrees, task cwds, merge). This is a
		// shallow copy that shares the bridge; the original is not mutated.
		runOrch = bc.orchestrator.WithRepoRoot(repoRoot)
	} else {
		log.Printf("PreflightExecution for chat=%d thread=%d: no orchestrator configured", chat.ID, threadID)
		_ = SendErrorWithThread(bc.bot, chat, "No orchestrator configured. Cannot execute plan.", threadID)
		return
	}

	// 0. Ensure CLAUDE.md and AGENTS.md exist
	agentSummaries := bc.buildAgentSummaries()
	if err := orchestrator.EnsureClaudeMd(repoRoot); err != nil {
		log.Printf("EnsureClaudeMd: %v", err)
	}
	if err := orchestrator.EnsureAgentsMd(repoRoot, agentSummaries); err != nil {
		log.Printf("EnsureAgentsMd: %v", err)
	}

	// 1. Send plan summary
	status := newWorkerStatusReporter(bc.bot, chat)
	status.SendPlanSummary(plan, messageID)

	// 2. Read context files for worker prompts
	claudeMd := orchestrator.ReadFileContent(filepath.Join(repoRoot, "CLAUDE.md"))
	agentsMd := orchestrator.ReadFileContent(filepath.Join(repoRoot, "AGENTS.md"))
	specContent, designContent := bc.loadFeatureDocs(repoRoot, plan.Feature)

	// 3. Execute workers via run-scoped orchestrator (uses handoff cwd)
	execCtx := orchestrator.ExecutionContext{
		RunID:      orchestrator.NewExecutionManifest("", repoRoot, baseBranch, plan.Feature, plan.Tasks).RunID,
		RepoRoot:   repoRoot,
		BaseBranch: baseBranch,
		ChatID:     chat.ID,
		ThreadID:   threadID,
		MessageID:  messageID,
		UserID:     userID,
		Feature:    plan.Feature,
		CreatePR:   plan.CreatePR,
		StartedAt:  time.Now(),
	}

	validationPrompt := orchestrator.BuildValidationPrompt(specContent, designContent)
	validator := func(ctx context.Context, task orchestrator.Task, result orchestrator.TaskResult, artifacts *orchestrator.ArtifactSnapshot, attempt int) (*orchestrator.ValidationResult, error) {
		return runOrch.Validate(ctx, task, result, artifacts, validationPrompt)
	}

	manifest, results, err := runOrch.ExecutePlan(
		ctx,
		execCtx,
		plan,
		bc.agents,
		func(task orchestrator.Task, cfg orchestrator.WorkerConfig) string {
			waves, _ := plan.ExecutionOrder()
			var siblings []orchestrator.Task
			for _, wave := range waves {
				for _, t := range wave {
					if t.ID != task.ID {
						siblings = append(siblings, t)
					}
				}
			}
			return orchestrator.BuildWorkerPrompt(cfg.Prompt, claudeMd, agentsMd, specContent, designContent, task, siblings)
		},
		validator,
		func(ev orchestrator.WorkerEvent) {
			switch ev.Type {
			case "start":
				status.SendStart(ev.TaskID, ev.Message)
			case "progress":
				status.UpdateProgress(ev.TaskID, ev.ToolName)
			case "done":
				status.MarkDone(ev.TaskID, 0)
			case "error":
				status.MarkError(ev.TaskID, ev.Message)
			case "escalated":
				status.MarkError(ev.TaskID, "Escalated: "+ev.Message)
			case "skipped":
				status.MarkDone(ev.TaskID, 0) // visually mark as done but different meaning
			case "merge_failed":
				status.MarkError(ev.TaskID, "Merge failed: "+ev.Message)
			}
		},
	)
	_ = manifest // used in later slices (commit, PR)

	if err != nil {
		log.Printf("ExecutePlan error: %v", err)
		_ = SendErrorWithThread(bc.bot, chat, "Erro na execução do plano: "+err.Error(), threadID)
		return
	}

	// 4. Post-execution status update based on final statuses
	for _, r := range results {
		switch r.Status {
		case orchestrator.TaskApproved:
			status.MarkDone(r.TaskID, r.DurationMs)
		case orchestrator.TaskSkipped:
			// already handled by event
		case orchestrator.TaskUnverified:
			status.MarkError(r.TaskID, "Unverified: validation unavailable")
		case orchestrator.TaskEscalated:
			if r.Error != "" {
				status.MarkError(r.TaskID, "Escalated: "+r.Error)
			}
		case orchestrator.TaskFailed:
			if r.Error != "" {
				status.MarkError(r.TaskID, "Failed: "+r.Error)
			}
		}
	}

	// 5. Delivery: update tasks.md, commit approved files, optionally create PR
	approved := manifest.ApprovedResults()
	if len(approved) > 0 {
		// 5a. Update tasks.md
		tasksPath := filepath.Join(repoRoot, ".specs", "features", plan.Feature, "tasks.md")
		tasksUpdated := false
		if plan.Feature != "" {
			if err := orchestrator.UpdateTasksStatus(tasksPath, results); err != nil {
				log.Printf("UpdateTasksStatus: %v", err)
			} else {
				tasksUpdated = true
			}
		}

		// 5b. Commit only approved changed files
		files := manifest.ApprovedChangedFiles()
		if tasksUpdated {
			files = append(files, tasksPath)
		}
		commitMsg := deriveCommitMessage(plan, results)
		if err := orchestrator.CommitChanges(repoRoot, files, commitMsg); err != nil {
			if errors.Is(err, orchestrator.ErrNothingToCommit) {
				log.Printf("CommitChanges: nothing to commit")
			} else {
				log.Printf("CommitChanges error: %v", err)
				_ = SendErrorWithThread(bc.bot, chat, "Commit falhou: "+err.Error(), threadID)
			}
		} else {
			_ = SendTextReplyWithThread(bc.bot, chat, "✅ Commit realizado: "+commitMsg, threadID)
		}

		// 5c. Optional PR
		if plan.CreatePR {
			if !orchestrator.IsGHAvailable() {
				_ = SendTextReplyWithThread(bc.bot, chat,
					"Commit realizado localmente. Instale/autentique `gh` para publicar um PR.", threadID)
			} else {
				prTitle := derivePRTitle(plan)
				prBody := derivePRBody(plan, manifest, results)
				url, err := orchestrator.CreatePR(repoRoot, prTitle, prBody, baseBranch)
				if err != nil {
					log.Printf("CreatePR error: %v", err)
					_ = SendErrorWithThread(bc.bot, chat, "PR falhou: "+err.Error(), threadID)
				} else {
					_ = SendTextReplyWithThread(bc.bot, chat, "🔗 PR: "+url, threadID)
				}
			}
		}
	}

	// 6. Consolidate and respond
	persona := ""
	if bc.persona != nil {
		persona, _ = bc.persona.BuildPrompt()
	}
	consolidationPrompt := orchestrator.BuildConsolidationPrompt(persona, plan, results)
	finalText, err := runOrch.Consolidate(ctx, plan, results, consolidationPrompt)
	if err != nil {
		log.Printf("Consolidate error: %v", err)
	}
	if finalText == "" {
		finalText = buildFallbackConsolidation(results)
	}

	if err := SendTextReplyWithThread(bc.bot, chat, finalText, threadID); err != nil {
		log.Printf("Failed to send consolidation: %v", err)
	}
}

func buildFallbackConsolidation(results []orchestrator.TaskResult) string {
	var sb strings.Builder
	sb.WriteString("**Resultado da execução:**\n\n")
	for _, r := range results {
		if r.Success {
			sb.WriteString("✅ " + r.TaskID + " — Concluído\n")
		} else {
			sb.WriteString("❌ " + r.TaskID + " — " + r.Error + "\n")
		}
	}
	return sb.String()
}

// deriveCommitMessage builds a conventional commit title from the plan and results.
func deriveCommitMessage(plan *orchestrator.Plan, results []orchestrator.TaskResult) string {
	scope := plan.Feature
	if scope == "" {
		scope = "orchestration"
	}
	var desc string
	for _, r := range results {
		if r.Status == orchestrator.TaskApproved {
			desc = r.TaskID + ": " + firstTaskDescription(plan, r.TaskID)
			break
		}
	}
	if desc == "" {
		desc = "execute approved plan"
	}
	msg := fmt.Sprintf("feat(%s): %s", scope, desc)
	if len(msg) > 72 {
		msg = msg[:69] + "..."
	}
	return msg
}

func firstTaskDescription(plan *orchestrator.Plan, taskID string) string {
	for _, t := range plan.Tasks {
		if t.ID == taskID {
			return t.Description
		}
	}
	return ""
}

// derivePRTitle builds a PR title from the plan feature.
func derivePRTitle(plan *orchestrator.Plan) string {
	if plan.Feature != "" {
		return "feat: " + plan.Feature
	}
	return "feat: execution results"
}

// derivePRBody builds a markdown PR body from the manifest and results.
func derivePRBody(plan *orchestrator.Plan, manifest *orchestrator.ExecutionManifest, results []orchestrator.TaskResult) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	if plan.Feature != "" {
		fmt.Fprintf(&sb, "Feature: `%s`\n\n", plan.Feature)
	}

	// Task table
	sb.WriteString("| Task | Status | Duration |\n")
	sb.WriteString("|------|--------|----------|\n")
	for _, r := range results {
		status := string(r.Status)
		if r.Status == orchestrator.TaskApproved {
			status = "✅ Approved"
		}
		dur := time.Duration(r.DurationMs) * time.Millisecond
		fmt.Fprintf(&sb, "| %s | %s | %s |\n", r.TaskID, status, dur.Round(time.Second))
	}

	// Changed files
	files := manifest.ApprovedChangedFiles()
	if len(files) > 0 {
		sb.WriteString("\n### Changed Files\n\n")
		for _, f := range files {
			fmt.Fprintf(&sb, "- `%s`\n", f)
		}
	}

	// Verify results
	var hasVerify bool
	for _, r := range results {
		if r.Verify != nil {
			if !hasVerify {
				sb.WriteString("\n### Verify Results\n\n")
				hasVerify = true
			}
			fmt.Fprintf(&sb, "**%s**: `%s` — exit %d\n", r.TaskID, r.Verify.Command, r.Verify.ExitCode)
			if r.Verify.Stdout != "" {
				fmt.Fprintf(&sb, "```\n%s\n```\n", r.Verify.Stdout)
			}
			if r.Verify.Stderr != "" {
				fmt.Fprintf(&sb, "```stderr\n%s\n```\n", r.Verify.Stderr)
			}
		}
	}

	return sb.String()
}

func (bc *BotController) buildAgentSummaries() []orchestrator.AgentSummary {
	var list []*profiles.PromptProfile
	if bc.profiles != nil {
		list = bc.profiles.List()
	} else if bc.agents != nil {
		for _, a := range bc.agents.Agents() {
			list = append(list, profiles.FromAgent(a))
		}
	}
	if len(list) == 0 {
		return nil
	}
	var summaries []orchestrator.AgentSummary
	for _, p := range list {
		summaries = append(summaries, orchestrator.AgentSummary{
			Name:        p.Name,
			Description: p.Description,
			Tools:       p.AllowedTools,
			ReadOnly:    p.IsReadOnly(),
		})
	}
	return summaries
}

// loadFeatureDocs reads spec.md and design.md from the feature directory
// identified by plan.Feature. If feature is empty or the directory does not
// exist, it falls back to the legacy alphabetical glob (last match).
func (bc *BotController) loadFeatureDocs(repoRoot, feature string) (spec, design string) {
	if feature == "" {
		log.Printf("plan has no feature field — using legacy glob for spec/design")
		return bc.findFeatureDoc(repoRoot, "spec.md"), bc.findFeatureDoc(repoRoot, "design.md")
	}
	base := filepath.Join(repoRoot, ".specs", "features", feature)
	if _, err := filepath.Glob(base); err != nil {
		log.Printf("feature dir %q not accessible: %v", feature, err)
		return "", ""
	}
	return orchestrator.ReadFileContent(filepath.Join(base, "spec.md")),
		orchestrator.ReadFileContent(filepath.Join(base, "design.md"))
}

func (bc *BotController) findFeatureDoc(repoRoot, filename string) string {
	// Legacy fallback: look in .specs/features/*/filename
	pattern := filepath.Join(repoRoot, ".specs", "features", "*", filename)
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		return ""
	}
	// Return the last one (most recently modified directory tends to be last alphabetically)
	return orchestrator.ReadFileContent(matches[len(matches)-1])
}
