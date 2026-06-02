package telegram

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/users"
)

// --- cwdSetTarget tests ---

func TestParseCwdSetTarget_GroupFlag(t *testing.T) {
	t.Parallel()

	target, err := parseCwdSetTarget("--group /repo", 59)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 0 {
		t.Errorf("ThreadID = %d, want 0", target.ThreadID)
	}
	if target.Path != "/repo" {
		t.Errorf("Path = %q, want %q", target.Path, "/repo")
	}
	if target.Scope != "group" {
		t.Errorf("Scope = %q, want %q", target.Scope, "group")
	}
	if !target.Explicit {
		t.Error("Explicit should be true")
	}
}

func TestParseCwdSetTarget_GroupFlagWithWhitespace(t *testing.T) {
	t.Parallel()

	target, err := parseCwdSetTarget("  --group   /Volumes/Dados/Workspace  ", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 0 {
		t.Errorf("ThreadID = %d, want 0", target.ThreadID)
	}
	if target.Path != "/Volumes/Dados/Workspace" {
		t.Errorf("Path = %q, want %q", target.Path, "/Volumes/Dados/Workspace")
	}
	if target.Scope != "group" {
		t.Errorf("Scope = %q, want %q", target.Scope, "group")
	}
	if !target.Explicit {
		t.Error("Explicit should be true")
	}
}

func TestParseCwdSetTarget_GroupFlagMissingPath(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("--group", 42)
	if err == nil {
		t.Fatal("expected error for missing path after --group")
	}
}

func TestParseCwdSetTarget_GroupFlagOnlyWhitespace(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("--group  ", 42)
	if err == nil {
		t.Fatal("expected error for whitespace-only path after --group")
	}
}

func TestParseCwdSetTarget_TopicFlag(t *testing.T) {
	t.Parallel()

	target, err := parseCwdSetTarget("--topic /my/topic/path", 59)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 59 {
		t.Errorf("ThreadID = %d, want 59", target.ThreadID)
	}
	if target.Path != "/my/topic/path" {
		t.Errorf("Path = %q, want %q", target.Path, "/my/topic/path")
	}
	if target.Scope != "topic" {
		t.Errorf("Scope = %q, want %q", target.Scope, "topic")
	}
	if !target.Explicit {
		t.Error("Explicit should be true")
	}
}

func TestParseCwdSetTarget_TopicFlagMissingPath(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("--topic", 99)
	if err == nil {
		t.Fatal("expected error for missing path after --topic")
	}
}

func TestParseCwdSetTarget_TopicFlagWhitespaceOnly(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("--topic ", 99)
	if err == nil {
		t.Fatal("expected error for whitespace-only path after --topic")
	}
}

func TestParseCwdSetTarget_NoFlagInTopic(t *testing.T) {
	t.Parallel()

	target, err := parseCwdSetTarget("/repo/path", 59)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 59 {
		t.Errorf("ThreadID = %d, want 59", target.ThreadID)
	}
	if target.Path != "/repo/path" {
		t.Errorf("Path = %q, want %q", target.Path, "/repo/path")
	}
	if target.Scope != "topic" {
		t.Errorf("Scope = %q, want %q", target.Scope, "topic")
	}
	if target.Explicit {
		t.Error("Explicit should be false when no flag")
	}
}

func TestParseCwdSetTarget_NoFlagInGeneralChat(t *testing.T) {
	t.Parallel()

	target, err := parseCwdSetTarget("/repo/path", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 0 {
		t.Errorf("ThreadID = %d, want 0", target.ThreadID)
	}
	if target.Path != "/repo/path" {
		t.Errorf("Path = %q, want %q", target.Path, "/repo/path")
	}
	if target.Scope != "group" {
		t.Errorf("Scope = %q, want %q", target.Scope, "group")
	}
	if target.Explicit {
		t.Error("Explicit should be false when no flag")
	}
}

func TestParseCwdSetTarget_EmptyArgs(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("", 0)
	if err == nil {
		t.Fatal("expected error for empty args")
	}
}

func TestParseCwdSetTarget_WhitespaceOnly(t *testing.T) {
	t.Parallel()

	_, err := parseCwdSetTarget("   ", 0)
	if err == nil {
		t.Fatal("expected error for whitespace-only args")
	}
}

func TestBuildCwdStatusText_NoBindingShowsKnownProjectsAsSuggestions(t *testing.T) {
	t.Parallel()

	known := []projectbinding.ProjectBinding{
		{CWD: "/repo/aurelia", ProjectSlug: "aurelia"},
		{CWD: "/repo/other", ProjectSlug: "other"},
	}

	got := buildCwdStatusText("/tmp/bot", "", "", "", 0, known)

	// Must mention known projects as suggestions, not active cwd
	if !strings.Contains(got, "sugestões") && !strings.Contains(got, "suggestions") {
		t.Fatal("expected suggestions/sugestões wording for known projects")
	}
	if !strings.Contains(got, "NOT the active operational cwd") {
		t.Fatal("expected 'NOT the active operational cwd' distinction")
	}
	if !strings.Contains(got, "/repo/aurelia") {
		t.Fatal("expected /repo/aurelia in known projects list")
	}
	if !strings.Contains(got, "/repo/other") {
		t.Fatal("expected /repo/other in known projects list")
	}
}

func TestBuildCwdStatusText_NoKnownProjectsOmitsSuggestions(t *testing.T) {
	t.Parallel()

	got := buildCwdStatusText("/tmp/bot", "", "", "", 0, nil)

	// No known projects → should not include suggestions section
	if strings.Contains(got, "sugestões") || strings.Contains(got, "suggestions") {
		t.Fatal("should NOT mention suggestions when no known projects")
	}
	// Should still include the active cwd distinction
	if !strings.Contains(got, "NOT the active operational cwd") {
		t.Fatal("expected 'NOT the active operational cwd' distinction even without known projects")
	}
}

func TestBuildCwdStatusText_GroupBindingShowsEffectiveCwd(t *testing.T) {
	t.Parallel()

	got := buildCwdStatusText("/tmp/bot", "/repo/group", "", "", 0, nil)

	// Group cwd is set, so no "no cwd" guidance
	if strings.Contains(got, "NOT the active operational cwd") {
		t.Fatal("should NOT show no-cwd guidance when group cwd is set")
	}
	// Should show the group cwd
	if !strings.Contains(got, "/repo/group") {
		t.Fatal("expected /repo/group in status output")
	}
}

func TestBuildCwdStatusText_TopicInheritsGroup(t *testing.T) {
	t.Parallel()

	got := buildCwdStatusText("/tmp/bot", "/repo/group", "", "", 99, nil)

	// Topic inherited from group
	if !strings.Contains(got, "inherited from group") {
		t.Fatal("expected 'inherited from group' wording for topic")
	}
	// No "no cwd" guidance
	if strings.Contains(got, "NOT the active operational cwd") {
		t.Fatal("should NOT show no-cwd guidance when group cwd exists")
	}
}

func TestBuildCwdStatusText_TopicOverridesGroup(t *testing.T) {
	t.Parallel()

	got := buildCwdStatusText("/tmp/bot", "/repo/group", "/repo/topic", "", 99, nil)

	if !strings.Contains(got, "/repo/topic") {
		t.Fatal("expected topic cwd in status output")
	}
	if !strings.Contains(got, "overrides group") {
		t.Fatal("expected 'overrides group' wording for topic override")
	}
}

func TestBuildCwdStatusText_DeduplicatesKnownProjects(t *testing.T) {
	t.Parallel()

	known := []projectbinding.ProjectBinding{
		{CWD: "/repo/aurelia", ProjectSlug: "slug1"},
		{CWD: "/repo/aurelia", ProjectSlug: "slug2"},
		{CWD: "/repo/unique", ProjectSlug: "unique"},
	}

	got := buildCwdStatusText("/tmp/bot", "", "", "", 0, known)

	// Count occurrences of /repo/aurelia in the output
	count := strings.Count(got, "/repo/aurelia")
	if count != 1 {
		t.Fatalf("expected 1 occurrence of /repo/aurelia (deduplicated), got %d", count)
	}
	if !strings.Contains(got, "/repo/unique") {
		t.Fatal("expected /repo/unique in known projects")
	}
}

func TestBuildCwdStatusText_AgentBindingShowsHighestPriority(t *testing.T) {
	t.Parallel()

	got := buildCwdStatusText("/tmp/bot", "/repo/group", "", "/repo/agent", 99, nil)

	if !strings.Contains(got, "/repo/agent") {
		t.Fatal("expected agent cwd in status output")
	}
	if !strings.Contains(got, "highest priority") {
		t.Fatal("expected 'highest priority' wording")
	}
	// No "no cwd" guidance since agent has cwd
	if strings.Contains(got, "NOT the active operational cwd") {
		t.Fatal("should NOT show no-cwd guidance when agent cwd is set")
	}
}

func TestParseCwdSetTarget_QuotedPathAfterFlag(t *testing.T) {
	t.Parallel()

	// Backtick-quoted path after --group flag
	target, err := parseCwdSetTarget("--group `/repo with spaces`", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 0 {
		t.Errorf("ThreadID = %d, want 0", target.ThreadID)
	}
	if target.Path != "`/repo with spaces`" {
		t.Errorf("Path = %q, want %q", target.Path, "`/repo with spaces`")
	}
}

func TestParseCwdSetTarget_FlagOnlyInPathPosition(t *testing.T) {
	t.Parallel()

	// --group in middle of string should be treated as path (not parsed as flag)
	target, err := parseCwdSetTarget("/some/path --group", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.ThreadID != 42 {
		t.Errorf("ThreadID = %d, want 42", target.ThreadID)
	}
	if target.Path != "/some/path --group" {
		t.Errorf("Path = %q, want %q", target.Path, "/some/path --group")
	}
}

func TestParseCwdSetTarget_GroupprefixNoMatch(t *testing.T) {
	t.Parallel()

	// --grouppath (no space) should NOT match --group flag
	target, err := parseCwdSetTarget("--grouppath", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Path != "--grouppath" {
		t.Errorf("Path = %q, want %q", target.Path, "--grouppath")
	}
	if target.ThreadID != 42 {
		t.Errorf("ThreadID = %d, want 42", target.ThreadID)
	}
}

func TestParseCwdSetTarget_TopicPrefixNoMatch(t *testing.T) {
	t.Parallel()

	// --topicsomething should NOT match --topic flag
	target, err := parseCwdSetTarget("--topicsomething", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Path != "--topicsomething" {
		t.Errorf("Path = %q, want %q", target.Path, "--topicsomething")
	}
}

// --- cwdClearThread tests ---

func TestCwdClearThread_Clear(t *testing.T) {
	t.Parallel()

	threadID, ok, err := cwdClearThread("clear", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if threadID != 42 {
		t.Errorf("threadID = %d, want 42", threadID)
	}
}

func TestCwdClearThread_ClearGroup(t *testing.T) {
	t.Parallel()

	threadID, ok, err := cwdClearThread("clear --group", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if threadID != 0 {
		t.Errorf("threadID = %d, want 0", threadID)
	}
}

func TestCwdClearThread_ClearTopic(t *testing.T) {
	t.Parallel()

	threadID, ok, err := cwdClearThread("clear --topic", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if threadID != 42 {
		t.Errorf("threadID = %d, want 42", threadID)
	}
}

func TestCwdClearThread_ClearUnknownFlag(t *testing.T) {
	t.Parallel()

	_, _, err := cwdClearThread("clear --invalid", 42)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestCwdClearThread_NotClear(t *testing.T) {
	t.Parallel()

	_, ok, err := cwdClearThread("/some/path", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for non-clear args")
	}
}

// --- currentCwdForContext tests (DefaultCWD fallback) ---

// newUserStoreWithDefaultCWD creates a users.Store rooted at t.TempDir() with a
// profile whose DefaultCWD is set to the given path. Returns the store and the
// profile's userID.
func newUserStoreWithDefaultCWD(t *testing.T, defaultCWD string) *users.Store {
	t.Helper()
	dir := t.TempDir()
	resolver := users.NewResolver(dir)
	store := users.NewStore(resolver)
	profile := &users.Profile{
		UserID:     1,
		Name:       "Alice",
		Language:   "pt",
		DefaultCWD: defaultCWD,
	}
	if err := store.Save(profile); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	return store
}

func TestCurrentCwdForContext_DefaultCWDInPrivateChat(t *testing.T) {
	t.Parallel()

	// Create a real directory that DefaultCWD will point to.
	repoDir := t.TempDir()
	store := newUserStoreWithDefaultCWD(t, repoDir)

	bc := &BotController{userStore: store}
	// Private chat, no topic, no /cwd binding, no session CWD → DefaultCWD wins.
	got := bc.currentCwdForContext(42, 0, 1, true)
	if got != repoDir {
		t.Fatalf("currentCwdForContext() = %q, want DefaultCWD %q", got, repoDir)
	}
}

func TestCurrentCwdForContext_DefaultCWDIgnoredInGroupChat(t *testing.T) {
	t.Parallel()

	// DefaultCWD is a per-user convenience for private chats only.
	// Group / supergroup / topic must NOT see it.
	repoDir := t.TempDir()
	store := newUserStoreWithDefaultCWD(t, repoDir)

	bc := &BotController{userStore: store}
	got := bc.currentCwdForContext(42, 0, 1, false)
	if got != "" {
		t.Fatalf("currentCwdForContext(group) = %q, want empty", got)
	}
}

func TestCurrentCwdForContext_DefaultCWDIgnoredInTopic(t *testing.T) {
	t.Parallel()

	// Topic chat (threadID > 0) in a private chat: still must NOT use DefaultCWD.
	// Per spec, DefaultCWD is the fallback for private chats with no topic only.
	repoDir := t.TempDir()
	store := newUserStoreWithDefaultCWD(t, repoDir)

	bc := &BotController{userStore: store}
	got := bc.currentCwdForContext(42, 99, 1, true)
	if got != "" {
		t.Fatalf("currentCwdForContext(topic) = %q, want empty", got)
	}
}

func TestCurrentCwdForContext_DefaultCWDInvalidPathIgnored(t *testing.T) {
	t.Parallel()

	// A profile with a DefaultCWD pointing at a non-existent path must log
	// and return "" — never propagate a broken path to the bridge.
	missing := filepath.Join(t.TempDir(), "does", "not", "exist")
	store := newUserStoreWithDefaultCWD(t, missing)

	bc := &BotController{userStore: store}
	got := bc.currentCwdForContext(42, 0, 1, true)
	if got != "" {
		t.Fatalf("currentCwdForContext(invalid path) = %q, want empty", got)
	}
}

func TestCurrentCwdForContext_NoUserStoreReturnsEmpty(t *testing.T) {
	t.Parallel()

	// No userStore → no DefaultCWD lookup → empty.
	bc := &BotController{}
	got := bc.currentCwdForContext(42, 0, 1, true)
	if got != "" {
		t.Fatalf("currentCwdForContext() = %q, want empty", got)
	}
}

func TestCurrentCwdForContext_DefaultCWDIsRegularDirectory(t *testing.T) {
	t.Parallel()

	// Defensive: DefaultCWD must be a directory, not a regular file.
	filePath := filepath.Join(t.TempDir(), "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newUserStoreWithDefaultCWD(t, filePath)

	bc := &BotController{userStore: store}
	got := bc.currentCwdForContext(42, 0, 1, true)
	if got != "" {
		t.Fatalf("currentCwdForContext(file) = %q, want empty (file is not a directory)", got)
	}
}
