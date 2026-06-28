package cron

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/persona"
	pipelinepkg "github.com/igormaneschy/aurelia/internal/pipeline"
	"github.com/igormaneschy/aurelia/internal/security"
)

// BridgeCronRuntime executes cron jobs via the Claude Code bridge,
// resolving agent config from the registry and injecting persona prompt.
type bridgeSyncFunc func(ctx context.Context, req bridge.Request) (*bridge.Event, error)

type BridgeCronRuntime struct {
	execute         bridgeSyncFunc
	deliver         DeliveryFunc
	agents          AgentRegistry
	persona         PersonaBuilder
	memoryDir       string
	defaultProvider string
	defaultModel    string
	exePath         string // path to the aurelia binary for CLI instructions
	userResolver    persona.UserPromptResolver
}

// AgentRegistry resolves agent definitions by name.
type AgentRegistry interface {
	Get(name string) *agents.Agent
}

// PersonaBuilder builds the base system prompt from persona files.
type PersonaBuilder interface {
	BuildPrompt() (string, error)
	BuildPromptForUser(userID int64, resolver persona.UserPromptResolver, isOwner bool, activeMode string) (string, error)
}

// NewBridgeCronRuntime creates a runtime that executes jobs via Bridge
// with agent config and persona prompt.
func NewBridgeCronRuntime(
	b *bridge.Bridge,
	ag AgentRegistry,
	p PersonaBuilder,
	memoryDir string,
	defaultProvider string,
	defaultModel string,
) *BridgeCronRuntime {
	r := &BridgeCronRuntime{
		agents:          ag,
		persona:         p,
		memoryDir:       memoryDir,
		defaultProvider: defaultProvider,
		defaultModel:    defaultModel,
	}
	if b != nil {
		r.execute = b.ExecuteSync
	}
	return r
}

// SetDelivery configures a callback invoked after each job execution.
func (r *BridgeCronRuntime) SetDelivery(fn DeliveryFunc) {
	r.deliver = fn
}

// SetExePath configures the path to the aurelia binary used in the cron
// scheduling instructions injected into the system prompt. Optional — if
// unset, the prompt uses the bare "aurelia" command name.
func (r *BridgeCronRuntime) SetExePath(path string) {
	r.exePath = path
}

// SetUserResolver configures the user path resolver for per-user persona support.
func (r *BridgeCronRuntime) SetUserResolver(ur persona.UserPromptResolver) {
	r.userResolver = ur
}

const maxCronCwdChars = 1024

// extractCwdFromPrompt parses "Set cwd to <path>" from the prompt text.
// Returns the path if found, empty string otherwise.
// Matches variants:
//   - "Set cwd to /some/path. Run: ..."
//   - "Set cwd to /some/path. Run both: ..."
//   - "Set cwd to /some/path. Run these three sequentially: ..."
//   - "Set cwd to /some/path\n..."
//   - "Set cwd to \"/some/path with spaces\". Run: ..."
func extractCwdFromPrompt(prompt string) string {
	const prefix = "Set cwd to "
	idx := strings.Index(prompt, prefix)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(prompt[idx+len(prefix):])
	if rest == "" {
		return ""
	}

	if rest[0] == '"' || rest[0] == '\'' {
		return extractQuotedCwd(rest)
	}

	end := len(rest)
	for _, delimiter := range []string{". Run", " Run:", "\n", "\r", ";"} {
		if found := strings.Index(rest, delimiter); found >= 0 && found < end {
			end = found
		}
	}

	cwd := strings.TrimSpace(rest[:end])
	if len(cwd) > maxCronCwdChars {
		return ""
	}
	return strings.Trim(cwd, "`\"")
}

func extractQuotedCwd(rest string) string {
	quote := rest[0]
	for i := 1; i < len(rest); i++ {
		if rest[i] == quote {
			return strings.TrimSpace(rest[1:i])
		}
	}
	return ""
}

func validateCronCwd(raw string) (string, error) {
	cwd := strings.TrimSpace(strings.Trim(raw, "`\""))
	if cwd == "" {
		return "", nil
	}
	if len(cwd) > maxCronCwdChars {
		return "", fmt.Errorf("cwd %q is too long: got %d chars, max %d", cwd[:80]+"...", len(cwd), maxCronCwdChars)
	}
	if strings.ContainsAny(cwd, "\x00\n\r") {
		return "", fmt.Errorf("cwd %q must be a single filesystem path", cwd)
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve cwd %q: %w", cwd, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("cwd %q is not accessible: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cwd %q must be a directory", abs)
	}
	return abs, nil
}

// ExecuteJob builds the system prompt with persona, agent, scheduling
// instructions and global memory, then executes via Bridge.
func (r *BridgeCronRuntime) ExecuteJob(ctx context.Context, job CronJob) (*ExecutionResult, error) {
	result, err := r.runJob(ctx, job)
	if r.deliver != nil {
		if deliverErr := r.deliver(ctx, job, result, err); deliverErr != nil {
			return result, deliverErr
		}
	}
	return result, err
}

func (r *BridgeCronRuntime) runJob(ctx context.Context, job CronJob) (*ExecutionResult, error) {
	if r.execute == nil {
		return nil, fmt.Errorf("bridge is required")
	}
	var basePrompt string
	var err error
	if r.userResolver != nil && job.OwnerUserID != "" {
		ownerNumeric, parseErr := parseInt64(job.OwnerUserID)
		if parseErr == nil && ownerNumeric > 0 {
			basePrompt, err = r.persona.BuildPromptForUser(ownerNumeric, r.userResolver, false, "")
		} else {
			if parseErr != nil {
				log.Printf("cron: failed to parse OwnerUserID %q as int64 for job %s, using global persona", job.OwnerUserID, job.ID)
			}
			basePrompt, err = r.persona.BuildPrompt()
		}
	} else {
		basePrompt, err = r.persona.BuildPrompt()
	}
	if err != nil {
		return nil, fmt.Errorf("build persona prompt: %w", err)
	}

	sections := []string{basePrompt}

	agent := r.agents.Get(job.AgentName)
	if agent != nil && agent.Prompt != "" {
		sections = append(sections, agent.Prompt)
	}

	// Cron-spawned agents need the scheduling instructions to be able to
	// create follow-up jobs (e.g. "remind me again in 1 hour"). Without
	// this section the LLM would invent non-existent internal tools.
	if cron := r.buildCronInstructions(job.TargetChatID, job.OwnerUserID, job.Cwd); cron != "" {
		sections = append(sections, cron)
	}

	// Inject global memory so the agent has continuity across runs. Per-project
	// memory layers are intentionally skipped — cron jobs are not tied to a
	// working directory.
	if mem := r.loadGlobalMemory(); mem != "" {
		sections = append(sections, mem)
	}

	opts := bridge.RequestOptions{
		Provider:     r.defaultProvider,
		SystemPrompt: strings.Join(sections, "\n\n"),
	}

	if agent != nil {
		opts.Model = agent.Model
		opts.Cwd = agent.Cwd
		opts.AllowedTools = agent.AllowedTools
		opts.DisallowedTools = agent.DisallowedTools
	}

	// Fall back to the default model when no agent or agent model is configured.
	// This ensures cron jobs use the user-specified default model (e.g. "big-pickle")
	// instead of the PI SDK's internal default.
	if opts.Model == "" && r.defaultModel != "" {
		opts.Model = r.defaultModel
	}

	// Scrub auth keys before sending to bridge — the TS bridge redacts log output
	// but the prompt itself (injected as the initial user message) should not
	// contain raw keys from persona or agent definitions.
	scrubbedPrompt := pipelinepkg.RedactSecrets(job.Prompt)

	// Attach security context so the bridge enforces tool-use policies.
	ownerNumeric, _ := parseInt64(job.OwnerUserID)
	cwd := ""
	cwdSource := "none"
	cwdAllowsExecution := false
	// INVARIANT: only an explicit job.Cwd or a prompt-extracted cwd grants
	// cwdAllowsExecution=true. An agent.Cwd without an explicit CapabilityProfile
	// stays read_only — the agent must opt into execution through its profile.
	// This prevents agents with a default Cwd from silently gaining Bash/Write.
	if job.Cwd != "" {
		cwd = job.Cwd
		cwdSource = "job"
		cwdAllowsExecution = true
	} else if agent != nil && agent.Cwd != "" {
		cwd = agent.Cwd
		cwdSource = "agent"
		// cwdAllowsExecution stays false: agent must declare CapabilityProfile.
	} else if extracted := extractCwdFromPrompt(job.Prompt); extracted != "" {
		cwd = extracted
		cwdSource = "prompt"
		cwdAllowsExecution = true
	}

	// Validate the resolved cwd at execution time. This does I/O (os.Stat)
	// on every run — a deliberate tradeoff. If the directory is on a
	// temporarily unavailable mount, the job silently falls back to
	// observe/read_only (no filesystem tools) instead of failing.
	// A run_log event or Telegram notification on cwd validation failure
	// could improve observability, but the current conservative fallback
	// is safe: it never executes with a broken cwd.
	if cwd != "" {
		validated, validateErr := validateCronCwd(cwd)
		if validateErr != nil {
			log.Printf("cron: job=%s cwd_source=%s invalid cwd: %v", job.ID, cwdSource, validateErr)
			cwd = ""
			opts.Cwd = ""
			cwdAllowsExecution = false
		} else {
			cwd = validated
			opts.Cwd = validated
			log.Printf("cron: job=%s using cwd=%q source=%s", job.ID, cwd, cwdSource)
		}
	}

	// Determine effective capability profile for cron execution.
	// Profile selection priority:
	//   1. Agent with CapabilityProfile → use it directly (agent owns its policy)
	//   2. Job/prompt cwd with cwdAllowsExecution → execute_safe (user opted in)
	//   3. Agent cwd without CapabilityProfile → read_only (safe default until agent declares profile)
	//   4. No agent, no cwd → observe (classification/routing only)
	var profileStr string
	var allowedTools []string
	if agent != nil && agent.CapabilityProfile != "" {
		profileStr = agent.CapabilityProfile
		allowedTools = agent.AllowedTools
	} else if cwd != "" {
		if cwdAllowsExecution {
			// Cwd was explicitly configured on the job or extracted from a prompt
			// that implies execution ("Run: script"). Elevate to execute_safe so
			// the LLM has Bash/Read/Write tools.
			profileStr = string(security.ProfileExecuteSafe)
		} else {
			// INVARIANT: agent.Cwd without CapabilityProfile → read_only.
			// The agent has a working directory but hasn't opted into execution.
			// This is safe: an agent that needs Bash/Write must set
			// capability_profile: execute_safe (or higher) in its definition.
			// Without this, a misconfigured agent with a default Cwd would
			// silently gain filesystem write access.
			profileStr = string(security.ProfileReadOnly)
		}
		allowedTools = security.ProfileTools(security.CapabilityProfile(profileStr))
	} else {
		// No agent, no cwd — observe only (no tools at all).
		profileStr = string(security.ProfileObserve)
		allowedTools = security.ProfileTools(security.ProfileObserve)
	}

	// If no explicit AllowedTools from agent, derive from effective profile.
	// Always pass explicit tools so the bridge never falls back to SDK defaults.
	if allowedTools == nil {
		allowedTools = security.ProfileTools(security.CapabilityProfile(profileStr))
	}

	// When cwd is empty, strip write/bash tools regardless of profile.
	if cwd == "" {
		var safe []string
		for _, t := range allowedTools {
			if t != "Write" && t != "Edit" && t != "Bash" {
				safe = append(safe, t)
			}
		}
		allowedTools = safe
		if agent == nil {
			profileStr = string(security.ProfileObserve)
		} else if profileStr != string(security.ProfileObserve) {
			profileStr = string(security.ProfileReadOnly)
		}
	}

	// Ensure AllowedTools is a non-empty safe list when no agent/cwd is configured.
	// A nil slice is omitted by omitempty, causing the bridge to fall back to SDK
	// defaults (which may include Write/Edit/Bash). An empty slice is also omitted.
	// Use a minimal metadata-only set that cannot read or write file content.
	if allowedTools == nil {
		allowedTools = []string{"Glob", "LS"}
		profileStr = string(security.ProfileReadOnly)
	}

	opts.Security = &bridge.SecurityContext{
		Enabled:   true,
		Profile:   profileStr,
		Mode:      "block",
		Cwd:       cwd,
		ChatID:    job.TargetChatID,
		ThreadID:  job.TargetThreadID,
		UserID:    ownerNumeric,
		AgentName: job.AgentName,
	}
	opts.ChatID = job.TargetChatID
	opts.ThreadID = job.TargetThreadID
	opts.UserID = ownerNumeric
	opts.AllowedTools = allowedTools

	ev, err := r.execute(ctx, bridge.Request{
		Command: "query",
		Prompt:  scrubbedPrompt,
		Options: opts,
	})
	if err != nil {
		return nil, fmt.Errorf("bridge execute: %w", err)
	}
	if ev.Type == "error" {
		return nil, fmt.Errorf("bridge error: %s", ev.Message)
	}

	return &ExecutionResult{
		Output:    ev.Content,
		SessionID: ev.SessionID,
		CostUSD:   ev.CostUSD,
		NumTurns:  ev.NumTurns,
	}, nil
}

// buildCronInstructions mirrors the text injected by the telegram pipeline so
// agents triggered by cron can schedule follow-up jobs. Returns empty when no
// target chat is set (the --chat-id flag is required).
// ownerUserID is included in the CLI example when non-empty, so follow-up jobs
// inherit the original job's owner.
func (r *BridgeCronRuntime) buildCronInstructions(targetChatID int64, ownerUserID string, cwd string) string {
	if targetChatID == 0 {
		return ""
	}
	bin := "aurelia"
	if r.exePath != "" {
		bin = r.exePath
	}
	flags := fmt.Sprintf("--chat-id %d", targetChatID)
	ownerSuffix := ""
	if ownerUserID != "" {
		flags = fmt.Sprintf("%s --owner-user-id %s", flags, ownerUserID)
		ownerSuffix = " If scheduling for yourself, include --owner-user-id <your_user_id>."
	}
	// %q quoting escapes spaces and special chars for the LLM to read as
	// instructional text (not a shell command). Paths with unusual characters
	// (e.g. Windows backslashes) are rare on this deployment target.
	if strings.TrimSpace(cwd) != "" {
		flags = fmt.Sprintf("%s --cwd %q", flags, strings.TrimSpace(cwd))
	}
	return fmt.Sprintf(`## Scheduling Tasks

Use the Aurelia cron CLI for ALL scheduling. Internal scheduling tools die with the session — only the CLI persists.

- Recurring: `+"`%s cron add \"<cron-expr>\" \"<prompt>\" %s`"+`
- One-time: `+"`%s cron once \"<ISO-timestamp>\" \"<prompt>\" %s`"+`
- List: `+"`%s cron list %s`"+` | Delete: `+"`%s cron del <id>`"+`

Cron prompts are ACTION instructions (not content). They run in isolated sessions with no history. Prefer --cwd for project jobs instead of encoding "Set cwd to ..." inside the prompt. The --chat-id flag is required.%s`,
		bin, flags,
		bin, flags,
		bin, flags,
		bin, ownerSuffix,
	)
}

// loadGlobalMemory reads MEMORY.md (if present) plus the first ~16KB of every
// .md file in the global memory directory. Heavier per-project layers are
// intentionally omitted — keeps the prompt bounded for cron jobs.
func (r *BridgeCronRuntime) loadGlobalMemory() string {
	if r.memoryDir == "" {
		return ""
	}
	entries, err := os.ReadDir(r.memoryDir)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Persistent Memory\n\nYou have memory across cron runs. Below is what you remember:\n")

	const perFileCap = 8000
	wrote := 0

	if data, err := os.ReadFile(filepath.Join(r.memoryDir, "MEMORY.md")); err == nil && len(data) > 0 {
		sb.WriteString("\n**MEMORY.md (index):**\n")
		sb.WriteString(cap8k(pipelinepkg.RedactSecrets(string(data)), perFileCap))
		wrote++
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == "MEMORY.md" || !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.memoryDir, name))
		if err != nil || len(data) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n**%s:**\n%s", name, cap8k(pipelinepkg.RedactSecrets(strings.TrimSpace(string(data))), perFileCap))
		wrote++
	}

	if wrote == 0 {
		return ""
	}
	return sb.String()
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func cap8k(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n\n[...truncado]"
}

// DeliveryFunc is called after a job completes to deliver its output.
type DeliveryFunc func(ctx context.Context, job CronJob, result *ExecutionResult, execErr error) error
