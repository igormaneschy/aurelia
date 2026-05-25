package cron

import (
	"context"
	"fmt"
	"log/slog"
)

// ChatSender sends text messages to a chat by ID.
type ChatSender interface {
	Send(chatID int64, text string) error
	// SendWithThread sends a text message to a specific chat thread.
	// threadID of 0 sends to the main chat (non-forum chats and backward compat).
	SendWithThread(chatID int64, threadID int, text string) error
}

// TelegramDelivery sends cron execution results to Telegram chats.
type TelegramDelivery struct {
	sender ChatSender
}

// NewTelegramDelivery creates a delivery that sends via the given sender.
func NewTelegramDelivery(sender ChatSender) *TelegramDelivery {
	return &TelegramDelivery{sender: sender}
}

// Deliver sends the cron job result or error to the target chat/thread.
func (d *TelegramDelivery) Deliver(ctx context.Context, job CronJob, result *ExecutionResult, execErr error) error {
	if job.TargetChatID == 0 {
		slog.Warn("Cron delivery skipped: no chat ID")
		return nil
	}

	output := ""
	if result != nil {
		output = result.Output
	}
	slog.Info("Cron delivery", "job_id", job.ID[:8], "chat", job.TargetChatID, "thread", job.TargetThreadID, "output_len", len(output), "error", execErr)

	send := func(text string) error {
		if job.TargetThreadID != 0 {
			return d.sender.SendWithThread(job.TargetChatID, job.TargetThreadID, text)
		}
		return d.sender.Send(job.TargetChatID, text)
	}

	if execErr != nil {
		return send(fmt.Sprintf("❌ Cron job %s falhou: %v", job.ID[:8], execErr))
	}
	if output == "" {
		return nil
	}
	header := fmt.Sprintf("📋 Resultado agendamento (%s):\n\n", job.ID[:8])
	return send(header + output)
}
