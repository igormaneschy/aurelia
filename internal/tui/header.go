package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

func (m Model) healthChipIcon() string {
	switch m.chromeState() {
	case "ready":
		return "🟢"
	case "waiting", "connecting":
		return "🟡"
	case "offline", "error":
		return "🔴"
	default:
		return "🟡"
	}
}

func (m Model) renderHealthChip() string {
	state := m.chromeState()
	if m.waiting {
		spinnerView := m.spinner.View()
		if m.animations.enabled && m.animations.SpinnerOpacity() < 0.95 {
			spinnerView = fadeStyle(m.styles.HeaderMetaStyle, m.animations.SpinnerOpacity()).Render(strings.TrimSpace(spinnerView))
		}
		return spinnerView + " thinking" + thinkingDots()
	}

	label := fmt.Sprintf("%s %s", m.healthChipIcon(), state)
	switch state {
	case "offline", "error":
		return m.styles.StatusErrorStyle.Render(label)
	case "waiting", "connecting":
		return m.styles.StatusBusyStyle.Render(label)
	default:
		return m.styles.StatusReadyStyle.Render(label)
	}
}

func (m Model) renderHeaderModelChip() string {
	label := sidebarModelPlaceholder
	if m.activeModel != "" {
		label = truncateMiddle(m.activeModel, maxInt(12, m.contentWidth()/4))
	}
	chip := m.styles.ChipStyle.Render(label)
	if m.mouseEnabled {
		chip = m.styles.ChipStyle.Underline(true).Render(label)
	}
	return chip
}

func (m Model) renderModeChip() string {
	if !m.isChatMode() {
		return ""
	}
	return m.styles.ChatModeStyle.Render("chat mode")
}

func (m Model) headerMetaChips() []string {
	var chips []string
	if model := m.renderHeaderModelChip(); model != "" {
		chips = append(chips, model)
	}
	chips = append(chips, m.renderHealthChip())
	if mode := m.renderModeChip(); mode != "" {
		chips = append(chips, mode)
	}
	return chips
}

func (m Model) decorativeHeaderRule(width int) string {
	pattern := "░▒▓"
	if !m.animations.enabled {
		pattern = "─"
	}
	repeat := maxInt(20, width-2)
	var b strings.Builder
	for i := 0; i < repeat; i++ {
		b.WriteRune(rune(pattern[i%len(pattern)]))
	}
	return m.styles.HeaderRuleStyle.Render(b.String())
}

func (m Model) renderChatHeader() string {
	sessionName := "DM"
	for _, s := range m.sessions {
		if s.ChatID == m.activeSession {
			if s.ChatID != ipc.ReservedTUIChatID {
				sessionName = safeSessionLabel(s.Name)
			}
			break
		}
	}

	meta := strings.Join(m.headerMetaChips(), "   ·   ")
	title := m.styles.HeaderTitleStyle.Render("Aurelia / "+sessionName)
	header := lipgloss.JoinVertical(
		lipgloss.Left,
		title+"  "+m.styles.HeaderMetaStyle.Render(meta),
		m.decorativeHeaderRule(m.contentWidth()),
	)
	return lipgloss.NewStyle().Width(m.contentWidth()).Render(header)
}