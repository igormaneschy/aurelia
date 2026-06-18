package images

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSupportedMIMEType(t *testing.T) {
	tests := []struct {
		mime string
		want bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/gif", true},
		{"image/webp", true},
		{"IMAGE/PNG", true},
		{" image/jpeg ", true},
		{"image/bmp", false},
		{"image/svg+xml", false},
		{"text/plain", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := SupportedMIMEType(tt.mime); got != tt.want {
			t.Errorf("SupportedMIMEType(%q) = %v, want %v", tt.mime, got, tt.want)
		}
	}
}

func TestMIMEFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"photo.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"anim.gif", "image/gif"},
		{"pic.webp", "image/webp"},
		{"doc.txt", "text/plain; charset=utf-8"},
		{"unknown.xyz", "chemical/x-xyz"}, // system-registered MIME
	}
	for _, tt := range tests {
		if got := MIMEFromPath(tt.path); got != tt.want {
			t.Errorf("MIMEFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestIsSupportedImageFile(t *testing.T) {
	tests := []struct {
		filename string
		mime     string
		want     bool
	}{
		{"photo.png", "image/png", true},
		{"photo.png", "", true},          // MIME inferred from extension
		{"photo", "image/jpeg", true},    // MIME explicit
		{"doc.txt", "text/plain", false}, // unsupported
		{"photo.bmp", "image/bmp", false},
	}
	for _, tt := range tests {
		if got := IsSupportedImageFile(tt.filename, tt.mime); got != tt.want {
			t.Errorf("IsSupportedImageFile(%q, %q) = %v, want %v", tt.filename, tt.mime, got, tt.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{10485760, "10.0 MB"},
	}
	for _, tt := range tests {
		if got := HumanBytes(tt.n); got != tt.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestEncode_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	// Minimal valid PNG: 1x1 pixel, transparent.
	data := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, // IDAT chunk
		0x54, 0x78, 0x9c, 0x62, 0x00, 0x00, 0x00, 0x02,
		0x00, 0x01, 0xe5, 0x27, 0xde, 0xfc, 0x00, 0x00, // IEND chunk
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42,
		0x60, 0x82,
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	att, err := Encode(path, "image/png", 0)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if att.Path != path {
		t.Errorf("Path = %q, want %q", att.Path, path)
	}
	if att.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want %q", att.MediaType, "image/png")
	}
	if att.Data == "" {
		t.Error("Data should not be empty")
	}
}

func TestEncode_FileNotFound(t *testing.T) {
	_, err := Encode("/nonexistent/image.png", "image/png", 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestEncode_TooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	// Write 1KB file, then set limit to 100 bytes.
	data := make([]byte, 1024)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Encode(path, "image/png", 100)
	if err == nil {
		t.Fatal("expected TooLargeError")
	}
	var tlErr TooLargeError
	if !errors.As(err, &tlErr) {
		t.Fatalf("expected TooLargeError, got %T: %v", err, err)
	}
	if tlErr.Size != 1024 {
		t.Errorf("Size = %d, want 1024", tlErr.Size)
	}
	if tlErr.Limit != 100 {
		t.Errorf("Limit = %d, want 100", tlErr.Limit)
	}
}
