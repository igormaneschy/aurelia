package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/persona"
	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/security"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/transport"
	"github.com/igormaneschy/aurelia/internal/users"
)

// ProgressState is a surface-neutral progress state. The core emits states;
// each transport (Telegram receipt, TUI indicator) maps them to its own
// surface without duplicating messages or polluting the transcript.
type ProgressState string

const (
	// ProgressStateWorking means the run is actively producing feedback
	// (tool_use, assistant text, steer resume).
	ProgressStateWorking ProgressState = "working"
	// ProgressStateWaiting means no feedback within the heartbeat threshold;
	// the model is thinking without tools.
	ProgressStateWaiting ProgressState = "waiting"
	// ProgressStateStallWarning means the bridge reported a stall with
	// severity=warning (long silence).
	ProgressStateStallWarning ProgressState = "stall_warning"
	// ProgressStateStallUrgent means the bridge reported a stall with
	// severity=urgent.
	ProgressStateStallUrgent ProgressState = "stall_urgent"
	// ProgressStateDone means the run completed with a result.
	ProgressStateDone ProgressState = "done"
	// ProgressStateCanceled means the run was canceled before completion.
	ProgressStateCanceled ProgressState = "canceled"
	// ProgressStateFailed means the run ended with an error.
	ProgressStateFailed ProgressState = "failed"
)

// ProgressReporter reports bridge tool activity to the chat transport.
type ProgressReporter interface {
	ReportTool(toolName, detail string)
	ReportToolResult(summary string)
	ReportText(text string)
	// ReportState carries a surface-neutral state change with an optional
	// human detail string. Adapters render it in their own surface.
	ReportState(state ProgressState, detail string)
	Delete()
}

// Output adapts pipeline responses to a chat transport such as Telegram.
type Output interface {
	StartTyping(chatID int64, threadID int) func()
	NewProgress(chatID int64, threadID int) ProgressReporter
	SendError(chatID int64, threadID int, text string) error
	SendReply(chatID int64, threadID int, text string) (int64, error)
	SendText(chatID int64, threadID int, text string) (transport.MessageHandle, error)
	DeleteMessage(message transport.MessageHandle)
	ConfirmMessage(chatID int64, messageID int)
}

// Dreamer receives turn lifecycle notifications for memory/nudge updates.
type Dreamer interface {
	AfterTurn(userID int64)
	AfterTurnNudge(chatID int64, threadID int, userID int64, cwd string, sessionFile string, buffer *session.NudgeBuffer)
	FlushNudge(chatID int64, threadID int, userID int64, cwd string, sessionFile string, buffer *session.NudgeBuffer)
}

// Config contains dependencies needed by the business pipeline.
type Config struct {
	AppConfig    *config.AppConfig
	Bridge       *bridge.Bridge
	Agents       *agents.Registry
	Profiles     *profiles.Resolver // Phase 1: canonical profile resolver
	Persona      *persona.CanonicalIdentityService
	Sessions     *session.Store
	Resolver     *runtime.PathResolver
	MemoryDir    string
	ExePath      string
	BotCwd       string
	Output       Output
	Dreamer      Dreamer
	ProjectIndex *runtime.ProjectIndex
	Bindings     projectbinding.Store
	RunLog       runlog.Store
	Continuity   continuity.Store
	UsersStore   *users.Store
	UserResolver *users.Resolver
	// NudgeBuffer, when set, is shared across pipeline instances (e.g. TUI
	// creates a pipeline per send). When nil, NewService creates a fresh one.
	NudgeBuffer *session.NudgeBuffer
	// MemoryCache, when set, is shared across pipeline instances so the TUI
	// (which creates a pipeline per send) reuses the mtime cache between turns.
	// When nil, NewService creates a fresh one.
	MemoryCache *MemoryCache
	// TokenGuard, when set, is shared across pipeline instances so the TUI
	// (which creates a pipeline per send) retains stall-turn compaction state.
	// When nil, NewService creates a fresh one.
	TokenGuard *session.TokenGuard
	// EntryPoint identifies the surface that triggered this pipeline instance.
	// Accepted values from observability: "telegram" (default), "tui", "cron".
	// When empty, NewService normalizes to observability.EntryPointTelegram.
	EntryPoint string
}

// Service owns the LLM/message pipeline independent from Telegram routing.
type Service struct {
	config            *config.AppConfig
	bridge            *bridge.Bridge // PI-specific: lifecycle, cancel, model catalog
	queryMaxRetries   int
	queryRetryBackoff time.Duration
	fallbackProvider  string
	fallbackModel     string
	openRouterAPIKey  string
	// OnEvent is called when executeQuery emits an observable event (retry, fallback).
	// The callback must be fast and fail-open. May be nil.
	OnEvent func(chatID int64, threadID int, userID int64, phase, level, message string)
	// testBridgeQuery overrides bridge.Execute in tests when set.
	testBridgeQuery    func(ctx context.Context, req bridge.Request) (<-chan bridge.Event, error)
	testSessionStats   func(ctx context.Context, opts bridge.RequestOptions) (*bridge.SessionStats, error)
	testCompactSession func(ctx context.Context, chatID int64, threadID int, userID int64, opts bridge.RequestOptions) (*bridge.CompactSessionResult, error)
	testRotateSession  func(ctx context.Context, chatID int64, threadID int, userID int64, opts bridge.RequestOptions) (*bridge.RotateSessionResult, error)
	agents             *agents.Registry
	profiles           *profiles.Resolver // Phase 1: canonical profile resolver
	persona            *persona.CanonicalIdentityService
	sessions           *session.Store
	resolver           *runtime.PathResolver
	memoryDir          string
	exePath            string
	botCwd             string
	output             Output
	dreamer            Dreamer
	nudgeBuffer        *session.NudgeBuffer
	memoryCache        *MemoryCache
	projectIndex       *runtime.ProjectIndex
	bindings           projectbinding.Store
	bridgeFailures     FailureTracker
	activeSessions     sync.Map // "chatID:threadID:userID" → context.CancelFunc
	runLog             runlog.Store
	runLogMu           sync.Mutex
	runLogStates       map[string]*runLogState
	continuity         continuity.Store
	summaryCounter     *summaryCounter
	summaryInterval    int
	tokenGuard         *session.TokenGuard
	usersStore         *users.Store
	userResolver       *users.Resolver
	entryPoint         string
	// active tool monitoring state — set/cleared per-run for /status.
	toolStateMu      sync.Mutex
	activeToolStates map[string]activeToolState

	// modelCataloger checks model capability (SupportsImages) via the PI model
	// registry. Defaults to bridgeModelCataloger; overridden in tests.
	modelCataloger ModelCataloger
}

type activeToolState struct {
	tracker  *toolCallTracker
	detector *loopDetector
}

const defaultSummaryInterval = 5

// NewService builds a pipeline service with explicit dependencies.
func NewService(cfg Config) *Service {
	s := &Service{
		config:           cfg.AppConfig,
		bridge:           cfg.Bridge,
		entryPoint:       normalizeEntryPoint(cfg.EntryPoint),
		agents:           cfg.Agents,
		profiles:         cfg.Profiles,
		persona:          cfg.Persona,
		sessions:         cfg.Sessions,
		resolver:         cfg.Resolver,
		memoryDir:        cfg.MemoryDir,
		exePath:          cfg.ExePath,
		botCwd:           cfg.BotCwd,
		output:           cfg.Output,
		dreamer:          cfg.Dreamer,
		nudgeBuffer:      cfg.NudgeBuffer,
		memoryCache:      cfg.MemoryCache,
		projectIndex:     cfg.ProjectIndex,
		bindings:         cfg.Bindings,
		runLog:           cfg.RunLog,
		runLogStates:     make(map[string]*runLogState),
		continuity:       cfg.Continuity,
		summaryCounter:   &summaryCounter{counts: make(map[continuity.ConversationKey]int)},
		summaryInterval:  defaultSummaryInterval,
		tokenGuard:       cfg.TokenGuard,
		usersStore:       cfg.UsersStore,
		userResolver:     cfg.UserResolver,
		activeToolStates: make(map[string]activeToolState),
	}

	// Backward compat: create fresh instances when not injected by the caller.
	if s.nudgeBuffer == nil {
		s.nudgeBuffer = session.NewNudgeBuffer()
	}
	if s.memoryCache == nil {
		s.memoryCache = NewMemoryCache()
	}
	if s.tokenGuard == nil {
		s.tokenGuard = session.NewTokenGuard()
	}

	if cfg.Bridge != nil {
		s.modelCataloger = &bridgeModelCataloger{br: cfg.Bridge, ttl: defaultModelsCacheTTL}
		s.queryMaxRetries = defaultQueryMaxRetries
		s.queryRetryBackoff = defaultQueryRetryBackoff
		s.fallbackProvider = defaultFallbackProvider
		s.fallbackModel = defaultFallbackModel
		if cfg.AppConfig != nil {
			s.openRouterAPIKey = cfg.AppConfig.ProviderAPIKey("openrouter")
		}
		s.OnEvent = func(chatID int64, threadID int, userID int64, phase, level, message string) {
			s.recordPipelineEvent(chatID, threadID, userID, observability.RunEvent{
				Phase:   phase,
				Level:   level,
				Message: message,
			})
		}
	}

	if s.config != nil && s.config.SummaryInterval > 0 {
		s.summaryInterval = s.config.SummaryInterval
	}

	return s
}

// normalizeEntryPoint normalizes the entry point string, defaulting to
// observability.EntryPointTelegram when empty or unknown.
func normalizeEntryPoint(ep string) string {
	if ep == "" {
		return observability.EntryPointTelegram
	}
	switch ep {
	case observability.EntryPointTelegram,
		observability.EntryPointTUI,
		observability.EntryPointCron,
		observability.EntryPointNudge,
		observability.EntryPointCLI:
		return ep
	default:
		return observability.EntryPointTelegram
	}
}

// EntryPoint returns the normalized entry point for this pipeline instance.
func (s *Service) EntryPoint() string {
	if s == nil {
		return ""
	}
	return s.entryPoint
}

// Cancel stops the active run for a chat thread by sending abort to bridge.
func (s *Service) Cancel(chatID int64, threadID int, userID ...int64) bool {
	if s == nil {
		return false
	}
	if s.bridge == nil {
		return false
	}
	uid := firstUserID(userID)
	key := sessionKey(chatID, threadID, uid)

	// Stop the old goroutine so it doesn't retry after abort
	activeRunSlotMu.Lock()
	val, loaded := s.activeSessions.LoadAndDelete(key)
	if loaded {
		if oldRun, ok := val.(*activeRun); ok {
			markRunSuperseded(oldRun)
		}
		if cancelFn := extractCancelFn(val); cancelFn != nil {
			cancelFn()
		}
	}
	activeRunSlotMu.Unlock()
	if !loaded {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
	defer cancel()
	_, err := s.bridge.ExecuteSync(ctx, scopedAbortRequest(chatID, threadID, uid))
	return err == nil
}

// CancelAllForUser cancels all active sessions for a given user.
// Iterates the local activeSessions map and cancels each matching session.
// For each session found, it cancels the local goroutine context, removes
// the entry from activeSessions, and sends a scoped bridge abort with
// chatID, threadID, and userID.
//
// Returns true if at least one local active session was cancelled.
// Returns false if no active sessions matched or if s is nil.
func (s *Service) CancelAllForUser(userID int64) bool {
	if s == nil {
		return false
	}
	cancelled := false
	s.activeSessions.Range(func(key, value interface{}) bool {
		keyStr, ok := key.(string)
		if !ok {
			return true // skip non-string keys
		}
		chatID, threadID, uid, ok := parseSessionKey(keyStr)
		if !ok {
			log.Printf("pipeline: CancelAllForUser: skipping malformed key %q", keyStr)
			return true
		}
		if uid != userID {
			return true // belongs to a different user, leave it
		}

		// Cancel the local goroutine context and remove from active sessions.
		activeRunSlotMu.Lock()
		val, loaded := deleteActiveSessionForCancel(&s.activeSessions, keyStr, value)
		if loaded {
			if oldRun, ok := val.(*activeRun); ok {
				markRunSuperseded(oldRun)
			}
			if cancelFn := extractCancelFn(val); cancelFn != nil {
				cancelFn()
				cancelled = true
			}
		}
		activeRunSlotMu.Unlock()

		// Send a scoped bridge abort so the bridge also cleans up this session.
		s.sendScopedAbort(chatID, threadID, uid)

		return true
	})
	return cancelled
}

// deleteActiveSessionForCancel removes the value observed by CancelAllForUser.
// A replacement may be installed between sync.Map.Range and the deletion; the
// activeRun path must compare the token so cancellation cannot delete the newer
// run. Legacy function values are not comparable in Go, so they retain the
// historical load-and-delete behavior under activeRunSlotMu.
func deleteActiveSessionForCancel(sessions *sync.Map, key string, expected any) (any, bool) {
	if run, ok := expected.(*activeRun); ok {
		if sessions.CompareAndDelete(key, run) {
			return run, true
		}
		return nil, false
	}
	return sessions.LoadAndDelete(key)
}

// scopedAbortRequest builds a bridge abort request scoped to a specific
// chat/thread/user session. This is a separate function so it can be
// unit-tested for scope correctness without a bridge process.
func scopedAbortRequest(chatID int64, threadID int, userID int64) bridge.Request {
	return bridge.Request{
		Command: "abort",
		Options: bridge.RequestOptions{
			ChatID:   chatID,
			ThreadID: threadID,
			UserID:   userID,
		},
	}
}

// sendScopedAbort sends an abort command scoped to a specific chat/thread/user.
// Does nothing if s.bridge is nil.
func (s *Service) sendScopedAbort(chatID int64, threadID int, userID int64) {
	if s.bridge == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
	defer cancel()
	_, _ = s.bridge.ExecuteSync(ctx, scopedAbortRequest(chatID, threadID, userID))
}

// WorkStatus returns the active session status from the bridge.
// Returns a description string and the pending message count.
func (s *Service) WorkStatus(chatID int64, threadID int, userID ...int64) (string, int) {
	if s == nil {
		return "", 0
	}
	if s.bridge == nil {
		return "", 0
	}
	uid := firstUserID(userID)
	key := sessionKey(chatID, threadID, uid)
	if _, active := s.activeSessions.Load(key); !active {
		return "", 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
	defer cancel()
	ev, err := s.bridge.ExecuteSync(ctx, bridge.Request{
		Command: "get-state",
		Options: bridge.RequestOptions{
			ChatID:   chatID,
			ThreadID: threadID,
			UserID:   uid,
		},
	})
	if err != nil || ev == nil {
		return "", 0
	}

	var state struct {
		IsStreaming  bool `json:"is_streaming"`
		PendingCount int  `json:"pending_count"`
	}
	if err := json.Unmarshal([]byte(ev.Content), &state); err != nil {
		return "", 0
	}

	desc := "rodando"
	if !state.IsStreaming {
		desc = "processando"
	}
	return desc, state.PendingCount
}

// SetProjectIndex injects a cached project name index for fast lookup.
func (s *Service) SetProjectIndex(pi *runtime.ProjectIndex) {
	s.projectIndex = pi
}

// SetDreamer injects the dream system after construction.
func (s *Service) SetDreamer(d Dreamer) {
	s.dreamer = d
}

// SetRunLog injects the run log store after construction (optional).
func (s *Service) SetRunLog(rl runlog.Store) {
	s.runLog = rl
}

// SetContinuity injects the continuity store after construction (optional).
func (s *Service) SetContinuity(cs continuity.Store) {
	s.continuity = cs
}

// NudgeBuffer returns the per-service nudge buffer for command-triggered flushes.
func (s *Service) NudgeBuffer() *session.NudgeBuffer {
	return s.nudgeBuffer
}

// MemoryCache returns the memory directory cache. Exposed so callers can
// verify sharing across pipeline instances (e.g. Telegram singleton vs TUI
// per-send pipelines should hold the same cache pointer).
func (s *Service) MemoryCache() *MemoryCache {
	return s.memoryCache
}

// TokenGuard returns the session token guard. Exposed so callers can share one
// instance across Telegram and TUI pipeline instances.
func (s *Service) TokenGuard() *session.TokenGuard {
	return s.tokenGuard
}

// getSecurityConfig returns the security configuration from AppConfig,
// falling back to safe defaults if not configured.
func (s *Service) getSecurityConfig() security.SecurityConfig {
	if s.config != nil {
		return s.config.SecurityConfig
	}
	return security.DefaultConfig()
}

// sessionKey builds a user-scoped key for the activeSessions map.
func sessionKey(chatID int64, threadID int, userID int64) string {
	return fmt.Sprintf("%d:%d:%d", chatID, threadID, userID)
}

// parseSessionKey parses a "chatID:threadID:userID" key into its components.
// Returns false if the key is malformed, has extra trailing content,
// or contains non-numeric fields.
func parseSessionKey(key string) (chatID int64, threadID int, userID int64, ok bool) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	cid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	tid, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, false
	}
	uid, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return cid, tid, uid, true
}

func firstUserID(userID []int64) int64 {
	if len(userID) == 0 {
		return 0
	}
	return userID[0]
}

// ActiveToolSnapshot holds a point-in-time view of the active run's tool state
// for diagnostic display (e.g. /status). All fields are zero when no run is active.
type ActiveToolSnapshot struct {
	ToolCount   int
	LoopWarned  bool
	RecentTools []string // up to 5 most recent distinct tool names, oldest first
}

// SetActiveToolState records the active run's tool monitoring state.
// Called at the start of executeAsync; cleared on return.
func (s *Service) SetActiveToolState(chatID int64, threadID int, userID int64, tracker *toolCallTracker, detector *loopDetector) {
	s.toolStateMu.Lock()
	defer s.toolStateMu.Unlock()
	if s.activeToolStates == nil {
		s.activeToolStates = make(map[string]activeToolState)
	}
	s.activeToolStates[sessionKey(chatID, threadID, userID)] = activeToolState{tracker: tracker, detector: detector}
}

// ClearActiveToolState removes the active run's tool monitoring state.
func (s *Service) ClearActiveToolState(chatID int64, threadID int, userID int64) {
	s.toolStateMu.Lock()
	defer s.toolStateMu.Unlock()
	delete(s.activeToolStates, sessionKey(chatID, threadID, userID))
}

// GetActiveToolSnapshot returns a safe snapshot of the active run's tool state.
// Returns zero values when no run is active.
func (s *Service) GetActiveToolSnapshot(chatID int64, threadID int, userID int64) ActiveToolSnapshot {
	s.toolStateMu.Lock()
	state := s.activeToolStates[sessionKey(chatID, threadID, userID)]
	s.toolStateMu.Unlock()

	var snap ActiveToolSnapshot
	if state.tracker != nil {
		snap.ToolCount = state.tracker.countLocked()
	}
	if state.detector != nil {
		state.detector.mu.Lock()
		snap.LoopWarned = state.detector.warned
		snap.RecentTools = state.detector.recentDistinctToolsLocked(5)
		state.detector.mu.Unlock()
	}
	return snap
}

const bridgeCommandTimeout = 10 * time.Second
