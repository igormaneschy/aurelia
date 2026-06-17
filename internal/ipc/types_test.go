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
