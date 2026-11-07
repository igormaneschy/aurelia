package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/users"
)

func TestEffectiveCwd_UsesPersistedBindingWithTopicFallback(t *testing.T) {
	store, err := projectbinding.NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := t.Context()
	if err := store.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 42, ThreadID: 0},
		CWD:         "/repo/group",
		ProjectSlug: "-repo-group",
		Source:      projectbinding.BindingManual,
	}); err != nil {
		t.Fatal(err)
	}

	bc := &Service{bindings: store, sessions: session.NewStore()}
	if got := bc.effectiveCwd(nil, 42, 99); got != "/repo/group" {
		t.Fatalf("effectiveCwd() = %q, want group fallback", got)
	}

	if err := store.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 42, ThreadID: 99},
		CWD:         "/repo/topic",
		ProjectSlug: "-repo-topic",
		Source:      projectbinding.BindingManual,
	}); err != nil {
		t.Fatal(err)
	}
	if got := bc.effectiveCwd(nil, 42, 99); got != "/repo/topic" {
		t.Fatalf("effectiveCwd() = %q, want topic override", got)
	}
}

func TestEffectiveCwdForContext_UsesDefaultCWDWhenPrivateNoBinding(t *testing.T) {
	dir := t.TempDir()
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	resolver := users.NewResolver(dir)
	store := users.NewStore(resolver)

	// Save profile with DefaultCWD set
	if err := store.Save(&users.Profile{
		UserID:     1,
		DefaultCWD: dir,
	}); err != nil {
		t.Fatal(err)
	}

	bc := &Service{usersStore: store}
	got := bc.effectiveCwdForContext(nil, 42, 0, 1, true)
	if got != want {
		t.Fatalf("effectiveCwdForContext() = %q, want DefaultCWD %q", got, want)
	}
}

func TestEffectiveCwdForContext_BindingBeatsDefaultCWD(t *testing.T) {
	dir := t.TempDir()
	resolver := users.NewResolver(dir)
	store := users.NewStore(resolver)

	if err := store.Save(&users.Profile{
		UserID:     1,
		DefaultCWD: dir,
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate a project binding
	bc := &Service{usersStore: store, sessions: session.NewStore()}
	bc.sessions.SetCwd(42, 0, "/repo/explicit")
	got := bc.effectiveCwdForContext(nil, 42, 0, 1, true)
	if got != "/repo/explicit" {
		t.Fatalf("effectiveCwdForContext() = %q, want binding cwd", got)
	}
}

func TestEffectiveCwdForContext_DefaultCWDNotUsedInGroup(t *testing.T) {
	dir := t.TempDir()
	resolver := users.NewResolver(dir)
	store := users.NewStore(resolver)

	if err := store.Save(&users.Profile{UserID: 1, DefaultCWD: dir}); err != nil {
		t.Fatal(err)
	}

	bc := &Service{usersStore: store}
	// Not private chat
	got := bc.effectiveCwdForContext(nil, 42, 0, 1, false)
	if got != "" {
		t.Fatalf("effectiveCwdForContext() = %q, want empty for group chat", got)
	}
}

func TestEffectiveCwdForContext_DefaultCWDNotUsedInTopic(t *testing.T) {
	dir := t.TempDir()
	resolver := users.NewResolver(dir)
	store := users.NewStore(resolver)

	if err := store.Save(&users.Profile{UserID: 1, DefaultCWD: dir}); err != nil {
		t.Fatal(err)
	}

	bc := &Service{usersStore: store}
	// Private chat but with topic/thread
	got := bc.effectiveCwdForContext(nil, 42, 99, 1, true)
	if got != "" {
		t.Fatalf("effectiveCwdForContext() = %q, want empty for topic chat", got)
	}
}

func TestEffectiveCwdForContext_InvalidDefaultCWDIgnored(t *testing.T) {
	dir := t.TempDir()
	resolver := users.NewResolver(dir)
	store := users.NewStore(resolver)

	// Path does not exist
	if err := store.Save(&users.Profile{
		UserID:     1,
		DefaultCWD: "/nonexistent/path",
	}); err != nil {
		t.Fatal(err)
	}

	bc := &Service{usersStore: store}
	got := bc.effectiveCwdForContext(nil, 42, 0, 1, true)
	if got != "" {
		t.Fatalf("effectiveCwdForContext() = %q, want empty for invalid path", got)
	}
}

// TestLoadMemoryContents_CwdSwitchPreservesProjectFacts verifies that changing
// the active /cwd swaps the injected cwd_overlay layer and that returning to a
// previous project still loads the same on-disk facts (project-scoped memory).

// fakeRunLog implements runlog.Store for testing checkpoint injection.
type fakeRunLog struct {
	latest *runlog.RunRecord
}

func (f *fakeRunLog) Start(ctx context.Context, record runlog.RunRecord) error  { return nil }
func (f *fakeRunLog) Update(ctx context.Context, update runlog.RunUpdate) error { return nil }
func (f *fakeRunLog) Complete(ctx context.Context, runID string, status runlog.RunStatus, checkpoint, errMsg, toolSummary string) error {
	return nil
}
func (f *fakeRunLog) RecordEvents(_ context.Context, _ []runlog.RunEvent) error { return nil }
func (f *fakeRunLog) Prune(_ context.Context, _ runlog.PruneOptions) (runlog.PruneResult, error) {
	return runlog.PruneResult{}, nil
}
func (f *fakeRunLog) Latest(ctx context.Context, chatID int64, threadID int) (*runlog.RunRecord, error) {
	if f.latest == nil || f.latest.ChatID != chatID || f.latest.ThreadID != threadID {
		return nil, nil
	}
	return f.latest, nil
}
func (f *fakeRunLog) RecordEvent(_ context.Context, _ runlog.RunEvent) error { return nil }
func (f *fakeRunLog) ListEvents(_ context.Context, _ string) ([]runlog.RunEvent, error) {
	return nil, nil
}
func (f *fakeRunLog) GetRun(_ context.Context, _ string) (*runlog.RunRecord, error) {
	return nil, nil
}
func (f *fakeRunLog) ListRuns(_ context.Context, _ int64, _ int) ([]runlog.RunRecord, error) {
	return nil, nil
}
func (f *fakeRunLog) Metrics(_ context.Context, _ runlog.MetricsFilter) (*runlog.MetricsResult, error) {
	return nil, nil
}
func (f *fakeRunLog) GetLastOutboundMessage(_ context.Context, _ string) (int64, int, int64, error) {
	return 0, 0, 0, nil
}
func (f *fakeRunLog) MarkStaleRunsInterrupted(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (f *fakeRunLog) Close() error { return nil }

func TestBuildLastRunStateSection_ReturnsEmptyWhenNoRunLog(t *testing.T) {
	bc := &Service{}
	got := bc.buildLastRunStateSection(1, 0, "hello", 0)
	if got != "" {
		t.Fatalf("expected empty without runlog, got %q", got)
	}
}

func TestBuildLastRunStateSection_CompletedRunSkipsWithoutContinuation(t *testing.T) {
	bc := &Service{
		runLog: &fakeRunLog{
			latest: &runlog.RunRecord{
				ChatID:     1,
				ThreadID:   0,
				Status:     runlog.RunCompleted,
				Checkpoint: "Status: completed\nFerramentas: Read\nResposta/último resumo: done",
			},
		},
		sessions: session.NewStore(),
	}
	// Active session + completed run + casual text = skip
	bc.sessions.Set(1, 0, "/tmp/test-session.jsonl")

	got := bc.buildLastRunStateSection(1, 0, "good morning", 0)
	if got != "" {
		t.Fatalf("expected empty for completed run without continuation, got %q", got)
	}
}

func TestBuildLastRunStateSection_FailedRunInjectsCheckpoint(t *testing.T) {
	bc := &Service{
		runLog: &fakeRunLog{
			latest: &runlog.RunRecord{
				ChatID:     1,
				ThreadID:   0,
				Status:     runlog.RunFailed,
				Checkpoint: "Status: failed\nFerramentas: Read, Grep\nErro: timeout",
			},
		},
		sessions: session.NewStore(),
	}
	bc.sessions.Set(1, 0, "/tmp/test-session.jsonl")

	got := bc.buildLastRunStateSection(1, 0, "good morning", 0)
	if got == "" {
		t.Fatal("expected checkpoint for failed run")
	}
	if !strings.Contains(got, "checkpoint_untrusted") {
		t.Fatalf("expected checkpoint_untrusted wrapper, got %q", got)
	}
	if !strings.Contains(got, "Status: failed") {
		t.Fatalf("expected status in checkpoint, got %q", got)
	}
}

func TestBuildLastRunStateSection_ContinuationTriggersInjection(t *testing.T) {
	bc := &Service{
		runLog: &fakeRunLog{
			latest: &runlog.RunRecord{
				ChatID:     1,
				ThreadID:   0,
				Status:     runlog.RunCompleted,
				Checkpoint: "Status: completed\nFerramentas: Read\nResposta/último resumo: done",
			},
		},
		sessions: session.NewStore(),
	}
	bc.sessions.Set(1, 0, "/tmp/test-session.jsonl")

	got := bc.buildLastRunStateSection(1, 0, "continua a análise", 0)
	if got == "" {
		t.Fatal("expected checkpoint for continuation trigger")
	}
	if !strings.Contains(got, "completed") {
		t.Fatalf("expected completed status in checkpoint, got %q", got)
	}
}

func TestBuildLastRunStateSection_RedactsSecrets(t *testing.T) {
	bc := &Service{
		runLog: &fakeRunLog{
			latest: &runlog.RunRecord{
				ChatID:     1,
				ThreadID:   0,
				Status:     runlog.RunFailed,
				Checkpoint: "Status: failed\nAPI Key: sk-test1234567890abcdef",
			},
		},
		sessions: session.NewStore(),
	}
	bc.sessions.Set(1, 0, "/tmp/test-session.jsonl")

	got := bc.buildLastRunStateSection(1, 0, "retoma", 0)
	if !strings.Contains(got, "API_KEY_REDACTED") && !strings.Contains(got, "REDACTED") {
		t.Fatalf("expected secrets to be redacted in checkpoint, got %q", got)
	}
	if strings.Contains(got, "sk-test") {
		t.Fatal("secrets leaked into checkpoint section")
	}
}

func TestIsContinuation(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"continua", true},
		{"Continue a análise", true},
		{"segue com o plano", true},
		{"nova análise", true},
		{"reanalisa", true},
		{"faz de novo", true},
		{"retoma", true},
		{"a partir do checkpoint", true},
		{"bom dia", false},
		{"qual o status", false},
		{"", false},
		{"continuação", false}, // word boundary: must not match "continua"
		{"analisa isso", false},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			if got := isContinuation(tc.text); got != tc.want {
				t.Errorf("isContinuation(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// TestBuildContinuitySection_HotActive_Skips verifies that continuity is NOT
// injected when state is hot (<5min) and the session is active (saves tokens).

// TestBuildContinuitySection_HotCold_Injects verifies that continuity IS
// injected when state is hot (<5min) but the session is cold (just died).

// TestBuildContinuitySection_WarmCold_Injects verifies that continuity IS
// injected when state is warm (>5min but within retention) and session is cold.

// TestBuildContinuitySection_WarmActive_Skips verifies that continuity is NOT
// injected when state is warm and session is active.

// TestBuildContinuitySection_Stale_Skips verifies that continuity is NOT
// injected when state is stale (>7 days), regardless of session activity.

// TestBuildContinuitySection_ContinuationAlwaysInjects verifies that
// continuation text always triggers injection regardless of freshness or
// session state.

// TestBuildContinuitySection_NoState_Skips verifies that when no continuity
// state exists, the section is empty regardless of other factors.

// TestBuildContinuitySection_NilStore_ReturnsEmpty verifies nil continuity
// store produces no section.

// TestBuildContinuitySection_NilSessions_DefaultsCold verifies that a nil
// sessions store defaults to "inactive", which means continuity is injected
// for hot+cold and warm+cold — the conservative fallback.

func TestBuildSystemPrompt_SingleActiveProfileSection(t *testing.T) {
	resolver, err := profiles.NewResolver("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	profile := resolver.Get("developer")
	if profile == nil {
		t.Fatal("developer builtin missing")
	}

	svc := &Service{
		config:   &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions: session.NewStore(),
	}

	prompt, err := svc.buildSystemPrompt("refactor the handler", profile, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(prompt, "# Active Prompt Profile:"); n != 1 {
		t.Fatalf("expected exactly one Active Prompt Profile section, got %d in prompt", n)
	}
	if !strings.Contains(prompt, "Active Prompt Profile: developer") {
		t.Fatal("expected developer profile header")
	}
	if strings.Contains(prompt, "You are in researcher mode") {
		t.Fatal("researcher profile body leaked into developer prompt")
	}
}

func TestBuildSystemPrompt_OneShotProfileOverridesActiveDefault(t *testing.T) {
	resolver, err := profiles.NewResolver("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	profile, stripped, err := resolver.ResolveEffectiveForUser("@researcher compare SDKs", "developer", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if profile == nil || profile.Name != "researcher" {
		t.Fatalf("ResolveEffectiveForUser profile = %v, want researcher", profile)
	}
	if stripped != "compare SDKs" {
		t.Fatalf("stripped text = %q, want %q", stripped, "compare SDKs")
	}

	svc := &Service{
		config:   &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions: session.NewStore(),
	}

	prompt, err := svc.buildSystemPrompt(stripped, profile, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(prompt, "# Active Prompt Profile:"); n != 1 {
		t.Fatalf("expected exactly one Active Prompt Profile section, got %d", n)
	}
	if !strings.Contains(prompt, "Active Prompt Profile: researcher") {
		t.Fatal("expected researcher profile header")
	}
	if !strings.Contains(prompt, "You are in researcher mode") {
		t.Fatal("expected researcher profile body")
	}
	if strings.Contains(prompt, "You are in developer mode") {
		t.Fatal("developer profile body leaked when @researcher overrides /mode developer")
	}
}

func TestBuildSystemPrompt_TUIExcludesTelegramOnlyText(t *testing.T) {
	// Assertion 1: TUI pipeline must NOT contain Telegram-only text.
	svc := &Service{
		entryPoint: observability.EntryPointTUI,
		config:     &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:   session.NewStore(),
	}

	prompt, err := svc.buildSystemPrompt("hello", nil, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	telegramOnly := []struct {
		needle string
		desc   string
	}{
		{needle: "You ARE the Telegram bot", desc: "Telegram identity"},
		{needle: "`aurelia telegram react", desc: "telegram react CLI"},
		{needle: "Available emojis", desc: "reaction emojis"},
		{needle: "## Telegram Context", desc: "Telegram context header"},
	}
	for _, c := range telegramOnly {
		if strings.Contains(prompt, c.needle) {
			t.Errorf("TUI prompt contains Telegram-only text %q (%s)", c.needle, c.desc)
		}
	}
	// TUI prompt must have the Terminal/TUI section header.
	if !strings.Contains(prompt, "## Terminal / TUI Context") {
		t.Error("TUI prompt missing TUI context section")
	}
}

func TestBuildSystemPrompt_TUIHasNoTelegramReactionCLI(t *testing.T) {
	// Assertion 1 (precise): the react CLI instruction must not appear.
	svc := &Service{
		entryPoint: observability.EntryPointTUI,
		config:     &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:   session.NewStore(),
	}

	prompt, err := svc.buildSystemPrompt("hello", nil, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(prompt, "`aurelia telegram react") {
		t.Error("TUI prompt must not contain 'telegram react' CLI instruction")
	}
}

func TestBuildSystemPrompt_TelegramStillHasReactionCLI(t *testing.T) {
	// Assertion 2: Telegram prompt still has reaction CLI.
	svc := &Service{
		entryPoint: observability.EntryPointTelegram,
		config:     &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:   session.NewStore(),
	}

	prompt, err := svc.buildSystemPrompt("hello", nil, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(prompt, "telegram react") {
		t.Error("Telegram prompt must contain 'telegram react' CLI instruction")
	}
	if !strings.Contains(prompt, "You ARE the Telegram bot") {
		t.Error("Telegram prompt must identify as Telegram bot")
	}
}

func TestBuildSystemPrompt_DefaultEntryPointUsesTelegram(t *testing.T) {
	// Assertion 4: Empty entrypoint defaults to Telegram prompt.
	svc := &Service{
		entryPoint: "", // zero value — but NewService normalizes it
		config:     &config.AppConfig{DefaultProvider: "test", DefaultModel: "test"},
		sessions:   session.NewStore(),
	}

	prompt, err := svc.buildSystemPrompt("hello", nil, 42, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(prompt, "## Telegram Context") {
		t.Error("Default (empty) entrypoint prompt missing Telegram context")
	}
	if !strings.Contains(prompt, "telegram react") {
		t.Error("Default (empty) entrypoint prompt missing 'telegram react'")
	}
}

// --- Project Work State prompt tests ---

func ptrString(s string) *string { return &s }

// TestBuildProjectWorkSection_RedactsSecrets verifies that secrets in
// LastUserIntent are redacted in the formatted Project Work State output.

// TestBuildProjectWorkSection_EscapesDelimiters verifies that delimiter-
// sensitive characters in LastUserIntent are escaped to prevent injection
// of closing </project_work_state_untrusted> tags.

// TestBuildProjectWorkSection_ZeroUserID verifies that buildContinuitySection
// with userID=0 and /cwd active returns empty — no project lookup for
// unidentifiable users.
