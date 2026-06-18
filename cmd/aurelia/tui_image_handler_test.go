package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

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
	// Create a test image file.
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	data := []byte{0x89, 0x50, 0x4e, 0x47} // PNG header
	if err := os.WriteFile(path, data, 0o644); err != nil {
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
	if result[0].Path != path {
		t.Errorf("Path = %q, want %q", result[0].Path, path)
	}
	if result[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want %q", result[0].MediaType, "image/png")
	}
	if result[0].Data == "" {
		t.Error("Data should not be empty")
	}
}

func TestConvertIPCImages_FromData(t *testing.T) {
	images := []ipc.IPCImage{
		{Data: "base64data", MediaType: "image/jpeg"},
	}

	result, err := convertIPCImages(images, 0)
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
	images := []ipc.IPCImage{
		{Path: "/nonexistent/image.png", MediaType: "image/png"},
	}

	_, err := convertIPCImages(images, 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestValidateImageFiles_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	images := []ipc.IPCImage{
		{Path: path, MediaType: "image/png"},
	}

	if err := validateImageFiles(images); err != nil {
		t.Fatalf("validateImageFiles() error = %v", err)
	}
}

func TestValidateImageFiles_NotFound(t *testing.T) {
	images := []ipc.IPCImage{
		{Path: "/nonexistent/image.png", MediaType: "image/png"},
	}

	if err := validateImageFiles(images); err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
