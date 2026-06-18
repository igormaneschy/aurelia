package images

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testPNG returns a minimal valid 1x1 transparent PNG encoded by the stdlib.
func testPNG() []byte {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := png.Encode(&buf, img); err != nil {
		panic("testPNG: " + err.Error())
	}
	return buf.Bytes()
}

// testJPEG returns a minimal valid JPEG file encoded by the stdlib.
func testJPEG() []byte {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic("testJPEG: " + err.Error())
	}
	return buf.Bytes()
}

// testGIF returns a minimal valid GIF file encoded by the stdlib.
func testGIF() []byte {
	var buf bytes.Buffer
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), []color.Color{color.Black, color.White})
	img.Set(0, 0, color.Black)
	if err := gif.Encode(&buf, img, nil); err != nil {
		panic("testGIF: " + err.Error())
	}
	return buf.Bytes()
}

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

func TestDetectMIMEFromFile_PNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	mime, err := DetectMIMEFromFile(path)
	if err != nil {
		t.Fatalf("DetectMIMEFromFile error: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("got %q, want %q", mime, "image/png")
	}
}

func TestDetectMIMEFromFile_JPEG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.jpg")
	if err := os.WriteFile(path, testJPEG(), 0o644); err != nil {
		t.Fatal(err)
	}
	mime, err := DetectMIMEFromFile(path)
	if err != nil {
		t.Fatalf("DetectMIMEFromFile error: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("got %q, want %q", mime, "image/jpeg")
	}
}

func TestDetectMIMEFromFile_GIF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.gif")
	if err := os.WriteFile(path, testGIF(), 0o644); err != nil {
		t.Fatal(err)
	}
	mime, err := DetectMIMEFromFile(path)
	if err != nil {
		t.Fatalf("DetectMIMEFromFile error: %v", err)
	}
	if mime != "image/gif" {
		t.Errorf("got %q, want %q", mime, "image/gif")
	}
}

func TestDetectMIMEFromFile_TextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := DetectMIMEFromFile(path)
	if err == nil {
		t.Fatal("expected error for text file")
	}
}

func TestValidateImagePath_ValidPNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	mime, err := ValidateImagePath(path, 0)
	if err != nil {
		t.Fatalf("ValidateImagePath error: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("ValidateImagePath mime = %q, want %q", mime, "image/png")
	}
}

func TestValidateImagePath_SymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.png")
	if err := os.WriteFile(target, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.png")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := ValidateImagePath(link, 0)
	if err == nil {
		t.Fatal("expected error for symlink")
	}
	if !errors.Is(err, ErrSymlinkRejected) {
		t.Fatalf("expected ErrSymlinkRejected, got %v", err)
	}
}

func TestValidateImagePath_OversizedRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	data := make([]byte, 1024)
	copy(data, testPNG())
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ValidateImagePath(path, 500)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	var tlErr TooLargeError
	if !errors.As(err, &tlErr) {
		t.Fatalf("expected TooLargeError, got %T: %v", err, err)
	}
}

func TestValidateImagePath_NonImageContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	if err := os.WriteFile(path, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ValidateImagePath(path, 0)
	if err == nil {
		t.Fatal("expected error for non-image content")
	}
}

func TestValidateImagePath_FileNotFound(t *testing.T) {
	_, err := ValidateImagePath("/nonexistent/img.png", 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestSanitizedError_TooLarge(t *testing.T) {
	err := TooLargeError{Path: "/Users/secret/passwords.png", Size: 20_000_000, Limit: 10_000_000}
	want := "Image too large (19.1 MB). Limit is 9.5 MB."
	if got := SanitizedError(err); got != want {
		t.Errorf("SanitizedError = %q, want %q", got, want)
	}
}

func TestSanitizedError_AccessPrefixStripped(t *testing.T) {
	err := fmt.Errorf("access somefile.png: file does not exist")
	sanitized := SanitizedError(err)
	if strings.Contains(sanitized, "access ") {
		t.Errorf("SanitizedError should strip 'access filename:' prefix, got %q", sanitized)
	}
	if !strings.Contains(sanitized, "file does not exist") {
		t.Errorf("SanitizedError should preserve the underlying message, got %q", sanitized)
	}
}

func TestSanitizedError_Nil(t *testing.T) {
	if got := SanitizedError(nil); got != "" {
		t.Errorf("expected empty string for nil, got %q", got)
	}
}

func TestEncode_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
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
	copy(data, testPNG())
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

// testWEBP returns a minimal valid WEBP (lossy) file.
// RIFF header + VP8 chunk with minimal valid keyframe data.
func testWEBP() []byte {
	// Minimal WEBP lossy: RIFF(12) + VP8 chunk header(8) + minimal VP8 keyframe.
	// VP8 keyframe: 0x9d 0x01 0x2a + frame header + 1x1 block data.
	vp8Data := []byte{
		0x9d, 0x01, 0x2a, // VC start code (0x9d012a)
		0x00, 0x00, 0x00, 0x00, // temp dummy
	}
	chunkSize := len(vp8Data)
	riffSize := 4 + 4 + 8 + chunkSize // "WEBP"(4) + chunk_hdr(8) + data
	header := make([]byte, 0, 20+chunkSize)
	header = append(header, 0x52, 0x49, 0x46, 0x46) // RIFF
	sz := make([]byte, 4)
	binary.LittleEndian.PutUint32(sz, uint32(riffSize))
	header = append(header, sz...)
	header = append(header, 0x57, 0x45, 0x42, 0x50) // WEBP
	header = append(header, 0x56, 0x50, 0x38, 0x20) // VP8 (note: trailing space)
	chunkSz := make([]byte, 4)
	binary.LittleEndian.PutUint32(chunkSz, uint32(chunkSize))
	header = append(header, chunkSz...)
	header = append(header, vp8Data...)
	return header
}

// testInvalidWEBP returns data that starts with RIFF+WEBP header but has
// an invalid chunk type (not VP8/VP8L/VP8X).
func testInvalidWEBP() []byte {
	data := make([]byte, 20)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 12)
	copy(data[8:12], "WEBP")
	copy(data[12:16], "XXXX") // Invalid chunk type
	binary.LittleEndian.PutUint32(data[16:20], 0)
	return data
}

// testMagicPrefixJunk returns data starting with PNG magic bytes but
// containing garbage for the rest of the file.
func testMagicPrefixJunk(magic []byte, size int) []byte {
	data := make([]byte, size)
	copy(data, magic)
	// Fill rest with non-image data.
	for i := len(magic); i < size; i++ {
		data[i] = byte(i % 256)
	}
	return data
}

func TestDetectMIMEFromFile_WEBP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.webp")
	if err := os.WriteFile(path, testWEBP(), 0o644); err != nil {
		t.Fatal(err)
	}
	mime, err := DetectMIMEFromFile(path)
	if err != nil {
		t.Fatalf("DetectMIMEFromFile error: %v", err)
	}
	if mime != "image/webp" {
		t.Errorf("got %q, want %q", mime, "image/webp")
	}
}

func TestValidateImageContent_ValidWEBP(t *testing.T) {
	data := testWEBP()
	mime, err := validateImageContent(data)
	if err != nil {
		t.Fatalf("validateImageContent(webp) error: %v", err)
	}
	if mime != "image/webp" {
		t.Errorf("got %q, want %q", mime, "image/webp")
	}
}

func TestValidateImageContent_InvalidWEBP(t *testing.T) {
	_, err := validateImageContent(testInvalidWEBP())
	if err == nil {
		t.Fatal("expected error for invalid WEBP chunk type")
	}
}

func TestValidateImageContent_ValidPNG(t *testing.T) {
	mime, err := validateImageContent(testPNG())
	if err != nil {
		t.Fatalf("validateImageContent(png) error: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("got %q, want %q", mime, "image/png")
	}
}

func TestValidateImageContent_ValidJPEG(t *testing.T) {
	mime, err := validateImageContent(testJPEG())
	if err != nil {
		t.Fatalf("validateImageContent(jpeg) error: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("got %q, want %q", mime, "image/jpeg")
	}
}

func TestValidateImageContent_ValidGIF(t *testing.T) {
	mime, err := validateImageContent(testGIF())
	if err != nil {
		t.Fatalf("validateImageContent(gif) error: %v", err)
	}
	if mime != "image/gif" {
		t.Errorf("got %q, want %q", mime, "image/gif")
	}
}

// Magic-prefix junk rejection tests.

func TestValidateImageContent_PNGMagicJunkRejected(t *testing.T) {
	// PNG magic prefix but non-PNG body.
	data := testMagicPrefixJunk([]byte{0x89, 0x50, 0x4e, 0x47}, 100)
	_, err := validateImageContent(data)
	if err == nil {
		t.Fatal("expected error for PNG magic junk")
	}
}

func TestValidateImageContent_JPEGMagicJunkRejected(t *testing.T) {
	// JPEG magic prefix but garbage body.
	data := testMagicPrefixJunk([]byte{0xff, 0xd8}, 100)
	_, err := validateImageContent(data)
	if err == nil {
		t.Fatal("expected error for JPEG magic junk")
	}
}

func TestValidateImageContent_GIFMagicJunkRejected(t *testing.T) {
	// GIF magic prefix but garbage body.
	data := testMagicPrefixJunk([]byte{0x47, 0x49, 0x46}, 100)
	_, err := validateImageContent(data)
	if err == nil {
		t.Fatal("expected error for GIF magic junk")
	}
}

func TestReadImageSafely_ValidPNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	data, mime, err := ReadImageSafely(path, 0)
	if err != nil {
		t.Fatalf("ReadImageSafely error: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("got mime %q, want %q", mime, "image/png")
	}
	if len(data) == 0 {
		t.Error("expected non-empty data")
	}
}

func TestReadImageSafely_SymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.png")
	if err := os.WriteFile(target, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.png")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, _, err := ReadImageSafely(link, 0)
	if err == nil {
		t.Fatal("expected error for symlink")
	}
	if !errors.Is(err, ErrSymlinkRejected) {
		t.Fatalf("expected ErrSymlinkRejected, got %v", err)
	}
}

func TestReadImageSafely_OversizedRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	data := make([]byte, 200)
	copy(data, testPNG())
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ReadImageSafely(path, 100)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	var tlErr TooLargeError
	if !errors.As(err, &tlErr) {
		t.Fatalf("expected TooLargeError, got %T: %v", err, err)
	}
}

func TestReadImageSafely_NonImageContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	if err := os.WriteFile(path, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ReadImageSafely(path, 0)
	if err == nil {
		t.Fatal("expected error for non-image content")
	}
}

func TestReadImageSafely_FileNotFound(t *testing.T) {
	_, _, err := ReadImageSafely("/nonexistent/img.png", 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// containsPath is a helper that checks if s contains the given path.
func containsPath(s, path string) bool {
	return len(s) > 0 && len(path) > 0 && strings.Contains(s, path)
}
