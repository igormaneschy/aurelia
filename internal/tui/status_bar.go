package tui

import (
	"fmt"
	"strings"
	"time"
)

const (
	statusBarExpandedMinWidth = 160
	statusBarSegmentSep       = " │ "
	statusBarHelpToken        = "F1 help"
)

type statusBarSegment struct {
	label string
	hit   statusBarHitKind
}

func (m Model) helpStatusLabel() string {
	if !m.mouseEnabled {
		return statusBarHelpToken
	}
	return m.styles.HeaderMetaStyle.Underline(true).Render(statusBarHelpToken)
}

func (m Model) statusBarSegments() []statusBarSegment {
	var segments []statusBarSegment

	for _, extra := range []statusBarSegment{
		{label: m.pendingCountLabel(), hit: statusBarHitNone},
		{label: m.historyNav.pageLabel(), hit: statusBarHitNone},
		{label: m.elapsedLabel(), hit: statusBarHitNone},
	} {
		if extra.label != "" {
			segments = append(segments, extra)
		}
	}

	segments = append(segments, statusBarSegment{label: m.themeStatusLabel(), hit: statusBarHitNone})
	segments = append(segments, statusBarSegment{label: m.helpStatusLabel(), hit: statusBarHitHelp})
	segments = append(segments, statusBarSegment{label: m.mouseStatusLabel(), hit: statusBarHitNone})

	if m.width < statusBarExpandedMinWidth {
		return segments
	}

	expanded := []struct {
		label string
		min   int
	}{
		{"↵ send", 44},
		{fmt.Sprintf("%s newline", newlineFallbackKey), 62},
		{"pg scroll", 80},
		{"esc cancel", 100},
		{"⌃L clear", 114},
		{"⌃P project", 128},
		{"⌃S · f2 · ⌃N", 144},
		{"⌃C quit", 158},
	}

	for _, item := range expanded {
		if item.label == "" {
			continue
		}
		if item.min > 0 && m.width < item.min {
			continue
		}
		segments = append(segments, statusBarSegment{label: item.label})
	}

	return segments
}

func (m Model) renderStatusBarSegment(seg statusBarSegment) string {
	if seg.hit == statusBarHitNone || seg.hit != m.statusBarHover {
		return seg.label
	}
	return lipglossHoverSegment(m.styles, seg.label)
}

func lipglossHoverSegment(styles themeStyles, label string) string {
	return styles.SidebarHoverStyle.Render(label)
}

func (m Model) renderStatusBar() string {
	var parts []string
	for _, seg := range m.statusBarSegments() {
		if seg.label == "" {
			continue
		}
		parts = append(parts, m.renderStatusBarSegment(seg))
	}

	content := strings.Join(parts, statusBarSegmentSep)
	width := m.composerColumnWidth()
	border := m.styles.MessageSeparatorStyle.Render(strings.Repeat("─", maxInt(20, width)))
	return border + "\n" + m.styles.StatusBarStyle.Width(maxInt(20, width)).Render(content)
}

// pendingCountLabel returns the pending count badge for the status bar.
func (m Model) pendingCountLabel() string {
	count := len(m.pendingQueue)
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("⏳ %d", count)
}

// elapsedLabel returns the elapsed time label for the status bar.
func (m Model) elapsedLabel() string {
	var elapsed time.Duration
	switch {
	case m.streamProgress.active && m.streamProgress.stopwatch.Running():
		elapsed = m.streamProgress.stopwatch.Elapsed()
	case !m.turnStart.IsZero():
		elapsed = time.Since(m.turnStart)
	default:
		return ""
	}
	return "⏱ " + formatElapsed(elapsed)
}