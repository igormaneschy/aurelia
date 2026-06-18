package ipc

import (
	"strings"
	"testing"
)

func TestReservedTUIChatID_Valid(t *testing.T) {
	if err := validateMessage(IPCMessage{
		Type:   MsgTypeSend,
		ChatID: ReservedTUIChatID,
		Text:   "hello",
	}); err != nil {
		t.Errorf("ReservedTUIChatID should be valid, got: %v", err)
	}
}

func TestReservedTUIChatID_NegativeOneRejected(t *testing.T) {
	err := validateMessage(IPCMessage{
		Type:   MsgTypeSend,
		ChatID: -1,
		Text:   "hello",
	})
	if err == nil {
		t.Fatal("expected error for ChatID=-1")
	}
	if !strings.Contains(err.Error(), "negative chat_id") {
		t.Errorf("expected 'negative chat_id' in error, got: %v", err)
	}
}

func TestReservedTUIRange_AllSlotsValid(t *testing.T) {
	for chatID := ReservedTUIChatID; chatID >= ReservedTUIChatIDFloor; chatID-- {
		if err := validateMessage(IPCMessage{
			Type:   MsgTypeSend,
			ChatID: chatID,
			Text:   "hello",
		}); err != nil {
			t.Errorf("ChatID %d should be valid in TUI range, got: %v", chatID, err)
		}
		if !IsReservedTUIID(chatID) {
			t.Errorf("IsReservedTUIID(%d) = false, want true", chatID)
		}
	}
}

func TestReservedTUIRange_OutsideRejected(t *testing.T) {
	outside := []int64{
		ReservedTUIChatIDFloor - 1, // too negative
		ReservedTUIChatID + 1,      // just above the DM (e.g. -9000000)
		-1,
		0,
		1,
	}
	for _, chatID := range outside {
		if IsReservedTUIID(chatID) {
			t.Errorf("IsReservedTUIID(%d) = true, want false", chatID)
		}
	}
}

func TestIsDefaultTUISession(t *testing.T) {
	if !IsDefaultTUISession(ReservedTUIChatID) {
		t.Errorf("IsDefaultTUISession(ReservedTUIChatID) = false, want true")
	}
	if IsDefaultTUISession(ReservedTUIChatID - 1) {
		t.Errorf("IsDefaultTUISession(non-DM) = true, want false")
	}
}

func TestDefaultSocketPath(t *testing.T) {
	path, err := DefaultSocketPath()
	if err != nil {
		t.Fatalf("DefaultSocketPath() returned error: %v", err)
	}

	if !strings.HasSuffix(path, "/.aurelia/aurelia.sock") {
		t.Errorf("expected path to end with /.aurelia/aurelia.sock, got %q", path)
	}

	if !strings.HasPrefix(path, "/") {
		t.Errorf("expected absolute path, got %q", path)
	}
}

func TestValidateMessage_ImagesValid(t *testing.T) {
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "describe this image",
		Images: []IPCImage{
			{Path: "/tmp/test.png", MediaType: "image/png"},
		},
	}
	if err := validateMessage(msg); err != nil {
		t.Errorf("valid image message should pass validation, got: %v", err)
	}
}

func TestValidateMessage_ImagesTooMany(t *testing.T) {
	images := make([]IPCImage, MaxImageCount+1)
	for i := range images {
		images[i] = IPCImage{Path: "/tmp/img.png", MediaType: "image/png"}
	}
	msg := IPCMessage{
		Type:   MsgTypeSend,
		Text:   "many images",
		Images: images,
	}
	err := validateMessage(msg)
	if err == nil {
		t.Fatal("expected error for too many images")
	}
	if !strings.Contains(err.Error(), "too many images") {
		t.Errorf("expected 'too many images' in error, got: %v", err)
	}
}

func TestValidateMessage_ImagePathOrDataRequired(t *testing.T) {
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "image with no path or data",
		Images: []IPCImage{
			{MediaType: "image/png"},
		},
	}
	err := validateMessage(msg)
	if err == nil {
		t.Fatal("expected error for image without path or data")
	}
	if !strings.Contains(err.Error(), "path or data required") {
		t.Errorf("expected 'path or data required' in error, got: %v", err)
	}
}

func TestValidateMessage_ImageMediaTypeRequired(t *testing.T) {
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "image with no media type",
		Images: []IPCImage{
			{Path: "/tmp/test.png"},
		},
	}
	err := validateMessage(msg)
	if err == nil {
		t.Fatal("expected error for image without media_type")
	}
	if !strings.Contains(err.Error(), "media_type required") {
		t.Errorf("expected 'media_type required' in error, got: %v", err)
	}
}

func TestValidateMessage_ImageUnsupportedMediaType(t *testing.T) {
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "unsupported image type",
		Images: []IPCImage{
			{Path: "/tmp/test.bmp", MediaType: "image/bmp"},
		},
	}
	err := validateMessage(msg)
	if err == nil {
		t.Fatal("expected error for unsupported image type")
	}
	if !strings.Contains(err.Error(), "unsupported media_type") {
		t.Errorf("expected 'unsupported media_type' in error, got: %v", err)
	}
}

func TestValidateMessage_ImageDataTooLarge(t *testing.T) {
	// Create a message with base64 data that exceeds the limit.
	largeData := strings.Repeat("A", MaxTotalImageBytes+1)
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "large image data",
		Images: []IPCImage{
			{Data: largeData, MediaType: "image/png"},
		},
	}
	err := validateMessage(msg)
	if err == nil {
		t.Fatal("expected error for total image data too large")
	}
	if !strings.Contains(err.Error(), "total image data too large") {
		t.Errorf("expected 'total image data too large' in error, got: %v", err)
	}
}
