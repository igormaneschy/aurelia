package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/security"
)

// runLogState tracks per-run state for the run journal.
// mu serializes summary mutations independently of the runLogState map lock,
// preventing data races between recordToolUse/recordToolResult and completeRunLog.
type runLogState struct {
	mu               sync.Mutex
	runID            string
	summary          strings.Builder
	summaryCount     int
	wg               sync.WaitGroup // tracks in-flight DB updates
	partialAssistant string         // last partial assistant text, for checkpoint on timeout
}

// activeRun is stored in activeSessions to carry both a cancel function
// and an ownership token. Cancel/supersede paths extract the cancel func
// via extractCancelFn; the token is used for ownership-safe cleanup.
type activeRun struct {
	token  any
	cancel context.CancelFunc
}

func newActiveRun() *activeRun {
	return &activeRun{token: &activeRun{}}
}

// extractCancelFn returns the cancel func from an activeSessions value.
// Handles *activeRun (new path) and context.CancelFunc (legacy path).
func extractCancelFn(v any) context.CancelFunc {
	switch val := v.(type) {
	case *activeRun:
		return val.cancel
	case context.CancelFunc:
		return val
	}
	return nil
}

// pipelineInput carries a user message through processing.
type pipelineInput struct {
	chatID    int64
	threadID  int
	messageID int
	userID    int64
	text      string
	images    []bridge.ImageAttachment
}

const (
	classifyTimeout        = 5 * time.Second
	classifyMinTextLen     = 10
	bridgeExecutionTimeout = 30 * time.Minute // hard safety net, not configurable
	defaultIdleTimeout     = 15 * time.Minute // fallback when config not available

	// tool call explosion thresholds
	toolCallWarningThreshold  = 20 // warn user after this many tool calls without result
	toolCallCriticalThreshold = 50 // critical warning after this many tool calls

	// timeout warning: warn N minutes before hard 30-min timeout
	timeoutWarningLead = 5 * time.Minute

	bridgeConnectErrorMessage = "Falha ao conectar com o processador.\n\n" +
		"Dica: verifique se o daemon está rodando. Se persistir, tente /new para reiniciar a sessão."
	bridgeRetryFailedMessage = "Processador reiniciado mas não conseguiu completar. Tente novamente.\n\n" +
		"Dica: se persistir, use /new para reiniciar a sessão."

	heartbeatInterval      = 10 * time.Second
	heartbeatThreshold     = 15 * time.Second
	heartbeatToolThreshold = 8 // include tool call count in heartbeat every N beats

	timeoutOriginUnknown      = "unknown_timeout"
	timeoutOriginMaxExecution = "max_execution_timeout"
	timeoutOriginIdleBridge   = "idle_bridge_timeout"
	timeoutOriginBridgeQuery  = "bridge_query_timeout"
	timeoutOriginProviderPI   = "provider/pi_timeout"
)

// buildTimeoutMessage returns a user-facing error message based on the timeout origin.
func buildTimeoutMessage(origin string) string {
	switch origin {
	case timeoutOriginIdleBridge:
		return "Tempo limite de inatividade atingido. O processador ficou muito tempo sem responder.\n\n" +
			"Dica: tente enviar uma mensagem mais curta ou dividir em partes. Se persistir, use /new."
	case timeoutOriginBridgeQuery:
		return "O processador não conseguiu completar a consulta a tempo.\n\n" +
			"Dica: tente novamente. Se o problema persistir, pode ser um problema no provedor de IA."
	case timeoutOriginProviderPI:
		return "O provedor de IA não respondeu a tempo.\n\n" +
			"Dica: tente novamente em alguns instantes. Se persistir, verifique o status do provedor."
	case timeoutOriginMaxExecution:
		return "Tempo máximo de execução atingido.\n\n" +
			"A solicitação foi muito complexa. Tente dividir em partes menores."
	default:
		return "Tempo limite atingido antes de concluir.\n\n" +
			"A solicitação foi muito complexa. Tente dividir em partes menores."
	}
}

type runTimeoutTracker struct {
	mu        sync.Mutex
	startedAt time.Time
	origin    string
}

func newRunTimeoutTracker() *runTimeoutTracker {
	return &runTimeoutTracker{startedAt: time.Now()}
}

func (t *runTimeoutTracker) mark(origin string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.origin == "" {
		t.origin = origin
	}
}

func (t *runTimeoutTracker) snapshot() (string, time.Duration) {
	if t == nil {
		return timeoutOriginUnknown, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	origin := t.origin
	if origin == "" {
		origin = timeoutOriginUnknown
	}
	return origin, time.Since(t.startedAt)
}

func bridgeCooldownMessage(remaining time.Duration) string {
	seconds := int((remaining + time.Second - time.Nanosecond) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("⏳ Processador em recuperação. Tente novamente em ~%d segundos.", seconds)
}

// Process handles a user message after transport-level bootstrap and command checks.
func (s *Service) Process(chatID int64, threadID int, messageID int, text string, images []bridge.ImageAttachment, userID int64) error {
	if s == nil {
		return errors.New("pipeline service is nil")
	}
	if s.output == nil {
		return errors.New("pipeline output is nil")
	}
	if s.bridge == nil {
		return errors.New("pipeline bridge is nil")
	}

	key := sessionKey(chatID, threadID, userID)
	input := pipelineInput{chatID: chatID, threadID: threadID, messageID: messageID, userID: userID, text: text, images: images}

	// Create a real cancelable context BEFORE atomic reservation so cancel/supersede
	// arriving during the reservation window actually cancel the run, not a no-op sentinel.
	// Store an *activeRun that carries both the cancel func and an ownership token.
	// All cancel/supersede paths extract the cancel func from *activeRun.
	run := newActiveRun()
	reservedCtx, reservedCancel := context.WithCancel(context.Background())
	run.cancel = reservedCancel

	_, loaded := s.activeSessions.LoadOrStore(key, run)
	if !loaded {
		// We reserved the slot — start the run.
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("pipeline: panic in processRun: %v", r)
				}
			}()
			s.processRunWithCancel(input, run, reservedCtx, reservedCancel)
		}()
		return nil
	}

	// Reservation failed: cancel the unused reservation context only.
	// Do NOT cancel the existing active run — that depends on the message type.
	reservedCancel()

	// Active session — classify and send appropriate bridge command.
	// Only concurrentCancel and concurrentSupersede cancel/delete the stored run;
	// concurrentEnqueue and concurrentStatus must leave it intact.
	switch classifyConcurrentMessage(text) {
	case concurrentCancel:
		// Stop the old goroutine so it doesn't retry after abort
		if val, loaded := s.activeSessions.LoadAndDelete(key); loaded {
			if cancelFn := extractCancelFn(val); cancelFn != nil {
				cancelFn()
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
		defer cancel()
		_, err := s.bridge.ExecuteSync(ctx, bridge.Request{
			Command: "abort",
			Options: bridge.RequestOptions{ChatID: chatID, ThreadID: threadID, UserID: userID},
		})
		if err != nil {
			log.Printf("pipeline: abort failed for chat=%d: %v", chatID, err)
		}
		if _, err := s.output.SendText(chatID, threadID, "🛑 Interrompendo o pedido anterior."); err != nil {
			log.Printf("pipeline: SendText(cancel) failed for chat=%d: %v", chatID, err)
		}
		s.output.ConfirmMessage(chatID, messageID)

	case concurrentSupersede:
		// Stop the old goroutine; the superseding message starts fresh
		if val, loaded := s.activeSessions.LoadAndDelete(key); loaded {
			if cancelFn := extractCancelFn(val); cancelFn != nil {
				cancelFn()
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
		defer cancel()
		_, err := s.bridge.ExecuteSync(ctx, bridge.Request{
			Command: "steer",
			Prompt:  text,
			Options: bridge.RequestOptions{ChatID: chatID, ThreadID: threadID, UserID: userID},
		})
		if err != nil {
			log.Printf("pipeline: steer failed for chat=%d: %v", chatID, err)
		}
		if _, err := s.output.SendText(chatID, threadID, "🔁 Interrompi o pedido anterior e vou seguir com sua correção."); err != nil {
			log.Printf("pipeline: SendText(supersede) failed for chat=%d: %v", chatID, err)
		}
		s.output.ConfirmMessage(chatID, messageID)
		// Start a new goroutine to process the steered session.
		// Store a fresh activeRun atomically before launching so the run is
		// tracked from the start; if another goroutine raced in, cancel and abort.
		supersedeRun := newActiveRun()
		supersedeCtx, supersedeCancel := context.WithCancel(context.Background())
		supersedeRun.cancel = supersedeCancel
		if _, loaded := s.activeSessions.LoadOrStore(key, supersedeRun); loaded {
			// Another run appeared before we could store ours — rare race.
			supersedeCancel()
			log.Printf("pipeline: supersede raced with another run for key=%s", key)
			if _, err := s.output.SendText(chatID, threadID, "⚠️ Outra solicitação já está em andamento. Tente novamente."); err != nil {
				log.Printf("pipeline: SendText(supersede race) failed for chat=%d: %v", chatID, err)
			}
			break
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("pipeline: panic in processRun(supersede): %v", r)
				}
			}()
			s.processRunWithCancel(input, supersedeRun, supersedeCtx, supersedeCancel)
		}()

	case concurrentEnqueue:
		ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
		defer cancel()
		_, err := s.bridge.ExecuteSync(ctx, bridge.Request{
			Command: "follow-up",
			Prompt:  text,
			Options: bridge.RequestOptions{ChatID: chatID, ThreadID: threadID, UserID: userID},
		})
		if err != nil {
			log.Printf("pipeline: follow-up failed for chat=%d: %v", chatID, err)
		}
		if _, err := s.output.SendText(chatID, threadID, "📥 Adicionado à fila. Processo após concluir o atual."); err != nil {
			log.Printf("pipeline: SendText(follow-up) failed for chat=%d: %v", chatID, err)
		}
		s.output.ConfirmMessage(chatID, messageID)

	case concurrentStatus:
		ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
		defer cancel()
		ev, err := s.bridge.ExecuteSync(ctx, bridge.Request{
			Command: "get-state",
			Options: bridge.RequestOptions{ChatID: chatID, ThreadID: threadID, UserID: userID},
		})
		if err == nil {
			var state struct {
				IsStreaming  bool `json:"is_streaming"`
				PendingCount int  `json:"pending_count"`
			}
			if json.Unmarshal([]byte(ev.Content), &state) == nil {
				desc := "⏳ Ainda estou processando o pedido anterior."
				if state.PendingCount > 0 {
					desc += fmt.Sprintf("\n📥 Fila: %d mensagens aguardando.", state.PendingCount)
				}
				if _, err := s.output.SendText(chatID, threadID, desc); err != nil {
					log.Printf("pipeline: SendText(status) failed for chat=%d: %v", chatID, err)
				}
			}
		}
		s.output.ConfirmMessage(chatID, messageID)
	}

	return nil
}

// processRunWithCancel starts a run with a pre-created cancel func and token-based
// ownership of the activeSessions slot. The reserved cancel func is used to cancel
// the run. On cleanup, the map entry is only deleted if it still belongs to this run
// (checked via Load + pointer comparison on the ownership token).
func (s *Service) processRunWithCancel(input pipelineInput, run *activeRun, reservedCtx context.Context, reservedCancel context.CancelFunc) {
	key := sessionKey(input.chatID, input.threadID, input.userID)
	defer func() {
		// Atomic ownership check: only delete if the stored value still points to
		// our run. This prevents a cancel/supersede-then-new-run cycle from
		// nuking the new run's entry. CompareAndDelete is atomic in sync.Map.
		s.activeSessions.CompareAndDelete(key, run)
	}()
	defer reservedCancel()
	_ = reservedCtx // available for cancellation via reservedCancel

	agent := s.routeAgent(input.text)
	userText := stripAgentPrefix(input.text, agent)

	if _, active := s.sessions.GetSessionWithState(input.chatID, input.threadID, input.userID); !active {
		s.autoDetectProject(input.chatID, input.threadID, userText)
	}

	if s.checkProjectPreflight(input, agent, userText) {
		return
	}

	systemPrompt, err := s.buildSystemPrompt(userText, agent, input.chatID, input.messageID, input.threadID, input.userID)
	if err != nil {
		log.Printf("Failed to build system prompt: %s", redactSecrets(err.Error()))
		if err := s.output.SendError(input.chatID, input.threadID, "Falha ao montar o prompt de sistema."); err != nil {
			log.Printf("pipeline: SendError(system prompt) failed for chat=%d: %v", input.chatID, err)
		}
		s.output.ConfirmMessage(input.chatID, input.messageID)
		return
	}

	req := s.buildBridgeRequest(userText, systemPrompt, agent, input.chatID, input.threadID, input.userID)
	req.RequestID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	req.Options.Images = input.images
	s.applyVisionFallback(&req, input.images)

	// Apply session lifecycle decision before executing
	if lcResult := s.applyLifecycle(reservedCtx, &req, input.chatID, input.threadID, input.userID); lcResult.SkipExecution {
		log.Printf("lifecycle: execution skipped for chat=%d: %s", input.chatID, lcResult.ErrorMessage)
		if lcResult.ErrorMessage != "" {
			if err := s.output.SendError(input.chatID, input.threadID, lcResult.ErrorMessage); err != nil {
				log.Printf("pipeline: SendError(lifecycle) failed for chat=%d: %v", input.chatID, err)
			}
		}
		s.output.ConfirmMessage(input.chatID, input.messageID)
		return
	}

	s.executeAsync(reservedCtx, input.chatID, input.threadID, input.messageID, req, userText, input.userID)
}

//nolint:unused // legacy path retained for potential fallback
func (s *Service) processRun(input pipelineInput) {
	key := sessionKey(input.chatID, input.threadID, input.userID)
	ctx, cancel := context.WithCancel(context.Background())
	s.activeSessions.Store(key, cancel)
	defer s.activeSessions.Delete(key)
	defer cancel()

	agent := s.routeAgent(input.text)
	userText := stripAgentPrefix(input.text, agent)

	if _, active := s.sessions.GetSessionWithState(input.chatID, input.threadID, input.userID); !active {
		s.autoDetectProject(input.chatID, input.threadID, userText)
	}

	if s.checkProjectPreflight(input, agent, userText) {
		return
	}

	systemPrompt, err := s.buildSystemPrompt(userText, agent, input.chatID, input.messageID, input.threadID, input.userID)
	if err != nil {
		log.Printf("Failed to build system prompt: %s", redactSecrets(err.Error()))
		if err := s.output.SendError(input.chatID, input.threadID, "Falha ao montar o prompt de sistema."); err != nil {
			log.Printf("pipeline: SendError(system prompt) failed for chat=%d: %v", input.chatID, err)
		}
		s.output.ConfirmMessage(input.chatID, input.messageID)
		return
	}

	req := s.buildBridgeRequest(userText, systemPrompt, agent, input.chatID, input.threadID, input.userID)
	req.RequestID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	req.Options.Images = input.images
	s.applyVisionFallback(&req, input.images)

	// Apply session lifecycle decision before executing
	if lcResult := s.applyLifecycle(ctx, &req, input.chatID, input.threadID, input.userID); lcResult.SkipExecution {
		log.Printf("lifecycle: execution skipped for chat=%d: %s", input.chatID, lcResult.ErrorMessage)
		if lcResult.ErrorMessage != "" {
			if err := s.output.SendError(input.chatID, input.threadID, lcResult.ErrorMessage); err != nil {
				log.Printf("pipeline: SendError(lifecycle) failed for chat=%d: %v", input.chatID, err)
			}
		}
		s.output.ConfirmMessage(input.chatID, input.messageID)
		return
	}

	s.executeAsync(ctx, input.chatID, input.threadID, input.messageID, req, userText, input.userID)
}

func stripAgentPrefix(text string, agent *agents.Agent) string {
	if agent == nil {
		return text
	}
	if idx := strings.IndexByte(text[1:], ' '); idx != -1 {
		if stripped := strings.TrimSpace(text[idx+2:]); stripped != "" {
			return stripped
		}
	}
	return text
}

func (s *Service) autoDetectProject(chatID int64, threadID int, userText string) {
	detectCtx, detectCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer detectCancel()

	detected := s.detectProjectPath(detectCtx, userText)
	if detected == "" {
		return
	}

	log.Printf("cwd: auto-detected %s for chat=%d thread=%d; not persisted, use /cwd %s to bind", detected, chatID, threadID, detected)
}

func (s *Service) applyVisionFallback(req *bridge.Request, images []bridge.ImageAttachment) {
	if len(images) == 0 || s.config == nil {
		return
	}
	if vModel, vProvider := s.config.VisionFallback(); vModel != "" {
		log.Printf("vision: switching to fallback model %s/%s for image input", vProvider, vModel)
		req.Options.Model = vModel
		if vProvider != "" {
			req.Options.Provider = vProvider
		}
		return
	}
	log.Printf("vision: no fallback configured, using default model")
}

// routeAgent resolves which agent should handle the message, first by @name
// prefix, then by LLM classification if agents are configured. Classification
// is skipped when there are fewer than 2 agents (no choice to make) or when
// the message is too short to carry useful intent — that saves a 5s round-trip
// to the bridge on trivial follow-ups like "ok" or "obrigado".
func (s *Service) routeAgent(text string) *agents.Agent {
	if s.agents == nil {
		return nil
	}
	agent := s.agents.Route(text)
	if agent != nil {
		return agent
	}
	if len(s.agents.Agents()) < 2 {
		return nil
	}
	if len(strings.TrimSpace(text)) < classifyMinTextLen {
		return nil
	}
	classifyCtx, classifyCancel := context.WithTimeout(context.Background(), classifyTimeout)
	defer classifyCancel()
	return s.agents.Classify(classifyCtx, text, s.classifyFunc())
}

func (s *Service) classifyFunc() agents.ClassifyFunc {
	return func(ctx context.Context, system, prompt string) (string, error) {
		options := bridge.RequestOptions{SystemPrompt: system}
		s.applyConfiguredModelOptions(&options)
		result, err := s.bridge.ExecuteSync(ctx, bridge.Request{
			Command: "query",
			Prompt:  prompt,
			Options: options,
		})
		if err != nil {
			return "", err
		}
		return result.Content, nil
	}
}

// buildBridgeRequest assembles the bridge.Request with agent overrides, session
// resume, and working directory.
func (s *Service) buildBridgeRequest(userText, systemPrompt string, agent *agents.Agent, chatID int64, threadID int, userID int64) bridge.Request {
	req := bridge.Request{
		Command: "query",
		Prompt:  userText,
		Options: bridge.RequestOptions{
			SystemPrompt: systemPrompt,
			ChatID:       chatID,
			ThreadID:     threadID,
			UserID:       userID,
		},
	}
	s.applyConfiguredModelOptions(&req.Options)

	if agent != nil {
		if agent.Model != "" {
			req.Options.Model = agent.Model
		}
		if agent.Cwd != "" {
			req.Options.Cwd = agent.Cwd
		}
		if len(agent.AllowedTools) > 0 {
			req.Options.AllowedTools = agent.AllowedTools
		}
		if len(agent.DisallowedTools) > 0 {
			req.Options.DisallowedTools = agent.DisallowedTools
		}
	}

	if sessionID, active := s.sessions.GetSessionWithState(chatID, threadID, userID); sessionID != "" {
		req.Options.Resume = sessionID
		sidPreview := sessionID
		if len(sidPreview) > 8 {
			sidPreview = sidPreview[:8]
		}
		if active {
			req.Options.Continue = true
			log.Printf("bridge: resume sid=%s (continue)", sidPreview)
		} else {
			log.Printf("bridge: resume sid=%s (cold)", sidPreview)
		}
	}

	cwd := s.effectiveCwd(agent, chatID, threadID)
	if cwd != "" {
		req.Options.Cwd = cwd
	} else {
		req.Options.Cwd = s.botCwd
		req.Options.DisallowedTools = appendUniqueTools(req.Options.DisallowedTools, chatModeDisallowedTools...)

		// Diagnostic: log why file tools are disabled — helps debug issues where
		// the model cannot access files despite the user asking to read/analyze code.
		sessionCwd := ""
		if s.sessions != nil {
			sessionCwd = s.sessions.GetCwd(chatID, threadID)
		}
		log.Printf("chat mode: file tools disabled for chat=%d thread=%d (bindings=%v session_cwd=%q effective_cwd=%q bot_cwd=%q)",
			chatID, threadID, s.bindings != nil, sessionCwd, cwd, s.botCwd)
	}

	// ── Resolve and attach security context ──
	cwd = req.Options.Cwd
	profile := security.DefaultProfileForContext(cwd != "", agent != nil && agent.CapabilityProfile == "", needsWriteTools(agent))

	// Allow agent-level capability_profile override
	if agent != nil && agent.CapabilityProfile != "" {
		profile = security.CapabilityProfile(agent.CapabilityProfile)
	}

	// Intersect agent allowed_tools with profile limits
	effectiveProfile, effectiveTools := security.ResolveProfile(
		profile,
		req.Options.AllowedTools,
		req.Options.DisallowedTools,
		cwd != "",
	)

	// Replace allowed_tools with profile-limited set
	req.Options.AllowedTools = effectiveTools

	// Attach security context
	secCfg := s.getSecurityConfig()
	agentName := ""
	if agent != nil {
		agentName = agent.Name
	}
	req.Options.Security = &bridge.SecurityContext{
		Enabled:   true,
		Profile:   string(effectiveProfile),
		Mode:      string(secCfg.Mode),
		Cwd:       cwd,
		ChatID:    int64(chatID),
		ThreadID:  threadID,
		UserID:    userID,
		AgentName: agentName,
		RequestID: req.RequestID,
	}

	// If profile is privileged, check allow_privileged config
	if effectiveProfile == security.ProfilePrivileged && !secCfg.AllowPrivilegedAgents {
		// Downgrade to execute_safe
		req.Options.Security.Profile = string(security.ProfileExecuteSafe)
		req.Options.AllowedTools = security.ProfileTools(security.ProfileExecuteSafe)
	}

	return req
}

func (s *Service) applyConfiguredModelOptions(options *bridge.RequestOptions) {
	if s == nil || s.config == nil || s.config.IsModelAuto() || options == nil {
		return
	}
	if s.config.DefaultProvider != "" {
		options.Provider = s.config.DefaultProvider
	}
	if s.config.DefaultModel != "" {
		options.Model = s.config.DefaultModel
	}
}

// needsWriteTools returns true if the agent requires write-capable tools.
func needsWriteTools(agent *agents.Agent) bool {
	if agent == nil {
		return true // default to write-capable
	}
	// If agent has explicit allowed_tools, check if it includes write tools
	for _, t := range agent.AllowedTools {
		if t == "Write" || t == "Edit" || t == "Bash" {
			return true
		}
	}
	// If agent has a capability profile, check if it's write-capable
	switch agent.CapabilityProfile {
	case "edit_project", "execute_safe", "privileged":
		return true
	case "observe", "read_only":
		return false
	}
	// Default: check IsReadOnly
	return !agent.IsReadOnly()
}

var chatModeDisallowedTools = []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "LS", "List"}

func appendUniqueTools(existing []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, tool := range existing {
		seen[tool] = struct{}{}
	}
	for _, tool := range additions {
		if _, ok := seen[tool]; ok {
			continue
		}
		existing = append(existing, tool)
		seen[tool] = struct{}{}
	}
	return existing
}

// executeAsync runs bridge execution with typing/progress reporting.
func (s *Service) executeAsync(parentCtx context.Context, chatID int64, threadID int, messageID int, req bridge.Request, userText string, userID int64) {
	stopTyping := s.output.StartTyping(chatID, threadID)
	defer stopTyping()

	progress := s.output.NewProgress(chatID, threadID)
	defer progress.Delete()

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	timeoutTracker := newRunTimeoutTracker()

	// steerDuringExecution sends a steer command to the active bridge session.
	// This injects a message into the model's context without canceling execution.
	steerDuringExecution := func(msg string) {
		steerCtx, steerCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer steerCancel()
		_, err := s.bridge.ExecuteSync(steerCtx, bridge.Request{
			Command: "steer",
			Prompt:  msg,
			Options: bridge.RequestOptions{ChatID: chatID, ThreadID: threadID, UserID: userID},
		})
		if err != nil {
			log.Printf("pipeline: steer failed during execution chat=%d: %v", chatID, err)
		}
	}
	toolTracker := newToolCallTracker(chatID, threadID, s.output, steerDuringExecution)
	loopDetect := newLoopDetector(chatID, threadID, s.output, steerDuringExecution)

	// Max timeout goroutine — safety net after 30min
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline: panic in maxTimeout goroutine: %v", r)
			}
		}()
		timer := time.NewTimer(bridgeExecutionTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			timeoutTracker.mark(timeoutOriginMaxExecution)
			log.Printf("pipeline: max execution timeout (%s) reached chat=%d thread=%d user=%d",
				bridgeExecutionTimeout, chatID, threadID, userID)
			cancel()
		case <-ctx.Done():
		}
	}()

	// Timeout warning goroutine — warns user 5min before hard timeout.
	// Does NOT cancel — only notifies. The max timeout goroutine above
	// handles the actual 30-min cancellation.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline: panic in timeoutWarning goroutine: %v", r)
			}
		}()
		timer := time.NewTimer(bridgeExecutionTimeout - timeoutWarningLead)
		defer timer.Stop()
		select {
		case <-timer.C:
			// Send Telegram warning to user
			userMsg := fmt.Sprintf(
				"⏰ Aproximando do limite de tempo de %s. "+
					"Vou concluir o que tenho e apresentar um resumo parcial.",
				bridgeExecutionTimeout)
			if _, err := s.output.SendText(chatID, threadID, userMsg); err != nil {
				log.Printf("pipeline: SendText(timeout warning) failed for chat=%d: %v", chatID, err)
			}
			// Steer the model to wrap up — this injects into the active model context
			steerDuringExecution(fmt.Sprintf(
				"Você está próximo do limite de tempo de %s. "+
					"Conclua imediatamente o que está fazendo e apresente um resumo parcial "+
					"do que conseguiu até agora.",
				bridgeExecutionTimeout))
		case <-ctx.Done():
		}
	}()

	cancelDone := s.cancelBridgeOnContextDone(ctx, req.RequestID)
	defer cancelDone()

	// Start runlog entry with extended observability context
	cwd := s.effectiveCwd(nil, chatID, threadID)
	agentName := ""
	profile := ""
	if req.Options.Security != nil {
		agentName = req.Options.Security.AgentName
		profile = req.Options.Security.Profile
	}
	runLogStarted := s.startRunLog(startRunLogParams{
		ChatID:    chatID,
		ThreadID:  threadID,
		RequestID: req.RequestID,
		CWD:       cwd,
		Prompt:    userText,
		UserID:    userID,
		AgentName: agentName,
		Provider:  req.Options.Provider,
		Model:     req.Options.Model,
		Profile:   profile,
	})

	// Record bridge_request_started event if the runlog started successfully.
	if runLogStarted {
		s.recordPipelineEvent(chatID, threadID, observability.NewEvent("",
			observability.PhaseBridgeRequestStarted,
			fmt.Sprintf("request_id=%s provider=%s model=%s", req.RequestID, req.Options.Provider, req.Options.Model)))
	}

	var ch <-chan bridge.Event
	var err error

	// usedFallback tracks whether the resilient bridge fell back to a secondary provider.
	var usedFallback bool
	if s.resilient != nil {
		res := s.resilient.Execute(ctx, req, func(msg string) {
			if _, err := s.output.SendText(chatID, threadID, msg); err != nil {
				log.Printf("pipeline: SendText(fallback status) failed for chat=%d: %v", chatID, err)
			}
		})
		if res.Err != nil {
			err = res.Err
		} else {
			ch = res.Events
			usedFallback = res.UsedFallback
		}
		// Record fallback event if the resilient bridge used a fallback provider.
		if usedFallback && runLogStarted {
			s.recordPipelineEvent(chatID, threadID, observability.NewWarnEvent("",
				observability.PhaseFallbackResult,
				fmt.Sprintf("provider=%s model=%s", req.Options.Provider, req.Options.Model)))
		}
	} else {
		ch, err = s.bridge.Execute(ctx, req)
	}

	if ch != nil {
		ch = idleTimeoutWrapper(ctx, ch, s.getIdleTimeout(), cancel, func() {
			timeoutTracker.mark(timeoutOriginIdleBridge)
		})
	}

	var outcome Outcome
	if err != nil {
		if errors.Is(err, errProcessDeath) {
			// Record process death event; recovery below will handle it.
			if runLogStarted {
				s.recordPipelineEvent(chatID, threadID, observability.NewErrorEvent("",
					observability.PhaseBridgeProcessDeath, "bridge process exited during Execute"))
			}
		} else if errors.Is(err, context.Canceled) {
			if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID, userID, timeoutTracker); handled {
				s.output.ConfirmMessage(chatID, messageID)
				return
			}
			log.Printf("pipeline: run canceled by user chat=%d thread=%d user=%d", chatID, threadID, userID)
			if runLogStarted {
				s.patchContinuityFailure(chatID, threadID, "canceled", "cancelado pelo usuário", userID)
				s.completeRunLog(chatID, threadID, runlog.RunCanceled, "", "cancelado pelo usuário")
			}
			return
		} else {
			log.Printf("Bridge execute error: %s", redactSecrets(err.Error()))
			if runLogStarted {
				redacted := redactSecrets(err.Error())
				s.recordPipelineEvent(chatID, threadID, observability.NewErrorEvent("",
					observability.PhaseBridgeExecuteError, redacted))
				s.patchContinuityFailure(chatID, threadID, "failed", redacted, userID)
				s.completeRunLog(chatID, threadID, runlog.RunFailed, "", redacted)
			}
			if s.resilient == nil {
				if err := s.output.SendError(chatID, threadID, bridgeConnectErrorMessage); err != nil {
					log.Printf("Failed to send error to chat %d: %v", chatID, err)
				}
			}
			s.output.ConfirmMessage(chatID, messageID)
			return
		}
	} else {
		toolUseSignal := make(chan struct{}, 16)
		go heartbeatMonitor(ctx.Done(), toolUseSignal, toolTracker, chatID, threadID, s.output)
		outcome = s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText, toolUseSignal, userID, toolTracker, loopDetect)
		if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID, userID, timeoutTracker); handled {
			s.output.ConfirmMessage(chatID, messageID)
			return
		}
		if outcome == OutcomeSuccess {
			s.bridgeFailures.reset()
			return
		}
		if outcome != OutcomeProcessDeath {
			if runLogStarted {
				s.patchContinuityFailure(chatID, threadID, "failed", "", userID)
				s.completeRunLog(chatID, threadID, runlog.RunFailed, "", "")
			}
			return
		}
	}

	s.bridgeFailures.record()
	log.Printf("bridge: process died mid-request, retrying for chat=%d thread=%d", chatID, threadID)

	// Mark process death in session store for lifecycle evaluation
	if s.sessions != nil {
		s.sessions.MarkProcessDeath(chatID, threadID, userID)
	}

	if runLogStarted {
		s.patchContinuityFailure(chatID, threadID, "failed", "process death, retrying", userID)
		s.completeRunLog(chatID, threadID, runlog.RunFailed, "", "process death, retrying")
		s.recordPipelineEvent(chatID, threadID, observability.NewWarnEvent("",
			observability.PhaseRetryStarted, "process death recovery, retrying"))
	}

	if s.bridgeFailures.inCooldown() {
		remaining := s.bridgeFailures.cooldownRemaining()
		log.Printf("bridge: in cooldown, skipping retry for chat=%d", chatID)
		if err := s.output.SendError(chatID, threadID, bridgeCooldownMessage(remaining)); err != nil {
			log.Printf("pipeline: SendError(cooldown) failed for chat=%d: %v", chatID, err)
		}
		s.output.ConfirmMessage(chatID, messageID)
		return
	}

	reconnectMsg, sErr := s.output.SendText(chatID, threadID, "⚡ Reconectando...")
	if sErr != nil {
		log.Printf("pipeline: SendText(reconnect) failed for chat=%d: %v", chatID, sErr)
	}

	retryReq := req
	retryReq.Options.Continue = false
	retryReq.RequestID = ""
	if sid := s.sessions.GetSession(chatID, threadID, userID); sid != "" {
		retryReq.Options.Resume = sid
		log.Printf("bridge: retry with resume file=%s", filepath.Base(sid))
	}

	ch, err = s.bridge.Execute(ctx, retryReq)
	s.output.DeleteMessage(reconnectMsg)
	if err != nil {
		log.Printf("bridge: retry failed for chat=%d: %s", chatID, redactSecrets(err.Error()))
		s.patchContinuitySessionCold(chatID, threadID, "bridge retry failed: "+redactSecrets(err.Error()))
		if runLogStarted {
			s.recordPipelineEvent(chatID, threadID, observability.NewErrorEvent("",
				observability.PhaseRetryFailed, "retry failed: process death persisted"))
		}
		if err := s.output.SendError(chatID, threadID, bridgeRetryFailedMessage); err != nil {
			log.Printf("pipeline: SendError(retry failed) for chat=%d: %v", chatID, err)
		}
		s.output.ConfirmMessage(chatID, messageID)
		return
	}

	if ch != nil {
		ch = idleTimeoutWrapper(ctx, ch, s.getIdleTimeout(), cancel, func() {
			timeoutTracker.mark(timeoutOriginIdleBridge)
		})
	}

	toolUseSignal := make(chan struct{}, 16)
	go heartbeatMonitor(ctx.Done(), toolUseSignal, toolTracker, chatID, threadID, s.output)
	outcome = s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText, toolUseSignal, userID, toolTracker, loopDetect)
	if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID, userID, timeoutTracker); handled {
		s.output.ConfirmMessage(chatID, messageID)
		return
	}
	s.handleRetryOutcome(chatID, threadID, messageID, outcome, userID)
}

func (s *Service) cancelBridgeOnContextDone(ctx context.Context, requestID string) func() {
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline: panic in cancelBridgeOnContextDone: %v", r)
			}
		}()
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := s.bridge.CancelRequest(cancelCtx, requestID); err != nil {
				log.Printf("bridge: cancel request %s failed: %s", requestID, redactSecrets(err.Error()))
			}
		case <-done:
		}
	}()
	return func() { close(done) }
}

func (s *Service) handleContextOutcome(parentCtx context.Context, ctx context.Context, chatID int64, threadID int, userID int64, tracker ...*runTimeoutTracker) bool {
	if parentCtx.Err() != nil {
		log.Printf("pipeline: run canceled chat=%d thread=%d user=%d", chatID, threadID, userID)
		s.patchContinuityFailure(chatID, threadID, "canceled", "cancelado pelo usuário", userID)
		s.completeRunLog(chatID, threadID, runlog.RunCanceled, "", "cancelado pelo usuário")
		s.recordPipelineEvent(chatID, threadID, observability.NewEvent("",
			observability.PhaseRunCanceled, "cancelado pelo usuário"))
		return true
	}
	if ctx.Err() != nil {
		origin, elapsed := timeoutDetails(tracker...)
		log.Printf("pipeline: run timeout origin=%s elapsed=%s chat=%d thread=%d user=%d", origin, elapsed.Round(time.Second), chatID, threadID, userID)
		s.patchContinuityFailure(chatID, threadID, "timed_out", origin, userID)
		s.completeRunLog(chatID, threadID, runlog.RunTimedOut, "", origin)
		s.recordPipelineEvent(chatID, threadID, observability.NewErrorEvent("",
			observability.PhaseRunTimedOut,
			fmt.Sprintf("origin=%s elapsed=%s", origin, elapsed.Round(time.Second))))
		if s.sessions != nil {
			s.sessions.MarkFailure(chatID, threadID, userID, origin)
		}
		if err := s.output.SendError(chatID, threadID, buildTimeoutMessage(origin)); err != nil {
			log.Printf("pipeline: SendError(timeout) failed for chat=%d: %v", chatID, err)
		}
		return true
	}
	return false
}

func timeoutDetails(trackers ...*runTimeoutTracker) (string, time.Duration) {
	if len(trackers) == 0 {
		return timeoutOriginUnknown, 0
	}
	return trackers[0].snapshot()
}

func sessionUserID(userID ...int64) int64 {
	if len(userID) == 0 {
		return 0
	}
	return userID[0]
}

func (s *Service) handleRetryOutcome(chatID int64, threadID int, messageID int, outcome Outcome, userID int64) {
	switch outcome {
	case OutcomeSuccess:
		s.bridgeFailures.reset()
	case OutcomeProcessDeath:
		s.bridgeFailures.record()
		if s.sessions != nil {
			s.sessions.MarkProcessDeath(chatID, threadID, userID)
		}
		s.patchContinuitySessionCold(chatID, threadID, "bridge retry process death")
		if err := s.output.SendError(chatID, threadID, bridgeRetryFailedMessage); err != nil {
			log.Printf("pipeline: SendError(retry outcome) failed for chat=%d: %v", chatID, err)
		}
		s.output.ConfirmMessage(chatID, messageID)
	}
}

// buildHeartbeatMessage formats the user-visible heartbeat message.
// Per Long Flow UX v2: human progress language, no technical terms like
// "chamadas de ferramenta" or tool counts.
func buildHeartbeatMessage(elapsed time.Duration, beatCount, toolCount int) string {
	if toolCount > 0 && beatCount%heartbeatToolThreshold == 0 {
		return fmt.Sprintf("⏱️ %s — Ainda estou trabalhando no pedido. Vou consolidar o progresso em breve.", elapsed)
	}
	return fmt.Sprintf("⏱️ %s — Ainda estou processando.", elapsed)
}

// heartbeatMonitor sends a "still thinking" update when no tool_use event
// arrives within heartbeatThreshold. It resets on each tool_use event so the
// user only sees the message when the model is thinking without tools.
// Stopped by doneCh (e.g., ctx.Done()).
func heartbeatMonitor(doneCh <-chan struct{}, toolUseSignal <-chan struct{}, toolTracker *toolCallTracker, chatID int64, threadID int, output Output) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("pipeline: panic in heartbeatMonitor: %v", r)
		}
	}()

	lastTool := time.Now()
	beatSent := false
	beatCount := 0
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-doneCh:
			return
		case <-toolUseSignal:
			lastTool = time.Now()
			beatSent = false
		case <-ticker.C:
			if time.Since(lastTool) >= heartbeatThreshold && !beatSent {
				elapsed := time.Since(lastTool).Round(time.Second)
				beatCount++
				toolCount := toolTracker.countLocked()
				msg := buildHeartbeatMessage(elapsed, beatCount, toolCount)
				if _, err := output.SendText(chatID, threadID, msg); err != nil {
					log.Printf("pipeline: heartbeat SendText failed for chat=%d: %v", chatID, err)
				}
				beatSent = true
			}
		}
	}
}

// ProcessBridgeEvents reads bridge events and sends responses to the output.
// toolUseSignal, if non-nil, receives a signal on every tool_use event so a
// caller can monitor thinking gaps (heartbeat).
// toolTracker, if non-nil, is used to count tool calls and warn on explosion.
func (s *Service) ProcessBridgeEvents(chatID int64, threadID int, messageID int, ch <-chan bridge.Event, progress ProgressReporter, userText string, toolUseSignal chan<- struct{}, userID int64, toolTracker *toolCallTracker, loopDetect *loopDetector) Outcome {
	var (
		assistantText       strings.Builder
		lastStreamFlush     = time.Now()
		streamFlushInterval = 3 * time.Second
	)

	for ev := range ch {
		switch ev.Type {
		case "system":
			s.handleSystemEvent(chatID, threadID, ev, userID)
			if ev.SessionID != "" {
				s.updateRunLogSession(chatID, threadID, ev.SessionID)
			}
			// Record bridge_system event with model info and available tool names.
			if s.runLog != nil {
				modelInfo := ev.Model
				sessionFile := ev.SessionFile
				msg := fmt.Sprintf("model=%s session=%s", modelInfo, filepath.Base(sessionFile))
				if len(ev.Tools) > 0 {
					msg += fmt.Sprintf(" tools=[%s]", strings.Join(ev.Tools, ", "))
				}
				s.recordPipelineEvent(chatID, threadID, observability.NewEvent("",
					observability.PhaseBridgeSystem, msg))
			}
		case "tool_use":
			toolName := ev.Name
			if toolName == "" {
				toolName = "tool"
			}
			// Flush pending thought block before showing tool
			if progress != nil {
				if text := strings.TrimSpace(assistantText.String()); text != "" {
					progress.ReportText(text)
				}
				progress.ReportTool(toolName)
			}
			lastStreamFlush = time.Now()
			s.recordToolUse(chatID, threadID, toolName)
			// Track tool call count and warn on explosion thresholds
			if toolTracker != nil {
				toolTracker.increment(toolName)
			}
			// Detect repetitive tool call patterns (loops)
			if loopDetect != nil {
				loopDetect.record(toolName, ev.Input)
			}
			// Record bridge_tool_use event.
			s.recordPipelineEvent(chatID, threadID, observability.NewEvent("",
				observability.PhaseBridgeToolUse,
				fmt.Sprintf("tool=%s", toolName)))
			if toolUseSignal != nil {
				select {
				case toolUseSignal <- struct{}{}:
				default:
				}
			}

		case "tool_result":
			// Append a truncated, redacted summary to the tool tracking state.
			// Also show the summary in the live progress display.
			content := eventContent(ev)
			summary := summarizeToolResult(content)
			if summary != "" {
				if s.runLog != nil {
					s.recordToolResult(chatID, threadID, summary)
				}
				if progress != nil {
					progress.ReportToolResult(summary)
				}
			}
			// Record bridge_tool_result event.
			s.recordPipelineEvent(chatID, threadID, observability.NewEvent("",
				observability.PhaseBridgeToolResult, summary))

		case "assistant":
			delta := eventContent(ev)
			assistantText.WriteString(delta)

			// Periodic flush — send full accumulated text so nothing is lost
			if time.Since(lastStreamFlush) >= streamFlushInterval {
				if progress != nil {
					if text := strings.TrimSpace(assistantText.String()); text != "" {
						progress.ReportText(text)
					}
				}
				// Save partial assistant text for checkpoint on timeout
				s.savePartialAssistant(chatID, threadID, assistantText.String())
				lastStreamFlush = time.Now()
			}
		case "result":
			return s.handleResultEvent(chatID, threadID, messageID, ev, &assistantText, userText, userID)
		case "error":
			return s.handleErrorEvent(chatID, threadID, messageID, ev, userID)
		case "compaction_start", "compaction_end":
			// Compaction events reset idle timer and provide observability.
			if s.runLog != nil {
				s.recordPipelineEvent(chatID, threadID, observability.NewEvent("",
					observability.PhaseBridgeSystem, fmt.Sprintf("event=%s", ev.Type)))
			}
		case "agent_start", "agent_end", "turn_start", "turn_end":
			// Agent/turn lifecycle events reset idle timer.
		case "auto_retry_start", "auto_retry_end":
			// Retry events reset idle timer.
			if s.runLog != nil {
				msg := fmt.Sprintf("event=%s", ev.Type)
				if ev.Content != "" {
					msg += " " + ev.Content
				}
				s.recordPipelineEvent(chatID, threadID, observability.NewEvent("",
					observability.PhaseBridgeSystem, msg))
			}
		default:
			log.Printf("Bridge event (ignored): %s", ev.Type)
		}
	}

	return OutcomeProcessDeath
}

func (s *Service) handleSystemEvent(chatID int64, threadID int, ev bridge.Event, userID int64) {
	if ev.SessionFile == "" {
		return
	}
	s.sessions.SetSession(chatID, threadID, userID, ev.SessionFile)
	s.patchContinuitySessionID(chatID, threadID, ev.SessionFile)
}

func eventContent(ev bridge.Event) string {
	return bridge.EventContent(ev)
}

func (s *Service) handleResultEvent(chatID int64, threadID int, messageID int, ev bridge.Event, assistantText *strings.Builder, userText string, userID int64) Outcome {
	content := eventContent(ev)
	if content != "" {
		prior := assistantText.String()
		if prior != "" && prior != content {
			diff := len(prior) - len(content)
			if diff < 0 {
				diff = -diff
			}
			// Só loga divergência significativa (>500 chars). Divergências pequenas
			// são normais: o SDK pode consolidar texto entre tool_use/tool_result
			// de forma diferente dos deltas de streaming.
			if diff > 500 {
				log.Printf("bridge: result.Content diverges from accumulated assistant text (%d vs %d chars, diff=%d)", len(prior), len(content), diff)
			}
		}
		assistantText.Reset()
		assistantText.WriteString(content)
	}

	// Store session file path as fallback in case the system event was missed.
	if ev.SessionFile != "" {
		existing := s.sessions.GetSession(chatID, threadID, userID)
		if existing == "" {
			s.sessions.SetSession(chatID, threadID, userID, ev.SessionFile)
			s.patchContinuitySessionID(chatID, threadID, ev.SessionFile)
		}
	}

	s.recordUsage(chatID, threadID, ev, userID)

	// Record bridge_result event with usage data.
	s.recordPipelineEvent(chatID, threadID, observability.NewEvent("",
		observability.PhaseBridgeResult,
		fmt.Sprintf("tokens_in=%d tokens_out=%d cost=$%.4f turns=%d",
			ev.InputTokens, ev.OutputTokens, ev.CostUSD, ev.NumTurns)))

	finalText := strings.TrimSpace(assistantText.String())

	if finalText == "" {
		toolSummary := s.getRunToolSummary(chatID, threadID)
		return s.handleEmptyResult(chatID, threadID, messageID, ev, userText, toolSummary, userID)
	}

	safeFinalText := sanitizeExecutionPlanForChat(finalText)

	// Capture runID before completeRunLog cleans up runLogStates.
	successRunID := s.getRunID(chatID, threadID)
	s.completeRunLog(chatID, threadID, runlog.RunCompleted, safeFinalText, "")
	// Record run_completed event.
	s.recordPipelineEvent(chatID, threadID, observability.NewEvent("",
		observability.PhaseRunCompleted, "status=completed"))

	if ok, outcome := s.handlePlanExecution(chatID, threadID, messageID, finalText, safeFinalText, successRunID, userText, userID); ok {
		return outcome
	}

	return s.handleNormalReply(chatID, threadID, messageID, safeFinalText, successRunID, userText, userID)
}

// handleEmptyResult handles the case where the bridge returned no text.
// It distinguishes between "worked but empty" (tokens consumed) and "no work at all".
func (s *Service) handleEmptyResult(chatID int64, threadID int, messageID int, ev bridge.Event, userText string, toolSummary string, userID int64) Outcome {
	if emptyResultHadWork(ev) {
		log.Printf("bridge: empty result after work chat=%d thread=%d request=%s turns=%d cost=$%.4f in=%d out=%d",
			chatID, threadID, ev.RequestID, ev.NumTurns, ev.CostUSD, ev.InputTokens, ev.OutputTokens)

		// Mark empty result so next turn does not Continue into a suspect session.
		// Skip billing errors — they're provider issues, not session failures.
		if s.sessions != nil && !isBillingError(ev.Message) && !isBillingError(ev.Content) {
			s.sessions.MarkEmptyResult(chatID, threadID, userID)
		}

		s.patchContinuityFailure(chatID, threadID, "failed", "empty result after work", userID)
		s.completeRunLog(chatID, threadID, runlog.RunFailed, "", "empty result after work")
		s.recordPipelineEvent(chatID, threadID, observability.NewErrorEvent("",
			observability.PhaseRunFailed, "empty result after work"))

		recoveryMsg := buildEmptyResultRecoveryMessage(toolSummary)
		if err := s.output.SendError(chatID, threadID, recoveryMsg); err != nil {
			log.Printf("Failed to send recovery message to chat %d: %v", chatID, err)
		}
	} else {
		log.Printf("bridge: empty result (no work) chat=%d thread=%d request=%s",
			chatID, threadID, ev.RequestID)
		s.patchContinuityFailure(chatID, threadID, "failed", "empty result", userID)
		s.completeRunLog(chatID, threadID, runlog.RunFailed, "", "empty result")
		s.recordPipelineEvent(chatID, threadID, observability.NewErrorEvent("",
			observability.PhaseRunFailed, "empty result"))
		if err := s.output.SendError(chatID, threadID, bridgeEmptyResultMessage); err != nil {
			log.Printf("Failed to send empty-result error to chat %d: %v", chatID, err)
		}
	}

	s.output.ConfirmMessage(chatID, messageID)
	return OutcomeLLMError
}

// handlePlanExecution checks whether the assistant output contains an execution
// plan and, if so, starts the orchestrator. Returns (true, outcome) when a plan
// was executed, or (false, OutcomeSuccess) to continue with normal reply.
func (s *Service) handlePlanExecution(chatID int64, threadID int, messageID int, finalText string, safeFinalText string, successRunID string, userText string, userID int64) (bool, Outcome) {
	handled, outcome := s.tryExecutePlan(chatID, threadID, messageID, finalText, userID)
	if !handled {
		return false, OutcomeSuccess
	}
	if outcome != OutcomeSuccess {
		return true, outcome
	}

	s.output.ConfirmMessage(chatID, messageID)
	s.afterSuccessfulTurn(chatID, threadID, userText, safeFinalText, successRunID, userID)
	return true, OutcomeSuccess
}

// handleNormalReply sends the assistant's text response to the chat as a
// normal reply and finalizes the turn.
func (s *Service) handleNormalReply(chatID int64, threadID int, messageID int, safeFinalText string, successRunID string, userText string, userID int64) Outcome {
	if err := s.output.SendReply(chatID, threadID, safeFinalText); err != nil {
		log.Printf("Failed to send reply to chat %d: %v", chatID, err)
	}
	s.output.ConfirmMessage(chatID, messageID)
	s.afterSuccessfulTurn(chatID, threadID, userText, safeFinalText, successRunID, userID)
	return OutcomeSuccess
}

// recordUsage logs token usage from the bridge result to the debug log.
// PI SDK compaction (enabled in SettingsManager) handles context pruning automatically.
func (s *Service) recordUsage(chatID int64, threadID int, ev bridge.Event, userID int64) {
	if ev.CostUSD <= 0 && ev.NumTurns <= 0 {
		return
	}
	log.Printf("session usage: chat=%d thread=%d user=%d cost=$%.4f turns=%d input=%d output=%d",
		chatID, threadID, userID, ev.CostUSD, ev.NumTurns, ev.InputTokens, ev.OutputTokens)
}

func (s *Service) tryExecutePlan(chatID int64, threadID int, messageID int, finalText string, userID int64) (bool, Outcome) {
	if s.orchestrator == nil {
		return false, OutcomeSuccess
	}
	plan, err := s.orchestrator.ExtractPlan(finalText)
	if err != nil {
		if orchestrator.ContainsPlanMarker(finalText) {
			log.Printf("Execution plan marker detected but plan was invalid: %v", err)
			if err := s.output.SendError(chatID, threadID, "Plano de execução gerado, mas não consegui interpretar o JSON. Não vou enviar os prompts internos no chat."); err != nil {
				log.Printf("pipeline: SendError(plan json parse) failed for chat=%d: %v", chatID, err)
			}
			return true, OutcomeSuccess
		}
		return false, OutcomeSuccess
	}
	if plan == nil {
		if orchestrator.ContainsPlanMarker(finalText) {
			log.Printf("Execution plan marker detected but plan block was incomplete")
			if err := s.output.SendError(chatID, threadID, "Plano de execução gerado, mas o bloco veio incompleto. Não vou enviar os prompts internos no chat."); err != nil {
				log.Printf("pipeline: SendError(plan block) failed for chat=%d: %v", chatID, err)
			}
			return true, OutcomeSuccess
		}
		return false, OutcomeSuccess
	}
	log.Printf("Execution plan detected with %d tasks", len(plan.Tasks))
	if displayText := orchestrator.StripPlanBlock(finalText); displayText != "" {
		if err := s.output.SendReply(chatID, threadID, displayText); err != nil {
			log.Printf("pipeline: SendReply(plan display) failed for chat=%d: %v", chatID, err)
		}
	}

	// Resolve effective working directory — refuse execution without one
	cwd := s.effectiveCwd(nil, chatID, threadID)
	if cwd == "" {
		log.Printf("orchestration: refusing plan execution for chat=%d thread=%d: no cwd bound", chatID, threadID)
		if err := s.output.SendError(chatID, threadID, "Não encontrei um diretório de trabalho (cwd) para executar o plano. Use /cwd para fixar um projeto e tente novamente."); err != nil {
			log.Printf("pipeline: SendError(no cwd) failed for chat=%d: %v", chatID, err)
		}
		return true, OutcomeSuccess
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline: panic in ExecuteApprovedPlan: %v", r)
			}
		}()
		s.output.ExecuteApprovedPlan(chatID, threadID, messageID, cwd, userID, plan)
	}()
	return true, OutcomeSuccess
}

func sanitizeExecutionPlanForChat(text string) string {
	if !orchestrator.ContainsPlanMarker(text) {
		return text
	}
	displayText := strings.TrimSpace(orchestrator.StripPlanBlock(text))
	if displayText == "" {
		return "Plano de execução gerado para o orquestrador. Prompts internos omitidos."
	}
	return displayText + "\n\n[plano de execução interno omitido]"
}

// --- Run log lifecycle ---

func runLogKey(chatID int64, threadID int) string {
	return fmt.Sprintf("%d:%d", chatID, threadID)
}

// startRunLogParams carries the extended context needed to populate a
// run_journal start row. All fields are populated before Bridge execution.
type startRunLogParams struct {
	ChatID    int64
	ThreadID  int
	RequestID string
	CWD       string
	Prompt    string
	UserID    int64
	AgentName string
	Provider  string
	Model     string
	Profile   string
}

// startRunLog creates a new runlog entry and stores the per-run state.
// RunID is set to a uuid for durable unique identification across restarts.
// Returns true if the runlog was started.
func (s *Service) startRunLog(p startRunLogParams) bool {
	if s.runLog == nil || p.RequestID == "" {
		return false
	}
	key := runLogKey(p.ChatID, p.ThreadID)

	s.runLogMu.Lock()
	defer s.runLogMu.Unlock()

	runID := uuid.NewString()
	now := time.Now()
	runLogCtx, runLogCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer runLogCancel()
	err := s.runLog.Start(runLogCtx, runlog.RunRecord{
		RunID:             runID,
		ChatID:            p.ChatID,
		ThreadID:          p.ThreadID,
		RequestID:         p.RequestID,
		CWD:               p.CWD,
		Prompt:            truncatePrompt(redactSecrets(p.Prompt)),
		StartedAt:         now,
		UserID:            p.UserID,
		EntryPoint:        observability.EntryPointTelegram,
		AgentName:         p.AgentName,
		Provider:          p.Provider,
		Model:             p.Model,
		CapabilityProfile: p.Profile,
	})
	if err != nil {
		log.Printf("runlog: failed to start %s: %v", p.RequestID, err)
		return false
	}
	s.runLogStates[key] = &runLogState{runID: runID}
	return true
}

// recordPipelineEvent records a single observable event for the active run
// on a chat/thread. Best-effort: errors are logged, never block the caller.
// Uses a 500ms context timeout to avoid blocking the pipeline.
func (s *Service) recordPipelineEvent(chatID int64, threadID int, ev observability.RunEvent) {
	if s.runLog == nil {
		return
	}
	runID := s.getRunID(chatID, threadID)
	if runID == "" {
		return
	}
	ev.RunID = runID
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := s.runLog.RecordEvent(ctx, runlog.RunEvent{
		RunID:        ev.RunID,
		Timestamp:    ev.Timestamp.Unix(),
		Phase:        ev.Phase,
		Level:        ev.Level,
		Message:      ev.Message,
		MetadataJSON: ev.MetadataJSON,
	}); err != nil {
		slog.Warn("observability: event dropped", "run_id", runID, "phase", ev.Phase, "error", err)
	}
}

// updateRunLogSession updates the session ID for an active runlog entry.
func (s *Service) updateRunLogSession(chatID int64, threadID int, sessionID string) {
	if s.runLog == nil || sessionID == "" {
		return
	}
	key := runLogKey(chatID, threadID)

	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()
	if !ok || state == nil {
		return
	}

	updateCtx, updateCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer updateCancel()
	if err := s.runLog.Update(updateCtx, runlog.RunUpdate{
		RunID:     state.runID,
		SessionID: &sessionID,
	}); err != nil {
		log.Printf("runlog: failed to update session for %s: %v", state.runID, err)
	}
}

// recordToolUse appends a tool name to the in-memory tool summary for a run.
func (s *Service) recordToolUse(chatID int64, threadID int, toolName string) {
	if s.runLog == nil || toolName == "" {
		return
	}
	key := runLogKey(chatID, threadID)

	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()
	if !ok || state == nil {
		return
	}

	state.mu.Lock()
	needsUpdate := false
	var toolSummary string
	if state.summary.Len() > 0 {
		state.summary.WriteString(", ")
	}
	state.summary.WriteString(toolName)

	// Persist summary every 5 tools to avoid loss on crash
	state.summaryCount++
	if state.summaryCount%5 == 0 {
		toolSummary = state.summary.String()
		needsUpdate = true
	}
	state.mu.Unlock()

	if needsUpdate {
		state.wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("pipeline: panic in recordToolUse update: %v", r)
				}
			}()
			defer state.wg.Done()
			updateCtx, updateCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer updateCancel()
			if err := s.runLog.Update(updateCtx, runlog.RunUpdate{
				RunID:       state.runID,
				ToolSummary: &toolSummary,
			}); err != nil {
				log.Printf("runlog: failed to persist tool summary for %s: %v", state.runID, err)
			}
		}()
	}
}

// savePartialAssistant stores the current partial assistant response text
// into the run log state. Used for checkpoint on timeout so the resume has
// context of what the model was saying.
func (s *Service) savePartialAssistant(chatID int64, threadID int, text string) {
	key := runLogKey(chatID, threadID)
	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()
	if !ok || state == nil {
		return
	}
	state.mu.Lock()
	if len(text) > 2000 {
		text = text[:2000]
	}
	state.partialAssistant = text
	state.mu.Unlock()
}

// recordToolResult appends a summarized tool result to the tool summary.
func (s *Service) recordToolResult(chatID int64, threadID int, summary string) {
	if s.runLog == nil || summary == "" {
		return
	}
	key := runLogKey(chatID, threadID)

	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()
	if !ok || state == nil {
		return
	}

	state.mu.Lock()
	state.summary.WriteString(" → [")
	state.summary.WriteString(summary)
	state.summary.WriteString("]")
	state.mu.Unlock()
}

// completeRunLog marks the runlog entry with a terminal status and checkpoint.
// All persisted data is redacted before storage to prevent credential leakage.
func (s *Service) completeRunLog(chatID int64, threadID int, status runlog.RunStatus, checkpoint, errMsg string) {
	key := runLogKey(chatID, threadID)

	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	delete(s.runLogStates, key)
	s.runLogMu.Unlock()

	if !ok || state == nil || s.runLog == nil {
		return
	}

	// Capture final tool summary and partial assistant text under the per-state lock
	state.mu.Lock()
	summary := state.summary.String()
	partialAssistant := state.partialAssistant
	state.mu.Unlock()

	// Defensive redaction: assistant output may contain credentials.
	summary = redactSecrets(summary)
	checkpoint = redactSecrets(checkpoint)
	errMsg = redactSecrets(errMsg)

	// Build checkpoint with partial assistant text if available
	if checkpoint == "" {
		checkpoint = buildCheckpoint(status, "", summary, errMsg, partialAssistant)
	} else {
		checkpoint = buildCheckpoint(status, checkpoint, summary, errMsg, partialAssistant)
	}

	// Wait for any in-flight DB updates (e.g., tool summary from recordToolUse)
	// before completing the runlog entry, ensuring consistent ordering.
	state.wg.Wait()

	completeCtx, completeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer completeCancel()
	if err := s.runLog.Complete(completeCtx, state.runID, status, checkpoint, errMsg); err != nil {
		log.Printf("runlog: failed to complete %s (status=%s): %v", state.runID, status, err)
	}

	// Flush session update with final summary
	if summary != "" {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer flushCancel()
		if err := s.runLog.Update(flushCtx, runlog.RunUpdate{
			RunID:       state.runID,
			ToolSummary: &summary,
		}); err != nil {
			log.Printf("runlog: failed to update summary for %s: %v", state.runID, err)
		}
	}
}

// buildCheckpoint formats a textual checkpoint from run status and context.
// If partialAssistant is provided, it includes the last partial response text
// so the model can continue from where it left off on resume.
func buildCheckpoint(status runlog.RunStatus, checkpoint, toolSummary, errMsg string, extraArgs ...string) string {
	var sb strings.Builder
	sb.WriteString("Status: ")
	sb.WriteString(string(status))
	if toolSummary != "" {
		sb.WriteString("\nFerramentas: ")
		sb.WriteString(toolSummary)
	}
	if checkpoint != "" {
		sb.WriteString("\nResposta/último resumo: ")
		sb.WriteString(truncateCheckpoint(checkpoint))
	}
	// Include partial assistant response (last thing model was saying)
	if len(extraArgs) > 0 && extraArgs[0] != "" {
		sb.WriteString("\nResposta parcial do assistente: ")
		sb.WriteString(truncateCheckpoint(extraArgs[0]))
	}
	if errMsg != "" {
		sb.WriteString("\nErro: ")
		sb.WriteString(errMsg)
	}
	if status == runlog.RunTimedOut {
		sb.WriteString("\nPróximo passo: continue a partir deste checkpoint")
	}
	return sb.String()
}

func truncatePrompt(prompt string) string {
	const maxPromptBytes = 500
	if len(prompt) > maxPromptBytes {
		// Use rune-aware truncation to avoid splitting multi-byte characters.
		trimmed := prompt
		for len(trimmed) > maxPromptBytes {
			trimmed = trimmed[:len(trimmed)-1]
		}
		// Ensure valid UTF-8 at the boundary.
		for i := 0; i < 4 && len(trimmed) > 0; i++ {
			if trimmed[len(trimmed)-1]&0xC0 != 0x80 {
				break
			}
			trimmed = trimmed[:len(trimmed)-1]
		}
		return trimmed + "..."
	}
	return prompt
}

func truncateCheckpoint(s string) string {
	if len(s) > 2000 {
		return s[:2000] + "..."
	}
	return s
}


