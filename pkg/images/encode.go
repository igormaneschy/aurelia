// Package images provides image encoding and validation utilities shared
// between the Telegram bot and TUI transports.
package images

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// ErrSymlinkRejected is returned when an image path is a symlink.
var ErrSymlinkRejected = errors.New("symlinks are not allowed for image files")

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

// ValidateImagePath validates that path points to a supported image file
// with no symlink indirection and within size limits. Does NOT read the full
// file — only stats it and reads the header bytes for MIME detection.
// Note: this has an inherent TOCTOU race since the file is reopened later.
// For daemon-side validation use ReadImageSafely instead.
// If maxBytes <= 0, DefaultMaxBytes is used.
// On success, returns the detected MIME type from content headers.
func ValidateImagePath(path string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	// Use Lstat to detect symlinks (best-effort for UI feedback).
	fi, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("access %s: %w", filepath.Base(path), err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", ErrSymlinkRejected
	}

	// Must be a regular file.
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", filepath.Base(path))
	}

	// Check size before read.
	size := fi.Size()
	if size > int64(maxBytes) {
		return "", TooLargeError{Path: path, Size: int(size), Limit: maxBytes}
	}

	// Detect MIME from content header bytes.
	mimeType, err := DetectMIMEFromFile(path)
	if err != nil {
		return "", fmt.Errorf("detect image type: %w", err)
	}
	if !SupportedMIMEType(mimeType) {
		return "", fmt.Errorf("unsupported image type: %s", mimeType)
	}

	return mimeType, nil
}

// DetectMIMEFromFile reads the header of a file and detects its MIME type
// from magic bytes rather than trusting the file extension.
func DetectMIMEFromFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 512)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	buf = buf[:n]
	if n == 0 {
		return "", errors.New("empty file")
	}

	// Detect by magic bytes.
	switch {
	case n >= 4 && buf[0] == 0x89 && buf[1] == 0x50 && buf[2] == 0x4e && buf[3] == 0x47:
		return "image/png", nil
	case n >= 2 && buf[0] == 0xff && buf[1] == 0xd8:
		return "image/jpeg", nil
	case n >= 4 && buf[0] == 0x47 && buf[1] == 0x49 && buf[2] == 0x46:
		return "image/gif", nil
	case n >= 12 && buf[0] == 0x52 && buf[1] == 0x49 && buf[2] == 0x46 && buf[3] == 0x46 &&
		buf[8] == 0x57 && buf[9] == 0x45 && buf[10] == 0x42 && buf[11] == 0x50:
		// RIFF header: "RIFF" + 4-byte size + "WEBP".
		return "image/webp", nil
	}

	return "", fmt.Errorf("unknown file type (%d bytes read)", n)
}

// validateImageContent decodes the image data to verify it is structurally
// valid. Returns the detected MIME type. For WEBP (no stdlib decoder),
// bounded structural RIFF validation is used.
func validateImageContent(data []byte) (string, error) {
	if len(data) < 12 {
		return "", errors.New("image data too short")
	}

	// Detect by magic bytes, then decode for structural validation.
	switch {
	case len(data) >= 4 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4e && data[3] == 0x47:
		if _, err := png.Decode(bytes.NewReader(data)); err != nil {
			return "", fmt.Errorf("invalid PNG: %w", err)
		}
		return "image/png", nil

	case len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8:
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			return "", fmt.Errorf("invalid JPEG: %w", err)
		}
		return "image/jpeg", nil

	case len(data) >= 4 && data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46:
		if _, err := gif.Decode(bytes.NewReader(data)); err != nil {
			return "", fmt.Errorf("invalid GIF: %w", err)
		}
		return "image/gif", nil

	case len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50:
		// WEBP: bounded structural RIFF validation (no stdlib decoder available).
		if err := validateWEBP(data); err != nil {
			return "", err
		}
		return "image/webp", nil
	}

	return "", errors.New("unsupported or unknown image format")
}

// validateWEBP performs bounded structural validation of WEBP data.
// WEBP is a RIFF container: "RIFF" + size + "WEBP" + chunks.
// Without a stdlib decoder, we validate the container structure and
// verify at least one well-formed chunk exists.
func validateWEBP(data []byte) error {
	if len(data) < 12 {
		return errors.New("WEBP data too short for RIFF header")
	}

	// Validate RIFF header.
	if string(data[:4]) != "RIFF" {
		return errors.New("missing RIFF header")
	}
	if string(data[8:12]) != "WEBP" {
		return errors.New("missing WEBP identifier")
	}

	// The RIFF size field (bytes 4-7) is little-endian uint32 = file size - 8.
	riffSize := int(binary.LittleEndian.Uint32(data[4:8]))
	expectedMin := 12 + 4 // header + first chunk header
	if riffSize < expectedMin {
		return fmt.Errorf("RIFF size %d too small for WEBP", riffSize)
	}
	if riffSize > DefaultMaxBytes {
		return fmt.Errorf("RIFF size %d exceeds max allowed", riffSize)
	}

	// Check that at least one well-known chunk type follows.
	// WEBP files have VP8 (lossy), VP8L (lossless), or VP8X (extended) chunk.
	if len(data) < 16 {
		return errors.New("WEBP data too short for chunk header")
	}
	chunkID := string(data[12:16])
	switch chunkID {
	case "VP8 ", "VP8L", "VP8X":
		// Valid chunk type — accept.
	default:
		return fmt.Errorf("unknown WEBP chunk type %q", chunkID)
	}

	// Chunk size is bytes 16-19 (little-endian uint32, does not include
	// the 8-byte chunk header). Bounded check.
	if len(data) >= 20 {
		chunkSize := int(binary.LittleEndian.Uint32(data[16:20]))
		if chunkSize > DefaultMaxBytes {
			return fmt.Errorf("WEBP chunk size %d exceeds max allowed", chunkSize)
		}
	}

	return nil
}

// ReadImageSafely opens a file with O_NOFOLLOW (preventing symlink following),
// validates it is a supported image within size limits (using structural decode),
// and returns the full file content with the detected MIME type.
// All operations use the same file descriptor to prevent TOCTOU races.
// If maxBytes <= 0, DefaultMaxBytes is used.
func ReadImageSafely(path string, maxBytes int) (data []byte, mediaType string, err error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	// Open with O_NOFOLLOW: if path is a symlink, open fails with ELOOP.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, "", ErrSymlinkRejected
		}
		return nil, "", fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }()

	// Stat the opened fd (guaranteed same file — not a symlink).
	fi, err := f.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("stat %s: %w", filepath.Base(path), err)
	}
	if !fi.Mode().IsRegular() {
		return nil, "", fmt.Errorf("not a regular file: %s", filepath.Base(path))
	}
	if fi.Size() > int64(maxBytes) {
		return nil, "", TooLargeError{Path: path, Size: int(fi.Size()), Limit: maxBytes}
	}

	// Read bounded content from the same fd.
	readLimit := int64(maxBytes) + 1
	if fi.Size() < readLimit {
		readLimit = fi.Size()
	}
	buf := make([]byte, readLimit)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, "", fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	buf = buf[:n]

	// Not strictly needed since size was checked, but as defense-in-depth.
	if int64(len(buf)) > int64(maxBytes) {
		return nil, "", TooLargeError{Path: path, Size: len(buf), Limit: maxBytes}
	}

	// Validate image structure by decoding.
	detectedMIME, err := validateImageContent(buf)
	if err != nil {
		return nil, "", err
	}

	return buf, detectedMIME, nil
}

// SanitizedError returns an error string suitable for user-facing display.
// Full local paths are replaced by the basename to prevent information
// leakage in the UI.
func SanitizedError(err error) string {
	if err == nil {
		return ""
	}
	// TooLargeError has its own UserMessage with size info but no path.
	var tlErr TooLargeError
	if errors.As(err, &tlErr) {
		return tlErr.UserMessage()
	}
	// For other errors, attempt to extract a basename-containing message.
	s := err.Error()
	// Replace known patterns like 'access /path/to/file: ...' with shorter form.
	const pathPrefix = "access "
	if idx := strings.Index(s, pathPrefix); idx >= 0 {
		after := s[idx+len(pathPrefix):]
		if colon := strings.IndexAny(after, ":"); colon >= 0 {
			// s has the form "... access filename: ..." — keep just "filename: ..."
			return after[colon+1:]
		}
		if after != "" {
			return after
		}
	}
	if idx := strings.Index(s, ": "); idx >= 0 {
		return s[idx+2:]
	}
	return s
}

// Encode reads an image file, base64-encodes it, and returns an
// ImageAttachment suitable for the bridge protocol.
// As defense-in-depth, it checks the file size via stat before reading the
// full content.
// Deprecated: prefer ReadImageSafely which handles TOCTOU and validation.
// If maxBytes <= 0, DefaultMaxBytes is used.
func Encode(filePath, defaultMIME string, maxBytes int) (bridge.ImageAttachment, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		return bridge.ImageAttachment{}, fmt.Errorf("stat image %q: %w", filePath, err)
	}
	if fi.Size() > int64(maxBytes) {
		return bridge.ImageAttachment{}, TooLargeError{Path: filePath, Size: int(fi.Size()), Limit: maxBytes}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return bridge.ImageAttachment{}, fmt.Errorf("read image %q: %w", filePath, err)
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
