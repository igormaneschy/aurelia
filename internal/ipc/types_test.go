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
