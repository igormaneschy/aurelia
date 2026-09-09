package telegram

import "testing"

func TestMarkdownToHTML_ConvertsCommonFormatting(t *testing.T) {
	md := `## Plano

- **Bold**
- *Italic*
- ~~Strike~~
- ` + "`inline code`" + `

` + "```go\nfmt.Println(\"ok\")\n```" + `

[GitHub](https://github.com)
`

	got := MarkdownToHTML(md)

	wants := []string{
		"<b>Plano</b>",
		"• <b>Bold</b>",
		"• <i>Italic</i>",
		"• <s>Strike</s>",
		"<code>inline code</code>",
		"<pre><code class=\"language-go\">",
		"<a href=\"https://github.com\">GitHub</a>",
	}

	for _, want := range wants {
		if !containsSubstring(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestMarkdownToHTML_EscapesRawHTML(t *testing.T) {
	got := MarkdownToHTML("Use <div> tags carefully")
	if containsSubstring(got, "<div>") {
		t.Fatalf("expected raw html to be escaped, got: %s", got)
	}
	if !containsSubstring(got, "&lt;div&gt;") {
		t.Fatalf("expected escaped html, got: %s", got)
	}
}

func containsSubstring(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOfSubstring(s, sub) >= 0)
}

func indexOfSubstring(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestMarkdownToHTML_SeparatesBlocksForLongReplies pins the readability fix:
// block elements end with a blank line so long Telegram replies stay scannable
// (Telegram collapses a single newline into a plain line break).
func TestMarkdownToHTML_SeparatesBlocksForLongReplies(t *testing.T) {
	md := "Intro paragraph.\n\n## Seção\n\nTexto com **destaque**.\n\n```go\nx := 1\n```\n\n> citação\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nFim.\n"
	got := MarkdownToHTML(md)

	for _, want := range []string{"</b>\n\n", "</code></pre>\n\n", "</blockquote>\n\n", "</pre>\n\n"} {
		if !containsSubstring(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
	if containsSubstring(got, "\n\n\n\n") {
		t.Fatalf("excessive blank lines in output:\n%s", got)
	}
}
