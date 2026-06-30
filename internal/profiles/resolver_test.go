package profiles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/users"
)

func writeAgentFile(t *testing.T, dir, name, description, body string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0644); err != nil {
		t.Fatalf("write agent file %s: %v", name, err)
	}
}

func TestResolver_Get_Builtins(t *testing.T) {
	r, err := NewResolver("", "", "") // no agents dir, no canonical dir
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	for _, name := range []string{"general", "developer", "researcher"} {
		p := r.Get(name)
		if p == nil {
			t.Fatalf("Get(%q) = nil, want builtin", name)
		}
		if p.Kind != KindBuiltin {
			t.Errorf("Get(%q).Kind = %q, want builtin", name, p.Kind)
		}
		if p.Prompt == "" {
			t.Errorf("Get(%q).Prompt is empty", name)
		}
	}
}

func TestResolver_Get_LegacyAgentOverridesBuiltin(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "developer", "custom dev", "Custom dev prompt.")

	r, err := NewResolver(dir, "", "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	p := r.Get("developer")
	if p == nil {
		t.Fatal("Get(developer) = nil")
	}
	if p.Kind != KindLegacyAgent {
		t.Errorf("Kind = %q, want legacy_agent", p.Kind)
	}
	if p.Prompt != "Custom dev prompt." {
		t.Errorf("Prompt = %q, want 'Custom dev prompt.'", p.Prompt)
	}
}

func TestResolver_List_IncludesBuiltinsAndLegacy(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "coder", "writes code", "Coder body.")

	r, err := NewResolver(dir, "", "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	list := r.List()

	// Should have at least: general, developer, researcher, coder
	names := make(map[string]bool)
	for _, p := range list {
		names[p.Name] = true
	}
	for _, want := range []string{"general", "developer", "researcher", "coder"} {
		if !names[want] {
			t.Errorf("List() missing profile %q", want)
		}
	}
}

func TestResolver_ResolveEffective_AtProfile(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "coder", "writes code", "You are a coder.")

	r, err := NewResolver(dir, "", "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	// @coder should match and strip prefix.
	profile, stripped, err := r.ResolveEffective("@coder implement feature X", "")
	if err != nil {
		t.Fatalf("ResolveEffective: %v", err)
	}
	if profile == nil {
		t.Fatal("profile = nil, want coder")
	}
	if profile.Name != "coder" {
		t.Errorf("profile.Name = %q, want coder", profile.Name)
	}
	if stripped != "implement feature X" {
		t.Errorf("stripped = %q, want 'implement feature X'", stripped)
	}
}

func TestResolver_ResolveEffective_AtProfileUnknown(t *testing.T) {
	r, err := NewResolver("", "", "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	_, _, err = r.ResolveEffective("@unknown do something", "")
	if err == nil {
		t.Fatal("expected error for unknown @profile")
	}
	if _, ok := err.(*ErrProfileNotFound); !ok {
		t.Errorf("expected ErrProfileNotFound, got %T: %v", err, err)
	}
}

func TestResolver_ResolveEffective_MiddleTextNotParsed(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "coder", "writes code", "Coder body.")

	r, err := NewResolver(dir, "", "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	// @coder in the middle of text should NOT be treated as profile invocation.
	profile, stripped, err := r.ResolveEffective("please help @coder with this", "")
	if err != nil {
		t.Fatalf("ResolveEffective: %v", err)
	}
	// Should return general (default) since @coder is in the middle.
	if profile == nil || profile.Name != "general" {
		t.Errorf("expected general profile, got %v", profile)
	}
	if stripped != "please help @coder with this" {
		t.Errorf("text should not be stripped: %q", stripped)
	}
}

func TestResolver_ResolveEffective_ActiveDefaultFallback(t *testing.T) {
	r, err := NewResolver("", "", "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	profile, stripped, err := r.ResolveEffective("help me", "developer")
	if err != nil {
		t.Fatalf("ResolveEffective: %v", err)
	}
	if profile == nil || profile.Name != "developer" {
		t.Errorf("expected developer, got %v", profile)
	}
	if stripped != "help me" {
		t.Errorf("text should not be stripped: %q", stripped)
	}
}

func TestResolver_ResolveEffective_GeneralDefault(t *testing.T) {
	r, err := NewResolver("", "", "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	profile, _, err := r.ResolveEffective("help me", "")
	if err != nil {
		t.Fatalf("ResolveEffective: %v", err)
	}
	if profile == nil || profile.Name != "general" {
		t.Errorf("expected general, got %v", profile)
	}
}

func TestResolver_AtProfileWinsOverActiveDefault(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "researcher", "research profile", "Researcher prompt.")

	r, err := NewResolver(dir, "", "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	// @researcher wins over active default "developer".
	profile, _, err := r.ResolveEffective("@researcher compare SDKs", "developer")
	if err != nil {
		t.Fatalf("ResolveEffective: %v", err)
	}
	if profile == nil || profile.Name != "researcher" {
		t.Errorf("expected researcher, got %v", profile)
	}
}

func TestResolver_InvalidFrontmatterSkipped(t *testing.T) {
	dir := t.TempDir()

	// Write a file with invalid frontmatter (no closing ---)
	bad := []byte("name: broken\ndescription: broken\n---\nbody")
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), bad, 0644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	// Should not crash.
	r, err := NewResolver(dir, "", "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	// Builtins should still be available.
	p := r.Get("general")
	if p == nil {
		t.Fatal("Get(general) = nil after invalid file")
	}

	// Broken file should not appear in list.
	for _, p := range r.List() {
		if strings.EqualFold(p.Name, "broken") {
			t.Errorf("broken profile should not appear in List()")
		}
	}
}

func TestFromAgent_Nil(t *testing.T) {
	p := FromAgent(nil)
	if p != nil {
		t.Errorf("FromAgent(nil) = %v, want nil", p)
	}
}

func TestFromAgent_Fields(t *testing.T) {
	a := &agents.Agent{
		Name:              "coder",
		Description:       "writes code",
		Model:             "gpt-4",
		CapabilityProfile: "edit_project",
		AllowedTools:      []string{"Read", "Write"},
		DisallowedTools:   []string{"Bash"},
		MaxTurns:          10,
		ToolBudget:        20,
		Cwd:               "/tmp/project",
		Prompt:            "You are a coder.",
	}
	p := FromAgent(a)
	if p.Name != "coder" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.Kind != KindLegacyAgent {
		t.Errorf("Kind = %q", p.Kind)
	}
	if p.Model != "gpt-4" {
		t.Errorf("Model = %q", p.Model)
	}
	if len(p.AllowedTools) != 2 || p.AllowedTools[0] != "Read" {
		t.Errorf("AllowedTools = %v", p.AllowedTools)
	}
	if p.Public != true {
		t.Errorf("Public = %v, want true", p.Public)
	}
}

func TestPromptProfile_IsReadOnly(t *testing.T) {
	tests := []struct {
		name     string
		pp       *PromptProfile
		readOnly bool
	}{
		{"read_only profile", &PromptProfile{CapabilityProfile: "read_only"}, true},
		{"observe profile", &PromptProfile{CapabilityProfile: "observe"}, true},
		{"edit_project profile", &PromptProfile{CapabilityProfile: "edit_project"}, false},
		{"privileged profile", &PromptProfile{CapabilityProfile: "privileged"}, false},
		{"only Read allowed", &PromptProfile{AllowedTools: []string{"Read"}}, true},
		{"Write allowed", &PromptProfile{AllowedTools: []string{"Read", "Write"}}, false},
		{"Write denied", &PromptProfile{AllowedTools: []string{"Read", "Write"}, DisallowedTools: []string{"Write"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pp.IsReadOnly(); got != tt.readOnly {
				t.Errorf("IsReadOnly() = %v, want %v", got, tt.readOnly)
			}
		})
	}
}

func writeCanonicalFile(t *testing.T, dir, name, description, body string) {
	t.Helper()
	writeCanonicalFileWithPublic(t, dir, name, description, body, true)
}

func writeCanonicalFileWithPublic(t *testing.T, dir, name, description, body string, public bool) {
	t.Helper()
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\nkind: prompt_profile\npublic: %t\ntags: [test]\n---\n%s", name, description, public, body)
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0644); err != nil {
		t.Fatalf("write canonical file %s: %v", name, err)
	}
}

func TestLoadCanonical_Basic(t *testing.T) {
	dir := t.TempDir()
	writeCanonicalFile(t, dir, "reviewer", "reviews code", "You are a code reviewer.")

	profiles := LoadCanonical(dir)
	if len(profiles) != 1 {
		t.Fatalf("LoadCanonical: got %d profiles, want 1", len(profiles))
	}
	p, ok := profiles["reviewer"]
	if !ok {
		t.Fatal("reviewer not found in canonical profiles")
	}
	if p.Name != "reviewer" || p.Description != "reviews code" || p.Kind != KindGlobal {
		t.Errorf("reviewer fields: name=%q desc=%q kind=%q", p.Name, p.Description, p.Kind)
	}
	if p.Public != true {
		t.Errorf("Public = %v, want true", p.Public)
	}
	if len(p.Tags) != 1 || p.Tags[0] != "test" {
		t.Errorf("Tags = %v, want [test]", p.Tags)
	}
}

func TestLoadCanonical_InvalidFileSkipped(t *testing.T) {
	dir := t.TempDir()
	// Write a file with no frontmatter.
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte("just body, no frontmatter"), 0644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}
	writeCanonicalFile(t, dir, "good", "good profile", "Good body.")

	profiles := LoadCanonical(dir)
	if len(profiles) != 1 {
		t.Errorf("LoadCanonical: got %d profiles, want 1 (bad skipped)", len(profiles))
	}
}

func TestResolver_CanonicalOverridesLegacy(t *testing.T) {
	legacyDir := t.TempDir()
	writeAgentFile(t, legacyDir, "coder", "legacy coder desc", "Legacy coder body.")

	canonicalDir := t.TempDir()
	writeCanonicalFile(t, canonicalDir, "coder", "canonical coder desc", "Canonical coder body.")

	r := &Resolver{
		builtins:  builtinProfiles(),
		canonical: LoadCanonical(canonicalDir),
	}
	reg, err := agents.Load(legacyDir)
	if err != nil {
		t.Fatalf("Load legacy agents: %v", err)
	}
	r.agentsReg = reg

	p := r.Get("coder")
	if p == nil {
		t.Fatal("Get(coder) = nil")
	}
	if p.Description != "canonical coder desc" {
		t.Errorf("Description = %q, want canonical coder desc", p.Description)
	}
	if p.Kind != KindGlobal {
		t.Errorf("Kind = %q, want global", p.Kind)
	}
}

func TestResolver_CanonicalAppearsInList(t *testing.T) {
	canonicalDir := t.TempDir()
	writeCanonicalFile(t, canonicalDir, "reviewer", "reviews", "Reviewer prompt.")

	r := &Resolver{
		builtins:  builtinProfiles(),
		canonical: LoadCanonical(canonicalDir),
	}

	list := r.List()
	found := false
	for _, p := range list {
		if p.Name == "reviewer" {
			found = true
			break
		}
	}
	if !found {
		t.Error("reviewer not found in List()")
	}
}

func TestResolver_ListVisible_HidesPrivateProfilesForNonOwner(t *testing.T) {
	canonicalDir := t.TempDir()
	writeCanonicalFileWithPublic(t, canonicalDir, "public", "public desc", "Public prompt.", true)
	writeCanonicalFileWithPublic(t, canonicalDir, "secret", "secret desc", "Secret prompt.", false)

	r := &Resolver{builtins: builtinProfiles(), canonical: LoadCanonical(canonicalDir)}

	nonOwnerNames := map[string]bool{}
	for _, p := range r.ListVisible(false) {
		nonOwnerNames[p.Name] = true
	}
	if nonOwnerNames["secret"] {
		t.Fatal("non-owner visible list included private profile")
	}
	if !nonOwnerNames["public"] {
		t.Fatal("non-owner visible list omitted public profile")
	}

	ownerNames := map[string]bool{}
	for _, p := range r.ListVisible(true) {
		ownerNames[p.Name] = true
	}
	if !ownerNames["secret"] {
		t.Fatal("owner visible list omitted private profile")
	}
}

func TestResolver_ResolveEffectiveForUser_BlocksPrivateProfileForNonOwner(t *testing.T) {
	canonicalDir := t.TempDir()
	writeCanonicalFileWithPublic(t, canonicalDir, "secret", "secret desc", "Secret prompt.", false)
	r := &Resolver{builtins: builtinProfiles(), canonical: LoadCanonical(canonicalDir)}

	_, _, err := r.ResolveEffectiveForUser("@secret do work", "", 0, false)
	if _, ok := err.(*ErrProfileNotAllowed); !ok {
		t.Fatalf("ResolveEffectiveForUser non-owner err = %T %v, want ErrProfileNotAllowed", err, err)
	}

	profile, stripped, err := r.ResolveEffectiveForUser("@secret do work", "", 0, true)
	if err != nil {
		t.Fatalf("ResolveEffectiveForUser owner error = %v", err)
	}
	if profile == nil || profile.Name != "secret" || stripped != "do work" {
		t.Fatalf("owner resolve got profile=%v stripped=%q", profile, stripped)
	}
}

func writeUserPrivateFile(t *testing.T, dir, name, description, body string) {
	t.Helper()
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\nkind: prompt_profile\n---\n%s", name, description, body)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0600); err != nil {
		t.Fatalf("write user-private file %s: %v", name, err)
	}
}

func TestResolver_GetForUser_UserPrivateOverridesCanonical(t *testing.T) {
	root := t.TempDir()
	canonicalDir := filepath.Join(root, "profiles")
	if err := os.MkdirAll(canonicalDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeCanonicalFile(t, canonicalDir, "coder", "global coder", "Global coder prompt.")

	userProfilesDir := filepath.Join(root, "users", "42", "profiles")
	writeUserPrivateFile(t, userProfilesDir, "coder", "my coder", "My private coder prompt.")

	r, err := NewResolver("", canonicalDir, root)
	if err != nil {
		t.Fatal(err)
	}

	global := r.Get("coder")
	if global == nil || global.Prompt != "Global coder prompt." {
		t.Fatalf("Get(coder) = %v, want global prompt", global)
	}

	user := r.GetForUser(42, "coder")
	if user == nil {
		t.Fatal("GetForUser(42, coder) = nil")
	}
	if user.Kind != KindUser {
		t.Errorf("Kind = %q, want user", user.Kind)
	}
	if user.Prompt != "My private coder prompt." {
		t.Errorf("Prompt = %q, want private prompt", user.Prompt)
	}
}

func TestResolver_GetForUser_ModeOverlayMergesBuiltin(t *testing.T) {
	root := t.TempDir()
	userRes := users.NewResolver(root)
	modePath := userRes.UserModePath(42, "developer")
	if err := os.MkdirAll(filepath.Dir(modePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modePath, []byte("Always run tests before committing."), 0600); err != nil {
		t.Fatal(err)
	}

	r, err := NewResolver("", "", root)
	if err != nil {
		t.Fatal(err)
	}

	base := r.builtins["developer"]
	p := r.GetForUser(42, "developer")
	if p == nil {
		t.Fatal("GetForUser(42, developer) = nil")
	}
	if !strings.Contains(p.Prompt, base.Prompt) {
		t.Fatal("expected builtin developer prompt in merged result")
	}
	if !strings.Contains(p.Prompt, "Always run tests before committing.") {
		t.Fatal("expected mode overlay content in merged result")
	}
	if !strings.Contains(p.Prompt, modeOverlayHeader) {
		t.Fatal("expected mode overlay header in merged result")
	}
}

func TestResolver_GetForUser_UserPrivateSkipsModeOverlay(t *testing.T) {
	root := t.TempDir()
	userRes := users.NewResolver(root)
	modePath := userRes.UserModePath(42, "developer")
	if err := os.MkdirAll(filepath.Dir(modePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modePath, []byte("Overlay should not appear."), 0600); err != nil {
		t.Fatal(err)
	}

	userProfilesDir := userRes.UserProfilesDir(42)
	writeUserPrivateFile(t, userProfilesDir, "developer", "custom", "Fully custom developer profile.")

	r, err := NewResolver("", "", root)
	if err != nil {
		t.Fatal(err)
	}

	p := r.GetForUser(42, "developer")
	if p == nil {
		t.Fatal("GetForUser = nil")
	}
	if p.Prompt != "Fully custom developer profile." {
		t.Errorf("Prompt = %q, want user-private only", p.Prompt)
	}
	if strings.Contains(p.Prompt, "Overlay should not appear.") {
		t.Fatal("mode overlay leaked into user-private profile")
	}
}
