package telegram

import (
	"context"
	"testing"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/transport"
)

func TestTelegramTransport_Delete_NilBot(t *testing.T) {
	tt := &TelegramTransport{bot: nil}
	err := tt.Delete(context.Background(), &telebot.Message{ID: 123})
	if err != nil {
		t.Fatalf("Delete with nil bot should return nil, got: %v", err)
	}
}

func TestTelegramTransport_Delete_NilHandle(t *testing.T) {
	tt := &TelegramTransport{bot: nil}
	err := tt.Delete(context.Background(), nil)
	if err != nil {
		t.Fatalf("Delete with nil handle should return nil, got: %v", err)
	}
}

func TestTelegramTransport_Delete_WrongHandleType(t *testing.T) {
	tt := &TelegramTransport{bot: nil}
	err := tt.Delete(context.Background(), "not-a-telebot-message")
	if err != nil {
		t.Fatalf("Delete with wrong handle type should return nil, got: %v", err)
	}
}

func TestTelegramTransport_React_NilBot(t *testing.T) {
	tt := &TelegramTransport{bot: nil}
	err := tt.React(context.Background(), 42, 123)
	if err != nil {
		t.Fatalf("React with nil bot should return nil, got: %v", err)
	}
}

func TestTelegramTransport_React_MessageIDZero(t *testing.T) {
	tt := &TelegramTransport{bot: nil}
	err := tt.React(context.Background(), 42, 0)
	if err != nil {
		t.Fatalf("React with messageID 0 should return nil, got: %v", err)
	}
}

func TestTelegramTransport_Send_NilBot(t *testing.T) {
	tt := &TelegramTransport{bot: nil}
	handle, err := tt.Send(context.Background(), transport.OutgoingMessage{ChatID: 1, Text: "test"})
	if err != nil {
		t.Fatalf("Send with nil bot should return nil error, got: %v", err)
	}
	if handle != nil {
		t.Errorf("Send with nil bot should return nil handle, got: %v", handle)
	}
}

func TestTelegramTransport_SendError_NilBot(t *testing.T) {
	tt := &TelegramTransport{bot: nil}
	err := tt.SendError(context.Background(), 42, 0, "something broke")
	if err != nil {
		t.Fatalf("SendError with nil bot should return nil, got: %v", err)
	}
}
