package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/igormaneschy/aurelia/internal/ipc"
)

func testDocContent() []byte {
	return []byte("# Test Document\n\nThis is a test document for attachment tests.")
}

func TestAttachDocumentFromPath_ValidFile(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(path, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachDocumentFromPath(path)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if len(m.pendingAttachments) != 1 {
		t.Fatalf("expected 1 pending attachment, got %d", len(m.pendingAttachments))
	}
	if m.pendingAttachments[0].name != "spec.md" {
		t.Errorf("name = %q, want %q", m.pendingAttachments[0].name, "spec.md")
	}
}

func TestAttachDocumentFromPath_NotFound(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	errMsg := m.attachDocumentFromPath("/nonexistent/doc.md")
	if errMsg == "" {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(errMsg, "not found") && !strings.Contains(errMsg, "Not found") {
		t.Errorf("expected 'not found' in error, got: %s", errMsg)
	}
	if len(m.pendingAttachments) != 0 {
		t.Errorf("expected 0 pending attachments, got %d", len(m.pendingAttachments))
	}
}

func TestAttachDocumentFromPath_SymlinkRejected(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	if err := os.WriteFile(target, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachDocumentFromPath(link)
	if errMsg == "" {
		t.Fatal("expected error for symlink")
	}
	if !strings.Contains(errMsg, "Symlink") {
		t.Errorf("expected 'Symlink' in error, got: %s", errMsg)
	}
	if len(m.pendingAttachments) != 0 {
		t.Errorf("expected 0 pending attachments, got %d", len(m.pendingAttachments))
	}
}

func TestAttachDocumentFromPath_Directory(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachDocumentFromPath(subdir)
	if errMsg == "" {
		t.Fatal("expected error for directory")
	}
	if !strings.Contains(errMsg, "Not a regular file") {
		t.Errorf("expected 'Not a regular file' in error, got: %s", errMsg)
	}
	if len(m.pendingAttachments) != 0 {
		t.Errorf("expected 0 pending attachments, got %d", len(m.pendingAttachments))
	}
}

func TestAttachDocumentFromPath_TooLarge(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "large.pdf")
	// Create a file larger than MaxAttachmentBytes (25MB).
	size := int64(ipc.MaxAttachmentBytes) + 1
	data := make([]byte, size)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachDocumentFromPath(path)
	if errMsg == "" {
		t.Fatal("expected error for too large file")
	}
	if !strings.Contains(errMsg, "too large") && !strings.Contains(errMsg, "Limit") {
		t.Errorf("expected size error, got: %s", errMsg)
	}
	if len(m.pendingAttachments) != 0 {
		t.Errorf("expected 0 pending attachments, got %d", len(m.pendingAttachments))
	}
}

func TestTryParseAsDocumentPath_ValidPDF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}

	name, resolved, ok := tryParseAsDocumentPath(path)
	if !ok {
		t.Fatal("expected ok=true for valid pdf path")
	}
	if name != "doc.pdf" {
		t.Errorf("name = %q, want %q", name, "doc.pdf")
	}
	if resolved != path {
		t.Errorf("path = %q, want %q", resolved, path)
	}
}

func TestTryParseAsDocumentPath_ImagePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, ok := tryParseAsDocumentPath(path)
	if ok {
		t.Error("expected ok=false for image path")
	}
}

func TestTryParseAsDocumentPath_NonExistent(t *testing.T) {
	_, _, ok := tryParseAsDocumentPath("/nonexistent/file.pdf")
	if ok {
		t.Error("expected ok=false for nonexistent path")
	}
}

func TestTryParseAsDocumentPath_PlainText(t *testing.T) {
	_, _, ok := tryParseAsDocumentPath("hello world")
	if ok {
		t.Error("expected ok=false for plain text")
	}
}

func TestTryParseAsDocumentPath_RelativePath(t *testing.T) {
	_, _, ok := tryParseAsDocumentPath("relative/path/doc.pdf")
	if ok {
		t.Error("expected ok=false for relative path")
	}
}

func TestTryParseAsDocumentPath_Symlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	if err := os.WriteFile(target, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, _, ok := tryParseAsDocumentPath(link)
	if ok {
		t.Error("expected ok=false for symlink")
	}
}

func TestTryParseAsDocumentPath_Directory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, ok := tryParseAsDocumentPath(subdir)
	if ok {
		t.Error("expected ok=false for directory")
	}
}

func TestClearPendingAttachments(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}

	_ = m.attachDocumentFromPath(path)
	if len(m.pendingAttachments) != 1 {
		t.Fatalf("expected 1 pending attachment, got %d", len(m.pendingAttachments))
	}

	m.clearPendingAttachments()
	if len(m.pendingAttachments) != 0 {
		t.Errorf("expected 0 pending attachments after clear, got %d", len(m.pendingAttachments))
	}
}

func TestPendingAttachmentBadges_Empty(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	badges := m.pendingAttachmentBadges()
	if badges != "" {
		t.Errorf("expected empty badges, got %q", badges)
	}
}

func TestPendingAttachmentBadges_WithAttachments(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path1 := filepath.Join(dir, "spec.md")
	path2 := filepath.Join(dir, "diagram.pdf")
	if err := os.WriteFile(path1, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path2, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}

	_ = m.attachDocumentFromPath(path1)
	_ = m.attachDocumentFromPath(path2)

	badges := m.pendingAttachmentBadges()
	if badges == "" {
		t.Fatal("expected non-empty badges")
	}
	expected := "[📎 spec.md] [📎 diagram.pdf]"
	if badges != expected {
		t.Errorf("badges = %q, want %q", badges, expected)
	}
}

func TestToIPCAttachments_Empty(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	result := m.toIPCAttachments()
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestToIPCAttachments_WithPath(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}

	_ = m.attachDocumentFromPath(path)

	result := m.toIPCAttachments()
	if len(result) != 1 {
		t.Fatalf("expected 1 IPCAttachment, got %d", len(result))
	}
	if result[0].Path != path {
		t.Errorf("Path = %q, want %q", result[0].Path, path)
	}
	if result[0].Name != "doc.pdf" {
		t.Errorf("Name = %q, want %q", result[0].Name, "doc.pdf")
	}
}

func TestToIPCAttachments_Multiple(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path1 := filepath.Join(dir, "doc1.md")
	path2 := filepath.Join(dir, "doc2.pdf")
	if err := os.WriteFile(path1, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path2, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}

	_ = m.attachDocumentFromPath(path1)
	_ = m.attachDocumentFromPath(path2)

	result := m.toIPCAttachments()
	if len(result) != 2 {
		t.Fatalf("expected 2 IPCAttachments, got %d", len(result))
	}
	if result[0].Path != path1 || result[1].Path != path2 {
		t.Errorf("paths don't match expected")
	}
}

func TestLooksLikeFilePath_ValidAbsolute(t *testing.T) {
	if !looksLikeFilePath("/Users/foo/spec.md") {
		t.Error("expected true for valid absolute path")
	}
}

func TestLooksLikeFilePath_WithQuotes(t *testing.T) {
	if !looksLikeFilePath(`"/Users/foo/spec.md"`) {
		t.Error("expected true for quoted absolute path")
	}
}

func TestLooksLikeFilePath_WithQuotesAndSpaces(t *testing.T) {
	// Path with spaces wrapped in quotes.
	if !looksLikeFilePath(`"/Users/foo/My File.md"`) {
		t.Error("expected true for quoted path with spaces")
	}
}

func TestLooksLikeFilePath_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if !looksLikeFilePath("~/Documents/spec.md") {
		t.Error("expected true for tilde path")
	}
	// Verify the path doesn't actually need to exist — it's a syntactic check.
	if !looksLikeFilePath("~/nonexistent/doc.md") {
		t.Error("expected true for tilde path even if file doesn't exist")
	}
	_ = home // used implicitly by looksLikeFilePath
}

func TestLooksLikeFilePath_FileURL(t *testing.T) {
	if !looksLikeFilePath("file:///Users/foo/spec.md") {
		t.Error("expected true for file:// path")
	}
}

func TestLooksLikeFilePath_EscapedSpaces(t *testing.T) {
	if !looksLikeFilePath("/path/to/file\\ with\\ spaces.pdf") {
		t.Error("expected true for path with escaped spaces")
	}
}

func TestLooksLikeFilePath_UpperCaseFileURLQuoted(t *testing.T) {
	if !looksLikeFilePath(`"FILE:///Users/test/SPEC.md"`) {
		t.Error("expected true for quoted uppercase file:// path")
	}
}

func TestLooksLikeFilePath_PlainText(t *testing.T) {
	if looksLikeFilePath("hello world") {
		t.Error("expected false for plain text")
	}
}

func TestLooksLikeFilePath_URL(t *testing.T) {
	if looksLikeFilePath("https://example.com") {
		t.Error("expected false for URL")
	}
}

func TestLooksLikeFilePath_Relative(t *testing.T) {
	if looksLikeFilePath("relative/path/doc.pdf") {
		t.Error("expected false for relative path")
	}
}

func TestLooksLikeFilePath_ImagePath(t *testing.T) {
	if looksLikeFilePath("/Users/foo/photo.png") {
		t.Error("expected false for image path (image flow should handle)")
	}
}

func TestDelegateKeyToTextarea_DocumentPaste_AttachesNotInserts(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(path, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate paste of an absolute document path.
	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune(path),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	// Should have added a pending attachment.
	if len(m2.pendingAttachments) != 1 {
		t.Fatalf("expected 1 pending attachment after paste, got %d", len(m2.pendingAttachments))
	}
	if m2.pendingAttachments[0].name != "spec.md" {
		t.Errorf("attachment name = %q, want %q", m2.pendingAttachments[0].name, "spec.md")
	}

	// The viewport should have a 📎 message, NOT the path in the textarea.
	if len(m2.messages) == 0 {
		t.Fatal("expected at least 1 message (attachment confirmation)")
	}
	lastMsg := m2.messages[len(m2.messages)-1]
	if lastMsg.Sender != "📎" {
		t.Errorf("expected sender '📎', got %q", lastMsg.Sender)
	}
	if !strings.Contains(lastMsg.Text, "spec.md") {
		t.Errorf("expected message to mention spec.md, got: %q", lastMsg.Text)
	}

	// The textarea content should be empty (path was NOT inserted).
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea, got %q", m2.textarea.Value())
	}
}

func TestDelegateKeyToTextarea_DocumentPaste_WithQuotes_AttachesNotInserts(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "My Doc.pdf")
	if err := os.WriteFile(path, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate paste of a quoted path (common from Finder drag-drop on macOS).
	quoted := fmt.Sprintf(`"%s"`, path)
	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune(quoted),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	// Should have attached the document despite quotes.
	if len(m2.pendingAttachments) != 1 {
		t.Fatalf("expected 1 pending attachment after quoted paste, got %d", len(m2.pendingAttachments))
	}
	if m2.pendingAttachments[0].name != "My Doc.pdf" {
		t.Errorf("attachment name = %q, want 'My Doc.pdf'", m2.pendingAttachments[0].name)
	}
	// The textarea must remain empty.
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea, got %q", m2.textarea.Value())
	}
}

func TestDelegateKeyToTextarea_DocumentPaste_Tilde_AttachesNotInserts(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	// Create a real file in the home dir (using temp dir inside home).
	realDir := filepath.Join(home, "Documents")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(realDir, "tilde-test.md")
	if err := os.WriteFile(path, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })

	// Paste a ~/ path.
	tildePath := "~/Documents/tilde-test.md"
	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune(tildePath),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	if len(m2.pendingAttachments) != 1 {
		t.Fatalf("expected 1 pending attachment for tilde paste, got %d", len(m2.pendingAttachments))
	}
	if m2.pendingAttachments[0].name != "tilde-test.md" {
		t.Errorf("attachment name = %q, want 'tilde-test.md'", m2.pendingAttachments[0].name)
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea, got %q", m2.textarea.Value())
	}
}

func TestDelegateKeyToTextarea_DocumentPaste_FileURL_AttachesNotInserts(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "from-web.pdf")
	if err := os.WriteFile(path, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}

	// Paste a file:// URL.
	fileURL := "file://" + path
	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune(fileURL),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	if len(m2.pendingAttachments) != 1 {
		t.Fatalf("expected 1 pending attachment for file:// paste, got %d", len(m2.pendingAttachments))
	}
	if m2.pendingAttachments[0].name != "from-web.pdf" {
		t.Errorf("attachment name = %q, want 'from-web.pdf'", m2.pendingAttachments[0].name)
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea, got %q", m2.textarea.Value())
	}
}

func TestDelegateKeyToTextarea_InvalidDocumentPath_NotExists_ShowsError(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	// Paste a path that doesn't exist.
	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune("/nonexistent/doc.md"),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	// Should show error message.
	if len(m2.messages) == 0 {
		t.Fatal("expected error message for nonexistent path")
	}
	lastMsg := m2.messages[len(m2.messages)-1]
	if lastMsg.Sender != "⚠️" {
		t.Errorf("expected sender '⚠️', got %q", lastMsg.Sender)
	}
	if !strings.Contains(lastMsg.Text, "not found") && !strings.Contains(lastMsg.Text, "Not found") {
		t.Errorf("expected 'not found' in error, got: %q", lastMsg.Text)
	}
	// Should NOT be in textarea.
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea for invalid path paste, got %q", m2.textarea.Value())
	}
}

func TestDelegateKeyToTextarea_InvalidDocumentPath_Symlink_ShowsError(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	if err := os.WriteFile(target, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune(link),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	if len(m2.messages) == 0 {
		t.Fatal("expected error message for symlink")
	}
	lastMsg := m2.messages[len(m2.messages)-1]
	if lastMsg.Sender != "⚠️" {
		t.Errorf("expected sender '⚠️', got %q", lastMsg.Sender)
	}
	if !strings.Contains(lastMsg.Text, "Symlink") {
		t.Errorf("expected 'Symlink' in error, got: %q", lastMsg.Text)
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea for symlink paste, got %q", m2.textarea.Value())
	}
}

func TestDelegateKeyToTextarea_InvalidDocumentPath_Directory_ShowsError(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	subdir := filepath.Join(dir, "mydir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune(subdir),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	if len(m2.messages) == 0 {
		t.Fatal("expected error message for directory")
	}
	lastMsg := m2.messages[len(m2.messages)-1]
	if lastMsg.Sender != "⚠️" {
		t.Errorf("expected sender '⚠️', got %q", lastMsg.Sender)
	}
	if !strings.Contains(lastMsg.Text, "Not a regular file") {
		t.Errorf("expected 'Not a regular file' in error, got: %q", lastMsg.Text)
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea for directory paste, got %q", m2.textarea.Value())
	}
}

func TestDelegateKeyToTextarea_NormalText_InsertsInTextarea(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	// Paste plain text (not a path).
	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune("hello world"),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	// Should be inserted into the textarea, not attached.
	if m2.textarea.Value() != "hello world" {
		t.Errorf("expected 'hello world' in textarea, got %q", m2.textarea.Value())
	}
	if len(m2.pendingAttachments) != 0 {
		t.Errorf("expected 0 pending attachments for plain text, got %d", len(m2.pendingAttachments))
	}
}

func TestDelegateKeyToTextarea_TextMentioningPath_InsertsInTextarea(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	// Paste a sentence that mentions a path (not a pure path paste).
	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune("veja /etc/passwd para referência"),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	// Should be inserted into the textarea (it has spaces, not a deliberate path).
	if m2.textarea.Value() != "veja /etc/passwd para referência" {
		t.Errorf("expected full sentence in textarea, got %q", m2.textarea.Value())
	}
	if len(m2.pendingAttachments) != 0 {
		t.Errorf("expected 0 pending attachments, got %d", len(m2.pendingAttachments))
	}
}

func TestDelegateKeyToTextarea_ImagePaste_StillHandledByImageFlow(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune(path),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	// Image should be attached (existing image flow), not inserted as text.
	if len(m2.pendingImages) != 1 {
		t.Fatalf("expected 1 pending image after paste, got %d", len(m2.pendingImages))
	}
	if m2.pendingImages[0].name != "photo.png" {
		t.Errorf("image name = %q, want 'photo.png'", m2.pendingImages[0].name)
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea for image paste, got %q", m2.textarea.Value())
	}
}

func TestDelegateKeyToTextarea_DocumentPaste_WithQuotedEscapedSpaces_Attaches(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	// Create a file within a directory with spaces.
	subdir := filepath.Join(dir, "My Folder")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(subdir, "My Doc.pdf")
	if err := os.WriteFile(path, testDocContent(), 0o644); err != nil {
		t.Fatal(err)
	}

	// The original text has escaped spaces (as macOS Finder would present it).
	escapedPath := strings.ReplaceAll(path, " ", "\\ ")
	msg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune(escapedPath),
		Paste: true,
	})
	result, _ := m.delegateKeyToTextarea(msg)
	m2 := result.(Model)

	// Should attach (not insert into textarea).
	if len(m2.pendingAttachments) != 1 {
		t.Fatalf("expected 1 pending attachment for escaped-space path, got %d", len(m2.pendingAttachments))
	}
	if m2.pendingAttachments[0].name != "My Doc.pdf" {
		t.Errorf("attachment name = %q, want 'My Doc.pdf'", m2.pendingAttachments[0].name)
	}
	if m2.textarea.Value() != "" {
		t.Errorf("expected empty textarea, got %q", m2.textarea.Value())
	}
}


