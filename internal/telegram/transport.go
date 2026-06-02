package telegram

import (
	"context"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/transport"
)

// TelegramTransport adapts a telebot.Bot to the transport.Transport interface.
// It reuses the existing sending helpers in this package (sendTextWithSender,
// sendErrorWithSender, startChatActionLoop) so that message formatting and
// delivery logic stays consistent.
type TelegramTransport struct {
	bot *telebot.Bot
}

// compile-time interface satisfaction check.
var _ transport.Transport = (*TelegramTransport)(nil)

// NewTelegramTransport creates a new TelegramTransport wrapping the given bot.
func NewTelegramTransport(bot *telebot.Bot) *TelegramTransport {
	return &TelegramTransport{bot: bot}
}

// Name returns "telegram".
func (t *TelegramTransport) Name() string { return "telegram" }

// Send sends an outgoing message. When Markdown is true the message is
// converted to HTML, split at safe boundaries, and sent in chunks.
// When Markdown is false the text is sent as-is (plain text).
func (t *TelegramTransport) Send(ctx context.Context, msg transport.OutgoingMessage) error {
	if msg.Markdown {
		_, err := sendTextWithSender(t.bot, &telebot.Chat{ID: msg.ChatID}, msg.Text, telegramMessageLimit, msg.ThreadID)
		return err
	}
	_, err := t.bot.Send(&telebot.Chat{ID: msg.ChatID}, msg.Text, &telebot.SendOptions{ThreadID: msg.ThreadID})
	return err
}

// SendError sends an error-formatted message with the "⚠️ Erro" header.
func (t *TelegramTransport) SendError(ctx context.Context, chatID int64, threadID int, text string) error {
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

// SendText sends plain text and returns a message reference suitable for
// DeleteMessage. This is a Telegram-specific extension outside the Transport
// interface, needed because the pipeline Output interface's SendText returns
// a deletion token (*telebot.Message) for the rare case where a system
// message must be removed later (e.g. the reconnect flow).
func (t *TelegramTransport) SendText(chatID int64, threadID int, text string) (any, error) {
	if t.bot == nil {
		return nil, nil
	}
	return t.bot.Send(&telebot.Chat{ID: chatID}, text, &telebot.SendOptions{ThreadID: threadID})
}
