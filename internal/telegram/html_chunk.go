package telegram

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// htmlChunkToken is one indivisible unit of the HTML stream: either a complete
// tag (<b>, </b>, <a href="…">) or a text run ending at a whitespace boundary.
// Tags are never split, which is what keeps every emitted chunk parseable.
type htmlChunkToken struct {
	text   string
	isTag  bool
	isOpen bool   // opening tag (vs closing)
	void   bool   // self-closing tag (never stacked)
	name   string // lowercased tag name for open/close pairing
}

// splitHTML splits Telegram HTML into chunks that each fit limit runes and are
// individually tag-balanced. Telegram rejects a message whose HTML contains an
// unclosed tag; the previous implementation split at tag boundaries but did not
// close/reopen tags spanning the cut, so a <b>…</b> across the boundary made
// BOTH chunks invalid and the sender silently fell back to plain text — losing
// all formatting in long replies.
//
// Every chunk closes the tags open at the cut and the next chunk re-opens them
// (attributes preserved), so bold/code/links survive the split.
func splitHTML(html string, limit int) []string {
	trimmed := strings.TrimSpace(html)
	if trimmed == "" {
		return []string{""}
	}
	if limit <= 0 || utf8.RuneCountInString(trimmed) <= limit {
		return []string{trimmed}
	}

	tokens := tokenizeHTML(trimmed)
	var (
		chunks  []string
		open    []htmlChunkToken
		body    strings.Builder
		bodyLen int
	)

	// flush emits the current body with every open tag closed, then starts the
	// next body with those tags re-opened so formatting continues seamlessly.
	flush := func() {
		var sb strings.Builder
		sb.WriteString(body.String())
		for i := len(open) - 1; i >= 0; i-- {
			sb.WriteString("</" + open[i].name + ">")
		}
		if s := strings.TrimSpace(sb.String()); s != "" {
			chunks = append(chunks, s)
		}
		body.Reset()
		bodyLen = 0
		for _, tag := range open {
			body.WriteString(tag.text)
			bodyLen += utf8.RuneCountInString(tag.text)
		}
	}

	for _, token := range tokens {
		tokenLen := utf8.RuneCountInString(token.text)
		next := applyHTMLToken(open, token)

		// Flush when this token would overflow once the currently-open tags are
		// closed — unless the body holds nothing but reopened tags, in which
		// case flushing cannot help and the token is split instead.
		if bodyLen+tokenLen+closingTagsLen(next) > limit && bodyLen > closingTagsLen(open) {
			flush()
			next = applyHTMLToken(open, token)
		}

		// A single text run longer than the budget is split at a rune boundary
		// (tags never are).
		for !token.isTag {
			room := limit - bodyLen - closingTagsLen(next)
			if room <= 0 || tokenLen <= room {
				break
			}
			body.WriteString(runesPrefix(token.text, room))
			bodyLen += room
			token.text = runesSuffix(token.text, room)
			tokenLen = utf8.RuneCountInString(token.text)
			flush()
			next = applyHTMLToken(open, token)
		}

		body.WriteString(token.text)
		bodyLen += tokenLen
		open = next
	}
	flush()
	return chunks
}

// applyHTMLToken returns the open-tag stack after consuming token. Opening
// tags are pushed, closing tags pop the nearest matching tag, and void tags
// (self-closing) never enter the stack. The input slice is never mutated.
func applyHTMLToken(open []htmlChunkToken, token htmlChunkToken) []htmlChunkToken {
	if !token.isTag || token.void {
		return open
	}
	if token.isOpen {
		next := make([]htmlChunkToken, len(open)+1)
		copy(next, open)
		next[len(open)] = token
		return next
	}
	for i := len(open) - 1; i >= 0; i-- {
		if open[i].name == token.name {
			next := make([]htmlChunkToken, i)
			copy(next, open[:i])
			return next
		}
	}
	return open
}

// closingTagsLen is the rune cost of closing every open tag.
func closingTagsLen(open []htmlChunkToken) int {
	total := 0
	for _, tag := range open {
		total += len(tag.name) + 3 // "</name>"
	}
	return total
}

// tokenizeHTML splits an HTML string into tag and whitespace-bounded text
// tokens. A stray "<" that does not form a tag is treated as text.
func tokenizeHTML(html string) []htmlChunkToken {
	var tokens []htmlChunkToken
	runes := []rune(html)
	for i := 0; i < len(runes); {
		if runes[i] == '<' {
			j := i + 1
			for j < len(runes) && runes[j] != '>' {
				j++
			}
			if j < len(runes) {
				raw := string(runes[i : j+1])
				if name, closing, selfClosing, ok := parseHTMLTag(raw); ok {
					tokens = append(tokens, htmlChunkToken{
						text:   raw,
						isTag:  true,
						isOpen: !closing && !selfClosing,
						void:   selfClosing,
						name:   name,
					})
					i = j + 1
					continue
				}
			}
			tokens = append(tokens, splitHTMLText(string(runes[i:i+1]))...)
			i++
			continue
		}
		j := i
		for j < len(runes) && runes[j] != '<' {
			j++
		}
		tokens = append(tokens, splitHTMLText(string(runes[i:j]))...)
		i = j
	}
	return tokens
}

// splitHTMLText breaks a tag-free text run into tokens that end on whitespace,
// keeping the whitespace with the preceding word so chunk boundaries read
// naturally.
func splitHTMLText(text string) []htmlChunkToken {
	if text == "" {
		return nil
	}
	var tokens []htmlChunkToken
	runes := []rune(text)
	start := 0
	for i := 0; i < len(runes); i++ {
		if unicode.IsSpace(runes[i]) {
			tokens = append(tokens, htmlChunkToken{text: string(runes[start : i+1])})
			start = i + 1
		}
	}
	if start < len(runes) {
		tokens = append(tokens, htmlChunkToken{text: string(runes[start:])})
	}
	return tokens
}

// parseHTMLTag extracts the lowercased tag name and whether it is a closing or
// self-closing tag. ok is false when raw is not a well-formed "<…>" tag.
func parseHTMLTag(raw string) (name string, closing, selfClosing, ok bool) {
	if !strings.HasPrefix(raw, "<") || !strings.HasSuffix(raw, ">") {
		return "", false, false, false
	}
	inner := strings.TrimSpace(raw[1 : len(raw)-1])
	if inner == "" {
		return "", false, false, false
	}
	if strings.HasPrefix(inner, "/") {
		closing = true
		inner = strings.TrimSpace(inner[1:])
	}
	if strings.HasSuffix(inner, "/") {
		selfClosing = true
		inner = strings.TrimSpace(strings.TrimSuffix(inner, "/"))
	}
	end := len(inner)
	for i, r := range inner {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			end = i
			break
		}
	}
	name = strings.ToLower(inner[:end])
	if name == "" {
		return "", false, false, false
	}
	return name, closing, selfClosing, true
}

// runesPrefix returns the first n runes of s (fewer when s is shorter).
func runesPrefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// runesSuffix returns s without its first n runes.
func runesSuffix(s string, n int) string {
	if n <= 0 {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[i:]
		}
		count++
	}
	return ""
}
