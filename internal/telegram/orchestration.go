package telegram

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/orchestrator"

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
	if bc.orchestrator != nil {
		result, err := bc.orchestrator.PreflightExecution(ctx, repoRoot, false)
		if err != nil {
			log.Printf("PreflightExecution for chat=%d thread=%d: %v", chat.ID, threadID, err)
			_ = SendErrorWithThread(bc.bot, chat, orchestrator.PreflightUserMessage(err), threadID)
			return
		}
		_ = result // BaseBranch etc. used in later slices

		// Build a run-scoped orchestrator that uses the handoff cwd for all
		// subsequent operations (worktrees, task cwds, merge). This is a
		// shallow copy that shares the bridge; the original is not mutated.
		runOrch = bc.orchestrator.WithRepoRoot(repoRoot)
	} else {
		// Fallback: use the handoff cwd directly when no orchestrator is
		// configured (unlikely, as tryExecutePlan guards this).
		runOrch = bc.orchestrator
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
	specContent := bc.findFeatureDoc(repoRoot, "spec.md")
	designContent := bc.findFeatureDoc(repoRoot, "design.md")

	// 3. Execute workers via run-scoped orchestrator (uses handoff cwd)
	results, err := runOrch.ExecutePlan(
		ctx,
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
			}
		},
	)

	if err != nil {
		log.Printf("ExecutePlan error: %v", err)
		_ = SendErrorWithThread(bc.bot, chat, "Erro na execução do plano: "+err.Error(), threadID)
		return
	}

	// 4. Validate each result
	validationPrompt := orchestrator.BuildValidationPrompt(specContent, designContent)
	for i, r := range results {
		task := findTaskInPlan(plan, r.TaskID)
		vr, err := runOrch.Validate(ctx, task, r, validationPrompt)
		if err != nil {
			log.Printf("Validate error for %s: %v", r.TaskID, err)
			continue
		}
		if vr.Approved {
			status.MarkDone(r.TaskID, r.DurationMs)
		} else {
			issues := strings.Join(vr.Issues, "; ")
			status.MarkError(r.TaskID, "Reprovado: "+issues)
			results[i].Success = false
			results[i].Error = issues
		}
	}

	// 5. Consolidate and respond
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

func findTaskInPlan(plan *orchestrator.Plan, taskID string) orchestrator.Task {
	for _, t := range plan.Tasks {
		if t.ID == taskID {
			return t
		}
	}
	return orchestrator.Task{ID: taskID}
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

func (bc *BotController) buildAgentSummaries() []orchestrator.AgentSummary {
	if bc.agents == nil {
		return nil
	}
	var summaries []orchestrator.AgentSummary
	for _, a := range bc.agents.Agents() {
		summaries = append(summaries, orchestrator.AgentSummary{
			Name:        a.Name,
			Description: a.Description,
			Tools:       a.AllowedTools,
			ReadOnly:    a.IsReadOnly(),
		})
	}
	return summaries
}

func (bc *BotController) findFeatureDoc(repoRoot, filename string) string {
	// Try to find the most recent feature spec/design
	// Simple approach: look in .specs/features/*/filename
	pattern := filepath.Join(repoRoot, ".specs", "features", "*", filename)
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		return ""
	}
	// Return the last one (most recently modified directory tends to be last alphabetically)
	return orchestrator.ReadFileContent(matches[len(matches)-1])
}
