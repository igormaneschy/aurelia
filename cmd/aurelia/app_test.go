package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/igormaneschy/aurelia/internal/tuisessions"
)

// TestMain isolates the suite from ambient provider API-key and Telegram
// token environment overrides. config.Load() applies these overrides from
// the process environment (config.applyEnvOverrides), so tests must not
// depend on what the developer's shell exports (e.g. OPENAI_API_KEY,
// ZAI_API_KEY). The list mirrors config.knownEnvProviders() in
// internal/config/config.go — keep both in sync when providers are added.
func TestMain(m *testing.M) {
	for _, name := range []string{
		"ANTHROPIC_API_KEY",
		"GOOGLE_API_KEY",
		"OPENAI_API_KEY",
		"OPENCODE_GO_API_KEY",
		"KIMI_API_KEY",
		"KIMI_CODING_API_KEY",
		"OPENROUTER_API_KEY",
		"ZAI_API_KEY",
		"ALIBABA_API_KEY",
		"OLLAMA_API_KEY",
		"GROQ_API_KEY",
		"TELEGRAM_BOT_TOKEN",
	} {
		if err := os.Unsetenv(name); err != nil {
			panic("cmd/aurelia test: unsetenv " + name + ": " + err.Error())
		}
	}
	os.Exit(m.Run())
}

// storeCloser is a small helper interface for the tuiSessionsStore cleanup
// defer pattern used in bootstrapApp. A storeCloser wraps a store and returns
// a cleanup function that closes it when the *bool flag is false.
type storeCloser struct {
	store *tuisessions.SQLiteStore
}

// deferCleanup returns a function that closes the store unless ok is set true.
// Usage:
//
//	store, err := tuisessions.NewSQLiteStore(path)
//	sc := &storeCloser{store: store}
//	var ok bool
//	defer sc.deferCleanup(&ok)
//	// ... bootstrap steps that may fail ...
//	ok = true
func (sc *storeCloser) deferCleanup(ok *bool) func() {
	return func() {
		if !*ok {
			_ = sc.store.Close()
		}
	}
}

func TestStoreCloser_CleanupOnFalse(t *testing.T) {
	store, err := tuisessions.NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	sc := &storeCloser{store: store}
	var ok bool
	cleanup := sc.deferCleanup(&ok)

	// Simulate error path — ok stays false.
	cleanup()

	// Verify store is closed: operations should fail.
	_, err = store.List(context.Background())
	if err == nil {
		t.Error("expected List to fail after Close")
	}
}

func TestStoreCloser_SkipOnTrue(t *testing.T) {
	store, err := tuisessions.NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	sc := &storeCloser{store: store}
	var ok bool
	cleanup := sc.deferCleanup(&ok)

	// Simulate success path.
	ok = true
	cleanup()

	// Store should still be usable.
	_, err = store.List(context.Background())
	if err != nil {
		t.Errorf("expected List to succeed after cleanup with ok=true: %v", err)
	}
	_ = store.Close()
}

func TestHasClaudeSubscriptionAuth_AcceptsLegacyCredentialsFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("create claude dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	if !hasClaudeSubscriptionAuth(home) {
		t.Fatal("expected legacy credentials file to be accepted")
	}
}

func TestHasClaudeSubscriptionAuth_AcceptsClaudeAIAuthStatus(t *testing.T) {
	original := runClaudeAuthStatus
	t.Cleanup(func() { runClaudeAuthStatus = original })
	runClaudeAuthStatus = func() ([]byte, error) {
		return []byte(`{"loggedIn":true,"authMethod":"claude.ai"}`), nil
	}

	if !hasClaudeSubscriptionAuth(t.TempDir()) {
		t.Fatal("expected claude.ai auth status to be accepted")
	}
}

func TestHasClaudeSubscriptionAuth_RejectsLoggedOutStatus(t *testing.T) {
	original := runClaudeAuthStatus
	t.Cleanup(func() { runClaudeAuthStatus = original })
	runClaudeAuthStatus = func() ([]byte, error) {
		return []byte(`{"loggedIn":false,"authMethod":"none"}`), nil
	}

	if hasClaudeSubscriptionAuth(t.TempDir()) {
		t.Fatal("expected logged out auth status to be rejected")
	}
}
