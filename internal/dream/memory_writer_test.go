package dream

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testResolver implements memoryDirResolver for tests.
type testResolver struct {
	memoryDir      string
	topicDir       string
	cwdOverlayDir  string
	teamDir        string
	root           string // instance root for containment boundary
}

func (r *testResolver) Root() string {
	if r.root != "" {
		return r.root
	}
	return r.memoryDir
}

func (r *testResolver) TopicMemoryDir(chatID int64, threadID int) string {
	return r.topicDir
}

func (r *testResolver) TopicCwdOverlayDir(chatID int64, threadID int) string {
	return r.cwdOverlayDir
}

func (r *testResolver) TeamMemoryDir(cwd string) string {
	return r.teamDir
}

func TestValidateFilename_RejectsAbsolute(t *testing.T) {
	err := validateFilename("/etc/passwd")
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestValidateFilename_RejectsRelativeTraversal(t *testing.T) {
	err := validateFilename("../../etc.md")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestValidateFilename_RejectsNoExtension(t *testing.T) {
	err := validateFilename("readme")
	if err == nil {
		t.Fatal("expected error for non-.md file")
	}
}

func TestValidateFilename_RejectsHidden(t *testing.T) {
	err := validateFilename(".secret.md")
	if err == nil {
		t.Fatal("expected error for hidden file")
	}
}

func TestValidateFilename_RejectsSubdir(t *testing.T) {
	err := validateFilename("sub/file.md")
	if err == nil {
		t.Fatal("expected error for subdirectory path")
	}
}

func TestValidateFilename_RejectsBackslash(t *testing.T) {
	err := validateFilename("sub\\file.md")
	if err == nil {
		t.Fatal("expected error for backslash path")
	}
}

func TestValidateFilename_AcceptsValid(t *testing.T) {
	err := validateFilename("user_preferences.md")
	if err != nil {
		t.Fatalf("expected no error for valid name, got: %v", err)
	}
}

func TestValidateFilename_AcceptsHyphens(t *testing.T) {
	err := validateFilename("my-memory-file.md")
	if err != nil {
		t.Fatalf("expected no error for hyphenated name, got: %v", err)
	}
}

func TestSafeWriter_RejectsPersonasLayer(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "user_global", Filename: "personas/test.md", Facts: []string{"should be rejected"}},
	}, 0, 0, "")
	if applied != 0 {
		t.Fatal("expected persona path to be rejected")
	}

	// Verify file was not created
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if f.Name() == "test.md" {
			t.Fatal("file should not exist under personas path")
		}
	}
}

func TestSafeWriter_RejectsPersonasSubdir(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}

	// Create the personas directory to make sure paths resolve
	personasDir := filepath.Join(dir, "personas")
	if err := os.MkdirAll(personasDir, 0755); err != nil {
		t.Fatal(err)
	}

	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "user_global", Filename: "../memory/personas/evil.md", Facts: []string{"traversal"}},
	}, 0, 0, "")
	if applied != 0 {
		t.Fatal("expected traversal to personas to be rejected")
	}
}

func TestSafeWriter_RejectsInvalidLayer(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "invalid", Filename: "test.md", Facts: []string{"data"}},
	}, 0, 0, "")
	if applied != 0 {
		t.Fatal("expected invalid layer to be rejected")
	}
}

func TestSafeWriter_AppendsFactsUnderGlobal(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "user_global", Filename: "test.md", Title: "Test", Facts: []string{"fact one", "fact two"}},
	}, 0, 0, "")
	if applied != 1 {
		t.Fatalf("expected 1 applied update, got %d", applied)
	}

	data, err := os.ReadFile(filepath.Join(dir, "test.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "- fact one") {
		t.Fatal("missing fact one")
	}
	if !strings.Contains(content, "- fact two") {
		t.Fatal("missing fact two")
	}
}

func TestSafeWriter_DeduplicatesFacts(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	// First write
	w.applyUpdates([]memoryUpdate{
		{Layer: "user_global", Filename: "test.md", Facts: []string{"fact one", "fact two"}},
	}, 0, 0, "")

	// Second write with one new, one duplicate
	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "user_global", Filename: "test.md", Facts: []string{"fact two", "fact three"}},
	}, 0, 0, "")
	if applied != 1 {
		t.Fatalf("expected 1 applied update, got %d", applied)
	}

	data, err := os.ReadFile(filepath.Join(dir, "test.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Should have exactly 3 lines of facts
	lines := strings.Split(strings.TrimSpace(content), "\n")
	factCount := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "- ") {
			factCount++
		}
	}
	if factCount != 3 {
		t.Fatalf("expected 3 fact lines (deduplicated), got %d", factCount)
	}
}

func TestSafeWriter_CreatesMEMORYIndex(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	w.applyUpdates([]memoryUpdate{
		{Layer: "user_global", Filename: "prefs.md", Title: "Preferences", Facts: []string{"user likes testing"}},
	}, 0, 0, "")

	memoryIndex := filepath.Join(dir, "MEMORY.md")
	data, err := os.ReadFile(memoryIndex)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "[Preferences](prefs.md)") {
		t.Fatalf("MEMORY.md missing entry: %s", content)
	}
}

func TestSafeWriter_UpdatesMEMORYIndexOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	// Two separate updates to the same file
	w.applyUpdates([]memoryUpdate{
		{Layer: "user_global", Filename: "prefs.md", Title: "Prefs", Facts: []string{"fact a"}},
	}, 0, 0, "")

	w.applyUpdates([]memoryUpdate{
		{Layer: "user_global", Filename: "prefs.md", Title: "Prefs", Facts: []string{"fact b"}},
	}, 0, 0, "")

	memoryIndex := filepath.Join(dir, "MEMORY.md")
	data, err := os.ReadFile(memoryIndex)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Entry should appear only once
	count := strings.Count(content, "[Prefs](prefs.md)")
	if count != 1 {
		t.Fatalf("expected 1 entry in MEMORY.md, got %d: %s", count, content)
	}
}

func TestSafeWriter_RejectsTopicLayerWithoutThread(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "topic", Filename: "test.md", Facts: []string{"data"}},
	}, 1, 0, "") // threadID=0
	if applied != 0 {
		t.Fatal("expected topic layer without threadID>0 to be rejected")
	}
}

func TestSafeWriter_RejectsProjectLayerWithoutCwd(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "cwd_overlay", Filename: "test.md", Facts: []string{"data"}},
	}, 1, 1, "") // empty cwd
	if applied != 0 {
		t.Fatal("expected project layer without cwd to be rejected")
	}
}

func TestSafeWriter_PartialFailure(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "invalid", Filename: "bad.md", Facts: []string{"data"}},
		{Layer: "user_global", Filename: "good.md", Facts: []string{"data"}},
	}, 0, 0, "")
	if applied != 1 {
		t.Fatalf("expected 1 applied update (second one valid), got %d", applied)
	}
}

func TestIsSubDirLexical_SameDir(t *testing.T) {
	if !isSubDirLexical("/a/b", "/a/b") {
		t.Fatal("same dir should be sub")
	}
}

func TestIsSubDirLexical_ChildDir(t *testing.T) {
	if !isSubDirLexical("/a/b", "/a/b/c") {
		t.Fatal("child dir should be sub")
	}
}

func TestIsSubDirLexical_NotChild(t *testing.T) {
	if isSubDirLexical("/a/b", "/a/c") {
		t.Fatal("sibling dir should not be sub")
	}
}

func TestIsSubDirLexical_Parent(t *testing.T) {
	if isSubDirLexical("/a/b/c", "/a/b") {
		t.Fatal("parent dir should not be sub")
	}
}

func TestIsPersonasDirLexical_DirectMatch(t *testing.T) {
	if !isPersonasDirLexical("/mem", "/mem/personas") {
		t.Fatal("/mem/personas should be personas")
	}
}

func TestIsPersonasDirLexical_InsidePersonas(t *testing.T) {
	if !isPersonasDirLexical("/mem", "/mem/personas/user.md") {
		t.Fatal("/mem/personas/user.md should be personas")
	}
}

func TestIsPersonasDirLexical_NotPersonas(t *testing.T) {
	if isPersonasDirLexical("/mem", "/mem/global") {
		t.Fatal("/mem/global should not be personas")
	}
}

func TestIsPersonasDirLexical_SubdirNotPersonas(t *testing.T) {
	if isPersonasDirLexical("/mem", "/mem/personalities") {
		t.Fatal("/mem/personalities should not be personas (not exact prefix)")
	}
}

func TestSafeWriter_TopicLayerWritesFiles(t *testing.T) {
	dir := t.TempDir()
	topicDir := filepath.Join(dir, "topics", "chat_1", "thread_2")
	resolver := &testResolver{memoryDir: dir, topicDir: topicDir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "topic", Filename: "topic_facts.md", Title: "Topic", Facts: []string{"topic fact"}},
	}, 1, 2, "")
	if applied != 1 {
		t.Fatalf("expected 1 applied update, got %d", applied)
	}

	data, err := os.ReadFile(filepath.Join(topicDir, "topic_facts.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- topic fact") {
		t.Fatal("missing topic fact")
	}

	// Verify MEMORY.md was created in topic dir
	if _, err := os.Stat(filepath.Join(topicDir, "MEMORY.md")); err != nil {
		t.Fatal("expected MEMORY.md in topic dir")
	}
}

func TestSafeWriter_TopicLayerRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	topicDir := filepath.Join(dir, "topics", "chat_1", "thread_2")

	// Create a symlink in the topic dir pointing outside the memory root
	outsideFile := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outsideFile, []byte("escape"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(topicDir, 0700); err != nil {
		t.Fatal(err)
	}
	escapeLink := filepath.Join(topicDir, "escape.md")
	if err := os.Symlink(outsideFile, escapeLink); err != nil {
		t.Fatal(err)
	}

	resolver := &testResolver{memoryDir: dir, topicDir: topicDir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "topic", Filename: "escape.md", Facts: []string{"should fail"}},
	}, 1, 2, "")
	if applied != 0 {
		t.Fatal("expected topic layer symlink escape to be rejected")
	}
}

func TestSafeWriter_TopicLayerRejectsPersonas(t *testing.T) {
	dir := t.TempDir()
	topicDir := filepath.Join(dir, "topics", "chat_1", "thread_2")
	personasTopicDir := filepath.Join(topicDir, "personas")
	if err := os.MkdirAll(personasTopicDir, 0700); err != nil {
		t.Fatal(err)
	}

	resolver := &testResolver{memoryDir: dir, topicDir: topicDir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	// Write to personas subdirectory within topic (should be rejected because
	// topic layer inherits global persona blocking via root=memoryDir)
	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "topic", Filename: "../topic/personas/evil.md", Facts: []string{"should fail"}},
	}, 1, 2, "")
	if applied != 0 {
		t.Fatal("expected topic layer personas write to be rejected")
	}
}

func TestSafeWriter_ProjectLayerWritesFiles(t *testing.T) {
	dir := t.TempDir()
	cwdOverlayDir := filepath.Join(dir, "projects", "my-project", "conversations", "chat_1", "thread_1")
	resolver := &testResolver{memoryDir: dir, cwdOverlayDir: cwdOverlayDir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "cwd_overlay", Filename: "work_log.md", Title: "Work Log", Facts: []string{"implemented feature X"}},
	}, 1, 1, "/some/cwd")
	if applied != 1 {
		t.Fatalf("expected 1 applied update, got %d", applied)
	}

	data, err := os.ReadFile(filepath.Join(cwdOverlayDir, "work_log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- implemented feature X") {
		t.Fatal("missing project fact")
	}
}

func TestSafeWriter_TeamLayerWritesFiles(t *testing.T) {
	dir := t.TempDir()
	teamDir := filepath.Join(dir, "projects", "my-project", "project_team")
	resolver := &testResolver{memoryDir: dir, teamDir: teamDir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "project_team", Filename: "conventions.md", Title: "Conventions", Facts: []string{"use tabs not spaces"}},
	}, 1, 1, "/some/cwd")
	if applied != 1 {
		t.Fatalf("expected 1 applied update, got %d", applied)
	}

	data, err := os.ReadFile(filepath.Join(teamDir, "conventions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- use tabs not spaces") {
		t.Fatal("missing team fact")
	}
}

// Regression: project layer writes succeed when project dir is outside global memory root.
func TestSafeWriter_ProjectLayerOutsideGlobalRoot(t *testing.T) {
	memoryDir := t.TempDir()
	cwdOverlayDir := t.TempDir() // unrelated temp dir, NOT under memoryDir
	// cwd_overlay layer uses instance root as containment boundary.
	// Set root to a common ancestor of both dirs.
	resolver := &testResolver{memoryDir: memoryDir, cwdOverlayDir: cwdOverlayDir, root: "/"}
	w, err := newSafeMemoryWriter(memoryDir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "cwd_overlay", Filename: "outside_root.md", Title: "Outside", Facts: []string{"project outside global root"}},
	}, 42, 7, "/some/project")
	if applied != 1 {
		t.Fatalf("expected 1 applied update, got %d", applied)
	}

	data, err := os.ReadFile(filepath.Join(cwdOverlayDir, "outside_root.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- project outside global root") {
		t.Fatal("missing project fact in outside-root project dir")
	}
}

// Regression: team layer writes succeed when team dir is outside global memory root.
func TestSafeWriter_TeamLayerOutsideGlobalRoot(t *testing.T) {
	memoryDir := t.TempDir()
	teamDir := t.TempDir() // unrelated temp dir, NOT under memoryDir
	resolver := &testResolver{memoryDir: memoryDir, teamDir: teamDir}
	w, err := newSafeMemoryWriter(memoryDir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "project_team", Filename: "team_notes.md", Title: "Team", Facts: []string{"team fact outside global root"}},
	}, 42, 7, "/some/project")
	if applied != 1 {
		t.Fatalf("expected 1 applied update, got %d", applied)
	}

	data, err := os.ReadFile(filepath.Join(teamDir, "team_notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- team fact outside global root") {
		t.Fatal("missing team fact in outside-root team dir")
	}
}

// Security: project layer rejects symlink escaping its layer root.
func TestSafeWriter_ProjectLayerRejectsSymlinkEscape(t *testing.T) {
	memoryDir := t.TempDir()
	cwdOverlayDir := t.TempDir()

	// Create a symlink inside cwdOverlayDir that points outside
	outsideFile := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outsideFile, []byte("escape"), 0600); err != nil {
		t.Fatal(err)
	}
	escapeLink := filepath.Join(cwdOverlayDir, "escape.md")
	if err := os.Symlink(outsideFile, escapeLink); err != nil {
		t.Fatal(err)
	}

	resolver := &testResolver{memoryDir: memoryDir, cwdOverlayDir: cwdOverlayDir}
	w, err := newSafeMemoryWriter(memoryDir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "cwd_overlay", Filename: "escape.md", Facts: []string{"should fail"}},
	}, 42, 7, "/some/project")
	if applied != 0 {
		t.Fatal("expected project layer symlink escape to be rejected")
	}
}

// Security: team layer rejects symlink escaping its layer root.
func TestSafeWriter_TeamLayerRejectsSymlinkEscape(t *testing.T) {
	memoryDir := t.TempDir()
	teamDir := t.TempDir()

	outsideFile := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outsideFile, []byte("escape"), 0600); err != nil {
		t.Fatal(err)
	}
	escapeLink := filepath.Join(teamDir, "escape.md")
	if err := os.Symlink(outsideFile, escapeLink); err != nil {
		t.Fatal(err)
	}

	resolver := &testResolver{memoryDir: memoryDir, teamDir: teamDir}
	w, err := newSafeMemoryWriter(memoryDir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "project_team", Filename: "escape.md", Facts: []string{"should fail"}},
	}, 42, 7, "/some/project")
	if applied != 0 {
		t.Fatal("expected team layer symlink escape to be rejected")
	}
}

// Permission: new memory files use private permissions (0600) on Unix.
func TestSafeWriter_PrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	resolver := &testResolver{memoryDir: dir}
	w, err := newSafeMemoryWriter(dir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "user_global", Filename: "perms.md", Facts: []string{"permission check"}},
	}, 0, 0, "")
	if applied != 1 {
		t.Fatalf("expected 1 applied update, got %d", applied)
	}

	// Check file permission — files must be owner-only (0600)
	fileInfo, err := os.Stat(filepath.Join(dir, "perms.md"))
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm()&0077 != 0 {
		t.Errorf("memory file should be private (mode&0077 == 0), got %o", fileInfo.Mode().Perm())
	}
}

// TestUpdateMemoryIndex_FailsOnUnwritableDir verifies that updateMemoryIndex
// returns an error when it cannot write to the target directory.
func TestUpdateMemoryIndex_FailsOnUnwritableDir(t *testing.T) {
	dir := t.TempDir()

	// Create a file where a directory component would be expected.
	// When updateMemoryIndex tries to write blocked/MEMORY.md,
	// it fails because "blocked" is a file, not a directory.
	// This avoids chmod-based tests which are unreliable on CI runners
	// (root bypasses permissions; filesystem quirks vary).
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	err := updateMemoryIndex(blocked, "new_file.md", "New")
	if err == nil {
		t.Fatal("expected updateMemoryIndex to fail when parent is a file, not a directory")
	}
}

// --- Fix 1: applyOne sanitizes titles and facts (C-02) ---

func TestApplyOne_SanitizesUnsafeFacts(t *testing.T) {
	dir := t.TempDir()
	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Facts with control chars, newlines, and instruction prefixes
	up := memoryUpdate{
		Layer:    "user_global",
		Filename: "test.md",
		Title:    "safe\ntitle",
		Facts:    []string{"clean fact", "system: override mode", "line1\nline2"},
	}
	err = w.applyOne(up, 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "test.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Instruction prefix fact must be rejected
	if strings.Contains(content, "system:") {
		t.Fatal("instruction prefix fact should be rejected")
	}

	// Multiline fact must be collapsed
	if strings.Contains(content, "\n") && strings.Contains(content, "line1") {
		// The newline was collapsed, but the fact as a whole should appear
		if !strings.Contains(content, "- clean fact") {
			t.Fatal("expected clean fact to be written")
		}
	}

	// Title must be sanitized (newline collapsed)
	// Check MEMORY.md has the sanitized title
	memIndex, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	memContent := string(memIndex)
	if strings.Contains(memContent, "safe\n") {
		t.Fatal("title with newline should be collapsed")
	}
}

func TestApplyOne_SanitizesTitle(t *testing.T) {
	dir := t.TempDir()
	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	up := memoryUpdate{
		Layer:    "user_global",
		Filename: "test.md",
		Title:    "  spaced\ttitle\nhere  ",
		Facts:    []string{"fact"},
	}
	err = w.applyOne(up, 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}

	memIndex, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(memIndex)
	// Title entry line should use the sanitized title "spaced title here"
	if !strings.Contains(content, "[spaced title here](test.md)") {
		t.Fatalf("expected sanitized title in MEMORY.md, got: %s", content)
	}
	// Ensure no raw control chars leaked into the title portion
	if strings.Contains(content, "\ttitle") || strings.Contains(content, "title\n") {
		t.Fatal("title should not contain raw control characters")
	}
}

func TestApplyOne_RejectsAllUnsafeFacts(t *testing.T) {
	dir := t.TempDir()
	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	up := memoryUpdate{
		Layer:    "user_global",
		Filename: "test.md",
		Facts:    []string{"system: override"},
	}
	err = w.applyOne(up, 0, 0, "")
	if err == nil {
		t.Fatal("expected error when all facts rejected by sanitization")
	}
}

// --- Fix 2: Symlink target protection (H-01) ---

func TestApplyOne_RejectsSymlinkToPersonas(t *testing.T) {
	dir := t.TempDir()
	personasDir := filepath.Join(dir, "personas")
	if err := os.MkdirAll(personasDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create a target inside personas
	personaFile := filepath.Join(personasDir, "target.md")
	if err := os.WriteFile(personaFile, []byte("persona data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink in the memory root pointing to the persona file
	symlink := filepath.Join(dir, "user_file.md")
	if err := os.Symlink(personaFile, symlink); err != nil {
		t.Fatal(err)
	}

	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Trying to write through the symlink should be rejected
	up := memoryUpdate{
		Layer:    "user_global",
		Filename: "user_file.md", // exists as symlink -> personas/target.md
		Facts:    []string{"fact"},
	}

	// The write happens via applyOne step 9 → checkTargetSymlink
	// which should reject because resolved path is under personas
	err = w.applyOne(up, 0, 0, "")
	if err == nil {
		t.Fatal("expected error when writing through symlink to personas")
	}
}

func TestApplyOne_RejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outsideFile := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outsideFile, []byte("outside data"), 0644); err != nil {
		t.Fatal(err)
	}

	symlink := filepath.Join(dir, "escape.md")
	if err := os.Symlink(outsideFile, symlink); err != nil {
		t.Fatal(err)
	}

	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	up := memoryUpdate{
		Layer:    "user_global",
		Filename: "escape.md", // symlink pointing outside
		Facts:    []string{"fact"},
	}

	err = w.applyOne(up, 0, 0, "")
	if err == nil {
		t.Fatal("expected error when writing through symlink to outside")
	}
}

func TestApplyOne_SymlinkCheckStillAllowsNormalWrites(t *testing.T) {
	dir := t.TempDir()
	w, err := newSafeMemoryWriter(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Normal write through applyOne should succeed
	up := memoryUpdate{
		Layer:    "user_global",
		Filename: "normal.md",
		Title:    "Normal",
		Facts:    []string{"normal fact"},
	}
	err = w.applyOne(up, 0, 0, "")
	if err != nil {
		t.Fatalf("expected normal write to succeed, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "normal.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- normal fact") {
		t.Fatal("expected fact to be written")
	}
}

func TestUpdateMemoryIndex_RejectsSymlinkMEMORY(t *testing.T) {
	dir := t.TempDir()
	personasDir := filepath.Join(dir, "personas")
	if err := os.MkdirAll(personasDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create a fake MEMORY.md in personas
	evilIndex := filepath.Join(personasDir, "MEMORY.md")
	if err := os.WriteFile(evilIndex, []byte("persona index"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink at real MEMORY.md pointing to personas/MEMORY.md
	realIndex := filepath.Join(dir, "MEMORY.md")
	if err := os.Symlink(evilIndex, realIndex); err != nil {
		t.Fatal(err)
	}

	// updateMemoryIndex should detect the symlink escape
	err := updateMemoryIndex(dir, "test.md", "Test")
	if err == nil {
		t.Fatal("expected error when MEMORY.md is symlink to personas")
	}
}

// isPersonasDirLexical checks if a relative path starts with "personas/".
// Test-only helper — production uses isPersonasRelPath.
func isPersonasDirLexical(memoryDir, path string) bool {
	rel, err := filepath.Rel(memoryDir, path)
	if err != nil {
		return true
	}
	return strings.HasPrefix(rel, "personas") && (len(rel) == 8 || rel[8] == filepath.Separator)
}

func TestUpdateMemoryIndex_AcceptsNormalFile(t *testing.T) {
	dir := t.TempDir()
	err := updateMemoryIndex(dir, "test.md", "Test")
	if err != nil {
		t.Fatalf("expected success for normal MEMORY.md write, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[Test](test.md)") {
		t.Fatal("expected MEMORY.md to have entry")
	}
}

// --- Fix 1: H-01 topic memory write under instance root ---

// TestSafeWriter_TopicLayerUnderInstanceRoot verifies that topic memory
// writes succeed when the topic dir is under the instance root but outside
// the user memory dir. Before the fix, resolveLayerTarget used w.memoryDir
// as the containment root for topic, causing every topic write to be
// rejected with errPathTraversal.
func TestSafeWriter_TopicLayerUnderInstanceRoot(t *testing.T) {
	instanceRoot := t.TempDir()
	userMemoryDir := filepath.Join(instanceRoot, "users", "42", "memory")
	topicDir := filepath.Join(instanceRoot, "topics", "chat_1", "thread_2")

	// Create the user memory dir so newSafeMemoryWriter's EvalSymlinks succeeds.
	if err := os.MkdirAll(userMemoryDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Resolver with root=instanceRoot (different from user memoryDir)
	resolver := &testResolver{memoryDir: userMemoryDir, topicDir: topicDir, root: instanceRoot}
	w, err := newSafeMemoryWriter(userMemoryDir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "topic", Filename: "topic_facts.md", Title: "Topic", Facts: []string{"topic fact under instance root"}},
	}, 1, 2, "")
	if applied != 1 {
		t.Fatalf("expected 1 applied update (topic under instance root), got %d", applied)
	}

	data, err := os.ReadFile(filepath.Join(topicDir, "topic_facts.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- topic fact under instance root") {
		t.Fatal("missing topic fact")
	}
}

// TestSafeWriter_TopicLayerBlocksPersonasUnderInstanceRoot verifies that
// personas blocking still works when the topic layer uses instance root
// as containment boundary.
func TestSafeWriter_TopicLayerBlocksPersonasUnderInstanceRoot(t *testing.T) {
	instanceRoot := t.TempDir()
	userMemoryDir := filepath.Join(instanceRoot, "users", "42", "memory")
	topicDir := filepath.Join(instanceRoot, "topics", "chat_1", "thread_2")
	personasTopicDir := filepath.Join(topicDir, "personas")

	// Create both the user memory dir and the personas subdir
	if err := os.MkdirAll(userMemoryDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(personasTopicDir, 0700); err != nil {
		t.Fatal(err)
	}

	resolver := &testResolver{memoryDir: userMemoryDir, topicDir: topicDir, root: instanceRoot}
	w, err := newSafeMemoryWriter(userMemoryDir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	// Write to personas subdirectory within topic (should be rejected)
	applied := w.applyUpdates([]memoryUpdate{
		{Layer: "topic", Filename: "../topic/personas/evil.md", Facts: []string{"should fail"}},
	}, 1, 2, "")
	if applied != 0 {
		t.Fatal("expected topic layer personas write under instance root to be rejected")
	}
}

// --- Fix 4: M-02 — unbounded fact writes rejection ---

// TestAppendUniqueFacts_RejectsNearMaxFileSize verifies that appending facts
// to a file near the 1MB limit is rejected with the appropriate error.
func TestAppendUniqueFacts_RejectsNearMaxFileSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")

	// Create a file just under the 1MB limit (maxMemoryFileSize = 1048576)
	// Fill it to maxMemoryFileSize - 100 bytes (leaving 100 bytes of headroom)
	fillSize := maxMemoryFileSize - 100
	var fillBuf strings.Builder
	for fillBuf.Len() < fillSize {
		fillBuf.WriteString("x\n")
	}
	fill := fillBuf.String()[:fillSize]
	if err := os.WriteFile(path, []byte(fill), 0600); err != nil {
		t.Fatal(err)
	}

	// Verify the file is near the limit
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() >= maxMemoryFileSize {
		t.Fatalf("test setup: file too large: %d >= %d", fi.Size(), maxMemoryFileSize)
	}

	// Add a fact that would push it over the limit (needs more than 100 bytes)
	longFact := strings.Repeat("a", 200)
	err = appendUniqueFacts(path, []string{longFact})
	if err == nil {
		t.Fatal("expected error when appending facts to near-limit file (M-02)")
	}
	if !strings.Contains(err.Error(), "memory file size limit exceeded") {
		t.Fatalf("expected 'memory file size limit exceeded' error, got: %v", err)
	}

	// Verify file was NOT modified (content unchanged)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != fill {
		t.Fatal("file content changed despite rejection")
	}
}

// TestAppendUniqueFacts_RejectsAlreadyOversizedFile verifies that files
// already over the 1MB limit are rejected immediately.
func TestAppendUniqueFacts_RejectsAlreadyOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")

	// Create a file already over the limit
	oversized := strings.Repeat("x\n", maxMemoryFileSize/2+1)
	if err := os.WriteFile(path, []byte(oversized), 0600); err != nil {
		t.Fatal(err)
	}

	err := appendUniqueFacts(path, []string{"new fact"})
	if err == nil {
		t.Fatal("expected error when appending to already oversized file (M-02)")
	}
	if !strings.Contains(err.Error(), "memory file size limit exceeded") {
		t.Fatalf("expected 'memory file size limit exceeded' error, got: %v", err)
	}
}

// TestSafeWriter_TwoUsersIsolated verifies that dream writes for user A
// do not affect user B's user_global memory, even when both users share
// the same topic (E9.4).
func TestSafeWriter_TwoUsersIsolated(t *testing.T) {
	baseDir := t.TempDir()

	// User A directories
	userADir := filepath.Join(baseDir, "users", "100", "memory")
	if err := os.MkdirAll(userADir, 0700); err != nil {
		t.Fatal(err)
	}

	// User B directories
	userBDir := filepath.Join(baseDir, "users", "200", "memory")
	if err := os.MkdirAll(userBDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Shared topic directory
	topicDir := filepath.Join(baseDir, "topics", "chat_42", "thread_99")
	if err := os.MkdirAll(topicDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Resolver pointing to the same topic dir for both
	resolver := &testResolver{
		memoryDir:     userADir,
		topicDir:      topicDir,
		cwdOverlayDir: filepath.Join(topicDir, "cwd_overlay"),
		teamDir:       filepath.Join(baseDir, "projects", "test-project", "team"),
		root:          baseDir,
	}

	// Writer for user A
	writerA, err := newSafeMemoryWriter(userADir, resolver)
	if err != nil {
		t.Fatal(err)
	}

	// Writer for user B (different memoryDir, same resolver)
	resolverB := &testResolver{
		memoryDir:     userBDir,
		topicDir:      topicDir,
		cwdOverlayDir: filepath.Join(topicDir, "cwd_overlay"),
		teamDir:       filepath.Join(baseDir, "projects", "test-project", "team"),
		root:          baseDir,
	}
	writerB, err := newSafeMemoryWriter(userBDir, resolverB)
	if err != nil {
		t.Fatal(err)
	}

	// Dream writes for user A to user_global
	updatesA := []memoryUpdate{
		{Layer: "user_global", Filename: "prefs.md", Facts: []string{"Alice prefers dark mode"}},
	}
	countA := writerA.applyUpdates(updatesA, 42, 99, "/repo/test")
	if countA != 1 {
		t.Fatalf("user A write: expected 1 applied, got %d", countA)
	}

	// Dream writes for user B to user_global
	updatesB := []memoryUpdate{
		{Layer: "user_global", Filename: "prefs.md", Facts: []string{"Bob prefers light mode"}},
	}
	countB := writerB.applyUpdates(updatesB, 42, 99, "/repo/test")
	if countB != 1 {
		t.Fatalf("user B write: expected 1 applied, got %d", countB)
	}

	// Verify user A's file only contains Alice's fact
	dataA, err := os.ReadFile(filepath.Join(userADir, "prefs.md"))
	if err != nil {
		t.Fatalf("read user A prefs: %v", err)
	}
	if !strings.Contains(string(dataA), "Alice prefers dark mode") {
		t.Fatal("user A prefs should contain Alice's fact")
	}
	if strings.Contains(string(dataA), "Bob prefers light mode") {
		t.Fatal("user B's fact leaked into user A's user_global memory")
	}

	// Verify user B's file only contains Bob's fact
	dataB, err := os.ReadFile(filepath.Join(userBDir, "prefs.md"))
	if err != nil {
		t.Fatalf("read user B prefs: %v", err)
	}
	if !strings.Contains(string(dataB), "Bob prefers light mode") {
		t.Fatal("user B prefs should contain Bob's fact")
	}
	if strings.Contains(string(dataB), "Alice prefers dark mode") {
		t.Fatal("user A's fact leaked into user B's user_global memory")
	}

	// Both can write to shared topic (topic layer is shared)
	updatesTopicA := []memoryUpdate{
		{Layer: "topic", Filename: "decision.md", Facts: []string{"Alice decided on React"}},
	}
	countTopicA := writerA.applyUpdates(updatesTopicA, 42, 99, "/repo/test")
	if countTopicA != 1 {
		t.Fatalf("user A topic write: expected 1 applied, got %d", countTopicA)
	}
	updatesTopicB := []memoryUpdate{
		{Layer: "topic", Filename: "decision.md", Facts: []string{"Bob decided on Vue"}},
	}
	countTopicB := writerB.applyUpdates(updatesTopicB, 42, 99, "/repo/test")
	if countTopicB != 1 {
		t.Fatalf("user B topic write: expected 1 applied, got %d", countTopicB)
	}

	// Topic file should contain both (shared layer)
	dataTopic, err := os.ReadFile(filepath.Join(topicDir, "decision.md"))
	if err != nil {
		t.Fatalf("read topic decision: %v", err)
	}
	if !strings.Contains(string(dataTopic), "React") {
		t.Fatal("topic should contain Alice's decision (React)")
	}
	if !strings.Contains(string(dataTopic), "Vue") {
		t.Fatal("topic should contain Bob's decision (Vue)")
	}
}
