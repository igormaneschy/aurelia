// Package images provides image encoding and validation utilities shared
// between the Telegram bot and TUI transports.
package images

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// DefaultMaxBytes is the default maximum image size (10 MB).
const DefaultMaxBytes = 10 * 1024 * 1024

// supportedMIMETypes is the set of MIME types accepted by the vision models.
var supportedMIMETypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// TooLargeError is returned when an image exceeds the size limit.
type TooLargeError struct {
	Path  string
	Size  int
	Limit int
}

func (e TooLargeError) Error() string {
	return fmt.Sprintf("image %q is %d bytes, exceeds %d byte limit", e.Path, e.Size, e.Limit)
}

// UserMessage returns a human-readable error message for display in the UI.
func (e TooLargeError) UserMessage() string {
	return fmt.Sprintf("Image too large (%s). Limit is %s.", HumanBytes(e.Size), HumanBytes(e.Limit))
}

// HumanBytes formats an integer byte count as a human-readable string.
func HumanBytes(n int) string {
	const (
		kib = 1024
		mib = kib * 1024
	)

	if n < kib {
		return fmt.Sprintf("%d B", n)
	}
	if n < mib {
		return fmt.Sprintf("%.1f KB", float64(n)/kib)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/mib)
}

// SupportedMIMEType reports whether the MIME type is accepted by vision models.
func SupportedMIMEType(mimeType string) bool {
	return supportedMIMETypes[strings.ToLower(strings.TrimSpace(mimeType))]
}

// MIMEFromPath guesses the MIME type from a file extension.
// Returns empty string if the extension is unknown.
func MIMEFromPath(path string) string {
	guessed := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	return guessed
}

// IsSupportedImageFile reports whether the file (by name and/or MIME type)
// is a supported image format.
func IsSupportedImageFile(filename, mimeType string) bool {
	if SupportedMIMEType(mimeType) {
		return true
	}
	return SupportedMIMEType(MIMEFromPath(filename))
}

// Encode reads an image file, base64-encodes it, and returns an
// ImageAttachment suitable for the bridge protocol.
// If maxBytes <= 0, DefaultMaxBytes is used.
func Encode(filePath, defaultMIME string, maxBytes int) (bridge.ImageAttachment, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return bridge.ImageAttachment{}, fmt.Errorf("read image %q: %w", filePath, err)
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if len(data) > maxBytes {
		return bridge.ImageAttachment{}, TooLargeError{Path: filePath, Size: len(data), Limit: maxBytes}
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return bridge.ImageAttachment{
		Path:      filePath,
		Data:      encoded,
		MediaType: defaultMIME,
	}, nil
}
