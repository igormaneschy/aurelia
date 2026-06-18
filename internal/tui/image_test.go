package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAttachImageFromPath_ValidPNG(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	data := []byte{0x89, 0x50, 0x4e, 0x47} // PNG header
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachImageFromPath(path)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if len(m.pendingImages) != 1 {
		t.Fatalf("expected 1 pending image, got %d", len(m.pendingImages))
	}
	if m.pendingImages[0].name != "test.png" {
		t.Errorf("name = %q, want %q", m.pendingImages[0].name, "test.png")
	}
}

func TestAttachImageFromPath_EmptyPath(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	errMsg := m.attachImageFromPath("")
	if errMsg != "Usage: /img <path-to-image>" {
		t.Errorf("unexpected error: %s", errMsg)
	}
	if len(m.pendingImages) != 0 {
		t.Errorf("expected 0 pending images, got %d", len(m.pendingImages))
	}
}

func TestAttachImageFromPath_NotFound(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	errMsg := m.attachImageFromPath("/nonexistent/image.png")
	if errMsg == "" {
		t.Fatal("expected error for nonexistent file")
	}
	if len(m.pendingImages) != 0 {
		t.Errorf("expected 0 pending images, got %d", len(m.pendingImages))
	}
}

func TestAttachImageFromPath_UnsupportedType(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachImageFromPath(path)
	if errMsg == "" {
		t.Fatal("expected error for unsupported type")
	}
	if len(m.pendingImages) != 0 {
		t.Errorf("expected 0 pending images, got %d", len(m.pendingImages))
	}
}

func TestAttachImageFromPath_TooLarge(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	// Create a file larger than 10MB.
	data := make([]byte, 11*1024*1024)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachImageFromPath(path)
	if errMsg == "" {
		t.Fatal("expected error for too large image")
	}
	if len(m.pendingImages) != 0 {
		t.Errorf("expected 0 pending images, got %d", len(m.pendingImages))
	}
}

func TestClearPendingImages(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	data := []byte{0x89, 0x50, 0x4e, 0x47}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_ = m.attachImageFromPath(path)
	if len(m.pendingImages) != 1 {
		t.Fatalf("expected 1 pending image, got %d", len(m.pendingImages))
	}

	m.clearPendingImages()
	if len(m.pendingImages) != 0 {
		t.Errorf("expected 0 pending images after clear, got %d", len(m.pendingImages))
	}
}

func TestPendingImageBadges_Empty(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	badges := m.pendingImageBadges()
	if badges != "" {
		t.Errorf("expected empty badges, got %q", badges)
	}
}

func TestPendingImageBadges_WithImages(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path1 := filepath.Join(dir, "image1.png")
	path2 := filepath.Join(dir, "image2.jpg")
	data := []byte{0x89, 0x50, 0x4e, 0x47}
	if err := os.WriteFile(path1, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path2, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_ = m.attachImageFromPath(path1)
	_ = m.attachImageFromPath(path2)

	badges := m.pendingImageBadges()
	if badges == "" {
		t.Fatal("expected non-empty badges")
	}
	if badges != "📎 image1.png, image2.jpg" {
		t.Errorf("badges = %q, want %q", badges, "📎 image1.png, image2.jpg")
	}
}

func TestToIPCImages_Empty(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	result := m.toIPCImages()
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestToIPCImages_WithPath(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	data := []byte{0x89, 0x50, 0x4e, 0x47}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_ = m.attachImageFromPath(path)

	result := m.toIPCImages()
	if len(result) != 1 {
		t.Fatalf("expected 1 IPCImage, got %d", len(result))
	}
	if result[0].Path != path {
		t.Errorf("Path = %q, want %q", result[0].Path, path)
	}
	if result[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want %q", result[0].MediaType, "image/png")
	}
}
