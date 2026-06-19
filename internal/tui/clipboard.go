package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// clipboardTimeout is the maximum time to wait for a clipboard subprocess.
var clipboardTimeout = 10 * time.Second

// clipboardNewContext returns a cancellable context for clipboard operations.
// Exposed as a variable so tests can inject a pre-cancelled context.
var clipboardNewContext = func() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), clipboardTimeout)
}

// clipboardPasteMsg is sent when clipboard paste completes.
type clipboardPasteMsg struct {
	path string // path to temp file with image data
	err  error  // error if paste failed
}

// pasteFromClipboardCmd returns a tea.Cmd that reads an image from clipboard.
func pasteFromClipboardCmd() tea.Cmd {
	return func() tea.Msg {
		path, err := pasteFromClipboard()
		return clipboardPasteMsg{path: path, err: err}
	}
}

// pasteFromClipboard reads an image from the system clipboard and returns
// the path to a temporary file containing the image data.
// Returns an error if no image is in the clipboard or the operation fails.
func pasteFromClipboard() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		ctx, cancel := clipboardNewContext()
		defer cancel()
		return pasteFromClipboardMacOS(ctx)
	case "linux":
		ctx, cancel := clipboardNewContext()
		defer cancel()
		return pasteFromClipboardLinux(ctx)
	default:
		return "", fmt.Errorf("clipboard image paste not supported on %s", runtime.GOOS)
	}
}

// pasteFromClipboardMacOS uses osascript to read the clipboard as PNG.
// Uses exec.CommandContext with ctx to prevent hangs.
func pasteFromClipboardMacOS(ctx context.Context) (string, error) {
	// Check if osascript is available.
	if _, err := exec.LookPath("osascript"); err != nil {
		return "", fmt.Errorf("osascript not available: %w", err)
	}

	// Create temp file for the image.
	f, err := os.CreateTemp("", "aurelia-clip-*.png")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tempPath := f.Name()
	_ = f.Close()

	// Use osascript to get clipboard as PNG and write to file.
	// The -e flag runs the AppleScript command.
	script := `set theFile to POSIX file "` + tempPath + `"
try
	set theImage to the clipboard as «class PNGf»
	set fp to open for access theFile with write permission
	write theImage to fp
	close access fp
on error errMsg number errNum
	try
		close access theFile
	end try
	error errMsg number errNum
end try`

	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tempPath)
		if ctx.Err() != nil {
			return "", fmt.Errorf("clipboard timed out after %v", clipboardTimeout)
		}
		// Check if the error is "clipboard doesn't contain image data".
		if len(output) > 0 {
			return "", fmt.Errorf("clipboard error: %s", string(output))
		}
		return "", fmt.Errorf("osascript failed: %w", err)
	}

	// Verify the file was created and has content.
	info, err := os.Stat(tempPath)
	if err != nil {
		return "", fmt.Errorf("temp file not created: %w", err)
	}
	if info.Size() == 0 {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("no image in clipboard")
	}

	return tempPath, nil
}

// pasteFromClipboardLinux tries xclip and wl-paste to read clipboard image.
func pasteFromClipboardLinux(ctx context.Context) (string, error) {
	// Try xclip first.
	if path, err := pasteFromXClip(ctx); err == nil {
		return path, nil
	}

	// Try wl-paste (Wayland).
	if path, err := pasteFromWlPaste(ctx); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("clipboard image paste not supported. Use /img <path> instead")
}

// pasteFromXClip uses xclip to read clipboard image.
func pasteFromXClip(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("xclip"); err != nil {
		return "", fmt.Errorf("xclip not available: %w", err)
	}

	f, err := os.CreateTemp("", "aurelia-clip-*.png")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tempPath := f.Name()
	_ = f.Close()

	cmd := exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	output, err := cmd.Output()
	if err != nil {
		_ = os.Remove(tempPath)
		if ctx.Err() != nil {
			return "", fmt.Errorf("clipboard timed out after %v", clipboardTimeout)
		}
		return "", fmt.Errorf("xclip failed: %w", err)
	}

	if len(output) == 0 {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("no image in clipboard")
	}

	if err := os.WriteFile(tempPath, output, 0o600); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("write temp file: %w", err)
	}

	return tempPath, nil
}

// pasteFromWlPaste uses wl-paste to read clipboard image.
func pasteFromWlPaste(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("wl-paste"); err != nil {
		return "", fmt.Errorf("wl-paste not available: %w", err)
	}

	f, err := os.CreateTemp("", "aurelia-clip-*.png")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tempPath := f.Name()
	f.Close()

	cmd := exec.CommandContext(ctx, "wl-paste", "-t", "image/png")
	output, err := cmd.Output()
	if err != nil {
		os.Remove(tempPath)
		if ctx.Err() != nil {
			return "", fmt.Errorf("clipboard timed out after %v", clipboardTimeout)
		}
		return "", fmt.Errorf("wl-paste failed: %w", err)
	}

	if len(output) == 0 {
		os.Remove(tempPath)
		return "", fmt.Errorf("no image in clipboard")
	}

	if err := os.WriteFile(tempPath, output, 0o600); err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("write temp file: %w", err)
	}

	return tempPath, nil
}

// isImagePath checks if a string looks like an image file path.
// Handles uppercase extensions, quoted paths, file:// URLs, and
// backslash-escaped spaces (common in drag-and-drop on macOS/Linux).
func isImagePath(s string) bool {
	s = normalizeImagePath(s)
	ext := strings.ToLower(filepath.Ext(s))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}
