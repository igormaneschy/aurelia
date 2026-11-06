package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
)

// A reset must not leak the previous conversation into the next turn.
//
// The continuity store keeps the last user intent and the last assistant
// summary, and the prompt builder injects them (plus the shared Project Work
// State) into the next system prompt. Before the fix, /reset only cleared the
// PI session, so the first message after a reset still answered from the
// previous context.
func TestBuildContinuitySection_AfterResetInjectsNoCarriedContext(t *testing.T) {
	contDir := t.TempDir()
	contStore, err := continuity.NewSQLiteStore(filepath.Join(contDir, "cont.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer contStore.Close()

	ctx := t.Context()
	now := time.Now()

	// Conversation with carried context from a previous turn.
	if err := contStore.Upsert(ctx, continuity.ConversationState{
		ChatID:               42,
		ThreadID:             0,
		UserID:               100,
		CWD:                  "/repo",
		LastUserIntent:       "analisar o modulo de auth",
		LastAssistantSummary: "o modulo de auth usa JWT",
		LastRunStatus:        "completed",
		UpdatedAt:            now,
	}); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		continuity: contStore,
		sessions:   session.NewStore(),
	}

	// Before the reset the context is injected.
	before := svc.buildContinuitySection(42, 0, "continua", 100, nil, false)
	if !strings.Contains(before, "analisar o modulo de auth") {
		t.Fatalf("pre-reset continuity missing carried context: %q", before)
	}

	// The reset clears the carried context and records the reason.
	if err := contStore.ResetConversation(ctx, 42, 0, 100, "user reset"); err != nil {
		t.Fatal(err)
	}

	after := svc.buildContinuitySection(42, 0, "continua", 100, nil, false)
	if after != "" {
		t.Fatalf("post-reset continuity must be empty, got %q", after)
	}
	if strings.Contains(after, "analisar o modulo de auth") ||
		strings.Contains(after, "o modulo de auth usa JWT") {
		t.Fatalf("post-reset continuity leaked previous context: %q", after)
	}
}

// A reset also clears the project work state mirror for that conversation:
// the shared project state must not reinstate the previous context.
func TestResetConversation_ClearsCarriedContextAndKeepsReason(t *testing.T) {
	contDir := t.TempDir()
	contStore, err := continuity.NewSQLiteStore(filepath.Join(contDir, "cont.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer contStore.Close()

	ctx := t.Context()
	if err := contStore.Upsert(ctx, continuity.ConversationState{
		ChatID:               7,
		ThreadID:             0,
		UserID:               100,
		LastUserIntent:       "old intent",
		LastAssistantSummary: "old summary",
		LastCheckpoint:       "old checkpoint",
		LastTools:            "old tools",
		SessionID:            "/tmp/old-session.jsonl",
		UpdatedAt:            time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := contStore.ResetConversation(ctx, 7, 0, 100, "user reset"); err != nil {
		t.Fatal(err)
	}

	state, err := contStore.Get(ctx, 7, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("reset must keep the conversation row so the reset stays visible")
	}
	if state.LastUserIntent != "" || state.LastAssistantSummary != "" ||
		state.LastCheckpoint != "" || state.LastTools != "" || state.SessionID != "" {
		t.Fatalf("reset did not clear carried context: %+v", state)
	}
	if state.ResetReason != "user reset" {
		t.Fatalf("reset reason = %q, want %q", state.ResetReason, "user reset")
	}
}

// The reset marker must not suppress context forever: once the conversation
// runs again, the fresh intent/summary are injected again.
func TestBuildContinuitySection_AfterResetFreshTurnRestoresContext(t *testing.T) {
	contDir := t.TempDir()
	contStore, err := continuity.NewSQLiteStore(filepath.Join(contDir, "cont.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer contStore.Close()

	ctx := t.Context()
	if err := contStore.ResetConversation(ctx, 9, 0, 100, "user reset"); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		continuity: contStore,
		sessions:   session.NewStore(),
	}

	if got := svc.buildContinuitySection(9, 0, "continua", 100, nil, false); got != "" {
		t.Fatalf("post-reset continuity must be empty, got %q", got)
	}

	// A new turn writes fresh state for the conversation.
	if err := contStore.Upsert(ctx, continuity.ConversationState{
		ChatID:               9,
		ThreadID:             0,
		UserID:               100,
		LastUserIntent:       "novo intent",
		LastAssistantSummary: "novo summary",
		UpdatedAt:            time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	got := svc.buildContinuitySection(9, 0, "continua", 100, nil, false)
	if !strings.Contains(got, "novo intent") {
		t.Fatalf("fresh turn must inject its own context, got %q", got)
	}
}

// The shared Project Work State is what actually leaked the old context in
// the desktop client: with /cwd active the prompt builder injects the
// project state instead of the conversation continuity. A reset conversation
// must not receive it until it produces fresh state of its own.
func TestBuildContinuitySection_AfterResetSkipsSharedProjectWorkState(t *testing.T) {
	contDir := t.TempDir()
	contStore, err := continuity.NewSQLiteStore(filepath.Join(contDir, "cont.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer contStore.Close()

	ctx := t.Context()
	slug := runtime.ProjectSlug("/repo")
	if slug == "" {
		t.Skip("project slug unavailable for /repo")
	}

	// Shared project state written by another surface (e.g. Telegram).
	if err := contStore.PatchProjectWork(ctx, continuity.ProjectWorkKey{UserID: 100, ProjectSlug: slug},
		continuity.ProjectWorkPatch{
			LastUserIntent:       &[]string{"contexto antigo do projecto"}[0],
			LastAssistantSummary: &[]string{"resumo antigo do projecto"}[0],
			UpdatedAt:            time.Now(),
		}); err != nil {
		t.Fatal(err)
	}

	// The desktop conversation is reset.
	if err := contStore.ResetConversation(ctx, 42, 0, 100, continuity.UserResetReason); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		continuity: contStore,
		sessions:   session.NewStore(),
	}

	// With a project bound, the builder consults the shared project state.
	// The reset conversation must still get nothing.
	section := svc.buildContinuitySection(42, 0, "continua", 100, &profiles.PromptProfile{Cwd: "/repo"}, false)
	if section != "" {
		t.Fatalf("post-reset project work state leaked: %q", section)
	}
	if strings.Contains(section, "contexto antigo do projecto") ||
		strings.Contains(section, "resumo antigo do projecto") {
		t.Fatalf("post-reset project work state leaked old context: %q", section)
	}
}

var _ = context.Background
