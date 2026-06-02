package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/users"
)

func TestBuildPromptForUser_PerUserMd(t *testing.T) {
	// Two users with different USER.md content should produce different prompts.
	dir := t.TempDir()
	resolver := users.NewResolver(dir)

	// Create per-user USER.md for user 1
	user1Md := filepath.Join(resolver.PersonasDir(1), "USER.md")
	if err := os.MkdirAll(filepath.Dir(user1Md), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user1Md, []byte("# User\nNome: Alice\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create per-user USER.md for user 2
	user2Md := filepath.Join(resolver.PersonasDir(2), "USER.md")
	if err := os.MkdirAll(filepath.Dir(user2Md), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user2Md, []byte("# User\nNome: Bob\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Global files
	identityPath := filepath.Join(dir, "IDENTITY.md")
	soulPath := filepath.Join(dir, "SOUL.md")
	if err := os.WriteFile(identityPath, []byte("---\nname: Lex\nrole: Helper\n---\nIDENTITY_BODY"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(soulPath, []byte("# Soul\nBe helpful.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewCanonicalIdentityService(identityPath, soulPath, "", "", "", "")

	prompt1, err := svc.BuildPromptForUser(1, resolver, false, "")
	if err != nil {
		t.Fatalf("BuildPromptForUser(1) error = %v", err)
	}
	prompt2, err := svc.BuildPromptForUser(2, resolver, false, "")
	if err != nil {
		t.Fatalf("BuildPromptForUser(2) error = %v", err)
	}

	if !strings.Contains(prompt1, "Alice") {
		t.Fatalf("prompt1 should mention Alice, got: %s", prompt1)
	}
	if !strings.Contains(prompt2, "Bob") {
		t.Fatalf("prompt2 should mention Bob, got: %s", prompt2)
	}
	if prompt1 == prompt2 {
		t.Fatal("prompts for different users should differ")
	}
}

func TestBuildPromptForUser_OwnerDocsOnlyForOwner(t *testing.T) {
	dir := t.TempDir()
	resolver := users.NewResolver(dir)

	// Create per-user USER.md for user 1
	user1Md := filepath.Join(resolver.PersonasDir(1), "USER.md")
	if err := os.MkdirAll(filepath.Dir(user1Md), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user1Md, []byte("# User\nNome: Alice\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Owner playbook
	playbookPath := filepath.Join(dir, "OWNER_PLAYBOOK.md")
	if err := os.WriteFile(playbookPath, []byte("Owner rules only."), 0644); err != nil {
		t.Fatal(err)
	}

	// Global files
	identityPath := filepath.Join(dir, "IDENTITY.md")
	soulPath := filepath.Join(dir, "SOUL.md")
	if err := os.WriteFile(identityPath, []byte("---\nname: Lex\nrole: Helper\n---\nIDENTITY_BODY"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(soulPath, []byte("# Soul\nBe helpful.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewCanonicalIdentityService(identityPath, soulPath, "", playbookPath, "", "")

	// Owner should see OWNER CONTEXT
	promptOwner, err := svc.BuildPromptForUser(1, resolver, true, "")
	if err != nil {
		t.Fatalf("BuildPromptForUser(owner) error = %v", err)
	}
	if !strings.Contains(promptOwner, "# OWNER CONTEXT") {
		t.Fatalf("owner prompt should contain OWNER CONTEXT, got: %s", promptOwner)
	}
	if !strings.Contains(promptOwner, "Owner rules only.") {
		t.Fatalf("owner prompt should contain playbook, got: %s", promptOwner)
	}

	// Non-owner should NOT see OWNER CONTEXT
	promptNonOwner, err := svc.BuildPromptForUser(1, resolver, false, "")
	if err != nil {
		t.Fatalf("BuildPromptForUser(non-owner) error = %v", err)
	}
	if strings.Contains(promptNonOwner, "# OWNER CONTEXT") {
		t.Fatalf("non-owner prompt should NOT contain OWNER CONTEXT, got: %s", promptNonOwner)
	}
}

func TestBuildPromptForUser_MissingUserMd(t *testing.T) {
	dir := t.TempDir()
	resolver := users.NewResolver(dir)

	// Global files only — no per-user USER.md
	identityPath := filepath.Join(dir, "IDENTITY.md")
	soulPath := filepath.Join(dir, "SOUL.md")
	if err := os.WriteFile(identityPath, []byte("---\nname: Lex\nrole: Helper\n---\nIDENTITY_BODY"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(soulPath, []byte("# Soul\nBe helpful.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewCanonicalIdentityService(identityPath, soulPath, "", "", "", "")

	prompt, err := svc.BuildPromptForUser(42, resolver, false, "")
	if err != nil {
		t.Fatalf("BuildPromptForUser() with missing USER.md should not error, got: %v", err)
	}

	// Should still have identity and soul content
	if !strings.Contains(prompt, "IDENTITY_BODY") {
		t.Fatalf("prompt should contain identity body, got: %s", prompt)
	}
	if !strings.Contains(prompt, "# Soul") {
		t.Fatalf("prompt should contain soul, got: %s", prompt)
	}
	// Should contain the stub with user id
	if !strings.Contains(prompt, "User id: 42") {
		t.Fatalf("expected stub mentioning user id 42, got: %s", prompt)
	}
}

func TestBuildPromptForUser_AppendsActiveModeLast(t *testing.T) {
	dir := t.TempDir()
	resolver := users.NewResolver(dir)

	// Create per-user USER.md
	user1Md := filepath.Join(resolver.PersonasDir(1), "USER.md")
	if err := os.MkdirAll(filepath.Dir(user1Md), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user1Md, []byte("# User\nNome: Alice\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create mode overlay file
	modePath := resolver.UserModePath(1, "developer")
	if err := os.WriteFile(modePath, []byte("You are in DEVELOPER mode. Write clean code."), 0644); err != nil {
		t.Fatal(err)
	}

	identityPath := filepath.Join(dir, "IDENTITY.md")
	soulPath := filepath.Join(dir, "SOUL.md")
	if err := os.WriteFile(identityPath, []byte("---\nname: Lex\nrole: Helper\n---\nIDENTITY_BODY"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(soulPath, []byte("# Soul\nBe helpful.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewCanonicalIdentityService(identityPath, soulPath, "", "", "", "")
	prompt, err := svc.BuildPromptForUser(1, resolver, false, "developer")
	if err != nil {
		t.Fatalf("BuildPromptForUser error = %v", err)
	}

	if !strings.Contains(prompt, "## ACTIVE MODE: DEVELOPER") {
		t.Fatalf("expected ACTIVE MODE: DEVELOPER header, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Write clean code.") {
		t.Fatalf("expected mode overlay content, got: %s", prompt)
	}

	// Verify order: IDENTITY_BODY should appear before ACTIVE MODE
	identityIdx := strings.Index(prompt, "IDENTITY_BODY")
	modeIdx := strings.Index(prompt, "## ACTIVE MODE: DEVELOPER")
	if identityIdx == -1 || modeIdx == -1 || identityIdx >= modeIdx {
		t.Fatalf("ACTIVE MODE should appear after IDENTITY_BODY, identityIdx=%d modeIdx=%d", identityIdx, modeIdx)
	}
}

func TestBuildPromptForUser_MissingModeOverlayIgnored(t *testing.T) {
	dir := t.TempDir()
	resolver := users.NewResolver(dir)

	// Create per-user USER.md but NO mode overlay file
	user1Md := filepath.Join(resolver.PersonasDir(1), "USER.md")
	if err := os.MkdirAll(filepath.Dir(user1Md), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user1Md, []byte("# User\nNome: Alice\n"), 0644); err != nil {
		t.Fatal(err)
	}

	identityPath := filepath.Join(dir, "IDENTITY.md")
	soulPath := filepath.Join(dir, "SOUL.md")
	if err := os.WriteFile(identityPath, []byte("---\nname: Lex\nrole: Helper\n---\nIDENTITY_BODY"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(soulPath, []byte("# Soul\nBe helpful.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewCanonicalIdentityService(identityPath, soulPath, "", "", "", "")
	// Request developer mode but no mode_developer.md exists
	prompt, err := svc.BuildPromptForUser(1, resolver, false, "developer")
	if err != nil {
		t.Fatalf("BuildPromptForUser error = %v", err)
	}

	if strings.Contains(prompt, "## ACTIVE MODE") {
		t.Fatalf("should NOT contain ACTIVE MODE header when overlay file is missing")
	}
}

func TestBuildPromptForUser_DefaultModeIgnored(t *testing.T) {
	dir := t.TempDir()
	resolver := users.NewResolver(dir)

	user1Md := filepath.Join(resolver.PersonasDir(1), "USER.md")
	if err := os.MkdirAll(filepath.Dir(user1Md), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user1Md, []byte("# User\nNome: Alice\n"), 0644); err != nil {
		t.Fatal(err)
	}

	identityPath := filepath.Join(dir, "IDENTITY.md")
	soulPath := filepath.Join(dir, "SOUL.md")
	if err := os.WriteFile(identityPath, []byte("---\nname: Lex\nrole: Helper\n---\nIDENTITY_BODY"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(soulPath, []byte("# Soul\nBe helpful.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewCanonicalIdentityService(identityPath, soulPath, "", "", "", "")
	// Empty mode should not inject anything
	for _, mode := range []string{"", "general", "auto"} {
		t.Run("mode="+mode, func(t *testing.T) {
			prompt, err := svc.BuildPromptForUser(1, resolver, false, mode)
			if err != nil {
				t.Fatalf("BuildPromptForUser error = %v", err)
			}
			if strings.Contains(prompt, "## ACTIVE MODE") {
				t.Fatalf("should NOT contain ACTIVE MODE for mode=%q", mode)
			}
		})
	}
}

func TestBuildPromptForUser_InvalidModeIgnored(t *testing.T) {
	dir := t.TempDir()
	resolver := users.NewResolver(dir)

	user1Md := filepath.Join(resolver.PersonasDir(1), "USER.md")
	if err := os.MkdirAll(filepath.Dir(user1Md), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user1Md, []byte("# User\nNome: Alice\n"), 0644); err != nil {
		t.Fatal(err)
	}

	identityPath := filepath.Join(dir, "IDENTITY.md")
	soulPath := filepath.Join(dir, "SOUL.md")
	if err := os.WriteFile(identityPath, []byte("---\nname: Lex\nrole: Helper\n---\nIDENTITY_BODY"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(soulPath, []byte("# Soul\nBe helpful.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewCanonicalIdentityService(identityPath, soulPath, "", "", "", "")
	// Invalid mode should be silently ignored (no mode block)
	prompt, err := svc.BuildPromptForUser(1, resolver, false, "invalid")
	if err != nil {
		t.Fatalf("BuildPromptForUser error = %v", err)
	}
	if strings.Contains(prompt, "## ACTIVE MODE") {
		t.Fatalf("should NOT contain ACTIVE MODE for invalid mode")
	}
}
