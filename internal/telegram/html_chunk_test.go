package telegram

import (
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/telebot.v3"
)

// assertBalancedHTML fails when chunk contains an unclosed or mismatched tag.
// Telegram rejects such a message outright ("can't parse entities"), which is
// the failure mode that silently downgraded long replies to plain text.
func assertBalancedHTML(t *testing.T, chunk string) {
	t.Helper()
	var stack []string
	runes := []rune(chunk)
	for i := 0; i < len(runes); {
		if runes[i] != '<' {
			i++
			continue
		}
		j := i + 1
		for j < len(runes) && runes[j] != '>' {
			j++
		}
		if j >= len(runes) {
			t.Fatalf("unterminated tag in chunk %q", chunk)
		}
		name, closing, selfClosing, ok := parseHTMLTag(string(runes[i : j+1]))
		if !ok {
			i = j + 1
			continue
		}
		switch {
		case selfClosing:
		case closing:
			if len(stack) == 0 || stack[len(stack)-1] != name {
				t.Fatalf("unbalanced closing tag %q (stack %v) in chunk %q", name, stack, chunk)
			}
			stack = stack[:len(stack)-1]
		default:
			stack = append(stack, name)
		}
		i = j + 1
	}
	if len(stack) != 0 {
		t.Fatalf("unclosed tags %v in chunk %q", stack, chunk)
	}
}

func visibleHTMLText(chunk string) string {
	var b strings.Builder
	runes := []rune(chunk)
	for i := 0; i < len(runes); {
		if runes[i] == '<' {
			j := i + 1
			for j < len(runes) && runes[j] != '>' {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

func TestSplitHTML_ShortHTMLIsUnchanged(t *testing.T) {
	html := "<b>oi</b> <code>x</code>"
	chunks := splitHTML(html, 100)
	if len(chunks) != 1 || chunks[0] != html {
		t.Fatalf("chunks = %#v, want single unchanged chunk", chunks)
	}
}

// TestSplitHTML_BalancesTagsAcrossChunks is the regression for the reported
// bug: a <b>…</b> spanning the cut made both chunks invalid and the whole
// reply was re-sent as plain text, losing all formatting.
func TestSplitHTML_BalancesTagsAcrossChunks(t *testing.T) {
	html := "<b>" + strings.Repeat("palavra ", 40) + "</b>"
	chunks := splitHTML(html, 60)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		assertBalancedHTML(t, chunk)
		if utf8.RuneCountInString(chunk) > 60 {
			t.Fatalf("chunk %d exceeds limit: %d runes", i, utf8.RuneCountInString(chunk))
		}
	}
}

func TestSplitHTML_BalancesNestedTags(t *testing.T) {
	html := "<b><code>" + strings.Repeat("dado ", 40) + "</code></b>"
	chunks := splitHTML(html, 50)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		assertBalancedHTML(t, chunk)
	}
}

// TestSplitHTML_ReopensTagsWithAttributes pins that a link spanning a cut is
// reopened with its href intact, not as a bare <a>.
func TestSplitHTML_ReopensTagsWithAttributes(t *testing.T) {
	html := `<a href="https://example.com/very/long/path">` + strings.Repeat("texto ", 30) + "</a>"
	chunks := splitHTML(html, 70)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		assertBalancedHTML(t, chunk)
		if i > 0 && !strings.Contains(chunk, `href="https://example.com/very/long/path"`) {
			t.Fatalf("chunk %d did not reopen the link with its attribute: %q", i, chunk)
		}
	}
}

func TestSplitHTML_PreservesVisibleText(t *testing.T) {
	html := "<b>" + strings.Repeat("alfa beta gama ", 30) + "</b>"
	chunks := splitHTML(html, 45)
	joined := ""
	for _, chunk := range chunks {
		joined += visibleHTMLText(chunk) + " "
	}
	for _, want := range []string{"alfa", "beta", "gama"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("visible text dropped %q: %q", want, joined)
		}
	}
}

func TestSplitHTML_PlainTextWithoutTags(t *testing.T) {
	html := strings.Repeat("palavra ", 50)
	chunks := splitHTML(html, 40)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if utf8.RuneCountInString(chunk) > 40 {
			t.Fatalf("chunk exceeds limit: %q", chunk)
		}
	}
}

// TestSplitHTML_MultiByteRunesStayIntact guards against slicing inside a rune.
func TestSplitHTML_MultiByteRunesStayIntact(t *testing.T) {
	html := "<b>" + strings.Repeat("ação ção ", 30) + "</b>"
	for _, chunk := range splitHTML(html, 25) {
		if !utf8.ValidString(chunk) {
			t.Fatalf("chunk is not valid UTF-8: %q", chunk)
		}
		assertBalancedHTML(t, chunk)
	}
}

// TestSendText_LongFormattedReplyStaysHTML is the end-to-end regression: a
// long reply must be delivered as several HTML messages with no plain-text
// fallback, so formatting survives.
func TestSendText_LongFormattedReplyStaysHTML(t *testing.T) {
	sender := &stubSender{}
	markdown := "## Relatório\n\n**Resumo:** " + strings.Repeat("detalhe importante ", 260)
	if _, err := sendTextWithSender(sender, &telebot.Chat{ID: 123}, markdown, telegramMessageLimit, 0); err != nil {
		t.Fatalf("sendTextWithSender returned error: %v", err)
	}
	if len(sender.calls) < 2 {
		t.Fatalf("expected the long reply to be split, got %d send call(s)", len(sender.calls))
	}
	for i, call := range sender.calls {
		options, ok := call.opts[0].(*telebot.SendOptions)
		if !ok || options.ParseMode != telebot.ModeHTML {
			t.Fatalf("call %d was not sent as HTML (options %#v) — formatting was lost", i, call.opts)
		}
		text, _ := call.what.(string)
		assertBalancedHTML(t, text)
	}
}
