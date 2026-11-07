package telegram

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/igormaneschy/aurelia/internal/memorycontext"
	"github.com/igormaneschy/aurelia/internal/session"
)

type fakeMemoryProvider struct{}

func (fakeMemoryProvider) WorkingSet(context.Context, memorycontext.Scope) (memorycontext.WorkingSet, error) {
	return memorycontext.WorkingSet{}, nil
}

func (fakeMemoryProvider) Status(context.Context, memorycontext.Scope) (memorycontext.Status, error) {
	return memorycontext.Status{}, nil
}

type fakeDreamer struct {
	flushed bool
}

func (f *fakeDreamer) AfterTurn(int64) {}

func (f *fakeDreamer) AfterTurnNudge(int64, int, int64, string, string, *session.NudgeBuffer) {}

func (f *fakeDreamer) FlushNudge(int64, int, int64, string, string, *session.NudgeBuffer) {
	f.flushed = true
}

func TestResetSessionSkipsLegacyNudgeInAIMemoryMode(t *testing.T) {
	sessions := session.NewStore()
	sessions.SetSession(42, 7, 1, "pi-session-file")
	dreamer := &fakeDreamer{}
	bc := &BotController{sessions: sessions, dreamer: dreamer, memoryProvider: fakeMemoryProvider{}}

	bc.ResetSession(42, 7, 1, true, false)

	if dreamer.flushed {
		t.Fatal("legacy nudge flush ran in ai-memory mode")
	}
	if got := sessions.GetSession(42, 7, 1); got != "" {
		t.Fatalf("session = %q, want empty after reset", got)
	}
}

func projectMarkerDir(t *testing.T, workspace, project string) string {
	t.Helper()
	dir := t.TempDir()
	body := fmt.Sprintf("workspace = %q\nproject = %q\n", workspace, project)
	if err := os.WriteFile(filepath.Join(dir, ".ai-memory.toml"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResetSessionOnProjectChangeClearsSession(t *testing.T) {
	sessions := session.NewStore()
	sessions.SetSession(42, 7, 1, "pi-session-file")
	bc := &BotController{sessions: sessions, memoryProvider: fakeMemoryProvider{}}

	projectA := projectMarkerDir(t, "personal", "aurelia-homebrewing")
	projectB := projectMarkerDir(t, "personal", "aurelia-finance")

	bc.resetSessionOnProjectChange(42, 7, 1, projectA, projectB)

	if got := sessions.GetSession(42, 7, 1); got != "" {
		t.Fatalf("session = %q, want empty after project change", got)
	}
}

func TestResetSessionOnProjectChangeKeepsSameProject(t *testing.T) {
	sessions := session.NewStore()
	sessions.SetSession(42, 7, 1, "pi-session-file")
	bc := &BotController{sessions: sessions, memoryProvider: fakeMemoryProvider{}}

	project := projectMarkerDir(t, "personal", "aurelia-homebrewing")
	nested := filepath.Join(project, "topics", "homebrewing")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}

	bc.resetSessionOnProjectChange(42, 7, 1, project, nested)

	if got := sessions.GetSession(42, 7, 1); got != "pi-session-file" {
		t.Fatalf("session = %q, want it preserved within the same project", got)
	}
}

func TestResetSessionOnProjectChangeLegacyModeDoesNothing(t *testing.T) {
	sessions := session.NewStore()
	sessions.SetSession(42, 7, 1, "pi-session-file")
	bc := &BotController{sessions: sessions}

	projectA := projectMarkerDir(t, "personal", "aurelia-homebrewing")
	projectB := projectMarkerDir(t, "personal", "aurelia-finance")

	bc.resetSessionOnProjectChange(42, 7, 1, projectA, projectB)

	if got := sessions.GetSession(42, 7, 1); got != "pi-session-file" {
		t.Fatalf("session = %q, want legacy behavior unchanged", got)
	}
}
