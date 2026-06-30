package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/telegram"
	"github.com/igormaneschy/aurelia/internal/users"
)

func testAppWithProfiles(t *testing.T, userID int64, activeMode string) *app {
	t.Helper()
	a, _, _ := testApp(t)
	root := t.TempDir()
	userRes := users.NewResolver(root)
	store := users.NewStore(userRes)
	if err := store.Save(&users.Profile{
		UserID:     userID,
		Name:       "Owner",
		Language:   "pt",
		IsOwner:    true,
		ActiveMode: activeMode,
	}); err != nil {
		t.Fatal(err)
	}

	profilesDir := filepath.Join(root, "profiles")
	if err := os.MkdirAll(profilesDir, 0700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: coder\ndescription: Writes code\npublic: true\n---\nCoder body"
	if err := os.WriteFile(filepath.Join(profilesDir, "coder.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	resolver := profiles.NewResolverFromRegistry(nil, profilesDir, root)
	a.bot = telegram.TestBotController(resolver, store)
	// makeTUIHandler maps TUI UserID to DefaultPersonaUserID (or os.Getuid()).
	a.config.DefaultPersonaUserID = userID
	return a
}

func TestHandleTUIMode_QueryAndSet(t *testing.T) {
	const userID int64 = 50929027
	a := testAppWithProfiles(t, userID, "")

	query := handleTUIMode(a, userID, "/mode")
	if !strings.Contains(query, "Perfil ativo: **general**") {
		t.Fatalf("query = %q", query)
	}

	set := handleTUIMode(a, userID, "/mode developer")
	if !strings.Contains(set, "✅ Perfil alterado para **developer**") {
		t.Fatalf("set = %q", set)
	}

	query2 := handleTUIMode(a, userID, "/mode")
	if !strings.Contains(query2, "Perfil ativo: **developer**") {
		t.Fatalf("query after set = %q", query2)
	}
}

func TestHandleTUIAgents_ListsProfiles(t *testing.T) {
	const userID int64 = 50929027
	a := testAppWithProfiles(t, userID, "developer")

	reply := handleTUIAgents(a, userID, "/agents")
	for _, want := range []string{"Perfis disponíveis", "developer", "coder", "Perfil ativo: **developer**"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("reply missing %q: %s", want, reply)
		}
	}
	if strings.Count(reply, "**Perfis disponíveis**") != 1 {
		t.Fatalf("duplicate catalog header: %q", reply)
	}
}

func TestTUIHandler_ModeAndAgentsCommands(t *testing.T) {
	const userID int64 = 50929027
	a := testAppWithProfiles(t, userID, "")
	ctx := context.Background()
	handler := makeTUIHandler(a)
	te := &testEmit{}

	if err := handler(ctx, ipc.IPCMessage{
		Type:   "command",
		Text:   "/agents",
		UserID: userID,
		ChatID: ipc.ReservedTUIChatID,
	}, te.emit); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(te.events[1].Body, "developer") {
		t.Fatalf("/agents body = %q", te.events[1].Body)
	}

	te2 := &testEmit{}
	if err := handler(ctx, ipc.IPCMessage{
		Type:   "command",
		Text:   "/mode researcher",
		UserID: userID,
		ChatID: ipc.ReservedTUIChatID,
	}, te2.emit); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(te2.events[1].Body, "researcher") {
		t.Fatalf("/mode body = %q", te2.events[1].Body)
	}
}