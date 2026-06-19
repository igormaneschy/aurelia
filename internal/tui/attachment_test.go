package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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


