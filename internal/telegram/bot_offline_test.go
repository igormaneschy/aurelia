package telegram

import (
	"testing"
	"time"
)

func TestBotControllerStopOfflineReturns(t *testing.T) {
	bc := &BotController{bot: newOfflineTestBot(t), offline: true}
	done := make(chan struct{})
	go func() {
		bc.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("offline Stop blocked; telebot would wait for a Start that never ran")
	}
}

func TestBotControllerOfflineSendersAreNoOps(t *testing.T) {
	bc := &BotController{bot: newOfflineTestBot(t), offline: true}
	if err := bc.ChatSender().Send(1, "test"); err != nil {
		t.Fatalf("offline ChatSender.Send: %v", err)
	}
	if err := bc.ChatSender().SendWithThread(1, 2, "test"); err != nil {
		t.Fatalf("offline ChatSender.SendWithThread: %v", err)
	}
}
