package ipc

import (
	"encoding/json"
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

func TestValidateMessage_EmptyTextWithImagesValid(t *testing.T) {
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "",
		Images: []IPCImage{
			{Path: "/tmp/test.png", MediaType: "image/png"},
		},
	}
	if err := validateMessage(msg); err != nil {
		t.Errorf("image-only message should pass IPC validation, got: %v", err)
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

func TestValidateMessage_AttachmentValid(t *testing.T) {
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "review this document",
		Attachments: []IPCAttachment{
			{Path: "/home/user/doc.pdf", Name: "spec.pdf"},
		},
	}
	if err := validateMessage(msg); err != nil {
		t.Errorf("valid attachment message should pass, got: %v", err)
	}
}

func TestValidateMessage_AttachmentTooMany(t *testing.T) {
	atts := make([]IPCAttachment, MaxAttachmentCount+1)
	for i := range atts {
		atts[i] = IPCAttachment{Path: "/tmp/doc.pdf"}
	}
	msg := IPCMessage{
		Type:        MsgTypeSend,
		Text:        "many attachments",
		Attachments: atts,
	}
	err := validateMessage(msg)
	if err == nil {
		t.Fatal("expected error for too many attachments")
	}
	if !strings.Contains(err.Error(), "too many attachments") {
		t.Errorf("expected 'too many attachments' in error, got: %v", err)
	}
}

func TestValidateMessage_AttachmentPathRequired(t *testing.T) {
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "attachment with no path",
		Attachments: []IPCAttachment{
			{Name: "doc.pdf"},
		},
	}
	err := validateMessage(msg)
	if err == nil {
		t.Fatal("expected error for attachment without path")
	}
	if !strings.Contains(err.Error(), "path required") {
		t.Errorf("expected 'path required' in error, got: %v", err)
	}
}

func TestValidateMessage_AttachmentPathTooLong(t *testing.T) {
	longPath := strings.Repeat("a", 4097)
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "attachment with long path",
		Attachments: []IPCAttachment{
			{Path: longPath},
		},
	}
	err := validateMessage(msg)
	if err == nil {
		t.Fatal("expected error for attachment path too long")
	}
	if !strings.Contains(err.Error(), "path too long") {
		t.Errorf("expected 'path too long' in error, got: %v", err)
	}
}

func TestValidateMessage_AttachmentRelativePath(t *testing.T) {
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "attachment with relative path",
		Attachments: []IPCAttachment{
			{Path: "spec.md"},
		},
	}
	err := validateMessage(msg)
	if err == nil {
		t.Fatal("expected error for relative attachment path")
	}
	if !strings.Contains(err.Error(), "must be absolute") {
		t.Errorf("expected 'must be absolute' in error, got: %v", err)
	}
}

func TestValidateMessage_AttachmentAbsolutePath(t *testing.T) {
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "attachment with absolute path",
		Attachments: []IPCAttachment{
			{Path: "/home/user/doc.pdf", Name: "spec.pdf"},
		},
	}
	if err := validateMessage(msg); err != nil {
		t.Errorf("absolute path attachment should pass, got: %v", err)
	}
}

func TestValidateMessage_AttachmentNilAndEmpty(t *testing.T) {
	// Nil attachments field.
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "no attachments",
	}
	if err := validateMessage(msg); err != nil {
		t.Errorf("nil attachments should pass, got: %v", err)
	}

	// Empty slice.
	msg.Attachments = []IPCAttachment{}
	if err := validateMessage(msg); err != nil {
		t.Errorf("empty attachments should pass, got: %v", err)
	}
}

func TestValidateMessage_ImageAndAttachmentTogether(t *testing.T) {
	msg := IPCMessage{
		Type: MsgTypeSend,
		Text: "review with image and document",
		Images: []IPCImage{
			{Path: "/tmp/diagram.png", MediaType: "image/png"},
		},
		Attachments: []IPCAttachment{
			{Path: "/tmp/spec.pdf", Name: "spec.pdf"},
		},
	}
	if err := validateMessage(msg); err != nil {
		t.Errorf("message with image and attachment should pass, got: %v", err)
	}
}

func TestAttachmentJSONRoundTrip(t *testing.T) {
	original := IPCMessage{
		Type:   MsgTypeSend,
		ChatID: ReservedTUIChatID,
		Text:   "review these files",
		Attachments: []IPCAttachment{
			{Path: "/tmp/spec.pdf", Name: "spec.pdf"},
			{Path: "/home/user/report.docx"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded IPCMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(decoded.Attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d", len(decoded.Attachments))
	}
	if decoded.Attachments[0].Path != "/tmp/spec.pdf" {
		t.Errorf("attachment[0].Path = %q, want %q", decoded.Attachments[0].Path, "/tmp/spec.pdf")
	}
	if decoded.Attachments[0].Name != "spec.pdf" {
		t.Errorf("attachment[0].Name = %q, want %q", decoded.Attachments[0].Name, "spec.pdf")
	}
	if decoded.Attachments[1].Path != "/home/user/report.docx" {
		t.Errorf("attachment[1].Path = %q, want %q", decoded.Attachments[1].Path, "/home/user/report.docx")
	}
	if decoded.Attachments[1].Name != "" {
		t.Errorf("attachment[1].Name should be empty (omitempty), got %q", decoded.Attachments[1].Name)
	}
}
