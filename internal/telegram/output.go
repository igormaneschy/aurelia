package telegram

import (
	"log"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

const telegramMessageLimit = 3900
const interChunkDelay = 200 * time.Millisecond

type messageSender interface {
	Send(to telebot.Recipient, what interface{}, opts ...interface{}) (*telebot.Message, error)
}

func SendText(bot *telebot.Bot, chat *telebot.Chat, text string) error {
	_, err := sendTextWithSender(bot, chat, text, telegramMessageLimit, 0)
	return err
}

func SendTextWithThread(bot *telebot.Bot, chat *telebot.Chat, text string, threadID int) error {
	_, err := sendTextWithSender(bot, chat, text, telegramMessageLimit, threadID)
	return err
}

func sendTextWithSender(sender messageSender, chat *telebot.Chat, text string, limit int, threadID int) (int64, error) {
	// Convert to HTML first, then split — HTML is larger than markdown due to tags
	htmlText := MarkdownToHTML(text)
	chunks := splitHTML(htmlText, limit)
	var lastMsgID int64
	for i, chunk := range chunks {
		isLast := i == len(chunks)-1
		opts := &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
			ThreadID:  threadID,
		}
		msg, err := sender.Send(chat, chunk, opts)
		if err == nil {
			if msg != nil {
				lastMsgID = int64(msg.ID)
			}
			if !isLast {
				time.Sleep(interChunkDelay)
			}
			continue
		}

		log.Printf("Send chunk with HTML failed (%v). Retrying as plain text...", err)
		opts = &telebot.SendOptions{ThreadID: threadID}
		msg, err = sender.Send(chat, chunk, opts)
		if err != nil {
			if floodErr, ok := err.(*telebot.FloodError); ok {
				log.Printf("Hit rate limit in chunk sending. Retrying in %v...", floodErr.RetryAfter)
				time.Sleep(time.Duration(floodErr.RetryAfter) * time.Second)
				if msg, retryErr := sender.Send(chat, chunk, opts); retryErr == nil {
					if msg != nil {
						lastMsgID = int64(msg.ID)
					}
					if !isLast {
						time.Sleep(interChunkDelay)
					}
					continue
				}
			}
			return lastMsgID, err
		}
		if msg != nil {
			lastMsgID = int64(msg.ID)
		}
		if !isLast {
			time.Sleep(interChunkDelay)
		}
	}
	return lastMsgID, nil
}

func splitTelegramMarkdown(text string, limit int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return []string{""}
	}

	// Convert to runes once; slicing by rune index is O(1) afterwards. The
	// previous implementation re-decoded the (shrinking) tail on every chunk
	// and bestSplitIndex re-decoded the head too — O(n²) on long replies.
	runes := []rune(trimmed)
	if len(runes) <= limit {
		return []string{trimmed}
	}

	var chunks []string
	for len(runes) > limit {
		splitAt := bestSplitIndexRunes(runes, limit)
		chunk := strings.TrimSpace(string(runes[:splitAt]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		runes = runes[splitAt:]
		// Drop leading whitespace runes without re-converting the slice.
		for len(runes) > 0 && (runes[0] == ' ' || runes[0] == '\t' || runes[0] == '\n' || runes[0] == '\r') {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		if tail := strings.TrimSpace(string(runes)); tail != "" {
			chunks = append(chunks, tail)
		}
	}
	return chunks
}

// splitHTML splits HTML content at safe boundaries (tag boundaries or whitespace)
// without breaking HTML tags. Must be called AFTER markdown→HTML conversion.
func splitHTML(html string, limit int) []string {
	trimmed := strings.TrimSpace(html)
	if trimmed == "" {
		return []string{""}
	}

	runes := []rune(trimmed)
	if len(runes) <= limit {
		return []string{trimmed}
	}

	var chunks []string
	for len(runes) > limit {
		splitAt := findHTMLSplitPoint(runes, limit)
		chunk := strings.TrimSpace(string(runes[:splitAt]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		runes = runes[splitAt:]
		// Drop leading whitespace
		for len(runes) > 0 && (runes[0] == ' ' || runes[0] == '\t' || runes[0] == '\n' || runes[0] == '\r') {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		if tail := strings.TrimSpace(string(runes)); tail != "" {
			chunks = append(chunks, tail)
		}
	}
	return chunks
}

// findHTMLSplitPoint finds a safe split position that doesn't break HTML tags.
// Tries: </tag> boundaries, > (end of opening tag), < (start of tag), then whitespace.
func findHTMLSplitPoint(runes []rune, limit int) int {
	if len(runes) <= limit {
		return len(runes)
	}

	// Look backwards from limit, trying to find safe split points
	// Priority: after closing tag, after opening tag, before tag, at whitespace
	for i := limit; i > 0; i-- {
		// Check if we're at a tag boundary (after '>' or before '<')
		if i < len(runes) && runes[i] == '<' {
			return i
		}
		if runes[i-1] == '>' {
			return i
		}
		// Check for whitespace
		if runes[i] == ' ' || runes[i] == '\n' {
			return i + 1
		}
	}
	return limit
}

// splitCandidates lists the preferred boundary substrings, ordered by
// readability — try paragraph break first, then sentence, then any space.
var splitCandidates = []string{"\n\n", "\n", ". ", " "}

// bestSplitIndexRunes returns a rune index in [0, limit] to split at. The
// returned index is suitable for `runes[:idx]`. Falls back to limit when no
// candidate boundary fits in the window.
func bestSplitIndexRunes(runes []rune, limit int) int {
	if len(runes) <= limit {
		return len(runes)
	}
	for _, candidate := range splitCandidates {
		cr := []rune(candidate)
		// Walk backwards from limit looking for the candidate substring.
		for i := limit - len(cr); i > 0; i-- {
			if runesEqualAt(runes, i, cr) {
				return i
			}
		}
	}
	return limit
}

func runesEqualAt(runes []rune, idx int, needle []rune) bool {
	if idx+len(needle) > len(runes) {
		return false
	}
	for i, r := range needle {
		if runes[idx+i] != r {
			return false
		}
	}
	return true
}

// SendTextReply sends text without reply-to quoting. Kept as a thin alias so
// existing callers do not need to switch to SendText; reply quoting was
// removed in v0.5.0 since the bot is the only participant alongside the user.
func SendTextReply(bot *telebot.Bot, chat *telebot.Chat, text string) error {
	_, err := sendTextReplyWithSender(bot, chat, text, telegramMessageLimit, 0)
	return err
}

func SendTextReplyWithThread(bot *telebot.Bot, chat *telebot.Chat, text string, threadID int) error {
	_, err := sendTextReplyWithSender(bot, chat, text, telegramMessageLimit, threadID)
	return err
}

func sendTextReplyWithSender(sender messageSender, chat *telebot.Chat, text string, limit int, threadID int) (int64, error) {
	// Convert to HTML first, then split — HTML is larger than markdown due to tags
	htmlText := MarkdownToHTML(text)
	chunks := splitHTML(htmlText, limit)
	var lastMsgID int64

	for i, chunk := range chunks {
		isLast := i == len(chunks)-1
		opts := &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
			ThreadID:  threadID,
		}

		msg, err := sender.Send(chat, chunk, opts)
		if err == nil {
			if msg != nil {
				lastMsgID = int64(msg.ID)
			}
			if !isLast {
				time.Sleep(interChunkDelay)
			}
			continue
		}

		log.Printf("Send chunk with HTML failed (%v). Retrying as plain text...", err)
		opts = &telebot.SendOptions{ThreadID: threadID}
		msg, err = sender.Send(chat, chunk, opts)
		if err != nil {
			return lastMsgID, err
		}
		if msg != nil {
			lastMsgID = int64(msg.ID)
		}
		if !isLast {
			time.Sleep(interChunkDelay)
		}
	}
	return lastMsgID, nil
}

func ReactToMessage(bot *telebot.Bot, chat *telebot.Chat, messageID int, emoji string) {
	if bot == nil || messageID == 0 || chat == nil {
		return
	}
	msg := &telebot.Message{ID: messageID, Chat: chat}
	err := bot.React(chat, msg, telebot.ReactionOptions{
		Reactions: []telebot.Reaction{{Type: "emoji", Emoji: emoji}},
	})
	if err != nil {
		log.Printf("React error: %v", err)
	}
}

func SendError(bot *telebot.Bot, chat *telebot.Chat, errMsg string) error {
	return sendErrorWithSender(bot, chat, "Erro", errMsg, 0)
}

func SendErrorWithThread(bot *telebot.Bot, chat *telebot.Chat, errMsg string, threadID int) error {
	return sendErrorWithSender(bot, chat, "Erro", errMsg, threadID)
}

func sendErrorWithSender(sender messageSender, chat *telebot.Chat, title, errMsg string, threadID int) error {
	formatted := ErrorMessage(title, errMsg)
	opts := &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
		ThreadID:  threadID,
	}
	_, err := sender.Send(chat, formatted, opts)
	if err == nil {
		return nil
	}

	log.Printf("Send error with HTML failed (%v). Retrying as plain text...", err)
	_, err = sender.Send(chat, title+"\n\n"+errMsg, &telebot.SendOptions{ThreadID: threadID})
	return err
}
