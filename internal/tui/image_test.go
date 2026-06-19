package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// testPNG returns a minimal valid PNG file with the first 8-byte signature.
// Size is kept small for fast tests while being detectable by content sniffing.
func testPNG() []byte {
	return []byte{
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
}

// testJPEG returns a minimal valid JPEG file (SOI + short APP0/EOI).
func testJPEG() []byte {
	return []byte{
		0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46,
		0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01,
		0x00, 0x01, 0x00, 0x00, 0xff, 0xd9,
	}
}

func TestAttachImageFromPath_ValidPNG(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
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
	if m.pendingImages[0].isTemp {
		t.Error("expected isTemp=false for user-supplied path")
	}
}

func TestAttachImageFromPath_ValidJPEG(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(path, testJPEG(), 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachImageFromPath(path)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if len(m.pendingImages) != 1 {
		t.Fatalf("expected 1 pending image, got %d", len(m.pendingImages))
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
	// Create a file larger than 10MB with valid PNG prefix.
	data := make([]byte, 11*1024*1024)
	copy(data, testPNG())
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

func TestAttachImageFromPath_SymlinkRejected(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	target := filepath.Join(dir, "real.png")
	if err := os.WriteFile(target, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.png")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachImageFromPath(link)
	if errMsg == "" {
		t.Fatal("expected error for symlink")
	}
	if len(m.pendingImages) != 0 {
		t.Errorf("expected 0 pending images, got %d", len(m.pendingImages))
	}
}

func TestAttachImageFromPath_FakePNGContentRejected(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "secret.png")
	// Write a text file with a .png extension.
	if err := os.WriteFile(path, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachImageFromPath(path)
	if errMsg == "" {
		t.Fatal("expected error for non-image content with .png extension")
	}
	if len(m.pendingImages) != 0 {
		t.Errorf("expected 0 pending images, got %d", len(m.pendingImages))
	}
}

func TestAttachImageFromPath_UppercaseExtension(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.PNG")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachImageFromPath(path)
	if errMsg != "" {
		t.Fatalf("unexpected error for uppercase extension: %s", errMsg)
	}
	if len(m.pendingImages) != 1 {
		t.Fatalf("expected 1 pending image, got %d", len(m.pendingImages))
	}
}

func TestNormalizeImagePath_Quoted(t *testing.T) {
	got := normalizeImagePath(`"/path/to/img.png"`)
	if got != "/path/to/img.png" {
		t.Errorf("got %q, want %q", got, "/path/to/img.png")
	}
}

func TestNormalizeImagePath_FileURL(t *testing.T) {
	got := normalizeImagePath("file:///path/to/img.png")
	if got != "/path/to/img.png" {
		t.Errorf("got %q, want %q", got, "/path/to/img.png")
	}
}

func TestNormalizeImagePath_EscapedSpaces(t *testing.T) {
	got := normalizeImagePath("/path/to/image\\ with\\ spaces.png")
	if got != "/path/to/image with spaces.png" {
		t.Errorf("got %q, want %q", got, "/path/to/image with spaces.png")
	}
}

func TestNormalizeImagePath_UpperCaseFileURLQuoted(t *testing.T) {
	got := normalizeImagePath(`"FILE:///Users/test/PHOTO.PNG"`)
	if got != "/Users/test/PHOTO.PNG" {
		t.Errorf("got %q, want %q", got, "/Users/test/PHOTO.PNG")
	}
}

func TestAttachImagePathsFromText_EscapedPathWithSpaces(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	dir := filepath.Join(t.TempDir(), "GravaçãoTela")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Captura de Tela 2026-06-17 às 19.04.36.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	escaped := filepath.Join(dir, "Captura\\ de\\ Tela\\ 2026-06-17\\ às\\ 19.04.36.png")

	text, count, errMsg := m.attachImagePathsFromText("descreva essa imagem " + escaped)

	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if text != "descreva essa imagem" {
		t.Errorf("text = %q, want %q", text, "descreva essa imagem")
	}
	if len(m.pendingImages) != 1 {
		t.Fatalf("expected 1 pending image, got %d", len(m.pendingImages))
	}
	if m.pendingImages[0].path != path {
		t.Errorf("path = %q, want %q", m.pendingImages[0].path, path)
	}
}

func TestAttachImagePathsFromText_UnescapedPathWithSpaces(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	dir := filepath.Join(t.TempDir(), "GravaçãoTela")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Captura de Tela 2026-06-17 às 19.04.36.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	text, count, errMsg := m.attachImagePathsFromText("descreva essa imagem " + path)

	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if text != "descreva essa imagem" {
		t.Errorf("text = %q, want %q", text, "descreva essa imagem")
	}
	if len(m.pendingImages) != 1 || m.pendingImages[0].path != path {
		t.Fatalf("pendingImages = %+v, want path %q", m.pendingImages, path)
	}
}

func TestAttachImagePathsFromText_QuotedPath(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	dir := t.TempDir()
	path := filepath.Join(dir, "screen shot.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	text, count, errMsg := m.attachImagePathsFromText("analise \"" + path + "\" agora")

	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if text != "analise agora" {
		t.Errorf("text = %q, want %q", text, "analise agora")
	}
}

func TestAttachImagePathsFromText_IgnoresHTTPURL(t *testing.T) {
	m := NewModel("/tmp/test.sock")
	input := "descreva https://example.com/image.png"

	text, count, errMsg := m.attachImagePathsFromText(input)

	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if text != input {
		t.Errorf("text = %q, want %q", text, input)
	}
	if len(m.pendingImages) != 0 {
		t.Fatalf("expected no pending images, got %d", len(m.pendingImages))
	}
}

func TestClearPendingImages(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
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
	if err := os.WriteFile(path1, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path2, testJPEG(), 0o644); err != nil {
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
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
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

func TestAttachTempImage_CleanedOnValidationFail(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	// Create a temp file that fails validation (not an image).
	path := filepath.Join(dir, "notimg.png")
	if err := os.WriteFile(path, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachTempImage(path)
	if errMsg == "" {
		t.Fatal("expected error for non-image temp file")
	}
	if len(m.pendingImages) != 0 {
		t.Errorf("expected 0 pending images after validation failure, got %d", len(m.pendingImages))
	}
	// The temp file should have been removed by attachTempImage.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected temp file to be removed on validation failure, stat: %v", err)
	}
}

func TestAttachTempImage_MarkedAsTemp(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachTempImage(path)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if len(m.pendingImages) != 1 {
		t.Fatalf("expected 1 pending image, got %d", len(m.pendingImages))
	}
	if !m.pendingImages[0].isTemp {
		t.Error("expected isTemp=true for clipboard temp image")
	}
}

func TestCleanupTempImages_OnlyRemovesTempFiles(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()

	// User-supplied image (not temp).
	userPath := filepath.Join(dir, "user.png")
	if err := os.WriteFile(userPath, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	// Clipboard temp image.
	tempPath := filepath.Join(dir, "clip.png")
	if err := os.WriteFile(tempPath, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	_ = m.attachImageFromPath(userPath)
	errMsg := m.attachTempImage(tempPath)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}

	if len(m.pendingImages) != 2 {
		t.Fatalf("expected 2 pending images, got %d", len(m.pendingImages))
	}

	m.clearPendingImages()

	// User file should still exist.
	if _, err := os.Stat(userPath); os.IsNotExist(err) {
		t.Error("expected user-supplied file to survive clearPendingImages")
	}
	// Temp file should be deleted.
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("expected temp file to be removed by clearPendingImages")
	}
}

func TestIsImagePath_UppercaseExtension(t *testing.T) {
	if !isImagePath("photo.PNG") {
		t.Error("expected isImagePath to accept .PNG")
	}
	if !isImagePath("photo.JPG") {
		t.Error("expected isImagePath to accept .JPG")
	}
}

func TestIsImagePath_QuotedPath(t *testing.T) {
	if !isImagePath(`"/path/to/img.png"`) {
		t.Error("expected isImagePath to accept quoted path")
	}
}

func TestIsImagePath_FileURL(t *testing.T) {
	if !isImagePath("file:///path/to/img.png") {
		t.Error("expected isImagePath to accept file:// URL")
	}
}

func TestIsImagePath_EscapedSpaces(t *testing.T) {
	if !isImagePath("/path/to/image\\ with\\ spaces.png") {
		t.Error("expected isImagePath to accept escaped-space path")
	}
}

func TestIsImagePath_NonImageText(t *testing.T) {
	if isImagePath("hello world") {
		t.Error("expected isImagePath to reject non-image text")
	}
	if isImagePath("/path/to/doc.txt") {
		t.Error("expected isImagePath to reject .txt paths")
	}
}

func TestStartsWithSyntacticImagePath_ExistingPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	input := path + " descreva"
	if !startsWithSyntacticImagePath(input) {
		t.Errorf("startsWithSyntacticImagePath(%q) = false, want true for existing path", input)
	}
}

func TestStartsWithSyntacticImagePath_MissingPath(t *testing.T) {
	// A nonexistent file with an image-looking path should still be
	// detected syntactically so the error is surfaced via image handling.
	input := "/tmp/missing.png descreva"
	if !startsWithSyntacticImagePath(input) {
		t.Errorf("startsWithSyntacticImagePath(%q) = false, want true for missing path", input)
	}
}

func TestStartsWithSyntacticImagePath_SlashCommand(t *testing.T) {
	// Ordinary slash commands must NOT be detected as image paths.
	if startsWithSyntacticImagePath("/help") {
		t.Error("startsWithSyntacticImagePath(\"/help\") = true, want false")
	}
	if startsWithSyntacticImagePath("/img") {
		t.Error("startsWithSyntacticImagePath(\"/img\") = true, want false")
	}
	if startsWithSyntacticImagePath("/model gpt-4") {
		t.Error("startsWithSyntacticImagePath(\"/model gpt-4\") = true, want false")
	}
}

func TestStartsWithSyntacticImagePath_PlainText(t *testing.T) {
	if startsWithSyntacticImagePath("hello world") {
		t.Error("startsWithSyntacticImagePath(\"hello world\") = true, want false")
	}
}

func TestStartsWithSyntacticImagePath_QuotedPathWithSpaces(t *testing.T) {
	// A quoted path at position 0 with spaces must still be detected,
	// since quotes define an unambiguous path boundary.
	input := `"/dir with spaces/photo.png" descreva`
	if !startsWithSyntacticImagePath(input) {
		t.Errorf("startsWithSyntacticImagePath(%q) = false, want true for quoted path", input)
	}

	// Also test single-quoted path.
	input = `'/dir with spaces/photo.png' descreva`
	if !startsWithSyntacticImagePath(input) {
		t.Errorf("startsWithSyntacticImagePath(%q) = false, want true for single-quoted path", input)
	}
}

func TestStartsWithSyntacticImagePath_EscapedPathWithSpaces(t *testing.T) {
	// A backslash-escaped path at position 0 must be detected, since
	// \ sequences mark the spaces as intentional path content.
	input := `/dir\ with\ spaces/photo.png descreva`
	if !startsWithSyntacticImagePath(input) {
		t.Errorf("startsWithSyntacticImagePath(%q) = false, want true for escaped path", input)
	}
}

func TestStartsWithSyntacticImagePath_UnescapedPathWithSpaces(t *testing.T) {
	// An unescaped, unquoted path with spaces at position 0 is NOT detected
	// as a syntactic image path. This is a known limitation: the parser
	// cannot distinguish "command + argument" (e.g., "/status /path/file.png")
	// from an unescaped path with spaces in directory names. Users should
	// quote or escape such paths, or use /img for reliable attachment.
	input := `/dir with spaces/photo.png desc`
	if startsWithSyntacticImagePath(input) {
		t.Errorf("startsWithSyntacticImagePath(%q) = true, want false (unsupported case)", input)
	}
}

func TestStartsWithSyntacticImagePath_CommandWithPathArgument(t *testing.T) {
	// /status followed by a path argument must not be detected as image path.
	if startsWithSyntacticImagePath("/status /tmp/photo.png") {
		t.Error("startsWithSyntacticImagePath(\"/status /tmp/photo.png\") = true, want false")
	}
	if startsWithSyntacticImagePath("/cwd /tmp/photo.png") {
		t.Error("startsWithSyntacticImagePath(\"/cwd /tmp/photo.png\") = true, want false")
	}
}

func TestToIPCImages_MediaTypeFromContent(t *testing.T) {
	// Test that toIPCImages uses content-detected MIME, not extension.
	// A PNG file with a .jpg extension should get "image/png" MediaType.
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg") // .jpg extension but PNG content
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachImageFromPath(path)
	if errMsg != "" {
		t.Fatalf("attachImageFromPath error: %s", errMsg)
	}

	result := m.toIPCImages()
	if len(result) != 1 {
		t.Fatalf("expected 1 IPCImage, got %d", len(result))
	}
	// Must be "image/png" (from content), not "image/jpeg" (from extension).
	if result[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want %q (detected from content)", result[0].MediaType, "image/png")
	}
}

func TestToIPCImages_MediaTypeFromContent_NoExtension(t *testing.T) {
	// Bare filename without extension — MIMEFromPath would return
	// an empty or wrong type, but content detection should work.
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "photo") // no extension
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachImageFromPath(path)
	if errMsg != "" {
		t.Fatalf("attachImageFromPath error: %s", errMsg)
	}

	result := m.toIPCImages()
	if len(result) != 1 {
		t.Fatalf("expected 1 IPCImage, got %d", len(result))
	}
	if result[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want %q (detected from content)", result[0].MediaType, "image/png")
	}
}

func TestToIPCImages_MediaTypeFromContent_JPEGWithPNGExtension(t *testing.T) {
	m := NewModel("/tmp/test.sock")

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png") // .png extension but JPEG content
	if err := os.WriteFile(path, testJPEG(), 0o644); err != nil {
		t.Fatal(err)
	}

	errMsg := m.attachImageFromPath(path)
	if errMsg != "" {
		t.Fatalf("attachImageFromPath error: %s", errMsg)
	}

	result := m.toIPCImages()
	if len(result) != 1 {
		t.Fatalf("expected 1 IPCImage, got %d", len(result))
	}
	if result[0].MediaType != "image/jpeg" {
		t.Errorf("MediaType = %q, want %q (detected from content)", result[0].MediaType, "image/jpeg")
	}
}
