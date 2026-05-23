package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/cron"
	"github.com/igormaneschy/aurelia/internal/deps"
	"github.com/igormaneschy/aurelia/internal/dream"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/persona"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/telegram"
	"github.com/igormaneschy/aurelia/internal/users"
	"github.com/igormaneschy/aurelia/pkg/stt"
)

type app struct {
	config     *config.AppConfig
	resolver   *runtime.PathResolver
	bridge     *bridge.Bridge
	agents     *agents.Registry
	cronStore  *cron.SQLiteCronStore
	bindings   projectbinding.Store
	runLog     runlog.Store
	continuity continuity.Store
	bot        *telegram.BotController
	sessions   *session.Store
	scheduler  *cron.Scheduler
	cronCtx    context.Context
	cronCancel context.CancelFunc

	onboardingStore *users.OnboardingStore
}

var runClaudeAuthStatus = func() ([]byte, error) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			fallback := filepath.Join(home, ".local", "bin", "claude")
			if stat, statErr := os.Stat(fallback); statErr == nil && !stat.IsDir() {
				claudePath = fallback
			}
		}
	}
	if claudePath == "" {
		return nil, err
	}
	return exec.Command(claudePath, "auth", "status").Output()
}

func bootstrapApp() (*app, error) {
	resolver, err := runtime.New()
	if err != nil {
		return nil, fmt.Errorf("resolve instance root: %w", err)
	}
	if err := runtime.Bootstrap(resolver); err != nil {
		return nil, fmt.Errorf("bootstrap instance directory: %w", err)
	}

	if err := checkMigrationLock(resolver); err != nil {
		return nil, err
	}

	cfg, err := config.Load(resolver)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	setProviderEnv(cfg)

	// Check runtime dependencies before touching the bridge.
	checkResult := deps.CheckAll()
	for _, d := range checkResult.Deps {
		if d.Required && !d.Found {
			return nil, fmt.Errorf("%s is required but not found. Install: %s", d.Name, d.InstallURL)
		}
		if d.Required && d.Found && !d.VersionOK {
			return nil, fmt.Errorf("%s v%s found but >= %s required. Update: %s", d.Name, d.Version, d.MinVersion, d.InstallURL)
		}
		if !d.Required && !d.Found {
			log.Printf("Warning: %s not found — some features may be limited", d.Name)
		}
	}

	// Isolate PI agent directory to prevent credential conflicts with PI CLI.
	// The bridge will use ~/.aurelia/pi-agent/ instead of ~/.pi/agent/.
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		log.Printf("Warning: cannot determine home directory — PI_CODING_AGENT_DIR not set, bridge will use default ~/.pi/agent/")
	} else {
		if err := os.Setenv("PI_CODING_AGENT_DIR", filepath.Join(homeDir, ".aurelia", "pi-agent")); err != nil {
			return nil, fmt.Errorf("set PI_CODING_AGENT_DIR: %w", err)
		}
	}

	br := setupBridge()
	personaSvc := setupPersona(resolver)

	agentReg, err := agents.Load(resolver.Agents())
	if err != nil {
		log.Printf("Warning: failed to load agents registry: %v (continuing without agents)", err)
		agentReg = nil
	}

	cronStore, err := cron.NewSQLiteCronStore(resolver.DBPath("cron.db"))
	if err != nil {
		return nil, fmt.Errorf("initialize cron store: %w", err)
	}
	bindings, err := projectbinding.NewSQLiteStore(resolver.DBPath("project_bindings.db"))
	if err != nil {
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("initialize project binding store: %w", err)
	}

	runLogStore, err := runlog.NewSQLiteStore(resolver.DBPath("runlog.db"))
	if err != nil {
		if closeErr := bindings.Close(); closeErr != nil {
			log.Printf("Warning: failed to close project binding store: %v", closeErr)
		}
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("initialize runlog store: %w", err)
	}

	continuityStore, err := continuity.NewSQLiteStore(resolver.DBPath("continuity.db"))
	if err != nil {
		if closeErr := runLogStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close runlog store: %v", closeErr)
		}
		if closeErr := bindings.Close(); closeErr != nil {
			log.Printf("Warning: failed to close project binding store: %v", closeErr)
		}
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("initialize continuity store: %w", err)
	}

	onboardingStore, err := users.NewOnboardingStoreFromFile(resolver.DBPath("onboarding.db"))
	if err != nil {
		if closeErr := continuityStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close continuity store: %v", closeErr)
		}
		if closeErr := runLogStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close runlog store: %v", closeErr)
		}
		if closeErr := bindings.Close(); closeErr != nil {
			log.Printf("Warning: failed to close project binding store: %v", closeErr)
		}
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("initialize onboarding store: %w", err)
	}

	transcriber, err := buildTranscriber(cfg)
	if err != nil {
		if closeErr := onboardingStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close onboarding store: %v", closeErr)
		}
		if closeErr := continuityStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close continuity store: %v", closeErr)
		}
		if closeErr := runLogStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close runlog store: %v", closeErr)
		}
		if closeErr := bindings.Close(); closeErr != nil {
			log.Printf("Warning: failed to close project binding store: %v", closeErr)
		}
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("initialize transcriber: %w", err)
	}

	cronSvc := cron.NewService(cronStore, nil)
	cronHandler := telegram.NewCronCommandHandler(cronSvc)
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("Warning: failed to resolve executable path: %v", err)
	}
	sessions, err := session.NewPersistentStore(resolver.DBPath("sessions.json"))
	if err != nil {
		log.Printf("Warning: failed to load session snapshot, starting with empty sessions: %v", err)
		sessions = session.NewStore()
	}

	br.SetOnDeath(func() {
		log.Printf("bridge: process died, deactivating all sessions")
		sessions.DeactivateAll()
		if continuityStore != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := continuityStore.MarkColdForSessions(ctx, "bridge process died"); err != nil {
				log.Printf("Warning: failed to mark continuity cold after bridge death: %v", err)
			}
		}
	})

	bot, err := telegram.NewBotController(
		cfg, br, agentReg, personaSvc, transcriber,
		cronHandler, resolver.MemoryPersonas(), resolver.Memory(), exePath, sessions, resolver, bindings,
	)
	if err != nil {
		if closeErr := onboardingStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close onboarding store: %v", closeErr)
		}
		if closeErr := continuityStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close continuity store: %v", closeErr)
		}
		if closeErr := runLogStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close runlog store: %v", closeErr)
		}
		if closeErr := bindings.Close(); closeErr != nil {
			log.Printf("Warning: failed to close project binding store: %v", closeErr)
		}
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("initialize telegram bot: %w", err)
	}
	bot.SetRunLog(runLogStore)
	bot.SetContinuity(continuityStore)
	bot.SetOnboardingStore(onboardingStore)

	// Wire orchestrator — enables autonomous agent orchestration
	cwd, err := os.Getwd()
	if err != nil {
		if closeErr := onboardingStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close onboarding store: %v", closeErr)
		}
		if closeErr := continuityStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close continuity store: %v", closeErr)
		}
		if closeErr := runLogStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close runlog store: %v", closeErr)
		}
		if closeErr := bindings.Close(); closeErr != nil {
			log.Printf("Warning: failed to close project binding store: %v", closeErr)
		}
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("get current working directory: %w", err)
	}
	orch := orchestrator.NewOrchestrator(br, orchestrator.OrchestratorConfig{
		RepoRoot: cwd,
	})
	bot.SetOrchestrator(orch)

	// When the user changes the default model via /model, the live cfg has been
	// mutated already; re-export the provider env vars so the bridge picks up
	// the right API key on its next query.
	bot.SetProviderEnvRefresher(func() { setProviderEnv(cfg) })

	// Wire dreamer — background memory consolidation + nudge review.
	// Fall back to the user's default model so dream/nudge work on any provider
	// (avoids 402 errors when the hardcoded Anthropic models aren't available).
	dreamCfg := dream.DefaultConfig()
	dreamCfg.Provider = cfg.DefaultProvider
	userModel := cfg.DefaultModel
	if userModel != "" {
		dreamCfg.Model = userModel
		dreamCfg.ExtractModel = userModel
		dreamCfg.NudgeModel = userModel
	}
	if cfg.DreamModel != "" {
		dreamCfg.Model = cfg.DreamModel
	}
	if cfg.ExtractModel != "" {
		dreamCfg.ExtractModel = cfg.ExtractModel
	}
	if cfg.NudgeEnabled != nil {
		dreamCfg.NudgeEnabled = *cfg.NudgeEnabled
	}
	if cfg.NudgeTurns > 0 {
		dreamCfg.NudgeTurns = cfg.NudgeTurns
	}
	if cfg.NudgeModel != "" {
		dreamCfg.NudgeModel = cfg.NudgeModel
	}
	userResolver := users.NewResolver(resolver.Root())
	dreamer := dream.New(userResolver, resolver, br, dreamCfg)
	bot.SetDreamer(dreamer)

	// Wire project index for fast project lookup.
	if homeDir == "" {
		return nil, fmt.Errorf("cannot determine home directory for project index path")
	}
	jsonPath := runtime.PersistPath(filepath.Join(homeDir, ".aurelia"))
	projectIndex := runtime.NewProjectIndex(nil, jsonPath)
	bot.SetProjectIndex(projectIndex)
	go func() {
		rebuildCtx, rebuildCancel := context.WithCancel(context.Background())
		defer rebuildCancel()
		// Initial rebuild in background.
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		if err := projectIndex.Rebuild(rebuildCtx); err != nil {
			log.Printf("project index: initial rebuild error: %v", err)
		}
		for {
			select {
			case <-ticker.C:
				if err := projectIndex.Rebuild(rebuildCtx); err != nil {
					log.Printf("project index: rebuild error: %v", err)
				}
			case <-rebuildCtx.Done():
				return
			}
		}
	}()

	scheduler, err := setupCronScheduler(cronStore, br, agentReg, personaSvc, bot, resolver.Memory(), cfg.DefaultProvider, exePath, users.NewResolver(resolver.Root()))
	if err != nil {
		if closeErr := onboardingStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close onboarding store: %v", closeErr)
		}
		if closeErr := continuityStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close continuity store: %v", closeErr)
		}
		if closeErr := runLogStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close runlog store: %v", closeErr)
		}
		if closeErr := bindings.Close(); closeErr != nil {
			log.Printf("Warning: failed to close project binding store: %v", closeErr)
		}
		if closeErr := cronStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close cron store: %v", closeErr)
		}
		return nil, fmt.Errorf("initialize cron scheduler: %w", err)
	}

	cronCtx, cronCancel := context.WithCancel(context.Background())

	return &app{
		config:          cfg,
		resolver:        resolver,
		bridge:          br,
		agents:          agentReg,
		cronStore:       cronStore,
		bindings:        bindings,
		runLog:          runLogStore,
		continuity:      continuityStore,
		bot:             bot,
		sessions:        sessions,
		scheduler:       scheduler,
		cronCtx:         cronCtx,
		cronCancel:      cronCancel,
		onboardingStore: onboardingStore,
	}, nil
}

// setupBridge creates the Bridge, ensuring ~/.aurelia/bridge/ is bootstrapped.
func setupBridge() *bridge.Bridge {
	home, _ := os.UserHomeDir()
	aureliBridgeDir := filepath.Join(home, ".aurelia", "bridge")
	if _, setupErr := bridge.EnsureBridge(aureliBridgeDir, bridge.EmbeddedBundleJS); setupErr != nil {
		log.Printf("Warning: bridge auto-setup failed: %v", setupErr)
	}
	bridgeDir := findBridgeDir()
	if bridgeDir == "" {
		bridgeDir = aureliBridgeDir
	}
	bundlePath := filepath.Join(bridgeDir, "bundle.js")

	// minBundleSize guards against a truncated bundle.js (e.g. an aborted
	// esbuild run leaving an empty file). The real bundle is ~12 MB; below
	// this threshold we assume corruption and fall back to running tsx.
	const minBundleSize = 10 * 1024
	switch info, err := os.Stat(bundlePath); {
	case err != nil:
		// Missing bundle — let the bridge fall back to tsx.
		bundlePath = ""
	case info.Size() < minBundleSize:
		log.Printf("warning: bridge bundle.js exists but is only %d bytes (<%d); using tsx fallback", info.Size(), minBundleSize)
		bundlePath = ""
	}

	return bridge.New(bridgeDir, bundlePath)
}

// setupPersona builds the canonical identity service from persona and playbook files.
func setupPersona(resolver *runtime.PathResolver) *persona.CanonicalIdentityService {
	personasDir := resolver.MemoryPersonas()
	memoryDir := resolver.Memory()
	ownerPlaybookPath := filepath.Join(memoryDir, "OWNER_PLAYBOOK.md")
	lessonsLearnedPath := filepath.Join(memoryDir, "LESSONS_LEARNED.md")

	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("Warning: failed to resolve working directory for project playbook: %v", err)
		cwd = ""
	}
	if err := runtime.BootstrapProject(cwd); err != nil {
		log.Printf("Warning: failed to bootstrap project-local Aurelia directory: %v", err)
	}
	var projectPlaybookPath string
	if cwd != "" {
		projectPlaybookPath = filepath.Join(cwd, "docs", "PROJECT_PLAYBOOK.md")
	}

	return persona.NewCanonicalIdentityService(
		filepath.Join(personasDir, "IDENTITY.md"),
		filepath.Join(personasDir, "SOUL.md"),
		filepath.Join(personasDir, "USER.md"),
		ownerPlaybookPath,
		lessonsLearnedPath,
		projectPlaybookPath,
	)
}

func setupCronScheduler(
	cronStore *cron.SQLiteCronStore,
	br *bridge.Bridge,
	agentReg *agents.Registry,
	personaSvc *persona.CanonicalIdentityService,
	bot *telegram.BotController,
	memoryDir string,
	defaultProvider string,
	exePath string,
	userResolver *users.Resolver,
) (*cron.Scheduler, error) {
	if agentReg == nil {
		return nil, nil
	}

	cronRuntime := cron.NewBridgeCronRuntime(
		&cron.BridgeAdapter{B: br},
		agentReg,
		personaSvc,
		memoryDir,
		defaultProvider,
	)
	cronRuntime.SetExePath(exePath)
	if userResolver != nil {
		cronRuntime.SetUserResolver(userResolver)
	}

	delivery := cron.NewTelegramDelivery(bot.ChatSender())
	deliverFn := func(ctx context.Context, job cron.CronJob, result *cron.ExecutionResult, execErr error) error {
		return delivery.Deliver(ctx, job, result, execErr)
	}

	notifyingRuntime := cron.NewNotifyingRuntime(cronRuntime, deliverFn)
	scheduler, err := cron.NewScheduler(cronStore, notifyingRuntime, nil, cron.SchedulerConfig{
		PollInterval: 15 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	registerScheduledAgents(cronStore, agentReg)
	return scheduler, nil
}

func (a *app) start() {
	if a.scheduler != nil {
		go func() {
			if err := a.scheduler.Start(a.cronCtx); err != nil && err != context.Canceled {
				log.Printf("Warning: cron scheduler stopped with error: %v", err)
			}
		}()
	}
	a.startSessionGC()
	go a.bot.Start()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic in interrupted session notifier: %v", r)
			}
		}()
		time.Sleep(2 * time.Second)
		maxAge := a.bot.InterruptedSessionMaxAgeFromConfig()
		a.bot.NotifyRecentInterruptedSessions(maxAge)
	}()

	// Reconcile stale run_journal rows from before this process start
	go a.reconcileStaleRuns()
}

func (a *app) reconcileStaleRuns() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in reconcileStaleRuns: %v", r)
		}
	}()

	if a.runLog == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Mark continuity sessions as cold with deploy reason
	if a.continuity != nil {
		if err := a.continuity.MarkColdForSessions(ctx, "daemon restarted/deployed"); err != nil {
			log.Printf("Warning: failed to mark continuity cold on startup: %v", err)
		}
	}
}

func (a *app) startSessionGC() {
	if a.sessions == nil || a.config == nil {
		return
	}
	ttlHours := a.config.SessionTTLHours
	if ttlHours <= 0 {
		ttlHours = 168
	}
	maxAge := time.Duration(ttlHours) * time.Hour
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-a.cronCtx.Done():
				return
			case <-ticker.C:
				a.sessions.GC(maxAge)
			}
		}
	}()
}

func (a *app) shutdown(ctx context.Context) {
	if a.cronCancel != nil {
		a.cronCancel()
	}
	if a.bot != nil {
		done := make(chan struct{})
		go func() {
			a.bot.Stop()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			log.Println("Warning: bot shutdown timed out")
		}
	}
}

func (a *app) close() {
	if a.bridge != nil {
		a.bridge.Stop()
	}
	if a.cronStore != nil {
		if err := a.cronStore.Close(); err != nil {
			log.Printf("Warning: failed to close cron store: %v", err)
		}
	}
	if a.bindings != nil {
		if err := a.bindings.Close(); err != nil {
			log.Printf("Warning: failed to close project binding store: %v", err)
		}
	}
	if a.runLog != nil {
		if err := a.runLog.Close(); err != nil {
			log.Printf("Warning: failed to close runlog store: %v", err)
		}
	}
	if a.continuity != nil {
		if err := a.continuity.Close(); err != nil {
			log.Printf("Warning: failed to close continuity store: %v", err)
		}
	}
	if a.onboardingStore != nil {
		if err := a.onboardingStore.Close(); err != nil {
			log.Printf("Warning: failed to close onboarding store: %v", err)
		}
	}
}

// setProviderEnv exports provider credentials as env vars consumed by PI.
func setProviderEnv(cfg *config.AppConfig) {
	provider := config.NormalizeProvider(cfg.DefaultProvider)
	authMode := cfg.ProviderAuthMode(provider)

	// Subscription mode remains supported for Anthropic when the PI auth store is
	// already configured. We also keep the legacy Claude auth check as fallback.
	if provider == "anthropic" && authMode == "subscription" {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("ANTHROPIC_BASE_URL")
		if hasPIAuth("anthropic") {
			return
		}
		home, _ := os.UserHomeDir()
		if !hasClaudeSubscriptionAuth(home) {
			log.Fatalf("Anthropic subscription requires PI /login or Claude auth. Run 'pi /login' or 'claude auth login' first.")
		}
		return
	}

	apiKey := cfg.ProviderAPIKey(provider)
	if apiKey == "" {
		return
	}

	if envName := piProviderEnvName(provider); envName != "" {
		_ = os.Setenv(envName, apiKey)
	}
}

func piProviderEnvName(provider string) string {
	switch config.NormalizeProvider(provider) {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "kimi":
		return "KIMI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "zai":
		return "ZAI_API_KEY"
	case "google":
		return "GEMINI_API_KEY"
	case "opencode-go":
		return "OPENCODE_API_KEY"
	case "ollama":
		return "OLLAMA_API_KEY"
	default:
		return ""
	}
}

func hasPIAuth(provider string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	// Check isolated dir first, then fall back to PI CLI dir for users who
	// configured PI CLI after the initial one-time inheritance.
	candidates := []string{
		filepath.Join(home, ".aurelia", "pi-agent", "auth.json"),
		filepath.Join(home, ".pi", "agent", "auth.json"),
	}
	for _, path := range candidates {
		if providerInAuthFile(path, provider) {
			return true
		}
	}
	return false
}

func providerInAuthFile(path, provider string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return false
	}
	var auth map[string]any
	if err := json.Unmarshal(data, &auth); err != nil {
		return false
	}
	_, ok := auth[provider]
	return ok
}

func hasClaudeSubscriptionAuth(home string) bool {
	if home != "" {
		credPath := filepath.Join(home, ".claude", ".credentials.json")
		if stat, err := os.Stat(credPath); err == nil && !stat.IsDir() && stat.Size() > 0 {
			return true
		}
	}

	out, err := runClaudeAuthStatus()
	if err != nil {
		return false
	}

	var status struct {
		LoggedIn   bool   `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return false
	}
	return status.LoggedIn && status.AuthMethod == "claude.ai"
}

// findBridgeDir returns ~/.aurelia/bridge/ as the canonical bridge directory.
func findBridgeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aurelia", "bridge")
}

func buildTranscriber(cfg *config.AppConfig) (stt.Transcriber, error) {
	switch cfg.STTProvider {
	case "", "groq":
		return stt.NewGroqTranscriber(cfg.ProviderAPIKey("groq")), nil
	default:
		return nil, fmt.Errorf("unsupported stt provider %q", cfg.STTProvider)
	}
}

// registerScheduledAgents syncs agent schedules into the cron store.
// Uses a deterministic job ID derived from agent name so that restarts
// skip agents that already have a job registered (idempotent).
func registerScheduledAgents(store *cron.SQLiteCronStore, reg *agents.Registry) {
	if reg == nil {
		return
	}
	svc := cron.NewService(store, nil)
	for _, a := range reg.Scheduled() {
		jobID := "scheduled-agent-" + a.Name

		// Skip if a job with this ID already exists.
		existing, err := store.GetJob(context.Background(), jobID)
		if err != nil {
			log.Printf("Warning: failed to check existing job for agent %q: %v", a.Name, err)
			continue
		}
		if existing != nil {
			log.Printf("Scheduled agent %q already registered (job %s), skipping", a.Name, jobID)
			continue
		}

		_, err = svc.CreateJob(context.Background(), cron.CronJob{
			ID:           jobID,
			AgentName:    a.Name,
			ScheduleType: "cron",
			CronExpr:     a.Schedule,
			Prompt:       a.Prompt,
		})
		if err != nil {
			log.Printf("Warning: failed to register scheduled agent %q: %v", a.Name, err)
		}
	}
}
