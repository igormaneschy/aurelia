package telegram

import (
	"context"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/transport"
)

// TelegramTransport adapts a telebot.Bot to the transport.Transport interface.
// It also implements DeletableTransport and ReactableTransport for Telegram-
// specific capabilities (message deletion and emoji reactions).
type TelegramTransport struct {
	bot *telebot.Bot
}

// compile-time interface satisfaction checks.
var (
	_ transport.Transport          = (*TelegramTransport)(nil)
	_ transport.DeletableTransport = (*TelegramTransport)(nil)
	_ transport.ReactableTransport = (*TelegramTransport)(nil)
)

// NewTelegramTransport creates a new TelegramTransport wrapping the given bot.
func NewTelegramTransport(bot *telebot.Bot) *TelegramTransport {
	return &TelegramTransport{bot: bot}
}

// Name returns "telegram".
func (t *TelegramTransport) Name() string { return "telegram" }

// Send sends an outgoing message. When Markdown is true the text is
// converted to HTML, split at safe boundaries, and sent in chunks;
// the returned handle is the last *telebot.Message sent.
// When Markdown is false the text is sent as-is (plain text).
func (t *TelegramTransport) Send(ctx context.Context, msg transport.OutgoingMessage) (transport.MessageHandle, error) {
	if t.bot == nil {
		return nil, nil
	}
	chat := &telebot.Chat{ID: msg.ChatID}
	if msg.Markdown {
		msgID, err := sendTextWithSender(t.bot, chat, msg.Text, telegramMessageLimit, msg.ThreadID)
		if err != nil {
			return nil, err
		}
		return &telebot.Message{ID: int(msgID), Chat: chat}, nil
	}
	return t.bot.Send(chat, msg.Text, &telebot.SendOptions{ThreadID: msg.ThreadID})
}

// SendError sends an error-formatted message with the "⚠️ Erro" header.
func (t *TelegramTransport) SendError(ctx context.Context, chatID int64, threadID int, text string) error {
	if t.bot == nil {
		return nil
	}
	return sendErrorWithSender(t.bot, &telebot.Chat{ID: chatID}, "Erro", text, threadID)
}

// StartTyping starts a periodic typing indicator. Returns a stop function
// that must be called to clean up the background goroutine.
func (t *TelegramTransport) StartTyping(chatID int64, threadID int) func() {
	return startChatActionLoop(t.bot, &telebot.Chat{ID: chatID}, telebot.Typing, typingIndicatorInterval, threadID)
}

// Receive returns a closed channel. Telegram is push-based and delivers
// messages via registered handlers, so the Receive channel is unused.
func (t *TelegramTransport) Receive() <-chan transport.IncomingMessage {
	ch := make(chan transport.IncomingMessage)
	close(ch)
	return ch
}

// Delete implements DeletableTransport. It deletes the message referenced
// by the handle (must be *telebot.Message). Safe no-op for nil handle or nil bot.
func (t *TelegramTransport) Delete(ctx context.Context, handle transport.MessageHandle) error {
	msg, ok := handle.(*telebot.Message)
	if !ok || msg == nil || t.bot == nil {
		return nil
	}
	return t.bot.Delete(msg)
}

// React implements ReactableTransport. It adds a 🎉 reaction to the message.
// Safe no-op for messageID == 0 or nil bot.
func (t *TelegramTransport) React(ctx context.Context, chatID int64, messageID int) error {
	if t.bot == nil || messageID == 0 {
		return nil
	}
	ReactToMessage(t.bot, &telebot.Chat{ID: chatID}, messageID, "🎉")
	return nil
}
