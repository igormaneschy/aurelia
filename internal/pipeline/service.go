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
	"github.com/igormaneschy/aurelia/internal/orchestrator"
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

// ProgressReporter reports bridge tool activity to the chat transport.
type ProgressReporter interface {
	ReportTool(toolName string)
	ReportToolResult(summary string)
	ReportText(text string)
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
	ExecuteApprovedPlan(chatID int64, threadID int, messageID int, cwd string, userID int64, plan *orchestrator.Plan)
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
	Orchestrator *orchestrator.Orchestrator
	Dreamer      Dreamer
	ProjectIndex *runtime.ProjectIndex
	Bindings     projectbinding.Store
	RunLog       runlog.Store
	Continuity   continuity.Store
	UsersStore   *users.Store
	UserResolver *users.Resolver
}

// Service owns the LLM/message pipeline independent from Telegram routing.
type Service struct {
	config          *config.AppConfig
	bridge          *bridge.Bridge
	resilient       *ResilientBridge
	agents          *agents.Registry
	profiles        *profiles.Resolver // Phase 1: canonical profile resolver
	persona         *persona.CanonicalIdentityService
	sessions        *session.Store
	resolver        *runtime.PathResolver
	memoryDir       string
	exePath         string
	botCwd          string
	output          Output
	orchestrator    *orchestrator.Orchestrator
	dreamer         Dreamer
	nudgeBuffer     *session.NudgeBuffer
	memoryCache     *memoryCache
	projectIndex    *runtime.ProjectIndex
	bindings        projectbinding.Store
	bridgeFailures  FailureTracker
	activeSessions  sync.Map // "chatID:threadID:userID" → context.CancelFunc
	runLog          runlog.Store
	runLogMu        sync.Mutex
	runLogStates    map[string]*runLogState
	continuity      continuity.Store
	summaryCounter  *summaryCounter
	summaryInterval int
	usersStore      *users.Store
	userResolver    *users.Resolver
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
		agents:           cfg.Agents,
		profiles:         cfg.Profiles,
		persona:          cfg.Persona,
		sessions:         cfg.Sessions,
		resolver:         cfg.Resolver,
		memoryDir:        cfg.MemoryDir,
		exePath:          cfg.ExePath,
		botCwd:           cfg.BotCwd,
		output:           cfg.Output,
		orchestrator:     cfg.Orchestrator,
		dreamer:          cfg.Dreamer,
		nudgeBuffer:      session.NewNudgeBuffer(),
		memoryCache:      newMemoryCache(),
		projectIndex:     cfg.ProjectIndex,
		bindings:         cfg.Bindings,
		runLog:           cfg.RunLog,
		runLogStates:     make(map[string]*runLogState),
		continuity:       cfg.Continuity,
		summaryCounter:   &summaryCounter{counts: make(map[continuity.ConversationKey]int)},
		summaryInterval:  defaultSummaryInterval,
		usersStore:       cfg.UsersStore,
		userResolver:     cfg.UserResolver,
		activeToolStates: make(map[string]activeToolState),
	}

	if cfg.Bridge != nil {
		s.modelCataloger = &bridgeModelCataloger{br: cfg.Bridge}
		rbCfg := DefaultResilientConfig()
		if cfg.AppConfig != nil {
			rbCfg.OpenRouterAPIKey = cfg.AppConfig.ProviderAPIKey("openrouter")
		}
		s.resilient = NewResilientBridge(cfg.Bridge, rbCfg)
		s.resilient.ContinuitySnapshot = s.continuitySnapshot
		s.resilient.OnEvent = func(chatID int64, threadID int, userID int64, phase, level, message string) {
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
	if val, loaded := s.activeSessions.LoadAndDelete(key); loaded {
		if cancelFn := extractCancelFn(val); cancelFn != nil {
			cancelFn()
		}
	} else {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), bridgeCommandTimeout)
	defer cancel()
	_, err := s.bridge.ExecuteSync(ctx, bridge.Request{
		Command: "abort",
		Options: bridge.RequestOptions{ChatID: chatID, ThreadID: threadID, UserID: uid},
	})
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
		if val, loaded := s.activeSessions.LoadAndDelete(key); loaded {
			if cancelFn := extractCancelFn(val); cancelFn != nil {
				cancelFn()
				cancelled = true
			}
		}

		// Send a scoped bridge abort so the bridge also cleans up this session.
		s.sendScopedAbort(chatID, threadID, uid)

		return true
	})
	return cancelled
}

// scopedAbortRequest builds a bridge abort request scoped to a specific
// chat/thread/user session. This is a separate function so it can be
// unit-tested for scope correctness without a bridge process.
func scopedAbortRequest(chatID int64, threadID int, userID int64) bridge.Request {
	return bridge.Request{
		Command: "abort",
		Options: bridge.RequestOptions{ChatID: chatID, ThreadID: threadID, UserID: userID},
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
		Options: bridge.RequestOptions{ChatID: chatID, ThreadID: threadID, UserID: uid},
	})
	if err != nil {
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

// SetOrchestrator injects the orchestrator after construction.
func (s *Service) SetOrchestrator(o *orchestrator.Orchestrator) {
	s.orchestrator = o
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
