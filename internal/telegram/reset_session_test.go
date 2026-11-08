package telegram

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/igormaneschy/aurelia/internal/memorycontext"
	"github.com/igormaneschy/aurelia/internal/session"
)

type cancelingMemoryProvider struct {
	scopes []memorycontext.Scope
}

func (p *cancelingMemoryProvider) WorkingSet(context.Context, memorycontext.Scope) (memorycontext.WorkingSet, error) {
	return memorycontext.WorkingSet{}, nil
}

func (p *cancelingMemoryProvider) Status(context.Context, memorycontext.Scope) (memorycontext.Status, error) {
	return memorycontext.Status{}, nil
}

func (p *cancelingMemoryProvider) CancelHandoffs(_ context.Context, scope memorycontext.Scope) error {
	p.scopes = append(p.scopes, scope)
	return nil
}

func TestResetSessionCancelsPendingHandoffs(t *testing.T) {
	dir := t.TempDir()
	marker := "workspace = \"personal\"\nproject = \"aurelia\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".ai-memory.toml"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	sessions := session.NewStore()
	sessions.SetCwd(42, 0, dir)
	sessions.SetSession(42, 0, 1, "pi-session-file")
	provider := &cancelingMemoryProvider{}
	bc := &BotController{sessions: sessions, memoryProvider: provider}

	bc.ResetSession(42, 0, 1, true, false)

	if len(provider.scopes) != 1 || provider.scopes[0].ProjectRef() != "personal/aurelia" {
		t.Fatalf("cancelled scopes = %+v, want personal/aurelia", provider.scopes)
	}
	if got := sessions.GetSession(42, 0, 1); got != "" {
		t.Fatalf("session = %q, want empty after reset", got)
	}
}
