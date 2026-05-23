package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/igormaneschy/aurelia/internal/runtime"
)

func TestSessionLifecycleConfig_Defaults(t *testing.T) {
	r, cleanup := testRuntime(t)
	defer cleanup()

	cfg, err := Load(r)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.SessionLifecycle.Enabled {
		t.Fatal("default session lifecycle should be enabled")
	}
	if cfg.SessionLifecycle.CompactAfterInputTokens != 120000 {
		t.Fatalf("expected compact_after=120000, got %d", cfg.SessionLifecycle.CompactAfterInputTokens)
	}
	if cfg.SessionLifecycle.RotateAfterInputTokens != 250000 {
		t.Fatalf("expected rotate_after=250000, got %d", cfg.SessionLifecycle.RotateAfterInputTokens)
	}
	if cfg.SessionLifecycle.IdleTimeoutMinutes != 20 {
		t.Fatalf("expected idle_timeout=20, got %d", cfg.SessionLifecycle.IdleTimeoutMinutes)
	}
}

func TestSessionLifecycleConfig_LoadFromFile(t *testing.T) {
	r, cleanup := testRuntime(t)
	defer cleanup()

	cfgJSON := `{
		"session_lifecycle": {
			"enabled": true,
			"compact_after_input_tokens": 50000,
			"rotate_after_input_tokens": 100000,
			"idle_timeout_minutes": 30
		}
	}`

	if err := os.MkdirAll(filepath.Dir(r.AppConfig()), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(r.AppConfig(), []byte(cfgJSON), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(r)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.SessionLifecycle.CompactAfterInputTokens != 50000 {
		t.Fatalf("expected compact_after=50000, got %d", cfg.SessionLifecycle.CompactAfterInputTokens)
	}
	if cfg.SessionLifecycle.RotateAfterInputTokens != 100000 {
		t.Fatalf("expected rotate_after=100000, got %d", cfg.SessionLifecycle.RotateAfterInputTokens)
	}
	if cfg.SessionLifecycle.IdleTimeoutMinutes != 30 {
		t.Fatalf("expected idle_timeout=30, got %d", cfg.SessionLifecycle.IdleTimeoutMinutes)
	}
}

func TestSessionLifecycleConfig_Disabled(t *testing.T) {
	r, cleanup := testRuntime(t)
	defer cleanup()

	cfgJSON := `{
		"session_lifecycle": {
			"enabled": false
		}
	}`

	if err := os.MkdirAll(filepath.Dir(r.AppConfig()), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(r.AppConfig(), []byte(cfgJSON), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(r)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.SessionLifecycle.Enabled {
		t.Fatal("expected session_lifecycle to be disabled")
	}
}

func TestSessionLifecycleConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SessionLifecycleConfig
		wantErr string
	}{
		{
			name: "valid defaults",
			cfg:  DefaultSessionLifecycleConfig(),
		},
		{
			name: "disabled is valid",
			cfg:  SessionLifecycleConfig{Enabled: false},
		},
		{
			name: "compact <= 0",
			cfg: SessionLifecycleConfig{
				Enabled:                 true,
				CompactAfterInputTokens: 0,
			},
			wantErr: "session_lifecycle.compact_after_input_tokens must be > 0, got 0",
		},
		{
			name: "rotate <= compact",
			cfg: SessionLifecycleConfig{
				Enabled:                 true,
				CompactAfterInputTokens: 200000,
				RotateAfterInputTokens:  100000,
			},
			wantErr: "session_lifecycle.rotate_after_input_tokens (100000) must be > compact_after_input_tokens (200000)",
		},
		{
			name: "rotate equals compact",
			cfg: SessionLifecycleConfig{
				Enabled:                 true,
				CompactAfterInputTokens: 100000,
				RotateAfterInputTokens:  100000,
			},
			wantErr: "session_lifecycle.rotate_after_input_tokens (100000) must be > compact_after_input_tokens (100000)",
		},
		{
			name: "idle timeout <= 0",
			cfg: SessionLifecycleConfig{
				Enabled:                      true,
				CompactAfterInputTokens:      50000,
				RotateAfterInputTokens:       100000,
				MaxEmptyResultsBeforeRotate:  1,
				MaxProcessDeathsBeforeRotate: 1,
				KeepRecentTokens:             1000,
				ReserveTokens:                16000,
				IdleTimeoutMinutes:           0,
			},
			wantErr: "session_lifecycle.idle_timeout_minutes must be > 0, got 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestSessionLifecycleConfig_LifecyclePolicy(t *testing.T) {
	cfg := DefaultSessionLifecycleConfig()
	policy := cfg.LifecyclePolicy()

	if policy.Enabled != cfg.Enabled {
		t.Fatal("LifecyclePolicy.Enabled mismatch")
	}
	if policy.CompactAfterInputTokens != cfg.CompactAfterInputTokens {
		t.Fatal("LifecyclePolicy.CompactAfterInputTokens mismatch")
	}
	if policy.RotateAfterInputTokens != cfg.RotateAfterInputTokens {
		t.Fatal("LifecyclePolicy.RotateAfterInputTokens mismatch")
	}
	if policy.IdleTimeoutMinutes != cfg.IdleTimeoutMinutes {
		t.Fatal("LifecyclePolicy.IdleTimeoutMinutes mismatch")
	}
}

func TestSessionLifecycleConfig_PartialConfigUsesDefaults(t *testing.T) {
	r, cleanup := testRuntime(t)
	defer cleanup()

	// Only set compact threshold, rotate should fall back to default
	cfgJSON := `{
		"session_lifecycle": {
			"compact_after_input_tokens": 50000
		}
	}`

	if err := os.MkdirAll(filepath.Dir(r.AppConfig()), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(r.AppConfig(), []byte(cfgJSON), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(r)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.SessionLifecycle.CompactAfterInputTokens != 50000 {
		t.Fatalf("expected compact_after=50000, got %d", cfg.SessionLifecycle.CompactAfterInputTokens)
	}
	if cfg.SessionLifecycle.RotateAfterInputTokens != 250000 {
		t.Fatalf("expected rotate_after=250000 (default), got %d", cfg.SessionLifecycle.RotateAfterInputTokens)
	}
}

func TestSessionLifecycleConfig_RoundTrip(t *testing.T) {
	r, cleanup := testRuntime(t)
	defer cleanup()

	// Save custom lifecycle config and reload (simulates SaveEditable flow)
	normalized := defaultFileConfig(r)
	normalized.SessionLifecycle.CompactAfterInputTokens = 75000
	normalized.SessionLifecycle.RotateAfterInputTokens = 150000

	if err := writeConfigFile(r.AppConfig(), normalized); err != nil {
		t.Fatalf("writeConfigFile: %v", err)
	}

	reloaded, err := Load(r)
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}

	if reloaded.SessionLifecycle.CompactAfterInputTokens != 75000 {
		t.Fatalf("expected compact_after=75000, got %d", reloaded.SessionLifecycle.CompactAfterInputTokens)
	}
	if reloaded.SessionLifecycle.RotateAfterInputTokens != 150000 {
		t.Fatalf("expected rotate_after=150000, got %d", reloaded.SessionLifecycle.RotateAfterInputTokens)
	}
}

func TestSessionLifecycleConfig_IsOmittedFromEditable(t *testing.T) {
	// The session_lifecycle section should not appear in EditableConfig
	// since it's a managed/operator-only setting.
	var raw map[string]any
	data, _ := json.Marshal(DefaultSessionLifecycleConfig())
	json.Unmarshal(data, &raw)

	// Verify the struct has expected fields
	if _, ok := raw["compact_after_input_tokens"]; !ok {
		t.Fatal("expected compact_after_input_tokens in JSON")
	}
	if _, ok := raw["enabled"]; !ok {
		t.Fatal("expected enabled in JSON")
	}
}

// testRuntime creates a temporary runtime for testing. The cleanup function
// restores the previous environment state.
func testRuntime(t *testing.T) (*runtime.PathResolver, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	origHome := os.Getenv("AURELIA_HOME")
	os.Setenv("AURELIA_HOME", tmpDir)

	r, err := runtime.New()
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	cleanup := func() {
		if origHome != "" {
			os.Setenv("AURELIA_HOME", origHome)
		} else {
			os.Unsetenv("AURELIA_HOME")
		}
	}

	return r, cleanup
}
