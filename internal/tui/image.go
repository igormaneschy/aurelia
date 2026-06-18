package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/pkg/images"
)

// pendingImage represents an image waiting to be sent with the next message.
type pendingImage struct {
	path string // filesystem path to the image
	name string // display name (filename only)
}

// attachImageFromPath validates and adds an image to the pending list.
// Returns an error message if the image is invalid (displayed in chat).
func (m *Model) attachImageFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "Usage: /img <path-to-image>"
	}

	// Expand ~ to home directory.
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[1:])
		}
	}

	// Resolve to absolute path.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Sprintf("Invalid path: %v", err)
	}

	// Check file exists.
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("File not found: %s", path)
		}
		return fmt.Sprintf("Cannot access file: %v", err)
	}

	// Check it's a regular file.
	if !info.Mode().IsRegular() {
		return fmt.Sprintf("Not a regular file: %s", path)
	}

	// Check MIME type.
	mimeType := images.MIMEFromPath(absPath)
	if !images.SupportedMIMEType(mimeType) {
		ext := strings.ToLower(filepath.Ext(absPath))
		return fmt.Sprintf("Unsupported image type: %s. Supported: png, jpg, jpeg, gif, webp", ext)
	}

	// Check file size (10 MB limit).
	if info.Size() > int64(images.DefaultMaxBytes) {
		return fmt.Sprintf("Image too large (%s). Limit is %s.",
			images.HumanBytes(int(info.Size())), images.HumanBytes(images.DefaultMaxBytes))
	}

	// Add to pending images.
	m.pendingImages = append(m.pendingImages, pendingImage{
		path: absPath,
		name: filepath.Base(absPath),
	})

	return ""
}

// clearPendingImages removes all pending image attachments.
func (m *Model) clearPendingImages() {
	m.pendingImages = nil
}

// pendingImageBadges returns a display string for pending images.
func (m *Model) pendingImageBadges() string {
	if len(m.pendingImages) == 0 {
		return ""
	}
	var names []string
	for _, img := range m.pendingImages {
		names = append(names, img.name)
	}
	return fmt.Sprintf("📎 %s", strings.Join(names, ", "))
}

// toIPCImages converts pending images to IPCImage slice for sending.
func (m *Model) toIPCImages() []ipc.IPCImage {
	if len(m.pendingImages) == 0 {
		return nil
	}
	var result []ipc.IPCImage
	for _, img := range m.pendingImages {
		mimeType := images.MIMEFromPath(img.path)
		result = append(result, ipc.IPCImage{
			Path:      img.path,
			MediaType: mimeType,
		})
	}
	return result
}
