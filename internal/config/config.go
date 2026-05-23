package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/security"
	"github.com/igormaneschy/aurelia/internal/session"
)

// NormalizeProvider returns a canonical lowercase provider name.
func NormalizeProvider(provider string) string {
	normalized := strings.TrimSpace(strings.ToLower(provider))
	if normalized == "" {
		return "kimi"
	}
	return normalized
}

// defaultModelForProvider returns the default model for the given provider.
func defaultModelForProvider(provider string) string {
	switch NormalizeProvider(provider) {
	case "anthropic":
		return "claude-sonnet-4-6"
	case "google":
		return "gemini-2.5-pro"
	case "opencode-go":
		return "openai/gpt-5.4"
	case "openrouter":
		return "openrouter/auto"
	case "zai":
		return "glm-5"
	case "alibaba":
		return "qwen3-coder-plus"
	case "ollama":
		return "llama3.1:8b"
	default:
		return "kimi-k2-thinking"
	}
}

const (
	defaultMaxIterations = 500
	// PI SDK auto-compacts sessions near model context limits (~180K for 200K
	// window models); this threshold is a safety net only, not primary management.
	defaultMaxSessionTokens = 180000
	defaultMaxImageBytes    = 10 * 1024 * 1024
	defaultSessionTTLHours  = 168
	defaultLLMProvider      = "kimi"
	defaultLLMModel         = "kimi-k2-thinking"
	defaultSTTProvider      = "groq"
)

// ProviderConfig holds credentials and endpoint for a single LLM provider.
type ProviderConfig struct {
	APIKey   string `json:"api_key"`
	BaseURL  string `json:"base_url,omitempty"`
	AuthMode string `json:"auth_mode,omitempty"`
}

// SessionLifecycleConfig controls automatic session health management.
type SessionLifecycleConfig struct {
	Enabled                      bool `json:"enabled"`
	CompactAfterInputTokens      int  `json:"compact_after_input_tokens"`
	RotateAfterInputTokens       int  `json:"rotate_after_input_tokens"`
	MaxEmptyResultsBeforeRotate  int  `json:"max_empty_results_before_rotate"`
	MaxProcessDeathsBeforeRotate int  `json:"max_process_deaths_before_rotate"`
	IdleTimeoutMinutes           int  `json:"idle_timeout_minutes"`
	InterruptedSessionMaxAgeMinutes int `json:"interrupted_session_max_age_minutes,omitempty"`
	KeepRecentTokens             int  `json:"keep_recent_tokens"`
	ReserveTokens                int  `json:"reserve_tokens"`
}

// DefaultSessionLifecycleConfig returns safe defaults for session lifecycle.
func DefaultSessionLifecycleConfig() SessionLifecycleConfig {
	return SessionLifecycleConfig{
		Enabled:                         true,
		CompactAfterInputTokens:         120000,
		RotateAfterInputTokens:          250000,
		MaxEmptyResultsBeforeRotate:     1,
		MaxProcessDeathsBeforeRotate:    1,
		IdleTimeoutMinutes:              20,
		InterruptedSessionMaxAgeMinutes: 1,
		KeepRecentTokens:                8000,
		ReserveTokens:                   32768,
	}
}

// Validate checks that thresholds are consistent. Returns a descriptive error
// with the offending value if invalid.
func (c SessionLifecycleConfig) Validate() error {
	if c.Enabled {
		if c.CompactAfterInputTokens <= 0 {
			return fmt.Errorf("session_lifecycle.compact_after_input_tokens must be > 0, got %d", c.CompactAfterInputTokens)
		}
		if c.RotateAfterInputTokens <= 0 {
			return fmt.Errorf("session_lifecycle.rotate_after_input_tokens must be > 0, got %d", c.RotateAfterInputTokens)
		}
		if c.RotateAfterInputTokens <= c.CompactAfterInputTokens {
			return fmt.Errorf(
				"session_lifecycle.rotate_after_input_tokens (%d) must be > compact_after_input_tokens (%d)",
				c.RotateAfterInputTokens, c.CompactAfterInputTokens,
			)
		}
		if c.MaxEmptyResultsBeforeRotate <= 0 {
			return fmt.Errorf("session_lifecycle.max_empty_results_before_rotate must be > 0, got %d", c.MaxEmptyResultsBeforeRotate)
		}
		if c.MaxProcessDeathsBeforeRotate <= 0 {
			return fmt.Errorf("session_lifecycle.max_process_deaths_before_rotate must be > 0, got %d", c.MaxProcessDeathsBeforeRotate)
		}
		if c.IdleTimeoutMinutes <= 0 {
			return fmt.Errorf("session_lifecycle.idle_timeout_minutes must be > 0, got %d", c.IdleTimeoutMinutes)
		}
		if c.KeepRecentTokens <= 0 {
			return fmt.Errorf("session_lifecycle.keep_recent_tokens must be > 0, got %d", c.KeepRecentTokens)
		}
		if c.ReserveTokens <= 0 {
			return fmt.Errorf("session_lifecycle.reserve_tokens must be > 0, got %d", c.ReserveTokens)
		}
	}
	return nil
}

// LifecyclePolicy converts the config to the session package's policy type.
func (c SessionLifecycleConfig) LifecyclePolicy() session.LifecyclePolicy {
	return session.LifecyclePolicy{
		Enabled:                      c.Enabled,
		CompactAfterInputTokens:      c.CompactAfterInputTokens,
		RotateAfterInputTokens:       c.RotateAfterInputTokens,
		MaxEmptyResultsBeforeRotate:  c.MaxEmptyResultsBeforeRotate,
		MaxProcessDeathsBeforeRotate: c.MaxProcessDeathsBeforeRotate,
		IdleTimeoutMinutes:           c.IdleTimeoutMinutes,
		KeepRecentTokens:             c.KeepRecentTokens,
		ReserveTokens:                c.ReserveTokens,
	}
}

// AppConfig holds all runtime configuration needed for the application.
type AppConfig struct {
	DefaultProvider string                    `json:"default_provider"`
	DefaultModel    string                    `json:"default_model"`
	Providers       map[string]ProviderConfig `json:"providers"`

	TelegramBotToken        string  `json:"telegram_bot_token"`
	TelegramAllowedUserIDs  []int64 `json:"telegram_allowed_user_ids"`
	TelegramAllowedGroupIDs []int64 `json:"telegram_allowed_group_ids"`

	STTProvider string `json:"stt_provider"`

	MaxIterations    int    `json:"max_iterations"`
	MaxSessionTokens int    `json:"max_session_tokens"`
	MaxImageBytes    int    `json:"max_image_bytes,omitempty"`
	SessionTTLHours  int    `json:"session_ttl_hours,omitempty"`
	DBPath           string `json:"db_path"`
	MCPConfigPath    string `json:"mcp_servers_config_path"`

	DreamModel   string `json:"dream_model,omitempty"`
	ExtractModel string `json:"extract_model,omitempty"`

	NudgeEnabled *bool  `json:"nudge_enabled,omitempty"` // nil = default true
	NudgeTurns   int    `json:"nudge_turns,omitempty"`
	NudgeModel   string `json:"nudge_model,omitempty"`

	VisionModel    string `json:"vision_model,omitempty"`
	VisionProvider string `json:"vision_provider,omitempty"`
	LogLevel       string `json:"log_level,omitempty"`
	LogFormat      string `json:"log_format,omitempty"`

	// DiskScanEnabled toggles the fallback that walks $HOME (and /media, /mnt)
	// looking for a directory whose name matches a word in the user's first
	// message. Disabled by default — adds up to 3s of latency on session start
	// and the projectIndex already covers the common cases. Opt-in for users
	// who really want fuzzy disk discovery.
	DiskScanEnabled bool `json:"disk_scan_enabled,omitempty"`

	// SecurityConfig governs capability profiles, tool policies, and audit mode.
	SecurityConfig security.SecurityConfig `json:"security,omitempty"`

	// SummaryInterval controls how many successful turns between progressive
	// LLM summarization for continuity. 0 disables summarization (default 5).
	SummaryInterval int `json:"summary_interval,omitempty"`

	// DefaultOwnerUserID is the fallback user ID when none is explicitly provided.
	DefaultOwnerUserID int64 `json:"default_owner_user_id,omitempty"`

	// SessionLifecycleConfig controls automatic session health management.
	SessionLifecycle SessionLifecycleConfig `json:"session_lifecycle,omitempty"`
}

// DefaultOwnerUserIDOrFallback returns the configured DefaultOwnerUserID, or the
// first entry in TelegramAllowedUserIDs if DefaultOwnerUserID is zero and the
// whitelist is non-empty, or 0 if neither is set.
func (c *AppConfig) DefaultOwnerUserIDOrFallback() int64 {
	if c.DefaultOwnerUserID != 0 {
		return c.DefaultOwnerUserID
	}
	if len(c.TelegramAllowedUserIDs) > 0 {
		return c.TelegramAllowedUserIDs[0]
	}
	return 0
}

// IsModelAuto returns true when Aurelia should let PI choose the default model.
func (c *AppConfig) IsModelAuto() bool {
	if c == nil {
		return true
	}
	return strings.TrimSpace(c.DefaultProvider) == "" && strings.TrimSpace(c.DefaultModel) == ""
}

// ModelDisplayName returns the user-facing model label.
func (c *AppConfig) ModelDisplayName() string {
	if c.IsModelAuto() {
		return "PI default"
	}
	return c.DefaultModel
}

// VisionFallback returns the configured vision model and provider for image inputs.
// Returns empty strings when no vision fallback is configured.
func (c *AppConfig) VisionFallback() (model, provider string) {
	return c.VisionModel, c.VisionProvider
}

// Onboarded returns true if the config has the minimum required fields to run.
func (c *AppConfig) Onboarded() bool {
	return c.TelegramBotToken != "" && len(c.TelegramAllowedUserIDs) > 0 && (c.DefaultProvider != "" || c.IsModelAuto())
}

// Editable returns a mutable copy of the user-editable configuration subset.
func (c *AppConfig) Editable() *EditableConfig {
	return appConfigToEditable(c)
}

// ProviderAPIKey returns the API key for the given provider, or empty string.
func (c *AppConfig) ProviderAPIKey(provider string) string {
	p, ok := c.Providers[NormalizeProvider(provider)]
	if !ok {
		return ""
	}
	return p.APIKey
}

// ProviderBaseURL returns the base URL for the given provider, or empty string.
func (c *AppConfig) ProviderBaseURL(provider string) string {
	p, ok := c.Providers[NormalizeProvider(provider)]
	if !ok {
		return ""
	}
	return p.BaseURL
}

// ProviderAuthMode returns the auth mode for the given provider, or empty string.
func (c *AppConfig) ProviderAuthMode(provider string) string {
	p, ok := c.Providers[NormalizeProvider(provider)]
	if !ok {
		return ""
	}
	return p.AuthMode
}

// fileConfig is the JSON structure written to disk (new schema).
type fileConfig struct {
	DefaultProvider string                    `json:"default_provider"`
	DefaultModel    string                    `json:"default_model"`
	Providers       map[string]ProviderConfig `json:"providers"`

	TelegramBotToken        string  `json:"telegram_bot_token"`
	TelegramAllowedUserIDs  []int64 `json:"telegram_allowed_user_ids"`
	TelegramAllowedGroupIDs []int64 `json:"telegram_allowed_group_ids"`

	STTProvider string `json:"stt_provider"`

	MaxIterations    int    `json:"max_iterations"`
	MaxSessionTokens int    `json:"max_session_tokens"`
	MaxImageBytes    int    `json:"max_image_bytes,omitempty"`
	SessionTTLHours  int    `json:"session_ttl_hours,omitempty"`
	DBPath           string `json:"db_path"`
	MCPConfigPath    string `json:"mcp_servers_config_path"`

	VisionModel     string                  `json:"vision_model,omitempty"`
	VisionProvider  string                  `json:"vision_provider,omitempty"`
	LogLevel        string                  `json:"log_level,omitempty"`
	LogFormat       string                  `json:"log_format,omitempty"`
	DiskScanEnabled bool                    `json:"disk_scan_enabled,omitempty"`
	SecurityConfig  security.SecurityConfig `json:"security,omitempty"`
	SummaryInterval int                     `json:"summary_interval,omitempty"`

	DefaultOwnerUserID int64 `json:"default_owner_user_id"`

	// SessionLifecycleConfig controls automatic session health management.
	SessionLifecycle SessionLifecycleConfig `json:"session_lifecycle,omitempty"`
}

// ValidateConfig checks the full fileConfig for consistency.
// Returns nil if valid, or an error describing the first issue.
func (f fileConfig) ValidateConfig() error {
	return f.SessionLifecycle.Validate()
}

// Load reads the instance-local JSON config, creates it with defaults when
// missing, and returns the normalized runtime config.
func Load(r *runtime.PathResolver) (*AppConfig, error) {
	path := r.AppConfig()
	defaults := defaultFileConfig(r)

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			if err := writeConfigFile(path, defaults); err != nil {
				return nil, err
			}
			return toAppConfig(defaults), nil
		}
		return nil, fmt.Errorf("stat app config: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read app config: %w", err)
	}

	cfg := defaults
	if len(data) != 0 {
		// Try new schema first
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("decode app config: %w", err)
		}

		// Detect legacy format: if providers map is empty and the JSON contains
		// legacy-specific field names (llm_provider, llm_model, or inline API keys).
		if len(cfg.Providers) == 0 {
			var legacy legacyFileConfig
			if err := json.Unmarshal(data, &legacy); err == nil && legacy.LLMProvider != "" {
				// Preserve any new-format fields not present in the legacy struct.
				migrated := migrateLegacy(legacy)
				migrated.DefaultOwnerUserID = cfg.DefaultOwnerUserID
				cfg = migrated
			}
		}
	}

	normalized := normalizeFileConfig(cfg, r, hasExplicitAutoModel(data))
	if !sameFileConfig(normalized, cfg) {
		if err := writeConfigFile(path, normalized); err != nil {
			return nil, err
		}
	}

	return toAppConfig(normalized), nil
}

func defaultFileConfig(r *runtime.PathResolver) fileConfig {
	return fileConfig{
		DefaultProvider:         defaultLLMProvider,
		DefaultModel:            defaultModelForProvider(defaultLLMProvider),
		Providers:               map[string]ProviderConfig{},
		STTProvider:             defaultSTTProvider,
		TelegramAllowedUserIDs:  []int64{},
		TelegramAllowedGroupIDs: []int64{},
		MaxIterations:           defaultMaxIterations,
		MaxSessionTokens:        defaultMaxSessionTokens,
		MaxImageBytes:           defaultMaxImageBytes,
		SessionTTLHours:         defaultSessionTTLHours,
		DBPath:                  filepath.Join(r.Data(), "aurelia.db"),
		MCPConfigPath:           filepath.Join(r.Config(), "mcp_servers.json"),
		SecurityConfig:          security.DefaultConfig(),
		SessionLifecycle:        DefaultSessionLifecycleConfig(),
	}
}

func normalizeFileConfig(cfg fileConfig, r *runtime.PathResolver, preserveAutoModel bool) fileConfig {
	defaults := defaultFileConfig(r)
	if preserveAutoModel {
		cfg.DefaultProvider = ""
		cfg.DefaultModel = ""
	}
	// Copy security config defaults if not set
	if cfg.SecurityConfig.Mode == "" {
		cfg.SecurityConfig = defaults.SecurityConfig
	}
	if cfg.TelegramAllowedUserIDs == nil {
		cfg.TelegramAllowedUserIDs = defaults.TelegramAllowedUserIDs
	}
	if cfg.TelegramAllowedGroupIDs == nil {
		cfg.TelegramAllowedGroupIDs = defaults.TelegramAllowedGroupIDs
	}
	if cfg.DefaultOwnerUserID == 0 && len(cfg.TelegramAllowedUserIDs) > 0 {
		cfg.DefaultOwnerUserID = cfg.TelegramAllowedUserIDs[0]
	}
	if !preserveAutoModel && cfg.DefaultProvider == "" {
		cfg.DefaultProvider = defaults.DefaultProvider
	}
	if !preserveAutoModel && cfg.DefaultModel == "" {
		cfg.DefaultModel = defaultModelForProvider(cfg.DefaultProvider)
	}
	if cfg.STTProvider == "" {
		cfg.STTProvider = defaults.STTProvider
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaults.MaxIterations
	}
	if cfg.DBPath == "" {
		cfg.DBPath = defaults.DBPath
	}
	if cfg.MaxSessionTokens <= 0 {
		cfg.MaxSessionTokens = defaults.MaxSessionTokens
	}
	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = defaults.MaxImageBytes
	}
	if cfg.SessionTTLHours <= 0 {
		cfg.SessionTTLHours = defaults.SessionTTLHours
	}
	if cfg.MCPConfigPath == "" {
		cfg.MCPConfigPath = defaults.MCPConfigPath
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	// Apply lifecycle defaults if not set (zero values = use default)
	if !cfg.SessionLifecycle.Enabled && cfg.SessionLifecycle == (SessionLifecycleConfig{}) {
		cfg.SessionLifecycle = defaults.SessionLifecycle
	} else if cfg.SessionLifecycle.CompactAfterInputTokens <= 0 {
		cfg.SessionLifecycle.CompactAfterInputTokens = defaults.SessionLifecycle.CompactAfterInputTokens
		cfg.SessionLifecycle.RotateAfterInputTokens = defaults.SessionLifecycle.RotateAfterInputTokens
	} else if cfg.SessionLifecycle.RotateAfterInputTokens <= 0 {
		cfg.SessionLifecycle.RotateAfterInputTokens = defaults.SessionLifecycle.RotateAfterInputTokens
	}
	if cfg.SessionLifecycle.MaxEmptyResultsBeforeRotate <= 0 {
		cfg.SessionLifecycle.MaxEmptyResultsBeforeRotate = defaults.SessionLifecycle.MaxEmptyResultsBeforeRotate
	}
	if cfg.SessionLifecycle.MaxProcessDeathsBeforeRotate <= 0 {
		cfg.SessionLifecycle.MaxProcessDeathsBeforeRotate = defaults.SessionLifecycle.MaxProcessDeathsBeforeRotate
	}
	if cfg.SessionLifecycle.IdleTimeoutMinutes <= 0 {
		cfg.SessionLifecycle.IdleTimeoutMinutes = defaults.SessionLifecycle.IdleTimeoutMinutes
	}
	if cfg.SessionLifecycle.KeepRecentTokens <= 0 {
		cfg.SessionLifecycle.KeepRecentTokens = defaults.SessionLifecycle.KeepRecentTokens
	}
	if cfg.SessionLifecycle.ReserveTokens <= 0 {
		cfg.SessionLifecycle.ReserveTokens = defaults.SessionLifecycle.ReserveTokens
	}
	if cfg.SessionLifecycle.InterruptedSessionMaxAgeMinutes <= 0 {
		cfg.SessionLifecycle.InterruptedSessionMaxAgeMinutes = defaults.SessionLifecycle.InterruptedSessionMaxAgeMinutes
	}
	return cfg
}

func hasExplicitAutoModel(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	var raw map[string]*json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	providerRaw, hasProvider := raw["default_provider"]
	modelRaw, hasModel := raw["default_model"]
	if !hasProvider || !hasModel || providerRaw == nil || modelRaw == nil {
		return false
	}
	var provider, model string
	if err := json.Unmarshal(*providerRaw, &provider); err != nil {
		return false
	}
	if err := json.Unmarshal(*modelRaw, &model); err != nil {
		return false
	}
	return provider == "" && model == ""
}

func writeConfigFile(path string, cfg fileConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create app config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode app config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write app config: %w", err)
	}
	return nil
}

// SetDefaultOwnerUserID reads the config file at r.AppConfig(), sets the
// default_owner_user_id field, and writes it back atomically (temp + rename).
// The enclosing directory must already exist (e.g. after runtime.Bootstrap).
//
// Uses raw JSON manipulation to avoid deserializing into fileConfig and
// re-serializing with all-zero maps that could trigger legacy config detection.
func SetDefaultOwnerUserID(r *runtime.PathResolver, userID int64) error {
	path := r.AppConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read app config: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse app config: %w", err)
	}
	raw["default_owner_user_id"] = userID
	data, err = json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encode app config: %w", err)
	}
	data = append(data, '\n')

	// Atomic write: temp file + rename to prevent partial writes on crash.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write app config: %w", err)
	}
	return os.Rename(tmpPath, path)
}

func toAppConfig(cfg fileConfig) *AppConfig {
	// Normalize provider keys once so lookups don't need repeated normalization.
	normalized := make(map[string]ProviderConfig, len(cfg.Providers))
	for name, pc := range cfg.Providers {
		normalized[NormalizeProvider(name)] = pc
	}
	app := &AppConfig{
		DefaultProvider:         cfg.DefaultProvider,
		DefaultModel:            cfg.DefaultModel,
		Providers:               normalized,
		TelegramBotToken:        cfg.TelegramBotToken,
		TelegramAllowedUserIDs:  cfg.TelegramAllowedUserIDs,
		TelegramAllowedGroupIDs: cfg.TelegramAllowedGroupIDs,
		STTProvider:             cfg.STTProvider,
		MaxIterations:           cfg.MaxIterations,
		MaxSessionTokens:        cfg.MaxSessionTokens,
		MaxImageBytes:           cfg.MaxImageBytes,
		SessionTTLHours:         cfg.SessionTTLHours,
		DBPath:                  cfg.DBPath,
		MCPConfigPath:           cfg.MCPConfigPath,
		VisionModel:             cfg.VisionModel,
		VisionProvider:          cfg.VisionProvider,
		LogLevel:                cfg.LogLevel,
		LogFormat:               cfg.LogFormat,
		DiskScanEnabled:         cfg.DiskScanEnabled,
		SecurityConfig:          cfg.SecurityConfig,
		SummaryInterval:         cfg.SummaryInterval,
		DefaultOwnerUserID:      cfg.DefaultOwnerUserID,
		SessionLifecycle:        cfg.SessionLifecycle,
	}
	applyEnvOverrides(app)
	return app
}

func applyEnvOverrides(cfg *AppConfig) {
	if cfg == nil {
		return
	}
	if token := secretEnv("TELEGRAM_BOT_TOKEN"); token != "" {
		cfg.TelegramBotToken = token
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	for _, provider := range knownEnvProviders() {
		applyProviderEnvOverride(cfg, provider)
	}
	for provider := range cfg.Providers {
		applyProviderEnvOverride(cfg, provider)
	}
}

func knownEnvProviders() []string {
	return []string{
		"anthropic",
		"google",
		"openai",
		"opencode-go",
		"kimi",
		"kimi-coding",
		"openrouter",
		"zai",
		"alibaba",
		"ollama",
		"groq",
	}
}

func applyProviderEnvOverride(cfg *AppConfig, provider string) {
	key := secretEnv(providerAPIKeyEnv(provider))
	if key == "" {
		return
	}
	name := NormalizeProvider(provider)
	providerConfig := cfg.Providers[name]
	providerConfig.APIKey = key
	cfg.Providers[name] = providerConfig
}

func providerAPIKeyEnv(provider string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(provider)) {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_") + "_API_KEY"
}

func secretEnv(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}
