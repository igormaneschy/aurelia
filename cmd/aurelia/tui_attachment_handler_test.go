package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// ── copyAttachmentsToCWD ──────────────────────────────────────────────────

func TestCopyAttachmentsToCWD_Success(t *testing.T) {
	ctx := context.Background()
	cwd := t.TempDir()
	srcDir := t.TempDir()

	// Create source files.
	src1 := filepath.Join(srcDir, "spec.md")
	src2 := filepath.Join(srcDir, "diagram.pdf")
	if err := os.WriteFile(src1, []byte("# Specification"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src2, []byte("PDF content"), 0o644); err != nil {
		t.Fatal(err)
	}

	attachments := []ipc.IPCAttachment{
		{Path: src1, Name: "spec.md"},
		{Path: src2, Name: "diagram.pdf"},
	}

	copied, err := copyAttachmentsToCWD(ctx, cwd, attachments)
	if err != nil {
		t.Fatalf("copyAttachmentsToCWD: %v", err)
	}

	if len(copied) != 2 {
		t.Fatalf("expected 2 copied attachments, got %d", len(copied))
	}

	// Verify uploads/ directory exists.
	uploadsDir := filepath.Join(cwd, "uploads")
	if fi, err := os.Stat(uploadsDir); err != nil {
		t.Fatalf("uploads/ dir stat: %v", err)
	} else if !fi.IsDir() {
		t.Fatal("uploads/ is not a directory")
	}

	// Verify files exist.
	for _, c := range copied {
		path := filepath.Join(uploadsDir, c.FinalName)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s: %v", path, err)
		}
		if c.Size == 0 {
			t.Errorf("expected non-zero size for %s", c.FinalName)
		}
	}
}

func TestCopyAttachmentsToCWD_CreatesUploadsDir(t *testing.T) {
	ctx := context.Background()
	cwd := t.TempDir()
	srcDir := t.TempDir()

	src := filepath.Join(srcDir, "doc.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := copyAttachmentsToCWD(ctx, cwd, []ipc.IPCAttachment{
		{Path: src, Name: "doc.txt"},
	})
	if err != nil {
		t.Fatalf("copyAttachmentsToCWD: %v", err)
	}

	uploadsDir := filepath.Join(cwd, "uploads")
	if _, err := os.Stat(uploadsDir); os.IsNotExist(err) {
		t.Fatal("uploads/ directory was not created")
	}
}

func TestCopyAttachmentsToCWD_SymlinkRejected(t *testing.T) {
	ctx := context.Background()
	cwd := t.TempDir()
	srcDir := t.TempDir()

	// Create a regular file then symlink to it.
	target := filepath.Join(srcDir, "real.txt")
	if err := os.WriteFile(target, []byte("real content"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(srcDir, "link.txt")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatal(err)
	}

	_, err := copyAttachmentsToCWD(ctx, cwd, []ipc.IPCAttachment{
		{Path: linkPath, Name: "link.txt"},
	})
	if err == nil {
		t.Fatal("expected error for symlink source, got nil")
	}
	if !strings.Contains(err.Error(), "open source") && !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCopyAttachmentsToCWD_FileSizeLimit(t *testing.T) {
	ctx := context.Background()
	cwd := t.TempDir()
	srcDir := t.TempDir()

	// Create a file that exceeds MaxAttachmentBytes.
	src := filepath.Join(srcDir, "big.bin")
	data := make([]byte, ipc.MaxAttachmentBytes+1)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := copyAttachmentsToCWD(ctx, cwd, []ipc.IPCAttachment{
		{Path: src, Name: "big.bin"},
	})
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds max size") {
		t.Errorf("expected size error, got: %v", err)
	}
}

func TestCopyAttachmentsToCWD_NameConflict(t *testing.T) {
	ctx := context.Background()
	cwd := t.TempDir()
	srcDir := t.TempDir()

	// Create two distinct files with the same name.
	src1 := filepath.Join(srcDir, "doc.md")
	src2 := filepath.Join(srcDir, "other.md")
	if err := os.WriteFile(src1, []byte("version A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src2, []byte("version B"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-create one file in uploads/ to force conflict.
	uploadsDir := filepath.Join(cwd, "uploads")
	if err := os.MkdirAll(uploadsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	preExisting := filepath.Join(uploadsDir, "doc.md")
	if err := os.WriteFile(preExisting, []byte("preexisting"), 0o644); err != nil {
		t.Fatal(err)
	}

	attachments := []ipc.IPCAttachment{
		{Path: src1, Name: "doc.md"},
		{Path: src2, Name: "doc.md"}, // same name — should become doc_1.md
	}

	copied, err := copyAttachmentsToCWD(ctx, cwd, attachments)
	if err != nil {
		t.Fatalf("copyAttachmentsToCWD: %v", err)
	}

	if len(copied) != 2 {
		t.Fatalf("expected 2 copied attachments, got %d", len(copied))
	}

	// First should be doc_1.md (doc.md existed), second should be doc_2.md.
	if copied[0].FinalName != "doc_1.md" {
		t.Errorf("expected first copied file to be doc_1.md, got %s", copied[0].FinalName)
	}
	if copied[1].FinalName != "doc_2.md" {
		t.Errorf("expected second copied file to be doc_2.md, got %s", copied[1].FinalName)
	}
}

func TestCopyAttachmentsToCWD_PathTraversalName(t *testing.T) {
	ctx := context.Background()
	cwd := t.TempDir()
	srcDir := t.TempDir()

	src := filepath.Join(srcDir, "safe.txt")
	if err := os.WriteFile(src, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Attempt to set Name to ".." — must be rejected before any file operation.
	_, err := copyAttachmentsToCWD(ctx, cwd, []ipc.IPCAttachment{
		{Path: src, Name: ".."},
	})
	if err == nil {
		t.Fatal("expected error for path traversal name '..', got nil")
	}
}

// ── copyFileNoFollow ──────────────────────────────────────────────────────

func TestCopyFileNoFollow_SymlinkSourceRejected(t *testing.T) {
	dir := t.TempDir()
	srcDir := t.TempDir()

	target := filepath.Join(srcDir, "real.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(srcDir, "link.txt")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "out.txt")
	_, err := copyFileNoFollow(linkPath, dst, 1024*1024)
	if err == nil {
		t.Fatal("expected error for symlink source")
	}
}

func TestCopyFileNoFollow_DirectorySourceRejected(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.txt")

	// Use the temp dir itself as source — it's a directory.
	_, err := copyFileNoFollow(dir, dst, 1024*1024)
	if err == nil {
		t.Fatal("expected error for directory source")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("expected 'not a regular file' error, got: %v", err)
	}
}

func TestCopyFileNoFollow_FileExceedsMaxBytes(t *testing.T) {
	dir := t.TempDir()
	srcDir := t.TempDir()

	src := filepath.Join(srcDir, "big.bin")
	data := make([]byte, 100)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "out.txt")
	_, err := copyFileNoFollow(src, dst, 50) // max = 50, file = 100
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "exceeds max size") {
		t.Errorf("expected 'exceeds max size' error, got: %v", err)
	}
}

func TestCopyFileNoFollow_DestinationSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	srcDir := t.TempDir()

	src := filepath.Join(srcDir, "real.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink at the destination path.
	dst := filepath.Join(dir, "target.txt")
	if err := os.Symlink(src, dst); err != nil {
		t.Fatal(err)
	}

	// Trying to copy over the symlink should fail with O_EXCL|O_NOFOLLOW.
	_, err := copyFileNoFollow(src, dst, 1024*1024)
	if err == nil {
		t.Fatal("expected error for destination symlink")
	}
}

func TestCopyFileNoFollow_Success(t *testing.T) {
	dir := t.TempDir()
	srcDir := t.TempDir()

	src := filepath.Join(srcDir, "hello.txt")
	content := []byte("Hello, World!")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "hello.txt")
	n, err := copyFileNoFollow(src, dst, 1024*1024)
	if err != nil {
		t.Fatalf("copyFileNoFollow: %v", err)
	}
	if n != int64(len(content)) {
		t.Errorf("expected %d bytes copied, got %d", len(content), n)
	}

	// Verify content.
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("expected content %q, got %q", content, got)
	}
}

func TestCopyFileNoFollow_ExactMaxBytes(t *testing.T) {
	dir := t.TempDir()
	srcDir := t.TempDir()

	src := filepath.Join(srcDir, "exact.bin")
	data := make([]byte, 50)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "exact.bin")
	n, err := copyFileNoFollow(src, dst, 50)
	if err != nil {
		t.Fatalf("copyFileNoFollow: %v", err)
	}
	if n != 50 {
		t.Errorf("expected 50 bytes, got %d", n)
	}
}

func TestCopyFileNoFollow_ExceedsLimitByOne(t *testing.T) {
	dir := t.TempDir()
	srcDir := t.TempDir()

	src := filepath.Join(srcDir, "over.bin")
	// File is 51 bytes, max is 50 → should fail upfront.
	data := make([]byte, 51)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "over.bin")
	_, err := copyFileNoFollow(src, dst, 50)
	if err == nil {
		t.Fatal("expected error for file 1 byte over limit")
	}
	if !strings.Contains(err.Error(), "exceeds max size") {
		t.Errorf("expected 'exceeds max size' error, got: %v", err)
	}

	// Destination should not exist.
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("destination should not exist after failure: %v", err)
	}
}

// ── uniqueUploadPath ──────────────────────────────────────────────────────

func TestUniqueUploadPath_NoConflict(t *testing.T) {
	dir := t.TempDir()

	path, err := uniqueUploadPath(dir, "readme.md")
	if err != nil {
		t.Fatalf("uniqueUploadPath: %v", err)
	}
	expected := filepath.Join(dir, "readme.md")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestUniqueUploadPath_OneConflict(t *testing.T) {
	dir := t.TempDir()

	// Pre-create the file to force conflict.
	pre := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(pre, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := uniqueUploadPath(dir, "doc.md")
	if err != nil {
		t.Fatalf("uniqueUploadPath: %v", err)
	}
	expected := filepath.Join(dir, "doc_1.md")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestUniqueUploadPath_ThreeConflicts(t *testing.T) {
	dir := t.TempDir()

	// Pre-create doc.md, doc_1.md, doc_2.md.
	for _, name := range []string{"doc.md", "doc_1.md", "doc_2.md"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	path, err := uniqueUploadPath(dir, "doc.md")
	if err != nil {
		t.Fatalf("uniqueUploadPath: %v", err)
	}
	expected := filepath.Join(dir, "doc_3.md")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestUniqueUploadPath_ExceedsMaxConflicts(t *testing.T) {
	dir := t.TempDir()

	// Pre-create doc.md and doc_1.md through doc_1000.md.
	baseName := "doc.md"
	pre := filepath.Join(dir, baseName)
	if err := os.WriteFile(pre, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 1000; i++ {
		p := filepath.Join(dir, fmt.Sprintf("doc_%d.md", i))
		if err := os.WriteFile(p, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, err := uniqueUploadPath(dir, "doc.md")
	if err == nil {
		t.Fatal("expected error when all names are exhausted")
	}
	if !strings.Contains(err.Error(), "could not find unique name") {
		t.Errorf("expected 'could not find unique name' error, got: %v", err)
	}
}

func TestUniqueUploadPath_WithExtensionNoConflict(t *testing.T) {
	dir := t.TempDir()

	path, err := uniqueUploadPath(dir, "archive.tar.gz")
	if err != nil {
		t.Fatalf("uniqueUploadPath: %v", err)
	}
	expected := filepath.Join(dir, "archive.tar.gz")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestUniqueUploadPath_WithExtensionConflict(t *testing.T) {
	dir := t.TempDir()

	pre := filepath.Join(dir, "archive.tar.gz")
	if err := os.WriteFile(pre, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := uniqueUploadPath(dir, "archive.tar.gz")
	if err != nil {
		t.Fatalf("uniqueUploadPath: %v", err)
	}
	// Ext is ".gz", stem is "archive.tar" → should produce archive.tar_1.gz
	expected := filepath.Join(dir, "archive.tar_1.gz")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestUniqueUploadPath_NoExtensionNoConflict(t *testing.T) {
	dir := t.TempDir()

	path, err := uniqueUploadPath(dir, "Makefile")
	if err != nil {
		t.Fatalf("uniqueUploadPath: %v", err)
	}
	expected := filepath.Join(dir, "Makefile")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestUniqueUploadPath_NoExtensionConflict(t *testing.T) {
	dir := t.TempDir()

	pre := filepath.Join(dir, "Makefile")
	if err := os.WriteFile(pre, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := uniqueUploadPath(dir, "Makefile")
	if err != nil {
		t.Fatalf("uniqueUploadPath: %v", err)
	}
	expected := filepath.Join(dir, "Makefile_1")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestUniqueUploadPath_PathTraversalRejected(t *testing.T) {
	dir := t.TempDir()

	_, err := uniqueUploadPath(dir, "..")
	if err == nil {
		t.Fatal("expected error for '..'")
	}

	_, err = uniqueUploadPath(dir, ".")
	if err == nil {
		t.Fatal("expected error for '.'")
	}
}

// ── buildAttachmentNote ───────────────────────────────────────────────────

func TestBuildAttachmentNote_Empty(t *testing.T) {
	got := buildAttachmentNote(nil)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}

	got = buildAttachmentNote([]copiedAttachment{})
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestBuildAttachmentNote_Single(t *testing.T) {
	copied := []copiedAttachment{
		{FinalName: "doc.md", Size: 100},
	}
	got := buildAttachmentNote(copied)

	if !strings.Contains(got, "[Attached files copied to ./uploads/]") {
		t.Errorf("expected note header, got: %s", got)
	}
	if !strings.Contains(got, "- doc.md") {
		t.Errorf("expected '- doc.md' in note, got: %s", got)
	}
}

func TestBuildAttachmentNote_Multiple(t *testing.T) {
	copied := []copiedAttachment{
		{FinalName: "spec.md", Size: 100},
		{FinalName: "diagram.pdf", Size: 200},
		{FinalName: "notes.txt", Size: 300},
	}
	got := buildAttachmentNote(copied)

	if !strings.Contains(got, "[Attached files copied to ./uploads/]") {
		t.Errorf("expected note header, got: %s", got)
	}
	if !strings.Contains(got, "- spec.md") {
		t.Errorf("expected '- spec.md', got: %s", got)
	}
	if !strings.Contains(got, "- diagram.pdf") {
		t.Errorf("expected '- diagram.pdf', got: %s", got)
	}
	if !strings.Contains(got, "- notes.txt") {
		t.Errorf("expected '- notes.txt', got: %s", got)
	}
}

func TestBuildAttachmentNote_Format(t *testing.T) {
	copied := []copiedAttachment{
		{FinalName: "readme.md", Size: 42},
	}
	got := buildAttachmentNote(copied)

	// Expected: "\n\n[Attached files copied to ./uploads/]\n- readme.md\n"
	if !strings.HasPrefix(got, "\n\n[Attached files copied to ./uploads/]") {
		t.Errorf("expected note to start with double newline, got: %q", got[:3])
	}
}

// ── Error log basename check (documented via compile guard) ───────────────
// All error messages in this file use filepath.Base for attachment paths.
// This is verified by code review; no automated capture of log output
// is performed.
