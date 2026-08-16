package telegram

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gopkg.in/telebot.v3"

	pipelinepkg "github.com/igormaneschy/aurelia/internal/pipeline"
)

// progressEditInterval throttles edits to avoid Telegram rate limits.
// Telegram allows ~1 edit/sec per message; we leave a margin.
const progressEditInterval = 1500 * time.Millisecond

// progressDeliverer abstracts the two Telegram primitives used by the
// progress receipt: send the first message and edit the same message after.
// The production implementation wraps *telebot.Bot; tests inject a fake to
// verify the single-receipt contract without network access.
type progressDeliverer interface {
	Send(chat *telebot.Chat, text string, threadID int) (*telebot.Message, error)
	Edit(msg *telebot.Message, text string) error
}

// botDeliverer adapts *telebot.Bot to progressDeliverer.
type botDeliverer struct{ bot *telebot.Bot }

func (d botDeliverer) Send(chat *telebot.Chat, text string, threadID int) (*telebot.Message, error) {
	return d.bot.Send(chat, text, &telebot.SendOptions{ThreadID: threadID})
}

func (d botDeliverer) Edit(msg *telebot.Message, text string) error {
	_, err := d.bot.Edit(msg, text)
	return err
}

type progressReporter struct {
	bot           *telebot.Bot
	deliver       progressDeliverer
	chat          *telebot.Chat
	msg           *telebot.Message
	tools         []string
	latestThought string
	statusLine    string
	threadID      int
	startTime     time.Time
	lastText      string
	lastEdit      time.Time
	mu            sync.Mutex
}

func newProgressReporterWithThread(bot *telebot.Bot, chat *telebot.Chat, threadID int) *progressReporter {
	return &progressReporter{
		bot:       bot,
		deliver:   botDeliverer{bot: bot},
		chat:      chat,
		threadID:  threadID,
		startTime: time.Now(),
	}
}

// deliverDisplay updates the single progress receipt: send when no message
// exists yet, edit the same message afterwards, throttled by
// progressEditInterval (Telegram rate-limits per-message edits and progress
// is purely visual feedback — the user only sees the final assistant reply).
// Must be called while p.mu is held. Without a deliverer we cannot send
// anything — just update internal state.
func (p *progressReporter) deliverDisplay(display string) {
	if p.deliver == nil {
		return
	}

	if p.msg == nil {
		sent, err := p.deliver.Send(p.chat, display, p.threadID)
		if err != nil {
			log.Printf("Progress send error: %v", err)
			return
		}
		p.msg = sent
		p.lastText = display
		p.lastEdit = time.Now()
		return
	}

	if time.Since(p.lastEdit) < progressEditInterval {
		return
	}
	if err := p.deliver.Edit(p.msg, display); err != nil {
		log.Printf("Progress edit error: %v", err)
		return
	}
	p.lastText = display
	p.lastEdit = time.Now()
}

func (p *progressReporter) ReportTool(toolName, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	label := toolDisplayName(toolName)
	if detail != "" {
		label += " · " + truncateProgressDetail(detail)
	}
	p.tools = append(p.tools, label)

	text := p.buildDisplay()
	if text == p.lastText {
		return
	}
	p.deliverDisplay(text)
}

// ReportToolResult marks the last tool as completed with a checkmark.
// The full result summary is logged in runlog — we don't show it in progress
// to avoid noise and leaking internal operation details.
// If there are no tools yet, the result is silently dropped.
func (p *progressReporter) ReportToolResult(summary string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.tools) == 0 {
		return
	}

	// Just mark as done with checkmark — don't show the actual result
	// to keep progress clean and avoid exposing internal operation details
	lastIdx := len(p.tools) - 1
	p.tools[lastIdx] += " ✓"

	text := p.buildDisplay()
	if text == p.lastText {
		return
	}
	p.deliverDisplay(text)
}

func (p *progressReporter) ReportText(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.latestThought = text

	display := p.buildDisplay()
	if display == p.lastText {
		return
	}
	p.deliverDisplay(display)
}

// ReportState updates the single progress receipt with a human state line.
// The receipt keeps its throttle and is never duplicated — states edit the
// same message. Terminal states (done/canceled/failed) are no-ops here: the
// run caller deletes the receipt once the final reply/error is out.
func (p *progressReporter) ReportState(state pipelinepkg.ProgressState, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch state {
	case pipelinepkg.ProgressStateStallWarning:
		p.statusLine = "⚠️ Estou demorando mais que o normal — aguarde."
	case pipelinepkg.ProgressStateStallUrgent:
		p.statusLine = "🚨 Estou com dificuldade para responder. Se não voltar, tente /stop e reenvie."
	case pipelinepkg.ProgressStateWaiting:
		p.statusLine = "⏳ Ainda estou processando."
	case pipelinepkg.ProgressStateWorking:
		// Back to productive activity — clear the stall/waiting line.
		p.statusLine = ""
	default:
		// done/canceled/failed: receipt is deleted by the caller.
		return
	}

	display := p.buildDisplay()
	if display == p.lastText {
		return
	}
	p.deliverDisplay(display)
}

// buildDisplay returns the progress message text, including the latest
// thought block when present. Must be called while p.mu is held.
func (p *progressReporter) buildDisplay() string {
	text := progressText(p.tools, time.Since(p.startTime))
	if p.statusLine != "" {
		text += "\n" + p.statusLine
	}
	if p.latestThought != "" {
		if len(p.tools) > 0 {
			text += fmt.Sprintf("\n— %d ferramentas —", len(p.tools))
		}
		snippet := p.latestThought
		runes := []rune(snippet)
		if len(runes) > 300 {
			snippet = string(runes[:300]) + "..."
		}
		text += "\n\n" + snippet
	}
	return text
}

func progressText(tools []string, elapsed time.Duration) string {
	display := tools
	if len(display) > 8 {
		display = display[len(display)-8:]
	}
	return fmt.Sprintf("⏱️ %s\n%s", formatProgressDuration(elapsed), strings.Join(display, "\n"))
}

func formatProgressDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d.Round(time.Second).Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
}

func (p *progressReporter) Delete() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.latestThought = ""
	if p.msg != nil && p.bot != nil {
		_ = p.bot.Delete(p.msg)
		p.msg = nil
	}
}

func truncateProgressDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	runes := []rune(detail)
	if len(runes) <= 40 {
		return detail
	}
	return string(runes[:37]) + "..."
}

func toolDisplayName(name string) string {
	// Normalize to Title case for matching (bridge sends lowercase: "bash", "read", etc.)
	lowerName := strings.ToLower(name)
	if lowerName != "" {
		name = strings.ToUpper(lowerName[:1]) + lowerName[1:]
	}

	switch name {
	case "Read":
		return "📖 read"
	case "Write":
		return "✍️ write"
	case "Edit":
		return "✏️ edit"
	case "Bash":
		return "⚡ bash"
	case "Glob":
		return " glob"
	case "Grep":
		return "🔎 grep"
	case "WebSearch":
		return "🌐 web_search"
	case "WebFetch":
		return " web_fetch"
	default:
		return fmt.Sprintf("🔧 %s", strings.ToLower(name))
	}
}
