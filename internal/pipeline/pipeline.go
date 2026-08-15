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

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/security"
	"github.com/igormaneschy/aurelia/pkg/idgen"
)

// ModelCataloger checks whether a given provider/model supports images
// through the PI model registry. Used by applyVisionFallback to determine
// whether the currently selected model can handle image input.
type ModelCataloger interface {
	// ModelSupportsImages performs an exact provider+model lookup.
	// Returns (supports, error). Error means the model was not found
	// or the catalog was unavailable.
	ModelSupportsImages(ctx context.Context, provider, model string) (bool, error)

	// ModelSupportsImagesByID looks up a model by ID alone (any provider).
	// Returns (supports, found, error) where found=false means zero or
	// multiple matches exist — caller must treat capability as unknown.
	ModelSupportsImagesByID(ctx context.Context, modelID string) (supports bool, found bool, err error)
}

// defaultModelsCacheTTL is how long the model catalog is cached to avoid
// redundant bridge ListModels calls per request.
const defaultModelsCacheTTL = 30 * time.Second

// bridgeModelCataloger adapts *bridge.Bridge to ModelCataloger using
// ListModels which returns ModelInfo with SupportsImages capability.
// Results are cached with a configurable TTL to avoid redundant bridge calls.
type bridgeModelCataloger struct {
	br       *bridge.Bridge
	mu       sync.Mutex
	cached   []bridge.ModelInfo
	cachedAt time.Time
	ttl      time.Duration
}

// getModels returns the model catalog, using a cached copy if still fresh.
func (b *bridgeModelCataloger) getModels(ctx context.Context) ([]bridge.ModelInfo, error) {
	b.mu.Lock()
	// Fast path: cache hit within TTL.
	if b.cached != nil && time.Since(b.cachedAt) < b.ttl {
		models := b.cached
		b.mu.Unlock()
		return models, nil
	}
	b.mu.Unlock()

	// Cache miss: fetch from bridge.
	models, err := b.br.ListModels(ctx, false)
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	b.cached = models
	b.cachedAt = time.Now()
	b.mu.Unlock()
	return models, nil
}

func (b *bridgeModelCataloger) ModelSupportsImages(ctx context.Context, provider, model string) (bool, error) {
	models, err := b.getModels(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range models {
		if m.Provider == provider && m.ID == model {
			return m.SupportsImages, nil
		}
	}
	return false, fmt.Errorf("model %s/%s not found in PI model catalog", provider, model)
}

// ModelSupportsImagesByID searches the catalog for a model by ID alone.
// Returns (_, found=false) when zero or multiple providers offer this model.
func (b *bridgeModelCataloger) ModelSupportsImagesByID(ctx context.Context, modelID string) (supports bool, found bool, err error) {
	models, err2 := b.getModels(ctx)
	if err2 != nil {
		return false, false, err2
	}
	var match *bridge.ModelInfo
	for _, m := range models {
		if m.ID == modelID {
			if match != nil {
				return false, false, nil // ambiguous — multiple providers
			}
			cp := m
			match = &cp
		}
	}
	if match == nil {
		return false, false, nil // not found
	}
	return match.SupportsImages, true, nil
}

// runLogState tracks per-run state for the run journal.
// mu serializes summary mutations independently of the runLogState map lock,
// preventing data races between recordToolUse/recordToolResult and completeRunLog.
// activeRun is stored in activeSessions to carry both a cancel function
// and an ownership token. Cancel/supersede paths extract the cancel func
// via extractCancelFn; the token is used for ownership-safe cleanup.
type activeRun struct {
	token  any
	cancel context.CancelFunc
	// mu serializes supersession with durable post-terminal mutations. A stale
	// run must not pass an ownership check and then write after its token has
	// been replaced by a newer run.
	mu          sync.RWMutex
	superseded  bool
	finalized   bool
	finalizer   *terminalFinalization
	runLogState *runLogState
}

func newActiveRun() *activeRun {
	return &activeRun{token: &activeRun{}}
}

// activeRunSlotMu serializes active-session replacement with the ownership
// check immediately adjacent to a durable mutation. sync.Map alone cannot
// make "is this still the current owner?" plus a subsequent store atomic.
var activeRunSlotMu sync.Mutex

func markRunSuperseded(run *activeRun) {
	if run == nil {
		return
	}
	run.mu.Lock()
	run.superseded = true
	run.mu.Unlock()
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
	chatID        int64
	threadID      int
	messageID     int
	userID        int64
	text          string
	images        []bridge.ImageAttachment
	isPrivateChat bool
}

const (
	bridgeExecutionTimeout = 30 * time.Minute // hard safety net, not configurable
	defaultIdleTimeout     = 15 * time.Minute // fallback when config not available

	// Tool call explosion thresholds — defaults. Per-agent overrides use
	// Agent.ToolBudget (warning = budget, critical = budget * 2.5).
	toolCallWarningThreshold  = 20
	toolCallCriticalThreshold = 50

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
func (s *Service) Process(chatID int64, threadID int, messageID int, text string, images []bridge.ImageAttachment, userID int64, isPrivateChat bool) error {
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
	input := pipelineInput{chatID: chatID, threadID: threadID, messageID: messageID, userID: userID, text: text, images: images, isPrivateChat: isPrivateChat}

	// Create a real cancelable context BEFORE atomic reservation so cancel/supersede
	// arriving during the reservation window actually cancel the run, not a no-op sentinel.
	// Store an *activeRun that carries both the cancel func and an ownership token.
	// All cancel/supersede paths extract the cancel func from *activeRun.
	run := newActiveRun()
	reservedCtx, reservedCancel := context.WithCancel(context.Background())
	run.cancel = reservedCancel

	activeRunSlotMu.Lock()
	_, loaded := s.activeSessions.LoadOrStore(key, run)
	activeRunSlotMu.Unlock()
	if !loaded {
		// We reserved the slot — start the run.
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("pipeline: panic in processRunWithCancel: %v", r)
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
		activeRunSlotMu.Lock()
		if val, loaded := s.activeSessions.LoadAndDelete(key); loaded {
			if oldRun, ok := val.(*activeRun); ok {
				markRunSuperseded(oldRun)
			}
			if cancelFn := extractCancelFn(val); cancelFn != nil {
				cancelFn()
			}
		}
		activeRunSlotMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
		defer cancel()
		_, err := s.bridge.ExecuteSync(ctx, bridge.Request{
			Command: "abort",
			Options: bridge.RequestOptions{
				ChatID:   chatID,
				ThreadID: threadID,
				UserID:   userID,
			},
		})
		if err != nil {
			log.Printf("pipeline: abort failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		if _, err := s.output.SendText(chatID, threadID, "🛑 Interrompendo o pedido anterior."); err != nil {
			log.Printf("pipeline: SendText(cancel) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		s.output.ConfirmMessage(chatID, messageID)

	case concurrentSupersede:
		// Stop the old goroutine; the superseding message starts fresh. Reserve
		// the replacement slot before the steer request so Cancel/CancelAll cannot
		// observe an empty slot and then lose the cancellation race.
		activeRunSlotMu.Lock()
		if val, loaded := s.activeSessions.LoadAndDelete(key); loaded {
			if oldRun, ok := val.(*activeRun); ok {
				markRunSuperseded(oldRun)
			}
			if cancelFn := extractCancelFn(val); cancelFn != nil {
				cancelFn()
			}
		}
		supersedeRun := newActiveRun()
		supersedeCtx, supersedeCancel := context.WithCancel(context.Background())
		supersedeRun.cancel = supersedeCancel
		if _, loaded := s.activeSessions.LoadOrStore(key, supersedeRun); loaded {
			activeRunSlotMu.Unlock()
			supersedeCancel()
			log.Printf("pipeline: supersede raced with another run for key=%s", key)
			if _, err := s.output.SendText(chatID, threadID, "⚠️ Outra solicitação já está em andamento. Tente novamente."); err != nil {
				log.Printf("pipeline: SendText(supersede race) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
			}
			s.output.ConfirmMessage(chatID, messageID)
			break
		}
		activeRunSlotMu.Unlock()

		steerCtx, steerCancel := context.WithTimeout(supersedeCtx, bridgeCommandTimeout)
		_, err := s.bridge.ExecuteSync(steerCtx, bridge.Request{
			Command: "steer",
			Prompt:  text,
			Options: bridge.RequestOptions{
				ChatID:   chatID,
				ThreadID: threadID,
				UserID:   userID,
			},
		})
		steerCancel()
		if err != nil {
			log.Printf("pipeline: steer failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		if !s.activeRunStillOwned(chatID, threadID, userID, supersedeRun) {
			s.output.ConfirmMessage(chatID, messageID)
			break
		}
		if _, err := s.output.SendText(chatID, threadID, "🔁 Interrompi o pedido anterior e vou seguir com sua correção."); err != nil {
			log.Printf("pipeline: SendText(supersede) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		s.output.ConfirmMessage(chatID, messageID)
		// Start a new goroutine to process the steered session.
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("pipeline: panic in processRunWithCancel(supersede): %v", r)
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
			Options: bridge.RequestOptions{
				ChatID:   chatID,
				ThreadID: threadID,
				UserID:   userID,
			},
		})
		if err != nil {
			log.Printf("pipeline: follow-up failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		if _, err := s.output.SendText(chatID, threadID, "📥 Adicionado à fila. Processo após concluir o atual."); err != nil {
			log.Printf("pipeline: SendText(follow-up) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
		s.output.ConfirmMessage(chatID, messageID)

	case concurrentStatus:
		ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
		defer cancel()
		ev, err := s.bridge.ExecuteSync(ctx, bridge.Request{
			Command: "get-state",
			Options: bridge.RequestOptions{
				ChatID:   chatID,
				ThreadID: threadID,
				UserID:   userID,
			},
		})
		if err == nil && ev != nil {
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
					log.Printf("pipeline: SendText(status) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
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
// userActiveMode returns the normalized active mode from the user's profile.
// Returns "" for general/default mode or when the store is unavailable.
func (s *Service) userActiveMode(userID int64) string {
	if s.usersStore != nil {
		if profile, err := s.usersStore.Get(userID); err == nil && profile != nil {
			return profile.ActiveMode
		}
	}
	return ""
}

func (s *Service) isOwnerUser(userID int64) bool {
	if userID <= 0 {
		return false
	}
	if s.config != nil && userID == s.config.DefaultOwnerUserIDOrFallback() {
		return true
	}
	if s.usersStore != nil {
		profile, err := s.usersStore.Get(userID)
		return err == nil && profile != nil && profile.IsOwner
	}
	return false
}

// resolveEffectiveProfile resolves the Prompt Profile for a message turn.
// Uses profiles.Resolver for @name > active mode > general precedence.
// Returns the resolved profile, stripped user text, and any error.
func (s *Service) resolveEffectiveProfile(text string, activeDefault string, userID int64, isOwner bool) (*profiles.PromptProfile, string, error) {
	if s.profiles != nil {
		return s.profiles.ResolveEffectiveForUser(text, activeDefault, userID, isOwner)
	}
	// Resolver is always wired in production; fall back to general builtin only.
	return profiles.GeneralBuiltin(), text, nil
}

func (s *Service) applyVisionFallback(ctx context.Context, req *bridge.Request, images []bridge.ImageAttachment) {
	if len(images) == 0 || s.config == nil {
		return
	}

	provider := req.Options.Provider
	model := req.Options.Model

	// ── Resolve the effective provider for catalog lookup ──

	// Case A: Provider is empty but Model is provider-qualified (e.g. "openai/gpt-4").
	// Profile overrides set req.Options.Model without setting req.Options.Provider,
	// so the model string may carry an explicit provider prefix.
	var parsedProvider bool
	if provider == "" && model != "" && strings.Contains(model, "/") {
		if parts := strings.SplitN(model, "/", 2); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			provider = parts[0]
			model = parts[1]
			parsedProvider = true
			slog.Debug("vision: parsed provider-qualified model for catalog lookup",
				"original", req.Options.Model, "parsed_provider", provider, "parsed_model", model)
		}
	}

	// ── Check model capability via the catalog ──

	if provider != "" && model != "" && s.modelCataloger != nil {
		// Case B: Exact provider+model lookup (explicit provider or parsed from qualified model).
		checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
		supports, err := s.modelCataloger.ModelSupportsImages(checkCtx, provider, model)
		checkCancel()
		if err == nil {
			if supports {
				slog.Debug("vision: current model supports images, keeping model",
					"provider", provider, "model", model)
				if parsedProvider {
					// Normalize the request: the profile set Model="provider/model"
					// without setting Provider. The bridge expects them as separate fields.
					req.Options.Provider = provider
					req.Options.Model = model
				}
				return
			}
			slog.Info("vision: current model does not support images, checking fallback",
				"provider", provider, "model", model)
		} else {
			slog.Warn("vision: model capability lookup failed, using fallback if configured",
				"provider", provider, "model", model, "error", err)
		}
	} else if provider == "" && model != "" && s.modelCataloger != nil {
		// Case C: Unqualified model (no provider). Search by model ID across the catalog.
		// Only accept the result when exactly one provider offers this model ID.
		checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
		supports, found, err := s.modelCataloger.ModelSupportsImagesByID(checkCtx, model)
		checkCancel()
		if err == nil && found {
			if supports {
				slog.Debug("vision: unqualified model supports images (single catalog match), keeping model",
					"model", model)
				return
			}
			slog.Info("vision: unqualified model does not support images, checking fallback",
				"model", model)
		} else {
			slog.Warn("vision: model capability lookup by ID failed, zero matches, or ambiguous, using fallback if configured",
				"model", model, "found", found, "error", err)
		}
	} else {
		slog.Debug("vision: provider/model unknown or no cataloger, applying fallback if configured",
			"provider", provider, "model", model, "cataloger", s.modelCataloger != nil)
	}

	// ── Apply configured vision fallback when available ──
	if vModel, vProvider := s.config.VisionFallback(); vModel != "" {
		slog.Info("vision: switching to fallback model",
			"fallback_provider", vProvider, "fallback_model", vModel)
		req.Options.Model = vModel
		if vProvider != "" {
			req.Options.Provider = vProvider
		}
		return
	}
	slog.Info("vision: no fallback configured, using current model for image input")
}

// buildBridgeRequest assembles a bridge.Request with agent overrides, session
// resume, and working directory.
func (s *Service) buildBridgeRequest(userText, systemPrompt string, pp *profiles.PromptProfile, chatID int64, threadID int, userID int64, isPrivateChat bool) bridge.Request {
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

	if pp != nil {
		if pp.Model != "" {
			req.Options.Model = pp.Model
		}
		if pp.Cwd != "" {
			req.Options.Cwd = pp.Cwd
		}
		if len(pp.AllowedTools) > 0 {
			req.Options.AllowedTools = pp.AllowedTools
		}
		if len(pp.DisallowedTools) > 0 {
			req.Options.DisallowedTools = pp.DisallowedTools
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

	cwd := s.effectiveCwdForContext(pp, chatID, threadID, userID, isPrivateChat)
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
	capProfile := security.DefaultProfileForContext(cwd != "", false, needsWriteTools(pp))

	// Allow agent-level capability_profile override
	if pp != nil && pp.CapabilityProfile != "" {
		capProfile = security.CapabilityProfile(pp.CapabilityProfile)
	}

	secCfg := s.getSecurityConfig()
	agentName := ""
	if pp != nil {
		agentName = pp.Name
	}

	_, effectiveTools, secCtx := bridge.BuildSecurityContext(
		capProfile,
		req.Options.AllowedTools,
		req.Options.DisallowedTools,
		cwd != "",
		&secCfg,
		cwd,
		chatID,
		threadID,
		userID,
		agentName,
		req.RequestID,
	)
	req.Options.AllowedTools = effectiveTools
	req.Options.Security = secCtx

	return req
}

func (s *Service) applyConfiguredModelOptions(opts *bridge.RequestOptions) {
	if s == nil || s.config == nil || s.config.IsModelAuto() || opts == nil {
		return
	}
	if s.config.DefaultProvider != "" {
		opts.Provider = s.config.DefaultProvider
	}
	if s.config.DefaultModel != "" {
		opts.Model = s.config.DefaultModel
	}
}

// needsWriteTools returns true if the profile requires write-capable tools.
func needsWriteTools(pp *profiles.PromptProfile) bool {
	if pp == nil {
		return true // default to write-capable
	}
	for _, t := range pp.AllowedTools {
		if t == "Write" || t == "Edit" || t == "Bash" {
			return true
		}
	}
	switch pp.CapabilityProfile {
	case "edit_project", "execute_safe", "privileged":
		return true
	case "observe", "read_only":
		return false
	}
	return !pp.IsReadOnly()
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
// warningThreshold and criticalThreshold override the global tool call limits
// when > 0 (set by agent tool_budget). Use 0 for defaults.
func (s *Service) cancelBridgeOnContextDone(ctx context.Context, requestID string) func() {
	if s == nil || s.bridge == nil || ctx == nil || requestID == "" {
		return func() {}
	}
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
				log.Printf("bridge: cancel request %s failed: %s", requestID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
			}
		case <-done:
		}
	}()
	return func() { close(done) }
}

func (s *Service) handleContextOutcome(parentCtx context.Context, ctx context.Context, chatID int64, threadID int, userID int64, isPrivateChat bool, tracker *runTimeoutTracker, owners ...runOwnership) bool {
	if parentCtx.Err() != nil {
		claimedOwnership, claimed := s.claimRunFinalization(chatID, threadID, userID, owners...)
		if !claimed {
			return true
		}
		ownership := claimedOwnership
		if !s.withCurrentRunOwnership(chatID, threadID, userID, []runOwnership{ownership}, func() {}) {
			s.cancelClaimedRunLog(pipelineInput{chatID: chatID, threadID: threadID, userID: userID}, ownership)
			return true
		}
		log.Printf("pipeline: run canceled chat=%d thread=%d user=%d", chatID, threadID, userID)
		runID := s.getRunID(chatID, threadID, userID, ownership)
		const reason = "user_cancel"
		s.patchContinuityFailure(chatID, threadID, "canceled", reason, userID, isPrivateChat, ownership)
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewEvent(runID,
			observability.PhaseRunCanceled, reason), ownership)
		s.completeRunLog(chatID, threadID, userID, runlog.RunCanceled, "", reason, ownership)
		return true
	}
	if ctx.Err() != nil {
		claimedOwnership, claimed := s.claimRunFinalization(chatID, threadID, userID, owners...)
		if !claimed {
			return true
		}
		ownership := claimedOwnership
		if !s.withCurrentRunOwnership(chatID, threadID, userID, []runOwnership{ownership}, func() {}) {
			s.cancelClaimedRunLog(pipelineInput{chatID: chatID, threadID: threadID, userID: userID}, ownership)
			return true
		}
		origin, elapsed := timeoutDetails(tracker)
		log.Printf("pipeline: run timeout origin=%s elapsed=%s chat=%d thread=%d user=%d", origin, elapsed.Round(time.Second), chatID, threadID, userID)
		runID := s.getRunID(chatID, threadID, userID, ownership)
		s.patchContinuityFailure(chatID, threadID, "timed_out", origin, userID, isPrivateChat, ownership)
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent(runID,
			observability.PhaseRunTimedOut,
			fmt.Sprintf("origin=%s elapsed=%s", origin, elapsed.Round(time.Second))), ownership)
		s.completeRunLog(chatID, threadID, userID, runlog.RunTimedOut, "", origin, ownership)
		if s.sessions != nil {
			s.withCurrentRunOwnership(chatID, threadID, userID, []runOwnership{ownership}, func() {
				s.sessions.MarkFailure(chatID, threadID, userID, origin)
			})
		}
		if s.activeRunStillOwned(chatID, threadID, userID, ownership.owner) {
			if err := s.output.SendError(chatID, threadID, buildTimeoutMessage(origin)); err != nil {
				log.Printf("pipeline: SendError(timeout) failed for chat=%d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
			}
		}
		return true
	}
	return false
}

// progressSilenceDetail formats a bounded silence duration for progress
// state detail. The duration is clamped by the caller; this only formats.
func progressSilenceDetail(silentMs int64) string {
	d := time.Duration(silentMs) * time.Millisecond
	return fmt.Sprintf("silêncio de %s", d.Round(time.Second))
}

// buildHeartbeatMessage formats the human detail for the waiting state.
// Per Long Flow UX v2: human progress language, no technical terms like
// "chamadas de ferramenta" or tool counts. Milestones escalate with elapsed
// time so long silences get progressively warmer copy.
func buildHeartbeatMessage(elapsed time.Duration, beatCount, toolCount int) string {
	if toolCount > 0 && beatCount%heartbeatToolThreshold == 0 {
		return fmt.Sprintf("Ainda estou trabalhando no pedido. Vou consolidar o progresso em breve (%s).", elapsed)
	}
	switch {
	case elapsed >= 3*time.Minute:
		return fmt.Sprintf("Continua processando — obrigado pela paciência (%s).", elapsed)
	case elapsed >= 1*time.Minute:
		return fmt.Sprintf("Ainda estou trabalhando no pedido (%s).", elapsed)
	default:
		return fmt.Sprintf("Ainda estou processando (%s).", elapsed)
	}
}

// heartbeatMonitor sends a "still thinking" state when no tool_use event
// arrives within threshold. It resets on each tool_use event so the user
// only sees the state when the model is thinking without tools. The state
// goes through the surface-neutral ProgressReporter; adapters decide how to
// render it (Telegram edits the receipt, TUI shows an indicator) instead of
// sending separate chat messages.
// Stopped by doneCh (e.g., ctx.Done()).
func heartbeatMonitor(doneCh <-chan struct{}, toolUseSignal <-chan struct{}, toolTracker *toolCallTracker, progress ProgressReporter) {
	heartbeatMonitorWithIntervals(doneCh, toolUseSignal, toolTracker, progress, heartbeatInterval, heartbeatThreshold)
}

// heartbeatMonitorWithIntervals is heartbeatMonitor with injectable tick
// interval/threshold so tests can drive the state machine quickly.
func heartbeatMonitorWithIntervals(doneCh <-chan struct{}, toolUseSignal <-chan struct{}, toolTracker *toolCallTracker, progress ProgressReporter, interval, threshold time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("pipeline: panic in heartbeatMonitor: %v", r)
		}
	}()

	if progress == nil {
		return
	}
	lastTool := time.Now()
	beatSent := false
	beatCount := 0
	var lastBeatAt time.Time
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-doneCh:
			return
		case <-toolUseSignal:
			lastTool = time.Now()
			beatSent = false
		case <-ticker.C:
			// Re-report on a refresh cadence (every 3 intervals) while the
			// model keeps thinking without tools: the embedded elapsed time
			// must stay fresh instead of freezing at the first beat.
			if time.Since(lastTool) >= threshold && (!beatSent || time.Since(lastBeatAt) >= interval*3) {
				elapsed := time.Since(lastTool).Round(time.Second)
				beatCount++
				toolCount := toolTracker.countLocked()
				progress.ReportState(ProgressStateWaiting, buildHeartbeatMessage(elapsed, beatCount, toolCount))
				beatSent = true
				lastBeatAt = time.Now()
			}
		}
	}
}

// ProcessBridgeEvents reads bridge events and sends responses to the output.
// toolUseSignal, if non-nil, receives a signal on every tool_use event so a
// caller can monitor thinking gaps (heartbeat).
// toolTracker, if non-nil, is used to count tool calls and warn on explosion.
func (s *Service) ProcessBridgeEvents(chatID int64, threadID int, messageID int, ch <-chan bridge.Event, progress ProgressReporter, userText string, toolUseSignal chan<- struct{}, userID int64, isPrivateChat bool, toolTracker *toolCallTracker, loopDetect *loopDetector, owners ...runOwnership) Outcome {
	ownership := firstRunOwnership(owners)
	var (
		assistantText       strings.Builder
		lastStreamFlush     = time.Now()
		streamFlushInterval = 3 * time.Second
	)

	for ev := range ch {
		if len(owners) > 0 && ownership.finalizer == nil && !s.activeRunStillOwned(chatID, threadID, userID, ownership.owner) {
			if progress != nil {
				progress.ReportState(ProgressStateCanceled, "")
			}
			return OutcomeCanceled
		}
		switch ev.Type {
		case "system":
			s.handleSystemEvent(chatID, threadID, ev, userID, ownership)
			if ev.SessionID != "" {
				s.updateRunLogSession(chatID, threadID, userID, ev.SessionID, ownership)
			}
			if ev.SessionFile != "" {
				s.updateRunLogSessionFile(chatID, threadID, userID, ev.SessionFile, ownership)
			}
			// Record bridge_system event with model info and available tool names.
			if s.runLog != nil {
				msg := fmt.Sprintf("model=%s session=%s", sanitizeForPersistence(ev.Model, 256), filepath.Base(ev.SessionFile))
				s.recordPipelineEvent(chatID, threadID, userID, observability.NewEvent("",
					observability.PhaseBridgeSystem, msg), ownership)
			}
		case "tool_use":
			toolName := normalizeToolLabel(ev.Name)
			s.trackRunFeedback(chatID, threadID, userID, bridgeEventTime(ev), ownership)
			// Flush pending thought block before showing tool
			if progress != nil {
				if text := strings.TrimSpace(assistantText.String()); text != "" {
					progress.ReportText(text)
				}
				progress.ReportState(ProgressStateWorking, "")
				progress.ReportTool(toolName, SummarizeToolInput(toolName, ev.Input))
			}
			lastStreamFlush = time.Now()
			s.recordToolUse(chatID, threadID, userID, toolName, ownership)
			// Track tool call count and warn on explosion thresholds
			if toolTracker != nil {
				toolTracker.increment(toolName)
			}
			// Detect repetitive tool call patterns (loops)
			if loopDetect != nil {
				if isLoop, pattern := loopDetect.record(toolName, ev.Input); isLoop {
					s.recordPipelineEvent(chatID, threadID, userID, observability.NewWarnEvent("",
						observability.PhaseLoopDetected,
						fmt.Sprintf("tool=%s pattern=%s", toolName, pattern)), ownership)
				}
			}
			// Record bridge_tool_use event.
			toolStartEvent := observability.NewEvent("",
				observability.PhaseBridgeToolUse,
				fmt.Sprintf("tool=%s", toolName))
			toolStartEvent.MetadataJSON = telemetryMetadata(map[string]any{
				"tool_call_id": safeTelemetryID(ev.ToolCallID),
				"ts_iso":       safeTelemetryTimestamp(ev.Timestamp),
			})
			s.recordPipelineEvent(chatID, threadID, userID, toolStartEvent, ownership)
			if toolUseSignal != nil {
				select {
				case toolUseSignal <- struct{}{}:
				default:
				}
			}

		case "tool_result":
			s.trackRunFeedback(chatID, threadID, userID, bridgeEventTime(ev), ownership)
			// Append a truncated, redacted summary to the tool tracking state.
			// Also show the summary in the live progress display.
			content := eventContent(ev)
			summary := summarizeToolResult(content)
			if summary != "" {
				if s.runLog != nil {
					s.recordToolResult(chatID, threadID, userID, summary, ownership)
				}
				if progress != nil {
					progress.ReportToolResult(summary)
				}
			}
			// Record bridge_tool_result event; duration_ms telemetry is
			// persisted in metadata only when the Bridge observed a pair
			// (duration_measured=true, present even for 0ms).
			toolResultEvent := observability.NewEvent("",
				observability.PhaseBridgeToolResult, "tool_result")
			toolID := safeTelemetryID(ev.ToolCallID)
			toolResultEvent.MetadataJSON = telemetryMetadata(map[string]any{
				"tool_call_id": toolID,
				"ts_iso":       safeTelemetryTimestamp(ev.Timestamp),
			})
			if ev.DurationMeasured && ev.DurationMs >= 0 && ev.DurationMs <= 24*time.Hour.Milliseconds() {
				toolResultEvent.MetadataJSON = telemetryMetadata(map[string]any{
					"tool_call_id":      toolID,
					"ts_iso":            safeTelemetryTimestamp(ev.Timestamp),
					"duration_measured": true,
					"duration_ms":       ev.DurationMs,
				})
			}
			s.recordPipelineEvent(chatID, threadID, userID, toolResultEvent, ownership)

		case "assistant":
			s.trackRunFeedback(chatID, threadID, userID, bridgeEventTime(ev), ownership)
			delta := eventContent(ev)
			appendAssistantText(&assistantText, delta)

			// Periodic flush — send full accumulated text so nothing is lost
			if time.Since(lastStreamFlush) >= streamFlushInterval {
				if progress != nil {
					if text := strings.TrimSpace(assistantText.String()); text != "" {
						progress.ReportText(text)
					}
				}
				// Save partial assistant text for checkpoint on timeout
				s.savePartialAssistant(chatID, threadID, userID, assistantText.String(), ownership)
				lastStreamFlush = time.Now()
			}
		case "result":
			if progress != nil {
				progress.ReportState(ProgressStateDone, "")
			}
			return s.handleResultEvent(chatID, threadID, messageID, ev, &assistantText, userText, userID, isPrivateChat, ownership)
		case "error":
			if progress != nil {
				progress.ReportState(ProgressStateFailed, "")
			}
			return s.handleErrorEvent(chatID, threadID, messageID, ev, userID, isPrivateChat, ownership)
		case "compaction_start", "compaction_end":
			// Compaction events reset idle timer and provide observability.
			// Telemetry only — never classified as productive feedback.
			// Only bounded values are persisted: enum reason, static
			// error_class, success, token deltas and the effectiveness
			// classification. Raw SDK reason/error text never reaches the
			// timeline.
			if s.runLog != nil {
				s.recordCompactionEvent(chatID, threadID, userID, bridge.CompactSessionEvent{
					Type:             ev.Type,
					RequestID:        ev.RequestID,
					Reason:           ev.Reason,
					Success:          ev.Success,
					ErrorClass:       ev.ErrorClass,
					TokensBefore:     ev.TokensBefore,
					TokensAfter:      ev.TokensAfter,
					DeltaTokens:      ev.DeltaTokens,
					DurationMeasured: ev.DurationMeasured,
					DurationMs:       ev.DurationMs,
					Timestamp:        ev.Timestamp,
				}, ownership)
			}
		case "stall", "steer":
			// bridge_health telemetry. Redacted, correlated by run/request,
			// and never treated as productive feedback or user text.
			severity := normalizeSeverity(ev.Severity)
			silentMs := ev.SilentMs
			if silentMs < 0 {
				silentMs = 0
			} else if silentMs > int64(24*time.Hour/time.Millisecond) {
				silentMs = int64(24 * time.Hour / time.Millisecond)
			}
			// Surface-neutral states: stall maps to warning/urgent, steer
			// resumes the run back to working.
			if progress != nil {
				switch {
				case ev.Type == "steer":
					progress.ReportState(ProgressStateWorking, "")
				case severity == "urgent":
					progress.ReportState(ProgressStateStallUrgent, progressSilenceDetail(silentMs))
				default:
					progress.ReportState(ProgressStateStallWarning, progressSilenceDetail(silentMs))
				}
			}
			if s.runLog != nil {
				s.countBridgeTelemetry(chatID, threadID, userID, ev.Type, ownership)
				phase := observability.PhaseBridgeStall
				if ev.Type == "steer" {
					phase = observability.PhaseBridgeSteer
				}
				level := observability.NewWarnEvent("", phase,
					fmt.Sprintf("event=%s severity=%s silent_ms=%d", ev.Type, severity, silentMs))
				meta := map[string]any{"source": "bridge_health"}
				if ts := safeTelemetryTimestamp(ev.Timestamp); ts != "" {
					meta["ts_iso"] = ts
				}
				meta["severity"] = severity
				if silentMs > 0 {
					meta["silent_ms"] = silentMs
				}
				level.MetadataJSON = telemetryMetadata(meta)
				s.recordPipelineEvent(chatID, threadID, userID, level, ownership)
			}
		case "turn_start":
			// Reset loop detector so a new turn can re-trigger loop warnings.
			if loopDetect != nil {
				loopDetect.ResetForNewTurn()
			}
		case "agent_start", "agent_end", "turn_end":
			// Agent/turn lifecycle events reset idle timer.
		case "auto_retry_start", "auto_retry_end":
			// Retry events reset idle timer.
			if s.runLog != nil {
				msg := fmt.Sprintf("event=%s", ev.Type)
				s.recordPipelineEvent(chatID, threadID, userID, observability.NewEvent("",
					observability.PhaseBridgeSystem, msg), ownership)
			}
		default:
			log.Printf("Bridge event (ignored): %s", ev.Type)
		}
	}

	return OutcomeProcessDeath
}

func (s *Service) handleSystemEvent(chatID int64, threadID int, ev bridge.Event, userID int64, owners ...runOwnership) {
	if ev.SessionFile == "" {
		return
	}
	if !s.withRunOwnership(chatID, threadID, userID, owners, func() {
		if s.sessions != nil {
			s.sessions.SetSession(chatID, threadID, userID, ev.SessionFile)
		}
	}) {
		return
	}
	s.patchContinuitySessionID(chatID, threadID, ev.SessionFile, userID, owners...)
}

func eventContent(ev bridge.Event) string {
	return ev.ContentText()
}

func (s *Service) handleResultEvent(chatID int64, threadID int, messageID int, ev bridge.Event, assistantText *strings.Builder, userText string, userID int64, isPrivateChat bool, owners ...runOwnership) Outcome {
	ownership := firstRunOwnership(owners)
	if ownership.finalizer == nil {
		var claimed bool
		ownership, claimed = s.claimRunFinalization(chatID, threadID, userID, owners...)
		if !claimed {
			return OutcomeCanceled
		}
	}
	// Terminal bridge event: counts as surface-updating feedback.
	s.trackRunFeedback(chatID, threadID, userID, bridgeEventTime(ev), ownership)
	content := eventContent(ev)
	if content != "" {
		prior := assistantText.String()
		if div, ok := detectContentDivergence(prior, content); ok && div.significant() {
			logContentDivergence(div)
			s.recordPipelineEvent(chatID, threadID, userID, func() observability.RunEvent {
				ev := observability.NewWarnEvent("", observability.PhaseBridgeContentDiverges, div.message())
				ev.MetadataJSON = div.metadataJSON()
				return ev
			}(), ownership)
		}
		// Result content is authoritative over streamed deltas.
		assistantText.Reset()
		appendAssistantText(assistantText, content)
	}

	// Store session file path as fallback in case the system event was missed.
	if ev.SessionFile != "" {
		existing := ""
		if s.sessions != nil {
			existing = s.sessions.GetSession(chatID, threadID, userID)
		}
		if existing == "" {
			if !s.withCurrentRunOwnership(chatID, threadID, userID, []runOwnership{ownership}, func() {
				if s.sessions != nil {
					s.sessions.SetSession(chatID, threadID, userID, ev.SessionFile)
				}
			}) {
				// A stale owner may still finish its detached runlog, but cannot
				// install its session or continuity state into the replacement slot.
				s.cancelClaimedRunLog(pipelineInput{chatID: chatID, threadID: threadID, userID: userID}, ownership)
				return OutcomeCanceled
			}
			if !s.activeRunStillOwned(chatID, threadID, userID, ownership.owner) {
				s.cancelClaimedRunLog(pipelineInput{chatID: chatID, threadID: threadID, userID: userID}, ownership)
				return OutcomeCanceled
			}
			s.patchContinuitySessionID(chatID, threadID, ev.SessionFile, userID, ownership)
		}
		// Also persist session_file to runlog as fallback in case the system
		// event was missed or arrived before the runlog entry existed. This is
		// an intentional redundant write — the value is idempotent (same
		// session_file), and the DB update is a no-op when the value is unchanged.
		s.updateRunLogSessionFile(chatID, threadID, userID, ev.SessionFile, ownership)
	}

	s.recordUsage(chatID, threadID, ev, userID)

	// Record bridge_result event with usage data.
	s.recordPipelineEvent(chatID, threadID, userID, observability.NewEvent("",
		observability.PhaseBridgeResult,
		fmt.Sprintf("tokens_in=%d tokens_out=%d cost=$%.4f turns=%d",
			ev.InputTokens, ev.OutputTokens, ev.CostUSD, ev.NumTurns)), ownership)

	finalText := strings.TrimSpace(assistantText.String())

	if finalText == "" {
		toolSummary := s.getRunToolSummary(chatID, threadID, userID, ownership)
		return s.handleEmptyResult(chatID, threadID, messageID, ev, userText, toolSummary, userID, isPrivateChat, ownership)
	}

	// Capture runID before completeRunLog cleans up runLogStates.
	successRunID := s.getRunID(chatID, threadID, userID, ownership)
	// A superseded run must not be recorded as completed: its final reply is
	// intentionally not delivered (the replacement run owns the conversation),
	// so the timeline would show a completed run with no delivery. Persist an
	// honest canceled/superseded terminal status instead.
	if !s.activeRunStillOwned(chatID, threadID, userID, ownership.owner) {
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewEvent(successRunID,
			observability.PhaseRunCanceled, "status=superseded"), ownership)
		s.completeRunLog(chatID, threadID, userID, runlog.RunCanceled, "", "superseded", ownership)
		return OutcomeCanceled
	}
	// Record run_completed while the state is pending so SQLite includes it in
	// the atomic terminal completion batch.
	s.recordPipelineEvent(chatID, threadID, userID, observability.NewEvent(successRunID,
		observability.PhaseRunCompleted, "status=completed"), ownership)
	s.completeRunLog(chatID, threadID, userID, runlog.RunCompleted, finalText, "", ownership)

	return s.handleNormalReply(chatID, threadID, messageID, finalText, successRunID, userText, userID, isPrivateChat, ownership)
}

// handleEmptyResult handles the case where the bridge returned no text.
// It distinguishes between "worked but empty" (tokens consumed) and "no work at all".
func (s *Service) handleEmptyResult(chatID int64, threadID int, messageID int, ev bridge.Event, userText string, toolSummary string, userID int64, isPrivateChat bool, owners ...runOwnership) Outcome {
	ownership := firstRunOwnership(owners)
	if emptyResultHadWork(ev) {
		log.Printf("bridge: empty result after work chat=%d thread=%d request=%s turns=%d cost=$%.4f in=%d out=%d",
			chatID, threadID, ev.RequestID, ev.NumTurns, ev.CostUSD, ev.InputTokens, ev.OutputTokens)

		// Mark empty result so next turn does not Continue into a suspect session.
		// Skip billing errors — they're provider issues, not session failures.
		if !s.withCurrentRunOwnership(chatID, threadID, userID, []runOwnership{ownership}, func() {
			if s.sessions != nil && !isBillingError(ev.Message) && !isBillingError(ev.Content) {
				s.sessions.MarkEmptyResult(chatID, threadID, userID)
			}
		}) {
			s.cancelClaimedRunLog(pipelineInput{chatID: chatID, threadID: threadID, userID: userID}, ownership)
			return OutcomeCanceled
		}

		emptyWorkRunID := s.getRunID(chatID, threadID, userID, ownership)
		s.patchContinuityFailure(chatID, threadID, "failed", "empty_result", userID, isPrivateChat, owners...)
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent(emptyWorkRunID,
			observability.PhaseRunFailed, "empty_result"), ownership)
		s.completeRunLog(chatID, threadID, userID, runlog.RunFailed, "", "empty_result", ownership)

		recoveryMsg := buildEmptyResultRecoveryMessage(toolSummary)
		if s.activeRunStillOwned(chatID, threadID, userID, ownership.owner) {
			if err := s.output.SendError(chatID, threadID, recoveryMsg); err != nil {
				log.Printf("Failed to send recovery message to chat %d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
			}
		}
	} else {
		log.Printf("bridge: empty result (no work) chat=%d thread=%d request=%s",
			chatID, threadID, ev.RequestID)
		emptyNoWorkRunID := s.getRunID(chatID, threadID, userID, ownership)
		s.patchContinuityFailure(chatID, threadID, "failed", "empty_result", userID, isPrivateChat, owners...)
		s.recordPipelineEvent(chatID, threadID, userID, observability.NewErrorEvent(emptyNoWorkRunID,
			observability.PhaseRunFailed, "empty_result"), ownership)
		s.completeRunLog(chatID, threadID, userID, runlog.RunFailed, "", "empty_result", ownership)
		if s.activeRunStillOwned(chatID, threadID, userID, ownership.owner) {
			if err := s.output.SendError(chatID, threadID, bridgeEmptyResultMessage); err != nil {
				log.Printf("Failed to send empty-result error to chat %d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
			}
		}
	}

	s.output.ConfirmMessage(chatID, messageID)
	return OutcomeLLMError
}

// handleNormalReply sends the assistant's text response to the chat as a
// normal reply and finalizes the turn.
func (s *Service) handleNormalReply(chatID int64, threadID int, messageID int, safeFinalText string, successRunID string, userText string, userID int64, isPrivateChat bool, owners ...runOwnership) Outcome {
	if len(owners) > 0 && !s.activeRunStillOwned(chatID, threadID, userID, firstRunOwnership(owners).owner) {
		return OutcomeCanceled
	}
	outboundMsgID, err := s.output.SendReply(chatID, threadID, safeFinalText)
	if err != nil {
		log.Printf("Failed to send reply to chat %d: %s", chatID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
	}
	if outboundMsgID != 0 && successRunID != "" {
		s.updateRunLogOutboundMessage(chatID, threadID, userID, successRunID, outboundMsgID, owners...)
	}
	s.output.ConfirmMessage(chatID, messageID)
	s.afterSuccessfulTurn(chatID, threadID, userText, safeFinalText, successRunID, userID, isPrivateChat, owners...)
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

// --- Run log lifecycle ---

func runLogKey(chatID int64, threadID int, userID int64) string {
	return fmt.Sprintf("%d:%d:%d", chatID, threadID, userID)
}

// startRunLogParams carries the extended context needed to populate a
// run_journal start row. All fields are populated before Bridge execution.
type startRunLogParams struct {
	ChatID     int64
	ThreadID   int
	RequestID  string
	MessageID  int
	CWD        string
	Prompt     string
	UserID     int64
	AgentName  string
	Provider   string
	Model      string
	Profile    string
	EntryPoint string
	Owner      *activeRun
}

// startRunLog creates a new runlog entry and stores the per-run state.
// RunID is set to a uuid for durable unique identification across restarts.
// Returns true if the runlog was started.
func (s *Service) startRunLog(p startRunLogParams) bool {
	if s.runLog == nil || p.RequestID == "" {
		return false
	}
	key := runLogKey(p.ChatID, p.ThreadID, p.UserID)

	// Keep runlog replacement in the same critical section as owner-conditional
	// post-terminal writes. A new run must not replace the state between an old
	// run's ownership check and its terminal persistence.
	activeRunSlotMu.Lock()
	defer activeRunSlotMu.Unlock()
	if p.Owner != nil {
		p.Owner.mu.RLock()
		defer p.Owner.mu.RUnlock()
		if p.Owner.superseded {
			return false
		}
		current, ok := s.activeSessions.Load(key)
		if !ok || current != p.Owner {
			return false
		}
	}
	s.runLogMu.Lock()
	defer s.runLogMu.Unlock()

	runID := idgen.New()
	now := time.Now()
	runLogCtx, runLogCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer runLogCancel()
	entryPoint := p.EntryPoint
	if entryPoint == "" {
		entryPoint = observability.EntryPointTelegram
	}
	err := s.runLog.Start(runLogCtx, runlog.RunRecord{
		RunID:             runID,
		ChatID:            p.ChatID,
		ThreadID:          p.ThreadID,
		RequestID:         p.RequestID,
		CWD:               sanitizeForPersistence(p.CWD, 512),
		Prompt:            sanitizeForPersistence(p.Prompt, 500),
		StartedAt:         now,
		UserID:            p.UserID,
		EntryPoint:        sanitizeForPersistence(entryPoint, 64),
		AgentName:         sanitizeForPersistence(p.AgentName, 128),
		Provider:          sanitizeForPersistence(p.Provider, 128),
		Model:             sanitizeForPersistence(p.Model, 256),
		CapabilityProfile: sanitizeForPersistence(p.Profile, 128),
		InboundMessageID:  int64(p.MessageID),
	})
	if err != nil {
		log.Printf("runlog: failed to start %s: %s", p.RequestID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		return false
	}
	state := &runLogState{runID: runID, requestID: p.RequestID, owner: p.Owner, startedAt: now}
	s.runLogStates[key] = state
	if p.Owner != nil {
		p.Owner.runLogState = state
	}
	return true
}

// recordPipelineEvent records a single observable event for the active run
// on a chat/thread. Best-effort: errors are logged, never block the caller.
// Uses a 500ms context timeout to avoid blocking the pipeline.
// When the runlog state has been cleaned up (e.g. after completeRunLog),
// falls back to the caller-provided ev.RunID so completion events are not
// silently dropped.
// updateRunLogSession updates the session ID for an active runlog entry.
func (s *Service) updateRunLogSession(chatID int64, threadID int, userID int64, sessionID string, owners ...runOwnership) {
	if s.runLog == nil || sessionID == "" {
		return
	}
	sessionID = sanitizeForPersistence(sessionID, 200)
	s.withRunOwnership(chatID, threadID, userID, owners, func() {
		state, ok := s.runLogStateFor(chatID, threadID, userID, firstRunOwnership(owners))
		if !ok || state == nil {
			return
		}

		updateCtx, updateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer updateCancel()
		if err := s.runLog.Update(updateCtx, runlog.RunUpdate{
			RunID:     state.runID,
			SessionID: &sessionID,
		}); err != nil {
			log.Printf("runlog: failed to update session for %s: %s", state.runID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
	})
}

// updateRunLogSessionFile persists the PI SDK session file path into the
// active runlog entry so that runlog.GetLastOutboundMessage(sessionFile)
// can bridge PI sessions to Telegram outbound messages.
func (s *Service) updateRunLogSessionFile(chatID int64, threadID int, userID int64, sessionFile string, owners ...runOwnership) {
	if s.runLog == nil || sessionFile == "" {
		return
	}
	sessionFile = sanitizeForPersistence(sessionFile, 512)
	s.withRunOwnership(chatID, threadID, userID, owners, func() {
		state, ok := s.runLogStateFor(chatID, threadID, userID, firstRunOwnership(owners))
		if !ok || state == nil {
			return
		}

		updateCtx, updateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer updateCancel()
		if err := s.runLog.Update(updateCtx, runlog.RunUpdate{
			RunID:       state.runID,
			SessionFile: &sessionFile,
		}); err != nil {
			log.Printf("runlog: failed to update session_file for %s: %s", state.runID, sanitizeForPersistence(err.Error(), maxRunlogErrorRunes))
		}
	})
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

const maxCheckpointRunes = 2000

func truncateCheckpoint(s string) string {
	return truncateRunes(s, maxCheckpointRunes)
}
