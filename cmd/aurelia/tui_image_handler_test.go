package main

import (
	"bytes"
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

	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/pkg/images"
)

// pngMagic is the shortest unambiguous PNG signature prefix (4 bytes).
var pngMagic = []byte{0x89, 0x50, 0x4e, 0x47}

func TestConvertIPCImages_Empty(t *testing.T) {
	result, err := convertIPCImages(nil, 0)
	if err != nil {
		t.Fatalf("convertIPCImages(nil) error = %v", err)
	}
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestConvertIPCImages_FromPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	// Write a 1x1 transparent PNG.
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	images := []ipc.IPCImage{
		{Path: path, MediaType: "image/png"},
	}

	result, err := convertIPCImages(images, 0)
	if err != nil {
		t.Fatalf("convertIPCImages() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(result))
	}
	if result[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want %q", result[0].MediaType, "image/png")
	}
	if result[0].Data == "" {
		t.Error("Data should not be empty")
	}
}

func TestConvertIPCImages_FromData(t *testing.T) {
	ipcImgs := []ipc.IPCImage{
		{Data: "base64data", MediaType: "image/jpeg"},
	}

	result, err := convertIPCImages(ipcImgs, 0)
	if err != nil {
		t.Fatalf("convertIPCImages() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(result))
	}
	if result[0].Data != "base64data" {
		t.Errorf("Data = %q, want %q", result[0].Data, "base64data")
	}
	if result[0].MediaType != "image/jpeg" {
		t.Errorf("MediaType = %q, want %q", result[0].MediaType, "image/jpeg")
	}
}

func TestConvertIPCImages_FileNotFound(t *testing.T) {
	ipcImgs := []ipc.IPCImage{
		{Path: "/nonexistent/image.png", MediaType: "image/png"},
	}

	_, err := convertIPCImages(ipcImgs, 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// TestConvertIPCImages_SymlinkRejected ensures symlinks are rejected before
// read by the daemon-side validation.
func TestConvertIPCImages_SymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.png")
	if err := os.WriteFile(target, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.png")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	ipcImgs := []ipc.IPCImage{
		{Path: link, MediaType: "image/png"},
	}

	_, err := convertIPCImages(ipcImgs, 0)
	if err == nil {
		t.Fatal("expected error for symlink")
	}
}

// TestConvertIPCImages_OversizedRejected ensures oversized path images are
// rejected before reading the full file.
func TestConvertIPCImages_OversizedRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.png")
	// Create a file containing a valid PNG but padded to exceed 10 MB.
	pngData := testPNG()
	data := make([]byte, 11*1024*1024)
	copy(data, pngData)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ipcImgs := []ipc.IPCImage{
		{Path: path, MediaType: "image/png"},
	}

	_, err := convertIPCImages(ipcImgs, 0)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

// TestConvertIPCImages_FakeContentRejected ensures a file with a .png
// extension but non-image content is rejected.
func TestConvertIPCImages_FakeContentRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.png")
	// Write a text/secret file pretending to be an image.
	if err := os.WriteFile(path, []byte("not an image at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	ipcImgs := []ipc.IPCImage{
		{Path: path, MediaType: "image/png"},
	}

	_, err := convertIPCImages(ipcImgs, 0)
	if err == nil {
		t.Fatal("expected error for non-image content")
	}
}

// TestConvertIPCImages_ClientMIMERejectedOverridden tests that even when a
// client sends a mismatched MediaType, the daemon detects the real content.
func TestConvertIPCImages_ClientMIMERejectedOverridden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	// Write a real PNG but claim it's JPEG.
	if err := os.WriteFile(path, testPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	ipcImgs := []ipc.IPCImage{
		{Path: path, MediaType: "image/jpeg"},
	}

	result, err := convertIPCImages(ipcImgs, 0)
	if err != nil {
		t.Fatalf("expected success with real PNG content, got: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(result))
	}
	// MIME should be detected from content, not from the client's claim.
	if result[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want %q (detected from content)", result[0].MediaType, "image/png")
	}
}

// testPNG returns a minimal valid 1x1 transparent PNG encoded by the stdlib.
func testPNG() []byte {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := png.Encode(&buf, img); err != nil {
		panic("testPNG: " + err.Error())
	}
	return buf.Bytes()
}

// testJPEG returns a minimal valid JPEG encoded by the stdlib.
func testJPEG() []byte {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic("testJPEG: " + err.Error())
	}
	return buf.Bytes()
}

// testGIF returns a minimal valid GIF encoded by the stdlib.
func testGIF() []byte {
	var buf bytes.Buffer
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), []color.Color{color.Black, color.White})
	img.Set(0, 0, color.Black)
	if err := gif.Encode(&buf, img, nil); err != nil {
		panic("testGIF: " + err.Error())
	}
	return buf.Bytes()
}

// TestEmptyImport ensures the images package import compiles and is
// accessible from test.
func TestImagePackageAccessible(t *testing.T) {
	if images.DefaultMaxBytes <= 0 {
		t.Error("expected DefaultMaxBytes > 0")
	}
}

// TestConvertIPCImages_AggregateSizeExceeded verifies that 10 near-limit
// images are rejected before full read if their total exceeds
// ipc.MaxTotalImageBytes.
func TestConvertIPCImages_AggregateSizeExceeded(t *testing.T) {
	dir := t.TempDir()

	// Image per-file size: just under 10% of MaxTotalImageBytes each.
	// 11 such files exceed the limit but each individually is under maxBytes.
	perFileSize := ipc.MaxTotalImageBytes / 10 // 1.5 MB per file
	_ = perFileSize

	// Create files that are real images just large enough to push the
	// aggregate past MaxTotalImageBytes. Use minimum valid PNG as base.
	basePNG := testPNG()
	numFiles := 11
	var ipcImgs []ipc.IPCImage

	for i := 0; i < numFiles; i++ {
		filename := filepath.Join(dir, fmt.Sprintf("img%d.png", i))
		// For image files, we need valid PNG content. Since each file
		// must be small (PNG test data is ~68 bytes), 11 such files
		// won't exceed MaxTotalImageBytes. So we write larger valid-looking
		// data: valid PNG header + IHDR + IDAT + IEND with appended padding.
		// The actual decode will work because the decoder stops at IEND.
		// For size testing, we want the pre-read stat to see large sizes.
		// Use a real PNG padded with zeros to the target size.
		fileData := make([]byte, perFileSize)
		copy(fileData, basePNG)
		// Pad with zeros after IEND — the PNG decoder should stop at IEND.
		// But the file's on-disk size is perFileSize which is what stat sees.
		if err := os.WriteFile(filename, fileData, 0o644); err != nil {
			t.Fatal(err)
		}
		ipcImgs = append(ipcImgs, ipc.IPCImage{
			Path:      filename,
			MediaType: "image/png",
		})
	}

	// We need aggregate size to exceed ipc.MaxTotalImageBytes (15 MB).
	// Since each file is ~1.5 MB, 11 would be ~16.5 MB, exceeding 15 MB.
	totalExpected := perFileSize * numFiles
	if totalExpected <= ipc.MaxTotalImageBytes {
		t.Skipf("test setup: %d x %d = %d, need > %d", numFiles, perFileSize, totalExpected, ipc.MaxTotalImageBytes)
	}

	_, err := convertIPCImages(ipcImgs, 0)
	if err == nil {
		t.Fatal("expected aggregate size error")
	}
	if !strings.Contains(err.Error(), "total image size") {
		t.Errorf("expected 'total image size' in error, got: %v", err)
	}
}

func TestConvertIPCImages_AggregateSizeJustUnderLimit(t *testing.T) {
	dir := t.TempDir()

	basePNG := testPNG()
	// Create 9 small valid PNG files — aggregate well under limit.
	var ipcImgs []ipc.IPCImage
	for i := 0; i < 9; i++ {
		filename := filepath.Join(dir, fmt.Sprintf("small%d.png", i))
		if err := os.WriteFile(filename, basePNG, 0o644); err != nil {
			t.Fatal(err)
		}
		ipcImgs = append(ipcImgs, ipc.IPCImage{
			Path:      filename,
			MediaType: "image/png",
		})
	}

	result, err := convertIPCImages(ipcImgs, 0)
	if err != nil {
		t.Fatalf("expected success for 9 small images, got: %v", err)
	}
	if len(result) != 9 {
		t.Errorf("expected 9 attachments, got %d", len(result))
	}
}

func TestConvertIPCImages_MixedPathAndDataExceedsAggregate(t *testing.T) {
	dir := t.TempDir()

	// One path image that's near the per-file but not over .
	path := filepath.Join(dir, "big.png")
	// Use a valid PNG padded to ~MaxTotalImageBytes to exceed aggregate.
	// Without reading fully, the stat will see the large size.
	fileData := make([]byte, ipc.MaxTotalImageBytes)
	copy(fileData, testPNG())
	if err := os.WriteFile(path, fileData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Plus a small inline data image to push over.
	// The path alone is already MaxTotalImageBytes, so any inline data
	// pushes over.
	ipcImgs := []ipc.IPCImage{
		{Path: path, MediaType: "image/png"},
		{Data: "data", MediaType: "image/png"},
	}

	_, err := convertIPCImages(ipcImgs, 0)
	if err == nil {
		t.Fatal("expected aggregate size error for mixed path+data exceeding limit")
	}
}
