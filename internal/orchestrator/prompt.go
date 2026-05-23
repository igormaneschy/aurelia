package orchestrator

import (
	"fmt"
	"strings"
)

// AgentSummary describes an available agent for the planning prompt.
type AgentSummary struct {
	Name        string
	Description string
	Tools       []string
	ReadOnly    bool
}

// BuildOrchestratorPrompt builds Aurelia's system prompt with TLC methodology,
// anti-overengineering rules, and the list of available agents.
func BuildOrchestratorPrompt(persona string, availableAgents []AgentSummary) string {
	var sections []string

	if persona != "" {
		sections = append(sections, persona)
	}

	sections = append(sections, buildTLCInstructions())
	sections = append(sections, buildAgentList(availableAgents))
	sections = append(sections, buildPlanOutputInstructions())

	return strings.Join(sections, "\n\n")
}

// BuildExecutionPrompt builds the prompt for generating an execution plan JSON
// from an approved tasks.md.
func BuildExecutionPrompt(tasksContent string, availableAgents []AgentSummary) string {
	var sb strings.Builder
	sb.WriteString("# Generate Execution Plan\n\n")
	sb.WriteString("Convert the approved tasks below into a structured execution plan.\n\n")
	sb.WriteString("## Approved Tasks\n\n")
	sb.WriteString(tasksContent)
	sb.WriteString("\n\n")
	sb.WriteString("## Available Agents\n\n")
	for _, a := range availableAgents {
		fmt.Fprintf(&sb, "- **%s**: %s (tools: %s)\n", a.Name, a.Description, strings.Join(a.Tools, ", "))
	}
	sb.WriteString("\nIf no specialized agent matches, use `worker` (the default).\n\n")
	sb.WriteString("## Output Format\n\n")
	sb.WriteString("Respond with ONLY a JSON plan inside an `aurelia-plan` code block:\n\n")
	sb.WriteString("```aurelia-plan\n")
	sb.WriteString(`{"feature":"feature-name","create_pr":false,"verify":"go test ./...","tasks":[{"id":"T1","description":"...","agent":"worker","prompt":"...","depends_on":[],"needs_worktree":true,"verify":""}]}`)
	sb.WriteString("\n```\n\n")
	sb.WriteString("Rules:\n")
	sb.WriteString("- `feature` is the feature directory name (e.g. \"agent-orchestration-execution\")\n")
	sb.WriteString("- `create_pr` is true when the user explicitly wants a PR opened\n")
	sb.WriteString("- `verify` is an optional default verify command for the whole plan (e.g. `go test ./...`)\n")
	sb.WriteString("- Each task maps to one entry in the tasks.md\n")
	sb.WriteString("- `agent` is the name of the agent to use (or `worker` for default)\n")
	sb.WriteString("- `prompt` is the full instruction for the worker — be specific and include file paths\n")
	sb.WriteString("- `needs_worktree` is true for tasks that modify files, false for read-only tasks (review, analysis)\n")
	sb.WriteString("- `verify` on a task overrides the plan-level verify for that task\n")
	sb.WriteString("- Respect dependencies from the tasks.md\n")
	return sb.String()
}

// BuildWorkerPrompt assembles the full system prompt for a worker.
// Layers: agent base prompt + CLAUDE.md + AGENTS.md + spec + design + task + siblings.
func BuildWorkerPrompt(agentPrompt, claudeMd, agentsMd, specContent, designContent string, task Task, siblings []Task) string {
	var sections []string

	if agentPrompt != "" {
		sections = append(sections, agentPrompt)
	}

	if claudeMd != "" {
		sections = append(sections, "# Project Conventions (CLAUDE.md)\n\n"+claudeMd)
	}

	if agentsMd != "" {
		sections = append(sections, "# Squad Configuration (AGENTS.md)\n\n"+agentsMd)
	}

	if specContent != "" {
		sections = append(sections, "# Feature Specification\n\n"+specContent)
	}

	if designContent != "" {
		sections = append(sections, "# Feature Design\n\n"+designContent)
	}

	// Task-specific instructions (summary only — full prompt is injected via user message)
	var taskSection strings.Builder
	taskSection.WriteString("# Your Task\n\n")
	fmt.Fprintf(&taskSection, "**ID**: %s\n", task.ID)
	fmt.Fprintf(&taskSection, "**Description**: %s\n\n", task.Description)
	taskSection.WriteString("## Rules\n\n")
	taskSection.WriteString("- Focus ONLY on this task — do not make changes outside its scope\n")
	taskSection.WriteString("- Follow the conventions in CLAUDE.md\n")
	taskSection.WriteString("- When done, report what you did and what files you changed\n")
	sections = append(sections, taskSection.String())

	// Sibling awareness (summaries only — no full prompts)
	if len(siblings) > 0 {
		var sibSection strings.Builder
		sibSection.WriteString("# Other Workers (context only — do not do their work)\n\n")
		for _, s := range siblings {
			if s.ID != task.ID {
				fmt.Fprintf(&sibSection, "- **%s** (%s): %s\n", s.ID, s.Agent, s.Description)
			}
		}
		sections = append(sections, sibSection.String())
	}

	return strings.Join(sections, "\n\n")
}

// BuildValidationPrompt builds the system prompt for Aurelia's quality gate.
func BuildValidationPrompt(specContent, designContent string) string {
	var sb strings.Builder
	sb.WriteString("# Quality Gate — Validate Worker Result\n\n")
	sb.WriteString("You are reviewing the output of a worker agent. Validate against:\n\n")
	sb.WriteString("1. **Task requirements**: Did the worker complete what was asked?\n")
	sb.WriteString("2. **Code quality**: Does it follow project conventions?\n")
	sb.WriteString("3. **Scope discipline**: Did the worker only touch what was needed?\n")
	sb.WriteString("4. **No overengineering**: Is the solution the simplest that works?\n")
	sb.WriteString("5. **Tests**: If tests were required, did they pass?\n\n")

	if specContent != "" {
		sb.WriteString("## Feature Specification (for reference)\n\n")
		sb.WriteString(specContent)
		sb.WriteString("\n\n")
	}

	if designContent != "" {
		sb.WriteString("## Feature Design (for reference)\n\n")
		sb.WriteString(designContent)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Response Format\n\n")
	sb.WriteString("Respond with a JSON object:\n")
	sb.WriteString("```json\n")
	sb.WriteString(`{"approved": true/false, "issues": ["issue1", "issue2"], "should_retry": true/false}`)
	sb.WriteString("\n```\n\n")
	sb.WriteString("- `approved`: true if the work meets all criteria\n")
	sb.WriteString("- `issues`: list of specific problems found (empty if approved)\n")
	sb.WriteString("- `should_retry`: true if the issues are fixable by the worker\n")

	return sb.String()
}

// BuildConsolidationPrompt builds the prompt for synthesizing all worker results.
func BuildConsolidationPrompt(persona string, plan *Plan, results []TaskResult) string {
	var sb strings.Builder

	if persona != "" {
		sb.WriteString(persona)
		sb.WriteString("\n\n")
	}

	sb.WriteString("# Consolidate Worker Results\n\n")
	sb.WriteString("Your workers have completed their tasks. Synthesize the results into a clear summary for the user.\n\n")

	sb.WriteString("## Execution Results\n\n")
	for _, r := range results {
		task := findTask(plan, r.TaskID)
		status := "✅ Success"
		if !r.Success {
			status = fmt.Sprintf("❌ Failed: %s", r.Error)
		}
		fmt.Fprintf(&sb, "### %s: %s — %s\n\n", r.TaskID, task.Description, status)
		if r.Content != "" {
			sb.WriteString(r.Content)
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("- Summarize what was accomplished\n")
	sb.WriteString("- List any issues or partial failures\n")
	sb.WriteString("- Suggest next steps if applicable\n")
	sb.WriteString("- Keep it concise — the user can see the individual worker status messages\n")

	return sb.String()
}

func findTask(plan *Plan, taskID string) Task {
	for _, t := range plan.Tasks {
		if t.ID == taskID {
			return t
		}
	}
	return Task{ID: taskID, Description: "unknown task"}
}

func buildTLCInstructions() string {
	return `# Methodology — Spec-Driven Development (TLC)

You follow the TLC spec-driven methodology:

## Phases

1. **SPECIFY** — Understand the problem. Create .specs/features/<feat>/spec.md with:
   - Problem statement, goals, out of scope
   - User stories with WHEN/THEN/SHALL acceptance criteria
   - Edge cases, success criteria
   - Always ask the user to approve before moving on

2. **DESIGN** — Define HOW to build it. Create .specs/features/<feat>/design.md with:
   - Architecture overview (use mermaid diagrams)
   - Code reuse analysis (what exists that we can leverage)
   - Component definitions with interfaces
   - Data models, error handling, tech decisions

3. **TASKS** — Break into atomic tasks. Create .specs/features/<feat>/tasks.md with:
   - Execution plan with phases and dependency graph
   - Each task: What, Where, DependsOn, Done When, Verify
   - One task = one component / one function / one endpoint
   - Mark parallel-safe tasks with [P]

4. **IMPLEMENT + VALIDATE** — Execute approved tasks via workers.
   - This phase is AUTOMATIC after user approves tasks
   - You generate an execution plan and workers handle it
   - You validate each worker's result before accepting

## Anti-Overengineering Rules

- Before suggesting anything, ask: "Will you need this in the next 3 months?"
- Simple and well-done beats complex and incomplete — ALWAYS
- No premature abstractions, no "flexibility" for hypothetical futures
- Three similar lines of code are better than a premature abstraction
- Only add what was asked for — no extra features, no extra configurability

## Approval Gates

- NEVER execute code without user approval of the spec
- NEVER implement without approved design
- NEVER spawn workers without approved tasks
- Simple tasks (bug fix, quick question) skip planning — use judgment`
}

func buildAgentList(agents []AgentSummary) string {
	if len(agents) == 0 {
		return "# Available Agents\n\nNo specialized agents configured. Default worker will be used for all tasks."
	}

	var sb strings.Builder
	sb.WriteString("# Available Agents\n\n")
	sb.WriteString("When creating execution plans, assign the best agent for each task:\n\n")
	sb.WriteString("| Agent | Description | Tools | Type |\n")
	sb.WriteString("|-------|-------------|-------|------|\n")
	for _, a := range agents {
		agentType := "read-write"
		if a.ReadOnly {
			agentType = "read-only"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n",
			a.Name, a.Description, strings.Join(a.Tools, ", "), agentType)
	}
	sb.WriteString("\nUse `worker` for any task that doesn't match a specialized agent.\n")
	return sb.String()
}

func buildPlanOutputInstructions() string {
	return `# Execution Plan Output

When the user approves the tasks and says to execute (e.g., "aprovado", "pode fazer", "manda ver", "bora", "execute"), emit an execution plan as a JSON block:

` + "```aurelia-plan" + `
{"feature":"feature-name","create_pr":false,"verify":"go test ./...","tasks":[
  {"id":"T1","description":"...","agent":"worker","prompt":"Full task instructions...","depends_on":[],"needs_worktree":true,"verify":""},
  {"id":"T2","description":"...","agent":"qa","prompt":"...","depends_on":["T1"],"needs_worktree":true}
]}
` + "```" + `

Rules for the plan:
- Only emit when user explicitly approves execution
- ` + "`feature`" + ` is the feature directory name under .specs/features/ (e.g. "agent-orchestration-execution")
- ` + "`create_pr`" + ` is true when the user explicitly asked to open a PR
- ` + "`verify`" + ` is an optional default verify command for the whole plan
- Each task must have a clear, self-contained prompt (workers have no conversation history)
- Set needs_worktree=true for tasks that modify files, false for read-only tasks
- Task-level ` + "`verify`" + ` overrides the plan-level verify for that task
- Respect task dependencies from tasks.md
- Use specific agent names from the Available Agents table, or "worker" for default`
}
