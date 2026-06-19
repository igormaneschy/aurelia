package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// pendingAttachment represents a document file waiting to be sent.
type pendingAttachment struct {
	path string // absolute filesystem path
	name string // display name (filename only)
}

// attachDocumentFromPath validates and adds a document to the pending list.
// Returns an error message if the document is invalid (displayed in chat).
func (m *Model) attachDocumentFromPath(path string) string {
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

	// Use Lstat to detect symlinks without following them.
	fi, err := os.Lstat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("File not found: %s", filepath.Base(absPath))
		}
		return fmt.Sprintf("Cannot access file: %v", err)
	}

	// Reject symlinks explicitly.
	if fi.Mode()&os.ModeSymlink != 0 {
		return "Symlinks are not allowed for attachments"
	}

	// Must be a regular file.
	if !fi.Mode().IsRegular() {
		return fmt.Sprintf("Not a regular file: %s", filepath.Base(absPath))
	}

	// Check size limit.
	if fi.Size() > int64(ipc.MaxAttachmentBytes) {
		sizeMB := float64(fi.Size()) / (1024 * 1024)
		limitMB := int64(ipc.MaxAttachmentBytes) / (1024 * 1024)
		return fmt.Sprintf("Attachment too large (%.1f MB). Limit is %d MB.", sizeMB, limitMB)
	}

	m.pendingAttachments = append(m.pendingAttachments, pendingAttachment{
		path: absPath,
		name: filepath.Base(absPath),
	})

	return ""
}

// clearPendingAttachments removes all pending document attachments.
func (m *Model) clearPendingAttachments() {
	m.pendingAttachments = nil
}

// pendingAttachmentBadges returns a display string for pending attachments.
func (m *Model) pendingAttachmentBadges() string {
	if len(m.pendingAttachments) == 0 {
		return ""
	}
	var badges []string
	for _, att := range m.pendingAttachments {
		badges = append(badges, fmt.Sprintf("[📎 %s]", att.name))
	}
	return strings.Join(badges, " ")
}

// toIPCAttachments converts pending attachments to IPC format for sending.
func (m *Model) toIPCAttachments() []ipc.IPCAttachment {
	if len(m.pendingAttachments) == 0 {
		return nil
	}
	var result []ipc.IPCAttachment
	for _, att := range m.pendingAttachments {
		result = append(result, ipc.IPCAttachment{
			Path: att.path,
			Name: att.name,
		})
	}
	return result
}

// tryParseAsDocumentPath detects whether text is an absolute path to a regular
// non-image document file. Returns the display name, resolved path, and true
// if valid. Returns ("", "", false) for image paths (image flow should handle),
// non-existent paths, symlinks, directories, or text that is not a path.
func tryParseAsDocumentPath(text string) (name, path string, ok bool) {
	text = strings.TrimSpace(text)
	if !filepath.IsAbs(text) {
		return "", "", false
	}
	if isImagePath(text) {
		return "", "", false // let image flow handle it
	}
	fi, err := os.Lstat(text)
	if err != nil {
		return "", "", false
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", "", false
	}
	if !fi.Mode().IsRegular() {
		return "", "", false
	}
	return filepath.Base(text), text, true
}
