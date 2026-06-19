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
	path      string // filesystem path to the image
	name      string // display name (filename only)
	mediaType string // detected MIME type from content validation
	isTemp    bool   // true if Aurelia-created temp file (clipboard) — never a user-supplied path
}

type imagePathCandidate struct {
	start int
	end   int
	path  string
}

// normalizeImagePath normalizes a pasted/dragged image path string:
// strips file:// prefix, removes wrapping quotes, and unescapes \ spaces.
func normalizeImagePath(s string) string {
	s = strings.TrimSpace(s)
	// Unquote if wrapped in matching quotes (before file:// stripping
	// since a quoted path may wrap a file:// URL inside).
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if first == last && (first == '"' || first == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	// Strip file:// prefix (case-insensitive).
	if len(s) > 7 && strings.EqualFold(s[:7], "file://") {
		s = s[7:]
	}
	// Unescape backslash-escaped spaces (common on macOS/Linux drag-drop).
	s = strings.ReplaceAll(s, "\\ ", " ")
	return s
}

// attachImagePathsFromText finds local image paths embedded in free-form text,
// attaches them, and returns the prompt text with those paths removed. It lets
// natural prompts like "describe /path/to/screenshot.png" use the same IPC image
// pipeline as /img without asking the model to call file tools itself.
func (m *Model) attachImagePathsFromText(text string) (string, int, string) {
	candidates := imagePathCandidates(text)
	if len(candidates) == 0 {
		return text, 0, ""
	}

	originalCount := len(m.pendingImages)
	for _, candidate := range candidates {
		if errMsg := m.attachImageFromPath(candidate.path); errMsg != "" {
			m.pendingImages = m.pendingImages[:originalCount]
			return text, 0, errMsg
		}
	}

	cleaned := text
	for i := len(candidates) - 1; i >= 0; i-- {
		c := candidates[i]
		cleaned = cleaned[:c.start] + cleaned[c.end:]
	}
	return cleanPromptAfterPathRemoval(cleaned), len(candidates), ""
}

func imagePathCandidates(text string) []imagePathCandidate {
	var candidates []imagePathCandidate
	for i := 0; i < len(text); i++ {
		if isQuote(text[i]) {
			if candidate, ok := quotedImagePathCandidate(text, i); ok {
				candidates = append(candidates, candidate)
				i = candidate.end - 1
			}
			continue
		}

		if !isLocalPathStart(text, i) {
			continue
		}
		end, ok := imagePathEnd(text[i:])
		if !ok {
			continue
		}
		candidate := text[i : i+end]
		if !isImagePath(candidate) {
			continue
		}
		candidates = append(candidates, imagePathCandidate{start: i, end: i + end, path: candidate})
		i += end - 1
	}
	return candidates
}

// startsWithSyntacticImagePath checks whether text starts with a string that
// syntactically looks like a local image path (leading /, ~/, or file:// with
// an image extension). Unlike startsWithLocalImagePath, it does NOT validate
// that the file exists or is a valid image — that is deferred to
// attachImagePathsFromText so actionable errors are surfaced to the user.
//
// It handles three categories of leading path:
//  1. Quoted paths such as  "/dir with spaces/photo.png" desc
//     → The opening quote at text[0] signals a reliable path boundary.
//  2. Escaped-space paths such as  /dir\ with\ spaces/photo.png desc
//     → The `\ ` sequence signals intentional path content.
//  3. Simple paths such as  /tmp/missing.png desc  or  /status /p/f.png
//     → For these, the first word must end with an image extension so that
//     "/status /path/file.png" is not misidentified as an image path.
func startsWithSyntacticImagePath(text string) bool {
	candidates := imagePathCandidates(text)
	if len(candidates) == 0 || candidates[0].start != 0 {
		return false
	}

	// Category 1: quoted path — quotes define an unambiguous path boundary.
	if len(text) > 0 && isQuote(text[0]) {
		return true
	}

	path := candidates[0].path

	// Category 2: escaped-space path — backslash-escaped spaces are
	// intentional path components, not word separators.
	if strings.Contains(path, "\\ ") {
		return true
	}

	// Category 3: simple path — verify the first whitespace-delimited word
	// ends with an image extension. This prevents "/status /path/file.png"
	// (command + argument) from being detected as an image path.
	firstWord := path
	if idx := strings.IndexAny(firstWord, " \t\n\r"); idx >= 0 {
		firstWord = firstWord[:idx]
	}
	ext := strings.ToLower(filepath.Ext(firstWord))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

func quotedImagePathCandidate(text string, start int) (imagePathCandidate, bool) {
	quote := text[start]
	close := strings.IndexByte(text[start+1:], quote)
	if close < 0 {
		return imagePathCandidate{}, false
	}
	end := start + 1 + close + 1
	inner := text[start+1 : end-1]
	if !looksLikeLocalImagePath(inner) {
		return imagePathCandidate{}, false
	}
	return imagePathCandidate{start: start, end: end, path: inner}, true
}

func looksLikeLocalImagePath(s string) bool {
	s = normalizeImagePath(s)
	return hasLocalPathPrefix(s) && isImagePath(s)
}

func isLocalPathStart(text string, i int) bool {
	if !isPathBoundary(text, i) {
		return false
	}
	return hasLocalPathPrefix(text[i:])
}

func hasLocalPathPrefix(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~/") || hasFileURLPrefix(s)
}

func hasFileURLPrefix(s string) bool {
	return len(s) >= len("file://") && strings.EqualFold(s[:len("file://")], "file://")
}

func isPathBoundary(text string, i int) bool {
	if i == 0 {
		return true
	}
	prev := text[i-1]
	return prev == ' ' || prev == '\t' || prev == '\n' || prev == '\r' ||
		prev == '(' || prev == '[' || prev == '{' || prev == '<'
}

func imagePathEnd(s string) (int, bool) {
	lower := strings.ToLower(s)
	for i := 0; i < len(lower); i++ {
		for _, ext := range []string{".jpeg", ".webp", ".png", ".jpg", ".gif"} {
			if !strings.HasPrefix(lower[i:], ext) {
				continue
			}
			end := i + len(ext)
			if end == len(s) || isPathTerminator(s[end]) {
				return end, true
			}
		}
	}
	return 0, false
}

func isPathTerminator(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' ||
		b == '\'' || b == '"' || b == ')' || b == ']' || b == '}' ||
		b == '>' || b == ',' || b == ';' || b == ':'
}

func isQuote(b byte) bool {
	return b == '\'' || b == '"'
}

func cleanPromptAfterPathRemoval(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// attachImageFromPath validates and adds an image to the pending list.
// Returns an error message if the image is invalid (displayed in chat).
func (m *Model) attachImageFromPath(path string) string {
	path = normalizeImagePath(path)
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

	// Full validation via images.ValidateImagePath: Lstat symlink check,
	// pre-read size limit, and content-MIME detection.
	mimeType, err := images.ValidateImagePath(absPath, 0)
	if err != nil {
		if err == images.ErrSymlinkRejected {
			return "Symlinks are not allowed for image attachments"
		}
		return images.SanitizedError(err)
	}

	// Add to pending images with content-detected MIME.
	m.pendingImages = append(m.pendingImages, pendingImage{
		path:      absPath,
		name:      filepath.Base(absPath),
		mediaType: mimeType,
	})

	return ""
}

// attachTempImage adds a clipboard temp file to pending images.
// The file will be removed when pending images are cleared or after send.
func (m *Model) attachTempImage(path string) string {
	path = normalizeImagePath(path)
	if path == "" {
		return "Usage: /img <path-to-image>"
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Sprintf("Invalid path: %v", err)
	}

	mimeType, err := images.ValidateImagePath(absPath, 0)
	if err != nil {
		if err == images.ErrSymlinkRejected {
			_ = os.Remove(absPath)
			return "Symlinks are not allowed for image attachments"
		}
		_ = os.Remove(absPath)
		return images.SanitizedError(err)
	}

	m.pendingImages = append(m.pendingImages, pendingImage{
		path:      absPath,
		name:      filepath.Base(absPath),
		mediaType: mimeType,
		isTemp:    true,
	})
	return ""
}

// clearPendingImages removes all pending image attachments and cleans up
// any temp files Aurelia created.
func (m *Model) clearPendingImages() {
	m.cleanupTempImages()
	m.pendingImages = nil
}

// cleanupTempImages removes temp files (clipboard-created) from pending
// images without clearing the pending list. Safe to call multiple times.
func (m *Model) cleanupTempImages() {
	for _, img := range m.pendingImages {
		if img.isTemp {
			_ = os.Remove(img.path)
		}
	}
}

func (m *Model) tempImagePaths() []string {
	var paths []string
	for _, img := range m.pendingImages {
		if img.isTemp {
			paths = append(paths, img.path)
		}
	}
	return paths
}

func (m *Model) cleanupSubmittedTempImages() {
	for _, path := range m.submittedTempImagePaths {
		_ = os.Remove(path)
	}
	m.submittedTempImagePaths = nil
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
// MediaType is populated from content detection stored during attachment
// validation, not from file extension, so the IPC server's media_type
// validation accepts images with missing/wrong extensions.
func (m *Model) toIPCImages() []ipc.IPCImage {
	if len(m.pendingImages) == 0 {
		return nil
	}
	var result []ipc.IPCImage
	for _, img := range m.pendingImages {
		result = append(result, ipc.IPCImage{
			Path:      img.path,
			MediaType: img.mediaType,
		})
	}
	return result
}
