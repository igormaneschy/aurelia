package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/internal/memorycontext"
)

type fakeTUIProvider struct{}

func (fakeTUIProvider) WorkingSet(context.Context, memorycontext.Scope) (memorycontext.WorkingSet, error) {
	return memorycontext.WorkingSet{}, nil
}

func (fakeTUIProvider) Status(context.Context, memorycontext.Scope) (memorycontext.Status, error) {
	return memorycontext.Status{}, nil
}

func tuiProjectMarkerDir(t *testing.T, workspace, project string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	body := fmt.Sprintf("workspace = %q\nproject = %q\n", workspace, project)
	if err := os.WriteFile(filepath.Join(dir, ".ai-memory.toml"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTUICwdChangeResetsSessionOnProjectChange(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()
	a.memoryProvider = fakeTUIProvider{}

	userID := int64(os.Getuid())
	projectA := tuiProjectMarkerDir(t, "personal", "aurelia-homebrewing")
	projectB := tuiProjectMarkerDir(t, "personal", "aurelia-finance")

	handler := makeTUIHandler(a)
	_ = handler(ctx, ipc.IPCMessage{Type: ipc.MsgTypeCommand, Text: "/cwd " + projectA}, (&testEmit{}).emit)
	a.sessions.SetSession(ipc.ReservedTUIChatID, 0, userID, "pi-session-file")

	_ = handler(ctx, ipc.IPCMessage{Type: ipc.MsgTypeCommand, Text: "/cwd " + projectB}, (&testEmit{}).emit)

	if got := a.sessions.GetSession(ipc.ReservedTUIChatID, 0, userID); got != "" {
		t.Fatalf("session = %q, want empty after project change", got)
	}
}

func TestTUICwdChangeKeepsSessionInSameProject(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()
	a.memoryProvider = fakeTUIProvider{}

	userID := int64(os.Getuid())
	project := tuiProjectMarkerDir(t, "personal", "aurelia-homebrewing")
	nested := filepath.Join(project, "topics", "homebrewing")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}

	handler := makeTUIHandler(a)
	_ = handler(ctx, ipc.IPCMessage{Type: ipc.MsgTypeCommand, Text: "/cwd " + project}, (&testEmit{}).emit)
	a.sessions.SetSession(ipc.ReservedTUIChatID, 0, userID, "pi-session-file")

	_ = handler(ctx, ipc.IPCMessage{Type: ipc.MsgTypeCommand, Text: "/cwd " + nested}, (&testEmit{}).emit)

	if got := a.sessions.GetSession(ipc.ReservedTUIChatID, 0, userID); got != "pi-session-file" {
		t.Fatalf("session = %q, want it preserved within the same project", got)
	}
}
