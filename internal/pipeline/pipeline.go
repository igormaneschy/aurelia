package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/security"
)

// runLogState tracks per-run state for the run journal.
// mu serializes summary mutations independently of the runLogState map lock,
// preventing data races between recordToolUse/recordToolResult and completeRunLog.
type runLogState struct {
	mu           sync.Mutex
	runID        string
	summary      strings.Builder
	summaryCount int
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
	bridgeExecutionTimeout = 30 * time.Minute
	idleBridgeTimeout      = 15 * time.Minute

	bridgeConnectErrorMessage = "Falha ao conectar com o processador.\n\n" +
		"Dica: verifique se o daemon está rodando. Se persistir, tente /new para reiniciar a sessão."
	bridgeRetryFailedMessage = "Processador reiniciado mas não conseguiu completar. Tente novamente.\n\n" +
		"Dica: se persistir, use /new para reiniciar a sessão."
	bridgeTimeoutMessage = "Tempo limite atingido antes de concluir.\n\n" +
		"A solicitação foi muito complexa. Tente dividir em partes menores."

	heartbeatInterval  = 10 * time.Second
	heartbeatThreshold = 15 * time.Second

	timeoutOriginUnknown      = "unknown_timeout"
	timeoutOriginMaxExecution = "max_execution_timeout"
	timeoutOriginIdleBridge   = "idle_bridge_timeout"
	timeoutOriginBridgeQuery  = "bridge_query_timeout"
	timeoutOriginProviderPI   = "provider/pi_timeout"
)

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

type concurrentMessageKind int

const (
	concurrentEnqueue concurrentMessageKind = iota
	concurrentCancel
	concurrentSupersede
	concurrentStatus
)

func classifyConcurrentMessage(text string) concurrentMessageKind {
	n := normalizeConcurrentText(text)
	if n == "" {
		return concurrentStatus
	}
	if isStatusMessage(n) {
		return concurrentStatus
	}
	if isCancelOnlyMessage(n) {
		return concurrentCancel
	}
	if isSupersedeMessage(n) {
		return concurrentSupersede
	}
	return concurrentEnqueue
}

func normalizeConcurrentText(text string) string {
	n := strings.ToLower(strings.TrimSpace(text))
	replacer := strings.NewReplacer("á", "a", "à", "a", "ã", "a", "â", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c")
	n = replacer.Replace(n)
	n = strings.Trim(n, ".,!?:; ")
	return strings.Join(strings.Fields(n), " ")
}

func isCancelOnlyMessage(n string) bool {
	exact := map[string]bool{
		"para": true, "pare": true, "parar": true, "stop": true,
		"cancela": true, "cancelar": true, "cancele": true,
		"interrompe": true, "interrompa": true,
		"esquece": true, "deixa pra la": true, "nao precisa": true,
	}
	if exact[n] {
		return true
	}
	needles := []string{"pode parar", "pode cancelar", "nao precisa mais", "para isso", "cancela isso", "cancele isso"}
	for _, needle := range needles {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}

func isSupersedeMessage(n string) bool {
	needles := []string{
		"na verdade", "corrigindo", "em vez", "ao inves", "melhor", "mudei", "troque",
		"nao corrija", "apenas", "so faca", "so teste", "topico errado", "lugar errado",
		"nao era", "errado", "pare e", "cancele e", "ignore o anterior",
	}
	for _, needle := range needles {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}

func isStatusMessage(n string) bool {
	needles := []string{"conseguiu", "terminou", "acabou", "status", "andamento", "ja foi", "ta pronto", "esta pronto"}
	for _, needle := range needles {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
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

	_, active := s.activeSessions.Load(key)
	if !active {
		// No active session — start new query in a goroutine
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("pipeline: panic in processRun: %v", r)
				}
			}()
			s.processRun(input)
		}()
		return nil
	}

	// Active session — classify and send appropriate bridge command
	switch classifyConcurrentMessage(text) {
	case concurrentCancel:
		// Stop the old goroutine so it doesn't retry after abort
		if cancelVal, loaded := s.activeSessions.LoadAndDelete(key); loaded {
			if cancel, ok := cancelVal.(context.CancelFunc); ok {
				cancel()
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
		if cancelVal, loaded := s.activeSessions.LoadAndDelete(key); loaded {
			if cancel, ok := cancelVal.(context.CancelFunc); ok {
				cancel()
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
		// Start a new goroutine to process the steered session
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("pipeline: panic in processRun(supersede): %v", r)
				}
			}()
			s.processRun(input)
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
		_ = s.output.SendError(input.chatID, input.threadID, "Falha ao montar o prompt de sistema.")
		s.output.ConfirmMessage(input.chatID, input.messageID)
		return
	}

	req := s.buildBridgeRequest(userText, systemPrompt, agent, input.chatID, input.threadID, input.userID)
	req.RequestID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	req.Options.Images = input.images
	s.applyVisionFallback(&req, input.images)

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
			_, _ = s.output.SendText(chatID, threadID, msg)
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
		ch = idleTimeoutWrapper(ctx, ch, idleBridgeTimeout, cancel, func() {
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
		go heartbeatMonitor(ctx.Done(), toolUseSignal, chatID, threadID, s.output)
		outcome = s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText, toolUseSignal, userID)
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

	if runLogStarted {
		s.patchContinuityFailure(chatID, threadID, "failed", "process death, retrying", userID)
		s.completeRunLog(chatID, threadID, runlog.RunFailed, "", "process death, retrying")
		s.recordPipelineEvent(chatID, threadID, observability.NewWarnEvent("",
			observability.PhaseRetryStarted, "process death recovery, retrying"))
	}

	if s.bridgeFailures.inCooldown() {
		remaining := s.bridgeFailures.cooldownRemaining()
		log.Printf("bridge: in cooldown, skipping retry for chat=%d", chatID)
		_ = s.output.SendError(chatID, threadID, bridgeCooldownMessage(remaining))
		s.output.ConfirmMessage(chatID, messageID)
		return
	}

	reconnectMsg, _ := s.output.SendText(chatID, threadID, "⚡ Reconectando...")

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
		_ = s.output.SendError(chatID, threadID, bridgeRetryFailedMessage)
		s.output.ConfirmMessage(chatID, messageID)
		return
	}

	if ch != nil {
		ch = idleTimeoutWrapper(ctx, ch, idleBridgeTimeout, cancel, func() {
			timeoutTracker.mark(timeoutOriginIdleBridge)
		})
	}

	toolUseSignal := make(chan struct{}, 16)
	go heartbeatMonitor(ctx.Done(), toolUseSignal, chatID, threadID, s.output)
	outcome = s.ProcessBridgeEvents(chatID, threadID, messageID, ch, progress, userText, toolUseSignal, userID)
	if handled := s.handleContextOutcome(parentCtx, ctx, chatID, threadID, userID, timeoutTracker); handled {
		s.output.ConfirmMessage(chatID, messageID)
		return
	}
	s.handleRetryOutcome(chatID, threadID, messageID, outcome)
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
			s.sessions.DeactivateSession(chatID, threadID, userID)
		}
		_ = s.output.SendError(chatID, threadID, bridgeTimeoutMessage)
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

func (s *Service) handleRetryOutcome(chatID int64, threadID int, messageID int, outcome Outcome) {
	switch outcome {
	case OutcomeSuccess:
		s.bridgeFailures.reset()
	case OutcomeProcessDeath:
		s.bridgeFailures.record()
		s.patchContinuitySessionCold(chatID, threadID, "bridge retry process death")
		_ = s.output.SendError(chatID, threadID, bridgeRetryFailedMessage)
		s.output.ConfirmMessage(chatID, messageID)
	}
}

// heartbeatMonitor sends a "still thinking" update when no tool_use event
// arrives within heartbeatThreshold. It resets on each tool_use event so the
// user only sees the message when the model is thinking without tools.
// Stopped by doneCh (e.g., ctx.Done()).
func heartbeatMonitor(doneCh <-chan struct{}, toolUseSignal <-chan struct{}, chatID int64, threadID int, output Output) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("pipeline: panic in heartbeatMonitor: %v", r)
		}
	}()

	lastTool := time.Now()
	beatSent := false
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
				msg := fmt.Sprintf("⏱️ %s — processando sem ferramentas ativas no momento", elapsed)
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
func (s *Service) ProcessBridgeEvents(chatID int64, threadID int, messageID int, ch <-chan bridge.Event, progress ProgressReporter, userText string, toolUseSignal chan<- struct{}, userID int64) Outcome {
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
				lastStreamFlush = time.Now()
			}
		case "result":
			return s.handleResultEvent(chatID, threadID, messageID, ev, &assistantText, userText, userID)
		case "error":
			return s.handleErrorEvent(chatID, threadID, messageID, ev, userID)
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

		// Deactivate session so next turn does not Continue into a suspect session
		if s.sessions != nil {
			s.sessions.DeactivateSession(chatID, threadID, userID)
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
	if !s.tryExecutePlan(chatID, threadID, messageID, finalText, userID) {
		return false, OutcomeSuccess
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

func (s *Service) tryExecutePlan(chatID int64, threadID int, messageID int, finalText string, userID int64) bool {
	if s.orchestrator == nil {
		return false
	}
	plan, err := s.orchestrator.ExtractPlan(finalText)
	if err != nil {
		if orchestrator.ContainsPlanMarker(finalText) {
			log.Printf("Execution plan marker detected but plan was invalid: %v", err)
			_ = s.output.SendError(chatID, threadID, "Plano de execução gerado, mas não consegui interpretar o JSON. Não vou enviar os prompts internos no chat.")
			return true
		}
		return false
	}
	if plan == nil {
		if orchestrator.ContainsPlanMarker(finalText) {
			log.Printf("Execution plan marker detected but plan block was incomplete")
			_ = s.output.SendError(chatID, threadID, "Plano de execução gerado, mas o bloco veio incompleto. Não vou enviar os prompts internos no chat.")
			return true
		}
		return false
	}
	log.Printf("Execution plan detected with %d tasks", len(plan.Tasks))
	if displayText := orchestrator.StripPlanBlock(finalText); displayText != "" {
		_ = s.output.SendReply(chatID, threadID, displayText)
	}

	// Resolve effective working directory — refuse execution without one
	cwd := s.effectiveCwd(nil, chatID, threadID)
	if cwd == "" {
		log.Printf("orchestration: refusing plan execution for chat=%d thread=%d: no cwd bound", chatID, threadID)
		_ = s.output.SendError(chatID, threadID, "Não encontrei um diretório de trabalho (cwd) para executar o plano. Use /cwd para fixar um projeto e tente novamente.")
		return true
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline: panic in ExecuteApprovedPlan: %v", r)
			}
		}()
		s.output.ExecuteApprovedPlan(chatID, threadID, messageID, cwd, userID, plan)
	}()
	return true
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

// summaryCounter tracks the number of successful turns since the last
// LLM-generated summary for a conversation. Stored in-memory only;
// on daemon restart it resets to 0, triggering a fresh summary on the
// next turn from existing continuity state.
type summaryCounter struct {
	mu     sync.Mutex
	counts map[continuity.ConversationKey]int
}

// increment increments the turn counter and returns the new count and whether
// we should generate a summary (turns >= interval).
func (c *summaryCounter) increment(key continuity.ConversationKey, interval int) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[key]++
	turns := c.counts[key]
	return turns, turns >= interval
}

// reset resets the turn counter for a conversation after summarization.
func (c *summaryCounter) reset(key continuity.ConversationKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.counts, key)
}

// runeCap returns the first n runes of s, preserving valid UTF-8.
func runeCap(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	var count int
	for i := range s {
		if count >= n {
			return s[:i]
		}
		count++
	}
	return s
}

// generateProgressiveSummary calls the LLM to merge the previous summary
// with the latest exchange. Returns an updated summary string, or empty
// string if summarization failed (caller falls back to raw text).
func (s *Service) generateProgressiveSummary(ctx context.Context, previousSummary, userText, assistantText string) string {
	if s.bridge == nil || s.config == nil {
		return ""
	}

	// Cap inputs to keep prompt tokens manageable
	cappedPrev := runeCap(previousSummary, 2000)
	cappedUser := runeCap(userText, 2000)
	cappedAssistant := runeCap(assistantText, 2000)

	prompt := fmt.Sprintf(`Merge the previous summary with the latest user message and assistant response into ONE updated summary that captures all important context, decisions, and open items.

Previous summary: %s
Latest user message: %s
Latest assistant response: %s

Updated summary (max 900 chars, no preamble):`,
		cappedPrev, cappedUser, cappedAssistant)

	sumCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	options := bridge.RequestOptions{
		SystemPrompt: "You are a conversation summarizer. Output ONLY the requested summary, no preamble, no explanation. Maximum 900 characters, in the same language as the conversation (Portuguese).",
	}
	s.applyConfiguredModelOptions(&options)

	result, err := s.bridge.ExecuteSync(sumCtx, bridge.Request{
		Command: "query",
		Prompt:  prompt,
		Options: options,
	})

	if err != nil {
		log.Printf("summary: failed to generate progressive summary: %v", err)
		return ""
	}

	// Redact BEFORE truncation (per redaction-before-truncation.md) so
	// secrets straddling the boundary aren't sliced in half.
	summary := redactSecrets(strings.TrimSpace(result.Content))
	return runeCap(summary, continuity.MaxAssistantSummary)
}

func (s *Service) afterSuccessfulTurn(chatID int64, threadID int, userText string, finalText string, runID string, userID int64) {
	key := continuity.ConversationKey{ChatID: chatID, ThreadID: threadID}

	// Progressive summarization: on non-summary turns, re-read the existing
	// summary from continuity so LastAssistantSummary accumulates across
	// intervals instead of being overwritten by the latest raw text.
	finalSummary := finalText
	if s.summaryInterval > 0 {
		turns, shouldSummarize := s.summaryCounter.increment(key, s.summaryInterval)
		if shouldSummarize {
			log.Printf("summary: generating progressive summary for chat=%d thread=%d after %d turns", chatID, threadID, turns)

			if s.continuity != nil {
				readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer readCancel()

				state, err := s.continuity.Get(readCtx, chatID, threadID)
				if err == nil && state != nil && state.LastAssistantSummary != "" {
					// Use background context (not readCtx) so generateProgressiveSummary
					// can derive its own 10s timeout — a 5s parent would truncate it.
					merged := s.generateProgressiveSummary(context.Background(), state.LastAssistantSummary, userText, finalText)
					if merged != "" {
						finalSummary = merged
						s.summaryCounter.reset(key)
						log.Printf("summary: progressive summary generated (%d chars) for chat=%d thread=%d", len(merged), chatID, threadID)
					}
				}
			}
		} else if s.continuity != nil {
			// Preserve accumulated summary across non-summary turns
			// so subsequent intervals build on the previous summary.
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			state, err := s.continuity.Get(ctx, chatID, threadID)
			if err == nil && state != nil && state.LastAssistantSummary != "" {
				finalSummary = state.LastAssistantSummary
			}
		}
	}

	// Patch continuity with the (potentially summarized) assistant text
	s.patchContinuityAfterSuccess(chatID, threadID, userText, finalSummary, runID, userID)

	if s.dreamer == nil {
		return
	}
	s.dreamer.AfterTurn(userID)
	cwd := s.effectiveCwd(nil, chatID, threadID)
	s.nudgeBuffer.AddTurn(chatID, threadID, userID, userText, finalText)
	s.dreamer.AfterTurnNudge(chatID, threadID, userID, cwd, s.nudgeBuffer)
	s.InvalidateMemoryDirs(chatID, threadID, userID, cwd)
}

// --- Continuity lifecycle helpers ---

// getRunID returns the current runID from runLogStates, or empty string.
// Must be called before completeRunLog, which deletes the state.
func (s *Service) getRunID(chatID int64, threadID int) string {
	key := runLogKey(chatID, threadID)
	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()
	if ok && state != nil {
		return state.runID
	}
	return ""
}

// patchContinuityAfterSuccess writes successful turn state into the continuity store.
// runID must be captured before completeRunLog (which cleans up runLogStates).
func (s *Service) patchContinuityAfterSuccess(chatID int64, threadID int, userText string, assistantText string, runID string, userID ...int64) {
	if s.continuity == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cwd := s.effectiveCwd(nil, chatID, threadID)
	now := time.Now()
	runStatus := "completed"
	sessionCold := false

	sessionID := ""
	if s.sessions != nil {
		sessionID = s.sessions.GetSession(chatID, threadID, sessionUserID(userID...))
	}

	err := s.continuity.Patch(ctx, continuity.ConversationKey{ChatID: chatID, ThreadID: threadID}, continuity.StatePatch{
		CWD:                  &cwd,
		LastUserIntent:       &userText,
		LastAssistantSummary: &assistantText,
		LastRunID:            &runID,
		LastRunStatus:        &runStatus,
		SessionID:            &sessionID,
		SessionCold:          &sessionCold,
		UpdatedAt:            now,
	})
	if err != nil {
		log.Printf("continuity: failed to patch after success chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// patchContinuityFailure writes failure/timeout/error state into the continuity store.
// Must be called BEFORE completeRunLog, since that cleans up the run log state.
func (s *Service) patchContinuityFailure(chatID int64, threadID int, status string, errMsg string, userID ...int64) {
	if s.continuity == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	now := time.Now()
	sessionCold := true

	// Capture latest checkpoint and tools from the runLogState
	checkpoint := ""
	tools := ""
	runID := ""
	key := runLogKey(chatID, threadID)
	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	if ok && state != nil {
		runID = state.runID
		state.mu.Lock()
		tools = state.summary.String()
		state.mu.Unlock()
	}
	s.runLogMu.Unlock()

	if tools != "" {
		tools = redactSecrets(tools)
	}

	// Build checkpoint from available info
	cp := buildCheckpoint(runlog.RunStatus(status), "", tools, errMsg)
	checkpoint = redactSecrets(cp)

	cwd := s.effectiveCwd(nil, chatID, threadID)

	sid := ""
	if s.sessions != nil {
		sid = s.sessions.GetSession(chatID, threadID, sessionUserID(userID...))
	}

	err := s.continuity.Patch(ctx, continuity.ConversationKey{ChatID: chatID, ThreadID: threadID}, continuity.StatePatch{
		CWD:            &cwd,
		LastRunID:      &runID,
		LastRunStatus:  &status,
		LastCheckpoint: &checkpoint,
		LastTools:      &tools,
		SessionID:      &sid,
		SessionCold:    &sessionCold,
		ResetReason:    &errMsg,
		UpdatedAt:      now,
	})
	if err != nil {
		log.Printf("continuity: failed to patch failure chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// patchContinuitySessionCold marks the session as cold with a reset reason.
func (s *Service) patchContinuitySessionCold(chatID int64, threadID int, reason string) {
	if s.continuity == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cold := true
	err := s.continuity.Patch(ctx, continuity.ConversationKey{ChatID: chatID, ThreadID: threadID}, continuity.StatePatch{
		SessionCold: &cold,
		ResetReason: &reason,
		UpdatedAt:   time.Now(),
	})
	if err != nil {
		log.Printf("continuity: failed to patch session cold chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// patchContinuitySessionID updates the session ID in continuity state.
func (s *Service) patchContinuitySessionID(chatID int64, threadID int, sessionID string) {
	if s.continuity == nil || sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := s.continuity.Patch(ctx, continuity.ConversationKey{ChatID: chatID, ThreadID: threadID}, continuity.StatePatch{
		SessionID: &sessionID,
		UpdatedAt: time.Now(),
	})
	if err != nil {
		log.Printf("continuity: failed to patch session ID chat=%d thread=%d: %v", chatID, threadID, err)
	}
}

// continuitySnapshot captures the current continuity state for fallback recovery.
// Returns a compact, redacted summary string, or empty if unavailable.
// All field values are redacted for defense-in-depth, escaped to prevent
// delimiter injection, and the total is capped at MaxContinuityBlockChars.
func (s *Service) continuitySnapshot(ctx context.Context, chatID int64, threadID int) string {
	if s.continuity == nil {
		return ""
	}
	getCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	state, err := s.continuity.Get(getCtx, chatID, threadID)
	if err != nil || state == nil {
		return ""
	}

	var parts []string
	if state.LastUserIntent != "" {
		parts = append(parts, "Last user intent: "+redactSecrets(state.LastUserIntent))
	}
	if state.LastAssistantSummary != "" {
		parts = append(parts, "Last assistant summary: "+redactSecrets(state.LastAssistantSummary))
	}
	if state.LastRunStatus != "" {
		parts = append(parts, "Last run status: "+redactSecrets(state.LastRunStatus))
	}
	if state.LastTools != "" {
		parts = append(parts, "Tools used: "+redactSecrets(state.LastTools))
	}
	if state.CWD != "" {
		parts = append(parts, "Working directory: "+redactSecrets(state.CWD))
	}

	if len(parts) == 0 {
		return ""
	}

	body := strings.Join(parts, "\n")

	// Cap the total block size (rune-aware to avoid splitting multi-byte chars).
	if utf8.RuneCountInString(body) > continuity.MaxContinuityBlockChars {
		for utf8.RuneCountInString(body) > continuity.MaxContinuityBlockChars {
			body = body[:len(body)-1]
		}
		// Walk back to valid rune boundary.
		for len(body) > 0 && body[len(body)-1]&0xC0 == 0x80 {
			body = body[:len(body)-1]
		}
	}

	// Escape delimiter-sensitive characters to prevent injection of
	// closing </fallback_context_untrusted> tags.
	body = continuity.EscapeUntrusted(body)

	return body
}

func classifyBridgeErrorOutcome(message string) (string, runlog.RunStatus, string) {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "query timeout") {
		return "timed_out", runlog.RunTimedOut, timeoutOriginBridgeQuery
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") || strings.Contains(lower, "deadline exceeded") {
		return "timed_out", runlog.RunTimedOut, timeoutOriginProviderPI
	}
	return "failed", runlog.RunFailed, message
}

func (s *Service) handleErrorEvent(chatID int64, threadID int, messageID int, ev bridge.Event, userID ...int64) Outcome {
	errMsg := ev.Message
	if errMsg == "" {
		errMsg = ev.Content
	}
	if errMsg == "" {
		errMsg = "Erro desconhecido no processador."
	}
	redacted := redactSecrets(errMsg)
	log.Printf("Bridge error: %s", redacted)
	status, runStatus, reason := classifyBridgeErrorOutcome(redacted)
	s.patchContinuityFailure(chatID, threadID, status, reason, sessionUserID(userID...))
	s.completeRunLog(chatID, threadID, runStatus, "", reason)
	s.recordPipelineEvent(chatID, threadID, observability.NewErrorEvent("",
		observability.PhaseRunFailed, reason))
	if err := s.output.SendError(chatID, threadID, redacted); err != nil {
		log.Printf("Failed to send error to chat %d: %v", chatID, err)
	}
	s.output.ConfirmMessage(chatID, messageID)
	return OutcomeLLMError
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

	if err := s.runLog.Update(context.Background(), runlog.RunUpdate{
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
		if err := s.runLog.Update(context.Background(), runlog.RunUpdate{
			RunID:       state.runID,
			ToolSummary: &toolSummary,
		}); err != nil {
			log.Printf("runlog: failed to persist tool summary for %s: %v", state.runID, err)
		}
	}
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

	// Capture final tool summary under the per-state lock to serialize
	// with concurrent recordToolUse / recordToolResult mutations.
	state.mu.Lock()
	summary := state.summary.String()
	state.mu.Unlock()

	// Defensive redaction: assistant output may contain credentials.
	summary = redactSecrets(summary)
	checkpoint = redactSecrets(checkpoint)
	errMsg = redactSecrets(errMsg)

	// Build checkpoint
	if checkpoint == "" {
		checkpoint = buildCheckpoint(status, "", summary, errMsg)
	} else {
		checkpoint = buildCheckpoint(status, checkpoint, summary, errMsg)
	}

	if err := s.runLog.Complete(context.Background(), state.runID, status, checkpoint, errMsg); err != nil {
		log.Printf("runlog: failed to complete %s (status=%s): %v", state.runID, status, err)
	}

	// Flush session update with final summary
	if summary != "" {
		if err := s.runLog.Update(context.Background(), runlog.RunUpdate{
			RunID:       state.runID,
			ToolSummary: &summary,
		}); err != nil {
			log.Printf("runlog: failed to update summary for %s: %v", state.runID, err)
		}
	}
}

// buildCheckpoint formats a textual checkpoint from run status and context.
func buildCheckpoint(status runlog.RunStatus, checkpoint, toolSummary, errMsg string) string {
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

// summarizeToolResult produces a truncated, redacted summary of a tool result.
func summarizeToolResult(content string) string {
	if content == "" {
		return ""
	}
	// Redact common secret patterns (API keys, tokens, etc.)
	redacted := redactSecrets(content)
	// Take first 1KB
	truncated := strings.TrimSpace(redacted)
	if len(truncated) > 1024 {
		truncated = truncated[:1024]
	}
	return truncated
}

// Pre-compiled redaction regexes to avoid re-parsing on every call.
var (
	prefixREs    []*regexp.Regexp
	prefixLabels []string
	privateKeyRE *regexp.Regexp
	authRE       *regexp.Regexp
	lineRE       *regexp.Regexp
	jsonSecretRE *regexp.Regexp
)

func init() {
	type pattern struct {
		repl  string
		label string
	}
	prefixPatterns := []pattern{
		// API keys
		{`\bsk-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bpk-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk-ant-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk-proj-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk_live_[A-Za-z0-9]+`, "[STRIPE_KEY_REDACTED]"},
		{`\bsk_test_[A-Za-z0-9]+`, "[STRIPE_KEY_REDACTED]"},
		// Cloud provider keys
		{`\bAKIA[A-Z0-9]{16}`, "[AWS_KEY_REDACTED]"},
		{`\bAIza[0-9A-Za-z_-]{35}`, "[GCP_KEY_REDACTED]"},
		// GitHub tokens
		{`\bghp_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bgho_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghu_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghs_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghr_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bgithub_pat_[0-9A-Za-z_-]+`, "[GH_PAT_REDACTED]"},
		// Other tokens
		{`\bglpat-[A-Za-z0-9_-]{20,}`, "[GL_TOKEN_REDACTED]"},
		{`\bhf_[A-Za-z0-9]{20,}`, "[HF_TOKEN_REDACTED]"},
		{`\bnpm_[A-Za-z0-9]{36}`, "[NPM_TOKEN_REDACTED]"},
		{`\bxox[bpasa]-[A-Za-z0-9-]{20,}`, "[SLACK_TOKEN_REDACTED]"},
		{`\bxapp-[A-Za-z0-9-]{20,}`, "[SLACK_TOKEN_REDACTED]"},
		// Base64-encoded JSON (JWT-like)
		{`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+`, "[JWT_REDACTED]"},
		// AI provider keys
		{`\bxai-[A-Za-z0-9]{20,}`, "[XAI_KEY_REDACTED]"},
	}

	prefixREs = make([]*regexp.Regexp, len(prefixPatterns))
	prefixLabels = make([]string, len(prefixPatterns))
	for i, p := range prefixPatterns {
		prefixREs[i] = regexp.MustCompile(p.repl)
		prefixLabels[i] = p.label
	}

	privateKeyRE = regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----`)
	authRE = regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|Basic)\s+)\S+`)
	lineRE = regexp.MustCompile(`(password|secret|api_key|api-key|api\.key|apikey|clientsecret|client_secret|access_token|refresh_token|token)\s*[=:]\s*\S+`)
	jsonSecretRE = regexp.MustCompile(`"(?:apiKey|api_key|api-key|api\.key|clientSecret|client_secret|client-secret|client\.secret|accessToken|access_token|access-token|access\.token|refreshToken|refresh_token|refresh-token|refresh\.token|token)"\s*:\s*"[^"]{4,}"`)
}

// RedactSecrets is the public wrapper for redactSecrets, shared with other
// packages (e.g. dream nudge prompt) that need credential redaction.
func RedactSecrets(s string) string { return redactSecrets(s) }

// redactSecrets replaces common credential patterns with [REDACTED].
// All regexes are pre-compiled at init for performance.
func redactSecrets(s string) string {
	result := s
	for i, re := range prefixREs {
		result = re.ReplaceAllString(result, prefixLabels[i])
	}

	// Multi-line block redaction (must run before line splitting)
	result = privateKeyRE.ReplaceAllString(result, "[PRIVATE_KEY_BLOCK_REDACTED]")

	// Header-based auth: Authorization: Bearer xxx, Authorization: Basic xxx
	result = authRE.ReplaceAllString(result, "$1[REDACTED]")

	// Line-based redaction for structured data with known keys
	lines := strings.Split(result, "\n")
	var filtered []string
	for _, line := range lines {
		lower := strings.ToLower(line)
		// Match key=value, key:value, key = value, and key: value patterns
		// Patterns cover: password, secret, api_key, api-key, api.key, apikey,
		// clientsecret, access_token, refresh_token, and generic token.
		if lineRE.MatchString(lower) {
			filtered = append(filtered, "[CREDENTIAL_REDACTED]")
			continue
		}
		// JSON-style embedded secrets: "apiKey":"xxx", "clientSecret":"xxx"
		if jsonSecretRE.MatchString(line) {
			filtered = append(filtered, "[CREDENTIAL_REDACTED]")
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}
