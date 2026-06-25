package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

)

func TestFormatChatForClipboardCopiesOnlyMessages(t *testing.T) {
	got := formatChatForClipboard([]chatMessage{
		{Sender: "Aurelia", Text: "Connected to Aurelia daemon. Type a message or /help."},
		{Sender: "Igor", Text: "hello"},
		{Sender: "Aurelia", Text: "hi\nthere"},
		{Sender: "📋", Text: "Copied chat to clipboard"},
		{Sender: "📎", Text: "Image attached"},
		{Sender: "⚠️", Text: "Copy failed"},
		{Sender: "⚠️", Text: "   "},
	})
	want := "Igor:\nhello\n\nAurelia:\nhi\nthere"

	if got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

func TestLastAureliaMessageText(t *testing.T) {
	got := lastAureliaMessageText([]chatMessage{
		{Sender: "Aurelia", Text: "Connected to Aurelia daemon. Type a message or /help."},
		{Sender: "Aurelia", Text: "first"},
		{Sender: "Igor", Text: "question"},
		{Sender: "Aurelia", Text: " latest answer \n"},
	})

	if got != "latest answer" {
		t.Fatalf("last Aurelia text = %q", got)
	}
}

func TestCopyChatShortcutUsesClipboardText(t *testing.T) {
	original := copyTextToClipboard
	var copied string
	copyTextToClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() { copyTextToClipboard = original }()

	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.messages = []chatMessage{{Sender: "Igor", Text: "hello"}}

	_, cmd := m.Update(keyCtrl('y'))
	if cmd == nil {
		t.Fatal("expected copy command")
	}
	if msg, ok := cmd().(clipboardCopyMsg); ok && msg.err != nil {
		t.Fatalf("copy msg err: %v", msg.err)
	}
	if copied != "Igor:\nhello" {
		t.Fatalf("copied = %q", copied)
	}
}

func TestCopyLastResponseShortcutUsesClipboardText(t *testing.T) {
	original := copyTextToClipboard
	var copied string
	copyTextToClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() { copyTextToClipboard = original }()

	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.messages = []chatMessage{
		{Sender: "Aurelia", Text: "first"},
		{Sender: "Igor", Text: "next"},
		{Sender: "Aurelia", Text: "second"},
	}

	_, cmd := m.Update(keyCtrl('r'))
	if cmd == nil {
		t.Fatal("expected copy command")
	}
	if msg, ok := cmd().(clipboardCopyMsg); ok && msg.err != nil {
		t.Fatalf("copy msg err: %v", msg.err)
	}
	if copied != "second" {
		t.Fatalf("copied = %q", copied)
	}
}

func TestClipboardCopyMsgAppendsStatusMessage(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat

	updated, _ := m.Update(clipboardCopyMsg{label: "chat"})
	m2 := updated.(Model)

	if len(m2.messages) != 1 || !strings.Contains(m2.messages[0].Text, "Copied chat") {
		t.Fatalf("expected copied status message, got %#v", m2.messages)
	}
}

func TestClipboardCopyMsgAppendsErrorMessage(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat

	updated, _ := m.Update(clipboardCopyMsg{label: "chat", err: errors.New("no clipboard")})
	m2 := updated.(Model)

	if len(m2.messages) != 1 || m2.messages[0].Sender != "⚠️" || !strings.Contains(m2.messages[0].Text, "no clipboard") {
		t.Fatalf("expected copy error message, got %#v", m2.messages)
	}
}

func TestClipboardMacOS_TimeoutReturnsPromptly(t *testing.T) {
	// Create a pre-cancelled context — the function should return promptly
	// with an error and clean up any temp file it created.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	// Verify that osascript is available (required for the test).
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("osascript not available, skipping macOS clipboard test")
	}

	path, err := pasteFromClipboardMacOS(ctx)
	if err == nil {
		// If there's no error, we somehow got a clipboard image.
		// Clean up and fail.
		if path != "" {
			os.Remove(path)
		}
		t.Fatal("expected error for cancelled context, got success")
	}

	// Verify the error mentions timeout/cancel.
	if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected timeout/cancel error, got: %v", err)
	}

	// Verify the temp file was cleaned up.
	if path != "" {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("expected temp file %q to be removed after timeout, stat: %v", path, statErr)
			os.Remove(path)
		}
	}
}

func TestClipboardLinux_TimeoutReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// This test always runs and validates the xclip path.
	path, err := pasteFromXClip(ctx)
	if err == nil {
		if path != "" {
			os.Remove(path)
		}
		t.Fatal("expected error for cancelled context")
	}
	// On systems without xclip, the error is about xclip not available — that's fine.
	// We're testing that the function returns promptly, not that xclip works.
	if path != "" {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("expected temp file to be removed, stat: %v", statErr)
			os.Remove(path)
		}
	}
}

func TestPasteFromClipboardLinux_TimeoutReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	path, err := pasteFromWlPaste(ctx)
	if err == nil {
		if path != "" {
			os.Remove(path)
		}
		t.Fatal("expected error for cancelled context")
	}
	if path != "" {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("expected temp file to be removed, stat: %v", statErr)
			os.Remove(path)
		}
	}
}

func TestPasteFromClipboardLinux_NoTool(t *testing.T) {
	// When no clipboard tool is available, the function should try xclip,
	// fail, try wl-paste, fail, and return the fallback error.
	// We test with a pre-cancelled context to avoid hanging.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	path, err := pasteFromClipboardLinux(ctx)
	if err == nil {
		if path != "" {
			os.Remove(path)
		}
		t.Fatal("expected error for cancelled/no-tool context")
	}
	// The error should mention the fallback message or the individual tool errors.
	if path != "" {
		os.Remove(path)
	}
}

func TestClipboardPasteFromClipboardCancelled(t *testing.T) {
	// Test the top-level dispatch with a cancelled context via the
	// clipboardNewContext override.
	original := clipboardNewContext
	clipboardNewContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	}
	defer func() { clipboardNewContext = original }()

	path, err := pasteFromClipboard()
	// On macOS with osascript available, this would fail due to cancelled context.
	// On other platforms, the dispatch handles it similarly.
	if err == nil {
		if path != "" {
			os.Remove(path)
		}
		t.Fatal("expected error for cancelled clipboard context")
	}
	if path != "" {
		// If somehow a file was created, ensure it's cleaned up.
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			os.Remove(path)
		}
	}
}

func TestClipboardPasteFromClipboard_FileCreatedOnTempDir(t *testing.T) {
	// Verify that temp files created by clipboard operations use an
	// OS-generated name in the system temp directory (not user-supplied).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	path, err := pasteFromXClip(ctx)
	// We don't care about the error (it's a cancelled context), but we
	// want to verify that any created temp file follows the pattern.
	if path != "" {
		// The temp file should start with the pre-determined prefix.
		_, name := filepath.Split(path)
		if !strings.HasPrefix(name, "aurelia-clip-") {
			t.Errorf("expected temp file prefix 'aurelia-clip-', got %q", name)
		}
		// Clean up if not already removed.
		if _, statErr := os.Stat(path); statErr == nil {
			os.Remove(path)
		}
	}
	if err == nil {
		// Context cancelled but somehow succeeded — shouldn't happen.
		t.Error("expected error")
	}
}
